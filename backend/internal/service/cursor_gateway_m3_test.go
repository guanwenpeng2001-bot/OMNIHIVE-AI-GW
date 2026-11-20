//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/cursor"
	"github.com/stretchr/testify/require"
)

func cursorProtocolRequest(protocol string, stream bool) []byte {
	switch protocol {
	case APIProtocolAnthropic:
		return []byte(fmt.Sprintf(`{"model":"default","stream":%t,"max_tokens":100,"system":"instruction","messages":[{"role":"user","content":"hi"}]}`, stream))
	case APIProtocolResponses:
		return []byte(fmt.Sprintf(`{"model":"default","stream":%t,"instructions":"instruction","input":"hi"}`, stream))
	default:
		return []byte(fmt.Sprintf(`{"model":"default","stream":%t,"stream_options":{"include_usage":true},"messages":[{"role":"system","content":"instruction"},{"role":"user","content":"hi"}]}`, stream))
	}
}
func callCursorProtocol(gateway *CursorGatewayService, requestContext *gin.Context, account *Account, body []byte, protocol string) (*ForwardResult, error) {
	switch protocol {
	case APIProtocolAnthropic:
		return gateway.ForwardAsAnthropic(context.Background(), requestContext, account, body)
	case APIProtocolResponses:
		return gateway.ForwardAsResponses(context.Background(), requestContext, account, body)
	default:
		return gateway.ForwardAsChatCompletions(context.Background(), requestContext, account, body)
	}
}
func cursorRelayFixture(protocol string, stream bool) string {
	chatUsage := `{"prompt_tokens":20,"completion_tokens":5,"total_tokens":25,"prompt_tokens_details":{"cached_tokens":6,"cache_write_tokens":2},"completion_tokens_details":{"reasoning_tokens":3}}`
	anthropicUsage := `{"input_tokens":12,"output_tokens":5,"cache_read_input_tokens":6,"cache_creation_input_tokens":2,"estimated":false}`
	responsesUsage := `{"input_tokens":20,"output_tokens":5,"total_tokens":25,"input_tokens_details":{"cached_tokens":6,"cache_write_tokens":2},"output_tokens_details":{"reasoning_tokens":3}}`
	response := `{"id":"resp_fixture","object":"response","status":"completed","model":"default","output":[{"id":"reason_fixture","type":"reasoning","summary":[{"type":"summary_text","text":"why"}]},{"id":"msg_fixture","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"hello","annotations":[]}]}],"usage":` + responsesUsage + `}`
	if !stream {
		switch protocol {
		case APIProtocolAnthropic:
			return `{"id":"msg_fixture","type":"message","role":"assistant","model":"default","content":[{"type":"thinking","thinking":"why"},{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":` + anthropicUsage + `}`
		case APIProtocolResponses:
			return response
		default:
			return `{"id":"chatcmpl_fixture","object":"chat.completion","model":"default","choices":[{"index":0,"message":{"role":"assistant","content":"hello","reasoning_content":"why"},"finish_reason":"stop"}],"usage":` + chatUsage + `}`
		}
	}
	event := func(name, data string) string { return "event: " + name + "\ndata: " + data + "\n\n" }
	switch protocol {
	case APIProtocolAnthropic:
		return event("message_start", `{"type":"message_start","message":{"id":"msg_fixture","type":"message","role":"assistant","model":"default","content":[],"usage":{"input_tokens":12,"output_tokens":0,"cache_read_input_tokens":6,"cache_creation_input_tokens":2}}}`) +
			event("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`) +
			event("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"why"}}`) +
			event("content_block_stop", `{"type":"content_block_stop","index":0}`) +
			event("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`) +
			event("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"hello"}}`) +
			event("content_block_stop", `{"type":"content_block_stop","index":1}`) +
			event("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`) +
			event("message_stop", `{"type":"message_stop"}`)
	case APIProtocolResponses:
		return event("response.created", `{"type":"response.created","response":{"id":"resp_fixture","object":"response","status":"in_progress","model":"default","output":[]}}`) +
			event("response.reasoning_summary_text.delta", `{"type":"response.reasoning_summary_text.delta","item_id":"reason_fixture","output_index":0,"summary_index":0,"delta":"why"}`) +
			event("response.output_text.delta", `{"type":"response.output_text.delta","item_id":"msg_fixture","output_index":1,"content_index":0,"delta":"hello"}`) +
			event("response.completed", `{"type":"response.completed","response":`+response+`}`)
	default:
		return "data: {\n" + "data: \"id\":\"chatcmpl_fixture\",\"model\":\"default\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"why\"},\"finish_reason\":null}]}\n\n" +
			"data: {\"id\":\"chatcmpl_fixture\",\"model\":\"default\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n" +
			"data: {\"id\":\"chatcmpl_fixture\",\"model\":\"default\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: {\"id\":\"chatcmpl_fixture\",\"choices\":[],\"usage\":" + chatUsage + "}\n\ndata: [DONE]\n\n"
	}
}

func TestCursorRelayThreeProtocols(t *testing.T) {
	protocols := []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses}
	for _, inbound := range protocols {
		for _, configured := range append([]string{APIProtocolAdaptive}, protocols...) {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%t", inbound, configured, stream), func(t *testing.T) {
					upstream := configured
					if upstream == APIProtocolAdaptive {
						upstream = inbound
					}
					endpoint := map[string]string{APIProtocolChatCompletions: "/v1/chat/completions", APIProtocolAnthropic: "/v1/messages", APIProtocolResponses: "/v1/responses"}[upstream]
					calls := 0
					server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
						calls++
						require.Equal(t, endpoint, request.URL.Path)
						require.Equal(t, "Bearer synthetic-relay-key", request.Header.Get("Authorization"))
						require.Empty(t, request.Header.Get("X-Cursor-Checksum"))
						var payload map[string]any
						require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
						require.Equal(t, "mapped-model", payload["model"])
						require.Equal(t, stream, payload["stream"])
						if upstream == APIProtocolChatCompletions {
							require.NotEmpty(t, payload["messages"])
						}
						if upstream == APIProtocolResponses {
							require.NotEmpty(t, payload["input"])
						}
						if upstream == APIProtocolAnthropic {
							require.NotEmpty(t, payload["messages"])
						}
						if stream {
							writer.Header().Set("Content-Type", "text/event-stream")
						} else {
							writer.Header().Set("Content-Type", "application/json")
						}
						_, _ = fmt.Fprint(writer, cursorRelayFixture(upstream, stream))
					}))
					defer server.Close()
					account := &Account{Type: AccountTypeUpstream, Platform: PlatformCursor, Credentials: map[string]any{"base_url": "http://invalid.example", "api_base_urls": map[string]any{upstream: server.URL}, "api_protocol": configured, "api_key": "synthetic-relay-key", "model_mapping": map[string]any{"default": "mapped-model"}}}
					gateway := NewCursorGatewayService(nil, nil)
					gateway.relayClient = server.Client()
					recorder := httptest.NewRecorder()
					requestContext, _ := gin.CreateTestContext(recorder)
					result, err := callCursorProtocol(gateway, requestContext, account, cursorProtocolRequest(inbound, stream), inbound)
					require.NoError(t, err)
					require.Equal(t, 1, calls)
					require.Equal(t, 200, recorder.Code)
					require.Equal(t, ClaudeUsage{InputTokens: 12, OutputTokens: 5, CacheReadInputTokens: 6, CacheCreationInputTokens: 2}, result.Usage)
					require.Contains(t, recorder.Body.String(), "hello")
					require.Contains(t, recorder.Body.String(), "why")
					if stream {
						terminal := map[string]string{APIProtocolChatCompletions: "data: [DONE]", APIProtocolAnthropic: "event: message_stop", APIProtocolResponses: "event: response.completed"}[inbound]
						require.Equal(t, 1, strings.Count(recorder.Body.String(), terminal))
					}
				})
			}
		}
	}
}

