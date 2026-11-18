package httpclient

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublicTransportRejectsSpecialUseAndMalformedAnswers(t *testing.T) {
	blocked := []net.IP{nil, {}, {1, 2, 3}, {1, 2, 3, 4, 5}}
	for _, value := range []string{
		"198.18.0.1", "198.19.255.255", "100.64.0.1", "192.0.2.1", "198.51.100.1", "203.0.113.1",
		"0.1.2.3", "240.0.0.1", "239.0.0.1", "192.88.99.2", "::ffff:198.19.1.1",
		"2001:db8::1", "3fff::1", "2001:2::1", "2002::1", "64:ff9b::a00:1", "64:ff9b:1::1",
	} {
		blocked = append(blocked, net.ParseIP(value))
	}
	for i, bad := range blocked {
		for _, override := range []bool{false, true} {
			for _, answers := range [][]net.IP{{bad}, {net.ParseIP("8.8.8.8"), bad}, {bad, net.ParseIP("2606:4700::1111")}} {
				t.Run(fmt.Sprintf("%d/override=%v/%v", i, override, answers), func(t *testing.T) {
					var dials atomic.Int32
					tr := NewPublicTransport(&http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
						dials.Add(1)
						return nil, fmt.Errorf("unexpected dial")
					}})
					defer tr.CloseIdleConnections()
					lookup := func(context.Context, string, string) ([]net.IP, error) { return answers, nil }
					ctx := context.Background()
					if override {
						ctx = WithPublicIPLookup(ctx, lookup)
					} else {
						tr.lookupIP = lookup
					}
					req, err := http.NewRequestWithContext(ctx, "GET", "http://example.test", nil)
					require.NoError(t, err)
					_, err = tr.RoundTrip(req)
					require.ErrorContains(t, err, "not allowed")
					require.Zero(t, dials.Load())
				})
			}
		}
	}
}

func TestPublicTransportLiteralCannotBeRewrittenByLookup(t *testing.T) {
	for _, host := range []string{"198.18.0.1", "::ffff:100.64.0.1", "fe80::1%25eth0", "localhost."} {
		tr := NewPublicTransport(&http.Transport{})
		req, err := http.NewRequest("GET", "http://"+net.JoinHostPort(host, "80"), nil)
		require.NoError(t, err)
		req = req.WithContext(WithPublicIPLookup(req.Context(), func(context.Context, string, string) ([]net.IP, error) {
			t.Error("blocked literal/name reached resolver")
			return []net.IP{net.ParseIP("8.8.8.8")}, nil
		}))
		_, err = tr.RoundTrip(req)
		require.ErrorContains(t, err, "not allowed")
	}
	var address string
	tr := NewPublicTransport(&http.Transport{DialContext: func(_ context.Context, _, addr string) (net.Conn, error) {
		address = addr
		return nil, fmt.Errorf("test dial stopped")
	}})
	defer tr.CloseIdleConnections()
	tr.lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		t.Error("public literal reached resolver")
		return nil, nil
	}
	req, err := http.NewRequest("GET", "http://[::ffff:8.8.8.8]", nil)
	require.NoError(t, err)
	_, err = tr.RoundTrip(req)
	require.ErrorContains(t, err, "test dial stopped")
	require.Equal(t, "8.8.8.8:80", address)
}

func TestPublicTransportSpecialUseRedirect(t *testing.T) {
	for _, target := range []string{"198.18.0.1", "100.64.0.1", "2001:db8::1", "64:ff9b::7f00:1"} {
		t.Run(target, func(t *testing.T) {
			var hits, dials atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				http.Redirect(w, r, "http://redirect.test/", http.StatusFound)
			}))
			defer server.Close()
			tr := NewPublicTransport(&http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				dials.Add(1)
				if addr != "93.184.216.34:80" {
					return nil, fmt.Errorf("unexpected destination")
				}
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			}})
			defer tr.CloseIdleConnections()
			tr.lookupIP = func(_ context.Context, _, host string) ([]net.IP, error) {
				if host == "redirect.test" {
					return []net.IP{net.ParseIP(target)}, nil
				}
				return publicLookup(context.Background(), "ip", host)
			}
			_, err := (&http.Client{Transport: tr}).Get("http://example.test/")
			require.ErrorContains(t, err, "not allowed")
			require.EqualValues(t, 1, hits.Load())
			require.EqualValues(t, 1, dials.Load())
		})
	}
}
