//go:build unit

package service

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/cursor"
	"github.com/stretchr/testify/require"
)

func cursorTestProxy(t *testing.T, address string, identifier int64) *Proxy {
	t.Helper()
	parsed, err := url.Parse(address)
	require.NoError(t, err)
	hostname, portText, err := net.SplitHostPort(parsed.Host)
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)
	return &Proxy{ID: identifier, Protocol: parsed.Scheme, Host: hostname, Port: port}
}

func TestCursorRelayAccountProxyAndCatalogHeaders(t *testing.T) {
	gateway := NewCursorGatewayService(nil, nil)
	var direct atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { direct.Add(1) }))
	defer origin.Close()
	for _, key := range []string{"account-alpha", "account-beta"} {
		t.Run(key, func(t *testing.T) {
			var requests atomic.Int32
			proxyServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requests.Add(1)
				require.Equal(t, "Bearer "+key, request.Header.Get("Authorization"))
				require.Equal(t, key, request.Header.Get("X-Route"))
				require.Empty(t, request.Header.Get("X-Api-Key"))
				require.Empty(t, request.Header.Get("Connection"))
				if request.URL.Path == "/v1/models" {
					io.WriteString(writer, `{"data":[{"id":"test-model"}]}`)
				} else {
					io.WriteString(writer, cursorRelayFixture(APIProtocolChatCompletions, false))
				}
			}))
			defer proxyServer.Close()
			configured := cursorTestProxy(t, proxyServer.URL, 17)
			account := &Account{ID: 17, Platform: PlatformCursor, Type: AccountTypeUpstream, ProxyID: &configured.ID, Proxy: configured, Credentials: map[string]any{"base_url": origin.URL, "api_key": key, credKeyHeaderOverrideEnabled: true, credKeyHeaderOverrides: map[string]any{"X-Route": key, "Host": "bad", "Connection": "close", "Content-Length": "999", "AUTHORIZATION": "bad"}}}
			models, err := gateway.AvailableModels(context.Background(), account)
			require.NoError(t, err)
			require.Equal(t, []string{"test-model"}, models)
			requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
			_, err = callCursorProtocol(gateway, requestContext, account, cursorProtocolRequest(APIProtocolChatCompletions, false), APIProtocolChatCompletions)
			require.NoError(t, err)
			require.EqualValues(t, 2, requests.Load())
			proxyServer.Close()
			_, err = gateway.AvailableModels(context.Background(), account)
			require.Error(t, err)
			require.Zero(t, direct.Load())
		})
	}
}

func TestCursorAccountProxyAllNativeServiceFamilies(t *testing.T) {
	for _, family := range []string{"exchange", "refresh", "models", "usage"} {
		t.Run(family, func(t *testing.T) {
			var tunnels atomic.Int32
			proxyServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				tunnels.Add(1)
				require.Equal(t, http.MethodConnect, request.Method)
				require.Equal(t, "api2.cursor.sh:443", request.Host)
				writer.WriteHeader(http.StatusProxyAuthRequired)
			}))
			defer proxyServer.Close()
			configured := cursorTestProxy(t, proxyServer.URL, 42)
			account := &Account{ID: 42, Platform: PlatformCursor, Type: AccountTypeAPIKey, ProxyID: &configured.ID, Proxy: configured, Credentials: map[string]any{"api_key": "synthetic-key", "refresh_token": "synthetic-refresh"}}
			switch family {
			case "exchange":
				_, err := NewCursorSessionProvider(newCursorMemoryCache()).GetSessionToken(context.Background(), account, "")
				require.Error(t, err)
			case "refresh":
				account.Type = AccountTypeOAuth
				_, err := NewCursorTokenRefresher().Refresh(context.Background(), account)
				require.Error(t, err)
			case "models":
				sessions := NewCursorSessionProvider(newCursorMemoryCache())
				sessions.exchange = func(context.Context, string) (cursor.Session, error) { return cursor.Session{}, io.EOF }
				// A valid cached session lets this assertion reach native model discovery.
				require.NoError(t, sessions.cache.Set(context.Background(), cursorSessionKey(account), cursor.Session{Token: "cached-session", ExpiresAt: time.Now().Add(time.Hour)}))
				_, err := NewCursorGatewayService(sessions, nil).AvailableModels(context.Background(), account)
				require.Error(t, err)
			case "usage":
				usage := (&AccountUsageService{cursorUsageClient: &http.Client{}}).fetchCursorUsageWithToken(context.Background(), account, "synthetic-session")
				require.NotEmpty(t, usage.ErrorCode)
			}
			expected := int32(1)
			if family == "usage" {
				expected = 3
			}
			require.Equal(t, expected, tunnels.Load())
		})
	}
}

func TestCursorUnresolvedProxyNeverUsesInjectedDirectTransport(t *testing.T) {
	identifier := int64(99)
	account := &Account{ProxyID: &identifier}
	var direct atomic.Int32
	service := &AccountUsageService{cursorUsageClient: cursorUsageParityClient(func(*http.Request) (*http.Response, error) { direct.Add(1); return nil, io.EOF })}
	usage := service.fetchCursorUsageWithToken(context.Background(), account, "secret")
	require.NotEmpty(t, usage.ErrorCode)
	require.Zero(t, direct.Load())
}
