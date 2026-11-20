package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/xai"
)

func TestApplyDefaultGrokUpstreamHeadersUsesCLIUserAgent(t *testing.T) {
	t.Setenv(xai.CLIVersionEnv, "")

	req, err := http.NewRequest(http.MethodGet, "https://api.x.ai/v1/responses", nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "claude-code/1.2.3")
	req.Header.Set("x-grok-client-version", "none")

	applyDefaultGrokUpstreamHeaders(req)

	require.Equal(t, xai.CLIUserAgent(xai.CLIClientVersion), req.Header.Get("User-Agent"))
	require.Equal(t, xai.CLIClientVersion, req.Header.Get("x-grok-client-version"))
	require.Equal(t, xai.CLIClientIdentifier, req.Header.Get("x-grok-client-identifier"))
}

func TestApplyDefaultGrokUpstreamHeadersHonorsCLIVersionOverride(t *testing.T) {
	t.Setenv(xai.CLIVersionEnv, "1.0.13")

	req, err := http.NewRequest(http.MethodGet, "https://api.x.ai/v1/responses", nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "codex_cli_rs/0.144.0")

	applyDefaultGrokUpstreamHeaders(req)

	require.Equal(t, "1.0.13", req.Header.Get("x-grok-client-version"))
	require.Equal(t, xai.CLIUserAgent("1.0.13"), req.Header.Get("User-Agent"))
	require.Equal(t, "grok-shell", req.Header.Get("x-grok-client-identifier"))
}

func TestGrokDiscoveryAndTextHeadersShareVersionPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, override, want string
	}{
		{"default", "", xai.CLIClientVersion},
		{"minimum", "1.0.13", "1.0.13"},
		{"outdated", "0.2.120", xai.CLIClientVersion},
		{"future", "1.0.45", "1.0.45"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(xai.CLIVersionEnv, tc.override)
			for _, path := range []string{"models", "responses", "chat/completions"} {
				req, err := http.NewRequest(http.MethodGet, "https://cli-chat-proxy.grok.com/v1/"+path, nil)
				require.NoError(t, err)
				// Used by upstream model discovery and subscription text requests.
				applyGrokCLIHeaders(req.Header)
				require.Equal(t, tc.want, req.Header.Get(xai.CLIClientVersionHeader))
				require.Equal(t, xai.CLIUserAgent(tc.want), req.UserAgent())
				before := req.Header.Clone()
				xai.ApplyCLIProxyHeaders(req)
				require.Equal(t, before.Get(xai.CLIClientVersionHeader), req.Header.Get(xai.CLIClientVersionHeader))
				require.Equal(t, before.Get("User-Agent"), req.UserAgent())
			}
		})
	}
}

func TestResolveGrokUpstreamUserAgentNeverPassthrough(t *testing.T) {
	t.Setenv(xai.CLIVersionEnv, "")
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("User-Agent", "claude-cli/2.0.0 (Mac OS; arm64)")

	require.Equal(t, xai.CLIUserAgent(xai.CLIClientVersion), resolveGrokUpstreamUserAgent(c))
	require.Equal(t, xai.CLIUserAgent(xai.CLIClientVersion), resolveGrokUpstreamUserAgent(nil))
}

func TestApplyGrokRuntimeHeadersKeepsCLIUserAgent(t *testing.T) {
	t.Setenv(xai.CLIVersionEnv, "")

	req, err := http.NewRequest(http.MethodPost, "https://cli-chat-proxy.grok.com/v1/responses", nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "claude-code/9.9.9")

	applyGrokRuntimeHeaders(req, openAITLSFingerprintRuntime{
		UpstreamUserAgent:  "codex_cli_rs/0.144.0",
		UpstreamOriginator: "codex_cli_rs",
	})

	require.Equal(t, xai.CLIUserAgent(xai.CLIClientVersion), req.Header.Get("User-Agent"))
	require.Equal(t, "codex_cli_rs", req.Header.Get("Originator"))
	require.Equal(t, xai.CLIClientVersion, req.Header.Get("x-grok-client-version"))
}

func TestApplyGrokTLSProfileHeadersAlwaysUsesCLIUserAgent(t *testing.T) {
	t.Setenv(xai.CLIVersionEnv, "")

	req, err := http.NewRequest(http.MethodPost, "https://api.x.ai/v1/responses", nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "grok-native/1.0")

	// HEAD Profile is TLS-only; Originator/UserAgent HTTP fields are not present.
	applyGrokTLSProfileHeaders(req, &tlsfingerprint.Profile{Name: "chrome"})

	require.Equal(t, xai.CLIUserAgent(xai.CLIClientVersion), req.Header.Get("User-Agent"))
	require.Equal(t, xai.CLIClientVersion, req.Header.Get("x-grok-client-version"))
}
