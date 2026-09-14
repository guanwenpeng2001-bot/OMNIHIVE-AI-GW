//go:build unit

package service

import (
	"io"

	"github.com/gin-gonic/gin"
)

// These adapters serve legacy Gin tests compiled with the unit build tag.
func (s *AccountTestService) processGeminiStream(c *gin.Context, body io.Reader) error {
	return s.probeProcessGeminiStream(newAccountProbeContext(c), body)
}
func (s *AccountTestService) testOpenAIAccountConnection(c *gin.Context, account *Account, modelID string, prompt string, mode string) error {
	return s.probeTestOpenAIAccountConnection(newAccountProbeContext(c), account, modelID, prompt, mode)
}
func (s *AccountTestService) testGrokAccountConnection(c *gin.Context, account *Account, modelID, prompt, mode string, opts AccountTestOptions) error {
	return s.probeTestGrokAccountConnection(newAccountProbeContext(c), account, modelID, prompt, mode, opts)
}

func (service *AccountTestService) testCursorAccountConnection(requestContext *gin.Context, account *Account, modelID, mode string) error {
	return service.probeTestCursorAccountConnection(newAccountProbeContext(requestContext), account, modelID, mode)
}
