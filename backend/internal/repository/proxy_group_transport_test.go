//go:build unit

package repository

import (
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"testing"
)

func TestProxyGroupTransportProxyAuthenticationEvidence(t *testing.T) {
	for _, status := range []int{407, 403, 502} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodConnect, r.Method)
				w.WriteHeader(status)
			}))
			defer proxy.Close()
			proxyURL, _ := url.Parse(proxy.URL)
			transport, err := buildUpstreamTransport(poolSettings{}, proxyURL, upstreamProtocolModeDefault)
			require.NoError(t, err)
			defer transport.CloseIdleConnections()
			request, _ := http.NewRequest(http.MethodPost, "https://destination.invalid/generate", nil)
			request, classify := traceProxyApplicationWrite(request)
			_, err = (&http.Client{Transport: transport}).Do(request)
			err = classify(err)
			require.Error(t, err)
			var failure *service.ProxyConnectFailure
			require.Equal(t, status == 407, errors.As(err, &failure))
		})
	}
}

func TestProxyGroupTransportRefusedProxyAndWrittenRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	proxyURL, _ := url.Parse("http://" + address)
	transport, err := buildUpstreamTransport(poolSettings{}, proxyURL, upstreamProtocolModeDefault)
	require.NoError(t, err)
	defer transport.CloseIdleConnections()
	request, _ := http.NewRequest(http.MethodPost, "https://destination.invalid/generate", nil)
	request, classify := traceProxyApplicationWrite(request)
	_, err = (&http.Client{Transport: transport}).Do(request)
	err = classify(err)
	var failure *service.ProxyConnectFailure
	require.ErrorAs(t, err, &failure)
	httptrace.ContextClientTrace(request.Context()).WroteHeaders()
	require.False(t, errors.As(classify(err), &failure), "a prior write strips replay permission")
}

func TestProxyGroupFingerprintProxyEvidenceExcludesDestinationTLS(t *testing.T) {
	for _, status := range []int{407, 403, 502, 200} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodConnect, r.Method)
				if status != 200 {
					w.WriteHeader(status)
					return
				}
				connection, buffer, err := w.(http.Hijacker).Hijack()
				require.NoError(t, err)
				defer connection.Close()
				_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\ninvalid destination TLS")
				_ = buffer.Flush()
			}))
			defer proxy.Close()
			proxyURL, _ := url.Parse(proxy.URL)
			transport, err := buildUpstreamTransportWithTLSFingerprint(poolSettings{}, proxyURL, &tlsfingerprint.Profile{Name: "fixture"})
			require.NoError(t, err)
			defer transport.CloseIdleConnections()
			request, _ := http.NewRequest(http.MethodPost, "https://destination.invalid/generate", nil)
			request, classify := traceProxyApplicationWrite(request)
			_, err = (&http.Client{Transport: transport}).Do(request)
			err = classify(err)
			require.Error(t, err)
			var failure *service.ProxyConnectFailure
			require.Equal(t, status == 407, errors.As(err, &failure))
		})
	}
}
