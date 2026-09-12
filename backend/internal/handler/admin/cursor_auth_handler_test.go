//go:build unit

package admin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type cursorAuthorizationAdminStub struct {
	service.AdminService
	account        *service.Account
	writes, clears int
}

func (stub *cursorAuthorizationAdminStub) GetAccount(context.Context, int64) (*service.Account, error) {
	return stub.account, nil
}
func (stub *cursorAuthorizationAdminStub) UpdateAccount(_ context.Context, _ int64, input *service.UpdateAccountInput) (*service.Account, error) {
	stub.writes++
	stub.account.Credentials = input.Credentials
	return stub.account, nil
}
func (stub *cursorAuthorizationAdminStub) ClearAccountError(context.Context, int64) (*service.Account, error) {
	stub.clears++
	return nil, errors.New("synthetic cleanup failure after commit")
}

type cursorPollTransport func(*http.Request) (*http.Response, error)

func (transport cursorPollTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestCursorAuthorizationEditDoesNotRunGenericCleanup(t *testing.T) {
	previousTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	token := "synthetic." + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, time.Now().Add(time.Hour).Unix()))) + ".signature"
	http.DefaultTransport = cursorPollTransport(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "api2.cursor.sh", request.URL.Host)
		require.Equal(t, "/auth/poll", request.URL.Path)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"accessToken":%q,"refreshToken":"synthetic-browser-refresh"}`, token)))}, nil
	})
	stub := &cursorAuthorizationAdminStub{account: &service.Account{ID: 42, Platform: service.PlatformCursor, Type: service.AccountTypeOAuth, Status: service.StatusError, ErrorMessage: "manual review required", Credentials: map[string]any{"access_token": "synthetic-old"}}}
	handler := &AccountHandler{adminService: stub, cursorAuthService: service.NewCursorAuthService()}
	router := gin.New()
	router.POST("/start", handler.StartCursorAuthorization)
	router.GET("/poll/:session", handler.PollCursorAuthorization)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/start", strings.NewReader(`{"account_id":42}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	var envelope struct {
		Data service.CursorAuthorizationStatus `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	id := envelope.Data.ID
	require.NotEmpty(t, id)
	for range 2 {
		response = httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/poll/"+id, nil))
		require.Equal(t, http.StatusOK, response.Code)
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
		require.Equal(t, "success", envelope.Data.Status)
	}
	require.Equal(t, 1, stub.writes)
	require.Zero(t, stub.clears)
	require.Equal(t, token, stub.account.GetCredential("access_token"))
	require.Equal(t, service.StatusError, stub.account.Status)
	require.Equal(t, "manual review required", stub.account.ErrorMessage)
}
