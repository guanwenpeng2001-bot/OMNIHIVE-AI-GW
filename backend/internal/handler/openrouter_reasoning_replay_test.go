//go:build unit

package handler

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const replayModel = "vendor/replay-fixture:thinking"
const replayDetails = `[{"type":"reasoning.text","text":"SYNTHETIC_TEXT_ONLY_测试_~?/","signature":"FAKE_SIGNATURE_NOT_PROVIDER_VALID","id":"rd_fixture_text","format":"anthropic-claude-v1","index":0},{"type":"reasoning.summary","summary":"SYNTHETIC_SUMMARY_ONLY","id":"rd_fixture_summary","format":"anthropic-claude-v1","index":1},{"type":"reasoning.encrypted","data":"ZmFrZS1lbmNyeXB0ZWQtZml4dHVyZQ==","id":"rd_fixture_encrypted","format":"anthropic-claude-v1","index":2}]`
const replayCallID = "call_fixture_echo"
const replayAlias = "SYNTHETIC_PLAINTEXT_ALIAS"

type replayCapturedUpstream struct {
	protocol    string
	bodies      [][]byte
	urls        []string
	firstStream bool
	firstBody   string
}

func (upstream *replayCapturedUpstream) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	upstream.bodies = append(upstream.bodies, body)
	upstream.urls = append(upstream.urls, request.URL.String())
	response := `{"id":"fixture_turn_two","choices":[{"index":0,"message":{"role":"assistant","content":"synthetic echo accepted"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`
	if upstream.protocol == "messages" {
		response = `{"id":"msg_second","type":"message","role":"assistant","model":"vendor/replay-fixture:thinking","content":[{"type":"text","text":"synthetic echo accepted"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":1}}`
	}
	if upstream.protocol == "responses" {
		response = `{"id":"resp_second","object":"response","status":"completed","model":"vendor/replay-fixture:thinking","output":[{"type":"message","id":"msg_final","role":"assistant","content":[{"type":"output_text","text":"synthetic echo accepted","annotations":[]}]}],"usage":{"input_tokens":3,"output_tokens":1}}`
	}
	contentType := "application/json"
	if len(upstream.bodies) == 1 {
		response = upstream.firstBody
		if upstream.firstStream {
			contentType = "text/event-stream"
		}
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}, "X-Request-Id": {fmt.Sprintf("fixture-replay-turn-%d", len(upstream.bodies))}}, Body: io.NopCloser(strings.NewReader(response))}, nil
}
func (upstream *replayCapturedUpstream) DoWithTLS(request *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return upstream.Do(request, proxy, accountID, concurrency)
}

func replayJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
func replayObject(raw string) map[string]any {
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		panic(err)
	}
	return value
}
func replayEvents(raw string) []gjson.Result {
	var result []gjson.Result
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "data: ") {
			payload := strings.TrimPrefix(line, "data: ")
			if gjson.Valid(payload) {
				result = append(result, gjson.Parse(payload))
			}
		}
	}
	return result
}

func replayFirstUpstream(stream bool) string {
	message := `{"role":"assistant","content":null,"reasoning_content":"` + replayAlias + `","reasoning_details":` + replayDetails + `,"tool_calls":[{"id":"` + replayCallID + `","type":"function","function":{"name":"echo","arguments":"{\"value\":\"synthetic-echo\"}"}}]}`
	if !stream {
		return `{"id":"fixture_turn_one","choices":[{"index":0,"message":` + message + `,"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`
	}
	var events strings.Builder
	details := gjson.Parse(replayDetails).Array()
	for index, detail := range details {
		delta := map[string]any{"reasoning_details": json.RawMessage("[" + detail.Raw + "]")}
		if index == 0 {
			delta["role"] = "assistant"
			delta["reasoning_content"] = replayAlias
		}
		fmt.Fprintf(&events, "data: %s\n\n", replayJSON(map[string]any{"id": "fixture_turn_one", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}}))
	}
	events.WriteString(`data: {"id":"fixture_turn_one","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_fixture_echo","type":"function","function":{"name":"echo","arguments":"{\"value\":\"synthetic-echo\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}` + "\n\ndata: [DONE]\n\n")
	return events.String()
}

func replayTools(protocol string) any {
	schema := map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}, "required": []string{"value"}}
	switch protocol {
	case "messages":
		return []any{map[string]any{"name": "echo", "input_schema": schema}}
	case "responses":
		return []any{map[string]any{"type": "function", "name": "echo", "parameters": schema}}
	case "google":
		return []any{map[string]any{"functionDeclarations": []any{map[string]any{"name": "echo", "parameters": schema}}}}
	default:
		return []any{map[string]any{"type": "function", "function": map[string]any{"name": "echo", "parameters": schema}}}
	}
}

