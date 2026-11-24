package service

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccountRuntimeIdentityConfigurationAndPresentation(t *testing.T) {
	original := &Account{ID: 17, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "secret"}, Extra: map[string]any{}, Proxy: &Proxy{Host: "first", Port: 80, Protocol: "http"}}
	key := AccountRuntimeIdentityKey(original)
	require.NotContains(t, key, "secret")
	for _, change := range []func(*Account){
		func(account *Account) { account.Credentials["access_token"] = "replacement" },
		func(account *Account) { account.Credentials["header_overrides"] = map[string]any{"X-Tenant": "other"} },
		func(account *Account) { account.Platform = PlatformAnthropic },
		func(account *Account) { account.Type = AccountTypeAPIKey },
		func(account *Account) { account.Proxy.Host = "second" },
		func(account *Account) { account.Extra["secret_revision"] = 2 },
		func(account *Account) {
			account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 2}
		},
	} {
		account := *original
		account.Credentials = shallowCopyMap(original.Credentials)
		account.Extra = shallowCopyMap(original.Extra)
		proxy := *original.Proxy
		account.Proxy = &proxy
		change(&account)
		require.NotEqual(t, key, AccountRuntimeIdentityKey(&account))
	}
	copy := *original
	note := "a note"
	copy.Notes = &note
	copy.Name = "renamed"
	copy.Status = StatusDisabled
	require.Equal(t, key, AccountRuntimeIdentityKey(&copy))
}

func TestAccountRuntimeInvalidatorClearsCacheFamilyOnlyOnIdentityChange(t *testing.T) {
	account := &Account{ID: 29, Credentials: map[string]any{"api_key": "old"}}
	cache := NewUsageCache()
	key := AccountRuntimeIdentityKey(account)
	entries := []*sync.Map{&cache.apiCache, &cache.windowStatsCache, &cache.antigravityCache, &cache.cursorCache, &cache.openAIProbeCache, &cache.grokProbeCache}
	for _, entry := range entries {
		entry.Store(key, true)
		entry.Store("other", true)
	}
	invalidator := &AccountRuntimeInvalidator{Usage: cache}
	unchanged := *account
	unchanged.Name = "new name"
	require.NoError(t, invalidator.Invalidate(context.Background(), account, &unchanged))
	for _, entry := range entries {
		_, found := entry.Load(key)
		require.True(t, found)
	}
	changed := *account
	changed.Credentials = map[string]any{"api_key": "new"}
	require.NoError(t, invalidator.Invalidate(context.Background(), account, &changed))
	for _, entry := range entries {
		_, found := entry.Load(key)
		require.False(t, found)
		_, found = entry.Load("other")
		require.True(t, found)
	}
	// A late old flight cannot become a hit for the new identity.
	cache.apiCache.Store(key, &apiUsageCache{timestamp: time.Now()})
	_, found := cache.apiCache.Load(AccountRuntimeIdentityKey(&changed))
	require.False(t, found)
}

func TestRuntimeCooldownScopeAndBoundedFallback(t *testing.T) {
	account := &Account{ID: 901, Credentials: map[string]any{"api_key": "first"}}
	key := AccountRuntimeCooldownKey(3, account, "actual-model")
	require.NotEqual(t, key, AccountRuntimeCooldownKey(4, account, "actual-model"))
	require.NotEqual(t, key, AccountRuntimeCooldownKey(3, account, "other-model"))
	other := *account
	other.ID++
	require.NotEqual(t, key, AccountRuntimeCooldownKey(3, &other, "actual-model"))
	other = *account
	other.Credentials = map[string]any{"api_key": "second"}
	require.NotEqual(t, key, AccountRuntimeCooldownKey(3, &other, "actual-model"))
	service := &AntigravityGatewayService{}
	service.setRuntimeCooldown(context.Background(), key, time.Now().Add(time.Minute))
	service.setRuntimeCooldown(context.Background(), AccountRuntimeCooldownKey(4, account, "actual-model"), time.Time{})
	cooldownUntil, recorded := service.runtimeCooldown(context.Background(), key)
	require.True(t, recorded)
	require.True(t, time.Now().Before(cooldownUntil))
	service.setRuntimeCooldown(context.Background(), key, time.Time{})
	_, recorded = service.runtimeCooldown(context.Background(), key)
	require.False(t, recorded)
	for entryIndex := 0; entryIndex <= runtimeCooldownFallbackLimit; entryIndex++ {
		service.setRuntimeCooldown(context.Background(), "bounded-test:"+strconv.Itoa(entryIndex), time.Now().Add(time.Minute))
	}
	runtimeCooldownFallback.Lock()
	require.LessOrEqual(t, len(runtimeCooldownFallback.entries), runtimeCooldownFallbackLimit)
	runtimeCooldownFallback.entries = make(map[string]time.Time)
	runtimeCooldownFallback.Unlock()
}

