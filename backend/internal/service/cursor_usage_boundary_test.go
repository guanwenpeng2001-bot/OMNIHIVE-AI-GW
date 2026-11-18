//go:build unit

package service

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/cursor"
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

// Built-in IDE tools remain outside the MCP bridge.
func TestCursorUnsupportedToolsRejectedBeforeNativeRun(t *testing.T) {
	gateway, account, exchanges := cursorTestGateway()
	recorder := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(recorder)
	result, err := gateway.ForwardAsChatCompletions(context.Background(), requestContext, account, []byte(`{"model":"default","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"web_search"}]}`))
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, 400, recorder.Code)
	require.Contains(t, recorder.Body.String(), "requires unique function tools")
	require.Zero(t, exchanges.Load())
}
