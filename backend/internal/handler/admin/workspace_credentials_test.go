package admin

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceCredentialBrowserProofUsesWorkspaceCookiePath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(http.MethodPost, "https://panel.example.invalid/api/v1/workspaces/2/accounts/credential-drafts", nil)
	ctx.Request = ctx.Request.WithContext(domain.WithWorkspaceScope(ctx.Request.Context(), 2))
	ctx.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
	binding, err := credentialDraftBinding(ctx, true)
	require.NoError(t, err)
	cookies := response.Result().Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, "/api/v1/workspaces/2", cookies[0].Path)
	require.True(t, cookies[0].HttpOnly)
	require.Equal(t, http.SameSiteStrictMode, cookies[0].SameSite)
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	jar.SetCookies(ctx.Request.URL, cookies)
	for _, path := range []string{"/api/v1/admin/accounts", "/api/v1/workspaces/3/accounts"} {
		target, err := url.Parse("https://panel.example.invalid" + path)
		require.NoError(t, err)
		require.Empty(t, jar.Cookies(target))
	}
	nextRequest := httptest.NewRequest(http.MethodPost, "https://panel.example.invalid/api/v1/workspaces/2/accounts", nil)
	nextRequest = nextRequest.WithContext(domain.WithWorkspaceScope(nextRequest.Context(), 2))
	for _, cookie := range jar.Cookies(nextRequest.URL) {
		nextRequest.AddCookie(cookie)
	}
	ctx.Request = nextRequest
	replayed, err := credentialDraftBinding(ctx, false)
	require.NoError(t, err)
	require.Equal(t, binding, replayed, "browser must retain proof from draft issue through final scoped creation")
}
