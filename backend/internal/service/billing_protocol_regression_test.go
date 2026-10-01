//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func TestCommandCodeMessagesPreservesExplicitEffort(t *testing.T) {
	for _, effort := range []string{"", "none", "minimal", "low", "medium", "high"} {
		body := []byte(`{"model":"moonshotai/Kimi-K2.7-Code-Highspeed","max_tokens":32,"messages":[{"role":"user","content":"OK"}]}`)
		if effort != "" {
			body, _ = sjson.SetBytes(body, "output_config.effort", effort)
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
		u := &httpUpstreamRecorder{resp: qianwenModelPage(`{"id":"test","choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":2}}`, 200)}
		s := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: u}
		a := rawChatCompletionsTestAccount()
		a.Platform = PlatformCommandCode
		result, err := s.forwardAnthropicViaRawChatCompletions(context.Background(), c, a, body, "")
		require.NoError(t, err)
		require.NotNil(t, result)
		want := effort
		require.Equal(t, want, gjson.GetBytes(u.lastBody, "reasoning_effort").String())
		require.Equal(t, want, optionalStringValue(result.ReasoningEffort))
	}
}

func TestQianwenUnknownConditionalModeRejectedBeforeConsumption(t *testing.T) {
	for _, protocol := range []string{"chat/completions", "messages", "responses", "native"} {
		body := []byte(`{"model":"unknown-conditional","max_tokens":32,"messages":[{"role":"user","content":"OK"}],"input":{"prompt":"OK"}}`)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/"+protocol, bytes.NewReader(body))
		price := 1e-6
		c.Set("api_key", &APIKey{Group: &Group{Platform: PlatformQianwen, ModelPricing: []ChannelModelPricing{{Platform: PlatformQianwen, NativePricingSource: QianwenPricingSource, Models: []string{"unknown-conditional"}, InputPrice: &price, OutputPrice: &price, ThinkingPricing: &QianwenModePricing{InputPrice: &price, OutputPrice: &price}}}}})
		u := &httpUpstreamRecorder{}
		s := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: u, resolver: NewModelPricingResolver(nil, newTestBillingServiceForResolver())}
		a := rawChatCompletionsTestAccount()
		a.Platform = PlatformQianwen
		var err error
		switch protocol {
		case "chat/completions":
			_, err = s.forwardAsRawChatCompletions(context.Background(), c, a, body, "")
		case "messages":
			_, err = s.forwardAnthropicViaRawChatCompletions(context.Background(), c, a, body, "")
		case "responses":
			body, _ = sjson.SetBytes(body, "input", "OK")
			_, err = s.forwardResponsesViaRawChatCompletions(context.Background(), c, a, body)
		case "native":
			_, err = s.ForwardQianwenNative(context.Background(), c, a, QianwenNativeText, body, false)
		}
		require.ErrorIs(t, err, ErrQianwenThinkingUndetermined, protocol)
		require.Empty(t, u.requests, protocol)
		require.Equal(t, http.StatusBadRequest, c.Writer.Status(), protocol)
	}
}

func TestQianwenThinkingSurvivesEveryInboundProtocol(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{`"enable_thinking":false`, `"enable_thinking":true`, `"thinking":{"type":"enabled"}`, `"thinking":{"type":"disabled"}`, `"unused":null`} {
		for _, protocol := range []string{"chat/completions", "messages", "responses"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/stream=%v", protocol, mode, stream), func(t *testing.T) {
					body := []byte(`{"model":"qwen-turbo","stream":false,` + mode + `,"max_tokens":32,"messages":[{"role":"user","content":"OK"}]}`)
					if protocol == "responses" {
						body = []byte(`{"model":"qwen-turbo","stream":false,` + mode + `,"input":"OK"}`)
					}
					body, _ = sjson.SetBytes(body, "stream", stream)
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest("POST", "/v1/"+protocol, bytes.NewReader(body))
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"audit","model":"qwen-turbo","choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":2,"total_tokens":14}}`))}}
					if stream {
						upstream.resp.Header.Set("Content-Type", "text/event-stream")
						upstream.resp.Body = io.NopCloser(strings.NewReader("data: " + `{"id":"audit","model":"qwen-turbo","choices":[{"index":0,"delta":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":2,"total_tokens":14}}` + "\n\ndata: [DONE]\n\n"))
					}
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
					account := rawChatCompletionsTestAccount()
					account.Platform = PlatformQianwen
					var result *OpenAIForwardResult
					var err error
					switch protocol {
					case "chat/completions":
						result, err = svc.forwardAsRawChatCompletions(context.Background(), c, account, body, "")
					case "messages":
						result, err = svc.forwardAnthropicViaRawChatCompletions(context.Background(), c, account, body, "")
					case "responses":
						result, err = svc.forwardResponsesViaRawChatCompletions(context.Background(), c, account, body)
					}
					require.NoError(t, err)
					require.NotNil(t, result)
					t.Logf("protocol=%s outbound_enable_thinking=%s outbound_reasoning_effort=%s result_thinking=%v", protocol, gjson.GetBytes(upstream.lastBody, "enable_thinking"), gjson.GetBytes(upstream.lastBody, "reasoning_effort"), result.ThinkingEnabled)
					require.NotNil(t, result.ThinkingEnabled, "explicit false must reach conditional billing")
					want := strings.Contains(mode, "true") || strings.Contains(mode, `"enabled"`)
					require.Equal(t, want, *result.ThinkingEnabled)
					require.Equal(t, want, gjson.GetBytes(upstream.lastBody, "enable_thinking").Bool())
				})
			}
		}

	}
}
