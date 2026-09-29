//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
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

func TestReasoningTransparencyOpenAIBudgetRejection(testingT *testing.T) {
	for _, budget := range []int{2048, 4096} {
		for _, forceChat := range []bool{false, true} {
			testingT.Run(fmt.Sprintf("budget=%d/chat=%t", budget, forceChat), func(testingT *testing.T) {
				body := []byte(fmt.Sprintf(`{"model":"gpt-5.2","max_tokens":8192,"thinking":{"type":"enabled","budget_tokens":%d},"messages":[{"role":"user","content":"hello"}]}`, budget))
				upstream := &httpUpstreamRecorder{}
				gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
				account := rawChatCompletionsTestAccount()
				if forceChat {
					account = forceChatMessagesFallbackAccount()
				}
				requestContext := adaptiveProtocolTestContext("/v1/messages", body)
				result, err := gateway.ForwardAsAnthropic(context.Background(), requestContext, account, body, "", "")
				require.ErrorContains(testingT, err, "unsupported thinking.budget_tokens")
				require.Nil(testingT, result)
				require.Nil(testingT, upstream.lastReq)
				require.Equal(testingT, http.StatusBadRequest, requestContext.Writer.Status())
			})
		}
	}
}

func TestReasoningTransparencyCursorBudgetRejection(testingT *testing.T) {
	for _, budget := range []int{2048, 4096} {
		body := []byte(fmt.Sprintf(`{"model":"claude-opus-5","max_tokens":8192,"thinking":{"type":"enabled","budget_tokens":%d},"messages":[{"role":"user","content":"hello"}]}`, budget))
		converted, _, err := cursorNormalizeRequest(body, APIProtocolAnthropic)
		require.ErrorContains(testingT, err, "unsupported thinking.budget_tokens")
		require.Nil(testingT, converted)
	}
}
