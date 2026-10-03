//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestZhipuAdaptiveResponsesLegacyBase(t *testing.T) {
	for _, mode := range []string{AccountModePayG, AccountModeCoding} {
		chat := DefaultZhipuPayGBaseURL
		otherPlan := DefaultZhipuCodingBaseURL
		if mode == AccountModeCoding {
			chat, otherPlan = otherPlan, chat
		}
		for _, scenario := range []struct {
			name, base, override, protocol, responses, chat, messages string
		}{
			{name: "missing base", responses: DefaultZhipuResponsesBaseURL, chat: chat, messages: DefaultZhipuAnthropicBaseURL},
			{name: "blank base", base: "  ", responses: DefaultZhipuResponsesBaseURL, chat: chat, messages: DefaultZhipuAnthropicBaseURL},
			{name: "matching plan default", base: chat, responses: DefaultZhipuResponsesBaseURL, chat: chat, messages: DefaultZhipuAnthropicBaseURL},
			{name: "matching plan trailing slash", base: " " + chat + "/ ", responses: DefaultZhipuResponsesBaseURL, chat: chat, messages: DefaultZhipuAnthropicBaseURL},
			{name: "different plan stays explicit", base: otherPlan, responses: otherPlan, chat: otherPlan},
			{name: "different plan trailing slash", base: otherPlan + "/", responses: otherPlan + "/", chat: otherPlan + "/"},
			{name: "custom host", base: "https://relay.example/api/paas/v4/", responses: "https://relay.example/api/paas/v4/", chat: "https://relay.example/api/paas/v4/"},
			{name: "custom official host prefix", base: chat + "/private/", responses: chat + "/private/", chat: chat + "/private/"},
			{name: "responses default", base: DefaultZhipuResponsesBaseURL, responses: DefaultZhipuResponsesBaseURL, chat: chat, messages: DefaultZhipuAnthropicBaseURL},
			{name: "explicit custom override isolates missing protocols", base: chat, override: "https://responses.example/custom/", responses: "https://responses.example/custom/"},
			{name: "explicit same profile override", base: chat, override: chat + "/", responses: chat + "/", chat: chat, messages: DefaultZhipuAnthropicBaseURL},
			{name: "explicit other plan override isolates missing protocols", base: chat, override: otherPlan + "/", responses: otherPlan + "/"},
			{name: "blank responses override", base: chat, override: "  ", responses: DefaultZhipuResponsesBaseURL, chat: chat, messages: DefaultZhipuAnthropicBaseURL},
			{name: "fixed responses common wins", base: chat + "/", override: "https://dormant.example/v1", protocol: APIProtocolResponses, responses: chat + "/"},
			{name: "fixed responses other plan stays explicit", base: otherPlan, protocol: APIProtocolResponses, responses: otherPlan},
		} {
			t.Run(mode+"/"+scenario.name, func(t *testing.T) {
				protocol := scenario.protocol
				if protocol == "" {
					protocol = APIProtocolAdaptive
				}
				account := &Account{Platform: PlatformZhipu, Type: AccountTypeAPIKey, Credentials: map[string]any{
					"account_mode": mode, "api_protocol": protocol,
				}}
				if scenario.base != "" {
					account.Credentials["base_url"] = scenario.base
				}
				if scenario.override != "" {
					account.Credentials["api_base_urls"] = map[string]any{APIProtocolResponses: scenario.override}
				}
				require.Equal(t, scenario.responses, account.GetCNProtocolBaseURL(APIProtocolResponses))
				require.Equal(t, protocol, account.GetAPIProtocol())
				if protocol == APIProtocolAdaptive {
					require.Equal(t, scenario.chat, account.GetCNProtocolBaseURL(APIProtocolChatCompletions))
					require.Equal(t, scenario.messages, account.GetCNProtocolBaseURL(APIProtocolAnthropic))
				}
				if scenario.base != "" {
					require.Equal(t, scenario.base, account.Credentials["base_url"], "resolution must not migrate credentials")
				}
			})
		}
	}
}

