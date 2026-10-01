//go:build unit

package repository

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
			request, classify := traceProxyApplicationWrite(request, proxyURL.String())
			_, err = (&http.Client{Transport: transport}).Do(request)
			err = classify(err)
			require.Error(t, err)
			var failure *service.ProxyConnectFailure
			require.Equal(t, status == 407, errors.As(err, &failure))
		})
	}
}

func TestProxyGroupTLSFailureRequiresPositivePhase(t *testing.T) {
	for _, fingerprint := range []bool{false, true} {
		for _, scenario := range []string{"handshake", "got connection before internal retry", "headers", "request", "cancelled", "deadline", "unmarked EOF", "policy error", "unrelated handshake", "no proxy"} {
			t.Run(fmt.Sprintf("fingerprint=%t/%s", fingerprint, scenario), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://destination.invalid/", nil)
				proxyURL := "http://127.0.0.1:1"
				if scenario == "no proxy" {
					proxyURL = ""
				}
				request, classify := traceProxyApplicationWrite(request, proxyURL)
				trace := httptrace.ContextClientTrace(request.Context())
				cause := errors.New("fixture handshake failed")
				var terminal error = fmt.Errorf("dial: %w", cause)
				if fingerprint {
					terminal = &tlsfingerprint.ProxyTLSHandshakeError{Err: cause}
				}
				switch scenario {
				case "got connection before internal retry":
					trace.GotConn(httptrace.GotConnInfo{})
				case "headers":
					trace.WroteHeaders()
				case "request":
					trace.WroteRequest(httptrace.WroteRequestInfo{})
				case "cancelled":
					cancel()
				case "deadline":
					terminal = context.DeadlineExceeded
				case "unmarked EOF":
					terminal = io.EOF
				case "policy error":
					terminal = errors.New("TLS handshake failed: policy rejected")
				case "unrelated handshake":
					terminal = errors.New("different failure")
				}
				if !fingerprint && scenario != "unmarked EOF" && scenario != "policy error" {
					trace.TLSHandshakeDone(tls.ConnectionState{}, cause)
				}
				classified := classify(terminal)
				var route *service.ProxyTLSHandshakeFailure
				require.Equal(t, scenario == "handshake", errors.As(classified, &route))
				var global *service.ProxyConnectFailure
				require.False(t, errors.As(classified, &global))
			})
		}
	}
}

func TestProxyGroupTLSFailureLoopbackPhaseAndCredentials(t *testing.T) {
	var originRequests atomic.Int32
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originRequests.Add(1)
		require.Empty(t, r.Header.Get("Proxy-Authorization"))
		require.Equal(t, "Bearer fixture-origin", r.Header.Get("Authorization"))
		body, _ := io.ReadAll(r.Body)
		require.Equal(t, "fixture payload", string(body))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer origin.Close()
	originURL, _ := url.Parse(origin.URL)
	for _, mode := range []string{"standard failure", "fingerprint failure", "standard healthy"} {
		t.Run(mode, func(t *testing.T) {
			before := originRequests.Load()
			var connects atomic.Int32
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodConnect, r.Method)
				require.Equal(t, originURL.Host, r.Host)
				require.Empty(t, r.Header.Get("Authorization"))
				require.NotEmpty(t, r.Header.Get("Proxy-Authorization"))
				connects.Add(1)
				conn, buffer, err := w.(http.Hijacker).Hijack()
				require.NoError(t, err)
				defer conn.Close()
				_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
				_ = buffer.Flush()
				if mode != "standard healthy" {
					return
				}
				target, err := net.DialTimeout("tcp", originURL.Host, time.Second)
				require.NoError(t, err)
				defer target.Close()
				go func() { _, _ = io.Copy(target, buffer) }()
				_, _ = io.Copy(conn, target)
			}))
			defer proxy.Close()
			proxyURL, _ := url.Parse(proxy.URL)
			proxyURL.User = url.UserPassword("fixture-proxy", "fixture-password")
			transport, err := buildUpstreamTransport(poolSettings{}, proxyURL, upstreamProtocolModeDefault)
			require.NoError(t, err)
			if mode == "fingerprint failure" {
				transport, err = buildUpstreamTransportWithTLSFingerprint(poolSettings{}, proxyURL, &tlsfingerprint.Profile{Name: "fixture"})
				require.NoError(t, err)
			}
			transport.TLSClientConfig = origin.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
			transport.DisableKeepAlives = true
			defer transport.CloseIdleConnections()
			request, _ := http.NewRequest(http.MethodPost, origin.URL, strings.NewReader("fixture payload"))
			request.Header.Set("Authorization", "Bearer fixture-origin")
			request, classify := traceProxyApplicationWrite(request, proxyURL.String())
			response, err := (&http.Client{Transport: transport, Timeout: 3 * time.Second}).Do(request)
			err = classify(err)
			require.Equal(t, int32(1), connects.Load())
			var global *service.ProxyConnectFailure
			require.False(t, errors.As(err, &global))
			if mode == "standard healthy" {
				require.NoError(t, err)
				require.Equal(t, http.StatusNoContent, response.StatusCode)
				_ = response.Body.Close()
				require.Equal(t, before+1, originRequests.Load())
			} else {
				var route *service.ProxyTLSHandshakeFailure
				require.ErrorAs(t, err, &route)
				require.Equal(t, before, originRequests.Load())
			}
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
	request, classify := traceProxyApplicationWrite(request, proxyURL.String())
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
			request, classify := traceProxyApplicationWrite(request, proxyURL.String())
			_, err = (&http.Client{Transport: transport}).Do(request)
			err = classify(err)
			require.Error(t, err)
			var failure *service.ProxyConnectFailure
			require.Equal(t, status == 407, errors.As(err, &failure))
		})
	}
}
