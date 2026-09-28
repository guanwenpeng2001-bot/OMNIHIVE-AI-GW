package service

import (
	"bytes"
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
	"github.com/tidwall/gjson"
)

type antigravityCompatTokenCache struct {
	token string
}

type antigravityCompatErrorReader struct {
	data []byte
	off  int
	err  error
}

func (r *antigravityCompatErrorReader) Read(p []byte) (int, error) {
	if r.off < len(r.data) {
		n := copy(p, r.data[r.off:])
		r.off += n
		return n, nil
	}
	return 0, r.err
}

func (r *antigravityCompatErrorReader) Close() error { return nil }

func (c *antigravityCompatTokenCache) GetAccessToken(context.Context, string) (string, error) {
	return c.token, nil
}

func (c *antigravityCompatTokenCache) SetAccessToken(context.Context, string, string, time.Duration) error {
	return nil
}

func (c *antigravityCompatTokenCache) DeleteAccessToken(context.Context, string) error {
	return nil
}

func (c *antigravityCompatTokenCache) AcquireRefreshLock(context.Context, string, string, time.Duration) (bool, error) {
	return true, nil
}

func (c *antigravityCompatTokenCache) ReleaseRefreshLock(context.Context, string, string) error {
	return nil
}

func newAntigravityCompatService(cfg config.GatewayConfig, upstream HTTPUpstream) *AntigravityGatewayService {
	tokenProvider := NewAntigravityTokenProvider(
		nil,
		&antigravityCompatTokenCache{token: "fresh-oauth-token"},
		nil,
	)
	return NewAntigravityGatewayService(
		nil,
		nil,
		nil,
		tokenProvider,
		nil,
		upstream,
		NewSettingService(&antigravitySettingRepoStub{}, &config.Config{Gateway: cfg}),
		nil,
	)
}

func newAntigravityCompatAccount(accountType string) *Account {
	return &Account{
		ID:          3757,
		Name:        "antigravity-compat",
		Platform:    PlatformAntigravity,
		Type:        accountType,
		Status:      StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "stale-account-token",
			"project_id":   "project-3757",
			"model_mapping": map[string]any{
				"gemini-3.1-pro-high":      "gemini-3.1-pro-high",
				"claude-sonnet-4-5":        "claude-sonnet-4-5",
				"claude-sonnet-4-6":        "claude-sonnet-4-6",
				"claude-opus-4-6-thinking": "claude-opus-4-6-thinking",
			},
		},
	}
}

func newAntigravityCompatContext(method, path string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, path, bytes.NewReader(body))
	return c, recorder
}

func antigravityCompatSuccessResponse() *http.Response {
	body := `data: {"response":{"responseId":"resp_3757","candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":3}}}` + "\n\n"
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"X-Request-Id": []string{"request-3757"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}
}

func TestAntigravityCompatOAuthUsesNativeTokenAndRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		path string
		body []byte
		call func(*AntigravityGatewayService, context.Context, *gin.Context, *Account, []byte) (*ForwardResult, error)
	}{
		{
			name: "chat completions",
			path: "/v1/chat/completions",
			body: []byte(`{"model":"gemini-3.1-pro-high","messages":[{"role":"user","content":"Reply exactly: ok"}]}`),
			call: func(svc *AntigravityGatewayService, ctx context.Context, c *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
				return svc.ForwardAsChatCompletions(ctx, c, account, body, nil)
			},
		},
		{
			name: "responses",
			path: "/v1/responses",
			body: []byte(`{"model":"gemini-3.1-pro-high","input":"Reply exactly: ok"}`),
			call: func(svc *AntigravityGatewayService, ctx context.Context, c *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
				return svc.ForwardAsResponses(ctx, c, account, body, nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var authorization string
			var upstreamPath string
			var upstreamAlt string
			upstream := &queuedHTTPUpstreamStub{
				responses: []*http.Response{antigravityCompatSuccessResponse()},
				onCall: func(req *http.Request, _ *queuedHTTPUpstreamStub) {
					authorization = req.Header.Get("Authorization")
					upstreamPath = req.URL.Path
					upstreamAlt = req.URL.Query().Get("alt")
				},
			}
			svc := newAntigravityCompatService(
				config.GatewayConfig{MaxLineSize: defaultMaxLineSize},
				upstream,
			)
			c, recorder := newAntigravityCompatContext(http.MethodPost, tt.path, tt.body)

			result, err := tt.call(svc, context.Background(), c, newAntigravityCompatAccount(AccountTypeOAuth), tt.body)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, "Bearer fresh-oauth-token", authorization)
			require.Equal(t, "/v1internal:streamGenerateContent", upstreamPath)
			require.Equal(t, "sse", upstreamAlt)
			require.Equal(t, "request-3757", result.RequestID)
			require.Equal(t, 8, result.Usage.InputTokens)
			require.Equal(t, 3, result.Usage.OutputTokens)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Contains(t, recorder.Body.String(), "ok")
			if tt.name == "chat completions" {
				require.Equal(t, "stop", gjson.Get(recorder.Body.String(), "choices.0.finish_reason").String())
				require.Equal(t, int64(8), gjson.Get(recorder.Body.String(), "usage.prompt_tokens").Int())
				require.Equal(t, int64(3), gjson.Get(recorder.Body.String(), "usage.completion_tokens").Int())
			}
		})
	}
}

