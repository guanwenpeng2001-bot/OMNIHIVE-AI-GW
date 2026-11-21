package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	pkghttputil "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/httputil"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ip"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/logger"
	middleware2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"go.uber.org/zap"
)

// AntigravityImages is the provider-side entry for the Antigravity standard-entry
// image actions (image.generate / image.edit).
//
// Route registration and the domain dispatch binding for "antigravity.images" are
// owned by the multimodal-ingress branch; this entry performs admission,
// authorization, account selection, provider dispatch and usage settlement so the
// Antigravity adapter can be exercised. Antigravity OAuth accounts use their own
// OAuth/project identity through AntigravityGatewayService.ForwardImages;
// Antigravity API-key deployments keep using the Gemini-compatible bridge. Neither
// path borrows a Gemini API key.
func (h *GatewayHandler) AntigravityImages(c *gin.Context) {
	requestStart := time.Now()
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.gateway.antigravity_images",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}

	request, err := service.ParseAntigravityImagesRequest(c.Request.URL.Path, c.GetHeader("Content-Type"), body)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	action := request.Action
	requestModel := strings.TrimSpace(request.Model)
	reqLog = reqLog.With(zap.String("model", requestModel), zap.String("action", action))
	defer beginGatewayRequestPlan(c, h.gatewayService, apiKey, requestModel, "media", h.maxAccountSwitches)()
	if c.IsAborted() {
		return
	}
	// 入口前置 hold：媒体/音频/realtime/异步 job 在调用上游之前先按估算占用周期预算，
	// 结算时按实际消耗修正；没有周期预算的 Key 不产生额外工作（T3-2.2）。
	if !reserveGatewayRequestBudget(c, apiKey, requestModel, body) {
		return
	}
	setOpsRequestContext(c, requestModel, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeSync))

	// Composite groups must have resolved this request to Antigravity; the
	// explicit ordered target set, not the model name, is the authorization.
	if !compositeTargetPlatformAllowed(c, apiKey, requestModel, service.PlatformAntigravity) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by this Antigravity endpoint for composite groups")
		return
	}

	if !service.GroupAllowsImageGeneration(apiKey.Group) {
		h.errorResponse(c, http.StatusForbidden, "permission_error", service.ImageGenerationPermissionMessage())
		return
	}
	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIImages, requestModel, antigravityImagesAuditBody(request, body)); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		reqLog.Info("antigravity.images.billing_eligibility_check_failed", zap.Error(err))
		status, code, message, _ := billingErrorDetails(err)
		writeAdmissionRejectionHeaders(c, err)
		h.errorResponse(c, status, code, message)
		return
	}
	pricingCtx, pricingAt := service.WithGatewayTokenRequestPricing(c.Request.Context())
	c.Request = c.Request.WithContext(pricingCtx)
	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, requestModel)

	sessionHash := h.openAIGatewayService.GenerateExplicitSessionHash(c, body)
	excluded := map[int64]struct{}{}
	maxSwitches := h.maxAccountSwitches
	if maxSwitches <= 0 {
		maxSwitches = 1
	}

	for attempt := 0; attempt <= maxSwitches; attempt++ {
		selection, err := h.gatewayService.SelectAccountWithLoadAwareness(
			c.Request.Context(), apiKey.GroupID, sessionHash, requestModel, excluded, "", subject.UserID,
		)
		if err != nil {
			if failoverClientGone(c) {
				return
			}
			cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, requestModel, requestModel, service.PlatformAntigravity)
			if !cls.ModelNotFound {
				markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
			}
			h.errorResponse(c, cls.Status, cls.ErrType, cls.Message)
			return
		}
		if selection == nil || selection.Account == nil {
			markOpsRoutingCapacityLimited(c)
			h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No eligible Antigravity image accounts")
			return
		}
		account := selection.Account
		release := selection.ReleaseFunc
		if account.Platform != service.PlatformAntigravity {
			if release != nil {
				release()
			}
			excluded[account.ID] = struct{}{}
			continue
		}
		setOpsSelectedAccount(c, account.ID, account.Platform)

		writerSizeBeforeForward := c.Writer.Size()
		var result *service.ForwardResult
		if account.Type == service.AccountTypeAPIKey {
			result, err = h.geminiCompatService.ForwardAction(c.Request.Context(), c, account, action, request.CanonicalBody)
		} else {
			result, err = h.antigravityGatewayService.ForwardImages(
				c.Request.Context(),
				c,
				account,
				request,
				service.WithForwardGeminiSession(derefGroupID(apiKey.GroupID), sessionHash),
			)
		}
		if release != nil {
			release()
		}
		if err == nil {
			if result != nil {
				reqLog.Info("antigravity.images.completed",
					zap.Int64("account_id", account.ID),
					zap.String("upstream_model", result.UpstreamModel),
					zap.Int("image_count", result.ImageCount),
				)
				h.recordAntigravityImageUsage(c, reqLog, apiKey, subject, subscription, account, result, body, requestModel, pricingAt, channelMapping)
			}
			return
		}
		var failoverErr *service.UpstreamFailoverError
		if errors.As(err, &failoverErr) && c.Writer.Size() == writerSizeBeforeForward {
			// Nothing was written downstream yet, so the request can move to
			// another account on the same provider identity.
			excluded[account.ID] = struct{}{}
			reqLog.Warn("antigravity.images.failover_switching", zap.Int64("account_id", account.ID), zap.Int("upstream_status", failoverErr.StatusCode))
			continue
		}
		// The upstream path already wrote (or deliberately suppressed) the
		// response; surface the failure as-is.
		reqLog.Warn("antigravity.images.forward_failed", zap.Int64("account_id", account.ID), zap.Error(err))
		return
	}

	h.errorResponse(c, http.StatusBadGateway, "api_error", "Antigravity image request failed after account switches")
}

