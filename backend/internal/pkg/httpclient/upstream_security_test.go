package httpclient

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestSharedClientRejectsRedirectToPrivateAddress(t *testing.T) {
	var targetCalls int
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls++ }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, strings.Replace(target.URL, "127.0.0.1", "private.invalid", 1), http.StatusFound)
	}))
	defer origin.Close()
	client, err := buildClient(Options{AllowPrivateHosts: true, PrivateHosts: []string{"127.0.0.1"}})
	require.NoError(t, err)
	defer client.CloseIdleConnections()
	ctx := WithPublicIPLookup(t.Context(), func(_ context.Context, _, host string) ([]net.IP, error) {
		require.Equal(t, "private.invalid", host)
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin.URL, nil)
	require.NoError(t, err)
	response, err := client.Do(req)
	if response != nil {
		_ = response.Body.Close()
	}
	require.ErrorContains(t, err, "not allowed")
	require.Zero(t, targetCalls)
}
