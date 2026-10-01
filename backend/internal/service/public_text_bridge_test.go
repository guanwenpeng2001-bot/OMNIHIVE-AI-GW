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

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// These fixtures use real HTTP locally; no vendor host or credential is contacted.
type publicTextHTTP struct{ client *http.Client }

func (u publicTextHTTP) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.client.Do(req)
}
func (u publicTextHTTP) DoWithTLS(req *http.Request, proxy string, account int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, account, concurrency)
}

func TestPublicMistralTextBridgeHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{"messages", "responses"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", protocol, stream), func(t *testing.T) {
				var upstreamBody []byte
				var upstreamHeader http.Header
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					upstreamBody, _ = io.ReadAll(r.Body)
					upstreamHeader = r.Header.Clone()
					if r.URL.Path != "/v1/chat/completions" {
						http.Error(w, "invented vendor endpoint", http.StatusNotFound)
						return
					}
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(w, "data: {\"id\":\"chat_fixture\",\"model\":\"native-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call_123456789\",\"type\":\"function\",\"function\":{\"name\":\"weather\",\"arguments\":\"{\\\"city\\\":\\\"Paris\\\"}\"}}]},\"finish_reason\":null}]}\n\n")
						fmt.Fprint(w, "data: {\"id\":\"chat_fixture\",\"model\":\"native-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3,\"total_tokens\":10}}\n\ndata: [DONE]\n\n")
					} else {
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprint(w, `{"id":"chat_fixture","model":"native-model","choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"call_123456789","type":"function","function":{"name":"weather","arguments":"{\"city\":\"Paris\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`)
					}
				}))
				defer server.Close()
				account := rawChatCompletionsTestAccount()
				account.Platform = PlatformMistral
				account.Credentials["base_url"] = server.URL
				account.Credentials["model_mapping"] = map[string]any{"public-model": "native-model"}
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: publicTextHTTP{server.Client()}}
				body := []byte(fmt.Sprintf(`{"model":"public-model","max_tokens":100,"stream":%v,"messages":[{"role":"user","content":"weather?"},{"role":"assistant","content":[{"type":"tool_use","id":"call_prior","name":"weather","input":{"city":"Rome"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_prior","content":"sunny"}]}],"tools":[{"name":"weather","input_schema":{"type":"object","properties":{"city":{"type":"string"}}}}]}`, stream))
				if protocol == "responses" {
					body = []byte(fmt.Sprintf(`{"model":"public-model","stream":%v,"input":[{"role":"user","content":"weather?"},{"type":"function_call","call_id":"call_prior","name":"weather","arguments":"{\"city\":\"Rome\"}"},{"type":"function_call_output","call_id":"call_prior","output":"sunny"}],"tools":[{"type":"function","name":"weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}]}`, stream))
				}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+protocol, bytes.NewReader(body))
				c.Request.Header.Set("Authorization", "Bearer caller-secret")
				c.Request.Header.Set("x-api-key", "caller-anthropic-secret")
				c.Request.Header.Set("x-goog-api-key", "caller-google-secret")
				var result *OpenAIForwardResult
				var err error
				if protocol == "messages" {
					result, err = svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
				} else {
					result, err = svc.Forward(context.Background(), c, account, body)
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, http.StatusOK, recorder.Code)
				require.Equal(t, "native-model", gjson.GetBytes(upstreamBody, "model").String())
				require.False(t, gjson.GetBytes(upstreamBody, "stream_options").Exists())
				require.Equal(t, "function", gjson.GetBytes(upstreamBody, "tools.0.type").String())
				require.Contains(t, string(upstreamBody), `"tool_call_id":"call_prior"`)
				require.Equal(t, "Bearer sk-test", upstreamHeader.Get("Authorization"))
				require.Empty(t, upstreamHeader.Get("x-api-key"))
				require.Empty(t, upstreamHeader.Get("x-goog-api-key"))
				require.Equal(t, "public-model", result.Model)
				require.Equal(t, 7, result.Usage.InputTokens)
				require.Equal(t, 3, result.Usage.OutputTokens)
				require.Contains(t, recorder.Body.String(), "public-model")
				require.Contains(t, recorder.Body.String(), "weather")
				require.Contains(t, recorder.Body.String(), "Paris")
				if stream {
					terminal := "message_stop"
					if protocol == "responses" {
						terminal = "response.completed"
					}
					require.Contains(t, recorder.Body.String(), terminal)
				}
			})
		}
	}
}