// antigravityImagesAuditBody prefers the transport-independent prompt/image
// payload so multipart uploads are audited the same way as JSON requests.
func antigravityImagesAuditBody(request *service.AntigravityImagesRequest, raw []byte) []byte {
	if request != nil && len(request.ModerationBody) > 0 {
		return request.ModerationBody
	}
	return raw
}

// recordAntigravityImageUsage settles one successful image action through the
// shared gateway trunk. Image pricing is per generated image and by size tier,
// which ForwardGemini already recorded on the forward result, so the trunk bills
// it without any provider-specific price table. Submission is plan-gated, so a
// retried or duplicated call cannot double-bill the same request.
func (h *GatewayHandler) recordAntigravityImageUsage(
	c *gin.Context,
	reqLog *zap.Logger,
	apiKey *service.APIKey,
	subject middleware2.AuthSubject,
	subscription *service.UserSubscription,
	account *service.Account,
	result *service.ForwardResult,
	body []byte,
	requestModel string,
	pricingAt time.Time,
	channelMapping service.ChannelMappingResult,
) {
	userAgent := c.GetHeader("User-Agent")
	clientIP := ip.GetClientIP(c)
	requestPayloadHash := service.HashUsageRequestPayload(body)
	inboundEndpoint := GetInboundEndpoint(c)
	upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
	quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
	sessionID := service.ExtractClientSessionID(c)
	upstreamModel := result.UpstreamModel
	h.submitMandatoryUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
		if err := h.gatewayService.RecordUsage(ctx, &service.RecordUsageInput{
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
				zap.String("component", "handler.gateway.antigravity_images"),
				zap.Int64("user_id", subject.UserID),
				zap.Int64("api_key_id", apiKey.ID),
				zap.Any("group_id", apiKey.GroupID),
				zap.String("model", requestModel),
				zap.Int64("account_id", account.ID),
			).Error("antigravity.images.record_usage_failed", zap.Error(err))
		}
	})
	reqLog.Debug("antigravity.images.usage_recorded", zap.Int64("account_id", account.ID))
}
