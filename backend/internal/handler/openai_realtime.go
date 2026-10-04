package handler

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	ws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// OpenAIRealtime serves the native OpenAI Realtime WebSocket protocol.
func (h *OpenAIGatewayHandler) OpenAIRealtime(c *gin.Context) {
	key, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || key == nil || key.User == nil || key.Group == nil {
		h.errorResponse(c, 401, "authentication_error", "Invalid API key")
		return
	}
	if openAICompatibleRequestPlatform(c.Request.Context(), key) != service.PlatformOpenAI {
		h.errorResponse(c, 404, "unsupported_operation_error", "Realtime requires an OpenAI group")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, 401, "authentication_error", "Invalid user context")
		return
	}
	if !isOpenAIWSUpgradeRequest(c.Request) {
		h.errorResponse(c, 426, "invalid_request_error", "WebSocket upgrade required")
		return
	}
	websocketKey, keyErr := base64.StdEncoding.DecodeString(c.GetHeader("Sec-WebSocket-Key"))
	connectionUpgrade := false
	for _, value := range c.Request.Header.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			connectionUpgrade = connectionUpgrade || strings.EqualFold(strings.TrimSpace(token), "upgrade")
		}
	}
	if !connectionUpgrade || c.Request.Method != "GET" || c.GetHeader("Sec-WebSocket-Version") != "13" || keyErr != nil || len(websocketKey) != 16 {
		h.errorResponse(c, 400, "invalid_request_error", "Invalid WebSocket handshake")
		return
	}
	// Reject browser cross-origin handshakes before dialing or acquiring leases.
	if origin := c.GetHeader("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !strings.EqualFold(u.Host, c.Request.Host) {
			h.errorResponse(c, 403, "permission_error", "Cross-origin realtime handshake rejected")
			return
		}
	}
	query, queryErr := url.ParseQuery(c.Request.URL.RawQuery)
	if queryErr != nil || len(query) != 1 || len(query["model"]) != 1 {
		h.errorResponse(c, 400, "unsupported_operation_error", "Only one model query is supported")
		return
	}
	if strings.TrimSpace(c.Query("model")) == "" {
		h.errorResponse(c, 400, "invalid_request_error", "model is required")
		return
	}
	if h.upstreamJobs == nil || !h.ensureResponsesDependencies(c, nil) {
		if !c.Writer.Written() {
			h.errorResponse(c, 503, "api_error", "Persistent realtime billing unavailable")
		}
		return
	}
	// Redis admission must not expire before our independent absolute deadline.
	if h.cfg != nil && h.cfg.Gateway.ConcurrencySlotTTLMinutes < 3 {
		h.errorResponse(c, 503, "api_error", "Realtime requires concurrency slot TTL of at least three minutes")
		return
	}
	model := strings.TrimSpace(c.Query("model"))
	defer beginGatewayRequestPlan(c, h.gatewayService, key, model, "realtime", h.maxAccountSwitches)()
	// 入口前置 hold：媒体/音频/realtime/异步 job 在调用上游之前先按估算占用周期预算，
	// 结算时按实际消耗修正；没有周期预算的 Key 不产生额外工作（T3-2.2）。
	if !reserveGatewayRequestBudget(c, key, model, nil) {
		return
	}
	ctx, release, err := h.upstreamJobs.RegisterQianwenRealtime(c.Request.Context())
	if err != nil {
		h.errorResponse(c, 503, "api_error", "Application is draining")
		return
	}
	defer release()
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	eligible := func(ctx context.Context) error {
		return h.billingCacheService.CheckBillingEligibility(ctx, key.User, key, key.Group, subscription, service.QuotaPlatform(ctx, key))
	}
	if err := eligible(ctx); err != nil {
		status, code, message, _ := billingErrorDetails(err)
		h.errorResponse(c, status, code, message)
		return
	}
	userSlot, err := h.concurrencyHelper.concurrencyService.AcquireUserSlot(ctx, subject.UserID, 1)
	if err != nil {
		h.errorResponse(c, 503, "api_error", "Concurrency store unavailable")
		return
	}
	if !userSlot.Acquired {
		h.errorResponse(c, 429, "rate_limit_error", "Realtime user concurrency limit reached")
		return
	}
	defer userSlot.ReleaseFunc()
	mapping, restricted := h.gatewayService.ResolveChannelMappingAndRestrict(ctx, key.GroupID, model)
	if restricted {
		h.errorResponse(c, 403, "permission_error", "Realtime model restricted by channel")
		return
	}
	// Realtime stays on the ingress-selected target for the session. An empty
	// explicit target keeps channel fallback; account aliases are mapped later.
	forwardModel := service.CompositeTargetForwardModel(ctx, nil, mapping, model)
	if targets, ok := service.CompositeRouteTargetsFromContext(ctx); ok {
		forwardModel = service.CompositeTargetForwardModel(ctx, &targets[0], mapping, model)
	} else if resolved, ok := service.ResolvedUpstreamModelFromContext(ctx); ok {
		forwardModel = resolved
	}
	ctx = service.WithOpenAIForwardModel(ctx, forwardModel, false)
	// Native Realtime is not the OAuth-only Codex Live capability. The request
	// plan and final dialer validate the realtime model binding directly.
	ctx = service.WithOpenAIProfitControlSuppressed(ctx)
	selection, _, err := h.gatewayService.SelectAccountWithSchedulerForCapability(ctx, key.GroupID, "", "", forwardModel, nil, service.OpenAIUpstreamTransportAny, "", false, false, false, service.PlatformOpenAI)
	if err != nil || selection == nil || selection.Account == nil {
		requestLogger(c, "handler.openai_realtime").Warn("realtime account selection failed", zap.Error(err))
		h.errorResponse(c, 503, "api_error", "No realtime account available")
		return
	}
	account := selection.Account
	if selection.Acquired && selection.ReleaseFunc != nil {
		defer selection.ReleaseFunc()
	} else {
		limit := account.Concurrency
		if limit <= 0 || limit > 2 {
			limit = 2
		}
		slot, slotErr := h.concurrencyHelper.concurrencyService.AcquireAccountSlot(ctx, account.ID, limit)
		if slotErr != nil {
			h.errorResponse(c, 503, "api_error", "Concurrency store unavailable")
			return
		}
		if !slot.Acquired {
			h.errorResponse(c, 429, "rate_limit_error", "Realtime account concurrency limit reached")
			return
		}
		defer slot.ReleaseFunc()
	}
	snapshot, err := service.OpenAIRealtimeBillingSnapshot(ctx, key.Group, account, key, forwardModel, subscription)
	if err != nil {
		h.errorResponse(c, 422, "unsupported_operation_error", err.Error())
		return
	}
	snapshot.RequestedModel = model
	if requested, ok := service.RequestedPublicModelFromContext(ctx); ok {
		snapshot.RequestedModel = requested
	}
	if key.GroupID != nil {
		pricingAt := service.GatewayRequestPlanFromContext(ctx).Snapshot().PricingAt
		snapshot.RateMultiplier = h.gatewayService.ResolveUserGroupRateMultiplier(ctx, key.UserID, *key.GroupID, key.Group.RateMultiplier) * key.Group.PeakMultiplierAt(pricingAt)
	}
	session, err := h.upstreamJobs.StartOpenAIRealtime(ctx, &service.UpstreamJob{Platform: service.PlatformOpenAI, AccountID: account.ID, UserID: key.UserID, APIKeyID: key.ID, GroupID: key.GroupID, Billing: snapshot})
	if err != nil {
		status := 503
		if errors.Is(err, service.ErrQianwenRealtimeCapacity) {
			status = 429
		}
		h.errorResponse(c, status, "api_error", "Realtime session admission unavailable or full")
		return
	}
	finishCode := "session_closed"
	defer func() {
		persist, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.upstreamJobs.FinishOpenAIRealtime(persist, session.PublicRequestID, finishCode); err != nil {
			requestLogger(c, "handler.openai_realtime").Error("persist realtime session closure", zap.String("failure_category", "persistence_unconfirmed"), zap.String("session_id", session.PublicRequestID))
		}
	}()
	// Keep the dial context alive for the session: OpenOpenAIRealtime closes
	// its socket on cancellation. A handshake timer is stopped after dial.
	ctx, cancelSession := context.WithTimeout(ctx, 120*time.Second)
	defer cancelSession()
	setOpsSelectedAccount(c, account.ID, account.Platform)
	handshakeTimer := time.AfterFunc(12*time.Second, cancelSession)
	upstream, err := h.gatewayService.OpenOpenAIRealtime(ctx, account, snapshot.Model)
	handshakeTimer.Stop()
	if err != nil {
		finishCode = "handshake_failed"
		h.openAIRealtimeDialError(c, err)
		return
	}
	defer func() {
		if err := upstream.CloseNow(); err != nil {
			slog.Debug("OpenAI realtime upstream cleanup failed")
		}
	}()
	c.Header("X-Request-ID", session.PublicRequestID)
	conn, err := ws.Accept(c.Writer, c.Request, &ws.AcceptOptions{CompressionMode: ws.CompressionDisabled})
	if err != nil {
		return
	}
	defer func() {
		if err := conn.CloseNow(); err != nil {
			slog.Debug("OpenAI realtime connection cleanup failed")
		}
	}()
	finishCode = "usage_pending"
	err = service.RelayOpenAIRealtime(ctx, conn, upstream, service.OpenAIRealtimeCallbacks{
		BeforeEvent: func(eventContext context.Context, raw []byte) error {
			if err := eligible(eventContext); err != nil {
				return err
			}
			switch gjson.GetBytes(raw, "type").String() {
			case "response.create", "input_audio_buffer.append":
				finishCode = "usage_pending"
			}
			return service.CheckAttemptBudget(eventContext)
		},
		Receipt: func(receiptContext context.Context, id string, usage []byte, status, declaredModel string) error {
			_, err := h.upstreamJobs.RecordOpenAIRealtimeTurn(receiptContext, session, id, usage, status, declaredModel)
			if err == nil {
				finishCode = "session_closed"
			}
			// Missing prices are a retained receipt, not a relay failure.
			return err
		},
	})
	_ = err // Closure is recorded above; the relay always closes both sockets.
}

func (h *OpenAIGatewayHandler) openAIRealtimeDialError(c *gin.Context, err error) {
	if writeGatewayTPMError(c, err) {
		return
	}
	if errors.Is(err, service.ErrAccountModelUnsupported) {
		h.errorResponse(c, 422, "invalid_request_error", "Selected account does not authorize this realtime model")
		return
	}
	var upstream *service.UpstreamFailoverError
	if errors.As(err, &upstream) && upstream.StatusCode >= 400 && upstream.StatusCode <= 599 {
		service.SetOpsUpstreamError(c, upstream.StatusCode, "OpenAI realtime handshake rejected", "")
		for _, name := range []string{"Retry-After", "X-Request-ID"} {
			if value := upstream.ResponseHeaders.Get(name); value != "" {
				c.Header(name, value)
			}
		}
		if len(upstream.ResponseBody) > 0 {
			c.Data(upstream.StatusCode, "application/json", upstream.ResponseBody)
		} else {
			h.errorResponse(c, upstream.StatusCode, "upstream_error", "OpenAI realtime upstream handshake rejected")
		}
		return
	}
	h.errorResponse(c, 502, "upstream_error", "OpenAI realtime upstream handshake failed")
}