func TestAntigravityCompatRejectsUnsupportedAccountType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		path        string
		accountType string
		call        func(*AntigravityGatewayService, context.Context, *gin.Context, *Account, []byte) (*ForwardResult, error)
	}{
		{
			name:        "chat completions upstream",
			path:        "/v1/chat/completions",
			accountType: AccountTypeUpstream,
			call: func(svc *AntigravityGatewayService, ctx context.Context, c *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
				return svc.ForwardAsChatCompletions(ctx, c, account, body, nil)
			},
		},
		{
			name:        "responses setup token",
			path:        "/v1/responses",
			accountType: AccountTypeSetupToken,
			call: func(svc *AntigravityGatewayService, ctx context.Context, c *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
				return svc.ForwardAsResponses(ctx, c, account, body, nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(`{"model":"gemini-3.1-pro-high"}`)
			c, recorder := newAntigravityCompatContext(http.MethodPost, tt.path, body)

			result, err := tt.call(&AntigravityGatewayService{}, context.Background(), c, newAntigravityCompatAccount(tt.accountType), body)

			require.Error(t, err)
			require.Nil(t, result)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Contains(t, recorder.Body.String(), "native OAuth account required for antigravity compatibility mode")
		})
	}
}

func TestBuildAntigravityCompatGeminiBody_ConfiguresMixedToolInvocations(t *testing.T) {
	svc := &AntigravityGatewayService{}
	tests := []struct {
		name           string
		tools          string
		wantFuncs      bool
		wantGoogle     bool
		wantServerFlag bool
	}{
		{
			name:       "mixed server and client tools prefer client tools",
			tools:      `[{"name":"get_weather","input_schema":{"type":"object"}},{"type":"web_search_20250305","name":"web_search"}]`,
			wantFuncs:  true,
			wantGoogle: false,
		},
		{
			name:      "client tools only",
			tools:     `[{"name":"get_weather","input_schema":{"type":"object"}}]`,
			wantFuncs: true,
		},
		{
			name:       "server tools only",
			tools:      `[{"type":"web_search_20250305","name":"web_search"}]`,
			wantGoogle: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claudeBody := []byte(`{"messages":[{"role":"user","content":"hello"}],"tools":` + tt.tools + `}`)
			claudeBody = bytes.ReplaceAll(claudeBody, []byte{92}, nil)
			body, err := svc.buildAntigravityCompatGeminiBody(context.Background(), claudeBody, nil, "project-1", "gemini-2.5-flash")
			require.NoError(t, err)

			var wrapped map[string]any
			require.NoError(t, json.Unmarshal(body, &wrapped))
			request, ok := wrapped["request"].(map[string]any)
			require.True(t, ok)

			tools, _ := request["tools"].([]any)
			hasFuncs, hasGoogle := false, false
			for _, raw := range tools {
				tool, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				if decls, ok := tool["functionDeclarations"].([]any); ok && len(decls) > 0 {
					hasFuncs = true
				}
				if _, ok := tool["googleSearch"]; ok {
					hasGoogle = true
				}
			}
			require.Equal(t, tt.wantFuncs, hasFuncs)
			require.Equal(t, tt.wantGoogle, hasGoogle)

			toolConfig, exists := request["toolConfig"].(map[string]any)
			if !tt.wantServerFlag {
				if exists {
					require.NotContains(t, toolConfig, "includeServerSideToolInvocations")
				}
				return
			}
			require.True(t, exists)
			require.Equal(t, true, toolConfig["includeServerSideToolInvocations"])
		})
	}
}

