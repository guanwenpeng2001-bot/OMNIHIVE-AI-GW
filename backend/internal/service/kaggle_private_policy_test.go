//go:build unit

package service

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

func TestKaggleVideoExplicitPrivatePolicy(t *testing.T) {
	for _, host := range []string{"10.1.2.3", "198.18.0.1", "100.64.0.1", "fc00::1"} {
		t.Run(host, func(t *testing.T) {
			account := &Account{ID: 1, Platform: PlatformKaggle, Type: AccountTypeAPIKey, Credentials: map[string]any{
				"api_key": "synthetic-test-only", "base_url": "https://" + net.JoinHostPort(host, "443"),
			}}
			upstream := &queuedHTTPUpstreamStub{
				responses: []*http.Response{{StatusCode: 204, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}},
				onCall: func(req *http.Request, _ *queuedHTTPUpstreamStub) {
					require.Equal(t, host, req.URL.Hostname())
					require.False(t, HTTPUpstreamPublicHostsOnly(req.Context()), "trusted Kaggle route must keep its policy")
				},
			}
			cfg := &config.Config{}
			cfg.Security.URLAllowlist.Enabled = true
			cfg.Security.URLAllowlist.AllowPrivateHosts = true
			cfg.Security.URLAllowlist.UpstreamHosts = []string{host}
			gateway := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
			response, err := gateway.kaggleVideoRequest(context.Background(), account, http.MethodGet, "/videos/fixture", nil)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.Equal(t, 1, upstream.callCount)
			cfg.Security.URLAllowlist.AllowPrivateHosts = false
			_, err = gateway.kaggleVideoRequest(context.Background(), account, http.MethodGet, "/videos/fixture", nil)
			require.ErrorContains(t, err, "not allowed")
			cfg.Security.URLAllowlist.AllowPrivateHosts = true
			cfg.Security.URLAllowlist.UpstreamHosts = []string{"other.example"}
			_, err = gateway.kaggleVideoRequest(context.Background(), account, http.MethodGet, "/videos/fixture", nil)
			require.ErrorContains(t, err, "not allowed")
			require.Equal(t, 1, upstream.callCount, "rejected requests must not reach upstream")
		})
	}
}
