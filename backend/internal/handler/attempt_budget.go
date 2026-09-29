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
	budget    *service.AttemptBudget
	inComment bool
}

func (w *attemptCommitWriter) Write(p []byte) (int, error) {
	w.commitContent(string(p))
	return w.ResponseWriter.Write(p)
}
func (w *attemptCommitWriter) WriteString(s string) (int, error) {
	w.commitContent(s)
	return w.ResponseWriter.WriteString(s)
}

func (w *attemptCommitWriter) commitContent(s string) {
	if !strings.HasPrefix(strings.ToLower(w.Header().Get("Content-Type")), "text/event-stream") {
		// JSON keepalives consist solely of whitespace before the actual body.
		if strings.TrimSpace(s) != "" {
			w.budget.Commit()
		}
		return
	}
	for len(s) > 0 {
		line, rest := s, ""
		end := strings.IndexAny(s, "\r\n")
		if end >= 0 {
			line, rest = s[:end], s[end+1:]
		}
		if !w.inComment && strings.HasPrefix(line, ":") {
			w.inComment = true
		}
		if !w.inComment {
			line = strings.TrimSpace(line)
			if line != "" && line != "event: ping" && !isAttemptBudgetPingData(line) {
				w.budget.Commit()
			}
		}
		if end >= 0 {
			w.inComment = false
		}
		s = rest
	}
}

func isAttemptBudgetPingData(line string) bool {
	data, ok := strings.CutPrefix(line, "data:")
	if !ok {
		return false
	}
	var payload map[string]any
	return json.Unmarshal([]byte(data), &payload) == nil && len(payload) == 1 && payload["type"] == "ping"
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
	w := &attemptCommitWriter{ResponseWriter: original, budget: service.AttemptBudgetFromContext(ctx)}
	c.Writer = w
	return func() {
		cancel()
		if c.Writer == w {
			c.Writer = original
		}
	}
}
