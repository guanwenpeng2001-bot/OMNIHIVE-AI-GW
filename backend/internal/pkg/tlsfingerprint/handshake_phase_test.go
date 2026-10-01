//go:build unit

package tlsfingerprint

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProxyTLSHandshakeMarkerOnlyAtHandshake(t *testing.T) {
	for _, scenario := range []string{"proxy handshake", "direct handshake", "cancelled", "invalid preset"} {
		t.Run(scenario, func(t *testing.T) {
			client, peer := net.Pipe()
			defer client.Close()
			_ = peer.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var profile *Profile
			if scenario == "cancelled" {
				cancel()
			}
			if scenario == "invalid preset" {
				profile = &Profile{KeyShareGroups: []uint16{0xffff}}
			}
			_, err := performTLSHandshake(ctx, client, profile, "destination.invalid:443", scenario != "direct handshake")
			require.Error(t, err)
			var marker *ProxyTLSHandshakeError
			require.Equal(t, scenario == "proxy handshake", errors.As(err, &marker))
			if scenario == "invalid preset" {
				require.ErrorContains(t, err, "apply TLS preset")
			}
			var global *ProxyConnectError
			require.False(t, errors.As(err, &global))
		})
	}
}
