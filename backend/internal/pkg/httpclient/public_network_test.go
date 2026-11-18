package httpclient

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/dns/dnsmessage"
)

// Run only through scripts/test-public-egress-netns.sh. Every destination below
// is assigned to loopback inside a NEW network namespace with no external route.
// There is no DialContext rewrite: real DNS packets and TCP sockets exercise
// the exact addresses accepted/rejected by the policy.
func TestPublicEgressNetworkNamespace(t *testing.T) {
	if os.Getenv("OMNIHIVE_SSRF_NETNS_TEST") != "1" {
		t.Skip("requires isolated network namespace; see scripts/test-public-egress-netns.sh")
	}
	interfaces, err := net.Interfaces()
	require.NoError(t, err)
	require.Len(t, interfaces, 1, "refuse to run outside an isolated namespace")
	require.Equal(t, "lo", interfaces[0].Name)
	for _, tc := range []struct {
		name, address string
		public        bool
	}{
		{"public_v4", "93.184.216.34", true},
		{"public_v6", "2606:4700:4700::1111", true},
		{"anycast_v4", "192.0.0.9", true},
		{"amt_v6", "2001:3::1", true},
		{"nat64_public", "64:ff9b::808:808", true},
		{"benchmark_first", "198.18.0.1", false},
		{"benchmark_second", "198.19.0.1", false},
		{"cgnat", "100.64.0.1", false},
		{"documentation", "203.0.113.1", false},
		{"documentation_v6", "2001:db8::1", false},
		{"documentation_v6_new", "3fff::1", false},
		{"nat64_loopback", "64:ff9b::7f00:1", false},
		{"private", "10.1.2.3", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			server := publicNetworkServer(t, tc.address, "0", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				local, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
				if !ok {
					t.Error("missing local socket address")
					return
				}
				w.Header().Set("X-Local-Address", local.String())
				w.WriteHeader(http.StatusNoContent)
			}))
			_, port, err := net.SplitHostPort(server.Listener.Addr().String())
			require.NoError(t, err)
			// Establish that every blocked endpoint really is reachable locally.
			trusted, err := buildClient(Options{ValidateResolvedIP: true, AllowPrivateHosts: true, Timeout: time.Second})
			require.NoError(t, err)
			defer trusted.CloseIdleConnections()
			response, err := trusted.Get(server.URL)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.EqualValues(t, 1, hits.Load())
			hits.Store(0)
			resolver := publicNetworkDNS(t, func(string) []net.IP { return []net.IP{net.ParseIP(tc.address)} })
			previous := net.DefaultResolver
			net.DefaultResolver = resolver
			t.Cleanup(func() { net.DefaultResolver = previous })
			tr := NewPublicTransport(&http.Transport{})
			defer tr.CloseIdleConnections()
			client := &http.Client{Transport: tr, Timeout: time.Second}
			err = urlvalidator.ValidateResolvedIP(t.Context(), "target.test")
			require.Equal(t, tc.public, err == nil, "DNS precheck")
			for _, host := range []string{"target.test", tc.address} {
				response, err = client.Get("http://" + net.JoinHostPort(host, port) + "/")
				if tc.public {
					require.NoError(t, err)
					require.Equal(t, server.Listener.Addr().String(), response.Header.Get("X-Local-Address"))
					require.NoError(t, response.Body.Close())
				} else {
					require.ErrorContains(t, err, "not allowed")
					require.Zero(t, hits.Load())
				}
			}
			if tc.public {
				require.EqualValues(t, 2, hits.Load())
			}
		})
	}

	t.Run("mixed_A_AAAA", func(t *testing.T) {
		for _, answers := range [][]net.IP{
			{net.ParseIP("93.184.216.34"), net.ParseIP("198.19.0.1")},
			{net.ParseIP("198.19.0.1"), net.ParseIP("93.184.216.34")},
			{net.ParseIP("93.184.216.34"), net.ParseIP("2001:db8::1")},
			{net.ParseIP("2606:4700:4700::1111"), net.ParseIP("100.64.0.1")},
			{net.ParseIP("2606:4700:4700::1111"), net.ParseIP("::ffff:198.18.0.1")},
		} {
			resolver := publicNetworkDNS(t, func(string) []net.IP { return answers })
			previous := net.DefaultResolver
			net.DefaultResolver = resolver
			err := urlvalidator.ValidateResolvedIP(t.Context(), "mixed.test")
			net.DefaultResolver = previous
			require.ErrorContains(t, err, "not allowed")
			var dials atomic.Int32
			tr := NewPublicTransport(&http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				dials.Add(1)
				return (&net.Dialer{}).DialContext(ctx, network, address)
			}})
			tr.lookupIP = resolver.LookupIP
			defer tr.CloseIdleConnections()
			_, err = (&http.Client{Transport: tr, Timeout: time.Second}).Get("http://mixed.test/")
			require.ErrorContains(t, err, "not allowed")
			require.Zero(t, dials.Load(), "mixed answers must be rejected before any connect")
		}
	})

	for _, mode := range []string{"precheck_rebind", "pinned_then_reuse", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			var publicHits, blockedHits, answers atomic.Int32
			origin := publicNetworkServer(t, "93.184.216.34", "0", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				publicHits.Add(1)
				if strings.Split(r.Host, ":")[0] != "rebind.test" {
					t.Error("original Host lost")
				}
				if mode == "redirect" {
					http.Redirect(w, r, "/next", http.StatusFound)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			_, port, err := net.SplitHostPort(origin.Listener.Addr().String())
			require.NoError(t, err)
			publicNetworkServer(t, "198.19.0.1", port, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				blockedHits.Add(1)
				w.WriteHeader(http.StatusNoContent)
			}))
			resolver := publicNetworkDNS(t, func(qtype string) []net.IP {
				if qtype != "A" {
					return nil
				}
				if answers.Add(1) == 1 {
					return []net.IP{net.ParseIP("93.184.216.34")}
				}
				return []net.IP{net.ParseIP("198.19.0.1")}
			})
			previous := net.DefaultResolver
			net.DefaultResolver = resolver
			t.Cleanup(func() { net.DefaultResolver = previous })
			tr := NewPublicTransport(&http.Transport{})
			defer tr.CloseIdleConnections()
			client := &http.Client{Transport: tr, Timeout: time.Second}
			raw := "http://rebind.test:" + port
			if mode == "precheck_rebind" {
				require.NoError(t, urlvalidator.ValidateResolvedIP(t.Context(), "rebind.test"))
			}
			response, err := client.Get(raw)
			if mode == "pinned_then_reuse" {
				require.NoError(t, err)
				require.Equal(t, raw, response.Request.URL.String())
				_, err = io.Copy(io.Discard, response.Body)
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())
				require.EqualValues(t, 1, answers.Load(), "dial must not resolve the name again")
				_, err = client.Get(raw)
			}
			require.ErrorContains(t, err, "not allowed")
			require.Zero(t, blockedHits.Load())
			if mode == "precheck_rebind" {
				require.Zero(t, publicHits.Load())
			} else {
				require.EqualValues(t, 1, publicHits.Load())
			}
			require.EqualValues(t, 2, answers.Load())
		})
	}
}