func TestAntigravityCompatToolReconciliation(t *testing.T) {
	for _, body := range []string{
		`{"contents":[],"toolConfig":{"includeServerSideToolInvocations":true}}`,
		`{"tools":[{"googleSearch":{}}]}`,
		`{"tools":[{"codeExecution":{}}]}`,
		`{"tools":[{"functionDeclarations":[{"name":"shell"}]}],"toolConfig":{"functionCallingConfig":{"mode":"ANY"}}}`,
		`{"tools":[{"functionDeclarations":[]},{"googleSearch":{}}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			cleaned, err := enableMixedGeminiToolInvocations([]byte(body))
			require.NoError(t, err)
			require.Equal(t, body, string(cleaned))
		})
	}
	t.Run("same object preserves functions and unrelated fields", func(t *testing.T) {
		body := []byte(`{"contents":[],"tools":[{"functionDeclarations":[{"name":"shell"},{"name":"read_file"}],"googleSearch":{},"codeExecution":{},"extension":"keep"},{"googleSearch":{}},{"codeExecution":{}}],"toolConfig":{"includeServerSideToolInvocations":true,"include_server_side_tool_invocations":true,"functionCallingConfig":{"mode":"ANY","allowedFunctionNames":["shell"]}}}`)
		cleaned, err := enableMixedGeminiToolInvocations(body)
		require.NoError(t, err)
		require.JSONEq(t, `{"contents":[],"tools":[{"functionDeclarations":[{"name":"shell"},{"name":"read_file"}],"extension":"keep"}],"toolConfig":{"functionCallingConfig":{"mode":"ANY","allowedFunctionNames":["shell"]}}}`, string(cleaned))
		again, err := enableMixedGeminiToolInvocations(cleaned)
		require.NoError(t, err)
		require.Equal(t, cleaned, again)
	})
	t.Run("invalid JSON is rejected", func(t *testing.T) {
		_, err := enableMixedGeminiToolInvocations([]byte(`{"tools":`))
		require.Error(t, err)
	})
}

// Capture the actual outbound request for every protocol, including Messages.
func TestAntigravityCompatToolEntryMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{"messages", "chat", "responses", "gemini"} {
		for _, scenario := range []string{"search", "functions", "mixed", "code execution", "no tools"} {
			t.Run(protocol+"/"+scenario, func(t *testing.T) {
				const model = "gemini-3.1-pro-high"
				function := map[string]any{"name": "shell", "input_schema": map[string]any{"type": "object"}}
				search := map[string]any{"type": "web_search"}
				code := map[string]any{"type": "code_execution"}
				switch protocol {
				case "chat":
					function = map[string]any{"type": "function", "function": map[string]any{"name": "shell", "parameters": map[string]any{"type": "object"}}}
				case "responses":
					function = map[string]any{"type": "function", "name": "shell", "parameters": map[string]any{"type": "object"}}
				case "gemini":
					function = map[string]any{"functionDeclarations": []any{map[string]any{"name": "shell", "parameters": map[string]any{"type": "object"}}}}
					search = map[string]any{"googleSearch": map[string]any{}}
					code = map[string]any{"codeExecution": map[string]any{}}
				}
				var tools []any
				switch scenario {
				case "search":
					tools = []any{search}
				case "functions":
					tools = []any{function}
				case "mixed":
					tools = []any{function, search, code}
				case "code execution":
					tools = []any{code}
				}
				payload := map[string]any{"model": model, "stream": true, "max_tokens": 16, "tools": tools}
				payload["messages"] = []any{map[string]any{"role": "user", "content": "hello"}}
				path := "/v1/messages"
				switch protocol {
				case "chat":
					path = "/v1/chat/completions"
				case "responses":
					path = "/v1/responses"
					delete(payload, "messages")
					payload["input"] = "hello"
				case "gemini":
					path = "/v1beta/models/" + model + ":streamGenerateContent"
					payload = map[string]any{"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hello"}}}}, "tools": tools}
				}
				body, err := json.Marshal(payload)
				require.NoError(t, err)
				upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{antigravityCompatSuccessResponse()}}
				svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, upstream)
				account := newAntigravityCompatAccount(AccountTypeOAuth)
				c, recorder := newAntigravityCompatContext(http.MethodPost, path, body)
				var result *ForwardResult
				switch protocol {
				case "messages":
					result, err = svc.Forward(context.Background(), c, account, body, false)
				case "chat":
					result, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, nil)
				case "responses":
					result, err = svc.ForwardAsResponses(context.Background(), c, account, body, nil)
				case "gemini":
					result, err = svc.ForwardGemini(context.Background(), c, account, model, "streamGenerateContent", true, body, false)
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, upstream.requestBodies, 1)
				outbound := upstream.requestBodies[0]
				wantModel, wantType := model, "agent"
				// Gemini-mapped compatibility requests use the existing raw route;
				// only Messages uses the transformer's pure-search Flash fallback.
				if scenario == "search" && protocol == "messages" {
					wantModel, wantType = "gemini-2.5-flash", "web_search"
				}
				require.Equal(t, wantModel, gjson.GetBytes(outbound, "model").String())
				require.Equal(t, wantType, gjson.GetBytes(outbound, "requestType").String())
				var names []string
				hasSearch, hasCode := false, false
				for _, tool := range gjson.GetBytes(outbound, "request.tools").Array() {
					hasSearch = hasSearch || tool.Get("googleSearch").Exists()
					hasCode = hasCode || tool.Get("codeExecution").Exists()
					for _, declaration := range tool.Get("functionDeclarations").Array() {
						names = append(names, declaration.Get("name").String())
					}
				}
				if scenario == "functions" || scenario == "mixed" {
					require.Equal(t, []string{"shell"}, names)
				} else {
					require.Empty(t, names)
				}
				require.Equal(t, scenario == "search", hasSearch)
				// The raw Claude-to-Gemini compatibility converter does not expose
				// code execution; preserve that existing protocol boundary.
				require.Equal(t, scenario == "code execution" && (protocol == "messages" || protocol == "gemini"), hasCode)
				require.False(t, gjson.GetBytes(outbound, "request.toolConfig.includeServerSideToolInvocations").Exists())
				require.False(t, gjson.GetBytes(outbound, "request.toolConfig.include_server_side_tool_invocations").Exists())
				require.NotContains(t, recorder.Body.String(), "dropping built-in")
				require.Empty(t, recorder.Header().Get("X-Sub2API-Tool-Degraded"))
			})
		}
	}
}

func TestAntigravityCompatChatMixedBuiltInToolsPreferClientFunctions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{antigravityCompatSuccessResponse()}}
	svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, upstream)
	body := []byte(`{
		"model":"claude-opus-4-6-thinking",
		"messages":[{"role":"user","content":"hello"}],
		"stream":true,
		"tools":[
			{"type":"function","function":{"name":"read_file","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}},
			{"type":"function","function":{"name":"terminal","parameters":{"type":"object","properties":{"command":{"type":"string"}}}}},
			{"type":"web_search"},
			{"type":"code_execution"}
		]
	}`)
	c, _ := newAntigravityCompatContext(http.MethodPost, "/v1/chat/completions", body)

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, newAntigravityCompatAccount(AccountTypeOAuth), body, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.requestBodies, 1)
	requestBody := upstream.requestBodies[0]
	require.False(t, gjson.GetBytes(requestBody, "request.toolConfig.includeServerSideToolInvocations").Exists())
	require.Len(t, gjson.GetBytes(requestBody, "request.tools.0.functionDeclarations").Array(), 2)
	// No built-in tool entries should remain once client functions are present.
	for _, tool := range gjson.GetBytes(requestBody, "request.tools").Array() {
		require.False(t, tool.Get("googleSearch").Exists())
		require.False(t, tool.Get("codeExecution").Exists())
	}
}

func TestAntigravityCompatResponsesCodexWebSearchMixedWithFunctionsDropsBuiltins(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{antigravityCompatSuccessResponse()}}
	svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, upstream)
	body := []byte(`{
		"model":"claude-sonnet-4-6",
		"input":"Reply with exactly: pong",
		"stream":true,
		"tools":[
			{"type":"function","name":"shell","description":"Run a shell command","parameters":{"type":"object","properties":{"command":{"type":"array","items":{"type":"string"}}},"required":["command"],"additionalProperties":false},"strict":true},
			{"type":"web_search"}
		]
	}`)
	c, _ := newAntigravityCompatContext(http.MethodPost, "/v1/responses", body)

	result, err := svc.ForwardAsResponses(context.Background(), c, newAntigravityCompatAccount(AccountTypeOAuth), body, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.requestBodies, 1)
	requestBody := upstream.requestBodies[0]
	require.Equal(t, "agent", gjson.GetBytes(requestBody, "requestType").String())
	require.NotEqual(t, "gemini-2.5-flash", gjson.GetBytes(requestBody, "model").String())
	require.True(t, gjson.GetBytes(requestBody, "request.tools.0.functionDeclarations.#(name==\"shell\")").Exists())
	for _, tool := range gjson.GetBytes(requestBody, "request.tools").Array() {
		require.False(t, tool.Get("googleSearch").Exists())
	}
	require.False(t, gjson.GetBytes(requestBody, "request.toolConfig.includeServerSideToolInvocations").Exists())
}

func TestAntigravityCompatPreservesChatTokenLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name string
		body string
		want int64
	}{
		{
			name: "legacy max_tokens below bridge floor",
			body: `{"model":"gemini-3.1-pro-high","messages":[{"role":"user","content":"ok"}],"max_tokens":8}`,
			want: 8,
		},
		{
			name: "max_completion_tokens takes precedence",
			body: `{"model":"gemini-3.1-pro-high","messages":[{"role":"user","content":"ok"}],"max_tokens":8,"max_completion_tokens":13}`,
			want: 13,
		},
		{
			name: "max_tokens at safe ceiling is preserved",
			body: `{"model":"gemini-3.1-pro-high","messages":[{"role":"user","content":"ok"}],"max_tokens":64000}`,
			want: 64000,
		},
		{
			name: "max_tokens above safe ceiling is clamped",
			body: `{"model":"gemini-3.1-pro-high","messages":[{"role":"user","content":"ok"}],"max_tokens":64001}`,
			want: 64000,
		},
		{
			name: "precedence applies before clamping",
			body: `{"model":"gemini-3.1-pro-high","messages":[{"role":"user","content":"ok"}],"max_tokens":8,"max_completion_tokens":64001}`,
			want: 64000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{antigravityCompatSuccessResponse()}}
			svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, upstream)
			body := []byte(tt.body)
			c, _ := newAntigravityCompatContext(http.MethodPost, "/v1/chat/completions", body)

			result, err := svc.ForwardAsChatCompletions(
				context.Background(),
				c,
				newAntigravityCompatAccount(AccountTypeOAuth),
				body,
				nil,
			)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, upstream.requestBodies, 1)
			require.Equal(t, tt.want, gjson.GetBytes(upstream.requestBodies[0], "request.generationConfig.maxOutputTokens").Int())
		})
	}
}

func TestPreserveAntigravityChatCompletionTokenLimitIgnoresAbsentAndNonPositiveValues(t *testing.T) {
	tests := []struct {
		name    string
		request apicompat.ChatCompletionsRequest
	}{
		{name: "absent"},
		{name: "zero max_tokens", request: apicompat.ChatCompletionsRequest{MaxTokens: antigravityCompatIntPtr(0)}},
		{name: "negative max_completion_tokens takes precedence", request: apicompat.ChatCompletionsRequest{MaxTokens: antigravityCompatIntPtr(12), MaxCompletionTokens: antigravityCompatIntPtr(-1)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claudeRequest := &apicompat.AnthropicRequest{MaxTokens: 99}
			preserveAntigravityChatCompletionTokenLimit(&tt.request, claudeRequest)
			require.Equal(t, 99, claudeRequest.MaxTokens)
		})
	}
}

func antigravityCompatIntPtr(v int) *int { return &v }

func TestAntigravityCompatRoutesByMappedModelFamily(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		model         string
		wantSessionID bool
	}{
		{model: "gemini-3.1-pro-high", wantSessionID: false},
		{model: "claude-sonnet-4-5", wantSessionID: true},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{antigravityCompatSuccessResponse()}}
			svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, upstream)
			body := []byte(`{"model":"` + tt.model + `","messages":[{"role":"user","content":"ok"}],"max_tokens":8}`)
			c, _ := newAntigravityCompatContext(http.MethodPost, "/v1/chat/completions", body)

			result, err := svc.ForwardAsChatCompletions(
				context.Background(),
				c,
				newAntigravityCompatAccount(AccountTypeOAuth),
				body,
				nil,
			)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, upstream.requestBodies, 1)
			require.Equal(t, tt.model, gjson.GetBytes(upstream.requestBodies[0], "model").String())
			require.Equal(t, tt.wantSessionID, gjson.GetBytes(upstream.requestBodies[0], "request.sessionId").Exists())
		})
	}
}

func TestAntigravityCompatUnauthorizedIsCredentialFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{{
		StatusCode: http.StatusUnauthorized,
		Header:     http.Header{"X-Request-Id": []string{"auth-3757"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"Invalid bearer token"}}`)),
	}}}
	svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, upstream)
	body := []byte(`{"model":"gemini-3.1-pro-high","messages":[{"role":"user","content":"ok"}]}`)
	c, recorder := newAntigravityCompatContext(http.MethodPost, "/v1/chat/completions", body)

	result, err := svc.ForwardAsChatCompletions(
		context.Background(),
		c,
		newAntigravityCompatAccount(AccountTypeOAuth),
		body,
		nil,
	)

	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, GatewayFailureStageAccountAuth, failoverErr.Stage)
	require.Equal(t, GatewayFailureScopeAccount, failoverErr.Scope)
	require.Equal(t, AntigravityCredentialRejectedReason, failoverErr.Reason)
	require.Equal(t, NextAccountRetry, failoverErr.NextAccountAction)
	require.Equal(t, http.StatusBadGateway, failoverErr.ClientStatusCode)
	require.Equal(t, AntigravityCredentialRejectedClientMessage, failoverErr.ClientMessage)
	require.Equal(t, "auth-3757", failoverErr.ResponseHeaders.Get("X-Request-Id"))
	require.Empty(t, recorder.Body.String())
}

