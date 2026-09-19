//go:build unit

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
	"time"
)

type accountConfigCache struct {
	service.SchedulerCache
	calls int
}

func (c *accountConfigCache) SetAccount(_ context.Context, _ *service.Account) error {
	c.calls++
	return nil
}

func TestAccountConfigurationPostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to an isolated job_test database")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	var database string
	require.NoError(t, db.QueryRow(`SELECT current_database()`).Scan(&database))
	require.Equal(t, "job_test", database)
	require.NoError(t, ApplyMigrations(ctx, db))
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	cache := &accountConfigCache{}
	repo := newAccountRepositoryWithSQL(client, db, cache)
	t.Run("catalog_discovery_lock_excludes_other_instances", func(t *testing.T) {
		locker := repo
		release, err := locker.TryLockAccountModelCatalog(ctx, 42)
		require.NoError(t, err)
		defer release()
		other := newAccountRepositoryWithSQL(client, db, nil)
		_, err = other.TryLockAccountModelCatalog(ctx, 42)
		require.ErrorIs(t, err, service.ErrAccountModelCatalogConflict)
		independentRelease, err := other.TryLockAccountModelCatalog(ctx, 43)
		require.NoError(t, err)
		independentRelease()
		release()
		reacquiredRelease, err := other.TryLockAccountModelCatalog(ctx, 42)
		require.NoError(t, err)
		reacquiredRelease()
	})
	create := func(t *testing.T) *service.Account {
		row, err := client.Account.Create().SetName("before").SetPlatform(service.PlatformQianwen).SetType(service.AccountTypeAPIKey).
			SetCredentials(map[string]any{"api_key": "old", "model_mapping": map[string]any{"alias": "target"}}).
			SetExtra(map[string]any{"quota_used": 10, "quota_daily_used": 10, "quota_daily_limit": 100, "quota_daily_reset_at": "2030-01-01T00:00:00Z"}).Save(ctx)
		require.NoError(t, err)
		return accountEntityToService(row)
	}
	t.Run("stale_edit_preserves_usage_windows_and_managed_limits", func(t *testing.T) {
		edit := create(t)
		edit.Name = "renamed"
		until := time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)
		_, err := client.Account.UpdateOneID(edit.ID).SetExtra(map[string]any{
			"quota_used": 11, "quota_daily_used": 11, "codex_5h_reset_at": "2031-01-01T00:00:00Z", "quota_daily_limit": 100, "quota_daily_reset_at": "2031-01-01T00:00:00Z",
			"model_rate_limits": map[string]any{"model": "2031-01-01T00:00:00Z"},
		}).SetRateLimitResetAt(until).SetSessionWindowEnd(until).Save(ctx)
		require.NoError(t, err)
		require.NoError(t, repo.UpdateWithAccountBillingSettings(ctx, edit, nil, nil, nil))
		got, err := client.Account.Get(ctx, edit.ID)
		require.NoError(t, err)
		require.Equal(t, "renamed", got.Name)
		require.Equal(t, float64(11), got.Extra["quota_used"])
		require.Equal(t, float64(11), got.Extra["quota_daily_used"])
		require.Equal(t, "2031-01-01T00:00:00Z", got.Extra["quota_daily_reset_at"])
		require.Contains(t, got.Extra, "model_rate_limits")
		require.Equal(t, "2031-01-01T00:00:00Z", got.Extra["codex_5h_reset_at"])
		require.True(t, until.Equal(*got.RateLimitResetAt))
		require.True(t, until.Equal(*got.SessionWindowEnd))
		// Dedicated reset remains effective and a stale form cannot resurrect its usage.
		require.NoError(t, repo.ResetQuotaUsedAndClearRateLimitCooldown(ctx, edit.ID))
		require.NoError(t, repo.UpdateWithAccountBillingSettings(ctx, edit, nil, nil, nil))
		got, err = client.Account.Get(ctx, edit.ID)
		require.NoError(t, err)
		require.Equal(t, float64(0), got.Extra["quota_used"])
		require.NotContains(t, got.Extra, "quota_daily_reset_at")
		edit.Extra = map[string]any{}
		require.NoError(t, repo.UpdateWithAccountBillingSettings(ctx, edit, nil, nil, nil))
		got, err = client.Account.Get(ctx, edit.ID)
		require.NoError(t, err)
		require.NotContains(t, got.Extra, "quota_daily_limit", "explicit configuration clear is still applied")
		require.Equal(t, float64(0), got.Extra["quota_used"])
	})
	commit := func(account *service.Account) error {
		return repo.CommitAccountModelCatalog(ctx, account.ID, account.ModelCatalogBaseline(), &service.AccountModelCatalogSnapshot{Revision: account.ModelCatalogBaseline().SnapshotRevision + 1, IdentityRevision: account.ModelCatalogBaseline().IdentityRevision, Complete: true, DiscoveredModels: []string{}, EligibleModels: []service.AccountCatalogModel{}, ExcludedModels: []service.ExcludedCatalogModel{}}, account.ModelCatalogState(), nil)
	}
	t.Run("same_baseline_first_commit_wins_and_preserves_aliases", func(t *testing.T) {
		account := create(t)
		done := make(chan error, 2)
		go func() { done <- commit(account) }()
		go func() { done <- commit(account) }()
		first, second := <-done, <-done
		if first == nil {
			require.ErrorIs(t, second, service.ErrAccountModelCatalogConflict)
		} else {
			require.ErrorIs(t, first, service.ErrAccountModelCatalogConflict)
			require.NoError(t, second)
		}
		got, err := repo.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.Equal(t, account.Credentials, got.Credentials)
		require.NotNil(t, got.GetUpstreamModelCatalog())
		require.Empty(t, got.GetUpstreamModelCatalog().EligibleModels)
		stale := got.ModelCatalogBaseline()
		require.NoError(t, repo.UpdateCredentials(ctx, got.ID, map[string]any{"api_key": "rotated", "model_mapping": got.Credentials["model_mapping"]}))
		require.ErrorIs(t, repo.CommitAccountModelCatalog(ctx, got.ID, stale, nil, got.ModelCatalogState(), nil), service.ErrAccountModelCatalogConflict)
	})

	t.Run("rule_edits_conflict_with_pending_discovery", func(t *testing.T) {
		for _, location := range []string{"credentials", "extra"} {
			fields := service.AccountCatalogRuleCredentialFields()
			if location == "extra" {
				fields = service.AccountCatalogRuleExtraFields()
			}
			for _, field := range fields {
				for _, method := range []string{"ordinary", "patch", "bulk"} {
					t.Run(location+"/"+field+"/"+method, func(t *testing.T) {
						account := create(t)
						baseline := account.ModelCatalogBaseline()
						if location == "credentials" {
							account.Credentials[field] = map[string]any{"changed": true}
						} else {
							account.Extra[field] = true
						}
						switch method {
						case "ordinary":
							require.NoError(t, repo.UpdateWithAccountBillingSettings(ctx, account, nil, nil, nil))
						case "patch":
							if location == "credentials" {
								require.NoError(t, repo.UpdateCredentials(ctx, account.ID, account.Credentials))
							} else {
								require.NoError(t, repo.UpdateExtra(ctx, account.ID, map[string]any{field: true}))
							}
						case "bulk":
							updates := service.AccountBulkUpdate{}
							if location == "credentials" {
								updates.Credentials = map[string]any{field: account.Credentials[field]}
							} else {
								updates.Extra = map[string]any{field: true}
							}
							_, err := repo.BulkUpdate(ctx, []int64{account.ID}, updates)
							require.NoError(t, err)
						}
						current, err := repo.GetByID(ctx, account.ID)
						require.NoError(t, err)
						require.Equal(t, baseline.RulesRevision+1, current.ModelCatalogBaseline().RulesRevision)
						require.Equal(t, baseline.IdentityRevision, current.ModelCatalogBaseline().IdentityRevision)
						require.ErrorIs(t, repo.CommitAccountModelCatalog(ctx, account.ID, baseline, nil, account.ModelCatalogState(), nil), service.ErrAccountModelCatalogConflict)
					})
				}
			}
		}
	})
	t.Run("minimal_catalog_preserves_capabilities_and_rules", func(t *testing.T) {
		group, err := client.Group.Create().SetName("catalog-capabilities-" + time.Now().Format("150405.000000000")).SetPlatform(service.PlatformOpenAI).Save(ctx)
		require.NoError(t, err)
		for _, window := range []int64{8192, 32768} {
			reasoning := false
			metadata := service.UpstreamModelMetadataSnapshot{Models: map[string]service.UpstreamModelMetadata{"private-model": {ID: "private-model", Reasoning: &reasoning, ContextWindow: window, InputModalities: []string{"text"}, CodexToolCapabilities: map[string]json.RawMessage{"supports_search_tool": json.RawMessage("false")}}}}
			row, err := client.Account.Create().SetName("capability-fixture").SetPlatform(service.PlatformOpenAI).SetType(service.AccountTypeAPIKey).SetSchedulable(true).
				SetCredentials(map[string]any{"api_key": "must-not-project", "access_token": "must-not-project", "protocol_rules": []any{}, "api_base_urls": map[string]any{}, "openai_capabilities": map[string]any{}, "compact_model_mapping": map[string]any{}}).
				SetExtra(map[string]any{service.UpstreamModelCatalogExtraKey: service.AccountModelCatalogSnapshot{Complete: true, Revision: 1, DiscoveryScope: "account", DiscoveredModels: []string{"private-model"}, EligibleModels: []service.AccountCatalogModel{{ID: "private-model", Endpoints: []string{"responses"}, EvidenceKind: "native_account_catalog"}}}, service.UpstreamModelMetadataExtraKey: metadata}).Save(ctx)
			require.NoError(t, err)
			_, err = client.AccountGroup.Create().SetAccountID(row.ID).SetGroupID(group.ID).Save(ctx)
			require.NoError(t, err)
			projected, err := repo.GetModelCatalogAccount(ctx, row.ID)
			require.NoError(t, err)
			require.NotContains(t, projected.Credentials, "api_key")
			require.NotContains(t, projected.Credentials, "access_token")
			for _, field := range []string{"protocol_rules", "api_base_urls", "openai_capabilities", "compact_model_mapping"} {
				require.Contains(t, projected.Credentials, field)
			}
			capability, ok := projected.GetUpstreamModelMetadata("private-model")
			require.True(t, ok)
			require.Equal(t, window, capability.ContextWindow)
			require.NotNil(t, capability.Reasoning)
			require.False(t, *capability.Reasoning)
		}

	})
	t.Run("revoked_product_members_require_new_apply", func(t *testing.T) {
		for _, disable := range []bool{false, true} {
			account := create(t)
			snapshot := &service.AccountModelCatalogSnapshot{Revision: 1, Complete: true, DiscoveryScope: "product", DiscoveredModels: []string{"first", "second"}, EligibleModels: []service.AccountCatalogModel{
				{ID: "first", Endpoints: []string{"chat_completions"}, EvidenceKind: "administrator_confirmation"},
				{ID: "second", Endpoints: []string{"chat_completions"}, EvidenceKind: "administrator_confirmation"},
			}}
			require.NoError(t, repo.CommitAccountModelCatalog(ctx, account.ID, account.ModelCatalogBaseline(), snapshot, account.ModelCatalogState(), nil))
			reload := func() { var err error; account, err = repo.GetByID(ctx, account.ID); require.NoError(t, err) }
			reload()
			allowlist := service.AccountModelAllowlist{Enabled: !disable, Models: []string{"first"}, ConfirmedBy: 1, ConfirmedAt: time.Now().UTC().Format(time.RFC3339Nano), Reason: "test"}
			require.NoError(t, repo.UpdateAccountModelAllowlist(ctx, account.ID, account.ModelCatalogBaseline(), allowlist))
			reload()
			allowlist.Enabled = true
			allowlist.Models = []string{"first", "second", "new"}
			require.NoError(t, repo.UpdateAccountModelAllowlist(ctx, account.ID, account.ModelCatalogBaseline(), allowlist))
			reload()
			members := account.EffectiveUpstreamModelCatalog().EligibleModels
			if disable {
				require.Len(t, members, 2, "disabled restrictions preserve discovered provider members")
			} else {
				require.Len(t, members, 1)
				require.Equal(t, "first", members[0].ID)
			}
			snapshot.Revision = account.ModelCatalogBaseline().SnapshotRevision + 1
			require.NoError(t, repo.CommitAccountModelCatalog(ctx, account.ID, account.ModelCatalogBaseline(), snapshot, account.ModelCatalogState(), nil))
			reload()
			require.Len(t, account.EffectiveUpstreamModelCatalog().EligibleModels, 2)
			require.NoError(t, repo.UpdateCredentials(ctx, account.ID, map[string]any{"api_key": "changed"}))
			reload()
			require.Nil(t, account.EffectiveUpstreamModelCatalog())
		}
	})

	t.Run("parent_reauthorization_rejects_shadow_commit", func(t *testing.T) {
		parent := create(t)
		child := create(t)
		baseline := child.ModelCatalogBaseline()
		baseline.CredentialAccountID = parent.ID
		baseline.CredentialIdentityRevision = parent.ModelCatalogState().IdentityRevision
		snapshot := &service.AccountModelCatalogSnapshot{SchemaVersion: service.AccountModelCatalogSchemaVersion, EnrichmentVersion: "capability-v2", Complete: true, Revision: 1, CredentialAccountID: parent.ID, CredentialIdentityRevision: baseline.CredentialIdentityRevision}
		parentTx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		_, err = parentTx.ExecContext(ctx, `UPDATE accounts SET extra=COALESCE(extra,'{}'::jsonb) || '{"upstream_model_catalog_state":{"identity_revision":1,"rules_revision":1}}'::jsonb WHERE id=$1`, parent.ID)
		require.NoError(t, err)
		result := make(chan error, 1)
		go func() {
			result <- repo.CommitAccountModelCatalog(ctx, child.ID, baseline, snapshot, child.ModelCatalogState(), &service.UpstreamModelMetadataSnapshot{})
		}()
		require.NoError(t, parentTx.Commit())
		require.ErrorIs(t, <-result, service.ErrAccountModelCatalogConflict)
		current, err := repo.GetByID(ctx, child.ID)
		require.NoError(t, err)
		require.Nil(t, current.GetUpstreamModelCatalog())
	})
	t.Run("catalog_lock_pool_and_cancellation", func(t *testing.T) {
		db.SetMaxOpenConns(2)
		defer db.SetMaxOpenConns(0)
		account := create(t)
		lockCtx, cancel := context.WithCancel(ctx)
		release, err := repo.TryLockAccountModelCatalog(lockCtx, account.ID)
		require.NoError(t, err)
		require.Equal(t, 1, db.Stats().InUse)
		commitCtx, stop := context.WithTimeout(ctx, 2*time.Second)
		defer stop()
		require.NoError(t, repo.CommitAccountModelCatalog(commitCtx, account.ID, account.ModelCatalogBaseline(), nil, account.ModelCatalogState(), nil))
		require.Equal(t, 1, db.Stats().InUse, "CAS released its second connection while discovery lock remains held")
		cancel()
		release()
		require.Eventually(t, func() bool { return db.Stats().InUse == 0 }, time.Second, 10*time.Millisecond)
		again, err := repo.TryLockAccountModelCatalog(ctx, account.ID)
		require.NoError(t, err)
		again()
	})
	t.Run("allowlist_concurrent_admins_have_one_winner", func(t *testing.T) {
		account := create(t)
		baseline := account.ModelCatalogBaseline()
		scope := service.AccountModelAllowlist{Enabled: true, Models: []string{"native"}, ConfirmedBy: 7, Reason: "verified"}
		result := make(chan error, 2)
		for range 2 {
			go func() { result <- repo.UpdateAccountModelAllowlist(ctx, account.ID, baseline, scope) }()
		}
		first, second := <-result, <-result
		if first == nil {
			require.ErrorIs(t, second, service.ErrAccountModelCatalogConflict)
		} else {
			require.ErrorIs(t, first, service.ErrAccountModelCatalogConflict)
			require.NoError(t, second)
		}
		current, err := repo.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.Equal(t, baseline.RulesRevision+1, current.ModelCatalogBaseline().RulesRevision)
	})
	t.Run("manual_bulk_token_rotation_invalidates_stable_subject", func(t *testing.T) {
		account := create(t)
		credentials := map[string]any{"access_token": "before", "sub": "same-subject"}
		require.NoError(t, repo.UpdateCredentials(ctx, account.ID, credentials))
		current, err := repo.GetByID(ctx, account.ID)
		require.NoError(t, err)
		baseline := current.ModelCatalogBaseline()
		_, err = repo.BulkUpdate(ctx, []int64{account.ID}, service.AccountBulkUpdate{Credentials: map[string]any{"access_token": "after"}})
		require.NoError(t, err)
		current, err = repo.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.Equal(t, baseline.IdentityRevision+1, current.ModelCatalogBaseline().IdentityRevision)
	})

	t.Run("failed_outbox_rolls_back_before_cache_refresh", func(t *testing.T) {
		edit := create(t)
		_, err := db.Exec(`CREATE FUNCTION reject_account_config_outbox() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test outbox failure'; END $$; CREATE TRIGGER reject_account_config_outbox BEFORE INSERT ON scheduler_outbox FOR EACH ROW EXECUTE FUNCTION reject_account_config_outbox()`)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, err := db.Exec(`DROP TRIGGER reject_account_config_outbox ON scheduler_outbox; DROP FUNCTION reject_account_config_outbox()`)
			require.NoError(t, err)
		})
		calls := cache.calls
		require.ErrorContains(t, repo.UpdateAccountModelAllowlist(ctx, edit.ID, edit.ModelCatalogBaseline(), service.AccountModelAllowlist{Reason: "fixture"}), "test outbox failure")
		require.ErrorContains(t, commit(edit), "test outbox failure")
		require.Equal(t, calls, cache.calls)
		got, err := client.Account.Get(ctx, edit.ID)
		require.NoError(t, err)
		require.NotContains(t, got.Extra, service.UpstreamModelCatalogExtraKey)
		require.NotContains(t, got.Extra, service.UpstreamModelCatalogStateExtraKey)
		require.NotContains(t, got.Extra, service.UpstreamModelAllowlistExtraKey)
	})
	require.Positive(t, cache.calls, "successful commits must refresh the scheduler cache")
}
