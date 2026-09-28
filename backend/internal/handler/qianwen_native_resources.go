package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// handleQianwenNativeSyncResource serves the synchronous DashScope resource
// families (text rerank, text embedding, voice enrollment). Model admission is
// the adapter's explicit allowlist plus tariff/price resolution — the same
// discipline as the asynchronous video family — because these model families
// are not part of the catalog endpoint evidence yet. Usage rides the shared
// RecordUsage pipeline; failed upstream calls never reach billing.
func (h *OpenAIGatewayHandler) handleQianwenNativeSyncResource(c *gin.Context, operation service.QianwenNativeOperation) {
	key, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || key == nil || key.Group == nil {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	if openAICompatibleRequestPlatform(c.Request.Context(), key) != service.PlatformQianwen {
		h.errorResponse(c, http.StatusNotFound, "unsupported_operation_error", "This DashScope operation requires a Qianwen group")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid user context")
		return
	}
	if !h.ensureResponsesDependencies(c, nil) {
		return
	}
	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	if err != nil || !gjson.ValidBytes(body) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Invalid DashScope JSON")
		return
	}
	model, err := service.ParseQianwenNativeBody(body)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	if strings.EqualFold(strings.TrimSpace(c.GetHeader("X-DashScope-SSE")), "enable") || strings.EqualFold(strings.TrimSpace(c.GetHeader("X-DashScope-Async")), "enable") {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Synchronous DashScope resource operations do not accept SSE or async headers")
		return
	}
	// Model-family validation against the raw request model runs before any
	// account work; the mapped model is revalidated inside the forward.
	if err := service.ValidateQianwenNativeOperation(operation, model, body, false); err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	reqLog := requestLogger(c, "handler.qianwen_resource", zap.String("operation", string(operation)), zap.String("model", model))
	if decision := h.checkSecurityAudit(c, reqLog, key, subject, service.ContentModerationProtocolOpenAIChat, model, qianwenNativeAuditBody(body)); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return
	}
	if h.rejectIfCyberSessionBlocked(c, key, body, model, cyberBlockFormatChat) {
		return
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), key.User, key, key.Group, subscription, service.QuotaPlatform(c.Request.Context(), key)); err != nil {
		status, code, message, _ := billingErrorDetails(err)
		writeAdmissionRejectionHeaders(c, err)
		h.errorResponse(c, status, code, message)
		return
	}
	streamStarted := false
	release, ok := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, false, &streamStarted, reqLog)
	if !ok {
		return
	}
	if release != nil {
		defer release()
	}
	pricingCtx, pricingAt := h.gatewayService.WithOpenAIRequestPricingContext(c.Request.Context(), key.GroupID)
	c.Request = c.Request.WithContext(pricingCtx)
	sessionHash := h.gatewayService.GenerateSessionHash(c, body)
	selection, _, err := h.gatewayService.SelectAccountWithSchedulerForCapability(c.Request.Context(), key.GroupID, "", sessionHash, model, nil, service.OpenAIUpstreamTransportAny, service.OpenAIEndpointCapabilityChatCompletions, false, false, true, service.PlatformQianwen)
	if err != nil || selection == nil || selection.Account == nil {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No Qianwen account available for this model")
		return
	}
	account := selection.Account
	accountRelease, slot := h.acquireResponsesAccountSlot(c, key.GroupID, sessionHash, selection, false, &streamStarted, reqLog)
	if slot != openAISlotAcquireOK {
		return
	}
	if accountRelease != nil {
		defer accountRelease()
	}
	// Voice listing and deletion are resource management, not generation.
	if operation != service.QianwenNativeVoiceEnrollment || gjson.GetBytes(body, "input.action").String() == "create" {
		defer beginGatewayRequestPlan(c, h.gatewayService, key, model, "resources", h.maxAccountSwitches)()
	}
	setOpsSelectedAccount(c, account.ID, account.Platform)
	result, err := h.gatewayService.ForwardQianwenNative(c.Request.Context(), c, account, operation, body, false)
	if err != nil {
		return
	}
	if result == nil {
		return
	}
	h.submitOpenAIUsageRecordTask(c.Request.Context(), result, func(ctx context.Context) {
		if err := h.gatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
			Result:           result,
			APIKey:           key,
			User:             key.User,
			Account:          account,
			Subscription:     subscription,
			InboundEndpoint:  GetInboundEndpoint(c),
			UpstreamEndpoint: result.UpstreamEndpoint,
			UserAgent:        c.GetHeader("User-Agent"),
			IPAddress:        ip.GetClientIP(c),
			APIKeyService:    h.apiKeyService,
			QuotaPlatform:    service.QuotaPlatform(c.Request.Context(), key),
			SessionID:        service.ExtractClientSessionID(c),
			PricingAt:        pricingAt,
		}); err != nil {
			logger.L().With(
				zap.String("component", "handler.qianwen_resource"),
				zap.String("operation", string(operation)),
				zap.Int64("account_id", account.ID),
			).Error("qianwen_resource.record_usage_failed", zap.Error(err))
		}
	})
}
