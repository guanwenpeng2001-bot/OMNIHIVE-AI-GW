package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

func TestProbeConnectionBuildDiagnostics(t *testing.T) {
	s := &AccountTestService{cfg: &config.Config{}}
	for _, target := range []string{"http://example.com", "https://127.0.0.1", "https://169.254.169.254", "https://10.0.0.1", "https://user:secret@%invalid"} {
		account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": target, "api_key": "never-expose-key"}}
		got := s.ProbeConnection(t.Context(), account, "")
		require.Equal(t, "failed", got.Network)
		require.Equal(t, "URL_NOT_ALLOWED", got.NetworkReason)
		require.Contains(t, got.Detail, "SECURITY_URL_ALLOWLIST_UPSTREAM_HOSTS")
		require.NotContains(t, got.Detail, "secret")
		require.NotContains(t, got.Detail, "never-expose-key")
	}
	s.cfg.Security.URLAllowlist = config.URLAllowlistConfig{Enabled: true, UpstreamHosts: []string{"allowed.example"}}
	a := &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://unlisted.example", "api_key": "secret"}}
	require.Equal(t, "URL_NOT_ALLOWED", s.ProbeConnection(t.Context(), a, "").NetworkReason)
	s.cfg.Security.URLAllowlist = config.URLAllowlistConfig{}
	a = &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{}}
	got := s.ProbeConnection(t.Context(), a, "")
	require.Equal(t, "failed", got.Authorization)
	require.Equal(t, "AUTH_REJECTED", got.AuthorizationReason)
	require.NotEmpty(t, got.Detail)
	for _, kind := range []UpstreamModelSyncErrorKind{UpstreamModelSyncErrorNotAdapted, UpstreamModelSyncErrorAccountType, UpstreamModelSyncErrorUnsupported, UpstreamModelSyncErrorDiscoveryUnconfirmed} {
		got = connectionProbeBuildFailure(fmt.Errorf("wrapped: %w", &UpstreamModelSyncError{Kind: kind, Message: "secret"}))
		require.Equal(t, "unsupported", got.Discovery)
		require.Equal(t, "CHECK_UNSUPPORTED", got.DiscoveryReason)
		require.NotContains(t, got.Detail, "secret")
	}
	got = connectionProbeBuildFailure(newUpstreamModelSyncConfigError("hidden", &connectionProbeURLError{errors.New("secret")}))
	require.Equal(t, "URL_NOT_ALLOWED", got.NetworkReason)
	validator, err := grokBaseURLValidator(&Account{Platform: PlatformGrok, Type: AccountTypeAPIKey}, &config.Config{})
	require.NoError(t, err)
	_, err = validator("https://127.0.0.1")
	require.Error(t, err)
	got = connectionProbeBuildFailure(newUpstreamModelSyncConfigError("Invalid Grok base URL", err))
	require.Equal(t, "URL_NOT_ALLOWED", got.NetworkReason)
}

func TestProbeConnectionHTTPDiagnostics(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500, 302} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "https://secret.example")
				w.WriteHeader(status)
				_, _ = w.Write([]byte("upstream-secret-body"))
			}))
			defer server.Close()
			req, err := http.NewRequestWithContext(t.Context(), "GET", server.URL, nil)
			require.NoError(t, err)
			got := verifyConnectionRequest(req, server.Client(), &Account{Credentials: map[string]any{}})
			if status == 302 {
				require.Equal(t, "failed", got.Network)
				require.Equal(t, "PROBE_FAILED", got.NetworkReason)
			} else {
				require.Equal(t, "passed", got.Network)
			}
			switch status {
			case 401, 403:
				require.Equal(t, "failed", got.Authorization)
				require.Equal(t, "AUTH_REJECTED", got.AuthorizationReason)
			case 429:
				require.Equal(t, "unknown", got.Authorization)
				require.Equal(t, "UPSTREAM_RATE_LIMITED", got.AuthorizationReason)
				require.Equal(t, "UPSTREAM_RATE_LIMITED", got.DiscoveryReason)
			}
			require.NotEqual(t, "unsupported", got.Discovery)
			require.NotEmpty(t, got.Detail)
			require.NotContains(t, got.Detail, "secret")
		})
	}
}

func TestProbeConnectionNetworkDiagnostics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()
	req, err := http.NewRequestWithContext(t.Context(), "GET", server.URL, nil)
	require.NoError(t, err)
	got := verifyConnectionRequest(req, server.Client(), &Account{})
	require.Equal(t, "failed", got.Network)
	require.Equal(t, "PROBE_FAILED", got.NetworkReason)
	require.NotEmpty(t, got.Detail)
	got = ConnectionProbeTransportFailure(errors.New("resolved ip is not allowed: secret"))
	require.Equal(t, "URL_NOT_ALLOWED", got.NetworkReason)
	require.NotContains(t, got.Detail, "secret")
}

func TestMissingModelDiscoveryCredentials(t *testing.T) {
	s := &AccountTestService{cfg: &config.Config{}}
	for _, tc := range []struct{ provider, auth, mode string }{
		{PlatformOpenAI, AccountTypeAPIKey, ""}, {PlatformOpenAI, AccountTypeOAuth, ""},
		{PlatformAnthropic, AccountTypeAPIKey, ""}, {PlatformAnthropic, AccountTypeOAuth, ""},
		{PlatformGrok, AccountTypeAPIKey, ""}, {PlatformGrok, AccountTypeOAuth, ""},
		{PlatformWorkBuddy, AccountTypeOAuth, ""}, {PlatformKilo, AccountTypeAPIKey, "apikey"},
		{PlatformKilo, AccountTypeAPIKey, "invalid"},
	} {
		t.Run(tc.provider+"/"+tc.auth+"/"+tc.mode, func(t *testing.T) {
			a := &Account{Platform: tc.provider, Type: tc.auth, Credentials: map[string]any{"account_mode": tc.mode}}
			_, err := s.buildUpstreamModelsRequest(context.WithValue(t.Context(), catalogDiscoveryKey{}, true), a)
			var syncErr *UpstreamModelSyncError
			require.ErrorAs(t, err, &syncErr)
			got := s.ProbeConnection(t.Context(), a, "")
			require.NotEmpty(t, got.Detail)
			if tc.mode == "invalid" {
				require.Equal(t, UpstreamModelSyncErrorConfiguration, syncErr.Kind)
				require.Equal(t, "PROBE_FAILED", got.DiscoveryReason)
				require.Equal(t, "unknown", got.Authorization)
				return
			}
			require.Equal(t, UpstreamModelSyncErrorCredentials, syncErr.Kind)
			require.Equal(t, "AUTH_REJECTED", got.AuthorizationReason)
			require.Equal(t, "failed", got.Authorization)
		})
	}
}
