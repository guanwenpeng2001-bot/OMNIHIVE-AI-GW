package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	ws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

func (h *OpenAIGatewayHandler) qianwenRealtime(c *gin.Context) {
	key, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || key == nil || key.User == nil || key.Group == nil {
		h.errorResponse(c, 401, "authentication_error", "Invalid API key")
		return
	}
	if openAICompatibleRequestPlatform(c.Request.Context(), key) != service.PlatformQianwen {
		h.errorResponse(c, 404, "unsupported_operation_error", "Realtime requires a Qianwen group")
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
	// Reject browser cross-origin handshakes before dialing or acquiring leases.
	if origin := c.GetHeader("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !strings.EqualFold(u.Host, c.Request.Host) {
			h.errorResponse(c, 403, "permission_error", "Cross-origin realtime handshake rejected")
			return
		}
	}
	if len(c.Request.URL.Query()) != 1 || len(c.Request.URL.Query()["model"]) != 1 {
		h.errorResponse(c, 400, "unsupported_operation_error", "Only one model query is supported")
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
	ctx := c.Request.Context()
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
	model := c.Query("model")
	mapping, restricted := h.gatewayService.ResolveChannelMappingAndRestrict(ctx, key.GroupID, model)
	if restricted {
		h.errorResponse(c, 403, "permission_error", "Realtime model restricted by channel")
		return
	}
	forwardModel := model
	if mapping.Mapped {
		forwardModel = mapping.MappedModel
	}
	if !service.IsQianwenRealtimeModel(forwardModel) {
		h.errorResponse(c, 422, "unsupported_operation_error", "Unsupported mapped realtime model")
		return
	}
	selection, _, err := h.gatewayService.SelectAccountWithSchedulerForCapability(ctx, key.GroupID, "", "", forwardModel, nil, service.OpenAIUpstreamTransportAny, service.OpenAIEndpointCapabilityChatCompletions, false, false, true, service.PlatformQianwen)
	if err != nil || selection == nil || selection.Account == nil {
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
	snapshot, err := service.QianwenRealtimeBillingSnapshot(key.Group, account, key, forwardModel, subscription)
	if err != nil {
		h.errorResponse(c, 422, "unsupported_operation_error", err.Error())
		return
	}
	snapshot.RequestedModel = model
	if key.GroupID != nil {
		snapshot.RateMultiplier = h.gatewayService.ResolveUserGroupRateMultiplier(ctx, key.UserID, *key.GroupID, key.Group.RateMultiplier) * key.Group.PeakMultiplierAt(time.Now())
	}
	session, err := h.upstreamJobs.StartQianwenRealtime(ctx, &service.UpstreamJob{Platform: service.PlatformQianwen, AccountID: account.ID, UserID: key.UserID, APIKeyID: key.ID, GroupID: key.GroupID, Billing: snapshot})
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
		_ = h.upstreamJobs.FinishQianwenRealtime(persist, session.PublicRequestID, finishCode)
	}()
	dialCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	upstream, err := h.gatewayService.OpenQianwenRealtime(dialCtx, account, snapshot.Model)
	cancel()
	if err != nil {
		finishCode = "handshake_failed"
		h.errorResponse(c, 502, "upstream_error", "DashScope realtime upstream handshake failed")
		return
	}
	defer upstream.Close()
	setOpsSelectedAccount(c, account.ID, account.Platform)
	c.Header("X-Request-ID", session.PublicRequestID)
	conn, err := ws.Accept(c.Writer, c.Request, &ws.AcceptOptions{CompressionMode: ws.CompressionDisabled})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	reqLog := requestLogger(c, "handler.qianwen_realtime")
	receipts := map[string]bool{}
	err = service.RelayQianwenRealtime(ctx, conn, upstream, service.QianwenRealtimeCallbacks{
		BeforeCommit: func(turnCtx context.Context, text string) error {
			if err := eligible(turnCtx); err != nil {
				return errors.New("Realtime billing eligibility rejected")
			}
			audit, _ := json.Marshal(gin.H{"model": model, "messages": []any{gin.H{"role": "user", "content": text}}})
			if decision := h.checkSecurityAudit(c, reqLog, key, subject, service.ContentModerationProtocolOpenAIChat, model, audit); decision != nil && !decision.AllowNextStage {
				return errors.New("Realtime content rejected by security audit")
			}
			if err := h.upstreamJobs.MarkQianwenRealtimePending(turnCtx, session.PublicRequestID); err != nil {
				return err
			}
			finishCode = "usage_pending"
			return nil
		},
		Receipt: func(turnCtx context.Context, id string, usage []byte, status string) error {
			persist, cancel := context.WithTimeout(context.WithoutCancel(turnCtx), 5*time.Second)
			defer cancel()
			job, err := h.upstreamJobs.RecordQianwenRealtimeTurn(persist, session, id, usage, status)
			if err != nil {
				return err
			}
			if job.ChargedUsageLogID == nil {
				finishCode = "unsupported_usage"
				return errors.New("Unexplained realtime usage retained without settlement")
			}
			if !receipts[id] {
				if err = h.upstreamJobs.AcknowledgeQianwenRealtimeReceipt(persist, session.PublicRequestID); err != nil {
					return err
				}
				receipts[id] = true
				finishCode = "session_closed"
			}
			return nil
		},
	})
	if err != nil {
		_ = conn.Close(ws.StatusPolicyViolation, "Realtime session ended; inspect session and turn receipts")
	}
}