func replayRequest(protocol string, stream bool) map[string]any {
	request := map[string]any{"model": replayModel, "stream": stream, "tools": replayTools(protocol)}
	switch protocol {
	case "responses":
		request["include"] = []string{"reasoning.encrypted_content"}
		request["input"] = []any{map[string]any{"role": "user", "content": "Echo the fixture value"}}
	case "google":
		delete(request, "model")
		delete(request, "stream")
		request["contents"] = []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "Echo the fixture value"}}}}
		request["generationConfig"] = map[string]any{"thinkingConfig": map[string]any{"includeThoughts": true}}
	default:
		request["messages"] = []any{map[string]any{"role": "user", "content": "Echo the fixture value"}}
		if protocol == "messages" {
			request["max_tokens"] = 256
		}
	}
	return request
}

// The client assembles only fields actually received. It does not recover
// missing details from the test's provider fixture or invent a carrier field.
func replayPublicAssistant(test *testing.T, protocol string, stream bool, raw string) (any, string, string) {
	test.Helper()
	switch protocol {
	case "chat":
		var message map[string]any
		if !stream {
			message = replayObject(gjson.Get(raw, "choices.0.message").Raw)
		} else {
			message = map[string]any{"role": "assistant", "content": nil}
			details := []any{}
			calls := []any{}
			alias := ""
			for _, event := range replayEvents(raw) {
				delta := event.Get("choices.0.delta")
				alias += delta.Get("reasoning_content").String()
				for _, detail := range delta.Get("reasoning_details").Array() {
					details = append(details, replayObject(detail.Raw))
				}
				for _, call := range delta.Get("tool_calls").Array() {
					value := replayObject(call.Raw)
					delete(value, "index")
					calls = append(calls, value)
				}
			}
			if len(details) > 0 {
				message["reasoning_details"] = details
			}
			message["tool_calls"] = calls
			message["reasoning_content"] = alias
		}
		encoded := replayJSON(message)
		return message, gjson.Get(encoded, "tool_calls.0.id").String(), gjson.Get(encoded, "tool_calls.0.function.arguments").String()
	case "messages":
		var content []any
		if !stream {
			require.NoError(test, json.Unmarshal([]byte(gjson.Get(raw, "content").Raw), &content))
		} else {
			blocks := map[int]map[string]any{}
			arguments := map[int]string{}
			order := []int{}
			for _, event := range replayEvents(raw) {
				index := int(event.Get("index").Int())
				switch event.Get("type").String() {
				case "content_block_start":
					blocks[index] = replayObject(event.Get("content_block").Raw)
					order = append(order, index)
				case "content_block_delta":
					block := blocks[index]
					switch event.Get("delta.type").String() {
					case "thinking_delta":
						value, _ := block["thinking"].(string)
						block["thinking"] = value + event.Get("delta.thinking").String()
					case "signature_delta":
						value, _ := block["signature"].(string)
						block["signature"] = value + event.Get("delta.signature").String()
					case "input_json_delta":
						arguments[index] += event.Get("delta.partial_json").String()
					case "text_delta":
						value, _ := block["text"].(string)
						block["text"] = value + event.Get("delta.text").String()
					}
				}
			}
			for _, index := range order {
				block := blocks[index]
				if arguments[index] != "" {
					block["input"] = replayObject(arguments[index])
				}
				content = append(content, block)
			}
		}
		for _, entry := range content {
			encoded := replayJSON(entry)
			if gjson.Get(encoded, "type").String() == "tool_use" {
				return content, gjson.Get(encoded, "id").String(), gjson.Get(encoded, "input").Raw
			}
		}
	case "responses":
		if stream {
			for _, event := range replayEvents(raw) {
				if event.Get("type").String() == "response.completed" {
					raw = event.Get("response").Raw
				}
			}
		}
		var output []any
		require.NoError(test, json.Unmarshal([]byte(gjson.Get(raw, "output").Raw), &output))
		for _, entry := range output {
			encoded := replayJSON(entry)
			if gjson.Get(encoded, "type").String() == "function_call" {
				return output, gjson.Get(encoded, "call_id").String(), gjson.Get(encoded, "arguments").String()
			}
		}
	case "google":
		var content map[string]any
		if !stream {
			content = replayObject(gjson.Get(raw, "candidates.0.content").Raw)
		} else {
			parts := []any{}
			for _, event := range replayEvents(raw) {
				for _, part := range event.Get("candidates.0.content.parts").Array() {
					parts = append(parts, replayObject(part.Raw))
				}
			}
			content = map[string]any{"role": "model", "parts": parts}
		}
		for _, part := range gjson.Get(replayJSON(content), "parts").Array() {
			if call := part.Get("functionCall"); call.Exists() {
				return content, call.Get("id").String(), call.Get("args").Raw
			}
		}
	}
	test.Fatalf("no executable tool call in %s response: %s", protocol, raw)
	return nil, "", ""
}

