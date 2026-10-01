package httpclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublicTransportRequestLookupStillPinsAndValidates(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "example.com", request.Host)
		require.Equal(t, "example.com", request.TLS.ServerName)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	dials, defaults := 0, 0
	transport := NewPublicTransport(&http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		dials++
		require.Equal(t, "93.184.216.34:443", address)
		require.Equal(t, "example.com", OriginalHostname(ctx))
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}})
	defer transport.CloseIdleConnections()
	transport.lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		defaults++
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}
	ctx := WithPublicIPLookup(context.Background(), func(_ context.Context, network, host string) ([]net.IP, error) {
		require.Equal(t, "ip", network)
		require.Equal(t, "example.com", host)
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	})
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com/", nil)
	require.NoError(t, err)
	response, err := (&http.Client{Transport: transport}).Do(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, 1, dials)
	require.Zero(t, defaults)
	_, err = (&http.Client{Transport: transport}).Get("https://example.com/")
	require.ErrorContains(t, err, "not allowed")
	require.Equal(t, 1, defaults, "override must not alter the shared transport default")
	ctx = WithPublicIPLookup(context.Background(), func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34"), net.ParseIP("10.0.0.1")}, nil
	})
	request = request.WithContext(ctx)
	_, err = transport.RoundTrip(request)
	require.ErrorContains(t, err, "not allowed")
	require.Equal(t, 1, dials, "trusted override cannot bypass validation, even with an existing connection")
}
