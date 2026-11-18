package handler

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

// Only response content commits the generation budget. Headers, flushes and
// transport keepalives can precede even the first upstream generation attempt.
type attemptCommitWriter struct {
	gin.ResponseWriter
	budget      *service.AttemptBudget
	inComment   bool
	pendingLine string
	context     *gin.Context
	committed   bool
}

func (w *attemptCommitWriter) Write(p []byte) (int, error) {
	w.commitContent(string(p))
	return w.ResponseWriter.Write(p)
}
func (w *attemptCommitWriter) WriteString(s string) (int, error) {
	w.commitContent(s)
	return w.ResponseWriter.WriteString(s)
}

func (w *attemptCommitWriter) commitContent(content string) {
	commit := func() {
		w.committed = true
		w.budget.Commit()
		if w.context != nil {
			w.context.Set("gateway_stream_only_heartbeats", false)
		}
	}
	if w.committed {
		return
	}
	if !strings.HasPrefix(strings.ToLower(w.Header().Get("Content-Type")), "text/event-stream") {
		if strings.TrimSpace(content) != "" {
			commit()
		}
		return
	}
	for _, character := range content {
		if character == '\r' || character == '\n' {
			line := strings.TrimSpace(w.pendingLine)
			w.pendingLine = ""
			if w.inComment {
				w.inComment = false
				continue
			}
			if line == "" || strings.HasPrefix(line, "event:") || strings.HasPrefix(line, "id:") || strings.HasPrefix(line, "retry:") {
				continue
			}
			if !isAttemptBudgetPingData(line) {
				commit()
				return
			}
			continue
		}
		if w.inComment {
			continue
		}
		if strings.TrimSpace(w.pendingLine) == "" && character == ':' {
			w.inComment = true
			continue
		}
		w.pendingLine += string(character)
		// A keepalive is small; do not buffer unbounded generated content.
		if len(w.pendingLine) > 4096 {
			commit()
			return
		}
	}
}

func isAttemptBudgetPingData(line string) bool {
	data, ok := strings.CutPrefix(line, "data:")
	if !ok {
		return false
	}
	return service.IsStreamHeartbeatData(data)
}

func installAttemptBudget(c *gin.Context) func() {
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
	w := &attemptCommitWriter{ResponseWriter: original, budget: service.AttemptBudgetFromContext(ctx), context: c}
	c.Writer = w
	return func() {
		cancel()
		if c.Writer == w {
			c.Writer = original
		}
	}
}