func TestProviderProtocolDefaultsAndOverrides(t *testing.T) {
	for _, mode := range []string{AccountModePayG, AccountModeCoding} {
		for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses, APIProtocolAdaptive} {
			for _, platform := range []string{PlatformZhipu, PlatformQianwen} {
				t.Run(platform+"/"+mode+"/"+protocol, func(t *testing.T) {
					account := &Account{Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"account_mode": mode, "api_protocol": protocol}}
					wantChat, wantMessages, wantResponses := DefaultZhipuPayGBaseURL, DefaultZhipuAnthropicBaseURL, DefaultZhipuResponsesBaseURL
					if mode == AccountModeCoding {
						wantChat = DefaultZhipuCodingBaseURL
					}
					if platform == PlatformQianwen {
						wantChat, wantMessages = DefaultQianwenBaseURL, DefaultQianwenAnthropicBaseURL
						if mode == AccountModeCoding {
							wantChat, wantMessages = DefaultQianwenCodingBaseURL, DefaultQianwenCodingAnthropicBaseURL
						}
						wantResponses = wantChat // Existing compatibility behavior; no native Coding Responses claim.
					}
					wantProtocol := protocol
					if platform == PlatformQianwen && mode == AccountModeCoding && protocol == APIProtocolResponses {
						wantProtocol = APIProtocolAdaptive // This plan has no implicit Responses endpoint.
					}
					require.Equal(t, wantProtocol, account.GetAPIProtocol())
					require.Equal(t, wantChat, account.GetCNProtocolBaseURL(APIProtocolChatCompletions))
					require.Equal(t, wantMessages, account.GetCNProtocolBaseURL(APIProtocolAnthropic))
					require.Equal(t, wantResponses, account.GetCNProtocolBaseURL(APIProtocolResponses))
					custom := "https://private.example/custom/prefix/v1"
					account.Credentials["base_url"] = custom
					if protocol == APIProtocolAdaptive {
						account.Credentials["api_base_urls"] = map[string]any{APIProtocolResponses: custom, APIProtocolAnthropic: custom, APIProtocolChatCompletions: custom}
						for _, native := range []string{APIProtocolResponses, APIProtocolAnthropic, APIProtocolChatCompletions} {
							require.Equal(t, custom, account.GetCNProtocolBaseURL(native))
						}
					} else {
						require.Equal(t, custom, account.GetCNProtocolBaseURL(protocol))
					}
					if protocol == APIProtocolAnthropic {
						require.Empty(t, account.GetOpenAIBaseURL(), "a custom Messages-only root does not establish Chat")
					} else {
						require.Equal(t, custom, account.GetOpenAIBaseURL())
					}
				})
			}
		}
	}
}

func TestCommandCodeResponsesSelection(t *testing.T) {
	account := commandCodeTestAccount()
	require.Equal(t, APIProtocolChatCompletions, account.ResolveCommandCodeUpstreamProtocol("private/model"))
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{
		"private/model":  {SupportedMethods: []string{"responses"}},
		"claude-private": {SupportedMethods: []string{"responses"}},
	}})
	require.Equal(t, APIProtocolResponses, account.ResolveCommandCodeUpstreamProtocol("private/model"))
	require.Equal(t, APIProtocolAnthropic, account.ResolveCommandCodeUpstreamProtocol("claude-private"))
	require.Equal(t, APIProtocolChatCompletions, account.ResolveCommandCodeUpstreamProtocol("missing"))
	account.Credentials["protocol_rules"] = []any{map[string]any{"pattern": "missing", "protocol": APIProtocolResponses}}
	require.Equal(t, APIProtocolResponses, account.ResolveCommandCodeUpstreamProtocol("missing"))
	account.Credentials["api_protocol"] = APIProtocolResponses
	require.Equal(t, APIProtocolResponses, account.ResolveCommandCodeUpstreamProtocol("private/undiscovered"))
}