type cursorCatalogDriver struct {
	cursorDriverStub
	calls     int
	available []cursor.AvailableModel
}

func (driver *cursorCatalogDriver) AvailableModels(context.Context, cursor.Credentials) ([]cursor.AvailableModel, error) {
	driver.calls++
	if driver.available == nil {
		return nil, fmt.Errorf("offline")
	}
	return driver.available, nil
}
func TestCursorCatalogFailureAndRecovery(t *testing.T) {
	gateway, account, _ := cursorTestGateway()
	driver := &cursorCatalogDriver{}
	gateway.native = driver
	expected := append(CursorPresetModels(), cursor.AvailableModel{ID: cursor.AutoModelID, RunSlug: cursor.AutoModelID})
	require.Equal(t, expected, gateway.picker(context.Background(), account, cursor.Credentials{}))
	driver.available = []cursor.AvailableModel{{ID: "new-live-model", RunSlug: "new-live-run"}}
	require.Equal(t, expected, gateway.picker(context.Background(), account, cursor.Credentials{}))
	require.Equal(t, 1, driver.calls)
	key := cursorSessionKey(account)
	cached := gateway.catalog[key]
	cached.expires = time.Time{}
	gateway.catalog[key] = cached
	require.Equal(t, driver.available, gateway.picker(context.Background(), account, cursor.Credentials{}))
	require.Equal(t, 2, driver.calls)
	require.Equal(t, driver.available, gateway.picker(context.Background(), account, cursor.Credentials{}))
	require.Equal(t, 2, driver.calls)
}
func TestCursorEstimatedUsageAllProtocols(t *testing.T) {
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", protocol, stream), func(t *testing.T) {
				gateway, account, _ := cursorTestGateway()
				gateway.native = cursorDriverStub{run: func(_ context.Context, _ cursor.Credentials, _ []cursor.ChatMessage, _ string, emit func(cursor.StreamEvent) error) (cursor.TokenUsage, error) {
					require.NoError(t, emit(cursor.StreamEvent{Type: "thinking", Text: "why"}))
					require.NoError(t, emit(cursor.StreamEvent{Type: "text", Text: "hello"}))
					return cursor.TokenUsage{OutputTokens: 9, Estimated: true}, nil
				}}
				recorder := httptest.NewRecorder()
				requestContext, _ := gin.CreateTestContext(recorder)
				result, err := callCursorProtocol(gateway, requestContext, account, cursorProtocolRequest(protocol, stream), protocol)
				require.NoError(t, err)
				require.Equal(t, 9, result.Usage.OutputTokens)
				require.Positive(t, result.Usage.InputTokens)
				require.Contains(t, recorder.Body.String(), `"estimated":true`)
				if !stream {
					require.Equal(t, "estimated_missing_turn_ended", recorder.Header().Get("X-OmniHive-Usage-Source"))
					require.Equal(t, "estimated_missing_turn_ended", recorder.Header().Get("X-Sub2API-Usage-Source"))
				}
			})
		}
	}
}