func publicNetworkServer(t *testing.T, address, port string, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp", net.JoinHostPort(address, port))
	require.NoError(t, err)
	server := httptest.NewUnstartedServer(handler)
	require.NoError(t, server.Listener.Close())
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func publicNetworkDNS(t *testing.T, answer func(string) []net.IP) *net.Resolver {
	t.Helper()
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	done := make(chan struct{})
	t.Cleanup(func() { _ = listener.Close(); <-done })
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, peer, err := listener.ReadFrom(buf)
			if err != nil {
				return
			}
			var msg dnsmessage.Message
			if msg.Unpack(buf[:n]) != nil {
				continue
			}
			msg.Response, msg.RecursionAvailable = true, true
			for _, q := range msg.Questions {
				qtype := "AAAA"
				if q.Type == dnsmessage.TypeA {
					qtype = "A"
				}
				for _, ip := range answer(qtype) {
					resource := dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, Type: q.Type}}
					if q.Type == dnsmessage.TypeA && ip.To4() != nil {
						resource.Body = &dnsmessage.AResource{A: [4]byte(ip.To4())}
					} else if q.Type == dnsmessage.TypeAAAA && ip.To4() == nil {
						resource.Body = &dnsmessage.AAAAResource{AAAA: [16]byte(ip.To16())}
					} else {
						continue
					}
					msg.Answers = append(msg.Answers, resource)
				}
			}
			packet, err := msg.Pack()
			if err != nil {
				t.Errorf("pack synthetic DNS: %v", err)
				continue
			}
			_, _ = listener.WriteTo(packet, peer)
		}
	}()
	return &net.Resolver{PreferGo: true, StrictErrors: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp", listener.LocalAddr().String())
	}}
}
