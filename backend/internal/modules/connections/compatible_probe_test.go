package connections

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCompatibleEndpointProbe(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/v1/models":
			if r.Header.Get("Authorization") != "Bearer test-secret" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"test-model"},{"id":"test-secret"},{"id":"test-model"}]}`)
		case "/redirect/models":
			http.Redirect(w, r, "/v1/models", http.StatusTemporaryRedirect)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = true
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	cfg.Security.URLAllowlist.UpstreamHosts = []string{"127.0.0.1"}
	native := service.NewAccountTestService(nil, nil, nil, nil, nil, nil, cfg, nil)
	store := New(nil, nil, nil, native, cfg)
	for _, platform := range []string{"a6api", "siliconflow_cn"} {
		t.Run(platform, func(t *testing.T) {
			account := &service.Account{Platform: platform, Type: service.AccountTypeAPIKey, Credentials: map[string]any{
				"api_key": "test-secret", "base_url": server.URL,
			}}
			got := store.probe.ProbeConnection(t.Context(), account, "")
			require.Equal(t, service.ConnectionProbe{Network: "passed", Authorization: "passed", Discovery: "passed", Models: []string{"test-model"}}, got)
		})
	}

	account := &service.Account{Platform: "a6api", Type: service.AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "invalid", "base_url": server.URL + "/v1",
	}}
	got := store.probe.ProbeConnection(t.Context(), account, "")
	require.Equal(t, "passed", got.Network)
	require.Equal(t, "failed", got.Authorization)
	account.Credentials["base_url"] = server.URL + "/redirect"
	before := calls.Load()
	got = store.probe.ProbeConnection(t.Context(), account, "")
	require.Equal(t, "failed", got.Network)
	require.Equal(t, before+1, calls.Load())

	cfg.Security.URLAllowlist.AllowPrivateHosts = false
	restricted := New(nil, nil, nil, native, cfg)
	before = calls.Load()
	got = restricted.probe.ProbeConnection(t.Context(), account, "")
	require.Equal(t, "failed", got.Network)
	require.Equal(t, before, calls.Load())

	delete(account.Credentials, "base_url")
	got = store.probe.ProbeConnection(t.Context(), account, "")
	require.Equal(t, "unknown", got.Network)
	require.Equal(t, "unsupported", got.Discovery)
}

func TestW05CompatibleProbeReportsIncompleteCatalog(t *testing.T) {
	for _, body := range []string{
		`{"data":[{"id":"one"}],"has_more":true,"last_id":"one"}`,
		`{"data":[{"id":"one"}],"nextPageToken":"second"}`,
		`{"data":[{"id":"one"}],"next_cursor":"second"}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
		req, _ := http.NewRequest("GET", server.URL, nil)
		result, tryNext := probeCompatibleModels(server.Client(), req, "bearer", "secret")
		require.False(t, tryNext)
		require.Equal(t, "failed", result.Discovery)
		require.Empty(t, result.Models)
		require.Contains(t, result.Detail, "incomplete")
		server.Close()
	}
}
