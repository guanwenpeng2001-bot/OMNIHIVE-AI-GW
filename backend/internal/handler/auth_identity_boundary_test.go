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

func TestOAuthCompletionCodeIsBrowserBoundAndConsumedOnce(testingT *testing.T) {
	handler, client := newOAuthPendingFlowTestHandler(testingT, false)
	user, err := client.User.Create().SetEmail("completion@example.com").SetPasswordHash("hash").Save(context.Background())
	require.NoError(testingT, err)
	domainUser, err := handler.userService.GetByID(context.Background(), user.ID)
	require.NoError(testingT, err)
	pair, err := handler.authService.GenerateTokenPair(context.Background(), domainUser, "")
	require.NoError(testingT, err)
	callback := httptest.NewRecorder()
	callbackContext, _ := gin.CreateTestContext(callback)
	callbackContext.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/oidc/callback", nil)
	handler.redirectOAuthTokenPair(callbackContext, "/auth/oidc/callback", pair, "/dashboard")
	require.Contains(testingT, callback.Header().Get("Location"), "code=")
	require.NotContains(testingT, callback.Header().Get("Location"), "access_token")
	require.NotContains(testingT, callback.Header().Get("Location"), "refresh_token")
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
	require.Equal(testingT, http.StatusUnauthorized, exchange(false, "wrong-code").Code)
	require.Equal(testingT, http.StatusUnauthorized, exchange(false, "").Code)
	require.Equal(testingT, http.StatusUnauthorized, exchange(true, code).Code)
	completed := exchange(false, code)
	require.Equal(testingT, http.StatusOK, completed.Code, completed.Body.String())
	var body struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(testingT, json.Unmarshal(completed.Body.Bytes(), &body))
	require.NotEmpty(testingT, body.Data["access_token"])
	require.Equal(testingT, "/dashboard", body.Data["redirect"])
	require.Equal(testingT, http.StatusUnauthorized, exchange(false, code).Code)
}

func TestOAuthBindStartRequiresOneTimeCSRFReference(testingT *testing.T) {
	handler, _ := newOAuthPendingFlowTestHandler(testingT, false)
	recorder := httptest.NewRecorder()
	prepared, _ := gin.CreateTestContext(recorder)
	prepared.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/oidc/bind/start?intent=bind_current_user", nil)
	prepareOAuthBindStart(testingT, handler, prepared, 42)
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
	require.False(testingT, attempt(false), "ambient browser cookie is not authorization to bind")
	require.True(testingT, attempt(true))
	require.False(testingT, attempt(true), "a copied start request cannot be replayed")
}

func TestOAuthNewFlowClearsAbandonedCompletionCode(testingT *testing.T) {
	handler, _ := newOAuthPendingFlowTestHandler(testingT, false)
	recorder := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(recorder)
	requestContext.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/oidc/start", nil)
	requestContext.Request.AddCookie(&http.Cookie{Name: "oauth_completion_code", Value: "abandoned-code"})
	state, err := handler.issueOAuthState(requestContext, "oidc")
	require.NoError(testingT, err)
	require.NotEmpty(testingT, state)
	cleared := findCookie(recorder.Result().Cookies(), "oauth_completion_code")
	require.NotNil(testingT, cleared)
	require.Equal(testingT, -1, cleared.MaxAge)
}
