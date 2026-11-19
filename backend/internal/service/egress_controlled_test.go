package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/httpclient"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/baggage"
)

func TestControlledEgressPolicy(t *testing.T) {
	var lookups, hits atomic.Int32
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		require.Equal(t, "example.com", r.Host)
		require.Equal(t, "example.com", r.TLS.ServerName)
		require.Empty(t, r.Header.Get("Baggage"))
		require.Empty(t, r.Header.Get("Tracestate"))
		switch r.URL.Path {
		case "/move":
			http.Redirect(w, r, "/ok", 302)
		case "/cycle":
			http.Redirect(w, r, "/cycle", 302)
		case "/private":
			http.Redirect(w, r, "https://127.0.0.1/private?token=secret", 302)
		case "/large":
			w.(http.Flusher).Flush()
			_, _ = io.WriteString(w, "123456")
		case "/slow":
			<-r.Context().Done()
		default:
			require.Empty(t, r.Header.Get("Authorization"))
			_, _ = io.WriteString(w, "image")
		}
	}))
	defer origin.Close()
	roots := x509.NewCertPool()
	roots.AddCert(origin.Certificate())
	base := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		require.Equal(t, "93.184.216.34:443", addr)
		return (&net.Dialer{}).DialContext(ctx, network, origin.Listener.Addr().String())
	}}
	transport := httpclient.NewPublicTransport(base)
	defer transport.CloseIdleConnections()
	e := ControlledEgress{client: &http.Client{Transport: transport}, timeout: time.Second, maxBytes: 5}
	ctx := httpclient.WithPublicIPLookup(t.Context(), func(context.Context, string, string) ([]net.IP, error) {
		lookups.Add(1)
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	})
	get := func(path string) (*http.Response, error) {
		r, _ := http.NewRequestWithContext(ctx, "GET", "https://example.com"+path, nil)
		r.Header.Set("baggage", "prompt=canary")
		if path == "/move" {
			r.Header.Set("Authorization", "credential")
		}
		return e.Do(r)
	}
	resp, err := get("/move")
	require.NoError(t, err)
	v, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)
	require.Equal(t, "image", string(v))
	require.EqualValues(t, 2, lookups.Load())
	before := hits.Load()
	_, err = get("/private")
	require.ErrorIs(t, err, errEgress)
	require.NotContains(t, err.Error(), "secret")
	require.Equal(t, before+1, hits.Load())
	before = hits.Load()
	_, err = get("/cycle")
	require.ErrorIs(t, err, errEgress)
	require.Equal(t, before+4, hits.Load())
	resp, err = get("/large")
	require.NoError(t, err)
	v, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	require.ErrorIs(t, err, errEgressSize)
	require.Len(t, v, 5)
	e.timeout = 30 * time.Millisecond
	_, err = get("/slow")
	require.Error(t, err)
}

func TestControlledEgressProxyUsesPinnedConnect(t *testing.T) {
	var calls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodConnect, r.Method)
		require.Equal(t, "93.184.216.34:443", r.Host)
		require.Empty(t, r.Header.Get("Authorization"))
		w.WriteHeader(403)
	}))
	defer proxy.Close()
	e, err := providerEgress(proxy.URL)
	require.NoError(t, err)
	var private atomic.Bool
	ctx := httpclient.WithPublicIPLookup(t.Context(), func(context.Context, string, string) ([]net.IP, error) {
		if private.Load() {
			return []net.IP{net.ParseIP("10.0.0.1")}, nil
		}
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	})
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://example.test/resource?token=secret", nil)
	for i := 0; i < 2; i++ {
		_, err = e.Do(req)
		require.ErrorIs(t, err, errEgress)
		require.EqualValues(t, 1, calls.Load())
		private.Store(true)
	}
}
func TestControlledProviderRetainsHostPolicy(t *testing.T) {
	b, _ := baggage.Parse("prompt=canary")
	ctx := baggage.ContextWithBaggage(t.Context(), b)
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://example.com/v1/responses", nil)
	req.Header.Set("Baggage", "prompt=canary")
	_, err := controlledProvider(req, func(r *http.Request) (*http.Response, error) {
		require.True(t, HTTPUpstreamPublicHostsOnly(r.Context()))
		require.True(t, HTTPUpstreamRedirectsDisabled(r.Context()))
		require.Empty(t, baggage.FromContext(r.Context()).Members())
		require.Empty(t, r.Header.Get("Baggage"))
		return nil, &url.Error{Op: "GET", URL: "https://example.test/?token=canary", Err: errors.New("provider prompt")}
	})
	require.ErrorIs(t, err, errEgress)
	require.NotContains(t, err.Error(), "canary")
}
func TestScopedAuditDropsSensitiveValuesBeforeQueueAndInHistoricalViews(t *testing.T) {
	entry := &AuditLog{Path: "/api/v2/workspaces/:workspace_id/projects/:project_id/audit/events/:id", Action: "canary", Method: "POST", UserAgent: "canary", RequestBody: "prompt-canary", CredentialMasked: "token-canary", Extra: map[string]any{"url": "https://canary", "params": "canary"}, RequestID: "canary"}
	svc := NewAuditLogService(nil, nil)
	svc.Record(entry)
	for _, safe := range []*AuditLog{<-svc.queue, auditManagementView(entry)} {
		if safe.CredentialMasked != "" || safe.Extra != nil || safe.UserAgent != "" || safe.RequestBody != "" || strings.Contains(safe.Action, "canary") || safe.RequestID != "" {
			t.Fatal("sensitive audit projection")
		}
	}
	require.Equal(t, "prompt-canary", entry.RequestBody)
}
