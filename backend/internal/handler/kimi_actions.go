package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	pkghttputil "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/httputil"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ip"
	middleware2 "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"go.uber.org/zap"
)

// KimiSearch serves the documented Basic and Pro native search contracts.
func (h *OpenAIGatewayHandler) KimiSearch(c *gin.Context) {
	streamStarted := false
	defer h.recoverResponsesPanic(c, &streamStarted)
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey.Group == nil {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	requestLog := requestLogger(c, "handler.kimi_search")
	if !h.ensureResponsesDependencies(c, requestLog) {
		return
	}
	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if limitErr, exceeded := extractMaxBytesError(err); exceeded {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(limitErr.Limit))
		} else {
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		}
		return
	}
	var request service.KimiWebSearchRequest
	if json.Unmarshal(body, &request) != nil || strings.TrimSpace(request.TextQuery) == "" || request.Limit < 0 || request.Limit > 20 || request.TimeoutSeconds < 0 || request.TimeoutSeconds > 60 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "text_query is required; limit must be 1-20 and timeout_seconds 1-60 when set")
		return
	}
	request.Pro = strings.HasSuffix(c.Request.URL.Path, "/search_pro")
	request.TrackID = c.GetHeader("X-Msh-Track-Id")
	model := "kimi-search"
	if request.Pro {
		model = "kimi-search-pro"
	}
	defer beginGatewayRequestPlan(c, h.gatewayService, apiKey, model, "search", h.maxAccountSwitches)()
	if c.IsAborted() {
		return
	}
	setOpsRequestContext(c, model, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeSync))
	auditBody, _ := json.Marshal(map[string]any{
		"messages": []map[string]any{{"role": "user", "content": request.TextQuery}},
	})
	if decision := h.checkSecurityAudit(c, requestLog, apiKey, subject, service.ContentModerationProtocolOpenAIChat, model, auditBody); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return
	}
	if !reserveGatewayRequestBudget(c, apiKey, model, body) {
		return
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		status, code, message, _ := billingErrorDetails(err)
		writeAdmissionRejectionHeaders(c, err)
		h.errorResponse(c, status, code, message)
		return
	}
	userRelease, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, false, &streamStarted, requestLog)
	if !acquired {
		return
	}
	if userRelease != nil {
		defer userRelease()
	}
	requestContext := service.WithOpenAIProfitControlSuppressed(c.Request.Context())
	c.Request = c.Request.WithContext(requestContext)
	sessionHash := h.gatewayService.GenerateExplicitSessionHash(c, body)
	selection, _, err := h.gatewayService.SelectAccountWithSchedulerForCapability(requestContext, apiKey.GroupID, "", sessionHash, "", nil, service.OpenAIUpstreamTransportHTTPSSE, "", false, false, false, service.PlatformKimi)
	if err != nil || selection == nil || selection.Account == nil {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No search account available")
		return
	}
	account := selection.Account
	accountRelease, slot := h.acquireResponsesAccountSlot(c, apiKey.GroupID, sessionHash, selection, false, &streamStarted, requestLog)
	if slot != openAISlotAcquireOK {
		return
	}
	if accountRelease != nil {
		defer accountRelease()
	}
	setOpsSelectedAccount(c, account.ID, account.Platform)
	search, usage, err := h.gatewayService.SearchKimi(requestContext, account, request)
	if err != nil {
		h.gatewayService.ReportOpenAIAccountScheduleResult(account, "", false, nil, err)
		var applicationError *infraerrors.ApplicationError
		if errors.As(err, &applicationError) {
			// Error bodies are local; upstream messages may contain credentials.
			h.errorResponse(c, int(applicationError.Code), applicationError.Reason, "Kimi search request failed")
		} else if !errors.Is(err, context.Canceled) {
			h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Kimi search request failed")
		}
		return
	}
	h.gatewayService.ReportOpenAIAccountScheduleResult(account, "", true, nil)
	if usage.WebSearchCalls > 0 {
		userAgent, clientIP := c.GetHeader("User-Agent"), ip.GetClientIP(c)
		inboundEndpoint, upstreamEndpoint := GetInboundEndpoint(c), GetUpstreamEndpoint(c, account.Platform)
		quotaPlatform := service.QuotaPlatform(requestContext, apiKey)
		h.submitOpenAIUsageRecordTask(requestContext, usage, func(ctx context.Context) {
			if err := h.gatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
				Result: usage, APIKey: apiKey, User: apiKey.User, Account: account,
				Subscription: subscription, InboundEndpoint: inboundEndpoint, UpstreamEndpoint: upstreamEndpoint,
				UserAgent: userAgent, IPAddress: clientIP, RequestPayloadHash: service.HashUsageRequestPayload(body),
				APIKeyService: h.apiKeyService, QuotaPlatform: quotaPlatform,
				PricingAt: service.OpenAIPricingAtFromContext(requestContext),
			}); err != nil {
				requestLog.Error("kimi_search.record_usage_failed", zap.Error(err))
			}
		})
	}
	c.Header("X-Msh-Track-Id", search.TrackID)
	c.Header("X-Msh-Chat-Id", search.ChatID)
	c.Data(http.StatusOK, "application/json", search.RawBody)
}