func TestOpenRouterReasoningReplayPublicHandlers(test *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []struct {
		protocol string
		stream   bool
	}{{"chat", false}, {"chat", true}, {"messages", false}, {"messages", true}, {"responses", false}, {"responses", true}, {"google", false}, {"google", true}} {
		name := fmt.Sprintf("%s_stream_%t", scenario.protocol, scenario.stream)
		test.Run(name, func(test *testing.T) {
			upstream := &replayCapturedUpstream{protocol: scenario.protocol, firstStream: scenario.stream, firstBody: replayNativeFirst(scenario.protocol, scenario.stream)}
			handler, group := newOpenRouterReplayHandler(test, upstream)
			invoke := func(body map[string]any, stream bool) *httptest.ResponseRecorder {
				path := "/v1/chat/completions"
				next := handler.ChatCompletions
				switch scenario.protocol {
				case "messages":
					path = "/v1/messages"
					next = handler.Messages
				case "responses":
					path = "/v1/responses"
					next = handler.Responses
				case "google":
					path = "/v1beta/models/" + replayModel + ":generateContent"
					if stream {
						path = "/v1beta/models/" + replayModel + ":streamGenerateContent?alt=sse"
					}
					next = GeminiProtocolBridge(handler.Messages, handler.CountTokens, handler.Embeddings)
				}
				request, recorder := requestPlanHTTPContext(path, replayJSON(body), group)
				if scenario.protocol == "google" {
					action := "generateContent"
					if stream {
						action = "streamGenerateContent"
					}
					request.Params = gin.Params{{Key: "modelAction", Value: "/" + replayModel + ":" + action}}
				}
				next(request)
				return recorder
			}
			firstRequest := replayRequest(scenario.protocol, scenario.stream)
			first := invoke(firstRequest, scenario.stream)
			require.Equal(test, 200, first.Code, first.Body.String())
			require.Len(test, upstream.bodies, 1)
			assistant, publicCallID, arguments := replayPublicAssistant(test, scenario.protocol, scenario.stream, first.Body.String())
			require.NotEmpty(test, publicCallID)
			require.Equal(test, "synthetic-echo", gjson.Get(arguments, "value").String())
			// Execute the only tool locally, from the returned arguments.
			toolResult := map[string]any{"echo": gjson.Get(arguments, "value").String()}
			secondRequest := replayRequest(scenario.protocol, false)
			switch scenario.protocol {
			case "chat":
				secondRequest["messages"] = append(secondRequest["messages"].([]any), assistant, map[string]any{"role": "tool", "tool_call_id": publicCallID, "content": replayJSON(toolResult)})
			case "messages":
				secondRequest["messages"] = append(secondRequest["messages"].([]any), map[string]any{"role": "assistant", "content": assistant}, map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": publicCallID, "content": replayJSON(toolResult)}}})
			case "responses":
				input := append(secondRequest["input"].([]any), assistant.([]any)...)
				secondRequest["input"] = append(input, map[string]any{"type": "function_call_output", "call_id": publicCallID, "output": replayJSON(toolResult)})
			case "google":
				secondRequest["contents"] = append(secondRequest["contents"].([]any), assistant, map[string]any{"role": "user", "parts": []any{map[string]any{"functionResponse": map[string]any{"id": publicCallID, "name": "echo", "response": toolResult}}}})
			}
			second := invoke(secondRequest, false)
			require.Equal(test, 200, second.Code, second.Body.String())
			require.Len(test, upstream.bodies, 2)
			expectedPath := "https://openrouter.ai/api/v1/chat/completions"
			if scenario.protocol == "messages" || scenario.protocol == "responses" {
				expectedPath = "https://openrouter.ai/api/v1/" + scenario.protocol
			}
			require.Equal(test, []string{expectedPath, expectedPath}, upstream.urls)
			require.Equal(test, replayModel, gjson.GetBytes(upstream.bodies[1], "model").String())
			require.Contains(test, second.Body.String(), "synthetic echo accepted")
			switch scenario.protocol {
			case "messages":
				require.JSONEq(test, replayNativeMessageContent, gjson.GetBytes(upstream.bodies[1], "messages.1.content").Raw)
				require.Equal(test, replayCallID, gjson.GetBytes(upstream.bodies[1], "messages.2.content.0.tool_use_id").String())
				require.JSONEq(test, replayJSON(toolResult), gjson.GetBytes(upstream.bodies[1], "messages.2.content.0.content").String())
			case "responses":
				expected := gjson.Parse(replayNativeResponseOutput).Array()
				require.JSONEq(test, expected[0].Raw, gjson.GetBytes(upstream.bodies[1], "input.1").Raw)
				require.JSONEq(test, expected[1].Raw, gjson.GetBytes(upstream.bodies[1], "input.2").Raw)
				require.Equal(test, replayCallID, gjson.GetBytes(upstream.bodies[1], "input.3.call_id").String())
				require.JSONEq(test, replayJSON(toolResult), gjson.GetBytes(upstream.bodies[1], "input.3.output").String())
				require.False(test, gjson.GetBytes(upstream.bodies[1], "store").Exists())
				require.False(test, gjson.GetBytes(upstream.bodies[1], "previous_response_id").Exists())
			default:
				messages := gjson.GetBytes(upstream.bodies[1], "messages").Array()
				require.Len(test, messages, 3)
				require.Equal(test, []string{"user", "assistant", "tool"}, []string{messages[0].Get("role").String(), messages[1].Get("role").String(), messages[2].Get("role").String()})
				require.Equal(test, replayCallID, messages[1].Get("tool_calls.0.id").String())
				require.Equal(test, replayCallID, messages[2].Get("tool_call_id").String())
				require.JSONEq(test, replayJSON(toolResult), messages[2].Get("content").String())
				require.JSONEq(test, replayDetails, messages[1].Get("reasoning_details").Raw)
				if scenario.protocol == "google" {
					found := false
					for _, part := range gjson.Get(replayJSON(assistant), "parts").Array() {
						if token := part.Get("thoughtSignature").String(); token != "" {
							found = true
							decoded, err := base64.StdEncoding.DecodeString(token)
							require.NoError(test, err)
							require.Equal(test, "sub2api.reasoning-continuation", gjson.GetBytes(decoded, "namespace").String())
							require.Equal(test, "openrouter", gjson.GetBytes(decoded, "source.platform").String())
							require.JSONEq(test, replayDetails, gjson.GetBytes(decoded, "details").Raw)
							require.NotContains(test, string(upstream.bodies[1]), token)
							// Models Google SDK bytes decoding and JSON base64 serialization.
							require.Equal(test, token, base64.StdEncoding.EncodeToString(decoded))
						}
					}
					require.True(test, found, "standard thoughtSignature must carry the continuation")
				}
			}

		})
	}
}

const replayNativeMessageContent = `[{"type":"thinking","thinking":"SYNTHETIC_NATIVE_THINKING","signature":"FAKE_NATIVE_SIGNATURE","provider_opaque":{"order":[3,1,2]}},{"type":"redacted_thinking","data":"FAKE_REDACTED_DATA"},{"type":"tool_use","id":"call_fixture_echo","name":"echo","input":{"value":"synthetic-echo"}}]`
const replayNativeResponseOutput = `[{"type":"reasoning","id":"rs_provider_custom","summary":[{"type":"summary_text","text":"SYNTHETIC_NATIVE_SUMMARY"}],"encrypted_content":"FAKE_NATIVE_CIPHERTEXT","provider_opaque":{"order":[3,1,2]}},{"type":"function_call","id":"fc_provider_custom","call_id":"call_fixture_echo","name":"echo","arguments":"{\"value\":\"synthetic-echo\"}"}]`

func replayNativeFirst(protocol string, stream bool) string {
	if protocol != "messages" && protocol != "responses" {
		raw := replayFirstUpstream(stream)
		if protocol == "google" && stream {
			// Encrypted continuation arrives after the tool delta. The public
			// tool candidate must wait until this final opaque state is known.
			frames := strings.Split(strings.TrimSpace(raw), "\n\n")
			raw = strings.Join([]string{frames[0], frames[1], frames[3], frames[2], frames[4]}, "\n\n") + "\n\n"
		}
		return raw
	}
	if protocol == "messages" {
		reply := `{"id":"msg_native_provider","type":"message","role":"assistant","model":"vendor/replay-fixture:thinking","content":` + replayNativeMessageContent + `,"stop_reason":"tool_use","usage":{"input_tokens":2,"output_tokens":1}}`
		if !stream {
			return reply
		}
		var events strings.Builder
		write := func(kind string, value any) { fmt.Fprintf(&events, "event: %s\ndata: %s\n\n", kind, replayJSON(value)) }
		write("message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_native_provider", "type": "message", "role": "assistant", "model": replayModel, "content": []any{}, "usage": map[string]any{"input_tokens": 2, "output_tokens": 0}}})
		for index, block := range gjson.Parse(replayNativeMessageContent).Array() {
			// Unknown provider fields stay in the native content block, not a gateway alias.
			content := replayObject(block.Raw)
			signature := ""
			if block.Get("type").String() == "thinking" {
				signature = block.Get("signature").String()
				delete(content, "signature")
			}
			write("content_block_start", map[string]any{"type": "content_block_start", "index": index, "content_block": content})
			if signature != "" {
				write("content_block_delta", map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]any{"type": "signature_delta", "signature": signature}})
			}
			write("content_block_stop", map[string]any{"type": "content_block_stop", "index": index})
		}
		write("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use", "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 1}})
		write("message_stop", map[string]any{"type": "message_stop"})
		return events.String()
	}
	reply := `{"id":"resp_native_provider","object":"response","status":"completed","model":"vendor/replay-fixture:thinking","output":` + replayNativeResponseOutput + `,"usage":{"input_tokens":2,"output_tokens":1}}`
	if !stream {
		return reply
	}
	return "event: response.created\ndata: " + `{"type":"response.created","response":{"id":"resp_native_provider","object":"response","status":"in_progress","output":[]}}` + "\n\nevent: response.completed\ndata: " + `{"type":"response.completed","response":` + reply + `}` + "\n\n"
}

