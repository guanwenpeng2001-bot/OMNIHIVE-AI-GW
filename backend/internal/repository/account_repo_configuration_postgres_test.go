//go:build unit

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
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
	repo.credentialEncryptor = persistentTestAES(t, true)
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
		return createEncryptedCatalogFixture(t, ctx, client, db, repo, &service.Account{Name: "before", Platform: service.PlatformQianwen, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "old", "model_mapping": map[string]any{"alias": "target"}}, Extra: map[string]any{"quota_used": 10, "quota_daily_used": 10, "quota_daily_limit": 100, "quota_daily_reset_at": "2030-01-01T00:00:00Z"}})
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
		return repo.CommitAccountModelCatalog(ctx, account.ID, account.ModelCatalogBaseline(), &service.AccountModelCatalogSnapshot{Revision: account.ModelCatalogBaseline().SnapshotRevision + 1, IdentityRevision: account.ModelCatalogBaseline().IdentityRevision, Complete: true, DiscoveredModels: []string{}, EligibleModels: []service.AccountCatalogModel{}, ExcludedModels: []service.ExcludedCatalogModel{}}, account.ModelCatalogState(), nil, false)
	}
	t.Run("same_baseline_first_commit_wins_and_preserves_aliases", func(t *testing.T) {
		account := create(t)
		done := make(chan error, 2)
		go func() { done <- commit(account) }()
		go func() { done <- commit(account) }()
		first, second := <-done, <-done
		if first == nil {
			require.Error(t, second)
		} else {
			require.Error(t, first)
			require.NoError(t, second)
		}
		got, err := repo.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.Equal(t, service.CredentialVersion(account.Credentials), service.CredentialVersion(got.Credentials))
		require.Equal(t, account.GetCredential("api_key"), got.GetCredential("api_key"))
		require.Equal(t, account.GetModelMapping(), got.GetModelMapping())
		require.NotNil(t, got.GetUpstreamModelCatalog())
		require.Empty(t, got.GetUpstreamModelCatalog().EligibleModels)
		stale := got.ModelCatalogBaseline()
		require.NoError(t, repo.UpdateCredentials(ctx, got.ID, map[string]any{"api_key": "rotated", "model_mapping": got.Credentials["model_mapping"], service.CredentialSecretVersionKey: service.CredentialVersion(got.Credentials)}))
		require.ErrorIs(t, repo.CommitAccountModelCatalog(ctx, got.ID, stale, nil, got.ModelCatalogState(), nil, false), service.ErrAccountModelCatalogConflict)
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
							if field == "model_mapping" {
								account.Credentials[field] = map[string]any{"changed": "changed-target"}
							} else {
								account.Credentials[field] = map[string]any{"changed": true}
							}
							if field == "model_mapping" {
								account.Credentials[field] = map[string]string{"changed-alias": "changed-target"}
							}
						} else {
							account.Extra[field] = true
						}
						switch method {
						case "ordinary":
							if field == "model_mapping" {
								command, err := service.ModelSelectionCommandFromCredentials(account.Credentials, 7)
								require.NoError(t, err)
								account.ModelSelectionCommand = command
							}
							require.NoError(t, repo.UpdateWithAccountBillingSettings(ctx, account, nil, nil, nil))
						case "patch":
							if location == "credentials" {
								require.NoError(t, repo.UpdateCredentials(ctx, account.ID, account.Credentials))
								if field == "model_mapping" {
									current, err := repo.GetByID(ctx, account.ID)
									require.NoError(t, err)
									require.Equal(t, baseline.RulesRevision, current.ModelCatalogBaseline().RulesRevision)
									require.Equal(t, "target", current.GetMappedModel("alias"))
									command, err := service.ModelSelectionCommandFromCredentials(account.Credentials, 7)
									require.NoError(t, err)
									_, err = service.NewAccountModelSelectionService(repo).SaveModelSelection(ctx, account.ID, *command)
									require.NoError(t, err)
								}
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
						require.ErrorIs(t, repo.CommitAccountModelCatalog(ctx, account.ID, baseline, nil, account.ModelCatalogState(), nil, false), service.ErrAccountModelCatalogConflict)
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
			row := createEncryptedCatalogFixture(t, ctx, client, db, repo, &service.Account{Name: "capability-fixture", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Schedulable: true, Credentials: map[string]any{"api_key": "must-not-project", "access_token": "must-not-project", "protocol_rules": []any{}, "api_base_urls": map[string]any{}, "openai_capabilities": map[string]any{}, "compact_model_mapping": map[string]any{}}, Extra: map[string]any{service.UpstreamModelCatalogExtraKey: service.AccountModelCatalogSnapshot{Complete: true, Revision: 1, DiscoveryScope: "account", DiscoveredModels: []string{"private-model"}, EligibleModels: []service.AccountCatalogModel{{ID: "private-model", Endpoints: []string{"responses"}, EvidenceKind: "native_account_catalog"}}}, service.UpstreamModelMetadataExtraKey: metadata}})
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
	t.Run("allowlist_expansion_applies_immediately", func(t *testing.T) {
		account := create(t)
		snapshot := &service.AccountModelCatalogSnapshot{Revision: 1, Complete: true, DiscoveredModels: []string{"first", "second"}, EligibleModels: []service.AccountCatalogModel{
			{ID: "first", Endpoints: []string{"chat_completions"}, EvidenceKind: "native_account_catalog"},
			{ID: "second", Endpoints: []string{"chat_completions"}, EvidenceKind: "native_account_catalog"},
		}}
		require.NoError(t, repo.CommitAccountModelCatalog(ctx, account.ID, account.ModelCatalogBaseline(), snapshot, account.ModelCatalogState(), nil, false))
		reload := func() { var err error; account, err = repo.GetByID(ctx, account.ID); require.NoError(t, err) }
		reload()
		mapping := map[string]string{"first": "first", "second": "second"}
		_, err := repo.ReplaceAccountModelSelectionScope(ctx, account.ID, account.ModelCatalogBaseline(), service.AccountModelAllowlist{Enabled: true, Models: []string{"first"}}, &mapping)
		require.NoError(t, err)
		reload()
		require.Len(t, account.EffectiveUpstreamModelCatalog().EligibleModels, 1)
		require.Equal(t, "first", account.EffectiveUpstreamModelCatalog().EligibleModels[0].ID)
		require.Empty(t, service.AccountModelSelectionView(account).ModelMapping)
		require.Len(t, account.GetUpstreamModelCatalog().EligibleModels, 2)
		require.NoError(t, repo.UpdateAccountModelAllowlist(ctx, account.ID, account.ModelCatalogBaseline(), service.AccountModelAllowlist{Enabled: true, Models: []string{"first", "second"}}))
		reload()
		require.Len(t, account.EffectiveUpstreamModelCatalog().EligibleModels, 2)
		require.NoError(t, repo.UpdateAccountModelAllowlist(ctx, account.ID, account.ModelCatalogBaseline(), service.AccountModelAllowlist{Enabled: true, Models: []string{}, ConfirmedBy: 7, Reason: "maintenance"}))
		reload()
		require.Empty(t, account.EffectiveUpstreamModelCatalog().EligibleModels)
		require.NoError(t, repo.UpdateAccountModelAllowlist(ctx, account.ID, account.ModelCatalogBaseline(), service.AccountModelAllowlist{Enabled: false}))
		reload()
		require.Len(t, account.EffectiveUpstreamModelCatalog().EligibleModels, 2, "an explicit inherit command restores routing")
		_, err = repo.ReplaceAccountModelSelectionScope(ctx, account.ID, account.ModelCatalogBaseline(), service.AccountModelAllowlist{Enabled: false}, &mapping)
		require.NoError(t, err)
		reload()
		require.Len(t, account.EffectiveUpstreamModelCatalog().EligibleModels, 2)
	})

	t.Run("discovery_checks_CAS_without_changing_selection", func(t *testing.T) {
		for _, oldSnapshot := range []bool{false, true} {
			extra := map[string]any{}
			if oldSnapshot {
				extra[service.UpstreamModelCatalogExtraKey] = &service.AccountModelCatalogSnapshot{Revision: 1, Complete: true, DiscoveredModels: []string{"old"}, EligibleModels: []service.AccountCatalogModel{{ID: "old", Endpoints: []string{"chat_completions"}, EvidenceKind: "native_account_catalog"}}}
			}
			account := createEncryptedCatalogFixture(t, ctx, client, db, repo, &service.Account{Name: "legacy-selection", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "fixture"}, Extra: extra})
			baseline := account.ModelCatalogBaseline()
			incoming := &service.AccountModelCatalogSnapshot{Revision: baseline.SnapshotRevision + 1, Complete: true, DiscoveredModels: []string{"new"}, EligibleModels: []service.AccountCatalogModel{{ID: "new", Endpoints: []string{"chat_completions"}, EvidenceKind: "native_account_catalog"}}}
			// A stale baseline cannot publish discovery or leave a selection-only partial update.
			stale := baseline
			stale.RulesRevision++
			state := account.ModelCatalogState()
			state.RulesRevision++
			require.ErrorIs(t, repo.CommitAccountModelCatalog(ctx, account.ID, stale, incoming, state, nil, true), service.ErrAccountModelCatalogConflict)
			unchanged, err := repo.GetByID(ctx, account.ID)
			require.NoError(t, err)
			require.Equal(t, service.ModelSelectionInherit, service.AccountModelSelectionView(unchanged).Mode)
			require.Equal(t, baseline, unchanged.ModelCatalogBaseline())
			require.NoError(t, repo.CommitAccountModelCatalog(ctx, account.ID, baseline, incoming, account.ModelCatalogState(), nil, true))
			current, err := repo.GetByID(ctx, account.ID)
			require.NoError(t, err)
			require.Equal(t, service.ModelSelectionInherit, service.AccountModelSelectionView(current).Mode)
			require.Equal(t, baseline.RulesRevision, current.ModelCatalogState().RulesRevision)
			binding := service.ResolveAccountModelBinding(ctx, current, "new", service.AccountModelBindingOptions{Endpoint: "chat_completions"})
			require.True(t, binding.Supported, "discovery does not create or overwrite configuration")
			require.Empty(t, current.GetModelMapping())
			require.ErrorIs(t, repo.CommitAccountModelCatalog(ctx, account.ID, baseline, incoming, account.ModelCatalogState(), nil, true), service.ErrAccountModelCatalogConflict)
		}
	})

	t.Run("credential_refresh_preserves_selection", func(t *testing.T) {
		account := create(t)
		require.NoError(t, repo.UpdateCredentials(ctx, account.ID, map[string]any{"api_key": "old", "model_mapping": map[string]string{}, service.CredentialSecretVersionKey: service.CredentialVersion(account.Credentials)}))
		account, err = repo.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.Equal(t, map[string]string{"alias": "target"}, account.GetModelMapping())
		require.NoError(t, repo.UpdateCredentials(ctx, account.ID, map[string]any{"api_key": "new", "model_mapping": map[string]string{"draft": "draft"}, service.CredentialSecretVersionKey: service.CredentialVersion(account.Credentials)}))
		account, err = repo.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.Equal(t, map[string]string{"alias": "target"}, account.GetModelMapping())
		require.Len(t, account.EffectiveUpstreamModelCatalog().EligibleModels, 1)
		mapping := map[string]string{"draft": "draft"}
		_, err = repo.ReplaceAccountModelSelectionScope(ctx, account.ID, account.ModelCatalogBaseline(), service.AccountModelAllowlist{Enabled: false}, &mapping)
		require.NoError(t, err)
		account, err = repo.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.Len(t, account.EffectiveUpstreamModelCatalog().EligibleModels, 1)
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
			result <- repo.CommitAccountModelCatalog(ctx, child.ID, baseline, snapshot, child.ModelCatalogState(), &service.UpstreamModelMetadataSnapshot{}, false)
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
		require.NoError(t, repo.CommitAccountModelCatalog(commitCtx, account.ID, account.ModelCatalogBaseline(), nil, account.ModelCatalogState(), nil, false))
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
		credentials := map[string]any{"access_token": "before", "sub": "same-subject", service.CredentialSecretVersionKey: service.CredentialVersion(account.Credentials)}
		require.NoError(t, repo.UpdateCredentials(ctx, account.ID, credentials))
		current, err := repo.GetByID(ctx, account.ID)
		require.NoError(t, err)
		baseline := current.ModelCatalogBaseline()
		_, err = repo.BulkUpdate(ctx, []int64{account.ID}, service.AccountBulkUpdate{Credentials: map[string]any{"access_token": "after"}})
		require.NoError(t, err)
		current, err = repo.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.Greater(t, current.ModelCatalogBaseline().IdentityRevision, baseline.IdentityRevision)
	})

	t.Run("bulk_selection_with_other_rules_advances_once", func(t *testing.T) {
		for _, field := range []string{"compact_model_mapping", "api_key", "compact_mapping_and_key"} {
			t.Run(field, func(t *testing.T) {
				account := create(t)
				baseline := account.ModelCatalogState().RulesRevision
				mapping := map[string]string{"alias": "new-target"}
				credentials := map[string]any{"model_mapping": mapping}
				if field != "compact_model_mapping" {
					credentials["api_key"] = "rotated-bulk-key"
				}
				if field != "api_key" {
					credentials["compact_model_mapping"] = map[string]any{"compact": "other-target"}
				}
				results := make(map[int64]service.ModelSelectionResult)
				_, err := repo.BulkUpdate(ctx, []int64{account.ID}, service.AccountBulkUpdate{
					Credentials: credentials, ModelSelectionResults: results,
				})
				require.NoError(t, err)
				current, err := repo.GetByID(ctx, account.ID)
				require.NoError(t, err)
				require.Equal(t, baseline+1, current.ModelCatalogState().RulesRevision)
				require.Equal(t, []string{"new-target"}, service.AccountModelSelectionView(current).Models)
				require.Equal(t, mapping, current.GetModelMapping())
				require.Equal(t, baseline+1, results[account.ID].RulesRevision)
				require.Equal(t, []string{"new-target"}, results[account.ID].AutoAdded)
			})
		}
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
		replacement := map[string]string{"replacement": "replacement"}
		_, selectionErr := repo.ReplaceAccountModelSelectionScope(ctx, edit.ID, edit.ModelCatalogBaseline(), service.AccountModelAllowlist{Enabled: false}, &replacement)
		require.ErrorContains(t, selectionErr, "test outbox failure")
		require.Equal(t, calls, cache.calls)
		got, err := client.Account.Get(ctx, edit.ID)
		require.NoError(t, err)
		require.NotContains(t, got.Extra, service.UpstreamModelCatalogExtraKey)
		require.NotContains(t, got.Extra, service.UpstreamModelCatalogStateExtraKey)
		require.NotContains(t, got.Extra, service.UpstreamModelAllowlistExtraKey)
		require.Equal(t, service.CredentialVersion(edit.Credentials), service.CredentialVersion(got.Credentials))
		require.Equal(t, map[string]any{"alias": "target"}, got.Credentials["model_mapping"])
	})
	require.Positive(t, cache.calls, "successful commits must refresh the scheduler cache")
}
