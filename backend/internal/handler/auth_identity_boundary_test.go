package handler

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOAuthCompletionCodeIsBrowserBoundAndConsumedOnce(t *testing.T) {
	handler, client := newOAuthPendingFlowTestHandler(t, false)
	user, err := client.User.Create().SetEmail("completion@example.com").SetPasswordHash("hash").Save(context.Background())
	require.NoError(t, err)
	domainUser, err := handler.userService.GetByID(context.Background(), user.ID)
	require.NoError(t, err)
	pair, err := handler.authService.GenerateTokenPair(context.Background(), domainUser, "")
	require.NoError(t, err)
	callback := httptest.NewRecorder()
	callbackContext, _ := gin.CreateTestContext(callback)
	callbackContext.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/oidc/callback", nil)
	handler.redirectOAuthTokenPair(callbackContext, "/auth/oidc/callback", pair, "/dashboard")
	require.Contains(t, callback.Header().Get("Location"), "code=")
	require.NotContains(t, callback.Header().Get("Location"), "access_token")
	require.NotContains(t, callback.Header().Get("Location"), "refresh_token")
	exchange := func(wrongBrowser bool, suppliedCode string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		requestContext, _ := gin.CreateTestContext(recorder)
		requestContext.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/oauth/pending/exchange", strings.NewReader(`{"code":"`+suppliedCode+`"}`))
		for _, cookie := range callback.Result().Cookies() {
			if cookie.MaxAge < 0 {
				continue
			}
			if wrongBrowser && cookie.Name == oauthPendingBrowserCookieName {
				cookie.Value = encodeCookieValue("other")
			}
			requestContext.Request.AddCookie(cookie)
		}
		handler.ExchangePendingOAuthCompletion(requestContext)
		return recorder
	}
	code := findCookie(callback.Result().Cookies(), "oauth_completion_code").Value
	require.Equal(t, http.StatusUnauthorized, exchange(false, "wrong-code").Code)
	require.Equal(t, http.StatusUnauthorized, exchange(false, "").Code)
	require.Equal(t, http.StatusUnauthorized, exchange(true, code).Code)
	completed := exchange(false, code)
	require.Equal(t, http.StatusOK, completed.Code, completed.Body.String())
	var body struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(completed.Body.Bytes(), &body))
	require.NotEmpty(t, body.Data["access_token"])
	require.Equal(t, "/dashboard", body.Data["redirect"])
	require.Equal(t, http.StatusUnauthorized, exchange(false, code).Code)
}

func TestOAuthBindStartRequiresOneTimeCSRFReference(t *testing.T) {
	handler, _ := newOAuthPendingFlowTestHandler(t, false)
	recorder := httptest.NewRecorder()
	prepared, _ := gin.CreateTestContext(recorder)
	prepared.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/oidc/bind/start?intent=bind_current_user", nil)
	prepareOAuthBindStart(t, handler, prepared, 42)
	attempt := func(withCode bool) bool {
		requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
		requestContext.Request = prepared.Request.Clone(prepared.Request.Context())
		if !withCode {
			query := requestContext.Request.URL.Query()
			query.Del("csrf_token")
			requestContext.Request.URL.RawQuery = query.Encode()
		}
		return handler.requireActionCaptchaForOAuthLoginStart(requestContext)
	}
	require.False(t, attempt(false), "ambient browser cookie is not authorization to bind")
	require.True(t, attempt(true))
	require.False(t, attempt(true), "a copied start request cannot be replayed")
}

func TestOAuthNewFlowClearsAbandonedCompletionCode(t *testing.T) {
	handler, _ := newOAuthPendingFlowTestHandler(t, false)
	recorder := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(recorder)
	requestContext.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/oidc/start", nil)
	requestContext.Request.AddCookie(&http.Cookie{Name: "oauth_completion_code", Value: "abandoned-code"})
	state, err := handler.issueOAuthState(requestContext, "oidc")
	require.NoError(t, err)
	require.NotEmpty(t, state)
	cleared := findCookie(recorder.Result().Cookies(), "oauth_completion_code")
	require.NotNil(t, cleared)
	require.Equal(t, -1, cleared.MaxAge)
}