func newOpenRouterReplayHandler(test *testing.T, upstream *replayCapturedUpstream) (*OpenAIGatewayHandler, *service.Group) {
	test.Helper()
	cfg := &config.Config{RunMode: config.RunModeSimple}
	account := preparedGatewayFixtureAccount(service.Account{ID: 7, Platform: service.PlatformOpenRouter, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "synthetic-fixture-not-a-key", "model_mapping": map[string]any{replayModel: replayModel}}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}})
	repository := &geminiReviewAccountRepo{openAIWSUsageHandlerAccountRepoStub: openAIWSUsageHandlerAccountRepoStub{account: account}}
	gateway := service.ProvideOpenAIGatewayService(repository, &cursorRouteUsageRepo{}, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, service.NewUsageSettlementService(newHunyuanSTTReceiptRepository(), nil, nil))
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	test.Cleanup(billing.Stop)
	handler := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	group := &service.Group{ID: 3, Platform: service.PlatformOpenRouter, Status: service.StatusActive, Hydrated: true}
	return handler, group
}

func TestOpenRouterGoogleRejectsCarrierBeforeUpstream(t *testing.T) {
	for _, source := range []string{"grok", "openrouter"} {
		upstream := &replayCapturedUpstream{protocol: "google", firstBody: replayFirstUpstream(false)}
		handler, group := newOpenRouterReplayHandler(t, upstream)
		token := base64.StdEncoding.EncodeToString([]byte(`{"namespace":"sub2api.reasoning-continuation","source":{"platform":"` + source + `","protocol":"chat_completions","endpoint":"wrong-endpoint"},"details":[{"type":"reasoning.encrypted","data":"FAKE"}]}`))
		body := replayRequest("google", false)
		body["contents"] = []any{map[string]any{"role": "model", "parts": []any{map[string]any{"text": "fixture", "thoughtSignature": token}}}, map[string]any{"role": "user", "parts": []any{map[string]any{"text": "continue"}}}}
		request, recorder := requestPlanHTTPContext("/v1beta/models/"+replayModel+":generateContent", replayJSON(body), group)
		request.Params = gin.Params{{Key: "modelAction", Value: "/" + replayModel + ":generateContent"}}
		GeminiProtocolBridge(handler.Messages, handler.CountTokens, handler.Embeddings)(request)
		require.Equal(t, 400, recorder.Code, recorder.Body.String())
		require.Empty(t, upstream.bodies)
	}
}

