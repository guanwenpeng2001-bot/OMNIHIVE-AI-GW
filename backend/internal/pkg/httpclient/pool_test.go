package httpclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func publicLookup(context.Context, string, string) ([]net.IP, error) {
	return []net.IP{net.ParseIP("93.184.216.34")}, nil
}

func TestPublicTransportPinsDNSAndRejectsRebinding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "example.com", r.Host)
		w.WriteHeader(204)
	}))
	defer server.Close()
	var dials, lookups int
	tr := NewPublicTransport(&http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		dials++
		require.Equal(t, "93.184.216.34:80", addr, "must not resolve hostname again")
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}})
	defer tr.CloseIdleConnections()
	tr.lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		lookups++
		if lookups > 1 {
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		}
		return publicLookup(context.Background(), "ip", "example.com")
	}
	req, _ := http.NewRequest("GET", "http://example.com/path", nil)
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, "example.com", req.URL.Host)
	require.Same(t, req, resp.Request)
	_, err = tr.RoundTrip(req)
	require.ErrorContains(t, err, "not allowed")
	require.Equal(t, 1, dials)
	require.Equal(t, 2, lookups)
}

func TestPublicTransportRejectsAllBlockedAnswers(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "0.0.0.0", "::1", "fc00::1", "fe80::1", "::", "::ffff:127.0.0.1", "ff02::1"} {
		t.Run(address, func(t *testing.T) {
			tr := NewPublicTransport(&http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
				t.Fatal("blocked answer reached dialer")
				return nil, nil
			}})
			tr.lookupIP = func(context.Context, string, string) ([]net.IP, error) {
				return []net.IP{net.ParseIP("93.184.216.34"), net.ParseIP(address)}, nil
			}
			req, _ := http.NewRequest("GET", "http://example.com", nil)
			_, err := tr.RoundTrip(req)
			require.ErrorContains(t, err, "not allowed")
		})
	}
}

func TestPublicTransportPreservesTLSIdentity(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "example.com", r.TLS.ServerName)
		require.Equal(t, "example.com", r.Host)
		w.WriteHeader(204)
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	tr := NewPublicTransport(&http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		require.Equal(t, "93.184.216.34:443", addr)
		require.Equal(t, "example.com", OriginalHostname(ctx))
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}})
	tr.lookupIP = publicLookup
	defer tr.CloseIdleConnections()
	resp, err := (&http.Client{Transport: tr}).Get("https://example.com")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
}

func TestPublicTransportPinsHTTPProxyTarget(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "93.184.216.34:80", r.URL.Host)
		w.WriteHeader(204)
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	tr := NewPublicTransport(&http.Transport{Proxy: http.ProxyURL(proxyURL)})
	tr.lookupIP = publicLookup
	defer tr.CloseIdleConnections()
	resp, err := (&http.Client{Transport: tr}).Get("http://example.com")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
}

func TestPublicTransportRedirectRevalidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://private.example/", http.StatusFound)
	}))
	defer server.Close()
	tr := NewPublicTransport(&http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		require.Equal(t, "93.184.216.34:80", addr)
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}})
	tr.lookupIP = func(ctx context.Context, network, host string) ([]net.IP, error) {
		if host == "private.example" {
			return []net.IP{net.ParseIP("10.0.0.1")}, nil
		}
		return publicLookup(ctx, network, host)
	}
	defer tr.CloseIdleConnections()
	_, err := (&http.Client{Transport: tr}).Get("http://example.com")
	require.ErrorContains(t, err, "not allowed")
}

func TestBuildClientPrivateHostPolicy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	for _, allow := range []bool{false, true} {
		client, err := buildClient(Options{ValidateResolvedIP: true, AllowPrivateHosts: allow, PrivateHosts: []string{"127.0.0.1"}})
		require.NoError(t, err)
		defer client.CloseIdleConnections()
		resp, err := client.Get(server.URL)
		if allow {
			require.NoError(t, err)
			_, copyErr := io.Copy(io.Discard, resp.Body)
			require.NoError(t, copyErr)
			require.NoError(t, resp.Body.Close())
		} else {
			require.ErrorContains(t, err, "not allowed")
		}
	}
}

