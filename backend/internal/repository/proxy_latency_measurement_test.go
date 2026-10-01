package repository

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProxyLatencySeparatesWarmRequestFromConnectionSetup(t *testing.T) {
	var requests atomic.Int32
	addresses := make(chan string, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		addresses <- r.RemoteAddr
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := server.Client()
	transport := client.Transport.(*http.Transport)
	var connections atomic.Int32
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		connections.Add(1)
		time.Sleep(80 * time.Millisecond)
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	started := time.Now()
	measurement, latency, err := measureProxyLatency(context.Background(), client, server.URL)
	require.EqualValues(t, 1, connections.Load())
	require.True(t, measurement.LatencyCheckedAt.After(started.Add(75*time.Millisecond)))
	require.NoError(t, err)
	require.EqualValues(t, 2, requests.Load())
	require.Equal(t, <-addresses, <-addresses, "preheating must permit connection reuse")
	require.Equal(t, "warmed_http", measurement.LatencyMethod)
	require.NotNil(t, measurement.LatencyCheckedAt)
	require.Equal(t, server.URL, measurement.LatencyTestURL)
	require.NotNil(t, measurement.FirstRequestLatencyMs)
	require.GreaterOrEqual(t, *measurement.FirstRequestLatencyMs, int64(75))
	require.Less(t, latency, *measurement.FirstRequestLatencyMs, "do not report the warm-up duration as node latency")
}

func TestProxyTimingAndExitLookupAreIndependent(t *testing.T) {
	for _, test := range []struct {
		name        string
		timing, geo bool
		wantMethod  string
		wantError   bool
	}{
		{"timing succeeds without geography", true, false, "warmed_http", false},
		{"geography does not masquerade as latency", false, true, "unavailable", false},
		{"both checks fail", false, false, "unavailable", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/timing" && test.timing {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if r.URL.Path == "/geo" && test.geo {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"ip":"203.0.113.1"}`))
					return
				}
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer server.Close()
			prober := &proxyProbeService{configuredProbeURLs: []configuredProbeTarget{{url: server.URL + "/geo", parser: "ipify"}}}
			info, latency, err := prober.probeWithClient(context.Background(), server.Client(), server.URL+"/timing")
			require.Equal(t, test.wantError, err != nil)
			require.NotNil(t, info)
			require.Equal(t, test.wantMethod, info.LatencyMethod)
			require.Equal(t, test.geo, *info.ExitInfoAvailable)
			if !test.timing {
				require.Zero(t, latency)
			}
		})
	}
}

func TestProxyLatencyRejectsRedirectWithoutChangingClient(t *testing.T) {
	var redirected atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/portal" {
			redirected.Add(1)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Redirect(w, r, "/portal", http.StatusFound)
	}))
	defer server.Close()
	client := server.Client()
	measurement, _, err := measureProxyLatency(context.Background(), client, server.URL)
	require.ErrorContains(t, err, "HTTP 302")
	require.Equal(t, "unavailable", measurement.LatencyMethod)
	require.Zero(t, redirected.Load())
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.EqualValues(t, 1, redirected.Load(), "the original client's redirect policy must remain unchanged")
}

func TestProxyLatencySecondRequestFailureRetainsFirstTiming(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	measurement, latency, err := measureProxyLatency(context.Background(), server.Client(), server.URL)
	require.Error(t, err)
	require.Equal(t, "unavailable", measurement.LatencyMethod)
	require.NotNil(t, measurement.FirstRequestLatencyMs)
	require.Zero(t, latency)
}
