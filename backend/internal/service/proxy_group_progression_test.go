//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProxyGroupTLSAlternateProgressesPastTwoBadRoutes(t *testing.T) {
	r, group, cache, account := runtimeFixture()
	nodes := map[int64]*Proxy{}
	for _, member := range group.group.Members {
		nodes[member.ProxyID] = member.Proxy
	}
	// Derive the unchanged initial HRW ranking independently of alternation.
	var ranking []*Proxy
	for len(nodes) > 0 {
		id := rendezvousProxy(7, "91", nodes)
		ranking = append(ranking, nodes[id])
		delete(nodes, id)
	}
	wantAttempts := [][]string{
		{ranking[0].URL(), ranking[1].URL()},
		{ranking[1].URL(), ranking[2].URL()},
		{ranking[2].URL()},
	}
	failure := &ProxyTLSHandshakeFailure{Cause: io.EOF}
	var attempts [][]string
	var failures []bool
	var bindings []int64
	for operation := 0; operation < 3; operation++ {
		a, target, err := r.ApplyEgress(context.Background(), account)
		require.NoError(t, err)
		request, err := http.NewRequest(http.MethodGet, "https://provider.invalid/", nil)
		require.NoError(t, err)
		var routes []string
		_, err = (&groupAccountUpstream{account: a}).do(request, target.URL, func(_ *http.Request, route string) (*http.Response, error) {
			routes = append(routes, route)
			if route != ranking[2].URL() {
				return nil, failure
			}
			return &http.Response{StatusCode: http.StatusOK}, nil
		})
		require.LessOrEqual(t, len(routes), 2)
		attempts = append(attempts, routes)
		failures = append(failures, err != nil)
		bindings = append(bindings, cache.bindings[[2]int64{7, account.ID}])
		require.Empty(t, cache.cooldowns)
	}
	t.Logf("ranking=%v attempts=%v failures=%v bindings=%v", []int64{ranking[0].ID, ranking[1].ID, ranking[2].ID}, attempts, failures, bindings)
	require.Equal(t, wantAttempts, attempts)
	require.Equal(t, []bool{true, false, false}, failures)
	require.Equal(t, []int64{ranking[1].ID, ranking[2].ID, ranking[2].ID}, bindings)
}

func TestProxyGroupAlternateOrderingAndEligibility(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failed   int64
		remove   bool
		cooldown bool
		proxyOwn bool
		want     int64
	}{
		{name: "initial unchanged", want: 13},
		{name: "first successor", failed: 13, want: 12},
		{name: "second successor", failed: 12, want: 11},
		{name: "wrap", failed: 11, want: 13},
		{name: "removed cursor", failed: 12, remove: true, want: 11},
		{name: "route only cooled cursor", failed: 12, cooldown: true, want: 11},
		{name: "proxy own excluded cursor", failed: 12, proxyOwn: true, want: 13},
		{name: "proxy own cooldown", failed: 12, cooldown: true, proxyOwn: true, want: 13},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, group, cache, account := runtimeFixture()
			if tc.remove {
				var members []ProxyGroupMember
				for _, member := range group.group.Members {
					if member.ProxyID != tc.failed {
						members = append(members, member)
					}
				}
				group.group.Members = members
			}
			if tc.cooldown {
				cache.cooldowns[tc.failed] = ProxyCooldown{ProxyID: tc.failed, ExpiresAt: cache.now.Add(time.Minute)}
			}
			target, err := r.resolveExcluding(context.Background(), account, tc.failed, !tc.proxyOwn)
			require.NoError(t, err)
			require.Equal(t, tc.want, target.ProxyID)
			require.Equal(t, tc.want, cache.bindings[[2]int64{7, account.ID}])
		})
	}
}

func TestProxyGroupAlternateRevalidatesMembershipAfterNX(t *testing.T) {
	r, group, cache, account := runtimeFixture()
	group.afterRead = func(read int, group *ProxyGroup) {
		if read != 2 {
			return
		}
		require.Equal(t, int64(11), cache.bindings[[2]int64{7, account.ID}])
		var members []ProxyGroupMember
		for _, member := range group.Members {
			if member.ProxyID != 11 {
				members = append(members, member)
			}
		}
		group.Members = members
	}
	target, err := r.resolveExcluding(context.Background(), account, 12, true)
	require.NoError(t, err)
	require.Equal(t, int64(13), target.ProxyID)
	require.Equal(t, target.ProxyID, cache.bindings[[2]int64{7, account.ID}])
	require.Empty(t, cache.cooldowns)
}

func TestProxyGroupBindingResetLosesProgressionCursor(t *testing.T) {
	r, _, cache, account := runtimeFixture()
	ctx := context.Background()
	target, err := r.resolveExcluding(ctx, account, 12, true)
	require.NoError(t, err)
	require.Equal(t, int64(11), target.ProxyID)
	// Model loss of the existing cache entry, including its ordinary TTL expiry.
	delete(cache.bindings, [2]int64{7, account.ID})
	_, target, err = r.ApplyEgress(ctx, account)
	require.NoError(t, err)
	require.Equal(t, int64(13), target.ProxyID)
}

func TestProxyGroupTLSAlternateAllBadRoutesWrapWithTwoAttempts(t *testing.T) {
	r, _, cache, account := runtimeFixture()
	failure := &ProxyTLSHandshakeFailure{Cause: io.EOF}
	for _, want := range [][]int64{{13, 12}, {12, 11}, {11, 13}} {
		a, target, err := r.ApplyEgress(context.Background(), account)
		require.NoError(t, err)
		require.Equal(t, want[0], target.ProxyID)
		request, err := http.NewRequest(http.MethodGet, "https://provider.invalid/", nil)
		require.NoError(t, err)
		calls := 0
		_, err = (&groupAccountUpstream{account: a}).do(request, target.URL, func(_ *http.Request, _ string) (*http.Response, error) {
			calls++
			return nil, failure
		})
		require.Same(t, failure, err)
		require.Equal(t, 2, calls)
		require.Equal(t, want[1], cache.bindings[[2]int64{7, account.ID}])
		require.Empty(t, cache.cooldowns)
	}
}

func TestProxyGroupOwnFailureKeepsHighestHRWAndExclusion(t *testing.T) {
	for _, tc := range []struct {
		name          string
		clearCooldown bool
		failed, want  int64
	}{
		{"middle cooldown retained", false, 12, 13},
		{"middle cooldown cleared", true, 12, 13},
		{"first cooldown cleared", true, 13, 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, group, cache, account := runtimeFixture()
			cache.bindings[[2]int64{7, account.ID}] = tc.failed
			a, target, err := r.ApplyEgress(context.Background(), account)
			require.NoError(t, err)
			group.afterRead = func(_ int, _ *ProxyGroup) {
				if tc.clearCooldown {
					delete(cache.cooldowns, tc.failed)
				}
			}
			request, err := http.NewRequest(http.MethodGet, "https://provider.invalid/", nil)
			require.NoError(t, err)
			calls := 0
			_, err = (&groupAccountUpstream{account: a}).do(request, target.URL, func(_ *http.Request, _ string) (*http.Response, error) {
				calls++
				if calls == 1 {
					return nil, &ProxyConnectFailure{Cause: io.EOF}
				}
				return &http.Response{StatusCode: http.StatusOK}, nil
			})
			require.NoError(t, err)
			require.Equal(t, 2, calls)
			require.Equal(t, tc.want, cache.bindings[[2]int64{7, account.ID}], "proxy-own failure keeps highest HRW while excluding the failed node")
		})
	}
}
