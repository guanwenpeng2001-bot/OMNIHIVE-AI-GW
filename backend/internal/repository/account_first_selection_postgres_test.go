//go:build unit

package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

// Repository-owned commits stay inside one test-owned outer transaction. SQL
// executes unchanged; cleanup rolls back fixtures and scheduler outbox writes.
type selectionRollbackDriver struct{ *entsql.Driver }
type selectionRollbackTransaction struct{ *entsql.Driver }

func (driver selectionRollbackDriver) Tx(context.Context) (dialect.Tx, error) {
	return selectionRollbackTransaction{driver.Driver}, nil
}
func (selectionRollbackTransaction) Commit() error   { return nil }
func (selectionRollbackTransaction) Rollback() error { return nil }

func firstSelectionRepository(t *testing.T) (*accountRepository, *dbent.Client) {
	t.Helper()
	database, err := testresources.OpenPostgres(context.Background(), dedicatedJobDSN(t), "job_test")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	transaction, err := database.BeginTx(context.Background(), &sql.TxOptions{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, transaction.Rollback()) })
	driver := selectionRollbackDriver{entsql.NewDriver(dialect.Postgres, entsql.Conn{ExecQuerier: transaction})}
	client := dbent.NewClient(dbent.Driver(driver))
	repo := newAccountRepositoryWithSQL(client, transaction, nil)
	repo.credentialEncryptor = persistentTestAES(t, true)
	return repo, client
}

func TestAccountFirstSelectionDedicatedPostgres(t *testing.T) {
	ctx := context.Background()
	for _, scenario := range []string{"cn_background_probe", "cn_native_responses", "support_observation", "discovery_refresh"} {
		t.Run(scenario, func(t *testing.T) {
			repository, _ := firstSelectionRepository(t)
			account := &service.Account{Name: "first-selection-fixture", Platform: service.PlatformQianwen, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}, Status: service.StatusActive, Schedulable: true, Concurrency: 1}
			if scenario == "cn_native_responses" {
				account.Platform = service.PlatformKimi
				account.Credentials["api_protocol"] = service.APIProtocolAdaptive
			}
			if scenario == "support_observation" {
				account.Platform = service.PlatformOpenAI
			}
			require.NoError(t, repository.Create(ctx, account))
			preview, err := repository.GetByID(ctx, account.ID)
			require.NoError(t, err)
			require.False(t, service.AccountModelSelectionNeedsFreeze(preview), "modern empty selection is already frozen")
			baseline := preview.ModelCatalogBaseline()
			require.Zero(t, baseline.RulesRevision)
			if scenario == "cn_background_probe" || scenario == "cn_native_responses" {
				// Exact post-create task used by the handler; CN classification makes
				// no provider request and a nil upstream would panic if it attempted one.
				probe := service.NewAccountTestService(repository, nil, nil, nil, nil, nil, nil, nil)
				probe.ProbeOpenAIAPIKeyResponsesSupport(ctx, account.ID)
			} else if scenario == "support_observation" {
				require.NoError(t, repository.UpdateExtra(ctx, account.ID, map[string]any{"openai_responses_supported": false}))
			} else {
				snapshot := &service.AccountModelCatalogSnapshot{Revision: 1, Complete: true, DiscoveredModels: []string{"discovered-other"}}
				require.NoError(t, repository.CommitAccountModelCatalog(ctx, account.ID, baseline, snapshot, preview.ModelCatalogState(), nil, false))
			}
			current, err := repository.GetByID(ctx, account.ID)
			require.NoError(t, err)
			t.Logf("preview rules=%d, after background rules=%d, snapshot=%d", baseline.RulesRevision, current.ModelCatalogBaseline().RulesRevision, current.ModelCatalogBaseline().SnapshotRevision)
			mapping := map[string]string{"public-name": "manual-undiscovered"}
			selectionService := service.NewAccountTestService(repository, nil, nil, nil, nil, nil, nil, nil)
			warnings, err := selectionService.ConfigureAccountModelAllowlist(ctx, preview, false, []string{"manual-undiscovered"}, "", 1, baseline.RulesRevision, &mapping)
			require.NoError(t, err, "first explicit save must not conflict with background observations")
			require.Equal(t, []string{"manual-undiscovered"}, warnings)
			current, err = repository.GetByID(ctx, account.ID)
			require.NoError(t, err)
			require.Equal(t, baseline.RulesRevision+1, current.ModelCatalogState().RulesRevision)
			require.Equal(t, mapping, current.GetModelMapping())
			_, err = repository.ReplaceAccountModelSelectionScope(ctx, account.ID, baseline, service.AccountModelAllowlist{Enabled: false}, &mapping)
			require.ErrorIs(t, err, service.ErrAccountModelCatalogConflict, "a competing explicit save must still conflict")
		})
	}
}

