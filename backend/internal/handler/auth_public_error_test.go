package handler

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOAuthRedirectKeepsReasonWithoutProviderRawResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "/api/v1/auth/oauth/provider/callback", nil)
	diagnostic := `HTTP/1.1 502 provider body Authorization: Bearer credential-secret https://private.internal?key=credential-secret`
	redirectOAuthError(ctx, "/auth/callback", "token_exchange_failed", "failed to exchange oauth code", diagnostic)
	location, err := url.Parse(recorder.Header().Get("Location"))
	require.NoError(t, err)
	fields, err := url.ParseQuery(location.Fragment)
	require.NoError(t, err)
	require.Equal(t, "token_exchange_failed", fields.Get("error"))
	require.Equal(t, "failed to exchange oauth code", fields.Get("error_message"))
	require.Equal(t, "OAuth provider request failed", fields.Get("error_description"))
	require.NotContains(t, recorder.Header().Get("Location"), "credential-secret")
	require.NotContains(t, recorder.Header().Get("Location"), "private.internal")
	require.NotContains(t, truncateLogValue(`{"api_key":"`+strings.Repeat("credential-secret", 500)+`"}`, 40), "credential-secret")
}
