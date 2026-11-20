package handler

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/lifecycle"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/protocol"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

func attemptResponse(c *gin.Context) *lifecycle.Response {
	if c == nil || c.Request == nil {
		return nil
	}
	return service.AttemptBudgetFromContext(c.Request.Context()).Response()
}

func emitGatewayTerminal(c *gin.Context, encode func()) {
	r := attemptResponse(c)
	if r == nil {
		encode()
		return
	}
	if !r.BeginTerminal() {
		return
	}
	c.Set("gateway_stream_only_heartbeats", false)
	errorsBefore := len(c.Errors)
	sent := false
	defer func() { r.FinishTerminal(sent) }()
	encode()
	sent = len(c.Errors) == errorsBefore
}

func streamTransportCommitted(c *gin.Context) bool {
	return c != nil && c.Writer != nil && c.Writer.Written() && strings.HasPrefix(strings.ToLower(c.Writer.Header().Get("Content-Type")), "text/event-stream")
}

func installAttemptBudget(c *gin.Context) func() {
	if c.GetBool("gateway_attempt_writer") {
		return func() {}
	}
	var ctx context.Context
	var cancel context.CancelFunc
	if strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
		ctx = service.WithStreamAttemptBudget(c.Request.Context())
		cancel = func() {}
	} else {
		ctx, cancel = service.WithAttemptBudget(c.Request.Context(), service.DefaultGenerationAttempts, service.DefaultGenerationDeadline)
	}
	c.Request = c.Request.WithContext(ctx)
	original := c.Writer
	c.Set("gateway_stream_only_heartbeats", true)
	w := protocol.TrackResponseWriter(c, service.AttemptBudgetFromContext(ctx).Response(), service.IsStreamHeartbeatData)
	c.Writer = w
	c.Set("gateway_attempt_writer", true)
	return func() {
		cancel()
		c.Set("gateway_attempt_writer", false)
		if c.Writer == w {
			c.Writer = original
		}
	}
}

func alreadyTerminal(c *gin.Context) bool {
	r := attemptResponse(c)
	return r != nil && r.State() >= lifecycle.EmittingTerminal
}

func writeGatewayStreamTerminal(c *gin.Context, status int, errType, code, message string) {
	emitGatewayTerminal(c, func() {
		if inboundIsResponses(c) && writeResponsesFailedSSE(c, errType, code, message) {
			return
		}
		protocol.WriteStreamError(c, status, errType, code, message)
	})
}
