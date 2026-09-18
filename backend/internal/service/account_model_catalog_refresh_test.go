package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestModelCatalogFreshnessBoundary(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name         string
		age          time.Duration
		result, want string
	}{
		{"within grace", 23 * time.Hour, "failed", "fresh"},
		{"exact boundary", 24 * time.Hour, "failed", "fresh"},
		{"past boundary", 24*time.Hour + time.Nanosecond, "failed", "stale"},
		{"invalid credentials", 25 * time.Hour, "credentials_invalid", "stale"},
		{"successful refresh", 25 * time.Hour, "succeeded", "fresh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := &AccountModelCatalogSnapshot{ObservedAt: now.Add(-tc.age).Format(time.RFC3339Nano), Complete: true, DiscoveryScope: "account", EligibleModels: []AccountCatalogModel{{ID: "kept"}}}
			account := &Account{Extra: map[string]any{UpstreamModelCatalogExtraKey: snapshot, UpstreamModelCatalogStateExtraKey: AccountModelCatalogState{Result: tc.result}}}
			got, success := account.ModelCatalogFreshness(now)
			require.Equal(t, tc.want, got)
			require.Equal(t, snapshot.ObservedAt, success)
			require.Len(t, account.GetUpstreamModelCatalog().EligibleModels, 1)
		})
	}
	got, success := (&Account{}).ModelCatalogFreshness(now)
	require.Equal(t, "unknown", got)
	require.Empty(t, success)
}

type refreshCatalogRepository struct {
	*catalogFixtureRepository
	active []Account
}

func (r *refreshCatalogRepository) ListActive(context.Context) ([]Account, error) {
	return r.active, nil
}

func TestAccountModelCatalogScheduledRefreshDueAndFailureRetention(t *testing.T) {
	now := time.Now()
	old := &AccountModelCatalogSnapshot{Revision: 1, ObservedAt: now.Add(-25 * time.Hour).UTC().Format(time.RFC3339Nano), Complete: true, DiscoveryScope: "account", EligibleModels: []AccountCatalogModel{{ID: "kept"}}}
	account := &Account{ID: 23, Status: StatusActive, Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test", "base_url": "https://api.deepseek.com"}, Extra: map[string]any{UpstreamModelCatalogExtraKey: old}}
	repo := &refreshCatalogRepository{catalogFixtureRepository: &catalogFixtureRepository{accounts: map[int64]*Account{23: account}}, active: []Account{*account}}
	upstream := &httpUpstreamRecorder{resp: catalogFixtureResponse(`{"data":[]}`)}
	upstream.resp.StatusCode = 502
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
	runner := &ScheduledTestRunnerService{accountTestSvc: svc}
	runner.refreshDueCatalogs(context.Background(), now)
	require.Equal(t, 1, repo.commits)
	require.Equal(t, "failed", account.ModelCatalogState().Result)
	require.Equal(t, old, account.GetUpstreamModelCatalog())
	stale, _ := account.ModelCatalogFreshness(now)
	require.Equal(t, "stale", stale)
	require.False(t, accountCatalogRefreshDue(account, now.Add(5*time.Hour)))
	require.True(t, accountCatalogRefreshDue(account, now.Add(7*time.Hour)))
	runner.refreshDueCatalogs(context.Background(), now)
	require.Equal(t, 1, repo.commits, "failed attempts also defer the next refresh")
	account.Status = "disabled"
	require.False(t, accountCatalogRefreshDue(account, now.Add(48*time.Hour)))
}

func TestAccountModelCatalogInFlightSkipsUpstream(t *testing.T) {
	svc := &AccountTestService{}
	svc.catalogInFlight.Store(int64(42), struct{}{})
	_, err := svc.ApplyAccountModelCatalog(context.Background(), &Account{ID: 42}, nil)
	require.ErrorIs(t, err, ErrAccountModelCatalogConflict)
}

func TestModelCatalogRefreshScheduleSwitch(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		runner := &ScheduledTestRunnerService{}
		runner.SetModelCatalogRefreshEnabled(enabled)
		runner.Start()
		want := 1
		if enabled {
			want++
		}
		require.Len(t, runner.cron.Entries(), want)
		runner.Stop()
	}
}

func TestModelCatalogRefreshConcurrentManualApply(t *testing.T) {
	account := &Account{ID: 61, Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test", "base_url": "https://api.deepseek.com"}}
	repo := &concurrentCatalogRepository{catalogFixtureRepository: &catalogFixtureRepository{accounts: map[int64]*Account{61: account}}, ready: make(chan struct{}, 1), release: make(chan struct{})}
	svc := &AccountTestService{accountRepo: repo, cfg: upstreamModelSyncTestConfig(), httpUpstream: &httpUpstreamRecorder{resp: catalogFixtureResponse(`{"data":[]}`)}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := svc.ApplyAccountModelCatalog(ctx, account, nil); result <- err }()
	select {
	case <-repo.ready:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	_, err := svc.ApplyAccountModelCatalog(ctx, &Account{ID: 61}, nil)
	require.ErrorIs(t, err, ErrAccountModelCatalogConflict)
	close(repo.release)
	require.NoError(t, <-result)
	require.Equal(t, 1, repo.commits)
}