func TestProviderNativeResponsesForward(t *testing.T) {
	for _, platform := range []string{PlatformZhipu, PlatformCommandCode} {
		for _, selection := range []string{"explicit", "adaptive", "metadata"} {
			for _, terminal := range []string{"response.completed", "response.failed", "response.incomplete", "error"} {
				t.Run(platform+"/"+selection+"/"+terminal, func(t *testing.T) {
					account := &Account{ID: 905, Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{
						"api_key": "fixture-key", "api_protocol": APIProtocolResponses, "model_mapping": map[string]any{"public": "private/exact-model"},
					}}
					if selection != "explicit" {
						account.Credentials["api_protocol"] = APIProtocolAdaptive
						if platform == PlatformCommandCode && selection == "adaptive" {
							account.Credentials["protocol_rules"] = []any{map[string]any{"pattern": "private/*", "protocol": APIProtocolResponses}}
						}
					}
					if selection == "metadata" && platform == PlatformCommandCode {
						account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{"private/exact-model": {SupportedMethods: []string{"responses"}}, "public": {SupportedMethods: []string{"chat_completions"}}}})
					}
					body := []byte(`{"model":"public","stream":true,"input":"hi"}`)
					terminalBody := fmt.Sprintf(`{"type":%q,"response":{"id":"fixture-response","status":%q,"model":"private/exact-model","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"fixture complete body"}]}],"usage":{"input_tokens":2,"output_tokens":3}}}`, terminal, strings.TrimPrefix(terminal, "response."))
					if terminal == "response.failed" || terminal == "error" {
						terminalBody = fmt.Sprintf(`{"type":%q,"error":{"code":"fixture_error","message":"upstream fixture rejection"},"response":{"status":"failed","error":{"code":"fixture_error","message":"upstream fixture rejection"}}}`, terminal)
					}
					stream := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"fixture complete body\"}\n\n" + "data: " + terminalBody + "\n\n"
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream))}}
					recorder := httptest.NewRecorder()
					ctx, _ := gin.CreateTestContext(recorder)
					ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
					SetOpenAIClientTransport(ctx, OpenAIClientTransportHTTP)
					result, err := openAIClientToolsTestService(upstream).Forward(context.Background(), ctx, account, body)
					if terminal == "response.failed" || terminal == "error" {
						require.Error(t, err)
					} else {
						require.NoError(t, err)
						require.NotNil(t, result)
						require.Equal(t, 2, result.Usage.InputTokens)
						require.Equal(t, 3, result.Usage.OutputTokens)
					}
					require.NotNil(t, upstream.lastReq)
					expected := DefaultZhipuResponsesBaseURL + "/responses"
					if platform == PlatformCommandCode {
						expected = DefaultCommandCodeBaseURL + "/responses"
					}
					require.Equal(t, expected, upstream.lastReq.URL.String())
					require.Equal(t, "Bearer fixture-key", upstream.lastReq.Header.Get("Authorization"))
					require.Equal(t, "private/exact-model", gjson.GetBytes(upstream.lastBody, "model").String())
					require.Contains(t, recorder.Body.String(), terminal)
					require.Contains(t, recorder.Body.String(), "fixture complete body")
					require.NotContains(t, recorder.Body.String(), "[DONE]")
				})
			}
		}
	}
}

func TestCommandCodeResponsesUpstreamRejections(t *testing.T) {
	for _, failure := range []struct {
		status int
		code   string
	}{{400, "invalid_request_error"}, {403, "upgrade_required"}, {403, "MODEL_NOT_IN_PLAN"}} {
		t.Run(failure.code, func(t *testing.T) {
			status := failure.status
			account := commandCodeTestAccount()
			account.Credentials["api_protocol"] = APIProtocolResponses
			body := []byte(`{"model":"private/exact-model","input":"hi"}`)
			payload := `{"error":{"type":"invalid_request_error","message":"actual upstream rejection"}}`
			if status == 403 {
				payload = fmt.Sprintf(`{"error":{"code":%q,"message":"actual upstream rejection"}}`, failure.code)
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(payload))}}
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			SetOpenAIClientTransport(ctx, OpenAIClientTransportHTTP)
			_, err := openAIClientToolsTestService(upstream).Forward(context.Background(), ctx, account, body)
			require.Error(t, err)
			require.Equal(t, DefaultCommandCodeBaseURL+"/responses", upstream.lastReq.URL.String())
			require.Equal(t, status, recorder.Code)
			require.Contains(t, recorder.Body.String(), "actual upstream rejection")
		})
	}
}

