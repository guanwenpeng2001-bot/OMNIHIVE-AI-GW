//go:build unit

package service

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/antigravity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/cursor"
	"github.com/stretchr/testify/require"
)

func TestW05OpenCodePaidPrivateCredential(t *testing.T) {
	for _, mode := range []string{AccountModeZen, AccountModeGo} {
		for _, key := range []string{"", "public", "private-key"} {
			a := freeAccount(PlatformOpenCodeGo, mode)
			a.Credentials["api_key"] = key
			err := validateExpandedPlatformAccount(a)
			if key == "private-key" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			old := freeAccount(PlatformOpenCodeGo, AccountModeZenFree)
			old.Credentials["api_key"] = "public"
			in := &BulkUpdateAccountsInput{Credentials: map[string]any{"account_mode": mode}}
			require.Error(t, normalizeFreeModelBulkCredentials([]*Account{old}, in))
			in.Credentials["api_key"] = "replacement-key"
			require.NoError(t, normalizeFreeModelBulkCredentials([]*Account{old}, in))
		}
	}
}

func TestW05ConnectionPartialAndKiloPaidEvidence(t *testing.T) {
	for _, page := range []string{
		`{"data":[{"id":"one"}],"has_more":true,"last_id":"one"}`,
		`{"data":[{"id":"one"}],"nextPageToken":"two"}`,
		`{"data":[{"id":"one"}],"next_cursor":"two"}`,
		`{"data":[{"id":"one"}],"links":{"next":"?cursor=two"}}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, page) }))
		a := freeAccount(PlatformOpenAI, "")
		a.Credentials["api_key"] = "private-key"
		req := httptest.NewRequest("GET", server.URL+"/models", nil)
		req.RequestURI = ""
		req.Header.Set("Authorization", "Bearer private-key")
		result := verifyConnectionRequest(req, server.Client(), a)
		require.Equal(t, "failed", result.Discovery)
		require.Empty(t, result.Models)
		require.Contains(t, result.Detail, "incomplete")
		server.Close()
	}
	var rows []string
	for i := 0; i < 1025; i++ {
		rows = append(rows, fmt.Sprintf(`{"id":"m-%d"}`, i))
	}
	require.True(t, ConnectionCatalogIncomplete([]byte(`{}`), len(rows)))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"data":[{"id":"paid/model"}]}`) }))
	defer server.Close()
	a := freeAccount(PlatformKilo, KiloAccountModeAPIKey)
	a.Credentials["api_key"] = "invalid-private-key"
	req, _ := http.NewRequest("GET", server.URL, nil)
	req.Header.Set("Authorization", "Bearer invalid-private-key")
	result := verifyConnectionRequest(req, server.Client(), a)
	require.Equal(t, "passed", result.Discovery)
	require.Equal(t, "unknown", result.Authorization)
}

func TestW05AuthorizationControlDiagnostics(t *testing.T) {
	primary := ConnectionProbe{Network: "passed", Discovery: "passed", Authorization: "unknown", Models: []string{"one"}}
	for _, status := range []int{200, 401, 403, 429, 302, 503} {
		got := ApplyConnectionAuthorizationControl(primary, &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("secret"))}, nil)
		require.Equal(t, "passed", got.Network)
		require.Equal(t, "passed", got.Discovery)
		if status == 401 || status == 403 {
			require.Equal(t, "passed", got.Authorization)
		}
		if status == 302 || status == 503 || status == 429 {
			require.NotEmpty(t, got.AuthorizationReason)
			require.NotEmpty(t, got.Detail)
		}
		require.NotContains(t, got.Detail, "secret")
	}
	for _, err := range []error{x509.UnknownAuthorityError{}, context.DeadlineExceeded, errors.New("https://secret.invalid/token")} {
		got := ApplyConnectionAuthorizationControl(primary, nil, err)
		require.Equal(t, "passed", got.Network)
		require.Equal(t, "PROBE_FAILED", got.AuthorizationReason)
		require.NotContains(t, got.Detail, "secret.invalid")
	}
}

type w05CursorDriver struct {
	cursorDriverStub
	err   error
	token string
}

func (d *w05CursorDriver) AvailableModels(_ context.Context, c cursor.Credentials) ([]cursor.AvailableModel, error) {
	d.token = c.AccessToken
	return []cursor.AvailableModel{{ID: "native-live"}}, d.err
}
func TestW05CursorOAuthProbeUsesProposedToken(t *testing.T) {
	gateway, a, exchanges := cursorTestGateway()
	a.Type = AccountTypeOAuth
	a.Credentials = map[string]any{"access_token": "proposed-token"}
	driver := &w05CursorDriver{}
	gateway.native = driver
	s := &AccountTestService{cfg: &config.Config{}, cursorGatewayService: gateway}
	got := s.ProbeConnection(t.Context(), a, "")
	require.Equal(t, "passed", got.Authorization)
	require.Equal(t, []string{"native-live"}, got.Models)
	require.Equal(t, "proposed-token", driver.token)
	require.Zero(t, exchanges.Load())
	driver.err = &cursor.HTTPError{Status: 401}
	require.Equal(t, "failed", s.ProbeConnection(t.Context(), a, "").Authorization)
	driver.err = context.DeadlineExceeded
	require.Equal(t, "PROBE_FAILED", s.ProbeConnection(t.Context(), a, "").NetworkReason)
}

func TestW05AntigravityOAuthProbeUsesNativeRoute(t *testing.T) {
	status := 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1internal:fetchAvailableModels", r.URL.Path)
		require.Equal(t, "Bearer proposed-token", r.Header.Get("Authorization"))
		w.WriteHeader(status)
		io.WriteString(w, `{"models":{"native-live":{"displayName":"Live"}}}`)
	}))
	defer server.Close()
	previous := antigravity.BaseURLs
	antigravity.BaseURLs = []string{server.URL}
	defer func() { antigravity.BaseURLs = previous }()
	cfg := &config.Config{}
	cfg.Security.URLAllowlist = config.URLAllowlistConfig{Enabled: true, AllowInsecureHTTP: true, AllowPrivateHosts: true, UpstreamHosts: []string{"127.0.0.1"}}
	s := &AccountTestService{cfg: cfg}
	a := &Account{Platform: PlatformAntigravity, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "proposed-token"}}
	got := s.ProbeConnection(t.Context(), a, "")
	require.Equal(t, "passed", got.Authorization)
	require.Equal(t, []string{"native-live"}, got.Models)
	status = 401
	require.Equal(t, "AUTH_REJECTED", s.ProbeConnection(t.Context(), a, "").AuthorizationReason)
	status = 403
	require.Equal(t, "AUTH_REJECTED", s.ProbeConnection(t.Context(), a, "").AuthorizationReason)
	status = 302
	require.Equal(t, "PROBE_FAILED", s.ProbeConnection(t.Context(), a, "").NetworkReason)
}
