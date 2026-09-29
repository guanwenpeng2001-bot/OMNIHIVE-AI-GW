package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AccountRuntimeIdentityKey excludes presentation and financial state. The digest
// keeps credentials, headers and proxy passwords out of cache keys and logs.
func AccountRuntimeIdentityKey(account *Account) string {
	if account == nil {
		return "missing"
	}
	identity := map[string]any{
		"platform": account.Platform, "type": account.Type,
		"credentials": account.Credentials, "revision": account.ModelCatalogState().IdentityRevision,
		"parent": account.ParentAccountID, "proxy_id": account.ProxyID,
		"quota_dimension": account.QuotaDimension,
	}
	if _, referenced := CredentialReference(account.Credentials); referenced {
		inline, _ := SplitCredentialSecrets(account.Credentials)
		inline[CredentialSecretVersionKey] = CredentialVersion(account.Credentials)
		identity["credentials"] = inline
	}
	if account.Proxy != nil {
		identity["proxy"] = []any{account.Proxy.URL(), account.Proxy.FallbackMode, account.Proxy.BackupProxyID, account.Proxy.Status}
	}
	for _, field := range []string{"secret_revision", "secret_reference", "credential_secret_id", "credential_secret_revision", "header_overrides", "base_url", "protocol"} {
		identity[field] = account.Extra[field]
	}
	if owner := account.modelCatalogCredentialAccount; owner != nil && owner != account {
		identity["owner"] = AccountRuntimeIdentityKey(owner)
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		panic("account runtime identity contains non-JSON configuration")
	}
	digest := sha256.Sum256(encoded)
	return strconv.FormatInt(account.ID, 10) + ":" + hex.EncodeToString(digest[:])
}

// AccountRuntimeCooldownKey scopes a cooldown to the actual credential and
// upstream model. Callers may pass a quota scope only with provider evidence.
func AccountRuntimeCooldownKey(groupID int64, account *Account, modelOrQuotaScope string) string {
	owner := account
	if account != nil && account.modelCatalogCredentialAccount != nil {
		owner = account.modelCatalogCredentialAccount
	}
	return fmt.Sprintf("group:%d:owner:%s:scope:%s", groupID, AccountRuntimeIdentityKey(owner), modelOrQuotaScope)
}

// AccountWithRuntimeCredentialOwner attaches the owner snapshot used by a
// shadow request. Repositories can use the same helper for an atomic CAS check.
func AccountWithRuntimeCredentialOwner(account, owner *Account) (*Account, error) {
	if account == nil || owner == nil || account.ParentAccountID == nil || *account.ParentAccountID != owner.ID || owner.ID == account.ID || owner.IsShadow() {
		return nil, errors.New("invalid runtime credential owner")
	}
	snapshot := snapshotOAuthRefreshAccount(account)
	snapshot.modelCatalogCredentialAccount = snapshotOAuthRefreshAccount(owner)
	return snapshot, nil
}

func loadRuntimeCredentialOwner(ctx context.Context, repo monitorAccountSource, account *Account) (*Account, error) {
	if account == nil || account.ParentAccountID == nil || account.modelCatalogCredentialAccount != nil {
		return account, nil
	}
	if repo == nil {
		return nil, errors.New("runtime credential owner repository is not configured")
	}
	owner, err := repo.GetByID(ctx, *account.ParentAccountID)
	if err != nil {
		return nil, err
	}
	return AccountWithRuntimeCredentialOwner(account, owner)
}

type accountRuntimeContextKey struct{}

func WithAccountRuntimeIdentity(ctx context.Context, account *Account) context.Context {
	return context.WithValue(ctx, accountRuntimeContextKey{}, AccountRuntimeIdentityKey(account))
}

// AccountRuntimeCacheSuffix lets existing counter interfaces keep their account
// ID API while their Redis implementations receive request identity evidence.
func AccountRuntimeCacheSuffix(ctx context.Context, accountID int64) string {
	if identity, ok := ctx.Value(accountRuntimeContextKey{}).(string); ok && strings.HasPrefix(identity, strconv.FormatInt(accountID, 10)+":") {
		return identity
	}
	return strconv.FormatInt(accountID, 10)
}

// AccountRuntimeInvalidator is called after a committed identity update with both
// snapshots, including affected shadows. It never resets usage ledger or status.
type AccountRuntimeInvalidator struct {
	Usage     *UsageCache
	Monitor   *ChannelMonitorQuotaFetcher
	Tokens    TokenCacheInvalidator
	Timeout   TimeoutCounterCache
	Forbidden OpenAI403CounterCache
	Internal  Internal500CounterCache
	Sessions  CursorSessionCache
	Gateway   *OpenAIGatewayService
	Health    interface {
		ResetRuntimeHealth(context.Context, int64) error
	}
}

