//go:build unit

package service

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type egressRecordingUpstream struct {
	urls   []string
	bodies []string
	fail   error
	status int
}

func (u *egressRecordingUpstream) Do(req *http.Request, p string, _ int64, _ int) (*http.Response, error) {
	u.urls = append(u.urls, p)
	if req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		u.bodies = append(u.bodies, string(body))
		_ = req.Body.Close()
	}
	if len(u.urls) == 1 && u.fail != nil {
		return nil, u.fail
	}
	status := u.status
	if status == 0 {
		status = 200
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[]}`)), Request: req}, nil
}
func (u *egressRecordingUpstream) DoWithTLS(req *http.Request, p string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, p, id, n)
}

func TestProxyGroupTransportOneSafeAlternatePinsOperation(t *testing.T) {
	r, _, cache, account := runtimeFixture()
	ctx := context.Background()
	a, target, err := r.ApplyEgress(ctx, account)
	require.NoError(t, err)
	base := &egressRecordingUpstream{fail: &ProxyConnectFailure{Cause: errors.New("proxy dial refused")}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://provider.invalid/generate", strings.NewReader("paid request"))
	require.NoError(t, err)
	response, err := accountHTTPUpstream(base, a).Do(request, target.URL, a.ID, 1)
	require.NoError(t, err)
	_ = response.Body.Close()
	require.Len(t, base.urls, 2)
	require.NotEqual(t, base.urls[0], base.urls[1])
	require.Equal(t, []string{"paid request", "paid request"}, base.bodies)
	require.Contains(t, cache.cooldowns, target.ProxyID)
	next, err := a.EgressProxyURL(ctx)
	require.NoError(t, err)
	require.Equal(t, base.urls[1], next)
	// A caller retaining its pre-retry URL still uses the operation's pinned route.
	request, _ = http.NewRequestWithContext(ctx, http.MethodGet, "https://provider.invalid/status", nil)
	response, err = accountHTTPUpstream(base, a).Do(request, target.URL, a.ID, 1)
	require.NoError(t, err)
	_ = response.Body.Close()
	require.Equal(t, next, base.urls[2])
	require.Nil(t, a.ProxyID)
	require.Equal(t, int64(7), *a.ProxyGroupID)
}

func TestProxyGroupTransportNeverReplaysProviderOrAmbiguousFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		failure error
	}{
		{"unauthorized", 401, nil}, {"quota", 429, nil}, {"upstream failure", 503, nil},
		{"destination TLS", 0, errors.New("destination TLS handshake failed")},
		{"after write", 0, io.ErrUnexpectedEOF}, {"cancelled", 0, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _, cache, a := runtimeFixture()
			a, target, err := r.ApplyEgress(context.Background(), a)
			require.NoError(t, err)
			base := &egressRecordingUpstream{fail: tc.failure, status: tc.status}
			request, _ := http.NewRequest(http.MethodPost, "https://provider.invalid/generate", strings.NewReader("paid"))
			response, err := accountHTTPUpstream(base, a).Do(request, target.URL, a.ID, 1)
			if tc.failure != nil {
				require.ErrorIs(t, err, tc.failure)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.status, response.StatusCode)
				_ = response.Body.Close()
			}
			require.Len(t, base.urls, 1)
			require.Empty(t, cache.cooldowns)
		})
	}
}

func TestProxyGroupOutboundDiscoveryAndAccountProbe(t *testing.T) {
	for _, family := range []string{"discovery", "account probe", "TLS account probe"} {
		for _, mode := range []string{"unresolved group", "resolved group", "legacy unbound", "single node"} {
			t.Run(family+"/"+mode, func(t *testing.T) {
				r, g, cache, a := runtimeFixture()
				base := &egressRecordingUpstream{}
				wantURL := ""
				switch mode {
				case "resolved group":
					a.AttachEgress(r)
					var err error
					wantURL, err = a.EgressProxyURL(context.Background())
					require.NoError(t, err)
				case "legacy unbound":
					a.ProxyGroupID = nil
					a.AttachEgress(r)
				case "single node":
					a.ProxyGroupID = nil
					a.Proxy = g.group.Members[0].Proxy
					a.ProxyID = &a.Proxy.ID
					wantURL = a.Proxy.URL()
				}
				svc := &AccountTestService{httpUpstream: base}
				request, _ := http.NewRequest(http.MethodGet, "https://provider.invalid/models", nil)
				var response *http.Response
				var err error
				switch family {
				case "discovery":
					response, err = svc.doUpstreamModelsRequest(request, wantURL, a)
				default:
					response, err = svc.doOpenAIAccountTestUpstream(request, wantURL, a, family == "TLS account probe")
				}
				if mode == "unresolved group" {
					require.ErrorIs(t, err, ErrProxyGroupUnavailable)
					require.Empty(t, base.urls)
				} else {
					require.NoError(t, err)
					require.Equal(t, []string{wantURL}, base.urls)
					_ = response.Body.Close()
				}
				if mode == "legacy unbound" || mode == "single node" {
					require.Zero(t, cache.calls)
					require.Zero(t, g.reads)
				}
			})
		}
	}
}

func TestProxyGroupOutboundWebsearchAndCursorFailBeforeIO(t *testing.T) {
	_, _, _, a := runtimeFixture()
	_, _, err := doWebSearch(context.Background(), a, "query")
	require.ErrorIs(t, err, ErrProxyGroupUnavailable)
	a.Platform = PlatformCursor
	a.Type = AccountTypeUpstream
	a.Credentials = map[string]any{"base_url": "https://provider.invalid", "api_key": "fixture"}
	_, err = NewCursorGatewayService(nil, nil).AvailableModels(context.Background(), a)
	require.ErrorIs(t, err, ErrProxyGroupUnavailable)
	_, err = NewCursorTokenRefresher().Refresh(context.Background(), a)
	require.ErrorIs(t, err, ErrProxyGroupUnavailable)
}

type runtimeBindingBarrier struct {
	ProxyGroupCache
	selected chan struct{}
	proceed  chan struct{}
	once     sync.Once
}

func (b *runtimeBindingBarrier) TrySetBindingNX(ctx context.Context, g, a, p int64, ttl time.Duration) (int64, error) {
	winner, err := b.ProxyGroupCache.TrySetBindingNX(ctx, g, a, p, ttl)
	b.once.Do(func() { close(b.selected); <-b.proceed })
	return winner, err
}

func TestProxyGroupConcurrentMembershipAndRevocationBeforeOutbound(t *testing.T) {
	for _, revokeGroup := range []bool{false, true} {
		t.Run(map[bool]string{false: "member replaced", true: "group revoked"}[revokeGroup], func(t *testing.T) {
			r, g, cache, a := runtimeFixture()
			barrier := &runtimeBindingBarrier{ProxyGroupCache: cache, selected: make(chan struct{}), proceed: make(chan struct{})}
			r.cache = barrier
			a.AttachEgress(r)
			var resume sync.Once
			t.Cleanup(func() { resume.Do(func() { close(barrier.proceed) }) })
			base := &egressRecordingUpstream{}
			request, _ := http.NewRequest(http.MethodGet, "https://provider.invalid/models", nil)
			completed := make(chan error, 1)
			go func() {
				response, err := accountHTTPUpstream(base, a).Do(request, "", a.ID, 1)
				if response != nil {
					_ = response.Body.Close()
				}
				completed <- err
			}()
			select {
			case <-barrier.selected:
			case <-time.After(2 * time.Second):
				t.Fatal("resolver did not reach NX barrier")
			}
			cache.mu.Lock()
			old := cache.bindings[[2]int64{7, a.ID}]
			cache.mu.Unlock()
			g.mu.Lock()
			if revokeGroup {
				g.group.Status = "inactive"
			} else {
				members := []ProxyGroupMember{}
				for _, member := range g.group.Members {
					if member.ProxyID != old {
						members = append(members, member)
					}
				}
				g.group.Members = members
			}
			g.mu.Unlock()
			resume.Do(func() { close(barrier.proceed) })
			select {
			case err := <-completed:
				if revokeGroup {
					require.ErrorIs(t, err, ErrProxyGroupUnavailable)
					require.Empty(t, base.urls)
				} else {
					require.NoError(t, err)
					require.Len(t, base.urls, 1)
					require.NotEqual(t, old, cache.bindings[[2]int64{7, a.ID}])
				}
			case <-time.After(2 * time.Second):
				t.Fatal("resolver did not leave NX barrier")
			}
		})
	}
}