func TestOpenRouterGoogleTruncatedStreamDoesNotReleaseTool(t *testing.T) {
	upstream := &replayCapturedUpstream{protocol: "google", firstStream: true, firstBody: strings.Replace(replayFirstUpstream(true), "data: [DONE]\n\n", "", 1)}
	handler, group := newOpenRouterReplayHandler(t, upstream)
	request, recorder := requestPlanHTTPContext("/v1beta/models/"+replayModel+":streamGenerateContent?alt=sse", replayJSON(replayRequest("google", true)), group)
	request.Params = gin.Params{{Key: "modelAction", Value: "/" + replayModel + ":streamGenerateContent"}}
	GeminiProtocolBridge(handler.Messages, handler.CountTokens, handler.Embeddings)(request)
	require.Len(t, upstream.bodies, 1, "committed partial stream cannot retry another account")
	require.Contains(t, recorder.Body.String(), "error")
	require.NotContains(t, recorder.Body.String(), "functionCall")
	require.NotContains(t, recorder.Body.String(), `"finishReason":"STOP"`)
}

func TestOpenRouterGoogleCarrierDoesNotBypassGroupModelPolicy(t *testing.T) {
	upstream := &replayCapturedUpstream{protocol: "google", firstBody: replayFirstUpstream(false)}
	handler, group := newOpenRouterReplayHandler(t, upstream)
	first, firstResponse := requestPlanHTTPContext("/v1beta/models/"+replayModel+":generateContent", replayJSON(replayRequest("google", false)), group)
	first.Params = gin.Params{{Key: "modelAction", Value: "/" + replayModel + ":generateContent"}}
	bridge := GeminiProtocolBridge(handler.Messages, handler.CountTokens, handler.Embeddings)
	bridge(first)
	require.Equal(t, 200, firstResponse.Code)
	assistant, callID, args := replayPublicAssistant(t, "google", false, firstResponse.Body.String())
	group.ModelAllowlist = service.GroupModelAllowlist{Enabled: true, Models: []string{"another/model"}}
	toolResult := map[string]any{"echo": gjson.Get(args, "value").String()}
	functionResponse := map[string]any{"id": callID, "name": "echo", "response": toolResult}
	userContent := map[string]any{"role": "user", "parts": []any{map[string]any{"functionResponse": functionResponse}}}
	body := replayRequest("google", false)
	body["contents"] = append(body["contents"].([]any), assistant, userContent)
	request, response := requestPlanHTTPContext("/v1beta/models/"+replayModel+":generateContent", replayJSON(body), group)
	router := gin.New()
	router.Use(func(current *gin.Context) {
		current.Keys = request.Keys
		current.Request = current.Request.WithContext(request.Request.Context())
	})
	router.POST("/v1beta/models/*modelAction", middleware.GroupModelAllowlist(), bridge)
	router.ServeHTTP(response, request.Request)
	require.Equal(t, 404, response.Code, response.Body.String())
	require.Len(t, upstream.bodies, 1)
}

