package service

import (
	"context"

	"github.com/gin-gonic/gin"
)

func (s *AccountTestService) testOpenAIImageAPIKey(c *gin.Context, ctx context.Context, account *Account, modelID, prompt string) error {
	return s.probeTestOpenAIImageAPIKey(newAccountProbeContext(c), ctx, account, modelID, prompt)
}
func (s *AccountTestService) testOpenAIImageOAuth(c *gin.Context, ctx context.Context, account *Account, modelID, prompt string) error {
	return s.probeTestOpenAIImageOAuth(newAccountProbeContext(c), ctx, account, modelID, prompt)
}
