//go:build unit

package service

import (
	"context"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCursorRelayProbeChecksConversationAndSelectedModel(t *testing.T) {
	for _, scenario := range []string{"chat", "responses", "anthropic", "failure", "empty", "invalid", "compact", "fallback"} {
		t.Run(scenario, func(t *testing.T) {
			calls, catalogs := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/models") {
					catalogs++
					_, _ = io.WriteString(w, `{"data":[{"id":"public"}]}`)
					return
				}
				calls++
				require.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, "chosen", body["model"])
				require.Equal(t, false, body["stream"])
				switch scenario {
				case "failure":
					w.WriteHeader(401)
					_, _ = io.WriteString(w, "private-body")
				case "empty":
					_, _ = io.WriteString(w, `{"choices":[{"message":{"content":""}}]}`)
				case "invalid":
					_, _ = io.WriteString(w, `{`)
				case "responses":
					require.Equal(t, "/v1/responses", r.URL.Path)
					_, _ = io.WriteString(w, `{"id":"r","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}]}`)
				case "anthropic":
					require.Equal(t, "/v1/messages", r.URL.Path)
					_, _ = io.WriteString(w, `{"id":"m","type":"message","role":"assistant","content":[{"type":"text","text":"OK"}]}`)
				default:
					require.Equal(t, "/v1/chat/completions", r.URL.Path)
					_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"OK"}}]}`)
				}
			}))
			defer server.Close()
			gateway, account, _ := cursorTestGateway()
			gateway.relayClient = server.Client()
			account.Type = AccountTypeUpstream
			account.Credentials = map[string]any{"base_url": server.URL, "api_key": "secret", "model_mapping": map[string]any{"public": "chosen"}}
			if scenario == "responses" {
				account.Credentials["api_protocol"] = APIProtocolResponses
			}
			if scenario == "anthropic" {
				account.Credentials["api_protocol"] = APIProtocolAnthropic
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("POST", "/test", nil)
			model, mode := "public", "default"
			if scenario == "fallback" {
				model = ""
			}
			if scenario == "compact" {
				mode = "compact"
			}
			err := (&AccountTestService{cursorGatewayService: gateway}).testCursorAccountConnection(c, account, model, mode)
			if scenario == "failure" || scenario == "empty" || scenario == "invalid" || scenario == "compact" {
				require.Error(t, err)
				require.NotContains(t, recorder.Body.String(), `"success":true`)
			} else {
				require.NoError(t, err)
				require.Contains(t, recorder.Body.String(), `"success":true`)
			}
			if scenario == "compact" {
				require.Zero(t, calls)
			} else {
				require.Equal(t, 1, calls)
			}
			if scenario == "fallback" {
				require.Equal(t, 1, catalogs)
			} else {
				require.Zero(t, catalogs)
			}
			require.NotContains(t, recorder.Body.String(), "private-body")
		})
	}
}

func TestCursorNativeProbeUsesSelectedModel(t *testing.T) {
	gateway, account, _ := cursorTestGateway()
	account.Credentials["model_mapping"] = map[string]any{"selected": "live"}
	gateway.native = cursorProbeDriver{models: []cursor.AvailableModel{{ID: "first", RunSlug: "wrong"}, {ID: "live", RunSlug: "live-run"}}, cursorDriverStub: cursorDriverStub{run: func(_ context.Context, _ cursor.Credentials, _ []cursor.ChatMessage, model string, emit func(cursor.StreamEvent) error) (cursor.TokenUsage, error) {
		require.Equal(t, "live-run", model)
		return cursor.TokenUsage{}, emit(cursor.StreamEvent{Type: "text", Text: "OK"})
	}}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/test", nil)
	require.NoError(t, (&AccountTestService{cursorGatewayService: gateway}).testCursorAccountConnection(c, account, "selected", "text"))
}