func TestAccountSelectionConflictsDedicatedPostgres(t *testing.T) {
	ctx := context.Background()
	for _, change := range []string{"identity", "mapping", "protocol_override", "deleted"} {
		t.Run(change, func(t *testing.T) {
			repository, client := firstSelectionRepository(t)
			account := &service.Account{Name: "selection-conflict-fixture", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}, Status: service.StatusActive, Concurrency: 1}
			require.NoError(t, repository.Create(ctx, account))
			account, err := repository.GetByID(ctx, account.ID)
			require.NoError(t, err)
			baseline := account.ModelCatalogBaseline()
			switch change {
			case "identity":
				require.NoError(t, repository.UpdateCredentialsCAS(ctx, account, map[string]any{"api_key": "rotated-fixture"}))
			case "mapping":
				winner := map[string]string{"winner": "winner"}
				_, err := repository.ReplaceAccountModelSelectionScope(ctx, account.ID, baseline, service.AccountModelAllowlist{Enabled: false}, &winner)
				require.NoError(t, err)
			case "protocol_override":
				require.NoError(t, repository.UpdateExtra(ctx, account.ID, map[string]any{"openai_responses_mode": "force_chat_completions"}))
			case "deleted":
				_, err := client.ExecContext(ctx, "UPDATE accounts SET deleted_at=NOW() WHERE id=$1", account.ID)
				require.NoError(t, err)
			}
			loser := map[string]string{"loser": "loser"}
			_, err = repository.ReplaceAccountModelSelectionScope(ctx, account.ID, baseline, service.AccountModelAllowlist{Enabled: false}, &loser)
			require.ErrorIs(t, err, service.ErrAccountModelCatalogConflict)
		})
	}
}

func TestAccountLegacyFreezeAndRefreshDedicatedPostgres(t *testing.T) {
	ctx := context.Background()
	repository, client := firstSelectionRepository(t)
	legacy := &service.Account{Name: "legacy-selection-fixture", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Credentials: map[string]any{"api_key": "fixture"}}
	require.NoError(t, repository.Create(ctx, legacy))
	// Restore the pre-selection catalog shape while retaining the encrypted credential.
	payload, err := json.Marshal(map[string]any{service.UpstreamModelCatalogExtraKey: service.AccountModelCatalogSnapshot{Revision: 1, Complete: true, EligibleModels: []service.AccountCatalogModel{{ID: "legacy-selected", Endpoints: []string{"responses"}}}}})
	require.NoError(t, err)
	_, err = client.ExecContext(ctx, `UPDATE accounts SET extra=$2::jsonb WHERE id=$1`, legacy.ID, payload)
	require.NoError(t, err)
	legacy, err = repository.GetByID(ctx, legacy.ID)
	require.NoError(t, err)
	require.True(t, service.AccountModelSelectionNeedsFreeze(legacy))
	staleBaseline := legacy.ModelCatalogBaseline()
	snapshot := &service.AccountModelCatalogSnapshot{Revision: 2, Complete: true, DiscoveredModels: []string{"new-observation"}}
	require.NoError(t, repository.CommitAccountModelCatalog(ctx, legacy.ID, staleBaseline, snapshot, legacy.ModelCatalogState(), nil, true))
	current, err := repository.GetByID(ctx, legacy.ID)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"legacy-selected": "legacy-selected"}, current.GetModelMapping())
	require.Equal(t, staleBaseline.RulesRevision+1, current.ModelCatalogState().RulesRevision)
	manual := map[string]string{"manual": "manual"}
	_, err = repository.ReplaceAccountModelSelectionScope(ctx, legacy.ID, staleBaseline, service.AccountModelAllowlist{Enabled: false}, &manual)
	require.ErrorIs(t, err, service.ErrAccountModelCatalogConflict, "legacy migration changes saved selection, unlike discovery")
	baseline := current.ModelCatalogBaseline()
	snapshot.Revision++
	snapshot.RulesRevision = baseline.RulesRevision
	require.NoError(t, repository.CommitAccountModelCatalog(ctx, legacy.ID, baseline, snapshot, current.ModelCatalogState(), nil, false))
	current, err = repository.GetByID(ctx, legacy.ID)
	require.NoError(t, err)
	require.Equal(t, baseline.RulesRevision, current.ModelCatalogState().RulesRevision)
	require.Equal(t, map[string]string{"legacy-selected": "legacy-selected"}, current.GetModelMapping())
}
