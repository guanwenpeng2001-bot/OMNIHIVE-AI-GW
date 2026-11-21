package httpclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/util/urlvalidator"
)

type originalHostnameKey struct{}
type publicIPLookupKey struct{}

// WithPublicIPLookup installs a trusted, request-local resolver. Its answers
// still pass the transport's complete public-IP validation and address pinning.
// It never changes the resolver or connection policy of other requests.
func WithPublicIPLookup(ctx context.Context, lookup func(context.Context, string, string) ([]net.IP, error)) context.Context {
	if lookup == nil {
		return ctx
	}
	return context.WithValue(ctx, publicIPLookupKey{}, lookup)
}

// HasPublicIPLookup lets callers defer redundant DNS prechecks only when they
// already guarantee that this request will use PublicTransport.
func HasPublicIPLookup(ctx context.Context) bool {
	lookup, ok := ctx.Value(publicIPLookupKey{}).(func(context.Context, string, string) ([]net.IP, error))
	return ok && lookup != nil
}

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
	privateHosts []string
	Base         *http.Transport // Template cloned before each per-origin pool is created.
	lookupIP     func(context.Context, string, string) ([]net.IP, error)
	mu           sync.Mutex
	transports   map[string]*http.Transport
	lastUsed     map[string]time.Time
	accessOrder  map[string]uint64
	nextAccess   uint64
}

func NewPublicTransport(base *http.Transport) *PublicTransport {
	return &PublicTransport{Base: base, lookupIP: net.DefaultResolver.LookupIP, transports: make(map[string]*http.Transport), lastUsed: make(map[string]time.Time), accessOrder: make(map[string]uint64)}
}

// NewUpstreamTransport permits private addresses only for explicitly named
// origins. Every origin, including an exception, is resolved and pinned.
func NewUpstreamTransport(base *http.Transport, privateHosts []string) *PublicTransport {
	t := NewPublicTransport(base)
	for _, host := range privateHosts {
		host = strings.ToLower(strings.TrimSpace(host))
		if parsedHost, _, err := net.SplitHostPort(host); err == nil {
			host = parsedHost
		}
		host = strings.TrimSuffix(host, ".")
		if !strings.HasPrefix(host, "*.") {
			t.privateHosts = append(t.privateHosts, host)
		}
	}
	return t
}

// WithBase keeps the destination policy when an account selects another proxy.
func (t *PublicTransport) WithBase(base *http.Transport) *PublicTransport {
	return NewUpstreamTransport(base, t.privateHosts)
}

func (t *PublicTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil || req.URL.Hostname() == "" {
		return nil, fmt.Errorf("request host is empty")
	}
	host := req.URL.Hostname()
	host = strings.TrimSuffix(host, ".")
	private := slices.Contains(t.privateHosts, strings.ToLower(host))
	literal := net.ParseIP(host)
	localName := strings.EqualFold(strings.TrimSuffix(host, "."), "localhost")
	if urlvalidator.IsBlockedHost(host) && (!private || (!literal.IsPrivate() && !literal.IsLoopback() && !localName)) {
		return nil, fmt.Errorf("host is not allowed: %s", host)
	}
	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
	lookup := t.lookupIP
	if override, ok := ctx.Value(publicIPLookupKey{}).(func(context.Context, string, string) ([]net.IP, error)); ok && override != nil {
		lookup = override
	}
	// Literal targets must not be reinterpreted by a request-local resolver.
	ips := []net.IP{net.ParseIP(host)}
	var err error
	if ips[0] == nil {
		ips, err = lookup(ctx, "ip", host)
	}
	cancel()
	if err != nil {
		return nil, fmt.Errorf("dns resolution failed: %w", err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("dns resolution returned no addresses for %s", host)
	}
	for _, ip := range ips {
		if !urlvalidator.IsPublicIP(ip) && (!private || !isPrivateUpstreamIP(ip)) {
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

func isPrivateUpstreamIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	return addr.IsPrivate() || addr.IsLoopback() || addr.IsUnspecified() || privateBenchmarkIPv4.Contains(addr)
}

var privateBenchmarkIPv4 = netip.MustParsePrefix("198.18.0.0/15")

func (t *PublicTransport) transportForHost(host string) *http.Transport {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	t.nextAccess++
	for key, used := range t.lastUsed {
		if now.Sub(used) >= sharedClientTTL {
			t.transports[key].CloseIdleConnections()
			delete(t.transports, key)
			delete(t.lastUsed, key)
			delete(t.accessOrder, key)
		}
	}
	if transport := t.transports[host]; transport != nil {
		t.lastUsed[host] = now
		t.accessOrder[host] = t.nextAccess
		return transport
	}
	if len(t.transports) >= 64 {
		var oldestKey string
		var oldest uint64
		for key, used := range t.accessOrder {
			if oldest == 0 || used < oldest {
				oldestKey, oldest = key, used
			}
		}
		t.transports[oldestKey].CloseIdleConnections()
		delete(t.transports, oldestKey)
		delete(t.lastUsed, oldestKey)
		delete(t.accessOrder, oldestKey)
	}
	t.lastUsed[host] = now
	t.accessOrder[host] = t.nextAccess
	transport := t.Base.Clone()
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
				// Preserve the TLS handshake error; closing the failed connection is cleanup.
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
