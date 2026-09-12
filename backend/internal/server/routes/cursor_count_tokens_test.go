package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGatewayRoutesCursorCountTokensUsesLocalEstimator(t *testing.T) {
	// Gateway dependencies are deliberately absent: reaching the Anthropic handler
	// would fail instead of quietly passing this route regression test.
	router := newGatewayRoutesTestRouter(service.PlatformCursor)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var result struct {
		InputTokens int `json:"input_tokens"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.Positive(t, result.InputTokens)
}
