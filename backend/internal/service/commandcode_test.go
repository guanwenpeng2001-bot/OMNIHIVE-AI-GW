//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/model"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func commandCodeTestAccount() *Account {
	return &Account{ID: 905, Platform: PlatformCommandCode, Type: AccountTypeAPIKey, Status: StatusActive, Concurrency: 1, Credentials: map[string]any{"api_key": "cc-test"}}
}
func TestCommandCodeProtocolAndBases(t *testing.T) {
	a := commandCodeTestAccount()
	require.True(t, a.IsOpenAICompatible())
	require.False(t, a.IsCNProvider())
	require.True(t, a.SupportsNativeCNResponses())
	require.Equal(t, APIProtocolAdaptive, a.GetAPIProtocol())
	require.Equal(t, DefaultCommandCodeBaseURL, a.GetOpenAIBaseURL())
	require.Equal(t, DefaultCommandCodeBaseURL, a.GetAnthropicProtocolBaseURL())
	for _, tc := range []struct{ model, protocol string }{
		{"claude-sonnet-5", APIProtocolAnthropic}, {" ANTHROPIC/Claude-Opus-5 ", APIProtocolAnthropic},
		{"foo/claude-sonnet-5", APIProtocolChatCompletions}, {"Qwen/Qwen3.7-Max", APIProtocolChatCompletions},
		{"deepseek/deepseek-v4-flash", APIProtocolChatCompletions}, {"glm-5", APIProtocolChatCompletions}, {"kimi-k3", APIProtocolChatCompletions},
	} {
		require.Equal(t, tc.protocol, a.ResolveCommandCodeUpstreamProtocol(tc.model), tc.model)
	}
	a.Credentials["api_protocol"] = APIProtocolResponses
	require.Equal(t, APIProtocolResponses, a.ResolveCommandCodeUpstreamProtocol("claude-sonnet-5"))
	a.Credentials["api_protocol"] = APIProtocolChatCompletions
	require.Equal(t, APIProtocolChatCompletions, a.ResolveCommandCodeUpstreamProtocol("claude-sonnet-5"))
	a.Credentials["api_protocol"] = APIProtocolAdaptive
	a.Credentials["protocol_rules"] = []any{map[string]any{"pattern": "custom/claude-*", "protocol": APIProtocolAnthropic}}
	require.Equal(t, APIProtocolAnthropic, a.ResolveCommandCodeUpstreamProtocol("custom/claude-test"))
	require.Equal(t, APIProtocolAnthropic, a.ResolveCommandCodeUpstreamProtocol("claude-sonnet-5"))
	a.Credentials["base_url"] = "https://relay.example/provider/v1"
	require.Equal(t, "https://relay.example/provider/v1", a.GetAnthropicProtocolBaseURL())
	a.Credentials["api_base_urls"] = map[string]any{APIProtocolAnthropic: "https://anthropic.example/v1"}
	require.Equal(t, "https://anthropic.example/v1", a.GetAnthropicProtocolBaseURL())
	require.Contains(t, model.AllPlatforms(), PlatformCommandCode)
	require.Contains(t, model.AllPlatforms(), PlatformCursor)
	require.Equal(t, PlatformCommandCode, NormalizeOpenAICompatiblePlatform(PlatformCommandCode))
}

func TestCommandCodeGatewayAllIngresses(t *testing.T) {
	for _, ingress := range []string{"chat", "messages", "responses"} {
		for _, native := range []string{"chat", "messages"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/stream=%v", ingress, native, stream), func(t *testing.T) {
					a := commandCodeTestAccount()
					target := "Qwen/Qwen3.7-Max"
					response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"cc1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))}
					endpoint := "/chat/completions"
					if native == "messages" {
						target = "claude-sonnet-5"
						endpoint = "/messages"
						response = nativeAnthropicBufferedResponse()
					}
					if native == "messages" && ingress != "messages" {
						response = nativeAnthropicStreamResponse()
					}
					if stream {
						if native == "messages" {
							response = nativeAnthropicStreamResponse()
						} else {
							response = adaptiveCNChatTestResponse()
						}
					}
					a.Credentials["model_mapping"] = map[string]any{"public-model": target}
					body := []byte(fmt.Sprintf(`{"model":"public-model","max_tokens":32,"stream":%v,"messages":[{"role":"user","content":"hi"}]}`, stream))
					if ingress == "responses" {
						body = []byte(fmt.Sprintf(`{"model":"public-model","stream":%v,"input":"hi"}`, stream))
					}
					upstream := &httpUpstreamRecorder{resp: response}
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
					c := adaptiveProtocolTestContext("/v1/"+ingress, body)
					var result *OpenAIForwardResult
					var err error
					switch ingress {
					case "chat":
						result, err = svc.ForwardAsChatCompletions(context.Background(), c, a, body, "", "")
					case "messages":
						result, err = svc.ForwardAsAnthropic(context.Background(), c, a, body, "", "")
					case "responses":
						SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
						result, err = svc.Forward(context.Background(), c, a, body)
					}
					if native == "chat" && stream {
						require.Equal(t, gjson.True, gjson.GetBytes(upstream.lastBody, "stream_options.include_usage").Type)
					}
					require.NoError(t, err)
					if native == "chat" && stream {
						require.Equal(t, 2, result.Usage.InputTokens)
						require.Equal(t, 1, result.Usage.OutputTokens)
					}
					require.Equal(t, DefaultCommandCodeBaseURL+endpoint, upstream.lastReq.URL.String())
					require.Equal(t, "cc-test", upstream.lastReq.Header.Get("x-api-key"))
					require.Equal(t, target, gjson.GetBytes(upstream.lastBody, "model").String())
					require.True(t, gjson.GetBytes(upstream.lastBody, "messages").IsArray())
					require.NotContains(t, upstream.lastReq.URL.Path, "responses")
				})
			}
		}
	}
}

