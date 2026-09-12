//go:build unit

package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestCursorCacheAndReasoningUsageNotDoubleCounted(t *testing.T) {
	usage := cursorChatUsage(cursor.TokenUsage{InputTokens: 12, OutputTokens: 5, CacheReadTokens: 3, CacheWriteTokens: 4, ReasoningTokens: 2})
	require.Equal(t, 19, usage["prompt_tokens"])
	require.Equal(t, 5, usage["completion_tokens"])
	require.Equal(t, 24, usage["total_tokens"])
	require.Equal(t, gin.H{"reasoning_tokens": 2}, usage["completion_tokens_details"])
}

// Tool wire numbering is unverified. Test the supported M1 rejection boundary
// rather than inventing a native tool interaction fixture.
func TestCursorToolsRejectedBeforeNativeRun(t *testing.T) {
	gateway, account, exchanges := cursorTestGateway()
	recorder := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(recorder)
	result, err := gateway.ForwardAsChatCompletions(context.Background(), requestContext, account, []byte(`{"model":"default","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"test"}}]}`))
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, 400, recorder.Code)
	require.Contains(t, recorder.Body.String(), "does not support tools")
	require.Zero(t, exchanges.Load())
}
