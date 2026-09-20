package handler

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
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
	var payload map[string]any
	if json.Unmarshal([]byte(data), &payload) != nil {
		return false
	}
	if len(payload) == 1 && payload["type"] == "ping" {
		return true
	}
	if payload["type"] != "content_block_delta" || len(payload) != 3 {
		return false
	}
	delta, ok := payload["delta"].(map[string]any)
	if !ok || len(delta) != 2 {
		return false
	}
	fields := map[string]string{"text_delta": "text", "thinking_delta": "thinking", "input_json_delta": "partial_json"}
	kind, _ := delta["type"].(string)
	field, known := fields[kind]
	return known && delta[field] == ""
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
