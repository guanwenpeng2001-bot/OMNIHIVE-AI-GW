package handler

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
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
	exchange := func(wrongBrowser bool) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		requestContext, _ := gin.CreateTestContext(recorder)
		requestContext.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/oauth/pending/exchange", nil)
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
	require.Equal(t, http.StatusUnauthorized, exchange(true).Code)
	completed := exchange(false)
	require.Equal(t, http.StatusOK, completed.Code, completed.Body.String())
	var body struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(completed.Body.Bytes(), &body))
	require.NotEmpty(t, body.Data["access_token"])
	require.Equal(t, "/dashboard", body.Data["redirect"])
	require.Equal(t, http.StatusUnauthorized, exchange(false).Code)
}