func TestPublicPlanProtocolBases(t *testing.T) {
	for _, platform := range []string{PlatformVolcengine, PlatformQianfan} {
		t.Run(platform, func(t *testing.T) {
			account := &Account{Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "account_mode": AccountModeCoding, "api_protocol": APIProtocolAnthropic}}
			want := DefaultVolcengineCodingAnthropicBaseURL
			if platform == PlatformQianfan {
				want = DefaultQianfanCodingAnthropicBaseURL
			}
			require.Equal(t, want, account.GetAnthropicProtocolBaseURL())
			account.Credentials["base_url"] = "https://relay.invalid/plan"
			require.Equal(t, "https://relay.invalid/plan", account.GetAnthropicProtocolBaseURL())
			account.Credentials["api_protocol"] = APIProtocolAdaptive
			account.Credentials["api_base_urls"] = map[string]any{APIProtocolAnthropic: "https://relay.invalid/native", APIProtocolResponses: "https://relay.invalid/responses-root"}
			require.Equal(t, "https://relay.invalid/native", account.GetAnthropicProtocolBaseURL())
			require.False(t, shouldForwardOpenAIResponsesViaRawChatCompletions(account))
			delete(account.Credentials, "api_base_urls")
			require.True(t, shouldForwardOpenAIResponsesViaRawChatCompletions(account), "plan adaptive uses Chat unless a Responses base is configured")
			account.Credentials["account_mode"] = AccountModePayG
			require.Empty(t, account.GetAnthropicProtocolBaseURL(), "no invented pay-as-you-go Messages URL")
			require.False(t, shouldForwardOpenAIResponsesViaRawChatCompletions(account))
			require.Equal(t, "https://relay.invalid/plan", account.GetCNProtocolBaseURL(APIProtocolResponses))
			account.Credentials["api_protocol"] = APIProtocolChatCompletions
			require.True(t, shouldForwardOpenAIResponsesViaRawChatCompletions(account))
			require.True(t, strings.HasPrefix(account.GetOpenAIBaseURL(), "https://relay.invalid/"))
		})
	}
}

func TestPublicMistralErrorsAndBeforeContentFailoverHTTP(t *testing.T) {
	for _, protocol := range []string{"messages", "responses"} {
		for _, status := range []int{http.StatusBadRequest, http.StatusTooManyRequests} {
			t.Run(fmt.Sprintf("%s/%d", protocol, status), func(t *testing.T) {
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					w.Header().Set("Content-Type", "application/json")
					if r.Header.Get("Authorization") == "Bearer first-account" {
						w.WriteHeader(status)
						fmt.Fprint(w, `{"error":{"type":"invalid_request_error","message":"fixture upstream error"}}`)
						return
					}
					fmt.Fprint(w, `{"id":"chat_ok","model":"public-model","choices":[{"index":0,"message":{"role":"assistant","content":"second account"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
				}))
				defer server.Close()
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: publicTextHTTP{server.Client()}}
				body := []byte(`{"model":"public-model","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`)
				if protocol == "responses" {
					body = []byte(`{"model":"public-model","input":"hello"}`)
				}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+protocol, bytes.NewReader(body))
				forward := func(account *Account) error {
					if protocol == "messages" {
						_, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
						return err
					}
					_, err := svc.Forward(context.Background(), c, account, body)
					return err
				}
				first := &Account{ID: 1, Platform: PlatformMistral, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "first-account", "base_url": server.URL}}
				second := &Account{ID: 2, Platform: PlatformMistral, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "second-account", "base_url": server.URL}}
				err := forward(first)
				require.Error(t, err)
				var failover *UpstreamFailoverError
				if status == http.StatusTooManyRequests {
					require.ErrorAs(t, err, &failover)
					require.False(t, c.Writer.Written())
					require.Empty(t, recorder.Body.String())
					require.NoError(t, forward(second))
					require.Equal(t, 2, calls)
					require.Contains(t, recorder.Body.String(), "second account")
				} else {
					require.False(t, errors.As(err, &failover))
					require.Equal(t, 1, calls)
					require.Equal(t, status, recorder.Code)
					require.NotEmpty(t, gjson.Get(recorder.Body.String(), "error.message").String())
					if protocol == "messages" {
						require.Equal(t, "error", gjson.Get(recorder.Body.String(), "type").String())
					}
				}
			})
		}
	}
}