func TestAntigravityCompatEmptyStreamTriggersFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		run  func(*AntigravityGatewayService, *gin.Context, *http.Response) (*antigravityStreamResult, error)
	}{
		{
			name: "chat completions",
			run: func(svc *AntigravityGatewayService, c *gin.Context, resp *http.Response) (*antigravityStreamResult, error) {
				return svc.handleChatCompletionsStreamingFromAntigravity(c, resp, time.Now(), "gemini-3.1-pro-high", true)
			},
		},
		{
			name: "responses",
			run: func(svc *AntigravityGatewayService, c *gin.Context, resp *http.Response) (*antigravityStreamResult, error) {
				return svc.handleResponsesStreamingFromAntigravity(c, resp, time.Now(), "gemini-3.1-pro-high")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, nil)
			c, recorder := newAntigravityCompatContext(http.MethodPost, "/", nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader("data: malformed\n\ndata: [DONE]\n\n")),
			}

			result, err := tt.run(svc, c, resp)

			require.Nil(t, result)
			var failoverErr *UpstreamFailoverError
			require.ErrorAs(t, err, &failoverErr)
			require.True(t, failoverErr.RetryableOnSameAccount)
			require.Empty(t, recorder.Body.String())
			require.Empty(t, recorder.Header().Get("Content-Type"))
		})
	}
}

