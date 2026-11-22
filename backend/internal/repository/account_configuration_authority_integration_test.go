//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestManagedConnectionConfigurationPostgres(t *testing.T) {
	ctx := domain.WithWorkspaceScope(context.Background(), 1)
	client := testEntClient(t)
	repo := newAccountRepositoryWithSQL(client, integrationDB, nil)
	repo.credentialEncryptor = persistentTestAES(t, true)
	create := func(t *testing.T, managed bool) *service.Account {
		t.Helper()
		row, err := client.Account.Create().SetName("authority-" + uuid.NewString()).SetPlatform(service.PlatformOpenAI).
			SetType(service.AccountTypeAPIKey).SetStatus(service.StatusActive).SetSchedulable(true).
			SetCredentials(map[string]any{"base_url": "https://original.example", "model_mapping": map[string]string{"alias": "model"}}).Save(ctx)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, err := integrationDB.Exec("DELETE FROM scheduler_outbox WHERE account_id=$1", row.ID)
			require.NoError(t, err)
			require.NoError(t, client.Account.DeleteOneID(row.ID).Exec(context.Background()))
		})
		if managed {
			_, err = integrationDB.Exec(`INSERT INTO provider_connections(account_id,workspace_id,name,provider,auth_type,concurrency,state,model_ids,verified_id)
VALUES($1,1,$2,'openai','api_key',3,'active',ARRAY['model'],$3)`, row.ID, row.Name, uuid.NewString())
			require.NoError(t, err)
		}
		return accountEntityToService(row)
	}
	assertClean := func(t *testing.T, id int64) {
		t.Helper()
		var dirty, verified bool
		var version int64
		require.NoError(t, integrationDB.QueryRow("SELECT legacy_dirty,verified_id IS NOT NULL,version FROM provider_connections WHERE account_id=$1", id).Scan(&dirty, &verified, &version))
		require.False(t, dirty)
		require.True(t, verified)
		require.EqualValues(t, 1, version)
	}

	t.Run("selection_and_bulk_reject_without_partial_writes", func(t *testing.T) {
		legacy, managed := create(t, false), create(t, true)
		for _, mapping := range []*map[string]string{nil, {"alias": "changed"}} {
			_, err := repo.ReplaceAccountModelSelectionScope(ctx, managed.ID, managed.ModelCatalogBaseline(), service.AccountModelAllowlist{Enabled: true, Models: []string{"changed"}}, mapping)
			require.ErrorIs(t, err, service.ErrAccountConnectionManaged)
		}
		name := "must-not-write"
		for _, patch := range []service.AccountBulkUpdate{
			{Name: &name},
			{Credentials: map[string]any{"api_key": "synthetic-rotation-must-not-write"}},
			{Credentials: map[string]any{"plan_type": "free"}},
			{Extra: map[string]any{"header_overrides": map[string]string{"X-Example": "changed"}}},
		} {
			count, err := repo.BulkUpdate(ctx, []int64{legacy.ID, managed.ID}, patch)
			require.ErrorIs(t, err, service.ErrAccountConnectionManaged)
			require.Zero(t, count)
		}
		for _, original := range []*service.Account{legacy, managed} {
			current, err := client.Account.Get(ctx, original.ID)
			require.NoError(t, err)
			require.Equal(t, original.Name, current.Name)
			require.NotContains(t, current.Credentials, "api_key")
			require.NotContains(t, current.Credentials, "plan_type")
			require.Equal(t, map[string]any{"alias": "model"}, current.Credentials["model_mapping"])
			require.NotContains(t, current.Extra, service.UpstreamModelAllowlistExtraKey)
			var events, secrets int
			require.NoError(t, integrationDB.QueryRow("SELECT count(*) FROM scheduler_outbox WHERE account_id=$1", original.ID).Scan(&events))
			require.NoError(t, integrationDB.QueryRow("SELECT count(*) FROM credential_secrets WHERE account_id=$1", original.ID).Scan(&secrets))
			require.Zero(t, events)
			require.Zero(t, secrets)
		}
		assertClean(t, managed.ID)
	})

	t.Run("discovery_observations_and_group_binding_remain_legal", func(t *testing.T) {
		managed := create(t, true)
		baseline := managed.ModelCatalogBaseline()
		snapshot := &service.AccountModelCatalogSnapshot{Complete: true, Revision: baseline.SnapshotRevision + 1, IdentityRevision: baseline.IdentityRevision, DiscoveredModels: []string{"model"}}
		require.NoError(t, repo.CommitAccountModelCatalog(ctx, managed.ID, baseline, snapshot, managed.ModelCatalogState(), nil, false))
		group, err := client.Group.Create().SetName("authority-routing-" + uuid.NewString()).SetPlatform(service.PlatformOpenAI).Save(ctx)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, client.Group.DeleteOneID(group.ID).Exec(context.Background())) })
		require.NoError(t, repo.BindGroups(ctx, managed.ID, []int64{group.ID}))
		require.NoError(t, repo.BindGroups(ctx, managed.ID, nil))
		assertClean(t, managed.ID)
	})

	t.Run("legacy_and_quarantined_ownership_remain_editable", func(t *testing.T) {
		legacy, quarantined := create(t, false), create(t, true)
		_, err := integrationDB.Exec("UPDATE provider_connections SET legacy_dirty=true,verified_id=NULL WHERE account_id=$1", quarantined.ID)
		require.NoError(t, err)
		name := "legacy-edit"
		count, err := repo.BulkUpdate(ctx, []int64{legacy.ID, quarantined.ID}, service.AccountBulkUpdate{Name: &name})
		require.NoError(t, err)
		require.EqualValues(t, 2, count)
		_, err = repo.ReplaceAccountModelSelectionScope(ctx, legacy.ID, legacy.ModelCatalogBaseline(), service.AccountModelAllowlist{Enabled: true, Models: []string{"model"}}, nil)
		require.NoError(t, err)
		var dirty bool
		require.NoError(t, integrationDB.QueryRow("SELECT legacy_dirty FROM provider_connections WHERE account_id=$1", quarantined.ID).Scan(&dirty))
		require.True(t, dirty, "a legacy edit cannot adopt or repair a quarantined connection")
	})

	t.Run("adoption_wins_before_legacy_configuration_check", func(t *testing.T) {
		legacy := create(t, false)
		adoption, err := integrationDB.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer func() { _ = adoption.Rollback() }()
		var locked int64
		require.NoError(t, adoption.QueryRowContext(ctx, "SELECT id FROM accounts WHERE id=$1 FOR UPDATE", legacy.ID).Scan(&locked))
		_, err = adoption.ExecContext(ctx, `INSERT INTO provider_connections(account_id,workspace_id,name,provider,auth_type,concurrency,state,model_ids,verified_id)
VALUES($1,1,$2,'openai','api_key',3,'active',ARRAY['model'],$3)`, legacy.ID, legacy.Name, uuid.NewString())
		require.NoError(t, err)
		writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		result := make(chan error, 1)
		go func() {
			name := fmt.Sprintf("stale-edit-%d", legacy.ID)
			_, err := repo.BulkUpdate(writeCtx, []int64{legacy.ID}, service.AccountBulkUpdate{Name: &name})
			result <- err
		}()
		select {
		case err := <-result:
			t.Fatalf("configuration did not wait for the adoption row lock: %v", err)
		case <-time.After(75 * time.Millisecond):
		}
		require.NoError(t, adoption.Commit())
		require.ErrorIs(t, <-result, service.ErrAccountConnectionManaged)
		current, err := client.Account.Get(ctx, legacy.ID)
		require.NoError(t, err)
		require.Equal(t, legacy.Name, current.Name)
		assertClean(t, legacy.ID)
	})
}
