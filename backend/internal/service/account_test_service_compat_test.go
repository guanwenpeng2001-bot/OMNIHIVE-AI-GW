package service

import (
	"context"
	"io"

	"github.com/gin-gonic/gin"
)

func (s *AccountTestService) processGeminiStream(c *gin.Context, body io.Reader) error {
	return s.probeProcessGeminiStream(newAccountProbeContext(c), body)
}
func (s *AccountTestService) testOpenAIAccountConnection(c *gin.Context, account *Account, modelID string, prompt string, mode string) error {
	return s.probeTestOpenAIAccountConnection(newAccountProbeContext(c), account, modelID, prompt, mode)
}
func (s *AccountTestService) testGrokAccountConnection(c *gin.Context, account *Account, modelID, prompt, mode string, opts AccountTestOptions) error {
	return s.probeTestGrokAccountConnection(newAccountProbeContext(c), account, modelID, prompt, mode, opts)
}
func (s *AccountTestService) testOpenAIImageAPIKey(c *gin.Context, ctx context.Context, account *Account, modelID, prompt string) error {
	return s.probeTestOpenAIImageAPIKey(newAccountProbeContext(c), ctx, account, modelID, prompt)
}
func (s *AccountTestService) testOpenAIImageOAuth(c *gin.Context, ctx context.Context, account *Account, modelID, prompt string) error {
	return s.probeTestOpenAIImageOAuth(newAccountProbeContext(c), ctx, account, modelID, prompt)
}
func (service *AccountTestService) testCursorAccountConnection(requestContext *gin.Context, account *Account, modelID, mode string) error {
	return service.probeTestCursorAccountConnection(newAccountProbeContext(requestContext), account, modelID, mode)
}
