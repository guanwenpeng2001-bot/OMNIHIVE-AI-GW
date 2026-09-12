package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type cursorCountTokensTransport struct{ hosts []string }

func (r *cursorCountTokensTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.hosts = append(r.hosts, req.URL.Hostname())
	return nil, fmt.Errorf("unexpected outbound request")
}

func TestCursorCountTokensLocalWithoutAnthropicRequests(t *testing.T) {
	transport := &cursorCountTokensTransport{}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	// No gateway service, account repository, or credentials are needed locally.
	h := &OpenAIGatewayHandler{}
	router := gin.New()
	router.POST("/v1/messages/count_tokens", h.CursorCountTokens)
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"valid", `{"model":"claude-sonnet-4-6","system":"Be concise","messages":[{"role":"user","content":"hello"}],"tools":[{"name":"search","input_schema":{"type":"object"}}]}`, http.StatusOK},
		{"missing_model", `{"messages":[]}`, http.StatusBadRequest},
		{"invalid_json", `{`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(rec, req)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			var result map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
			if tc.status == http.StatusOK {
				require.Greater(t, result["input_tokens"].(float64), float64(0))
			} else {
				require.Equal(t, "error", result["type"])
			}
		})
	}
	require.NotContains(t, transport.hosts, "api.anthropic.com")
	require.Empty(t, transport.hosts)
}
