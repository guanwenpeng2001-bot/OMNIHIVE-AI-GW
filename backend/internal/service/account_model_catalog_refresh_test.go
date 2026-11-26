package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
	runner := &ScheduledTestRunnerService{catalogRefreshEnabled: true, accountTestSvc: svc}
	runner.refreshDueCatalogs(context.Background(), now)
	require.Equal(t, 1, repo.commits)
	require.Equal(t, "failed", account.ModelCatalogState().Result)
	require.Equal(t, old, account.GetUpstreamModelCatalog())
	stale, _ := account.ModelCatalogFreshness(now)
	require.Equal(t, "stale", stale)
	require.False(t, accountCatalogScheduledRefreshDue(account, now.Add(5*time.Hour), true))
	require.True(t, accountCatalogScheduledRefreshDue(account, now.Add(7*time.Hour), true))
	runner.refreshDueCatalogs(context.Background(), now)
	require.Equal(t, 1, repo.commits, "failed attempts also defer the next refresh")
	account.Status = "disabled"
	require.False(t, accountCatalogScheduledRefreshDue(account, now.Add(48*time.Hour), true))
}

func TestAccountModelCatalogInFlightSkipsUpstream(t *testing.T) {
	svc := &AccountTestService{}
	svc.catalogInFlight.Store(int64(42), struct{}{})
	_, err := svc.ApplyAccountModelCatalog(context.Background(), &Account{ID: 42}, nil)
	require.ErrorIs(t, err, ErrAccountModelCatalogConflict)
}

func TestModelCatalogRefreshScheduleSwitch(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		runner := &ScheduledTestRunnerService{accountTestSvc: &AccountTestService{}}
		runner.SetModelCatalogRefreshEnabled(enabled)
		runner.Start()
		require.Len(t, runner.cron.Entries(), 2, "recovery is always scheduled")
		runner.Stop()
	}
}

func TestInvalidatedCatalogRefreshRetriesOnlyAfterBackoffOrNewIdentity(t *testing.T) {
	now := time.Now()
	previous := &AccountModelCatalogSnapshot{Revision: 2, IdentityRevision: 4, Complete: true, ObservedAt: now.Format(time.RFC3339Nano)}
	account := &Account{ID: 23, Status: StatusActive, Platform: PlatformDeepseek, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "fixture", "base_url": "https://api.deepseek.com"},
		Extra: map[string]any{
			UpstreamModelCatalogExtraKey: previous,
			UpstreamModelCatalogStateExtraKey: AccountModelCatalogState{
				IdentityRevision: 5, AttemptedAt: previous.ObservedAt, Result: "succeeded",
			},
		},
	}
	healthy := Account{ID: 24, Status: StatusActive}
	repo := &refreshCatalogRepository{catalogFixtureRepository: &catalogFixtureRepository{
		accounts: map[int64]*Account{account.ID: account, healthy.ID: &healthy},
	}, active: []Account{*account, healthy}}
	failure := catalogFixtureResponse(`{"error":"unavailable"}`)
	failure.StatusCode = 502
	upstream := &httpUpstreamRecorder{responses: []*http.Response{failure, catalogFixtureResponse(`{"data":[]}`)}}
	runner := &ScheduledTestRunnerService{accountTestSvc: &AccountTestService{accountRepo: repo, httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}}

	runner.refreshDueCatalogs(context.Background(), now)
	require.Equal(t, 1, repo.commits)
	require.Equal(t, previous, account.GetUpstreamModelCatalog())
	state := account.ModelCatalogState()
	require.Equal(t, "failed", state.Result)
	require.NotNil(t, state.AttemptedIdentityRevision)
	require.EqualValues(t, 5, *state.AttemptedIdentityRevision)
	runner.refreshDueCatalogs(context.Background(), now.Add(time.Minute))
	require.Equal(t, 1, repo.commits)
	require.True(t, accountCatalogScheduledRefreshDue(account, now.Add(7*time.Hour), false))

	state.IdentityRevision++
	account.Extra[UpstreamModelCatalogStateExtraKey] = state
	runner.refreshDueCatalogs(context.Background(), now.Add(time.Minute))
	require.Equal(t, 2, repo.commits)
	require.EqualValues(t, 6, account.GetUpstreamModelCatalog().IdentityRevision)
	runner.refreshDueCatalogs(context.Background(), now.Add(7*time.Hour))
	require.Equal(t, 2, repo.commits, "a recovered catalog stays out of the recovery-only schedule")
}

func TestReverseCatalogIdentityMismatchUsesSameRetryInterval(t *testing.T) {
	now := time.Now()
	attemptedIdentity := int64(5)
	state := AccountModelCatalogState{IdentityRevision: 4, AttemptedAt: now.Format(time.RFC3339Nano), AttemptedIdentityRevision: &attemptedIdentity}
	account := &Account{ID: 23, Status: StatusActive, Extra: map[string]any{
		UpstreamModelCatalogExtraKey:      AccountModelCatalogSnapshot{IdentityRevision: 5, Complete: true},
		UpstreamModelCatalogStateExtraKey: state,
	}}
	require.True(t, accountCatalogScheduledRefreshDue(account, now, false))

	attemptedIdentity = state.IdentityRevision
	interval := 6*time.Hour + time.Duration((account.ID*2654435761)%1801-900)*time.Second
	require.False(t, accountCatalogScheduledRefreshDue(account, now.Add(time.Minute), false))
	require.False(t, accountCatalogScheduledRefreshDue(account, now.Add(interval-time.Nanosecond), false))
	require.True(t, accountCatalogScheduledRefreshDue(account, now.Add(interval), false))
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