func TestOpenRouterNativeHandlerDoesNotAppendSyntheticTerminal(t *testing.T) {
	for _, fixture := range []struct {
		body   string
		stream bool
		status int
	}{
		{"<html>not JSON</html>", false, 502},
		{`{"id":"resp_failed","object":"response","status":"failed","model":"vendor/replay-fixture:thinking","output":[],"error":{"code":"provider_error","message":"fixture"},"usage":{"input_tokens":2,"output_tokens":1}}`, false, 200},
		{"event: response.failed\ndata: " + `{"type":"response.failed","response":{"id":"resp_failed","object":"response","status":"failed","model":"vendor/replay-fixture:thinking","output":[],"error":{"code":"provider_error","message":"fixture"},"usage":{"input_tokens":2,"output_tokens":1}}}` + "\n\n", true, 200},
	} {
		upstream := &replayCapturedUpstream{protocol: "responses", firstBody: fixture.body, firstStream: fixture.stream}
		handler, group := newOpenRouterReplayHandler(t, upstream)
		request, response := requestPlanHTTPContext("/v1/responses", replayJSON(replayRequest("responses", fixture.stream)), group)
		handler.Responses(request)
		require.Equal(t, fixture.status, response.Code, response.Body.String())
		require.Len(t, upstream.bodies, 1)
		if !fixture.stream {
			require.True(t, gjson.Valid(response.Body.String()), response.Body.String())
			if fixture.status == 200 {
				require.JSONEq(t, fixture.body, response.Body.String())
			}
		} else {
			require.Equal(t, 1, strings.Count(response.Body.String(), "event: response.failed"))
			require.NotContains(t, response.Body.String(), "response.completed")
		}
	}
}
