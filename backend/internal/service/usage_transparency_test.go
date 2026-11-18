package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

// U01-U04: accounting reads beside the original payload, including new fields
// and the distinction between absent, null, zero, and partial usage.
func TestUsageTransparencyNativeJSONReplay(testingT *testing.T) {
	for _, protocol := range []string{"anthropic", "openai", "gemini"} {
		usageField := "usage"
		usageFixture := `{"input_tokens":10,"output_tokens":20,"cache_read_input_tokens":60,"cache_creation_input_tokens":30,"cache_creation":{"ephemeral_5m_input_tokens":12,"ephemeral_1h_input_tokens":18},"future_detail":{"tokens":7}}`
		content := `"content":[{"type":"text","text":"ok"}]`
		if protocol == "openai" {
			usageFixture = `{"input_tokens":100,"output_tokens":30,"total_tokens":130,"input_tokens_details":{"cached_tokens":60},"output_tokens_details":{"reasoning_tokens":10},"future_detail":{"tokens":7}}`
			content = `"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]`
		}
		if protocol == "gemini" {
			usageField = "usageMetadata"
			usageFixture = `{"promptTokenCount":100,"cachedContentTokenCount":60,"candidatesTokenCount":20,"thoughtsTokenCount":10,"totalTokenCount":130,"promptTokensDetails":[{"modality":"TEXT","tokenCount":100}],"futureDetail":{"tokens":7}}`
			content = `"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]`
		}
		zeroUsage := `{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0}`
		partialUsage := `{"output_tokens":20}`
		if protocol == "gemini" {
			zeroUsage = `{"promptTokenCount":0,"candidatesTokenCount":0,"thoughtsTokenCount":0}`
			partialUsage = `{"toolUsePromptTokenCount":13,"promptTokensDetails":[{"modality":"IMAGE","tokenCount":7}]}`
		}
		for _, usageValue := range []string{usageFixture, "null", "{}", zeroUsage, partialUsage, ""} {
			testingT.Run(protocol+"/"+usageValue, func(testingT *testing.T) {
				body := "{" + content + ",\"future_envelope\":true"
				if usageValue != "" {
					body += fmt.Sprintf(",%q:%s", usageField, usageValue)
				}
				body += "}"
				recorder := httptest.NewRecorder()
				requestContext, _ := gin.CreateTestContext(recorder)
				requestContext.Request = httptest.NewRequest(http.MethodPost, "/", nil)
				response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
				account := &Account{ID: 53}
				forcedBilling := WithForceCacheBilling(context.Background())
				switch protocol {
				case "anthropic":
					gateway := &GatewayService{cfg: &config.Config{}}
					usage, err := gateway.handleNonStreamingResponseAnthropicAPIKeyPassthrough(forcedBilling, response, requestContext, account)
					require.NoError(testingT, err)
					if usageValue == usageFixture {
						require.Equal(testingT, 10, usage.InputTokens)
						require.Equal(testingT, 20, usage.OutputTokens)
						require.Equal(testingT, 60, usage.CacheReadInputTokens)
						require.Equal(testingT, 30, usage.CacheCreationInputTokens)
					}
				case "openai":
					gateway := &OpenAIGatewayService{cfg: &config.Config{}}
					result, err := gateway.handleNonStreamingResponsePassthrough(forcedBilling, response, requestContext, account, "", "")
					require.NoError(testingT, err)
					if usageValue == usageFixture {
						require.Equal(testingT, 100, result.usage.InputTokens)
						require.Equal(testingT, 30, result.usage.OutputTokens)
						require.Equal(testingT, 60, result.usage.CacheReadInputTokens)
					}
				case "gemini":
					gateway := &GeminiMessagesCompatService{cfg: &config.Config{}}
					usage, err := gateway.handleNativeNonStreamingResponse(requestContext, response, false, account, "")
					require.NoError(testingT, err)
					if usageValue == usageFixture {
						require.Equal(testingT, 40, usage.InputTokens)
						require.Equal(testingT, 30, usage.OutputTokens)
						require.Equal(testingT, 60, usage.CacheReadInputTokens)
					}
				}
				require.Equal(testingT, body, recorder.Body.String(), "raw usage must not be rebuilt from billing buckets")
			})
		}
	}
}

func TestUsageTransparencyAnthropicStreamReplay(testingT *testing.T) {
	body := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"message-fixture","type":"message","role":"assistant","content":[],"usage":{"input_tokens":10,"cache_read_input_tokens":60,"cache_creation_input_tokens":30,"cache_creation":{"ephemeral_5m_input_tokens":12,"ephemeral_1h_input_tokens":18},"future_detail":7}}}`,
		"",
		`event: future_event`,
		`data: {"type":"future_event","usage_extension":{"count":9}}`,
		"",
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":20,"future_detail":8}}`,
		"",
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		"", "",
	}, "\n")
	recorder := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(recorder)
	requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	gateway := &GatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}
	result, err := gateway.handleStreamingResponseAnthropicAPIKeyPassthrough(WithForceCacheBilling(context.Background()), response, requestContext, &Account{ID: 53}, time.Now(), "claude-sonnet-4-5")
	require.NoError(testingT, err)
	require.Equal(testingT, body, recorder.Body.String())
	require.Equal(testingT, 10, result.usage.InputTokens)
	require.Equal(testingT, 20, result.usage.OutputTokens)
	require.Equal(testingT, 60, result.usage.CacheReadInputTokens)
	require.Equal(testingT, 30, result.usage.CacheCreationInputTokens)
}