func TestPublicPlanAndPayGMessagesHTTP(t *testing.T) {
	for _, platform := range []string{PlatformVolcengine, PlatformQianfan} {
		for _, mode := range []string{AccountModeCoding, AccountModePayG} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/stream=%v", platform, mode, stream), func(t *testing.T) {
					var path string
					var headers http.Header
					var sent []byte
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						path, headers = r.URL.Path, r.Header.Clone()
						sent, _ = io.ReadAll(r.Body)
						if mode == AccountModeCoding {
							if stream {
								w.Header().Set("Content-Type", "text/event-stream")
								fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_plan\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"native-model\",\"content\":[],\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"plan answer\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
							} else {
								w.Header().Set("Content-Type", "application/json")
								fmt.Fprint(w, `{"id":"msg_plan","type":"message","role":"assistant","model":"native-model","content":[{"type":"text","text":"plan answer"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":2}}`)
							}
						} else {
							w.Header().Set("Content-Type", "text/event-stream")
							fmt.Fprint(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_payg\",\"model\":\"native-model\",\"status\":\"in_progress\"}}\n\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"content_index\":0,\"delta\":\"payg answer\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_payg\",\"object\":\"response\",\"model\":\"native-model\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"id\":\"msg_1\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"payg answer\"}]}],\"usage\":{\"input_tokens\":5,\"output_tokens\":2,\"total_tokens\":7}}}\n\ndata: [DONE]\n\n")
						}
					}))
					defer server.Close()
					account := &Account{ID: 1, Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "account-key", "base_url": server.URL + "/custom", "account_mode": mode, "model_mapping": map[string]any{"public-model": "native-model"}}}
					if mode == AccountModeCoding {
						account.Credentials["api_protocol"] = APIProtocolAnthropic
					}
					body := []byte(fmt.Sprintf(`{"model":"public-model","max_tokens":32,"stream":%v,"messages":[{"role":"user","content":"hello"}]}`, stream))
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
					c.Request.Header.Set("Authorization", "Bearer caller-secret")
					c.Request.Header.Set("x-api-key", "caller-secret")
					c.Request.Header.Set("x-goog-api-key", "caller-secret")
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: publicTextHTTP{server.Client()}}
					result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
					require.NoError(t, err)
					require.Equal(t, "public-model", result.Model)
					require.Equal(t, 5, result.Usage.InputTokens)
					require.Equal(t, 2, result.Usage.OutputTokens)
					require.Contains(t, recorder.Body.String(), "public-model")
					answer := "payg answer"
					if mode == AccountModeCoding {
						answer = "plan answer"
					}
					require.Contains(t, recorder.Body.String(), answer)
					require.NotContains(t, fmt.Sprint(headers), "caller-secret")
					if mode == AccountModeCoding {
						require.Equal(t, "/custom/v1/messages", path)
						require.True(t, headers.Get("x-api-key") == "account-key" || headers.Get("Authorization") == "Bearer account-key")
						require.True(t, gjson.GetBytes(sent, "messages").IsArray())
					} else {
						require.Equal(t, "/custom/v1/responses", path)
						require.Equal(t, "Bearer account-key", headers.Get("Authorization"))
						require.True(t, gjson.GetBytes(sent, "input").IsArray())
					}
				})
			}
		}
	}
}

func TestPublicMistralStreamFailureAfterContentIsNotReplayable(t *testing.T) {
	for _, protocol := range []string{"messages", "responses"} {
		t.Run(protocol, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "text/event-stream")
				// Deliberately truncate the HTTP entity after visible content.
				w.Header().Set("Content-Length", "10000")
				fmt.Fprint(w, "data: {\"id\":\"chat_partial\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"visible output\"},\"finish_reason\":null}]}\n\n")
				w.(http.Flusher).Flush()
			}))
			defer server.Close()
			account := &Account{ID: 1, Platform: PlatformMistral, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "base_url": server.URL}}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: publicTextHTTP{server.Client()}}
			body := []byte(`{"model":"public-model","max_tokens":32,"stream":true,"messages":[{"role":"user","content":"hello"}]}`)
			if protocol == "responses" {
				body = []byte(`{"model":"public-model","input":"hello","stream":true}`)
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+protocol, bytes.NewReader(body))
			var err error
			if protocol == "messages" {
				_, err = svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
			} else {
				_, err = svc.Forward(context.Background(), c, account, body)
			}
			require.Error(t, err)
			var failover *UpstreamFailoverError
			require.False(t, errors.As(err, &failover))
			require.True(t, c.Writer.Written())
			require.Contains(t, recorder.Body.String(), "visible output")
			require.NotContains(t, recorder.Body.String(), "message_stop")
			require.NotContains(t, recorder.Body.String(), "response.completed")
			require.Equal(t, 1, calls)
		})
	}
}