func (invalidator *AccountRuntimeInvalidator) Invalidate(ctx context.Context, before, after *Account) error {
	if invalidator == nil || before == nil || (after != nil && AccountRuntimeIdentityKey(before) == AccountRuntimeIdentityKey(after)) {
		return nil
	}
	if invalidator.Usage != nil {
		invalidator.Usage.InvalidateAccount(before.ID)
	}
	if invalidator.Monitor != nil {
		invalidator.Monitor.InvalidateAccount(before.ID)
	}
	if invalidator.Gateway != nil {
		invalidator.Gateway.openaiAccountRuntimeBlockUntil.Delete(AccountRuntimeIdentityKey(before))
		invalidator.Gateway.openaiAccountRuntimeBlockGeneration.Delete(AccountRuntimeIdentityKey(before))
		throttle := invalidator.Gateway.getCodexSnapshotThrottle()
		throttle.mu.Lock()
		delete(throttle.lastByID, before.ID)
		throttle.mu.Unlock()
	}
	InvalidateAvailableModelsCacheForAccount(before)
	ctx = WithAccountRuntimeIdentity(ctx, before)
	var failures []error
	if invalidator.Sessions != nil {
		failures = append(failures, invalidator.Sessions.Delete(ctx, cursorSessionKey(before)))
	}
	if invalidator.Health != nil {
		failures = append(failures, invalidator.Health.ResetRuntimeHealth(ctx, before.ID))
	}
	if invalidator.Tokens != nil {
		failures = append(failures, invalidator.Tokens.InvalidateToken(ctx, before))
	}
	if invalidator.Timeout != nil {
		failures = append(failures, invalidator.Timeout.ResetTimeoutCount(ctx, before.ID))
	}
	if invalidator.Forbidden != nil {
		failures = append(failures, invalidator.Forbidden.ResetOpenAI403Count(ctx, before.ID))
	}
	if invalidator.Internal != nil {
		failures = append(failures, invalidator.Internal.ResetInternal500Count(ctx, before.ID))
	}
	return errors.Join(failures...)
}

// These writes must compare identity and mutate in one repository transaction.
// A missing writer never falls back to an unconditional asynchronous update.
type AccountRuntimeSnapshotWriter interface {
	UpdateAccountRuntimeSnapshot(context.Context, *Account, map[string]any, *time.Time) error
}

type AccountRuntimeCredentialsWriter interface {
	UpdateAccountRuntimeCredentials(context.Context, *Account, map[string]any) error
}

var ErrAccountRuntimeIdentityChanged = errors.New("account runtime identity changed")
var ErrAccountRuntimeWriterUnavailable = errors.New("account runtime CAS writer is not configured")

func persistRuntimeCredentials(ctx context.Context, repo AccountRepository, expected *Account, credentials map[string]any) error {
	writer, ok := repo.(AccountRuntimeCredentialsWriter)
	if !ok {
		return ErrAccountRuntimeWriterUnavailable
	}
	if err := writer.UpdateAccountRuntimeCredentials(ctx, expected, credentials); err != nil {
		return err
	}
	expected.Credentials = shallowCopyMap(credentials)
	return nil
}

func (service *AccountUsageService) persistRuntimeSnapshot(ctx context.Context, expected *Account, updates map[string]any, end *time.Time) error {
	return persistAccountRuntimeSnapshot(ctx, service.accountRepo, expected, updates, end)
}

func persistAccountRuntimeSnapshot(ctx context.Context, repo AccountRepository, expected *Account, updates map[string]any, end *time.Time) error {
	writer, ok := repo.(AccountRuntimeSnapshotWriter)
	if !ok {
		return ErrAccountRuntimeWriterUnavailable
	}
	return writer.UpdateAccountRuntimeSnapshot(ctx, expected, updates, end)
}

func (cache *UsageCache) InvalidateAccount(accountID int64) {
	prefix := strconv.FormatInt(accountID, 10) + ":"
	for _, entries := range []*sync.Map{&cache.apiCache, &cache.windowStatsCache, &cache.antigravityCache, &cache.cursorCache, &cache.openAIProbeCache, &cache.grokProbeCache} {
		entries.Delete(accountID)
		entries.Range(func(key, _ any) bool {
			if text, ok := key.(string); ok && strings.HasPrefix(text, prefix) {
				entries.Delete(key)
			}
			return true
		})
	}
}

func (fetcher *ChannelMonitorQuotaFetcher) InvalidateAccount(accountID int64) {
	fetcher.mu.Lock()
	defer fetcher.mu.Unlock()
	delete(fetcher.cache, accountID)
}

// ClearAccountRuntimeObservations is used by the identity-changing repository
// transaction before persisting Extra. It only removes observed snapshots;
// account status, financial consumption and configured limits are untouched.
func ClearAccountRuntimeObservations(account *Account) {
	if account == nil {
		return
	}
	account.Extra = shallowCopyMap(account.Extra)
	for _, field := range []string{
		"cursor_usage_snapshot", "cursor_usage_updated_at", "session_window_utilization",
		"passive_usage_sampled_at", "passive_usage_7d_utilization", "passive_usage_7d_reset",
		"passive_usage_7d_oi_utilization", "passive_usage_7d_oi_reset",
		grokBillingExtraKey, grokObservedModelsExtraKey, grokQuotaSnapshotExtraKey,
		"codex_usage_updated_at", "codex_primary_over_secondary_percent",
		"codex_primary_used_percent", "codex_primary_reset_after_seconds", "codex_primary_window_minutes",
		"codex_secondary_used_percent", "codex_secondary_reset_after_seconds", "codex_secondary_window_minutes",
		"codex_5h_used_percent", "codex_5h_reset_after_seconds", "codex_5h_reset_at", "codex_5h_window_minutes",
		"codex_7d_used_percent", "codex_7d_reset_after_seconds", "codex_7d_reset_at", "codex_7d_window_minutes",
	} {
		delete(account.Extra, field)
	}
	for _, provider := range []string{PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformOpenCodeGo, PlatformCommandCode} {
		for _, suffix := range []string{cnExtraSuffix5hUsed, cnExtraSuffix5hReset, cnExtraSuffixWeeklyUsed, cnExtraSuffixWeeklyReset, cnExtraSuffixMonthlyUsed, cnExtraSuffixMonthlyReset, cnExtraSuffixUsageUpdated, cnBalanceExtraSuffixBalance, cnBalanceExtraSuffixCurrency, cnBalanceExtraSuffixAvailable, cnBalanceExtraSuffixUpdated, cnBalanceExtraSuffixBalances} {
			delete(account.Extra, cnExtraKey(provider, suffix))
		}
	}
}
