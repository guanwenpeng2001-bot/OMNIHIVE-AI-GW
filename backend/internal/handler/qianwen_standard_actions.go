package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// Standard-ingress Qianwen actions: OpenAI-shape requests served by DashScope
// compatible mode or native endpoints with native billing. Admission, security
// audit, billing eligibility, concurrency slots, scheduling and usage recording
// mirror the existing OpenAI-compatible handlers; account selection is pinned
// to the request's Qianwen platform (group or explicit composite route).

type qianwenStandardAdmission struct {
	key          *service.APIKey
	subject      middleware2.AuthSubject
	subscription *service.UserSubscription
	body         []byte
	model        string
	pricingAt    time.Time
}

// admitQianwenStandardRequest runs admission before account selection.
// forwardQianwenStandard installs the action plan after selection and before send.
func (h *OpenAIGatewayHandler) admitQianwenStandardRequest(c *gin.Context, auditProtocol string) (*qianwenStandardAdmission, bool) {
	key, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || key == nil || key.Group == nil {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return nil, false
	}
	if openAICompatibleRequestPlatform(c.Request.Context(), key) != service.PlatformQianwen {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "This endpoint requires a Qianwen group or an explicit Qianwen model route")
		return nil, false
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid user context")
		return nil, false
	}
	if !h.ensureResponsesDependencies(c, nil) {
		return nil, false
	}
	body, err := httputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return nil, false
	}
	if !gjson.ValidBytes(body) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return nil, false
	}
	model := gjson.GetBytes(body, "model").String()
	if model == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, false
	}
	ensureCompositeTargetPlatform(c, key, model)
	if !compositeTargetPlatformAllowed(c, key, model, service.PlatformQianwen) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by this Qianwen endpoint for composite groups")
		return nil, false
	}
	reqLog := requestLogger(c, "handler.qianwen_standard", zap.String("model", model))
	if decision := h.checkSecurityAudit(c, reqLog, key, subject, auditProtocol, model, body); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return nil, false
	}
	if h.rejectIfCyberSessionBlocked(c, key, body, model, cyberBlockFormatChat) {
		return nil, false
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), key.User, key, key.Group, subscription, service.QuotaPlatform(c.Request.Context(), key)); err != nil {
		status, code, message, _ := billingErrorDetails(err)
		writeAdmissionRejectionHeaders(c, err)
		h.errorResponse(c, status, code, message)
		return nil, false
	}
	pricingCtx, pricingAt := h.gatewayService.WithOpenAIRequestPricingContext(c.Request.Context(), key.GroupID)
	c.Request = c.Request.WithContext(pricingCtx)
	return &qianwenStandardAdmission{key: key, subject: subject, subscription: subscription, body: body, model: model, pricingAt: pricingAt}, true
}

// forwardQianwenStandard selects one pinned Qianwen account (with the user and
// account concurrency slots of the existing handlers) and runs the forward.
// Billable generation is never retried behind the client's back; upstream
// failures return before any usage is recorded.
func (h *OpenAIGatewayHandler) forwardQianwenStandard(c *gin.Context, adm *qianwenStandardAdmission, protocol string, capability service.OpenAIEndpointCapability, forward func(account *service.Account) (*service.OpenAIForwardResult, error)) (*service.OpenAIForwardResult, *service.Account, bool) {
	reqLog := requestLogger(c, "handler.qianwen_standard")
	streamStarted := false
	release, ok := h.acquireResponsesUserSlot(c, adm.subject.UserID, adm.subject.Concurrency, false, &streamStarted, reqLog)
	if !ok {
		return nil, nil, false
	}
	if release != nil {
		defer release()
	}
	sessionHash := h.gatewayService.GenerateSessionHash(c, adm.body)
	selection, _, err := h.gatewayService.SelectAccountWithSchedulerForCapability(c.Request.Context(), adm.key.GroupID, "", sessionHash, adm.model, nil, service.OpenAIUpstreamTransportHTTPSSE, capability, false, false, true, service.PlatformQianwen)
	if err != nil || selection == nil || selection.Account == nil {
		message := "No Qianwen account available for this model"
		if err != nil {
			message = message + ": " + err.Error()
		}
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", message)
		return nil, nil, false
	}
	account := selection.Account
	accountRelease, slot := h.acquireResponsesAccountSlot(c, adm.key.GroupID, sessionHash, selection, false, &streamStarted, reqLog)
	if slot != openAISlotAcquireOK {
		return nil, nil, false
	}
	if accountRelease != nil {
		defer accountRelease()
	}
	defer beginGatewayRequestPlan(c, h.gatewayService, adm.key, adm.model, protocol, h.maxAccountSwitches)()
	setOpsSelectedAccount(c, account.ID, account.Platform)
	result, err := forward(account)
	if writeGatewayTPMError(c, err) {
		return nil, nil, false
	}
	if err != nil || result == nil {
		return nil, nil, false
	}
	return result, account, true
}

