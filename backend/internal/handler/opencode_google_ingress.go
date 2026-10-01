package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// OpenCodeGenerateContent serves the Google-protocol column OpenCode publishes
// at {base}/models/{model}:{method} on the shared /v1beta/models/{model}:{method}
// ingress.
//
// The inbound segment carries a method rather than a gateway action, so the
// action is resolved here (countTokens is a non-billable estimate and must not
// be mistaken for generate_content). Account selection is plan aware: the Go
// subscription publishes no Google-shaped route at all, so a Go-plan account is
// never a candidate and the request switches to another account instead of
// failing after the request was claimed.
func (h *GatewayHandler) OpenCodeGenerateContent(c *gin.Context) {
	requestStart := time.Now()
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		googleError(c, http.StatusUnauthorized, "Invalid API key")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		googleError(c, http.StatusInternalServerError, "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.gateway.opencode_generate_content",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)

	modelName, googleMethod, err := parseGeminiModelAction(strings.TrimPrefix(c.Param("modelAction"), "/"))
	if err != nil {
		googleError(c, http.StatusNotFound, err.Error())
		return
	}
	callerModel := clientRequestedModel(c, modelName)
	action, stream, ok := service.OpenCodeGoogleActionForMethod(googleMethod)
	if !ok {
		googleError(c, http.StatusNotFound, "Unsupported OpenCode Google-protocol method")
		return
	}
	if resolvedModel, resolved := service.ResolvedUpstreamModelFromContext(c.Request.Context()); resolved && strings.TrimSpace(resolvedModel) != "" {
		modelName = strings.TrimSpace(resolvedModel)
	}
	if !service.IsSafeGeminiModelPathSegment(modelName) {
		googleError(c, http.StatusBadRequest, "Invalid model in URL")
		return
	}
	reqLog = reqLog.With(zap.String("model", modelName), zap.String("action", action), zap.Bool("stream", stream))

	if alt := c.Query("alt"); alt != "" && alt != "json" && alt != "sse" {
		googleError(c, http.StatusBadRequest, "alt must be json or sse")
		return
	}

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			googleError(c, http.StatusRequestEntityTooLarge, buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		googleError(c, http.StatusBadRequest, "Failed to read request body")
		return
	}
	if len(body) == 0 {
		googleError(c, http.StatusBadRequest, "Request body is empty")
		return
	}

	setOpsRequestContext(c, modelName, stream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(stream, false)))
	defer beginGatewayRequestPlan(c, h.gatewayService, apiKey, modelName, "gemini", h.maxAccountSwitchesGemini)()
	// 入口前置 hold：先把周期预算占用下来再调用上游，结算时按实际消耗修正。
	if !reserveGatewayRequestBudget(c, apiKey, modelName, body) {
		return
	}
	if !compositeTargetPlatformAllowed(c, apiKey, modelName, service.PlatformOpenCodeGo) {
		googleError(c, http.StatusBadRequest, "Model is not supported by this OpenCode endpoint for composite groups")
		return
	}
	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolGemini, modelName, body); decision != nil && !decision.AllowNextStage {
		googleSecurityAuditError(c, decision)
		return
	}

	pricingCtx, pricingAt := service.WithGatewayTokenRequestPricing(c.Request.Context())
	c.Request = c.Request.WithContext(pricingCtx)
	if action == "count_tokens" {
		// count_tokens is a non-billable estimate; the profit gate must not turn a
		// free count into "no available accounts" for a throttled key.
		c.Request = c.Request.WithContext(service.WithOpenAIProfitControlSuppressed(c.Request.Context()))
	}
	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, modelName)

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		reqLog.Info("opencode.generate_content.billing_eligibility_check_failed", zap.Error(err))
		status, _, message, _ := billingErrorDetails(err)
		writeAdmissionRejectionHeaders(c, err)
		googleError(c, status, message)
		return
	}

	sessionHash := h.openAIGatewayService.GenerateExplicitSessionHash(c, body)
	excluded := map[int64]struct{}{}
	maxSwitches := h.maxAccountSwitchesGemini
	if maxSwitches <= 0 {
		maxSwitches = 1
	}

	for attempt := 0; attempt <= maxSwitches; attempt++ {
		selection, _, err := h.openAIGatewayService.SelectAccountWithSchedulerForCapability(
			c.Request.Context(), apiKey.GroupID, "", sessionHash, modelName, excluded,
			service.OpenAIUpstreamTransportAny, "", false, false, false,
			service.PlatformOpenCodeGo,
		)
		if err != nil || selection == nil || selection.Account == nil {
			if failoverClientGone(c) {
				return
			}
			cls := classifyOpenAICompatibleNoAccountErrorFromGin(c, h.openAIGatewayService, apiKey, modelName, modelName)
			if !cls.ModelNotFound {
				markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
			}
			googleError(c, cls.Status, cls.Message)
			return
		}
		account := selection.Account
		release := selection.ReleaseFunc
		if account.Platform != service.PlatformOpenCodeGo {
			if release != nil {
				release()
			}
			excluded[account.ID] = struct{}{}
			continue
		}
		setOpsSelectedAccount(c, account.ID, account.Platform)

		writerSizeBeforeForward := c.Writer.Size()
		var result *service.OpenAIForwardResult
		var forwardErr error
		// Keep the native Google column native after account selection. Endpoint
		// membership is account-catalog-backed, including configured aliases; a
		// Go plan or a different native text column uses its existing forwarder.
		native := service.NativeOpenCodeGoogleModel(c.Request.Context(), account, modelName)
		if native {
			binding := service.ResolveAccountModelBinding(c.Request.Context(), account, modelName, service.AccountModelBindingOptions{Endpoint: "generate_content"})
			result, forwardErr = h.openAIGatewayService.ForwardOpenCodeGoogleAction(c.Request.Context(), c, account, action, binding.UpstreamModel, body)
		} else {
			result, forwardErr = h.forwardOpenCodeGeminiText(c, account, body, modelName, callerModel, googleMethod, stream)
		}
		if release != nil {
			release()
		}
		if forwardErr == nil {
			if result != nil {
				reqLog.Info("opencode.generate_content.completed",
					zap.Int64("account_id", account.ID),
					zap.String("upstream_model", result.UpstreamModel),
					zap.Bool("stream", result.Stream),
				)
				h.recordOpenCodeGoogleUsage(c, reqLog, apiKey, subject, subscription, account, result, body, callerModel, pricingAt, channelMapping, action)
			}
			return
		}
		if writeGatewayTPMRequestError(c, forwardErr) {
			return
		}
		if result != nil && (result.Usage.InputTokens > 0 || result.Usage.OutputTokens > 0) {
			h.recordOpenCodeGoogleUsage(c, reqLog, apiKey, subject, subscription, account, result, body, callerModel, pricingAt, channelMapping, action)
		}
		var failoverErr *service.UpstreamFailoverError
		if errors.As(forwardErr, &failoverErr) && c.Writer.Size() == writerSizeBeforeForward {
			// Nothing was written downstream yet, so the request can move to
			// another OpenCode account (for example from a throttled Zen key).
			excluded[account.ID] = struct{}{}
			reqLog.Warn("opencode.generate_content.failover_switching",
				zap.Int64("account_id", account.ID), zap.Int("upstream_status", failoverErr.StatusCode))
			continue
		}
		reqLog.Warn("opencode.generate_content.forward_failed", zap.Int64("account_id", account.ID), zap.Error(forwardErr))
		return
	}

	googleError(c, http.StatusBadGateway, "OpenCode request failed after account switches")
}

