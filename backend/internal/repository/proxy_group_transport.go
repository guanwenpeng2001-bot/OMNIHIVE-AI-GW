package repository

import (
	"context"
	"errors"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sync/atomic"
)

// Only a dial to the configured HTTP proxy and its CONNECT authentication
// response prove a node-wide fault. A CONNECT destination error or target TLS
// failure is intentionally not classified as a proxy-wide failure.
func markProxyConnectionFailures(transport *http.Transport, proxy *url.URL) {
	if proxy == nil || (proxy.Scheme != "http" && proxy.Scheme != "https") {
		return
	}
	dial := transport.DialContext
	if dial == nil {
		return
	}
	port := proxy.Port()
	if port == "" {
		port = "80"
		if proxy.Scheme == "https" {
			port = "443"
		}
	}
	address := net.JoinHostPort(proxy.Hostname(), port)
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := dial(ctx, network, addr)
		if err != nil && ctx.Err() == nil && addr == address {
			return conn, &service.ProxyConnectFailure{Cause: err}
		}
		return conn, err
	}
	transport.OnProxyConnectResponse = func(ctx context.Context, _ *url.URL, _ *http.Request, response *http.Response) error {
		if ctx.Err() == nil && response.StatusCode == http.StatusProxyAuthRequired {
			return &service.ProxyConnectFailure{Cause: errors.New("proxy authentication rejected")}
		}
		return nil
	}
}

// Reused-connection retries inside net/http can dial after a prior write. Keep
// one trace across the entire client.Do, stripping the prewrite classification
// whenever any attempt wrote headers/request bytes or cancellation occurred.
func traceProxyApplicationWrite(req *http.Request) (*http.Request, func(error) error) {
	var wrote atomic.Bool
	trace := &httptrace.ClientTrace{WroteHeaders: func() { wrote.Store(true) }, WroteRequest: func(httptrace.WroteRequestInfo) { wrote.Store(true) }}
	traced := req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	return traced, func(err error) error {
		var fingerprintFailure *tlsfingerprint.ProxyConnectError
		if errors.As(err, &fingerprintFailure) {
			err = &service.ProxyConnectFailure{Cause: err}
		}
		var failure *service.ProxyConnectFailure
		if errors.As(err, &failure) && (wrote.Load() || req.Context().Err() != nil) {
			return fmt.Errorf("upstream transport failed without safe replay: %w", failure.Cause)
		}
		return err
	}
}
