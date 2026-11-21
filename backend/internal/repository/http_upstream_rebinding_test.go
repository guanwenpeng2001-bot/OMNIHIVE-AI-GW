package repository

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/httpclient"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/dns/dnsmessage"
)

func TestHTTPUpstreamPublicRequestsUseIsolatedPinnedTransport(t *testing.T) {
	s, ok := NewHTTPUpstream(nil).(*httpUpstreamService)
	require.True(t, ok)
	entry, err := s.acquireClient("", 1, 1)
	require.NoError(t, err)
	req, _ := http.NewRequest("GET", "https://example.com", nil)
	require.Same(t, entry.publicClient, s.requestClient(entry, req))
	marked := req.WithContext(service.WithHTTPUpstreamPublicHostsOnly(req.Context()))
	strict := s.requestClient(entry, marked)
	require.NotSame(t, entry.client, strict)
	require.IsType(t, &httpclient.PublicTransport{}, strict.Transport)
	require.Same(t, strict, s.requestClient(entry, marked), "reuse the restricted connection pool")
	s.cfg = &config.Config{}
	s.cfg.Security.URLAllowlist.Enabled = true
	require.Same(t, strict, s.requestClient(entry, req))
	s.cfg.Security.URLAllowlist.AllowPrivateHosts = true
	s.cfg.Security.URLAllowlist.UpstreamHosts = []string{"127.0.0.1"}
	require.Same(t, entry.client, s.requestClient(entry, req))
	require.Same(t, strict, s.requestClient(entry, marked), "per-request policy overrides private-host exception")
}

// The first lookup (validateRequestHost) sees a public address. The second
// lookup, immediately before transport use, rebinds to a live private server.
func TestHTTPUpstreamDNSRebindingBetweenPrecheckAndConnect(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(204)
	}))
	defer server.Close()
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	require.NoError(t, err)
	lookups := installTestPublicDNS(t, func(n int32) [4]byte {
		if n > 1 {
			return [4]byte{127, 0, 0, 1}
		}
		return [4]byte{93, 184, 216, 34}
	})
	req, err := http.NewRequestWithContext(service.WithHTTPUpstreamPublicHostsOnly(t.Context()), "GET", "http://rebind.test:"+port, nil)
	require.NoError(t, err)
	_, err = NewHTTPUpstream(nil).Do(req, "", 1, 1)
	require.ErrorContains(t, err, "not allowed")
	require.GreaterOrEqual(t, lookups.Load(), int32(2))
	require.Zero(t, requests.Load(), "private target must never receive the request")
}

func installTestPublicDNS(t *testing.T, answer func(int32) [4]byte) *atomic.Int32 {
	t.Helper()
	dns, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, dns.Close()) })
	var lookups atomic.Int32
	go func() {
		buf := make([]byte, 2048)
		for {
			n, addr, err := dns.ReadFrom(buf)
			if err != nil {
				return
			}
			var msg dnsmessage.Message
			if msg.Unpack(buf[:n]) != nil {
				continue
			}
			msg.Response = true
			msg.RecursionAvailable = true
			for _, q := range msg.Questions {
				if q.Type != dnsmessage.TypeA {
					continue
				}
				ip := answer(lookups.Add(1))
				msg.Answers = append(msg.Answers, dnsmessage.Resource{
					Header: dnsmessage.ResourceHeader{Name: q.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET},
					Body:   &dnsmessage.AResource{A: ip},
				})
			}
			response, err := msg.Pack()
			if err == nil {
				_, _ = dns.WriteTo(response, addr)
			}
		}
	}()
	previous := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp", dns.LocalAddr().String())
	}}
	t.Cleanup(func() { net.DefaultResolver = previous })
	return &lookups
}

func TestHTTPUpstreamPinnedTransportBlocksPrivateEvenWithoutPrecheck(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) }))
	defer server.Close()
	s, ok := NewHTTPUpstream(loopbackUpstreamConfig()).(*httpUpstreamService)
	require.True(t, ok)
	entry, err := s.acquireClient("", 1, 1)
	require.NoError(t, err)
	// Seed the unrestricted pool, then access the strict transport directly to
	// prove that safety does not depend on validateRequestHost or cache state.
	resp, err := entry.client.Get(server.URL)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	_, err = entry.publicClient.Get(server.URL)
	require.ErrorContains(t, err, "not allowed")
	require.Equal(t, 1, calls)
	entry.client.CloseIdleConnections()
	entry.publicClient.CloseIdleConnections()
}