func TestCursorNativeRequestVariantsAndRejections(t *testing.T) {
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses} {
		t.Run(protocol, func(t *testing.T) {
			gateway, account, _ := cursorTestGateway()
			runCalls := 0
			gateway.native = cursorDriverStub{run: func(_ context.Context, _ cursor.Credentials, messages []cursor.ChatMessage, model string, emit func(cursor.StreamEvent) error) (cursor.TokenUsage, error) {
				runCalls++
				require.Contains(t, messages, cursor.ChatMessage{Role: "system", Content: "instruction"})
				require.Equal(t, "claude-opus-5-thinking-high-fast", model)
				require.NoError(t, emit(cursor.StreamEvent{Type: "thinking", Text: "why"}))
				require.NoError(t, emit(cursor.StreamEvent{Type: "text", Text: "hello"}))
				return cursor.TokenUsage{InputTokens: 12, OutputTokens: 5, ReasoningTokens: 3}, nil
			}}
			var request map[string]any
			require.NoError(t, json.Unmarshal(cursorProtocolRequest(protocol, false), &request))
			request["model"] = "claude-opus-5"
			request["fast"] = true
			switch protocol {
			case APIProtocolAnthropic:
				request["thinking"] = map[string]any{"type": "enabled"}
				request["output_config"] = map[string]any{"effort": "high"}
			case APIProtocolResponses:
				request["reasoning"] = map[string]any{"effort": "high"}
			default:
				request["reasoning_effort"] = "high"
				request["thinking"] = true
			}
			encoded, _ := json.Marshal(request)
			recorder := httptest.NewRecorder()
			requestContext, _ := gin.CreateTestContext(recorder)
			_, err := callCursorProtocol(gateway, requestContext, account, encoded, protocol)
			require.NoError(t, err)
			require.Equal(t, 1, runCalls)
			for _, failure := range []string{"tools", "model"} {
				var rejected map[string]any
				require.NoError(t, json.Unmarshal(cursorProtocolRequest(protocol, false), &rejected))
				if failure == "tools" {
					rejected["tools"] = []any{map[string]any{"type": "function", "name": "", "input_schema": map[string]any{"type": "object"}, "function": map[string]any{"name": "", "parameters": map[string]any{"type": "object"}}}}
				} else {
					rejected["model"] = "unknown-no-default-fallback"
				}
				encoded, _ = json.Marshal(rejected)
				recorder = httptest.NewRecorder()
				requestContext, _ = gin.CreateTestContext(recorder)
				_, err = callCursorProtocol(gateway, requestContext, account, encoded, protocol)
				require.Error(t, err)
				if failure == "model" {
					var unavailable *UpstreamFailoverError
					require.ErrorAs(t, err, &unavailable)
					require.True(t, unavailable.ShouldRetryNextAccount())
					require.Contains(t, unavailable.ClientMessage, "unknown model")
					require.False(t, requestContext.Writer.Written())
					require.Equal(t, 1, runCalls)
					continue
				}
				require.Equal(t, 400, recorder.Code)
				var envelope map[string]any
				require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
				require.Equal(t, "invalid_request_error", envelope["error"].(map[string]any)["type"])
				if protocol == APIProtocolAnthropic {
					require.Equal(t, "error", envelope["type"])
				} else {
					require.NotContains(t, envelope, "type")
				}
				require.Equal(t, 1, runCalls)
				if failure == "model" {
					require.Contains(t, recorder.Body.String(), "unknown model")
				}
			}
		})
	}
}