func TestPublicLocalCountIsMarkedEstimated(t *testing.T) {
	for _, platform := range []string{PlatformVolcengine, PlatformQianfan, PlatformMistral} {
		c, recorder := qianfanActionTestContext(t, http.MethodPost, "/v1/messages/count_tokens")
		account := &Account{Platform: platform, Type: AccountTypeAPIKey}
		err := (&OpenAIGatewayService{}).ForwardCountTokensAsAnthropic(context.Background(), c, account, []byte(`{"model":"unknown-model","messages":[{"role":"user","content":"hello"}]}`), "")
		require.NoError(t, err)
		require.Equal(t, "true", recorder.Header().Get("X-Token-Count-Estimated"))
		require.Positive(t, gjson.Get(recorder.Body.String(), "input_tokens").Int())
	}
}

func TestNativeOpenCodeGoogleColumn(t *testing.T) {
	account := requestPlanGateAccount(PlatformOpenCodeGo, "private-google-model", "generate_content")
	account.Credentials = map[string]any{"account_mode": AccountModeZen, "api_protocol": APIProtocolAdaptive, "model_mapping": map[string]any{"public-google": "private-google-model"}}
	ctx := context.Background()
	// A literal catalog family is sufficient; model names need no Gemini prefix.
	account.Status, account.Schedulable = StatusActive, true
	if !NativeOpenCodeGoogleModel(ctx, &account, "public-google") {
		t.Fatal("native catalog Google column must survive alias resolution")
	}
	require.False(t, NativeOpenCodeGoogleModel(ctx, &account, "unselected"))
	account.Credentials["account_mode"] = AccountModeGo
	require.False(t, NativeOpenCodeGoogleModel(ctx, &account, "public-google"))
	account.Credentials["account_mode"] = AccountModeZen
	account.Extra[UpstreamModelCatalogExtraKey] = AccountModelCatalogSnapshot{Complete: true, DiscoveredModels: []string{"private-google-model"}, EligibleModels: []AccountCatalogModel{{ID: "private-google-model", Endpoints: []string{"chat_completions"}, EvidenceKind: "synthetic-contract"}}}
	require.False(t, NativeOpenCodeGoogleModel(ctx, &account, "public-google"), "a public conversion must not manufacture a native Google column")
}

func TestPublicMistralMissingUsageIsNotBillable(t *testing.T) {
	for _, protocol := range []string{"messages", "responses"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", protocol, stream), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(w, "data: {\"id\":\"chat_fixture\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n")
					} else {
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprint(w, `{"id":"chat_fixture","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`)
					}
				}))
				defer server.Close()
				account := &Account{ID: 1, Platform: PlatformMistral, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "base_url": server.URL}}
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: publicTextHTTP{server.Client()}}
				body := []byte(fmt.Sprintf(`{"model":"public-model","stream":%v,"max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`, stream))
				if protocol == "responses" {
					body = []byte(fmt.Sprintf(`{"model":"public-model","input":"hello","stream":%v}`, stream))
				}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+protocol, bytes.NewReader(body))
				var result *OpenAIForwardResult
				var err error
				if protocol == "messages" {
					result, err = svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
				} else {
					result, err = svc.Forward(context.Background(), c, account, body)
				}
				require.ErrorContains(t, err, "usage is unknown")
				require.Nil(t, result)
				require.NotContains(t, recorder.Body.String(), "message_stop")
				require.NotContains(t, recorder.Body.String(), "response.completed")
				if !stream {
					require.Equal(t, http.StatusBadGateway, recorder.Code)
				}
			})
		}
	}
}
