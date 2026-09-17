package handler

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// The wrapper sees header flushes and keepalive writes as well as semantic data.
// Merely setting a status in Gin does not submit headers.
type attemptCommitWriter struct {
	gin.ResponseWriter
	budget *service.AttemptBudget
}

func (w *attemptCommitWriter) Write(p []byte) (int, error) {
	w.budget.Commit()
	return w.ResponseWriter.Write(p)
}
func (w *attemptCommitWriter) WriteString(s string) (int, error) {
	w.budget.Commit()
	return w.ResponseWriter.WriteString(s)
}
func (w *attemptCommitWriter) WriteHeaderNow() {
	w.budget.Commit()
	w.ResponseWriter.WriteHeaderNow()
}
func (w *attemptCommitWriter) Flush() {
	w.budget.Commit()
	w.ResponseWriter.Flush()
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
