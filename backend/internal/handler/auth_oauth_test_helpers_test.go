package handler

import (
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func buildEncodedOAuthBindUserCookie(t *testing.T, userID int64, secret string) string {
	t.Helper()
	value, err := buildOAuthBindUserCookieValue(userID, secret)
	require.NoError(t, err)
	return value
}

func encodedCookie(name, value string) *http.Cookie {
	return &http.Cookie{
		Name:  name,
		Value: encodeCookieValue(value),
		Path:  "/",
	}
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func requireCookieCleared(t *testing.T, recorder *httptest.ResponseRecorder, name string) {
	t.Helper()
	cookie := findCookie(recorder.Result().Cookies(), name)
	require.NotNil(t, cookie)
	require.Equal(t, -1, cookie.MaxAge)
}

func decodeCookieValueForTest(t *testing.T, value string) string {
	t.Helper()
	decoded, err := decodeCookieValue(value)
	require.NoError(t, err)
	return decoded
}

func assertOAuthRedirectError(t *testing.T, location string, errorCode string, errorMessage string) {
	t.Helper()
	values := parseOAuthRedirectFragment(t, location)
	require.Equal(t, errorCode, values.Get("error"))
	require.Equal(t, errorMessage, values.Get("error_message"))
}

func parseOAuthRedirectFragment(t *testing.T, location string) url.Values {
	t.Helper()
	require.NotEmpty(t, location)

	parsed, err := url.Parse(location)
	require.NoError(t, err)

	rawValues := parsed.RawQuery
	if rawValues == "" {
		rawValues = parsed.Fragment
	}
	values, err := url.ParseQuery(rawValues)
	require.NoError(t, err)
	return values
}

// Legacy callback fixtures now model a state issued by the server, preserving
// mismatched/missing-state cases as negative tests.
func prepareOAuthCallbackState(t *testing.T, handler *AuthHandler, requestContext *gin.Context, provider, cookieName string) {
	t.Helper()
	expected, err := readCookieDecoded(requestContext, cookieName)
	if err != nil || expected == "" || expected != requestContext.Request.URL.Query().Get("state") {
		return
	}
	state, err := handler.issueOAuthState(requestContext, provider)
	require.NoError(t, err)
	query := requestContext.Request.URL.Query()
	query.Set("state", state)
	requestContext.Request.URL.RawQuery = query.Encode()
	cookies := requestContext.Request.Cookies()
	requestContext.Request.Header.Del("Cookie")
	for _, cookie := range cookies {
		if cookie.Name == cookieName {
			cookie.Value = encodeCookieValue(state)
		}
		requestContext.Request.AddCookie(cookie)
	}
}

func prepareOAuthBindStart(t *testing.T, handler *AuthHandler, requestContext *gin.Context, userID int64) int64 {
	t.Helper()
	client := handler.authService.EntClient()
	user, err := client.User.Get(requestContext.Request.Context(), userID)
	if err != nil {
		user, err = client.User.Create().SetEmail(fmt.Sprintf("bind-%d@example.com", userID)).SetPasswordHash("hash").SetRole(service.RoleUser).SetStatus(service.StatusActive).Save(requestContext.Request.Context())
		require.NoError(t, err)
	}
	domainUser, err := handler.userService.GetByID(requestContext.Request.Context(), user.ID)
	require.NoError(t, err)
	token, err := handler.authService.GenerateToken(requestContext.Request.Context(), domainUser)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	prepareContext, _ := gin.CreateTestContext(recorder)
	prepareContext.Request = requestContext.Request.Clone(requestContext.Request.Context())
	prepareContext.Request.Header = requestContext.Request.Header.Clone()
	prepareContext.Request.Header.Set("Authorization", "Bearer "+token)
	handler.PrepareOAuthBindAccessTokenCookie(prepareContext)
	require.Equal(t, http.StatusOK, recorder.Code)
	var result struct {
		Data struct {
			CSRFToken string `json:"csrf_token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
	query := requestContext.Request.URL.Query()
	query.Set("csrf_token", result.Data.CSRFToken)
	requestContext.Request.URL.RawQuery = query.Encode()
	requestContext.Request.Header.Del("Cookie")
	for _, cookie := range recorder.Result().Cookies() {
		requestContext.Request.AddCookie(cookie)
	}
	return user.ID
}
