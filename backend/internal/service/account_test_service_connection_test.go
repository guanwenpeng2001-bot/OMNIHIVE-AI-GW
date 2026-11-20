package service

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

func TestConnectionProbeEgressAndAuthorizationEvidence(t *testing.T) {
	var received atomic.Int64
	fixture := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"public-model"}]}`))
	}))
	defer fixture.Close()
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": fixture.URL, "api_key": "fixture"}}
	probe := &AccountTestService{cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: true, AllowPrivateHosts: true, UpstreamHosts: []string{"unrelated.example"}}}}}
	require.NotEqual(t, "passed", probe.ProbeConnection(t.Context(), account, "").Authorization)
	require.Zero(t, received.Load())
	probe.cfg.Security.URLAllowlist = config.URLAllowlistConfig{}
	require.NotEqual(t, "passed", probe.ProbeConnection(t.Context(), account, "").Network)
	require.Zero(t, received.Load())
	req, err := http.NewRequestWithContext(t.Context(), "GET", fixture.URL+"/models", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer fixture")
	result := verifyConnectionRequest(req, fixture.Client(), account)
	require.Equal(t, "passed", result.Discovery)
	require.Equal(t, "unknown", result.Authorization)
	require.Equal(t, int64(2), received.Load())
}