func TestCursorRelayErrorsRespectOutputBoundary(t *testing.T) {
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses} {
		for _, afterOutput := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", protocol, afterOutput), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					writer.Header().Set("Content-Type", "text/event-stream")
					if afterOutput {
						fixture := cursorRelayFixture(protocol, true)
						beforeEnd := strings.LastIndex(fixture, "event: message_stop")
						if protocol == APIProtocolResponses {
							beforeEnd = strings.LastIndex(fixture, "event: response.completed")
						}
						if protocol == APIProtocolChatCompletions {
							beforeEnd = strings.LastIndex(fixture, "data: [DONE]")
						}
						_, _ = fmt.Fprint(writer, fixture[:beforeEnd])
					}
					_, _ = fmt.Fprint(writer, "event: error\ndata: {\"type\":\"error\",\"error\":{\"message\":\"private-upstream-detail\"}}\n\n")
				}))
				defer server.Close()
				gateway := NewCursorGatewayService(nil, nil)
				gateway.relayClient = server.Client()
				account := &Account{Type: AccountTypeUpstream, Credentials: map[string]any{"api_protocol": APIProtocolAdaptive, "base_url": server.URL}}
				recorder := httptest.NewRecorder()
				requestContext, _ := gin.CreateTestContext(recorder)
				result, err := callCursorProtocol(gateway, requestContext, account, cursorProtocolRequest(protocol, true), protocol)
				require.Error(t, err)
				require.NotContains(t, err.Error(), "private-upstream-detail")
				if afterOutput {
					require.NotNil(t, result)
					require.True(t, requestContext.Writer.Written())
					require.NotContains(t, recorder.Body.String(), "[DONE]")
				} else {
					require.Nil(t, result)
					require.False(t, requestContext.Writer.Written())
				}
			})
		}
	}
}

func TestCursorRelayPreservesEstimatedAcrossConversion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = fmt.Fprint(writer, strings.ReplaceAll(cursorRelayFixture(APIProtocolAnthropic, false), `"estimated":false`, `"estimated":true`))
	}))
	defer server.Close()
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolResponses} {
		gateway := NewCursorGatewayService(nil, nil)
		gateway.relayClient = server.Client()
		account := &Account{Type: AccountTypeUpstream, Credentials: map[string]any{"base_url": server.URL, "api_protocol": APIProtocolAnthropic}}
		recorder := httptest.NewRecorder()
		requestContext, _ := gin.CreateTestContext(recorder)
		_, err := callCursorProtocol(gateway, requestContext, account, cursorProtocolRequest(protocol, false), protocol)
		require.NoError(t, err)
		require.Contains(t, recorder.Body.String(), `"estimated":true`)
	}
}