func (h *OpenAIGatewayHandler) recordQianwenStandardUsage(c *gin.Context, adm *qianwenStandardAdmission, account *service.Account, result *service.OpenAIForwardResult, action string) {
	h.submitOpenAIUsageRecordTask(c.Request.Context(), result, func(ctx context.Context) {
		if err := h.gatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
			Result:           result,
			APIKey:           adm.key,
			User:             adm.key.User,
			Account:          account,
			Subscription:     adm.subscription,
			InboundEndpoint:  GetInboundEndpoint(c),
			UpstreamEndpoint: result.UpstreamEndpoint,
			UserAgent:        c.GetHeader("User-Agent"),
			IPAddress:        ip.GetClientIP(c),
			APIKeyService:    h.apiKeyService,
			QuotaPlatform:    service.QuotaPlatform(c.Request.Context(), adm.key),
			SessionID:        service.ExtractClientSessionID(c),
			PricingAt:        adm.pricingAt,
		}); err != nil {
			logger.L().With(
				zap.String("component", "handler.qianwen_standard"),
				zap.String("action", action),
				zap.Int64("account_id", account.ID),
			).Error("qianwen_standard.record_usage_failed", zap.Error(err))
		}
	})
}

// QianwenEmbeddings handles POST /v1/embeddings for Qianwen groups by
// forwarding the OpenAI-shape request to DashScope compatible-mode
// /embeddings (official embedding compatibility doc, 2026-09-22).
func (h *OpenAIGatewayHandler) QianwenEmbeddings(c *gin.Context) {
	adm, ok := h.admitQianwenStandardRequest(c, "openai_embeddings")
	if !ok {
		return
	}
	if !service.IsQianwenCompatibleEmbeddingModel(adm.model) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by the DashScope compatible embeddings endpoint")
		return
	}
	result, account, ok := h.forwardQianwenStandard(c, adm, "embeddings", service.OpenAIEndpointCapabilityEmbeddings, func(account *service.Account) (*service.OpenAIForwardResult, error) {
		return h.gatewayService.ForwardEmbeddings(c.Request.Context(), c, account, adm.body, "")
	})
	if !ok {
		return
	}
	h.recordQianwenStandardUsage(c, adm, account, result, "embeddings")
}

// QianwenRerank handles the standard flat rerank shape for Qianwen groups,
// bridging to the native gte-rerank-v2/qwen3-vl-rerank endpoint.
func (h *OpenAIGatewayHandler) QianwenRerank(c *gin.Context) {
	adm, ok := h.admitQianwenStandardRequest(c, service.ContentModerationProtocolOpenAIChat)
	if !ok {
		return
	}
	result, account, ok := h.forwardQianwenStandard(c, adm, "rerank", service.OpenAIEndpointCapabilityChatCompletions, func(account *service.Account) (*service.OpenAIForwardResult, error) {
		return h.gatewayService.ForwardQianwenRerank(c.Request.Context(), c, account, adm.body)
	})
	if !ok {
		return
	}
	h.recordQianwenStandardUsage(c, adm, account, result, "rerank")
}

// QianwenImages handles POST /v1/images/generations for Qianwen groups,
// forwarding the flat OpenAI images shape to DashScope compatible-mode
// /images/generations (qwen-image-3.0 family, official 3.0 API reference).
func (h *OpenAIGatewayHandler) QianwenImages(c *gin.Context) {
	adm, ok := h.admitQianwenStandardRequest(c, service.ContentModerationProtocolOpenAIChat)
	if !ok {
		return
	}
	if _, err := service.ValidateQianwenCompatibleImageBody(adm.body); err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	result, account, ok := h.forwardQianwenStandard(c, adm, "images", service.OpenAIEndpointCapabilityChatCompletions, func(account *service.Account) (*service.OpenAIForwardResult, error) {
		return h.gatewayService.ForwardQianwenImages(c.Request.Context(), c, account, adm.body)
	})
	if !ok {
		return
	}
	h.recordQianwenStandardUsage(c, adm, account, result, "image.generate")
}
