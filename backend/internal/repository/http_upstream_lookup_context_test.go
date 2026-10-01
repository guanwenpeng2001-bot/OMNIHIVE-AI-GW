package repository

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestHTTPUpstreamRequestLookupDefersOnlyToGuardedClient(t *testing.T) {
	for _, fingerprint := range []bool{false, true} {
		for _, answer := range []string{"public", "mixed_private", "invalid"} {
			s := NewHTTPUpstream(nil).(*httpUpstreamService)
			profile := &tlsfingerprint.Profile{Name: "synthetic"}
			var entry *upstreamClientEntry
			var err error
			if fingerprint {
				entry, err = s.getClientEntryWithTLS("", 7, 1, profile, service.HTTPUpstreamProfileDefault, false, false)
			} else {
				entry, err = s.getClientEntry("", 7, 1, service.HTTPUpstreamProfileDefault, false, false)
			}
			require.NoError(t, err)
			dials, lookups := 0, 0
			dialError := errors.New("synthetic dial reached")
			guarded := httpclient.NewPublicTransport(&http.Transport{DialContext: func(_ context.Context, _, address string) (net.Conn, error) {
				dials++
				require.Equal(t, "93.184.216.34:443", address)
				return nil, dialError
			}})
			defer guarded.CloseIdleConnections()
			entry.publicClient.Transport = guarded
			ctx := httpclient.WithPublicIPLookup(service.WithHTTPUpstreamPublicHostsOnly(context.Background()), func(context.Context, string, string) ([]net.IP, error) {
				lookups++
				ips := []net.IP{net.ParseIP("93.184.216.34")}
				if answer == "mixed_private" {
					ips = append(ips, net.ParseIP("10.0.0.1"))
				}
				if answer == "invalid" {
					ips = append(ips, nil)
				}
				return ips, nil
			})
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://lookup.invalid/result", nil)
			require.NoError(t, err)
			if fingerprint {
				_, err = s.DoWithTLS(request, "", 7, 1, profile)
			} else {
				_, err = s.Do(request, "", 7, 1)
			}
			require.Error(t, err)
			require.Equal(t, 1, lookups, "must not run system DNS before the scoped lookup")
			if answer == "public" {
				require.ErrorIs(t, err, dialError)
				require.Equal(t, 1, dials)
			} else {
				require.ErrorContains(t, err, "not allowed")
				require.Zero(t, dials)
			}
		}
	}
}

func TestHTTPUpstreamLookupOverrideKeepsDefaultSelectionAndLiteralGuards(t *testing.T) {
	s := NewHTTPUpstream(nil).(*httpUpstreamService)
	entry, err := s.getClientEntry("", 7, 1, service.HTTPUpstreamProfileDefault, false, false)
	require.NoError(t, err)
	defer entry.client.CloseIdleConnections()
	defer entry.publicClient.CloseIdleConnections()
	ctx := httpclient.WithPublicIPLookup(context.Background(), func(context.Context, string, string) ([]net.IP, error) {
		t.Fatal("literal guard must run before lookup")
		return nil, nil
	})
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://127.0.0.1/result", nil)
	require.NoError(t, err)
	require.Same(t, entry.client, s.requestClient(entry, request), "override alone cannot select a different route")
	require.NoError(t, s.validateRequestHost(request), "unchanged default unrestricted policy")
	for _, global := range []bool{false, true} {
		guarded := request.WithContext(service.WithHTTPUpstreamPublicHostsOnly(ctx))
		if global {
			s.cfg = &config.Config{}
			s.cfg.Security.URLAllowlist.Enabled = true
			guarded = request
		}
		require.Same(t, entry.publicClient, s.requestClient(entry, guarded))
		require.ErrorContains(t, s.validateRequestHost(guarded), "not allowed")
		plain := guarded.WithContext(service.WithHTTPUpstreamPublicHostsOnly(context.Background()))
		require.ErrorContains(t, s.validateRequestHost(plain), "not allowed", "default DNS preflight remains enforced")
	}
}