func TestQianwenCodingPlanForwarding(t *testing.T) {
	for _, ingress := range []string{"chat", "messages", "responses"} {
		t.Run(ingress, func(t *testing.T) {
			account := &Account{ID: 905, Platform: PlatformQianwen, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-coding-key", "account_mode": AccountModeCoding, "api_protocol": APIProtocolAdaptive, "model_mapping": map[string]any{"public": "private/exact-model"}}}
			body := []byte(`{"model":"public","max_tokens":32,"messages":[{"role":"user","content":"hi"}]}`)
			response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"fixture","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"coding fixture"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`))}
			expected := DefaultQianwenCodingBaseURL + "/chat/completions"
			if ingress == "messages" {
				response = nativeAnthropicBufferedResponse()
				expected = DefaultQianwenCodingAnthropicBaseURL + "/v1/messages"
			}
			if ingress == "responses" {
				body = []byte(`{"model":"public","input":"hi"}`)
			}
			upstream := &httpUpstreamRecorder{resp: response}
			gateway := openAIClientToolsTestService(upstream)
			ctx := adaptiveProtocolTestContext("/v1/"+ingress, body)
			var err error
			switch ingress {
			case "chat":
				_, err = gateway.ForwardAsChatCompletions(context.Background(), ctx, account, body, "", "")
			case "messages":
				_, err = gateway.ForwardAsAnthropic(context.Background(), ctx, account, body, "", "")
			case "responses":
				SetOpenAIClientTransport(ctx, OpenAIClientTransportHTTP)
				_, err = gateway.Forward(context.Background(), ctx, account, body)
			}
			require.NoError(t, err)
			require.Equal(t, expected, upstream.lastReq.URL.String())
			require.Equal(t, "private/exact-model", gjson.GetBytes(upstream.lastBody, "model").String())
			require.True(t, gjson.GetBytes(upstream.lastBody, "messages").IsArray())
		})
	}
}

func TestProviderCustomResponsesEndpointPreserved(t *testing.T) {
	for _, platform := range []string{PlatformZhipu, PlatformCommandCode, PlatformQianwen} {
		for _, protocol := range []string{APIProtocolResponses, APIProtocolAdaptive} {
			t.Run(platform+"/"+protocol, func(t *testing.T) {
				account := &Account{ID: 905, Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key", "account_mode": AccountModeCoding, "api_protocol": protocol, "base_url": "https://private.example/prefix/v1", "protocol_rules": []any{map[string]any{"pattern": "*", "protocol": APIProtocolResponses}}}}
				if platform == PlatformQianwen {
					account.Credentials["api_base_urls"] = map[string]any{APIProtocolResponses: "https://private.example/prefix/v1"}
				}
				body := []byte(`{"model":"private/exact-model","input":"hi"}`)
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"fixture","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"complete body"}]}],"usage":{"input_tokens":2,"output_tokens":1}}`))}}
				recorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(recorder)
				ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				SetOpenAIClientTransport(ctx, OpenAIClientTransportHTTP)
				_, err := openAIClientToolsTestService(upstream).Forward(context.Background(), ctx, account, body)
				require.NoError(t, err)
				require.Equal(t, "https://private.example/prefix/v1/responses", upstream.lastReq.URL.String())
				require.Contains(t, recorder.Body.String(), "complete body")
			})
		}
	}
}

func TestProviderResponsesTransportFailure(t *testing.T) {
	for _, platform := range []string{PlatformCommandCode, PlatformZhipu} {
		t.Run(platform, func(t *testing.T) {
			account := &Account{ID: 905, Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key", "api_protocol": APIProtocolResponses}}
			upstream := &httpUpstreamRecorder{err: errors.New("fixture connection reset")}
			body := []byte(`{"model":"private/exact-model","input":"hi"}`)
			ctx := adaptiveProtocolTestContext("/v1/responses", body)
			SetOpenAIClientTransport(ctx, OpenAIClientTransportHTTP)
			_, err := openAIClientToolsTestService(upstream).Forward(context.Background(), ctx, account, body)
			require.Error(t, err)
			var failure *UpstreamFailoverError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, http.StatusBadGateway, failure.StatusCode)
			require.Contains(t, upstream.lastReq.URL.Path, "/responses")
		})
	}
}

func TestAdaptiveResponsesAccountModeCompatibility(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformQianwen} {
		for _, mode := range []string{AccountModePayG, AccountModeCoding} {
			account := &Account{Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_protocol": APIProtocolAdaptive, "account_mode": mode}}
			require.Equal(t, platform == PlatformQianwen && mode == AccountModeCoding, shouldForwardOpenAIResponsesViaRawChatCompletions(account), platform+"/"+mode)
		}
	}
}

func TestCommandCodeNativeResponsesAllIngresses(t *testing.T) {
	for _, ingress := range []string{"chat", "messages", "responses"} {
		for _, choice := range []string{"explicit", "rule", "metadata"} {
			t.Run(ingress+"/"+choice, func(t *testing.T) {
				account := commandCodeTestAccount()
				account.Credentials["model_mapping"] = map[string]any{"public": "private/exact-model"}
				switch choice {
				case "explicit":
					account.Credentials["api_protocol"] = APIProtocolResponses
				case "rule":
					account.Credentials["protocol_rules"] = []any{map[string]any{"pattern": "private/*", "protocol": APIProtocolResponses}}
				case "metadata":
					_, metadata, err := extractUpstreamModelCatalog([]byte(`{"data":[{"id":"private/exact-model","supported_endpoints":["/v1/responses"]},{"id":"public","supported_endpoints":["/v1/chat/completions"]}]}`), false, PlatformCommandCode)
					require.NoError(t, err)
					account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: metadata})
				}
				body := []byte(`{"model":"public","max_tokens":32,"messages":[{"role":"user","content":"hi"}]}`)
				if ingress == "responses" {
					body = []byte(`{"model":"public","input":"hi"}`)
				}
				stream := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"fixture\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"native complete body\"}]}],\"usage\":{\"input_tokens\":2,\"output_tokens\":1}}}\n\n"
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream))}}
				gateway := openAIClientToolsTestService(upstream)
				recorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(recorder)
				ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/"+ingress, bytes.NewReader(body))
				var err error
				switch ingress {
				case "chat":
					_, err = gateway.ForwardAsChatCompletions(context.Background(), ctx, account, body, "", "")
				case "messages":
					_, err = gateway.ForwardAsAnthropic(context.Background(), ctx, account, body, "", "")
				case "responses":
					SetOpenAIClientTransport(ctx, OpenAIClientTransportHTTP)
					_, err = gateway.Forward(context.Background(), ctx, account, body)
				}
				require.NoError(t, err)
				require.Equal(t, DefaultCommandCodeBaseURL+"/responses", upstream.lastReq.URL.String())
				require.Equal(t, "private/exact-model", gjson.GetBytes(upstream.lastBody, "model").String())
				require.Contains(t, recorder.Body.String(), "native complete body")
			})
		}
	}
}

