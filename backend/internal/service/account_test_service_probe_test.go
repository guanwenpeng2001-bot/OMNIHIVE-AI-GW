package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

func TestProbeProtocolCompletion(t *testing.T) {
	s := &AccountTestService{}
	protocols := []struct {
		name    string
		parse   func(*accountProbeContext, io.Reader) error
		valid   string
		partial string
	}{
		{"claude", s.processClaudeStream, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"content\":[]}}\n\ndata: {\"type\":\"message_stop\"}", "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"hello\"}}\n\n"},
		{"responses", s.processOpenAIStream, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[]}}", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n"},
		{"chat", s.processOpenAIChatCompletionsStream, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"id\":\"call_1\"}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"},
		{"gemini", s.probeProcessGeminiStream, "data: {\"candidates\":[{\"content\":{\"parts\":[]},\"finishReason\":\"STOP\"}]}", "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}]}\n\n"},
	}
	for _, protocol := range protocols {
		t.Run(protocol.name, func(t *testing.T) {
			for _, fixture := range []struct {
				name, body string
				success    bool
			}{
				{"empty", "", false}, {"done_only", "data: [DONE]\n\n", false}, {"partial", protocol.partial, false}, {"partial_done", protocol.partial + "data: [DONE]\n\n", false}, {"empty_or_tool_terminal", protocol.valid, true},
			} {
				t.Run(fixture.name, func(t *testing.T) {
					completed := false
					c := &accountProbeContext{ctx: context.Background(), emit: func(event TestEvent) {
						if event.Type == "test_complete" {
							completed = event.Success
						}
					}}
					err := protocol.parse(c, strings.NewReader(fixture.body))
					require.Equal(t, fixture.success, err == nil)
					require.Equal(t, fixture.success, completed)
				})
			}
		})
	}
}

func TestProbeForegroundAndBackgroundAgree(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, fixture := range []struct {
		name, body string
		success    bool
	}{
		{"empty", "", false}, {"done_only", "data: [DONE]\n\n", false},
		{"partial", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n", false},
		{"empty_terminal", "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_fixture\",\"status\":\"completed\",\"output\":[]}}\n\n", true},
		{"tool_terminal", "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_fixture\",\"status\":\"completed\",\"output\":[{\"type\":\"function_call\",\"name\":\"ping\",\"arguments\":\"{}\"}]}}\n\n", true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			account := Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key"}}
			makeService := func() *AccountTestService {
				return &AccountTestService{cfg: &config.Config{}, accountRepo: &stubOpenAIAccountRepo{accounts: []Account{account}}, httpUpstream: &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fixture.body))}}}
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/test", nil)
			frontErr := makeService().TestAccountConnection(c, 1, "gpt-5.4", "", "")
			background, err := makeService().RunTestBackground(context.Background(), 1, "gpt-5.4")
			require.NoError(t, err)
			require.Equal(t, fixture.success, frontErr == nil)
			require.Equal(t, fixture.success, background.Status == "success")
		})
	}
}

func TestUnsupportedMediaProbeDoesNotCallUpstream(t *testing.T) {
	account := Account{ID: 1, Platform: PlatformQianwen, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": map[string]any{"MyImage": "qwen-image"}}}
	s := &AccountTestService{accountRepo: &stubOpenAIAccountRepo{accounts: []Account{account}}}
	result := s.ProbeAccount(context.Background(), AccountProbeInput{AccountID: 1, ModelID: "MyImage"}, nil)
	require.False(t, result.Success)
	require.Equal(t, "unsupported", result.Code)
	require.Contains(t, result.Error, "unsupported")
}
