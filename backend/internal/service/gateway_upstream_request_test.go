package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

func TestBuildUpstreamRequestRejectsGemini(t *testing.T) {
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth, AccountTypeSetupToken, AccountTypeServiceAccount} {
		t.Run(accountType, func(t *testing.T) {
			account := &Account{Platform: PlatformGemini, Type: accountType}
			// Even a caller supplying a token directly must be rejected before
			// constructing an authenticated Anthropic request.
			gateway := &GatewayService{cfg: &config.Config{}}
			request, wireBody, err := gateway.buildUpstreamRequest(context.Background(), nil, account,
				[]byte(`{"model":"gemini-2.5-flash"}`), "fictional-gemini-secret", "oauth", "gemini-2.5-flash", false, false)
			require.ErrorContains(t, err, "gemini accounts cannot use the Anthropic gateway")
			require.Nil(t, request)
			require.Nil(t, wireBody)
		})
	}
}

func TestForwardAsResponsesRejectsGeminiWithoutOutboundRequest(t *testing.T) {
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth, AccountTypeSetupToken} {
		t.Run(accountType, func(t *testing.T) {
			account := &Account{ID: 703, Platform: PlatformGemini, Type: accountType,
				Credentials: map[string]any{"api_key": "fictional-gemini-secret", "access_token": "fictional-gemini-secret"}}
			upstream := &geminiCompatHTTPUpstreamStub{}
			gateway := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			recorder := httptest.NewRecorder()
			ginContext, _ := gin.CreateTestContext(recorder)
			ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			result, err := gateway.ForwardAsResponses(context.Background(), ginContext, account,
				[]byte(`{"model":"gemini-2.5-flash","input":"hello"}`), nil)
			require.ErrorContains(t, err, "gemini accounts cannot use the Anthropic gateway")
			require.Nil(t, result)
			require.Nil(t, upstream.lastReq, "rejection must precede the transport call")
			require.Zero(t, upstream.calls)
			require.NotContains(t, recorder.Body.String(), "fictional-gemini-secret")
		})
	}
}

func TestBuildUpstreamRequestPreservesAnthropicAccounts(t *testing.T) {
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth, AccountTypeSetupToken} {
		t.Run(accountType, func(t *testing.T) {
			gateway := &GatewayService{cfg: &config.Config{}}
			account := &Account{Platform: PlatformAnthropic, Type: accountType}
			request, _, err := gateway.buildUpstreamRequest(context.Background(), nil, account,
				[]byte(`{"model":"claude-sonnet-4-6"}`), "fictional-anthropic-secret", "oauth", "claude-sonnet-4-6", false, false)
			require.NoError(t, err)
			require.Equal(t, "api.anthropic.com", request.URL.Hostname())
			require.Equal(t, "/v1/messages", request.URL.Path)
		})
	}
}