func TestCommandCodeSupportedEndpointsDiscovery(t *testing.T) {
	for _, entry := range []struct {
		name, field string
		methods     []string
		invalid     bool
	}{
		{"paths", `"supported_endpoints":["/provider/v1/responses","/v1/chat/completions","/messages","/responses"]`, []string{"responses", "chat_completions", "messages"}, false},
		{"names", `"supported_endpoints":["responses","chat_completions","messages"]`, []string{"responses", "chat_completions", "messages"}, false},
		{"missing", `"display_name":"Private fixture"`, nil, false},
		{"null", `"supported_endpoints":null`, nil, false},
		{"malformed", `"supported_endpoints":{"responses":true}`, nil, true},
		{"mixed malformed", `"supported_endpoints":["responses",42]`, nil, true},
		{"empty", `"supported_endpoints":[]`, []string{}, false},
		{"unknown path", `"supported_endpoints":["/custom/responses"]`, []string{"/custom/responses"}, false},
	} {
		t.Run(entry.name, func(t *testing.T) {
			body := []byte(`{"data":[{"id":"Private/Exact-Model",` + entry.field + `,"context_length":4096}]}`)
			ids, metadata, err := extractUpstreamModelCatalog(body, false, PlatformCommandCode)
			require.NoError(t, err)
			require.Equal(t, []string{"Private/Exact-Model"}, ids)
			require.Equal(t, entry.methods, metadata[ids[0]].SupportedMethods)
			require.EqualValues(t, 4096, metadata[ids[0]].ContextWindow)
			if entry.invalid {
				require.Contains(t, metadata[ids[0]].Conflicts, "invalid_optional_metadata")
			}
			account := commandCodeTestAccount()
			account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: metadata})
			if entry.name == "paths" || entry.name == "names" {
				require.Equal(t, APIProtocolResponses, account.ResolveCommandCodeUpstreamProtocol(ids[0]))
			} else {
				require.Equal(t, APIProtocolChatCompletions, account.ResolveCommandCodeUpstreamProtocol(ids[0]))
			}
			account.Credentials["api_protocol"] = APIProtocolResponses
			require.Equal(t, APIProtocolResponses, account.ResolveCommandCodeUpstreamProtocol(ids[0]))
		})
	}
}
