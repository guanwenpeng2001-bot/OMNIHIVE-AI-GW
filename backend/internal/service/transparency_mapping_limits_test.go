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

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func transparencyReasoningBody(testingT *testing.T, protocol, model string) []byte {
	testingT.Helper()
	request := map[string]any{"model": model}
	if protocol == APIProtocolResponses {
		request["input"] = "hello"
		request["reasoning"] = map[string]string{"effort": "high"}
		request["max_output_tokens"] = 2048
	} else {
		request["messages"] = []map[string]string{{"role": "user", "content": "hello"}}
		request["reasoning_effort"] = "high"
		request["max_completion_tokens"] = 2048
	}
	body, err := json.Marshal(request)
	require.NoError(testingT, err)
	return body
}

func TestTransparencyAntigravityFinalModelReplay(testingT *testing.T) {
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolResponses} {
		testingT.Run(protocol, func(testingT *testing.T) {
			var directConfig string
			for _, model := range []string{"claude-sonnet-4-6", "public-alias"} {
				upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{antigravityCompatSuccessResponse()}}
				gateway := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, upstream)
				account := newAntigravityCompatAccount(AccountTypeOAuth)
				account.Credentials["model_mapping"] = map[string]any{"public-alias": "claude-sonnet-4-6", "claude-sonnet-4-6": "claude-sonnet-4-6"}
				body := transparencyReasoningBody(testingT, protocol, model)
				requestContext, recorder := newAntigravityCompatContext(http.MethodPost, "/v1/"+protocol, body)
				var result *ForwardResult
				var err error
				if protocol == APIProtocolResponses {
					result, err = gateway.ForwardAsResponses(context.Background(), requestContext, account, body, nil)
				} else {
					result, err = gateway.ForwardAsChatCompletions(context.Background(), requestContext, account, body, nil)
				}
				require.NoError(testingT, err)
				require.Equal(testingT, model, result.Model)
				require.Equal(testingT, "claude-sonnet-4-6", result.UpstreamModel)
				require.Equal(testingT, model, gjson.Get(recorder.Body.String(), "model").String())
				require.NotNil(testingT, result.ReasoningEffort)
				require.Equal(testingT, "high", *result.ReasoningEffort)
				require.Len(testingT, upstream.requestBodies, 1)
				sent := upstream.requestBodies[0]
				require.Equal(testingT, "claude-sonnet-4-6", gjson.GetBytes(sent, "model").String())
				require.EqualValues(testingT, 2048, gjson.GetBytes(sent, "request.generationConfig.maxOutputTokens").Int())
				require.True(testingT, gjson.GetBytes(sent, "request.generationConfig.thinkingConfig.includeThoughts").Bool())
				require.EqualValues(testingT, -1, gjson.GetBytes(sent, "request.generationConfig.thinkingConfig.thinkingBudget").Int())
				// Antigravity's existing Gemini wire format has no separate effort field.
				require.False(testingT, gjson.GetBytes(sent, "request.output_config.effort").Exists())
				require.False(testingT, gjson.GetBytes(sent, "request.generationConfig.effort").Exists())
				generationConfig := gjson.GetBytes(sent, "request.generationConfig").Raw
				if directConfig == "" {
					directConfig = generationConfig
				} else {
					require.JSONEq(testingT, directConfig, generationConfig)
				}
			}
			for _, target := range []string{"claude-sonnet-4-5", "unknown-model"} {
				upstream := &queuedHTTPUpstreamStub{}
				if target == "unknown-model" {
					upstream.responses = []*http.Response{antigravityCompatSuccessResponse()}
				}
				gateway := newAntigravityCompatService(config.GatewayConfig{}, upstream)
				account := newAntigravityCompatAccount(AccountTypeOAuth)
				account.Credentials["model_mapping"] = map[string]any{"claude-sonnet-4-6": target}
				body := transparencyReasoningBody(testingT, protocol, "claude-sonnet-4-6")
				requestContext, recorder := newAntigravityCompatContext(http.MethodPost, "/v1/"+protocol, body)
				var err error
				if protocol == APIProtocolResponses {
					_, err = gateway.ForwardAsResponses(context.Background(), requestContext, account, body, nil)
				} else {
					_, err = gateway.ForwardAsChatCompletions(context.Background(), requestContext, account, body, nil)
				}
				if target == "unknown-model" {
					require.NoError(testingT, err)
					require.Equal(testingT, http.StatusOK, recorder.Code)
					require.Len(testingT, upstream.requestBodies, 1)
					require.Equal(testingT, target, gjson.GetBytes(upstream.requestBodies[0], "model").String())
					continue
				}
				require.ErrorContains(testingT, err, "unsupported reasoning.effort")
				require.Equal(testingT, http.StatusBadRequest, recorder.Code)
				require.Empty(testingT, upstream.requestBodies)
			}
		})
	}
}

