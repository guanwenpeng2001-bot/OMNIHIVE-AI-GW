package repository

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sync"
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
// Route-only TLS eligibility additionally requires positive handshake evidence
// and no GotConn event on any attempt, even when no write callback was observed.
func traceProxyApplicationWrite(req *http.Request, proxyURL string) (*http.Request, func(error) error) {
	var wrote, gotConn atomic.Bool
	var handshakeMu sync.Mutex
	var handshakeErrors []error
	trace := &httptrace.ClientTrace{
		GotConn:      func(httptrace.GotConnInfo) { gotConn.Store(true) },
		WroteHeaders: func() { wrote.Store(true) },
		WroteRequest: func(httptrace.WroteRequestInfo) { wrote.Store(true) },
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			if err != nil {
				handshakeMu.Lock()
				handshakeErrors = append(handshakeErrors, err)
				handshakeMu.Unlock()
			}
		},
	}
	traced := req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	return traced, func(err error) error {
		var fingerprintFailure *tlsfingerprint.ProxyConnectError
		if errors.As(err, &fingerprintFailure) {
			err = &service.ProxyConnectFailure{Cause: err}
		}
		var failure *service.ProxyConnectFailure
		cancelled := req.Context().Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
		if errors.As(err, &failure) {
			if wrote.Load() || cancelled {
				return fmt.Errorf("upstream transport failed without safe replay: %w", failure.Cause)
			}
			return err
		}
		if err == nil || proxyURL == "" || gotConn.Load() || wrote.Load() || cancelled {
			return err
		}
		var handshakeFailure *tlsfingerprint.ProxyTLSHandshakeError
		proven := errors.As(err, &handshakeFailure)
		handshakeMu.Lock()
		for _, handshakeErr := range handshakeErrors {
			// A speculative or unrelated handshake callback cannot authorize
			// retry of a different terminal error from client.Do.
			proven = proven || errors.Is(err, handshakeErr)
		}
		handshakeMu.Unlock()
		if proven {
			return &service.ProxyTLSHandshakeFailure{Cause: err}
		}
		return err
	}
}
