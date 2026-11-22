//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestMessagesUpstreamPublicError(t *testing.T) {
	upstream := &bedrockHandlerUpstream{responder: func(r *http.Request) *http.Response {
		return &http.Response{StatusCode: 500, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader("private upstream diagnostic")), Request: r}
	}}
	handler := &GatewayHandler{antigravityGatewayService: service.NewAntigravityGatewayService(nil, nil, nil, nil, nil, upstream, nil, nil)}
	account := &service.Account{ID: 1, Type: service.AccountTypeUpstream, Platform: service.PlatformAntigravity, Credentials: map[string]any{"api_key": "test", "base_url": "https://example.test"}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	result, err := handler.forwardAntigravityMessages(context.Background(), c, account, []byte(`{"model":"test","messages":[]}`), false)
	require.Error(t, err)
	require.Nil(t, result)
	require.Len(t, upstream.requests, 1)
	require.GreaterOrEqual(t, rec.Code, 400)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	require.NotContains(t, rec.Body.String(), "private upstream diagnostic")
}
