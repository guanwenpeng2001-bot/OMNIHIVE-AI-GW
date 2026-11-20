package cursor

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/proxyutil"
	"golang.org/x/net/proxy"
)

type accountRouteKey struct{}
type accountRoute struct {
	proxyURL string
	invalid  bool
}

// WithAccountProxy carries an immutable route through refresh and parallel usage calls.
// An unresolved configured proxy is an error, never permission to dial directly.
func WithAccountProxy(ctx context.Context, proxyURL string, invalid bool) context.Context {
	return context.WithValue(ctx, accountRouteKey{}, accountRoute{proxyURL, invalid})
}

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Do uses a request-owned transport for configured account routes, so no pool or
// mutable client configuration can cross account boundaries.
func Do(client HTTPDoer, request *http.Request, native bool) (*http.Response, error) {
	route, configured := request.Context().Value(accountRouteKey{}).(accountRoute)
	if !configured {
		return client.Do(request)
	}
	if route.invalid {
		return nil, fmt.Errorf("cursor: account proxy unavailable")
	}
	var proxyURL *url.URL
	if route.proxyURL != "" {
		parsed, err := url.Parse(route.proxyURL)
		if err != nil || parsed.Hostname() == "" {
			return nil, fmt.Errorf("cursor: invalid account proxy")
		}
		switch parsed.Scheme {
		case "http", "https", "socks5", "socks5h":
		default:
			return nil, fmt.Errorf("cursor: unsupported account proxy")
		}
		proxyURL = parsed
	}
	// Preserve injected test clients and the native direct handshake when direct is explicit.
	if proxyURL == nil {
		return client.Do(request)
	}
	var transport http.RoundTripper
	var closeIdle func()
	if native {
		nativeClient := newClientWithDial(func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialAccountProxy(ctx, network, address, proxyURL)
		})
		transport = nativeClient.HTTPClient.Transport
		closeIdle = nativeClient.HTTPClient.CloseIdleConnections
	} else {
		defaultTransport, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return nil, fmt.Errorf("cursor: default HTTP transport unavailable")
		}
		standard := defaultTransport.Clone()
		standard.Proxy = nil
		if err := proxyutil.ConfigureTransportProxy(standard, proxyURL); err != nil {
			return nil, fmt.Errorf("cursor: invalid account proxy")
		}
		transport = standard
		closeIdle = standard.CloseIdleConnections
	}
	selected := &http.Client{Transport: transport, Timeout: 5 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if original, ok := client.(*http.Client); ok {
		selected.Timeout = original.Timeout
	}
	// The caller supplies the upstream request; account proxy context selects
	// its route only, with redirects disabled and no fallback to direct dialing.
	response, err := selected.Do(request) //nolint:gosec // G704: upstream target validation belongs to the calling native/relay service.
	if err != nil {
		closeIdle()
		return nil, &TransportError{cause: err}
	}
	response.Body = &routeResponseBody{ReadCloser: response.Body, closeIdle: closeIdle}
	return response, nil
}

type routeResponseBody struct {
	io.ReadCloser
	closeIdle func()
}

func (body *routeResponseBody) Close() error {
	err := body.ReadCloser.Close()
	body.closeIdle()
	return err
}

func dialAccountProxy(ctx context.Context, network, address string, proxyURL *url.URL) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	if proxyURL.Scheme == "socks5" || proxyURL.Scheme == "socks5h" {
		socks, err := proxy.FromURL(proxyURL, dialer)
		if err != nil {
			return nil, fmt.Errorf("cursor: invalid SOCKS proxy")
		}
		contextDialer, ok := socks.(proxy.ContextDialer)
		if !ok {
			return nil, fmt.Errorf("cursor: SOCKS proxy does not support context dialing")
		}
		return contextDialer.DialContext(ctx, network, address)
	}
	proxyAddress := proxyURL.Host
	if proxyURL.Port() == "" {
		port := "80"
		if proxyURL.Scheme == "https" {
			port = "443"
		}
		proxyAddress = net.JoinHostPort(proxyURL.Hostname(), port)
	}
	connection, err := dialer.DialContext(ctx, network, proxyAddress)
	if err != nil {
		return nil, err
	}
	succeeded := false
	defer func() {
		if !succeeded {
			_ = connection.Close()
		}
	}()
	rawConnection := connection
	stop := context.AfterFunc(ctx, func() { _ = rawConnection.Close() })
	defer stop()
	deadline := time.Now().Add(15 * time.Second)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		return nil, err
	}
	if proxyURL.Scheme == "https" {
		secured := tls.Client(connection, &tls.Config{ServerName: proxyURL.Hostname(), MinVersion: tls.VersionTLS12})
		if err := secured.HandshakeContext(ctx); err != nil {
			return nil, err
		}
		connection = secured
	}
	request := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: address}, Host: address, Header: make(http.Header)}
	if proxyURL.User != nil {
		password, _ := proxyURL.User.Password()
		request.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(proxyURL.User.Username()+":"+password)))
	}
	if err := request.Write(connection); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(connection)
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cursor: proxy CONNECT rejected")
	}
	if reader.Buffered() != 0 {
		return nil, fmt.Errorf("cursor: unexpected proxy tunnel data")
	}
	if err := connection.SetDeadline(time.Time{}); err != nil {
		return nil, err
	}
	succeeded = true
	return connection, nil
}

// ApplyRelayHeaders shares the routing header policy between discovery and inference.
func ApplyRelayHeaders(headers http.Header, overrides http.Header, apiKey string) {
	for name, values := range overrides {
		switch strings.ToLower(name) {
		case "authorization", "proxy-authorization", "x-api-key", "host", "content-length", "connection", "proxy-connection", "keep-alive", "transfer-encoding", "te", "trailer", "upgrade", "content-type", "accept", "anthropic-version":
			continue
		}
		headers[http.CanonicalHeaderKey(name)] = append([]string(nil), values...)
	}
	headers.Set("Authorization", "Bearer "+apiKey)
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "application/json")
}