func TestAntigravityCompatUsageOnlyStreamTriggersFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		run  func(*AntigravityGatewayService, *gin.Context, *http.Response) (*antigravityStreamResult, error)
	}{
		{
			name: "chat completions",
			run: func(svc *AntigravityGatewayService, c *gin.Context, resp *http.Response) (*antigravityStreamResult, error) {
				return svc.handleChatCompletionsStreamingFromAntigravity(c, resp, time.Now(), "gemini-3.1-pro-high", true)
			},
		},
		{
			name: "responses",
			run: func(svc *AntigravityGatewayService, c *gin.Context, resp *http.Response) (*antigravityStreamResult, error) {
				return svc.handleResponsesStreamingFromAntigravity(c, resp, time.Now(), "gemini-3.1-pro-high")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, nil)
			c, recorder := newAntigravityCompatContext(http.MethodPost, "/", nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body: io.NopCloser(strings.NewReader(
					`data: {"response":{"responseId":"resp_3757","usageMetadata":{"promptTokenCount":8}}}` + "\n\n",
				)),
			}

			result, err := tt.run(svc, c, resp)

			require.Nil(t, result)
			var failoverErr *UpstreamFailoverError
			require.ErrorAs(t, err, &failoverErr)
			require.True(t, failoverErr.RetryableOnSameAccount)
			require.Empty(t, recorder.Body.String())
			require.Empty(t, recorder.Header().Get("Content-Type"))
		})
	}
}