func TestPublicTransportPinsCONNECTAndPreservesProxyTLSIdentity(t *testing.T) {
	for _, secureProxy := range []bool{false, true} {
		t.Run(fmt.Sprint(secureProxy), func(t *testing.T) {
			origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "example.com", r.TLS.ServerName)
				require.Equal(t, "example.com", r.Host)
				w.WriteHeader(204)
			}))
			defer origin.Close()
			proxy := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "CONNECT", r.Method)
				require.Equal(t, "93.184.216.34:443", r.Host)
				if secureProxy {
					require.Empty(t, r.TLS.ServerName, "literal-IP proxy must not receive origin SNI")
				}
				upstream, err := net.Dial("tcp", origin.Listener.Addr().String())
				require.NoError(t, err)
				hijacker, ok := w.(http.Hijacker)
				require.True(t, ok)
				conn, rw, err := hijacker.Hijack()
				require.NoError(t, err)
				_, err = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
				require.NoError(t, err)
				require.NoError(t, rw.Flush())
				// Either relay can finish first and close both ends. Copy/close errors
				// during that teardown are expected; the client assertions below
				// verify the tunneled response and TLS identity.
				go func() {
					defer func() { _ = conn.Close(); _ = upstream.Close() }()
					_, _ = io.Copy(upstream, rw)
				}()
				go func() {
					defer func() { _ = conn.Close(); _ = upstream.Close() }()
					_, _ = io.Copy(conn, upstream)
				}()
			}))
			if secureProxy {
				proxy.StartTLS()
			} else {
				proxy.Start()
			}
			defer proxy.Close()
			roots := x509.NewCertPool()
			roots.AddCert(origin.Certificate())
			if secureProxy {
				roots.AddCert(proxy.Certificate())
			}
			proxyURL, _ := url.Parse(proxy.URL)
			tr := NewPublicTransport(&http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{RootCAs: roots}})
			tr.lookupIP = publicLookup
			defer tr.CloseIdleConnections()
			resp, err := (&http.Client{Transport: tr}).Get("https://example.com")
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
		})
	}
}

func TestPublicTransportFallsBackOnlyToValidatedAddresses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	var addresses []string
	tr := NewPublicTransport(&http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		addresses = append(addresses, addr)
		if addr == "[2001:4860:4860::8888]:80" {
			return nil, fmt.Errorf("IPv6 unavailable")
		}
		require.Equal(t, "93.184.216.34:80", addr)
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}})
	tr.lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("2001:4860:4860::8888"), net.ParseIP("93.184.216.34")}, nil
	}
	defer tr.CloseIdleConnections()
	resp, err := (&http.Client{Transport: tr}).Get("http://example.com")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, []string{"[2001:4860:4860::8888]:80", "93.184.216.34:80"}, addresses)
}

func TestPublicTransportRejectsFailedOrEmptyResolution(t *testing.T) {
	for _, lookupErr := range []error{nil, fmt.Errorf("DNS unavailable")} {
		tr := NewPublicTransport(&http.Transport{})
		tr.lookupIP = func(context.Context, string, string) ([]net.IP, error) { return nil, lookupErr }
		req, _ := http.NewRequest("GET", "http://example.com", nil)
		_, err := tr.RoundTrip(req)
		require.Error(t, err)
	}
}

func TestPublicTransportConditionalProxyPreservesDirectTLSIdentity(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "example.com", r.TLS.ServerName)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	tr := NewPublicTransport(&http.Transport{
		Proxy:           func(*http.Request) (*url.URL, error) { return nil, nil },
		TLSClientConfig: &tls.Config{RootCAs: roots},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			require.Equal(t, "93.184.216.34:443", addr)
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		},
	})
	tr.lookupIP = publicLookup
	defer tr.CloseIdleConnections()
	resp, err := (&http.Client{Transport: tr}).Get("https://example.com")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
}

func TestPublicTransportRejectsWrongTLSIdentity(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("request reached origin with wrong certificate identity")
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	tr := NewPublicTransport(&http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: roots},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		},
	})
	tr.lookupIP = publicLookup
	defer tr.CloseIdleConnections()
	_, err := (&http.Client{Transport: tr}).Get("https://wrong.example")
	var hostnameErr x509.HostnameError
	require.ErrorAs(t, err, &hostnameErr)
}

func TestDefaultPoolRejectsLocalDestination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected dispatch") }))
	defer server.Close()
	client, err := buildClient(Options{})
	require.NoError(t, err)
	defer client.CloseIdleConnections()
	resp, err := client.Get(server.URL)
	if resp != nil {
		resp.Body.Close()
	}
	require.ErrorContains(t, err, "not allowed")
}

func TestPrivateExceptionRequiresExactHost(t *testing.T) {
	for _, hosts := range [][]string{nil, {"*.test"}, {"other.test"}} {
		client, err := buildClient(Options{AllowPrivateHosts: true, PrivateHosts: hosts})
		require.NoError(t, err)
		ctx := WithPublicIPLookup(t.Context(), func(context.Context, string, string) ([]net.IP, error) { return []net.IP{net.ParseIP("10.0.0.1")}, nil })
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://upstream.test", nil)
		require.NoError(t, err)
		_, err = client.Do(req)
		require.ErrorContains(t, err, "not allowed")
		client.CloseIdleConnections()
	}
}

func TestPrivateExceptionNormalizesHostPort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	client, err := buildClient(Options{AllowPrivateHosts: true, PrivateHosts: []string{server.Listener.Addr().String()}})
	require.NoError(t, err)
	defer client.CloseIdleConnections()
	response, err := client.Get(server.URL)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
}