func TestCommandCodeErrors(t *testing.T) {
	for _, tc := range []struct {
		status     int
		body, code string
	}{
		{403, `{"error":{"code":"MODEL_NOT_IN_PLAN"}}`, "MODEL_NOT_IN_PLAN"},
		{403, `{"type":"error","error":{"type":"permission_error","message":"MODEL_NOT_IN_PLAN: upgrade plan"}}`, "MODEL_NOT_IN_PLAN"},
		{403, `{"error":{"code":"upgrade_required"}}`, "UPGRADE_REQUIRED"},
		{401, `{"error":{"code":"upgrade_required"}}`, ""},
		{403, `{"error":{"message":"permission denied"}}`, ""},
	} {
		require.Equal(t, tc.code, commandCodePlanError(tc.status, []byte(tc.body)))
	}
	a := commandCodeTestAccount()
	svc := &OpenAIGatewayService{}
	routeBody := []byte(`{"success":false,"status":404,"cause":"POST /provider/v1/chat/completions is not a registered API route"}`)
	require.True(t, svc.shouldFailoverOpenAIUpstreamResponse(a, 404, "", routeBody))
	fo := svc.failoverOpenAIUpstreamHTTPError(context.Background(), nil, a, &http.Response{StatusCode: 404}, routeBody, "edge routing failure", "model")
	require.NotNil(t, fo)
	require.True(t, fo.RetryableOnSameAccount)
	require.True(t, fo.RequestScopedTransient)
	require.Equal(t, 502, fo.StatusCode)
	require.False(t, isCommandCodeTransientUpstreamError(404, []byte(`{"error":{"code":"model_not_found"}}`)))
}

func TestCommandCodeModelSyncAndCursorRegistry(t *testing.T) {
	svc := &AccountTestService{cfg: rawChatCompletionsTestConfig()}
	a := commandCodeTestAccount()
	req, err := svc.buildUpstreamModelsRequest(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, DefaultCommandCodeBaseURL+"/models", req.URL.String())
	ids, metadata, err := extractUpstreamModelCatalog([]byte(`{"data":[{"id":"Qwen/Qwen3.7-Max","context_length":1000000},{"id":"claude-sonnet-5","context_window":200000,"max_context_window":150000,"context_length":1000000}]}`), false)
	require.NoError(t, err)
	require.Contains(t, ids, "Qwen/Qwen3.7-Max")
	require.EqualValues(t, 1000000, metadata["Qwen/Qwen3.7-Max"].ContextWindow)
	require.EqualValues(t, 200000, metadata["claude-sonnet-5"].ContextWindow)
	registry := map[string]modelsDevProvider{"cursor": {Models: map[string]modelsDevModel{"claude-sonnet-5": {ID: "claude-sonnet-5"}}}}
	cursorAccount := &Account{Platform: PlatformCursor}
	provider, ok := matchModelsDevProvider(registry, upstreamModelRegistryBaseURL(cursorAccount))
	require.True(t, ok)
	require.Equal(t, "cursor", provider.ID)
	_, ok = matchModelsDevProvider(registry, "https://api2.cursor.sh.evil.example")
	require.False(t, ok)
	_, ok = matchModelsDevProvider(map[string]modelsDevProvider{}, upstreamModelRegistryBaseURL(cursorAccount))
	require.False(t, ok)
}

