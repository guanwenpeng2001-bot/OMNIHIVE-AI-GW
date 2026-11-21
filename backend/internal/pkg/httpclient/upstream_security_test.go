package httpclient

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpstreamTransportRequiresExactPrivateException(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	for _, target := range []string{server.URL, "http://2130706433", "http://0177.0.0.1", "http://[::1]", "http://169.254.169.254"} {
		client, err := buildClient(Options{})
		require.NoError(t, err)
		response, err := client.Get(target)
		if response != nil {
			_ = response.Body.Close()
		}
		require.ErrorContains(t, err, "not allowed", target)
		client.CloseIdleConnections()
	}
}

func TestUpstreamTransportPrivateDomainException(t *testing.T) {
	transport := NewUpstreamTransport(&http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("test dial stopped")
	}}, []string{"www.gstatic.com"})
	transport.lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("198.18.0.132")}, nil
	}
	require.Equal(t, []string{"www.gstatic.com"}, transport.privateHosts)
	require.True(t, isPrivateUpstreamIP(net.ParseIP("198.18.0.132")))
	req, err := http.NewRequest(http.MethodGet, "https://www.gstatic.com", nil)
	require.NoError(t, err)
	_, err = transport.RoundTrip(req)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "not allowed")
}