func TestAntigravityCompatUsageOnlyNonStreamingTriggersFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		path string
		body []byte
		call func(*AntigravityGatewayService, context.Context, *gin.Context, *Account, []byte) (*ForwardResult, error)
	}{
		{
			name: "chat completions",
			path: "/v1/chat/completions",
			body: []byte(`{"model":"gemini-3.1-pro-high","messages":[{"role":"user","content":"ok"}]}`),
			call: func(svc *AntigravityGatewayService, ctx context.Context, c *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
				return svc.ForwardAsChatCompletions(ctx, c, account, body, nil)
			},
		},
		{
			name: "responses",
			path: "/v1/responses",
			body: []byte(`{"model":"gemini-3.1-pro-high","input":"ok"}`),
			call: func(svc *AntigravityGatewayService, ctx context.Context, c *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
				return svc.ForwardAsResponses(ctx, c, account, body, nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body: io.NopCloser(strings.NewReader(
					`data: {"response":{"responseId":"resp_3757","usageMetadata":{"promptTokenCount":8}}}` + "\n\n",
				)),
			}}}
			svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, upstream)
			c, recorder := newAntigravityCompatContext(http.MethodPost, tt.path, tt.body)

			result, err := tt.call(
				svc,
				context.Background(),
				c,
				newAntigravityCompatAccount(AccountTypeOAuth),
				tt.body,
			)

			require.Nil(t, result)
			var failoverErr *UpstreamFailoverError
			require.ErrorAs(t, err, &failoverErr)
			require.True(t, failoverErr.RetryableOnSameAccount)
			require.Empty(t, recorder.Body.String())
			require.Empty(t, recorder.Header().Get("Content-Type"))
		})
	}
}

