package httpclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

type originalHostnameKey struct{}
type pinnedAddressesKey struct{}
type pinnedAddresses struct {
	primary   string
	addresses []string
}

// OriginalHostname preserves the TLS identity when a request is routed to a
// validated literal IP. Custom TLS dialers must use it for SNI and verification.
func OriginalHostname(ctx context.Context) string {
	host, _ := ctx.Value(originalHostnameKey{}).(string)
	return host
}

// PublicTransport resolves and validates every request, then passes only a
// literal IP to the transport (including HTTP CONNECT and SOCKS proxies).
// Host headers and TLS identities retain the original hostname. Its connection
// pools are separate from unrestricted requests, so those cannot seed an unsafe
// connection that a restricted request would later reuse.
type PublicTransport struct {
	base       *http.Transport
	lookupIP   func(context.Context, string, string) ([]net.IP, error)
	mu         sync.Mutex
	transports map[string]*http.Transport
}

func NewPublicTransport(base *http.Transport) *PublicTransport {
	return &PublicTransport{base: base, lookupIP: net.DefaultResolver.LookupIP, transports: make(map[string]*http.Transport)}
}

func (t *PublicTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil || req.URL.Hostname() == "" {
		return nil, fmt.Errorf("request host is empty")
	}
	host := req.URL.Hostname()
	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
	ips, err := t.lookupIP(ctx, "ip", host)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("dns resolution failed: %w", err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("dns resolution returned no addresses for %s", host)
	}
	for _, ip := range ips {
		if ip == nil || urlvalidator.IsBlockedHost(ip.String()) {
			return nil, fmt.Errorf("resolved ip %s is not allowed", ip)
		}
	}
	// Clone the request rather than mutating the caller's URL or Host. This also
	// leaves redirect resolution and cookie policy attached to the original URL.
	pinned := req.Clone(context.WithValue(req.Context(), originalHostnameKey{}, host))
	if pinned.Host == "" {
		pinned.Host = req.URL.Host
	}
	port := req.URL.Port()
	if port == "" {
		if req.URL.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	pinned.URL.Host = net.JoinHostPort(ips[0].String(), port)
	addresses := pinnedAddresses{primary: pinned.URL.Host}
	for _, ip := range ips {
		addresses.addresses = append(addresses.addresses, net.JoinHostPort(ip.String(), port))
	}
	pinned = pinned.WithContext(context.WithValue(pinned.Context(), pinnedAddressesKey{}, addresses))
	transport := t.transportForHost(host)
	if pinned.URL.Scheme == "http" && transport.Proxy != nil {
		proxyURL, err := transport.Proxy(pinned)
		if err != nil {
			return nil, err
		}
		if proxyURL != nil {
			// Request.WriteProxy otherwise constructs the absolute request URI
			// from Request.Host, undoing IP pinning. Keep Host for the origin,
			// but explicitly supply the validated proxy destination.
			path := pinned.URL.EscapedPath()
			if path == "" {
				path = "/"
			}
			pinned.URL.Opaque = "//" + pinned.URL.Host + path
		}
	}
	resp, err := transport.RoundTrip(pinned)
	if resp != nil {
		resp.Request = req
	}
	return resp, err
}

func (t *PublicTransport) transportForHost(host string) *http.Transport {
	t.mu.Lock()
	defer t.mu.Unlock()
	if transport := t.transports[host]; transport != nil {
		return transport
	}
	// Bound the extra per-TLS-identity pools just as the parent client cache is
	// bounded. Eviction closes idle sockets without interrupting active streams.
	if len(t.transports) >= 64 {
		for key, transport := range t.transports {
			transport.CloseIdleConnections()
			delete(t.transports, key)
			break
		}
	}
	transport := t.base.Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	transport.TLSClientConfig.ServerName = host
	dial := transport.DialContext
	if dial == nil {
		dial = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
	}
	transport.DialContext = dialPinnedAddresses(dial)
	if transport.DialTLSContext != nil {
		transport.DialTLSContext = dialPinnedAddresses(transport.DialTLSContext)
	}
	// HTTPS proxies have a TLS identity separate from the target inside CONNECT.
	if transport.Proxy != nil && transport.DialTLSContext == nil {
		transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			config := transport.TLSClientConfig.Clone()
			proxyHost, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			config.ServerName = proxyHost
			if pinned, ok := ctx.Value(pinnedAddressesKey{}).(pinnedAddresses); ok && addr == pinned.primary {
				// A conditional proxy can select direct HTTPS for this request.
				config.ServerName = OriginalHostname(ctx)
			}
			conn, err := transport.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			tlsConn := tls.Client(conn, config)
			timeout := transport.TLSHandshakeTimeout
			if timeout <= 0 {
				timeout = 10 * time.Second
			}
			handshakeCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			if err := tlsConn.HandshakeContext(handshakeCtx); err != nil {
				_ = conn.Close()
				return nil, err
			}
			return tlsConn, nil
		}
	}
	t.transports[host] = transport
	return transport
}

// Preserve address fallback without ever asking a dialer or SOCKS proxy to
// resolve the hostname again. Proxy endpoint addresses are intentionally left
// alone: the administrator-selected proxy is not the untrusted request target.
func dialPinnedAddresses(dial func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		pinned, ok := ctx.Value(pinnedAddressesKey{}).(pinnedAddresses)
		if !ok || addr != pinned.primary {
			return dial(ctx, network, addr)
		}
		var lastErr error
		for _, address := range pinned.addresses {
			conn, err := dial(ctx, network, address)
			if err == nil {
				return conn, nil
			}
			lastErr = err
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		return nil, lastErr
	}
}

func (t *PublicTransport) CloseIdleConnections() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, transport := range t.transports {
		transport.CloseIdleConnections()
	}
}