func TestCommandCodeGatewayPlanErrorAllIngresses(t *testing.T) {
	for _, ingress := range []string{"chat", "messages", "responses"} {
		for _, target := range []string{"claude-sonnet-5", DefaultCommandCodeTestModel} {
			t.Run(ingress+target, func(t *testing.T) {
				a := commandCodeTestAccount()
				body := []byte(fmt.Sprintf(`{"model":%q,"max_tokens":32,"messages":[{"role":"user","content":"hi"}]}`, target))
				if ingress == "responses" {
					body = []byte(fmt.Sprintf(`{"model":%q,"input":"hi"}`, target))
				}
				errorBody := `{"error":{"code":"upgrade_required","message":"upgrade plan"}}`
				if target == "claude-sonnet-5" {
					errorBody = `{"type":"error","error":{"type":"permission_error","message":"MODEL_NOT_IN_PLAN: upgrade plan"}}`
				}
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 403, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(errorBody))}}
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
				c := adaptiveProtocolTestContext("/v1/"+ingress, body)
				var err error
				switch ingress {
				case "chat":
					_, err = svc.ForwardAsChatCompletions(context.Background(), c, a, body, "", "")
				case "messages":
					_, err = svc.ForwardAsAnthropic(context.Background(), c, a, body, "", "")
				case "responses":
					SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
					_, err = svc.Forward(context.Background(), c, a, body)
				}
				require.Error(t, err)
				var failover *UpstreamFailoverError
				require.NotErrorAs(t, err, &failover)
				require.Equal(t, 403, c.Writer.Status())
			})
		}
	}
}

func TestCommandCodePinnedAnthropicSyncUsesRelay(t *testing.T) {
	a := commandCodeTestAccount()
	a.Credentials["api_protocol"] = APIProtocolAnthropic
	a.Credentials["base_url"] = "https://relay.example/provider/v1"
	req, err := (&AccountTestService{cfg: rawChatCompletionsTestConfig()}).buildUpstreamModelsRequest(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, "https://relay.example/provider/v1/models", req.URL.String())
	a.Credentials["api_protocol"] = APIProtocolAdaptive
	a.Credentials["protocol_rules"] = []any{map[string]any{"pattern": "*", "protocol": APIProtocolResponses}}
	require.Equal(t, APIProtocolResponses, a.ResolveCommandCodeUpstreamProtocol("claude-sonnet-5"))
	a.Extra = map[string]any{"anthropic_apikey_auth_scheme": AnthropicAPIKeyAuthSchemeAuthorizationBearer}
	header := http.Header{}
	setAnthropicAPIKeyAuthHeader(header, a, "cc-test", a.GetAnthropicProtocolBaseURL())
	require.Equal(t, "Bearer cc-test", header.Get("Authorization"))
}

func TestCommandCodeWAFAndCredentialState(t *testing.T) {
	for _, tc := range []struct {
		name                string
		status              int
		body                string
		transient, disabled bool
	}{
		{"waf html", 403, `<html>Access forbidden (403): The site owner has blocked access based on your browser's signature.</html>`, true, false},
		{"waf json", 403, `{"error":{"message":"Forbidden browser signature"}}`, true, false},
		{"plan", 403, `{"error":{"code":"MODEL_NOT_IN_PLAN"}}`, false, false},
		{"credentials", 401, `{"error":{"code":"UNAUTHORIZED","message":"Invalid API key"}}`, false, true},
		{"unconfirmed 401", 401, `site owner has blocked access`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &openAIAuthPolicyAccountRepo{}
			rates := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			svc := &OpenAIGatewayService{rateLimitService: rates}
			rates.SetAccountRuntimeBlocker(svc)
			account := commandCodeTestAccount()
			account.Schedulable = true
			body := []byte(tc.body)
			require.Equal(t, tc.transient, isCommandCodeTransientUpstreamError(tc.status, body))
			if tc.transient {
				failure := svc.failoverOpenAIUpstreamHTTPError(context.Background(), nil, account, &http.Response{StatusCode: tc.status}, body, tc.body, "model")
				require.NotNil(t, failure)
				require.True(t, failure.RequestScopedTransient)
				require.True(t, failure.RetryableOnSameAccount)
				require.Equal(t, GatewayFailureScopeRequest, failure.Scope)
				require.Equal(t, 502, failure.ClientStatusCode)
			}
			disabled := svc.handleOpenAIAccountUpstreamError(context.Background(), account, tc.status, http.Header{}, body)
			require.Equal(t, tc.disabled, disabled)
			if tc.disabled {
				require.Equal(t, 1, repo.setErrorCalls)
			} else {
				require.Zero(t, repo.setErrorCalls)
				require.Zero(t, repo.tempCalls)
				require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
				require.Equal(t, StatusActive, account.Status)
				require.True(t, account.Schedulable)
			}
		})
	}
	require.False(t, isCommandCodeTransientUpstreamError(403, []byte(`{"error":{"message":"permission denied"}}`)))
}