func TestTransparencyCursorFinalModelReplay(testingT *testing.T) {
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolResponses} {
		testingT.Run(protocol, func(testingT *testing.T) {
			var directBody string
			for _, scenario := range []struct {
				model, target string
				rejected      bool
			}{
				{model: "claude-sonnet-4-6", target: "claude-sonnet-4-6"},
				{model: "public-alias", target: "claude-sonnet-4-6"},
				{model: "claude-sonnet-4-6", target: "claude-sonnet-4-5", rejected: true},
				{model: "claude-sonnet-4-6", target: "unknown-model"},
			} {
				var captured []byte
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					captured, _ = io.ReadAll(request.Body)
					writer.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(writer, cursorRelayFixture(APIProtocolAnthropic, false))
				}))
				account := &Account{ID: 73, Platform: PlatformCursor, Type: AccountTypeUpstream, Credentials: map[string]any{
					"base_url": server.URL, "api_key": "fixture-key", "api_protocol": APIProtocolAnthropic,
					"model_mapping": map[string]any{scenario.model: scenario.target},
				}}
				gateway := NewCursorGatewayService(nil, nil)
				gateway.relayClient = server.Client()
				body := transparencyReasoningBody(testingT, protocol, scenario.model)
				requestContext := adaptiveProtocolTestContext("/v1/"+protocol, body)
				result, err := gateway.forwardRelayProtocol(context.Background(), requestContext, account, body, protocol)
				server.Close()
				if scenario.rejected {
					require.ErrorContains(testingT, err, "unsupported reasoning.effort")
					require.Empty(testingT, captured)
					continue
				}
				require.NoError(testingT, err)
				require.Equal(testingT, scenario.model, result.Model)
				require.Equal(testingT, scenario.target, result.UpstreamModel)
				require.Equal(testingT, scenario.target, gjson.GetBytes(captured, "model").String())
				require.Equal(testingT, "adaptive", gjson.GetBytes(captured, "thinking.type").String())
				require.Equal(testingT, "high", gjson.GetBytes(captured, "output_config.effort").String())
				require.False(testingT, gjson.GetBytes(captured, "thinking.budget_tokens").Exists())
				require.EqualValues(testingT, 2048, gjson.GetBytes(captured, "max_tokens").Int())
				if scenario.target == "unknown-model" {
					continue
				}
				if directBody == "" {
					directBody = string(captured)
				} else {
					require.JSONEq(testingT, directBody, string(captured))
				}
			}
		})
	}
}

func TestTransparencyGenericAnthropicTokenLimits(testingT *testing.T) {
	for _, entry := range []string{"anthropic-gateway", "openai-native-anthropic"} {
		for _, scenario := range []struct {
			name, fields              string
			expected                  int
			malformed, upstreamReject bool
		}{
			{name: "legacy above ceiling", fields: `,"max_tokens":64001`, expected: 64001},
			{name: "completion above ceiling", fields: `,"max_completion_tokens":128000`, expected: 128000},
			{name: "completion precedence", fields: `,"max_tokens":64001,"max_completion_tokens":128000`, expected: 128000},
			{name: "small integer", fields: `,"max_tokens":64001,"max_completion_tokens":8`, expected: 8},
			{name: "default", expected: 8192},
			{name: "zero retains upstream rejection", fields: `,"max_tokens":0`, expected: 0, upstreamReject: true},
			{name: "negative precedence retains upstream rejection", fields: `,"max_tokens":128000,"max_completion_tokens":-1`, expected: -1, upstreamReject: true},
			{name: "fraction", fields: `,"max_tokens":2.5`, malformed: true},
			{name: "string", fields: `,"max_completion_tokens":"128000"`, malformed: true},
			{name: "overflow", fields: `,"max_tokens":9223372036854775808`, malformed: true},
		} {
			testingT.Run(entry+"/"+scenario.name, func(testingT *testing.T) {
				body := []byte(fmt.Sprintf(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}],"reasoning_effort":"high"%s}`, scenario.fields))
				upstream := &httpUpstreamRecorder{resp: nativeAnthropicStreamResponse()}
				if scenario.upstreamReject {
					upstream.resp = &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","message":"max_tokens must be positive"}}`))}
				}
				account := nativeAnthropicTestAccount()
				requestContext := adaptiveProtocolTestContext("/v1/chat/completions", body)
				var err error
				if entry == "anthropic-gateway" {
					account.Platform = PlatformAnthropic
					account.Credentials["base_url"] = "https://anthropic.example"
					gateway := &GatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
					_, err = gateway.ForwardAsChatCompletions(context.Background(), requestContext, account, body, nil)
				} else {
					gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
					_, err = gateway.forwardChatCompletionsViaNativeAnthropic(context.Background(), requestContext, account, body, "")
				}
				if scenario.malformed {
					require.Error(testingT, err)
					require.Nil(testingT, upstream.lastReq)
					return
				}
				if scenario.upstreamReject {
					require.Error(testingT, err)
				} else {
					require.NoError(testingT, err)
				}
				require.NotNil(testingT, upstream.lastReq)
				require.True(testingT, gjson.GetBytes(upstream.lastBody, "max_tokens").Exists())
				require.EqualValues(testingT, scenario.expected, gjson.GetBytes(upstream.lastBody, "max_tokens").Int())
				require.Equal(testingT, "claude-sonnet-4-6", gjson.GetBytes(upstream.lastBody, "model").String())
				require.Equal(testingT, "adaptive", gjson.GetBytes(upstream.lastBody, "thinking.type").String())
				require.Equal(testingT, "high", gjson.GetBytes(upstream.lastBody, "output_config.effort").String())
			})
		}
	}
}