func TestRuntimeShadowIdentityTracksOwnerRevision(t *testing.T) {
	owner := &Account{ID: 123, Credentials: map[string]any{"access_token": "first"}}
	shadow := &Account{ID: 124, ParentAccountID: &owner.ID}
	first, err := AccountWithRuntimeCredentialOwner(shadow, owner)
	require.NoError(t, err)
	owner.Credentials = map[string]any{"access_token": "second"}
	second, err := AccountWithRuntimeCredentialOwner(shadow, owner)
	require.NoError(t, err)
	require.NotEqual(t, AccountRuntimeIdentityKey(first), AccountRuntimeIdentityKey(second))
	require.NotEqual(t, AccountRuntimeCooldownKey(1, first, "model"), AccountRuntimeCooldownKey(1, second, "model"))
	require.Equal(t, AccountRuntimeCooldownKey(1, owner, "model"), AccountRuntimeCooldownKey(1, second, "model"))
}

type runtimeSnapshotTestRepo struct {
	AccountRepository
	current *Account
	writes  int
}

func (repo *runtimeSnapshotTestRepo) UpdateAccountRuntimeSnapshot(_ context.Context, expected *Account, _ map[string]any, _ *time.Time) error {
	if AccountRuntimeIdentityKey(expected) != AccountRuntimeIdentityKey(repo.current) {
		return ErrAccountRuntimeIdentityChanged
	}
	repo.writes++
	return nil
}

func TestRuntimeSnapshotRejectsOldRevision(t *testing.T) {
	old := &Account{ID: 7, Credentials: map[string]any{"api_key": "old"}}
	current := &Account{ID: 7, Credentials: map[string]any{"api_key": "new"}}
	repo := &runtimeSnapshotTestRepo{current: current}
	service := &AccountUsageService{accountRepo: repo}
	require.ErrorIs(t, service.persistRuntimeSnapshot(context.Background(), old, map[string]any{"usage": 1}, nil), ErrAccountRuntimeIdentityChanged)
	require.Zero(t, repo.writes)
	require.NoError(t, service.persistRuntimeSnapshot(context.Background(), current, map[string]any{"usage": 2}, nil))
	require.Equal(t, 1, repo.writes)
	service.accountRepo = nil
	require.Error(t, service.persistRuntimeSnapshot(context.Background(), current, nil, nil))
	require.False(t, errors.Is(service.persistRuntimeSnapshot(context.Background(), current, nil, nil), ErrAccountRuntimeIdentityChanged))
}

func (repo *runtimeSnapshotTestRepo) GetByID(context.Context, int64) (*Account, error) {
	return snapshotOAuthRefreshAccount(repo.current), nil
}

func (repo *runtimeSnapshotTestRepo) UpdateAccountRuntimeCredentials(_ context.Context, expected *Account, credentials map[string]any) error {
	if AccountRuntimeIdentityKey(expected) != AccountRuntimeIdentityKey(repo.current) {
		return ErrAccountRuntimeIdentityChanged
	}
	repo.current.Credentials = shallowCopyMap(credentials)
	repo.writes++
	return nil
}

type runtimeChangingRefreshExecutor struct{ repo *runtimeSnapshotTestRepo }

func (executor runtimeChangingRefreshExecutor) CacheKey(account *Account) string {
	return OpenAITokenCacheKey(account)
}
func (runtimeChangingRefreshExecutor) CanRefresh(*Account) bool                  { return true }
func (runtimeChangingRefreshExecutor) NeedsRefresh(*Account, time.Duration) bool { return true }
func (executor runtimeChangingRefreshExecutor) Refresh(context.Context, *Account) (map[string]any, error) {
	executor.repo.current.Credentials = map[string]any{"refresh_token": "new-owner"}
	return map[string]any{"access_token": "late-old-owner-token"}, nil
}

func TestRuntimeOAuthRefreshCannotOverwriteReplacement(t *testing.T) {
	account := &Account{ID: 92, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"refresh_token": "old-owner"}}
	repo := &runtimeSnapshotTestRepo{current: snapshotOAuthRefreshAccount(account)}
	api := NewOAuthRefreshAPI(repo, nil)
	_, err := api.RefreshIfNeeded(context.Background(), account, runtimeChangingRefreshExecutor{repo}, time.Minute)
	require.ErrorIs(t, err, ErrAccountRuntimeIdentityChanged)
	require.Zero(t, repo.writes)
	require.Equal(t, "new-owner", repo.current.GetCredential("refresh_token"))
	require.Empty(t, repo.current.GetCredential("access_token"))
}

