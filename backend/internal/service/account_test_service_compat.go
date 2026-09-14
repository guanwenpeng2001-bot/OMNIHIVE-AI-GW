package service

import (
	"encoding/json"
	"fmt"

	"github.com/gin-gonic/gin"
)

// Legacy Gin callers retain their wire format while probes operate on events.
func newAccountProbeContext(c *gin.Context) *accountProbeContext {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	return &accountProbeContext{ctx: c.Request.Context(), emit: func(event TestEvent) {
		body, err := json.Marshal(event)
		if err != nil {
			_ = c.Error(err)
			return
		}
		if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", body); err != nil {
			_ = c.Error(err)
			return
		}
		c.Writer.Flush()
	}}
}

func (s *AccountTestService) TestAccountConnection(c *gin.Context, accountID int64, modelID, prompt, mode string, opts ...AccountTestOptions) error {
	adapter := newAccountProbeContext(c)
	result := s.ProbeAccount(c.Request.Context(), AccountProbeInput{AccountID: accountID, ModelID: modelID, Prompt: prompt, Mode: mode, Options: firstAccountTestOptions(opts)}, adapter.emit)
	if !result.Success {
		return fmt.Errorf("%s", result.Error)
	}
	return nil
}
