//go:build unit

package repository

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/httpclient"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type handshakeLoopbackGroups struct {
	service.ProxyGroupRepository
	group *service.ProxyGroup
}

func (r *handshakeLoopbackGroups) GetByID(context.Context, int64) (*service.ProxyGroup, error) {
	return r.group, nil
}

// Reuse the in-memory binding fixture; no PostgreSQL or Redis is contacted.
type handshakeLoopbackCache struct {
	*postgresRuntimeCache
	cooldownWrites int
}

func (c *handshakeLoopbackCache) SetCooldownIfAbsent(context.Context, service.ProxyCooldown, time.Duration) (bool, error) {
	c.cooldownWrites++
	return true, nil
}

func TestProxyGroupTLSLoopbackDiscoveryAlternatesAndSticks(t *testing.T) {
	var origins, failedConnects, healthyConnects atomic.Int32
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origins.Add(1)
		require.Equal(t, "/v1/models", r.URL.Path)
		require.Equal(t, "Bearer fixture-origin", r.Header.Get("Authorization"))
		require.Empty(t, r.Header.Get("Proxy-Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"fixture-model"}]}`)
	}))
	defer origin.Close()
	originURL, _ := url.Parse(origin.URL)
	makeProxy := func(fail bool, count *atomic.Int32) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodConnect, r.Method)
			require.Equal(t, originURL.Host, r.Host)
			require.Empty(t, r.Header.Get("Authorization"))
			require.NotEmpty(t, r.Header.Get("Proxy-Authorization"))
			count.Add(1)
			conn, buffer, err := w.(http.Hijacker).Hijack()
			require.NoError(t, err)
			defer conn.Close()
			_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
			_ = buffer.Flush()
			if fail {
				return
			}
			target, err := net.DialTimeout("tcp", originURL.Host, time.Second)
			require.NoError(t, err)
			defer target.Close()
			go func() { _, _ = io.Copy(target, buffer) }()
			_, _ = io.Copy(conn, target)
		}))
	}
	failed := makeProxy(true, &failedConnects)
	defer failed.Close()
	healthy := makeProxy(false, &healthyConnects)
	defer healthy.Close()
	group := &service.ProxyGroup{ID: 7, WorkspaceID: 1, Status: service.StatusActive, FallbackMode: service.FallbackModeNone}
	upstream := NewHTTPUpstream(loopbackUpstreamConfig()).(*httpUpstreamService)
	for index, raw := range []string{failed.URL, healthy.URL} {
		u, _ := url.Parse(raw)
		port, _ := strconv.Atoi(u.Port())
		p := &service.Proxy{ID: int64(11 + index), WorkspaceID: 1, Status: service.StatusActive, Protocol: "http", Host: u.Hostname(), Port: port, Username: "fixture-proxy", Password: "fixture-password"}
		group.Members = append(group.Members, service.ProxyGroupMember{GroupID: 7, WorkspaceID: 1, ProxyID: p.ID, Proxy: p})
		entry, err := upstream.getOrCreateClient(p.URL(), 91, 1)
		require.NoError(t, err)
		transport := entry.client.Transport.(*httpclient.PublicTransport).Base
		transport.TLSClientConfig = origin.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
		transport.DisableKeepAlives = true
		defer transport.CloseIdleConnections()
	}
	cache := &handshakeLoopbackCache{postgresRuntimeCache: &postgresRuntimeCache{bindings: map[[2]int64]int64{{7, 91}: 11}}}
	runtime := service.NewProxyGroupRuntime(&handshakeLoopbackGroups{group: group}, nil, cache)
	account := &service.Account{ID: 91, WorkspaceID: 1, Platform: service.PlatformDeepseek, Type: service.AccountTypeAPIKey, Concurrency: 1, ProxyGroupID: &group.ID, Credentials: map[string]any{"api_key": "fixture-origin", "base_url": origin.URL}}
	account.AttachEgress(runtime)
	discovery := service.NewAccountTestService(nil, nil, nil, nil, nil, upstream, &config.Config{}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	models, err := discovery.FetchUpstreamSupportedModels(ctx, account)
	require.NoError(t, err)
	require.Equal(t, []string{"fixture-model"}, models)
	require.Equal(t, int32(1), failedConnects.Load())
	require.Equal(t, int32(1), healthyConnects.Load())
	require.Equal(t, int32(1), origins.Load(), "failed TLS route must not deliver an origin request")
	require.Zero(t, cache.cooldownWrites)
	require.Equal(t, int64(12), cache.bindings[[2]int64{7, 91}])
	// A fresh operation reuses the healthy binding without revisiting failed TLS.
	next := *account
	next.AttachEgress(runtime)
	_, err = discovery.FetchUpstreamSupportedModels(ctx, &next)
	require.NoError(t, err)
	require.Equal(t, int32(1), failedConnects.Load())
	require.Equal(t, int32(2), healthyConnects.Load())
	require.Equal(t, int32(2), origins.Load())
	require.Zero(t, cache.cooldownWrites)
}
