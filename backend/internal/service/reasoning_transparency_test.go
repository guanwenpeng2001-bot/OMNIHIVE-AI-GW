//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/cursor"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestReasoningTransparencyNativeBudgetReplay(testingT *testing.T) {
	for _, budget := range []int{2048, 4096} {
		for _, stream := range []bool{false, true} {
			testingT.Run(fmt.Sprintf("budget=%d/stream=%t", budget, stream), func(testingT *testing.T) {
				body := []byte(fmt.Sprintf(`{"model":"claude-sonnet-4-5","max_tokens":8192,"stream":%t,"thinking":{"type":"enabled","budget_tokens":%d},"messages":[{"role":"user","content":"hello"}]}`, stream, budget))
				recorder := httptest.NewRecorder()
				requestContext, _ := gin.CreateTestContext(recorder)
				requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
				gateway := &GatewayService{cfg: &config.Config{}}
				account := &Account{ID: 41, Platform: PlatformAnthropic, Type: AccountTypeAPIKey}
				upstreamRequest, forwarded, err := gateway.buildUpstreamRequestAnthropicAPIKeyPassthrough(context.Background(), requestContext, account, body, "fixture-key")
				require.NoError(testingT, err)
				captured, err := io.ReadAll(upstreamRequest.Body)
				require.NoError(testingT, err)
				require.JSONEq(testingT, string(body), string(forwarded))
				require.JSONEq(testingT, string(body), string(captured))
				require.EqualValues(testingT, budget, gjson.GetBytes(captured, "thinking.budget_tokens").Int())
			})
		}
	}
}

func TestReasoningTransparencyMappedModelReplay(testingT *testing.T) {
	for _, endpoint := range []string{"responses", "chat/completions"} {
		for _, effort := range []string{"none", "low", "high", "minimal"} {
			testingT.Run(endpoint+"/"+effort, func(testingT *testing.T) {
				request := map[string]any{"model": "public-alias", "stream": false}
				if endpoint == "responses" {
					request["input"] = "hello"
					request["reasoning"] = map[string]string{"effort": effort}
					request["max_output_tokens"] = 2048
				} else {
					request["messages"] = []map[string]string{{"role": "user", "content": "hello"}}
					request["reasoning_effort"] = effort
					request["max_completion_tokens"] = 2048
				}
				body, err := json.Marshal(request)
				require.NoError(testingT, err)
				upstream := &httpUpstreamRecorder{resp: nativeAnthropicStreamResponse()}
				gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
				account := nativeAnthropicTestAccount()
				account.Credentials["model_mapping"] = map[string]any{"public-alias": "claude-sonnet-4-6"}
				requestContext := adaptiveProtocolTestContext("/v1/"+endpoint, body)
				if endpoint == "responses" {
					_, err = gateway.forwardResponsesViaNativeAnthropic(context.Background(), requestContext, account, body, "")
				} else {
					_, err = gateway.forwardChatCompletionsViaNativeAnthropic(context.Background(), requestContext, account, body, "")
				}
				if effort == "minimal" {
					require.ErrorContains(testingT, err, "unsupported reasoning.effort")
					require.Nil(testingT, upstream.lastReq)
					require.Equal(testingT, http.StatusBadRequest, requestContext.Writer.Status())
					return
				}
				require.NoError(testingT, err)
				require.Equal(testingT, "claude-sonnet-4-6", gjson.GetBytes(upstream.lastBody, "model").String())
				require.EqualValues(testingT, 2048, gjson.GetBytes(upstream.lastBody, "max_tokens").Int())
				require.False(testingT, gjson.GetBytes(upstream.lastBody, "thinking.budget_tokens").Exists())
				if effort == "none" {
					require.Equal(testingT, "disabled", gjson.GetBytes(upstream.lastBody, "thinking.type").String())
					require.False(testingT, gjson.GetBytes(upstream.lastBody, "output_config.effort").Exists())
				} else {
					require.Equal(testingT, "adaptive", gjson.GetBytes(upstream.lastBody, "thinking.type").String())
					require.Equal(testingT, effort, gjson.GetBytes(upstream.lastBody, "output_config.effort").String())
				}
			})
		}
	}
}

func TestReasoningTransparencyOpenAIBudgetCompatibility(testingT *testing.T) {
	for _, budget := range []int{2048, 4096} {
		for _, forceChat := range []bool{false, true} {
			testingT.Run(fmt.Sprintf("budget=%d/chat=%t", budget, forceChat), func(testingT *testing.T) {
				body := []byte(fmt.Sprintf(`{"model":"gpt-5.2","max_tokens":8192,"thinking":{"type":"enabled","budget_tokens":%d},"messages":[{"role":"user","content":"hello"}]}`, budget))
				upstream := &httpUpstreamRecorder{resp: openAICompatSSECompletedResponse("budget_compatibility", "gpt-5.2")}
				if forceChat {
					upstream.resp = &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"budget-chat","model":"gpt-5.2","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))}
				}
				gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
				account := rawChatCompletionsTestAccount()
				if forceChat {
					account = forceChatMessagesFallbackAccount()
				}
				requestContext := adaptiveProtocolTestContext("/v1/messages", body)
				result, err := gateway.ForwardAsAnthropic(context.Background(), requestContext, account, body, "", "")
				require.NoError(testingT, err)
				require.NotNil(testingT, result)
				require.NotNil(testingT, upstream.lastReq)
				field := "reasoning.effort"
				if forceChat {
					field = "reasoning_effort"
				}
				require.Equal(testingT, "low", gjson.GetBytes(upstream.lastBody, field).String())
				require.Equal(testingT, http.StatusOK, requestContext.Writer.Status())
			})
		}
	}
}

