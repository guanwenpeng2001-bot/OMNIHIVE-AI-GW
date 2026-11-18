package handler

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/lifecycle"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/gatewayerror"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type attemptCommitWriter struct {
	gin.ResponseWriter
	response       *lifecycle.Response
	context        *gin.Context
	line           string
	event          string
	skipLine       bool
	skipLF         bool
	nativeTerminal bool
}

func (w *attemptCommitWriter) Write(p []byte) (int, error) {
	terminal := w.observe(string(p))
	n, err := w.ResponseWriter.Write(p)
	w.finishWrite(terminal, n == len(p) && err == nil)
	return n, err
}

func (w *attemptCommitWriter) WriteString(s string) (int, error) {
	terminal := w.observe(s)
	n, err := w.ResponseWriter.WriteString(s)
	w.finishWrite(terminal, n == len(s) && err == nil)
	return n, err
}

func (w *attemptCommitWriter) finishWrite(terminalEnd, success bool) {
	if !success && terminalSSEEvent(w.event) {
		w.response.BeginTerminal()
	}
	if w.response.State() == lifecycle.EmittingTerminal && (!success || terminalEnd) {
		w.response.FinishTerminal(success)
		w.nativeTerminal = false
	}
}

func (w *attemptCommitWriter) Flush() {
	w.ResponseWriter.Flush()
	w.response.Heartbeat()
}

func (w *attemptCommitWriter) WriteHeaderNow() {
	w.ResponseWriter.WriteHeaderNow()
	w.response.Heartbeat()
}

func terminalSSEEvent(event string) bool {
	switch event {
	case "response.completed", "response.done", "response.failed", "response.incomplete", "response.cancelled", "response.canceled", "message_stop", "error":
		return true
	}
	return false
}

func (w *attemptCommitWriter) classifyLine(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		w.event = ""
		return w.nativeTerminal
	}
	if name, ok := strings.CutPrefix(line, "event:"); ok {
		w.event = strings.TrimSpace(name)
		return false
	}
	if strings.HasPrefix(line, ":") || strings.HasPrefix(line, "id:") || strings.HasPrefix(line, "retry:") {
		return false
	}
	if data, ok := strings.CutPrefix(line, "data:"); ok {
		data = strings.TrimSpace(data)
		if terminalSSEEvent(w.event) || data == "[DONE]" || terminalSSEEvent(gjson.Get(data, "type").String()) {
			if w.response.BeginTerminal() {
				w.nativeTerminal = true
			}
			return false
		}
		if w.response.State() >= lifecycle.SemanticOutput || service.IsStreamHeartbeatData(data) {
			return false
		}
	}
	w.response.Semantic()
	return false
}

func (w *attemptCommitWriter) observe(content string) bool {
	if content == "" || (w.response.State() >= lifecycle.EmittingTerminal && !w.nativeTerminal) {
		return false
	}
	w.response.Heartbeat()
	if !strings.HasPrefix(strings.ToLower(w.Header().Get("Content-Type")), "text/event-stream") {
		if strings.TrimSpace(content) == "" {
			return false
		}
		w.context.Set("gateway_stream_only_heartbeats", false)
		return w.response.BeginTerminal()
	}

	wasSemantic := w.response.ReplayBlocked()
	terminal := false
	for len(content) > 0 {
		if w.skipLF {
			content = strings.TrimPrefix(content, "\n")
			w.skipLF = false
			if content == "" {
				break
			}
		}
		i := strings.IndexAny(content, "\r\n")
		fragment := content
		if i >= 0 {
			w.skipLF = content[i] == '\r'
			fragment, content = content[:i], content[i+1:]
		} else {
			content = ""
		}
		if !w.skipLine {
			if w.line == "" && strings.HasPrefix(fragment, ":") {
				w.skipLine = true
			} else if i >= 0 || len(w.line)+len(fragment) > 4096 {
				// Parse complete lines in place. Bound retained bytes for fragmented payloads.
				terminal = w.classifyLine(w.line+fragment) || terminal
				w.line = ""
				w.skipLine = i < 0
			} else {
				w.line += fragment
			}
		}
		if i >= 0 {
			w.skipLine = false
		}
	}
	w.response.SetPartial(strings.TrimSpace(w.line) != "")
	if wasSemantic != w.response.ReplayBlocked() {
		w.context.Set("gateway_stream_only_heartbeats", !w.response.ReplayBlocked())
	}
	return terminal
}

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
	w := &attemptCommitWriter{ResponseWriter: original, response: service.AttemptBudgetFromContext(ctx).Response(), context: c}
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
		if code == "" {
			code = errType
		}
		gatewayerror.WriteStream(c, status, code, message)
	})
}