func TestHTTPUpstreamPinnedFingerprintPreservesSNI(t *testing.T) {
	installTestPublicDNS(t, func(int32) [4]byte { return [4]byte{93, 184, 216, 34} })
	sni := make(chan string, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		sni <- hello.ServerName
		return nil, fmt.Errorf("captured ClientHello")
	}}
	server.StartTLS()
	defer server.Close()
	dialer := tlsfingerprint.NewDialer(&tlsfingerprint.Profile{}, func(ctx context.Context, network, addr string) (net.Conn, error) {
		require.Equal(t, "93.184.216.34:443", addr)
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	})
	tr := httpclient.NewPublicTransport(&http.Transport{DialTLSContext: dialer.DialTLSContext})
	defer tr.CloseIdleConnections()
	_, err := (&http.Client{Transport: tr, Timeout: 3 * time.Second}).Get("https://rebind.test")
	require.Error(t, err, "capture server intentionally aborts TLS")
	select {
	case host := <-sni:
		require.Equal(t, "rebind.test", host)
	default:
		t.Fatal("TLS ClientHello was not received")
	}
}

func TestHTTPUpstreamPinnedSOCKSTarget(t *testing.T) {
	for _, fingerprint := range []bool{false, true} {
		t.Run(fmt.Sprint(fingerprint), func(t *testing.T) {
			installTestPublicDNS(t, func(int32) [4]byte { return [4]byte{93, 184, 216, 34} })
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			defer func() { require.NoError(t, listener.Close()) }()
			target := make(chan []byte, 1)
			sni := make(chan string, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer func() { assert.NoError(t, conn.Close()) }()
				if !assert.NoError(t, conn.SetDeadline(time.Now().Add(3*time.Second))) {
					return
				}
				greeting := make([]byte, 2)
				if _, err := io.ReadFull(conn, greeting); err != nil {
					return
				}
				methods := make([]byte, int(greeting[1]))
				if _, err := io.ReadFull(conn, methods); err != nil {
					return
				}
				if _, err := conn.Write([]byte{5, 0}); !assert.NoError(t, err) {
					return
				}
				command := make([]byte, 10)
				if _, err := io.ReadFull(conn, command); err != nil {
					return
				}
				target <- command
				if _, err := conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0}); !assert.NoError(t, err) {
					return
				}
				if fingerprint {
					tlsConn := tls.Server(conn, &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
						sni <- hello.ServerName
						return nil, fmt.Errorf("captured ClientHello")
					}})
					assert.ErrorContains(t, tlsConn.Handshake(), "captured ClientHello")
				} else {
					req, err := http.ReadRequest(bufio.NewReader(conn))
					if err != nil {
						return
					}
					assert.NoError(t, req.Body.Close())
					if _, err := io.WriteString(conn, "HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n"); !assert.NoError(t, err) {
						return
					}
				}
			}()
			proxyURL, _ := url.Parse("socks5h://" + listener.Addr().String())
			var base *http.Transport
			scheme := "http"
			if fingerprint {
				scheme = "https"
				base, err = buildUpstreamTransportWithTLSFingerprint(defaultPoolSettings(nil), proxyURL, &tlsfingerprint.Profile{})
			} else {
				base, err = buildUpstreamTransport(defaultPoolSettings(nil), proxyURL, upstreamProtocolModeDefault)
			}
			require.NoError(t, err)
			tr := httpclient.NewPublicTransport(base)
			defer tr.CloseIdleConnections()
			resp, err := (&http.Client{Transport: tr, Timeout: 3 * time.Second}).Get(scheme + "://rebind.test")
			if fingerprint {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NoError(t, resp.Body.Close())
			}
			select {
			case command := <-target:
				require.Equal(t, byte(1), command[3], "SOCKS destination must be numeric IPv4, never a hostname")
				require.Equal(t, []byte{93, 184, 216, 34}, command[4:8])
			default:
				t.Fatal("SOCKS target not received")
			}
			if fingerprint {
				select {
				case host := <-sni:
					require.Equal(t, "rebind.test", host)
				default:
					t.Fatal("missing ClientHello")
				}
			}
		})
	}
}

