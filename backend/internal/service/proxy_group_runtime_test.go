//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type runtimeGroups struct {
	ProxyGroupRepository
	mu        sync.Mutex
	group     ProxyGroup
	reads     int
	afterRead func(int, *ProxyGroup)
}

func (g *runtimeGroups) GetByID(context.Context, int64) (*ProxyGroup, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.reads++
	if g.afterRead != nil {
		g.afterRead(g.reads, &g.group)
	}
	copy := g.group
	copy.Members = append([]ProxyGroupMember(nil), g.group.Members...)
	return &copy, nil
}

type runtimeCache struct {
	ProxyGroupCache
	mu        sync.Mutex
	bindings  map[[2]int64]int64
	cooldowns map[int64]ProxyCooldown
	calls     int
	fail      error
	now       time.Time
}

func (c *runtimeCache) GetBinding(_ context.Context, g, a int64) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.bindings[[2]int64{g, a}], c.fail
}
func (c *runtimeCache) TrySetBindingNX(_ context.Context, g, a, p int64, ttl time.Duration) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if a <= 0 {
		panic("account zero binding")
	}
	if c.fail != nil {
		return 0, c.fail
	}
	k := [2]int64{g, a}
	if c.bindings[k] == 0 {
		c.bindings[k] = p
	}
	return c.bindings[k], nil
}
func (c *runtimeCache) TouchBindingIfValue(_ context.Context, g, a, p int64, ttl time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.bindings[[2]int64{g, a}] == p, c.fail
}
func (c *runtimeCache) DeleteBindingIfValue(_ context.Context, g, a, p int64) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	k := [2]int64{g, a}
	if c.bindings[k] != p {
		return false, c.fail
	}
	delete(c.bindings, k)
	return true, c.fail
}
func (c *runtimeCache) GetCooldown(_ context.Context, p int64) (*ProxyCooldown, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	v, ok := c.cooldowns[p]
	if !ok || !v.ExpiresAt.After(c.now) {
		return nil, c.fail
	}
	return &v, c.fail
}
func (c *runtimeCache) SetCooldownIfAbsent(_ context.Context, v ProxyCooldown, ttl time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if old, ok := c.cooldowns[v.ProxyID]; ok && old.ExpiresAt.After(c.now) {
		return false, c.fail
	}
	c.cooldowns[v.ProxyID] = v
	return true, c.fail
}
func runtimeFixture() (*ProxyGroupRuntime, *runtimeGroups, *runtimeCache, *Account) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	g := &runtimeGroups{group: ProxyGroup{ID: 7, WorkspaceID: 1, Status: StatusActive, FallbackMode: FallbackModeNone}}
	for _, id := range []int64{11, 12, 13} {
		g.group.Members = append(g.group.Members, ProxyGroupMember{GroupID: 7, WorkspaceID: 1, ProxyID: id, Proxy: &Proxy{ID: id, WorkspaceID: 1, Status: StatusActive, Protocol: "http", Host: "proxy.test", Port: int(id)}})
	}
	c := &runtimeCache{bindings: map[[2]int64]int64{}, cooldowns: map[int64]ProxyCooldown{}, now: now}
	r := NewProxyGroupRuntime(g, nil, c)
	r.now = func() time.Time { return c.now }
	id := int64(7)
	a := &Account{ID: 91, WorkspaceID: 1, ProxyGroupID: &id}
	return r, g, c, a
}
func TestProxyGroupRuntimeLegacyAndUnresolved(t *testing.T) {
	r, g, c, a := runtimeFixture()
	_, err := a.EgressProxyURL(context.Background())
	require.ErrorIs(t, err, ErrProxyGroupUnavailable)
	for _, a := range []*Account{{}, {ProxyID: &g.group.Members[0].ProxyID, Proxy: g.group.Members[0].Proxy}} {
		_, target, err := r.ApplyEgress(context.Background(), a)
		require.NoError(t, err)
		require.True(t, target.Resolved)
	}
	require.Zero(t, c.calls)
	require.Zero(t, g.reads)
}
func TestProxyGroupRuntimeConcurrentWinnerAndIdentity(t *testing.T) {
	r, _, c, a := runtimeFixture()
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan EgressTarget, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			copy, target, err := r.ApplyEgress(ctx, a)
			if !assertRuntimeResult(t, copy, target, err) {
				return
			}
			results <- target
		}()
	}
	wg.Wait()
	close(results)
	var first int64
	for target := range results {
		if first == 0 {
			first = target.ProxyID
		}
		require.Equal(t, first, target.ProxyID)
	}
	require.Nil(t, a.ProxyID)
	require.Nil(t, a.Proxy)
	require.Nil(t, a.egress)
	require.Equal(t, first, c.bindings[[2]int64{7, 91}])
}
func assertRuntimeResult(t *testing.T, a *Account, target EgressTarget, err error) bool {
	t.Helper()
	if err != nil {
		t.Error(err)
		return false
	}
	if a.ProxyID != nil || !target.Resolved {
		t.Error("logical identity changed or target unresolved")
		return false
	}
	return true
}
func TestProxyGroupRuntimeRevocationBetweenNXAndUse(t *testing.T) {
	for _, revoke := range []string{"member", "group"} {
		t.Run(revoke, func(t *testing.T) {
			r, g, c, a := runtimeFixture()
			g.afterRead = func(n int, g *ProxyGroup) {
				if n == 2 {
					if revoke == "group" {
						g.Status = "inactive"
						g.FallbackMode = FallbackModeDirect
					} else {
						g.Members = nil
					}
				}
			}
			_, _, err := r.ApplyEgress(context.Background(), a)
			require.ErrorIs(t, err, ErrProxyGroupUnavailable)
			if revoke == "member" {
				require.Empty(t, c.bindings)
			}
		})
	}
}
func TestProxyGroupRuntimeEligibilityFallbackAndCacheError(t *testing.T) {
	cases := []string{"inactive", "deleted", "expired", "workspace", "member workspace", "cooldown", "cache"}
	for _, reason := range cases {
		t.Run(reason, func(t *testing.T) {
			r, g, c, a := runtimeFixture()
			g.group.Members = g.group.Members[:1]
			p := g.group.Members[0].Proxy
			switch reason {
			case "inactive":
				p.Status = "inactive"
			case "deleted":
				p.DeletedAt = &c.now
			case "expired":
				p.ExpiresAt = &c.now
			case "workspace":
				p.WorkspaceID = 2
			case "member workspace":
				g.group.Members[0].WorkspaceID = 2
			case "cooldown":
				c.cooldowns[p.ID] = ProxyCooldown{ProxyID: p.ID, ExpiresAt: c.now.Add(time.Minute)}
			case "cache":
				c.fail = errors.New("cache unavailable")
			}
			_, _, err := r.ApplyEgress(context.Background(), a)
			require.ErrorIs(t, err, ErrProxyGroupUnavailable)
		})
	}
	r, g, _, a := runtimeFixture()
	g.group.Members = nil
	g.group.FallbackMode = FallbackModeDirect
	_, target, err := r.ApplyEgress(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, EgressSourceGroupFallbackDirect, target.Source)
	require.Empty(t, target.URL)
	g.group.Status = "inactive"
	_, _, err = r.ApplyEgress(context.Background(), a)
	require.ErrorIs(t, err, ErrProxyGroupUnavailable)
}
func TestProxyGroupRuntimeSessionNoZeroBindingAndMismatch(t *testing.T) {
	r, g, c, _ := runtimeFixture()
	ctx := context.Background()
	target, err := r.ResolveForNewSession(ctx, 7, "server-random-session")
	require.NoError(t, err)
	require.Empty(t, c.bindings)
	require.ErrorIs(t, r.ConfirmSessionBinding(ctx, &g.group, 0, target.ProxyID), ErrProxyBindingConflict)
	require.NoError(t, r.ConfirmSessionBinding(ctx, &g.group, 100, target.ProxyID))
	c.bindings[[2]int64{7, 101}] = 999
	require.ErrorIs(t, r.ConfirmSessionBinding(ctx, &g.group, 101, target.ProxyID), ErrProxyBindingConflict)
	require.Equal(t, int64(999), c.bindings[[2]int64{7, 101}])
}
func TestProxyGroupRuntimeFixedCooldownAndNoDestinationQuarantine(t *testing.T) {
	r, _, c, _ := runtimeFixture()
	ctx := context.Background()
	for _, err := range []error{errors.New("upstream 503"), errors.New("CONNECT destination denied"), context.Canceled, context.DeadlineExceeded} {
		require.NoError(t, r.ReportTransportFailure(ctx, 7, 91, 11, err))
	}
	require.Empty(t, c.cooldowns)
	require.Zero(t, c.calls)
	cause := &ProxyConnectFailure{Cause: errors.New("proxy dial refused")}
	require.NoError(t, r.ReportTransportFailure(ctx, 7, 91, 11, cause))
	deadline := c.cooldowns[11].ExpiresAt
	c.now = c.now.Add(time.Minute)
	require.NoError(t, r.ReportTransportFailure(ctx, 7, 91, 11, cause))
	require.Equal(t, deadline, c.cooldowns[11].ExpiresAt)
	c.now = deadline
	v, err := c.GetCooldown(ctx, 11)
	require.NoError(t, err)
	require.Nil(t, v)
}