func TestAntigravityCompatChatStreamMapsToolCallAndUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, nil)
	c, recorder := newAntigravityCompatContext(http.MethodPost, "/v1/chat/completions", nil)
	body := `data: {"response":{"responseId":"resp_3757","candidates":[{"content":{"parts":[{"functionCall":{"id":"call_3757","name":"get_weather","args":{"city":"Tokyo"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":3}}}` + "\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	result, err := svc.handleChatCompletionsStreamingFromAntigravity(
		c,
		resp,
		time.Now(),
		"gemini-3.1-pro-high",
		true,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 8, result.usage.InputTokens)
	require.Equal(t, 3, result.usage.OutputTokens)
	require.Contains(t, recorder.Body.String(), `"tool_calls"`)
	require.Contains(t, recorder.Body.String(), `"get_weather"`)
	require.Contains(t, recorder.Body.String(), `"finish_reason":"tool_calls"`)
	require.Contains(t, recorder.Body.String(), `"prompt_tokens":8`)
	require.Equal(t, 1, strings.Count(recorder.Body.String(), "data: [DONE]"))
}

func TestAntigravityCompatFirstEventTimeoutTriggersFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newAntigravityCompatService(
		config.GatewayConfig{MaxLineSize: defaultMaxLineSize, StreamDataIntervalTimeout: 1},
		nil,
	)
	c, recorder := newAntigravityCompatContext(http.MethodPost, "/v1/chat/completions", nil)
	reader, writer := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}
	type outcome struct {
		result *antigravityStreamResult
		err    error
	}
	done := make(chan outcome, 1)

	go func() {
		result, err := svc.handleChatCompletionsStreamingFromAntigravity(c, resp, time.Now(), "gemini-3.1-pro-high", false)
		done <- outcome{result: result, err: err}
	}()

	select {
	case got := <-done:
		require.Nil(t, got.result)
		var failoverErr *UpstreamFailoverError
		require.ErrorAs(t, got.err, &failoverErr)
		require.True(t, failoverErr.RetryableOnSameAccount)
		require.Empty(t, recorder.Header().Get("Content-Type"))
	case <-time.After(2 * time.Second):
		_ = writer.Close()
		_ = reader.Close()
		t.Fatal("compat stream ignored StreamDataIntervalTimeout")
	}
	_ = writer.Close()
	_ = reader.Close()
}

