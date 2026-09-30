package xai

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveCLIVersionDefaultsToPinnedClientVersion(t *testing.T) {
	t.Setenv(CLIVersionEnv, "")
	// Default advertise pin is CLIClientVersion; CLIStableVersion is only the floor.
	require.Equal(t, CLIClientVersion, ResolveCLIVersion())
	require.True(t, IsSupportedCLIVersion(CLIClientVersion))
	require.True(t, IsSupportedCLIVersion(CLIStableVersion))
	require.Equal(t, "1.0.44", CLIClientVersion)
	require.Equal(t, "1.0.13", CLIStableVersion)
}

func TestResolveCLIVersionAcceptsValidOverride(t *testing.T) {
	t.Setenv(CLIVersionEnv, "1.0.45-alpha.1")
	require.Equal(t, "1.0.45-alpha.1", ResolveCLIVersion())
}

func TestCLIHeaderVersionsStayConsistent(t *testing.T) {
	for _, tc := range []struct {
		name, override, want string
	}{
		{"default", "", CLIClientVersion},
		{"minimum", "1.0.13", "1.0.13"},
		{"trimmed override", " 1.0.44 ", "1.0.44"},
		{"future prerelease", "1.0.45-alpha.1", "1.0.45-alpha.1"},
		{"outdated default", "0.2.120", CLIClientVersion},
		{"below minimum", "1.0.12", CLIClientVersion},
		{"minimum prerelease", "1.0.13-beta.1", CLIClientVersion},
		{"unsafe", "1.0.45\r\nX-Injected: true", CLIClientVersion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(CLIVersionEnv, tc.override)
			text, err := http.NewRequest(http.MethodPost, "https://cli-chat-proxy.grok.com/v1/responses", nil)
			require.NoError(t, err)
			billing, err := http.NewRequest(http.MethodGet, BuildBillingURL(true), nil)
			require.NoError(t, err)
			ApplyCLIProxyHeaders(text)
			ApplyCLIBillingHeaders(billing, "test-token")
			require.Equal(t, tc.want, text.Header.Get(CLIClientVersionHeader))
			require.Equal(t, tc.want, billing.Header.Get(CLIClientVersionHeader))
			require.Equal(t, CLIUserAgent(tc.want), text.UserAgent())
			require.Equal(t, "grok-pager/"+tc.want+" grok-shell/"+tc.want+" (macos; aarch64)", billing.UserAgent())
		})
	}
}

func TestResolveCLIVersionRejectsUnsafeOrTooOld(t *testing.T) {
	for _, version := range []string{
		"0.2.120",
		"1.0.12",
		"1.0.13-beta.1",
		"1.0.45\r\nX-Injected: true",
		"1.0.045",
		"0.3",
		"1",
	} {
		t.Run(version, func(t *testing.T) {
			t.Setenv(CLIVersionEnv, version)
			require.Equal(t, CLIClientVersion, ResolveCLIVersion())
		})
	}
}

func TestApplyCLIProxyHeaders(t *testing.T) {
	t.Setenv(CLIVersionEnv, "")

	req, err := http.NewRequest(http.MethodPost, "https://cli-chat-proxy.grok.com/v1/responses", nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "legacy-client/1.0")

	ApplyCLIProxyHeaders(req)

	require.Equal(t, CLIClientVersion, req.Header.Get("x-grok-client-version"))
	require.Equal(t, CLIClientIdentifier, req.Header.Get("x-grok-client-identifier"))
	require.Equal(t, CLITokenAuth, req.Header.Get("X-XAI-Token-Auth"))
	require.Equal(t, CLIUserAgent(CLIClientVersion), req.Header.Get("User-Agent"))
}

func TestApplyCLIProxyHeadersLeavesAPIHostUnchanged(t *testing.T) {
	t.Setenv(CLIVersionEnv, "1.0.45")

	req, err := http.NewRequest(http.MethodPost, "https://api.x.ai/v1/responses", nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "direct-api-client/1.0")

	ApplyCLIProxyHeaders(req)

	require.Empty(t, req.Header.Get("x-grok-client-version"))
	require.Empty(t, req.Header.Get("x-grok-client-identifier"))
	require.Empty(t, req.Header.Get("X-XAI-Token-Auth"))
	require.Equal(t, "direct-api-client/1.0", req.Header.Get("User-Agent"))
}