func TestCursorMessagesExplicitVariantOptions(t *testing.T) {
	for _, test := range []struct {
		name, options, model string
	}{
		{"unspecified", "", "claude-opus-5-medium"},
		{"empty_output_config", `,"output_config":{}`, "claude-opus-5-medium"},
		{"explicit_effort", `,"output_config":{"effort":"high"}`, "claude-opus-5-thinking-high"},
		{"thinking_enabled", `,"thinking":{"type":"enabled"}`, "claude-opus-5-thinking-medium"},
		{"thinking_adaptive", `,"thinking":{"type":"adaptive"}`, "claude-opus-5-thinking-medium"},
		{"thinking_disabled_overrides_effort", `,"thinking":{"type":"disabled"},"output_config":{"effort":"high"}`, "claude-opus-5-low"},
	} {
		t.Run(test.name, func(t *testing.T) {
			gateway, account, _ := cursorTestGateway()
			calls := 0
			gateway.native = cursorDriverStub{run: func(_ context.Context, _ cursor.Credentials, _ []cursor.ChatMessage, model string, emit func(cursor.StreamEvent) error) (cursor.TokenUsage, error) {
				calls++
				require.Equal(t, test.model, model)
				require.NoError(t, emit(cursor.StreamEvent{Type: "text", Text: "hello"}))
				return cursor.TokenUsage{InputTokens: 1, OutputTokens: 1}, nil
			}}
			requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
			body := []byte(`{"model":"claude-opus-5","max_tokens":100,"messages":[{"role":"user","content":"hi"}]` + test.options + `}`)
			result, err := gateway.ForwardAsAnthropic(context.Background(), requestContext, account, body)
			require.NoError(t, err)
			require.Equal(t, test.model, result.UpstreamModel)
			require.Equal(t, 1, calls)
		})
	}
}

func TestCursorNativeInterruptedProtocolErrors(t *testing.T) {
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses} {
		t.Run(protocol, func(t *testing.T) {
			gateway, account, exchanges := cursorTestGateway()
			calls := 0
			gateway.native = cursorDriverStub{run: func(_ context.Context, _ cursor.Credentials, _ []cursor.ChatMessage, _ string, emit func(cursor.StreamEvent) error) (cursor.TokenUsage, error) {
				calls++
				require.NoError(t, emit(cursor.StreamEvent{Type: "text", Text: "partial"}))
				return cursor.TokenUsage{InputTokens: 12, OutputTokens: 3, CacheReadTokens: 6, CacheWriteTokens: 2}, fmt.Errorf("private-upstream-detail synthetic-secret")
			}}
			recorder := httptest.NewRecorder()
			requestContext, _ := gin.CreateTestContext(recorder)
			result, err := callCursorProtocol(gateway, requestContext, account, cursorProtocolRequest(protocol, true), protocol)
			require.Error(t, err)
			var failover *UpstreamFailoverError
			require.NotErrorAs(t, err, &failover)
			require.NotNil(t, result)
			require.Equal(t, ClaudeUsage{InputTokens: 12, OutputTokens: 3, CacheReadInputTokens: 6, CacheCreationInputTokens: 2}, result.Usage)
			require.Equal(t, 1, calls)
			require.Equal(t, int32(1), exchanges.Load())
			body := recorder.Body.String()
			require.Contains(t, body, "partial")
			require.NotContains(t, body, "private-upstream-detail")
			require.NotContains(t, body, "synthetic-secret")
			for _, terminal := range []string{"message_stop", "response.completed", "[DONE]"} {
				require.NotContains(t, body, terminal)
			}
			errors := 0
			for _, line := range strings.Split(body, "\n") {
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				var event map[string]any
				require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event))
				if detail, ok := event["error"]; ok {
					errors++
					require.Equal(t, map[string]any{"type": "upstream_error", "message": "Cursor stream interrupted"}, detail)
					if protocol == APIProtocolAnthropic {
						require.Equal(t, "error", event["type"])
					} else {
						require.NotContains(t, event, "type")
					}
				}
			}
			require.Equal(t, 1, errors)
			if protocol == APIProtocolAnthropic {
				require.True(t, IsResponseCommitted(requestContext))
			}
			if protocol != APIProtocolChatCompletions {
				require.Equal(t, 1, strings.Count(body, "event: error\n"))
			}
		})
	}
}