func (h *GatewayHandler) forwardOpenCodeGeminiText(c *gin.Context, account *service.Account, body []byte, model, callerModel, method string, stream bool) (*service.OpenAIForwardResult, error) {
	converted, err := service.GeminiToMessages(body, model, stream)
	if err != nil {
		googleError(c, http.StatusBadRequest, err.Error())
		return nil, err
	}
	writer := service.NewGeminiProtocolWriter(c.Writer, callerModel, method, stream, 0)
	if stream && c.Query("alt") != "sse" {
		writer.UseJSONStream()
	}
	c.Writer = writer
	defer func() { c.Writer = writer.ResponseWriter }()
	var result *service.OpenAIForwardResult
	if method == "countTokens" {
		err = h.openAIGatewayService.ForwardCountTokensAsAnthropic(c.Request.Context(), c, account, converted, model)
	} else {
		result, err = h.openAIGatewayService.ForwardAsAnthropic(c.Request.Context(), c, account, converted, "", model)
	}
	if err != nil && !writer.Written() {
		return result, err
	}
	if finishErr := writer.Finish(); err == nil {
		err = finishErr
	}
	return result, err
}

// recordOpenCodeGoogleUsage settles a successful OpenCode Google-protocol action
// through the shared OpenAI usage trunk. Token counting is an estimate the
// gateway produced itself, so it never carries billable units.
func (h *GatewayHandler) recordOpenCodeGoogleUsage(
	c *gin.Context,
	reqLog *zap.Logger,
	apiKey *service.APIKey,
	subject middleware2.AuthSubject,
	subscription *service.UserSubscription,
	account *service.Account,
	result *service.OpenAIForwardResult,
	body []byte,
	requestModel string,
	pricingAt time.Time,
	channelMapping service.ChannelMappingResult,
	action string,
) {
	if action == "count_tokens" {
		reqLog.Debug("opencode.generate_content.count_tokens_not_billed", zap.Int64("account_id", account.ID))
		return
	}
	userAgent := c.GetHeader("User-Agent")
	clientIP := ip.GetClientIP(c)
	requestPayloadHash := service.HashUsageRequestPayload(body)
	inboundEndpoint := GetInboundEndpoint(c)
	upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
	quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
	sessionID := service.ExtractClientSessionID(c)
	upstreamModel := result.UpstreamModel
	h.submitMandatoryUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
		if err := h.openAIGatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
			Result:             result,
			QuotaPlatform:      quotaPlatform,
			APIKey:             apiKey,
			User:               apiKey.User,
			Account:            account,
			Subscription:       subscription,
			PricingAt:          pricingAt,
			InboundEndpoint:    inboundEndpoint,
			UpstreamEndpoint:   upstreamEndpoint,
			UserAgent:          userAgent,
			IPAddress:          clientIP,
			SessionID:          sessionID,
			RequestPayloadHash: requestPayloadHash,
			APIKeyService:      h.apiKeyService,
			ChannelUsageFields: clientRequestedUsageFields(c, channelMapping, requestModel, upstreamModel),
		}); err != nil {
			logger.L().With(
				zap.String("component", "handler.gateway.opencode_generate_content"),
				zap.Int64("user_id", subject.UserID),
				zap.Int64("api_key_id", apiKey.ID),
				zap.Any("group_id", apiKey.GroupID),
				zap.String("model", requestModel),
				zap.Int64("account_id", account.ID),
			).Error("opencode.generate_content.record_usage_failed", zap.Error(err))
		}
	})
	reqLog.Debug("opencode.generate_content.usage_recorded", zap.Int64("account_id", account.ID))
}