func TestAntigravityCompatClientDisconnectDrainsUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, nil)
	c, _ := newAntigravityCompatContext(http.MethodPost, "/v1/chat/completions", nil)
	c.Writer = &antigravityFailingWriter{ResponseWriter: c.Writer, failAfter: 0}
	body := strings.Join([]string{
		`data: {"response":{"responseId":"resp_3757","candidates":[{"content":{"parts":[{"text":"partial"}]}}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":1}}}`,
		"",
		`data: {"response":{"responseId":"resp_3757","candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":15}}}`,
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	result, err := svc.handleChatCompletionsStreamingFromAntigravity(c, resp, time.Now(), "gemini-3.1-pro-high", false)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.clientDisconnect)
	require.Equal(t, 1, result.usage.OutputTokens)
}

func TestAntigravityCompatStreamErrorCommitsSingleTerminalFrame(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, nil)
	c, recorder := newAntigravityCompatContext(http.MethodPost, "/v1/responses", nil)
	body := []byte(`data: {"response":{"responseId":"resp_3757","candidates":[{"content":{"parts":[{"text":"partial"}]}}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":1}}}` + "\n\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: &antigravityCompatErrorReader{
			data: body,
			err:  io.ErrUnexpectedEOF,
		},
	}

	result, err := svc.handleResponsesStreamingFromAntigravity(c, resp, time.Now(), "gemini-3.1-pro-high")

	require.Error(t, err)
	require.Nil(t, result)
	require.True(t, IsResponseCommitted(c))
	require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: error"))
}

func TestAntigravityCompatKeepaliveAfterFirstEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newAntigravityCompatService(
		config.GatewayConfig{MaxLineSize: defaultMaxLineSize, StreamKeepaliveInterval: 1},
		nil,
	)
	c, recorder := newAntigravityCompatContext(http.MethodPost, "/v1/responses", nil)
	reader, writer := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}
	done := make(chan error, 1)

	go func() {
		_, err := svc.handleResponsesStreamingFromAntigravity(c, resp, time.Now(), "gemini-3.1-pro-high")
		done <- err
	}()
	_, err := io.WriteString(
		writer,
		`data: {"response":{"responseId":"resp_3757","candidates":[{"content":{"parts":[{"text":"partial"}]}}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":1}}}`+"\n\n",
	)
	require.NoError(t, err)
	time.Sleep(1200 * time.Millisecond)
	require.NoError(t, writer.Close())
	require.NoError(t, <-done)
	require.Contains(t, recorder.Body.String(), ": ping\n\n")
	require.Contains(t, recorder.Header().Get("Content-Type"), "text/event-stream")
	require.NoError(t, reader.Close())
}