type runtimeFallbackProxies struct {
	ProxyRepository
	proxy *Proxy
}

func (p *runtimeFallbackProxies) GetByID(context.Context, int64) (*Proxy, error) { return p.proxy, nil }

func TestProxyGroupRuntimeExplicitBackupStaysSticky(t *testing.T) {
	r, g, cache, a := runtimeFixture()
	members := g.group.Members
	g.group.Members = nil
	backup := &Proxy{ID: 99, WorkspaceID: 1, Status: StatusActive, Protocol: "http", Host: "backup.test", Port: 8080}
	r.proxies = &runtimeFallbackProxies{proxy: backup}
	g.group.FallbackMode = FallbackModeProxy
	g.group.FallbackProxyID = &backup.ID
	_, target, err := r.ApplyEgress(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, EgressSourceGroupFallbackProxy, target.Source)
	require.Equal(t, backup.ID, cache.bindings[[2]int64{7, a.ID}])
	g.group.Members = members
	_, target, err = r.ApplyEgress(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, backup.ID, target.ProxyID, "recovered members do not silently replace an eligible sticky backup")
	backup.Status = "inactive"
	_, target, err = r.ApplyEgress(context.Background(), a)
	require.NoError(t, err)
	require.NotEqual(t, backup.ID, target.ProxyID)
	require.Equal(t, EgressSourceGroupBinding, target.Source)
	backup.Status = StatusActive
	backup.WorkspaceID = 2
	g.group.Members = nil
	_, _, err = r.ApplyEgress(context.Background(), a)
	require.ErrorIs(t, err, ErrProxyGroupUnavailable)
}