func TestReasoningTransparencyCursorBudgetCompatibility(t *testing.T) {
	for _, tc := range []struct{ controls, effort string }{
		{`"thinking":{"type":"enabled","budget_tokens":2048}`, "low"},
		{`"thinking":{"type":"enabled","budget_tokens":32768}`, "high"},
		{`"thinking":{"type":"adaptive","budget_tokens":32768},"output_config":{"effort":"low"}`, "low"},
		{`"thinking":{"type":"adaptive"},"output_config":{"effort":"max"}`, "max"},
		{`"thinking":{"type":"adaptive"}`, ""},
		{`"thinking":{"type":"disabled"},"output_config":{"effort":"high"}`, "none"},
	} {
		body := []byte(`{"model":"claude-opus-5","max_tokens":64000,"messages":[{"role":"user","content":"hello"}],` + tc.controls + `}`)
		converted, _, err := cursorNormalizeRequest(body, APIProtocolAnthropic)
		require.NoError(t, err)
		require.Equal(t, tc.effort, gjson.GetBytes(converted, "reasoning_effort").String())
	}
}

func TestReasoningTransparencyBudgetGroupPolicy(t *testing.T) {
	for _, forceChat := range []bool{false, true} {
		for _, deny := range []bool{false, true} {
			t.Run(fmt.Sprintf("chat=%t/deny=%t", forceChat, deny), func(t *testing.T) {
				body := []byte(`{"model":"gpt-5.2","max_tokens":64000,"thinking":{"type":"enabled","budget_tokens":32768},"messages":[{"role":"user","content":"hello"}]}`)
				upstream := &httpUpstreamRecorder{resp: openAICompatSSECompletedResponse("budget_policy", "gpt-5.2")}
				account := rawChatCompletionsTestAccount()
				field, capField := "reasoning.effort", "max_output_tokens"
				if forceChat {
					account = forceChatMessagesFallbackAccount()
					field = "reasoning_effort"
					capField = "max_completion_tokens"
					upstream.resp = &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"chat","model":"gpt-5.2","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))}
				}
				c := adaptiveProtocolTestContext("/v1/messages", body)
				mode := ReasoningEffortOverLimitDowngrade
				if deny {
					mode = ReasoningEffortOverLimitDeny
				}
				ctx := WithOpenAIReasoningEffortPolicy(context.Background(), "medium", nil, mode)
				c.Request = c.Request.WithContext(ctx)
				gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
				result, err := gateway.ForwardAsAnthropic(ctx, c, account, body, "", "")
				if deny {
					require.Error(t, err)
					require.Nil(t, upstream.lastReq)
					return
				}
				require.NoError(t, err)
				require.Equal(t, "medium", gjson.GetBytes(upstream.lastBody, field).String())
				require.EqualValues(t, 64000, gjson.GetBytes(upstream.lastBody, capField).Int())
				require.Nil(t, result.RequestedReasoningEffort)
				require.Equal(t, "medium", *result.ReasoningEffort)
			})
		}
	}
}

func TestBinaryReasoningCeilingNeverWidens(t *testing.T) {
	for _, tc := range []struct{ in, want string }{{"none", "none"}, {"minimal", "minimal"}, {"low", "low"}, {"medium", "low"}, {"high", "high"}, {"max", "high"}} {
		require.Equal(t, tc.want, binaryReasoningCeiling(tc.in))
	}
}

func TestReasoningTransparencyCursorBudgetVariantAndCeiling(t *testing.T) {
	for _, tc := range []struct {
		budget                int
		ceiling, action, want string
	}{
		{2048, "", "", "claude-opus-5-thinking-low"},
		{32768, "", "", "claude-opus-5-thinking-high"},
		{32768, "medium", "", "claude-opus-5-thinking-medium"},
		{32768, "medium", ReasoningEffortOverLimitDeny, ""},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/%s/%s/%t", tc.budget, tc.ceiling, tc.action, stream), func(t *testing.T) {
				gateway, account, _ := cursorTestGateway()
				calls := 0
				gateway.native = cursorDriverStub{run: func(_ context.Context, _ cursor.Credentials, _ []cursor.ChatMessage, model string, emit func(cursor.StreamEvent) error) (cursor.TokenUsage, error) {
					calls++
					require.Equal(t, tc.want, model)
					require.NoError(t, emit(cursor.StreamEvent{Type: "text", Text: "ok"}))
					return cursor.TokenUsage{InputTokens: 1, OutputTokens: 1}, nil
				}}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Set("api_key", &APIKey{Group: &Group{Platform: PlatformComposite, MaxReasoningEffort: tc.ceiling, MaxReasoningEffortOverLimit: tc.action}})
				body := []byte(fmt.Sprintf(`{"model":"claude-opus-5","max_tokens":64000,"stream":%t,"thinking":{"type":"enabled","budget_tokens":%d},"messages":[{"role":"user","content":"hi"}]}`, stream, tc.budget))
				result, err := gateway.ForwardAsAnthropic(context.Background(), c, account, body)
				if tc.want == "" {
					require.True(t, IsReasoningEffortPolicyDenied(err))
					require.Zero(t, calls)
					return
				}
				require.NoError(t, err)
				require.Equal(t, 1, calls)
				require.Equal(t, tc.want, result.UpstreamModel)
				require.Equal(t, cursor.ResolvedModelEffort(tc.want), *result.ReasoningEffort)
				require.Nil(t, result.RequestedReasoningEffort)
				if stream {
					require.Contains(t, recorder.Body.String(), "event: message_stop")
				}
			})
		}
	}
}
