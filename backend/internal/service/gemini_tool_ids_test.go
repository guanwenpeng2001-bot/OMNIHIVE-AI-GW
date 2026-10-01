package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const geminiParallelToolHistory = `{"model":"gemini-test","messages":[
{"role":"assistant","content":[
{"type":"tool_use","id":"call-a","name":"lookup","input":{"query":"same"}},
{"type":"tool_use","id":"call-b","name":"lookup","input":{"query":"same"}}]},
{"role":"user","content":[
{"type":"tool_result","tool_use_id":"call-b","content":"second"},
{"type":"tool_result","tool_use_id":"call-a","content":"first"}]}]}`

func TestGeminiToolIDsProfilesAndOutOfOrderResults(t *testing.T) {
	for _, profile := range []geminiToolIDProfile{geminiToolIDNative, geminiToolIDLegacy, geminiToolIDUnknown} {
		t.Run(string(profile), func(t *testing.T) {
			body, err := convertClaudeMessagesToGeminiGenerateContent([]byte(geminiParallelToolHistory), profile)
			if profile == geminiToolIDUnknown {
				require.ErrorContains(t, err, "unknown Gemini tool ID profile")
				return
			}
			require.NoError(t, err)
			var req map[string]any
			require.NoError(t, json.Unmarshal(body, &req))
			contents := requireGeminiToolValue[[]any](t, req["contents"])
			require.Len(t, contents, 2)
			calls := requireGeminiToolValue[[]any](t, requireGeminiToolValue[map[string]any](t, contents[0])["parts"])
			require.Len(t, calls, 2)
			results := requireGeminiToolValue[[]any](t, requireGeminiToolValue[map[string]any](t, contents[1])["parts"])
			require.Len(t, results, 2)
			for i, id := range []string{"call-a", "call-b"} {
				call := requireGeminiToolValue[map[string]any](t, requireGeminiToolValue[map[string]any](t, calls[i])["functionCall"])
				require.Equal(t, map[string]any{"query": "same"}, call["args"], "no schema-external ID injection")
				if profile == geminiToolIDNative {
					require.Equal(t, id, call["id"])
				} else {
					require.NotContains(t, call, "id")
				}
			}
			first := requireGeminiToolValue[map[string]any](t, requireGeminiToolValue[map[string]any](t, results[0])["functionResponse"])
			second := requireGeminiToolValue[map[string]any](t, requireGeminiToolValue[map[string]any](t, results[1])["functionResponse"])
			if profile == geminiToolIDNative {
				require.Equal(t, "call-b", first["id"])
				require.Equal(t, "call-a", second["id"])
				require.Equal(t, map[string]any{"content": "second"}, first["response"])
			} else {
				require.NotContains(t, first, "id")
				require.NotContains(t, second, "id")
				require.Equal(t, map[string]any{"content": "first"}, first["response"])
				require.Equal(t, map[string]any{"content": "second"}, second["response"])
			}
		})
	}
}

func TestGeminiToolIDsRejectUncorrelatedResults(t *testing.T) {
	for name, body := range map[string]string{
		"unknown":          strings.Replace(geminiParallelToolHistory, `"tool_use_id":"call-b"`, `"tool_use_id":"missing"`, 1),
		"duplicate call":   strings.Replace(geminiParallelToolHistory, `"id":"call-b"`, `"id":"call-a"`, 1),
		"duplicate result": strings.Replace(geminiParallelToolHistory, `"tool_use_id":"call-b"`, `"tool_use_id":"call-a"`, 1),
		"missing call id":  strings.Replace(geminiParallelToolHistory, `"id":"call-b"`, `"id":""`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := convertClaudeMessagesToGeminiGenerateContent([]byte(body), geminiToolIDNative)
			require.Error(t, err)
		})
	}
	partial := strings.Replace(geminiParallelToolHistory, `,{"type":"tool_result","tool_use_id":"call-a","content":"first"}`, "", 1)
	// The fixture has a newline after the comma.
	partial = strings.Replace(partial, ",\n"+`{"type":"tool_result","tool_use_id":"call-a","content":"first"}`, "", 1)
	_, err := convertClaudeMessagesToGeminiGenerateContent([]byte(partial), geminiToolIDLegacy)
	require.ErrorContains(t, err, "complete call batch")
	_, err = convertClaudeMessagesToGeminiGenerateContent([]byte(partial), geminiToolIDNative)
	require.NoError(t, err)
}