func TestAntigravityOAuthRefreshKeepsProjectIdentity(t *testing.T) {
	account := &Account{ID: 92, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Status: StatusActive,
		Credentials: map[string]any{"project_id": "stable-project", "access_token": "old", "refresh_token": "old-refresh", "credential_secret_version": 1}}
	incoming := snapshotOAuthRefreshAccount(account)
	incoming.Credentials["access_token"] = "new"
	incoming.Credentials["refresh_token"] = "new-refresh"
	incoming.Credentials["credential_secret_version"] = 2
	AdvanceAccountModelCatalogRevisionsForUpdate(account, incoming, false)
	require.Zero(t, incoming.ModelCatalogState().IdentityRevision)
	require.Equal(t, account.GetUpstreamModelCatalog(), incoming.GetUpstreamModelCatalog())

	incoming.Credentials["project_id"] = "different-project"
	AdvanceAccountModelCatalogRevisionsForUpdate(account, incoming, false)
	require.EqualValues(t, 1, incoming.ModelCatalogState().IdentityRevision)
}

func TestRegistryDownloadAndDraftIsolation(t *testing.T) {
	key := ModelRegistryDownloadKey("https://source", "http://proxy", "verified")
	for _, other := range []string{ModelRegistryDownloadKey("https://other", "http://proxy", "verified"), ModelRegistryDownloadKey("https://source", "http://other", "verified"), ModelRegistryDownloadKey("https://source", "http://proxy", "unverified")} {
		require.NotEqual(t, key, other)
	}
	draft := &Account{}
	_, err := ModelRegistryEnrichmentKey(key, draft, "")
	require.Error(t, err)
	first, err := ModelRegistryEnrichmentKey(key, draft, "first")
	require.NoError(t, err)
	second, err := ModelRegistryEnrichmentKey(key, draft, "second")
	require.NoError(t, err)
	require.NotEqual(t, first, second)
}

func TestClearRuntimeObservationsPreservesFinancialAndOperationalState(t *testing.T) {
	account := &Account{Status: StatusDisabled, ErrorMessage: "real outstanding balance", Extra: map[string]any{
		"cursor_usage_snapshot": "old", "codex_5h_used_percent": 98,
		"kimi_balance": 12, "quota_used": 400, "quota_limit": 500,
		"kimi_balance_low": true, "auto_reset_enabled": true,
	}}
	previous := account.Extra
	ClearAccountRuntimeObservations(account)
	require.NotContains(t, account.Extra, "cursor_usage_snapshot")
	require.NotContains(t, account.Extra, "codex_5h_used_percent")
	require.NotContains(t, account.Extra, "kimi_balance")
	require.Equal(t, 400, account.Extra["quota_used"])
	require.Equal(t, 500, account.Extra["quota_limit"])
	require.Equal(t, true, account.Extra["kimi_balance_low"])
	require.Equal(t, StatusDisabled, account.Status)
	require.Equal(t, "real outstanding balance", account.ErrorMessage)
	require.Contains(t, previous, "cursor_usage_snapshot", "old snapshots must remain immutable")
}

func (repo *poolHealthAccountRepo) UpdateAccountRuntimeCredentials(ctx context.Context, expected *Account, credentials map[string]any) error {
	return repo.UpdateCredentials(ctx, expected.ID, credentials)
}

func (repo *productionPathRateRepo) UpdateAccountRuntimeCredentials(ctx context.Context, expected *Account, credentials map[string]any) error {
	return repo.UpdateCredentials(ctx, expected.ID, credentials)
}

func (repo *tokenRefreshCandidateRepo) UpdateAccountRuntimeCredentials(ctx context.Context, expected *Account, credentials map[string]any) error {
	return repo.UpdateCredentials(ctx, expected.ID, credentials)
}

func (repo *grokReconcileRepo) UpdateAccountRuntimeCredentials(ctx context.Context, expected *Account, credentials map[string]any) error {
	return repo.UpdateCredentials(ctx, expected.ID, credentials)
}

func (repo *sparkShadowUsageTestRepo) UpdateAccountRuntimeSnapshot(ctx context.Context, expected *Account, updates map[string]any, _ *time.Time) error {
	return repo.UpdateExtra(ctx, expected.ID, updates)
}

func (repo *snapshotUpdateAccountRepo) UpdateAccountRuntimeSnapshot(ctx context.Context, expected *Account, updates map[string]any, _ *time.Time) error {
	return repo.UpdateExtra(ctx, expected.ID, updates)
}

func (repo *openAICodexSnapshotAsyncRepo) UpdateAccountRuntimeSnapshot(ctx context.Context, expected *Account, updates map[string]any, _ *time.Time) error {
	return repo.UpdateExtra(ctx, expected.ID, updates)
}
