package service

import (
	"bytes"
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
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/antigravity"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAntigravityMessagesExplicitEffortUnavailable(t *testing.T) {
	for _, model := range []string{"gemini-3-flash", "gemini-3-flash-high", "public-flash"} {
		t.Run(model, func(t *testing.T) {
			account := approvedAntigravityVariantAccount()
			account.Credentials["model_mapping"] = map[string]any{"gemini-3-flash-high": "gemini-3-flash-high"}
			account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{"gemini-3-flash-high"}}
			if model == "public-flash" {
				account.Credentials["model_mapping"] = map[string]any{model: "gemini-3-flash-high"}
			}
			body := []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"output_config":{"effort":"low"}}`, model))
			writer := httptest.NewRecorder()
			client, _ := gin.CreateTestContext(writer)
			client.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
			// No token provider or upstream: this must fail locally even before OAuth.
			_, err := (&AntigravityGatewayService{}).Forward(context.Background(), client, account, body, false)
			require.Error(t, err)
			require.Equal(t, http.StatusBadRequest, writer.Code, writer.Body.String())
			require.Contains(t, writer.Body.String(), "output_config.effort")
			require.Contains(t, writer.Body.String(), "low")
		})
	}
}

func TestAntigravityMessagesEffortPreservesSignedContentsAndTools(t *testing.T) {
	request := &antigravity.ClaudeRequest{
		Model:    "gemini-3-flash-high",
		Thinking: &antigravity.ThinkingConfig{Type: "adaptive"},
		Messages: []antigravity.ClaudeMessage{
			{Role: "assistant", Content: json.RawMessage(`[{"type":"thinking","thinking":"signed thought","signature":"signed-payload"},{"type":"tool_use","id":"call-fixture","name":"lookup","input":{"query":"hello"}}]`)},
			{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"call-fixture","content":"result"}]`)},
		},
		Tools: []antigravity.ClaudeTool{{Name: "lookup", InputSchema: map[string]any{"type": "object"}}},
	}
	options := antigravity.DefaultTransformOptions()
	baseline, err := antigravity.TransformClaudeToGeminiWithOptions(request, "fixture", "gemini-3-flash-low", options)
	require.NoError(t, err)
	withEffort, err := transformClaudeToGeminiWithThinkingLevel(request, "fixture", "gemini-3-flash-low", "low", options)
	require.NoError(t, err)
	for _, path := range []string{"request.contents", "request.tools", "request.toolConfig", "request.systemInstruction"} {
		require.JSONEq(t, gjson.GetBytes(baseline, path).Raw, gjson.GetBytes(withEffort, path).Raw, path)
	}
	require.Equal(t, "LOW", gjson.GetBytes(withEffort, "request.generationConfig.thinkingConfig.thinkingLevel").String())
	require.False(t, gjson.GetBytes(withEffort, "request.generationConfig.thinkingConfig.thinkingBudget").Exists())
}

func TestAntigravityMessagesEffortRejectsToolModelDowngrade(t *testing.T) {
	request := &antigravity.ClaudeRequest{
		Model:    "gemini-3-flash-high",
		Messages: []antigravity.ClaudeMessage{{Role: "user", Content: json.RawMessage(`"search for hello"`)}},
		Tools:    []antigravity.ClaudeTool{{Type: "web_search_20250305", Name: "web_search"}},
	}
	_, err := transformClaudeToGeminiWithThinkingLevel(request, "fixture", "gemini-3-flash-low", "low", antigravity.DefaultTransformOptions())
	require.ErrorContains(t, err, "output_config.effort")
	legacy, err := transformClaudeToGeminiWithThinkingLevel(request, "fixture", "gemini-3-flash-high", "", antigravity.DefaultTransformOptions())
	require.NoError(t, err)
	require.Equal(t, "gemini-2.5-flash", gjson.GetBytes(legacy, "model").String())
}

func TestAntigravityNativeThinkingControlsRemainUnchanged(t *testing.T) {
	for _, controls := range []string{`{"thinkingBudget":0}`, `{"thinkingBudget":4000}`, `{"thinkingBudget":-1}`, `{"thinkingLevel":"LOW"}`} {
		t.Run(controls, func(t *testing.T) {
			account := approvedAntigravityVariantAccount()
			account.Credentials["model_mapping"] = map[string]any{"gemini-3-flash-high": "gemini-3-flash-high"}
			account.Credentials["access_token"] = "fixture"
			account.Credentials["project_id"] = "fixture"
			body := []byte(fmt.Sprintf(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"thinkingConfig":%s}}`, controls))
			writer := httptest.NewRecorder()
			client, _ := gin.CreateTestContext(writer)
			client.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3-flash-high:generateContent", bytes.NewReader(body))
			upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}]}}\n\n"))}}}
			gateway := &AntigravityGatewayService{tokenProvider: &AntigravityTokenProvider{}, httpUpstream: upstream, settingService: NewSettingService(&antigravitySettingRepoStub{}, &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}})}
			result, err := gateway.ForwardGemini(context.Background(), client, account, "gemini-3-flash-high", "generateContent", false, body, false)
			require.NoError(t, err)
			require.Equal(t, "gemini-3-flash-high", result.Model)
			require.Len(t, upstream.requestBodies, 1)
			require.Equal(t, "gemini-3-flash-high", gjson.GetBytes(upstream.requestBodies[0], "model").String())
			require.JSONEq(t, controls, gjson.GetBytes(upstream.requestBodies[0], "request.generationConfig.thinkingConfig").Raw)
		})
	}
}

func TestAntigravityMessagesEffortDoesNotAuthorizeOriginalModel(t *testing.T) {
	account := approvedAntigravityVariantAccount()
	account.Credentials["model_mapping"] = map[string]any{"gemini-3-flash-low": "gemini-3-flash-low"}
	body := []byte(`{"model":"unapproved-high","messages":[{"role":"user","content":"hello"}],"output_config":{"effort":"low"}}`)
	writer := httptest.NewRecorder()
	client, _ := gin.CreateTestContext(writer)
	client.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	_, err := (&AntigravityGatewayService{}).Forward(context.Background(), client, account, body, false)
	require.Error(t, err)
	require.Equal(t, http.StatusForbidden, writer.Code)
	require.Contains(t, writer.Body.String(), "unapproved-high")
}