func TestHTTPUpstreamPinnedHTTP2RetainsIdentityAndConnectionReuse(t *testing.T) {
	installTestPublicDNS(t, func(int32) [4]byte { return [4]byte{93, 184, 216, 34} })
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "example.com", r.TLS.ServerName)
		require.Equal(t, "example.com", r.Host)
		w.WriteHeader(204)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	base, err := buildUpstreamTransport(defaultPoolSettings(nil), nil, upstreamProtocolModeLongStreamH2)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	base.TLSClientConfig.RootCAs = roots
	var dials atomic.Int32
	base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dials.Add(1)
		require.Equal(t, "93.184.216.34:443", addr)
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	require.Equal(t, longStreamHTTP2ReadIdleTimeout, base.HTTP2.SendPingTimeout)
	tr := httpclient.NewPublicTransport(base)
	defer tr.CloseIdleConnections()
	for range 2 {
		resp, err := (&http.Client{Transport: tr}).Get("https://example.com")
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, 2, resp.ProtoMajor)
	}
	require.Equal(t, int32(1), dials.Load())
}

func TestHTTPUpstreamPinnedFingerprintCONNECTTarget(t *testing.T) {
	installTestPublicDNS(t, func(int32) [4]byte { return [4]byte{93, 184, 216, 34} })
	sni := make(chan string, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "CONNECT", r.Method)
		require.Equal(t, "93.184.216.34:443", r.Host)
		hijacker, ok := w.(http.Hijacker)
		require.True(t, ok)
		conn, rw, err := hijacker.Hijack()
		require.NoError(t, err)
		defer func() { assert.NoError(t, conn.Close()) }()
		_, err = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		require.NoError(t, err)
		require.NoError(t, rw.Flush())
		tlsConn := tls.Server(conn, &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			sni <- hello.ServerName
			return nil, fmt.Errorf("captured ClientHello")
		}})
		assert.ErrorContains(t, tlsConn.Handshake(), "captured ClientHello")
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	base, err := buildUpstreamTransportWithTLSFingerprint(defaultPoolSettings(nil), proxyURL, &tlsfingerprint.Profile{})
	require.NoError(t, err)
	tr := httpclient.NewPublicTransport(base)
	defer tr.CloseIdleConnections()
	_, err = (&http.Client{Transport: tr, Timeout: 3 * time.Second}).Get("https://rebind.test")
	require.Error(t, err)
	select {
	case host := <-sni:
		require.Equal(t, "rebind.test", host)
	default:
		t.Fatal("missing ClientHello")
	}
}

func TestHTTPUpstreamPinnedFingerprintHTTPSProxyUsesTLS(t *testing.T) {
	installTestPublicDNS(t, func(int32) [4]byte { return [4]byte{93, 184, 216, 34} })
	proxySNI := make(chan string, 1)
	proxy := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("untrusted proxy certificate must prevent CONNECT")
	}))
	proxy.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		proxySNI <- hello.ServerName
		return nil, nil
	}}
	proxy.StartTLS()
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	base, err := buildUpstreamTransportWithTLSFingerprint(defaultPoolSettings(nil), proxyURL, &tlsfingerprint.Profile{})
	require.NoError(t, err)
	tr := httpclient.NewPublicTransport(base)
	defer tr.CloseIdleConnections()
	_, err = (&http.Client{Transport: tr, Timeout: 3 * time.Second}).Get("https://rebind.test")
	require.Error(t, err)
	var certificateErr *tls.CertificateVerificationError
	require.ErrorAs(t, err, &certificateErr)
	select {
	case host := <-proxySNI:
		require.Empty(t, host, "literal-IP proxy must not receive origin SNI")
	default:
		t.Fatal("HTTPS proxy did not receive TLS ClientHello")
	}
}