func TestGeminiToolIDProfileSelection(t *testing.T) {
	for _, tc := range []struct {
		name    string
		account *Account
		want    geminiToolIDProfile
	}{
		{"public", &Account{Type: AccountTypeAPIKey}, geminiToolIDNative},
		{"public oauth", &Account{Type: AccountTypeOAuth}, geminiToolIDNative},
		{"public URL", &Account{Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://generativelanguage.googleapis.com/"}}, geminiToolIDNative},
		{"custom", &Account{Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://example.com"}}, geminiToolIDLegacy},
		{"code assist", &Account{Type: AccountTypeOAuth, Credentials: map[string]any{"project_id": "project"}}, geminiToolIDLegacy},
		{"vertex", &Account{Type: AccountTypeServiceAccount}, geminiToolIDLegacy},
		{"nil", nil, geminiToolIDUnknown},
		{"unsupported", &Account{Type: "unsupported"}, geminiToolIDUnknown},
		{"custom native override", &Account{Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://example.com"}, Extra: map[string]any{"gemini_tool_id_profile": "native"}}, geminiToolIDNative},
		{"explicit legacy", &Account{Extra: map[string]any{"gemini_tool_id_profile": "legacy_no_id"}}, geminiToolIDLegacy},
		{"explicit native", &Account{Extra: map[string]any{"gemini_tool_id_profile": "native"}}, geminiToolIDNative},
		{"unknown metadata uses transport", &Account{Type: AccountTypeAPIKey, Extra: map[string]any{"gemini_tool_id_profile": "future"}}, geminiToolIDNative},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, geminiToolIDProfileForAccount(tc.account)) })
	}
}

// These pre-existing account configurations have no gemini_tool_id_profile.
// Exercise the conversion used by both compatibility entry points so a tools-only
// first turn cannot fail locally before the upstream request is built.
func TestGeminiToolIDsExistingAccounts(t *testing.T) {
	for name, account := range map[string]*Account{
		"code assist oauth":      {Type: AccountTypeOAuth, Credentials: map[string]any{"project_id": "project"}},
		"vertex service account": {Type: AccountTypeServiceAccount},
		"custom api key":         {Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://example.com"}},
	} {
		t.Run(name, func(t *testing.T) {
			profile := geminiToolIDProfileForAccount(account)
			require.Equal(t, geminiToolIDLegacy, profile)
			for _, chat := range []bool{false, true} {
				entry := "messages"
				if chat {
					entry = "chat"
				}
				t.Run(entry, func(t *testing.T) {
					for _, withTools := range []bool{false, true} {
						request := map[string]any{"model": "gemini-test", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
						if withTools {
							tool := map[string]any{"name": "lookup", "input_schema": map[string]any{"type": "object"}}
							if chat {
								tool = map[string]any{"type": "function", "function": map[string]any{"name": "lookup", "parameters": map[string]any{"type": "object"}}}
							}
							request["tools"] = []any{tool}
						}
						body, err := json.Marshal(request)
						require.NoError(t, err)
						if chat {
							var req apicompat.ChatCompletionsRequest
							require.NoError(t, json.Unmarshal(body, &req))
							responses, err := apicompat.ChatCompletionsToResponses(&req)
							require.NoError(t, err)
							anthropic, err := apicompat.ResponsesToAnthropicRequest(responses)
							require.NoError(t, err)
							body, err = json.Marshal(anthropic)
							require.NoError(t, err)
						}
						converted, err := convertClaudeMessagesToGeminiGenerateContent(body, profile)
						require.NoError(t, err, "existing accounts must not require new profile metadata")
						var got map[string]any
						require.NoError(t, json.Unmarshal(converted, &got))
						if withTools {
							require.Contains(t, string(converted), `"functionDeclarations"`)
							require.Contains(t, string(converted), `"name":"lookup"`)
						} else {
							require.NotContains(t, got, "tools")
							// Profile selection must not change ordinary text requests.
							native, err := convertClaudeMessagesToGeminiGenerateContent(body, geminiToolIDNative)
							require.NoError(t, err)
							require.JSONEq(t, string(native), string(converted))
						}
					}
				})
			}
			// A subsequent parallel call batch still correlates results by local ID
			// before stripping unsupported IDs from the legacy wire format.
			converted, err := convertClaudeMessagesToGeminiGenerateContent([]byte(geminiParallelToolHistory), profile)
			require.NoError(t, err)
			require.NotContains(t, string(converted), `"id":`)
			require.Less(t, strings.Index(string(converted), `"content":"first"`), strings.Index(string(converted), `"content":"second"`))
		})
	}
}

func TestGeminiToolIDsUnknownRejectsDeclarationsButAllowsText(t *testing.T) {
	_, err := convertClaudeMessagesToGeminiGenerateContent([]byte(`{"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"lookup","input_schema":{"type":"object"}}]}`), geminiToolIDUnknown)
	require.ErrorContains(t, err, "unknown Gemini tool ID profile")
	_, err = convertClaudeMessagesToGeminiGenerateContent([]byte(`{"messages":[{"role":"user","content":"hi"}]}`), geminiToolIDUnknown)
	require.NoError(t, err)
}

func TestGeminiToolIDsAmbiguousStreamFailsWithoutSuccess(t *testing.T) {
	for _, payload := range []string{
		`{"functionCall":{"name":"lookup","args":"{\"query\":"}}`,
		`{"functionCall":{"id":"same","name":"lookup","args":{}}},{"functionCall":{"id":"other","name":"lookup","args":{}}},{"functionCall":{"id":"same","name":"lookup","args":{}}}`,
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		body := `data: {"candidates":[{"content":{"parts":[` + payload + `]}}]}` + "\n\n"
		_, err := (&GeminiMessagesCompatService{}).handleStreamingResponse(c, &http.Response{Body: io.NopCloser(strings.NewReader(body))}, time.Now(), "test")
		require.ErrorContains(t, err, "ambiguous")
		require.Contains(t, w.Body.String(), "event: error")
		require.NotContains(t, w.Body.String(), "event: message_stop")
	}
}

func TestGeminiToolIDsDuplicateUpstreamIDFails(t *testing.T) {
	body := `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"duplicate","name":"lookup","args":{}}},{"functionCall":{"id":"duplicate","name":"lookup","args":{}}}]}}]}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	_, err := (&GeminiMessagesCompatService{}).handleNonStreamingResponse(c,
		&http.Response{Body: io.NopCloser(strings.NewReader(body))}, "test")
	require.ErrorContains(t, err, "ambiguous")
	require.Equal(t, http.StatusBadGateway, w.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &response))
	_, _, err = geminiResponseToChatCompletions(response, "test", []byte(body), nil)
	require.ErrorContains(t, err, "ambiguous")
}

func TestGeminiToolIDsResponsePreservation(t *testing.T) {
	for _, ids := range []bool{true, false} {
		calls := []any{}
		for _, id := range []string{"native-a", "native-b"} {
			call := map[string]any{"name": "lookup", "args": map[string]any{"query": "same"}}
			if ids {
				call["id"] = id
			}
			calls = append(calls, map[string]any{"functionCall": call})
		}
		response := map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": calls}}}}
		raw, err := json.Marshal(response)
		require.NoError(t, err)
		converted, _ := convertGeminiToClaudeMessage(response, "test", raw, false)
		blocks := requireGeminiToolValue[[]any](t, converted["content"])
		require.Len(t, blocks, 2)
		first := requireGeminiToolValue[map[string]any](t, blocks[0])["id"]
		second := requireGeminiToolValue[map[string]any](t, blocks[1])["id"]
		require.NotEqual(t, first, second)
		if ids {
			require.Equal(t, "native-a", first)
			require.Equal(t, "native-b", second)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		var events strings.Builder
		for _, call := range calls {
			chunk, marshalErr := json.Marshal(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{call}}}}})
			require.NoError(t, marshalErr)
			_, _ = events.WriteString("data: " + string(chunk) + "\n\n")
		}
		events.WriteString("data: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n")
		upstream := &http.Response{Body: io.NopCloser(strings.NewReader(events.String()))}
		collected, _, collectErr := collectGeminiSSE(strings.NewReader(events.String()), false)
		require.NoError(t, collectErr)
		require.Len(t, extractGeminiParts(collected), 2, "SSE aggregation must retain both calls")
		_, err = (&GeminiMessagesCompatService{}).handleStreamingResponse(c, upstream, time.Now(), "test")
		require.NoError(t, err)
		var streamIDs []string
		for _, line := range strings.Split(w.Body.String(), "\n") {
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var event map[string]any
			require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event))
			if event["type"] == "content_block_start" {
				block := requireGeminiToolValue[map[string]any](t, event["content_block"])
				if block["type"] == "tool_use" {
					streamIDs = append(streamIDs, requireGeminiToolValue[string](t, block["id"]))
				}
			}
		}
		require.Len(t, streamIDs, 2)
		require.NotEqual(t, streamIDs[0], streamIDs[1])
		if ids {
			require.Equal(t, []string{"native-a", "native-b"}, streamIDs)
		}

		chatWriter := httptest.NewRecorder()
		chatContext, _ := gin.CreateTestContext(chatWriter)
		_, err = (&GeminiMessagesCompatService{}).handleChatCompletionsStreamingResponseFromGemini(chatContext,
			&http.Response{Body: io.NopCloser(strings.NewReader(events.String()))}, time.Now(), "test", false, false)
		require.NoError(t, err)
		var chatIDs []string
		for _, line := range strings.Split(chatWriter.Body.String(), "\n") {
			if !strings.HasPrefix(line, "data: ") || strings.Contains(line, "[DONE]") {
				continue
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						ToolCalls []struct {
							ID string `json:"id"`
						} `json:"tool_calls"`
					} `json:"delta"`
				} `json:"choices"`
			}
			require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &chunk))
			for _, choice := range chunk.Choices {
				for _, call := range choice.Delta.ToolCalls {
					if call.ID != "" {
						chatIDs = append(chatIDs, call.ID)
					}
				}
			}
		}
		require.Len(t, chatIDs, 2)
		require.NotEqual(t, chatIDs[0], chatIDs[1])
		if ids {
			require.Equal(t, []string{"native-a", "native-b"}, chatIDs)
		}
	}
}

func TestGeminiToolIDsForwardProfileBoundary(t *testing.T) {
	for _, profile := range []geminiToolIDProfile{geminiToolIDNative, geminiToolIDLegacy, geminiToolIDUnknown} {
		t.Run(string(profile), func(t *testing.T) {
			stub := &geminiCompatHTTPUpstreamStub{response: &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`))}}
			svc := &GeminiMessagesCompatService{httpUpstream: stub, cfg: &config.Config{}}
			account := &Account{Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test"}, Extra: map[string]any{"gemini_tool_id_profile": string(profile)}}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			_, err := svc.Forward(context.Background(), c, account, []byte(geminiParallelToolHistory))
			require.NoError(t, err)
			posted, err := io.ReadAll(stub.lastReq.Body)
			require.NoError(t, err)
			if profile != geminiToolIDLegacy {
				require.Contains(t, string(posted), `"id":"call-a"`)
			} else {
				require.NotContains(t, string(posted), `"id":`)
			}
		})
	}
}

func requireGeminiToolValue[T any](t *testing.T, value any) T {
	t.Helper()
	typed, ok := value.(T)
	require.True(t, ok, "unexpected Gemini JSON value type: %T", value)
	return typed
}
