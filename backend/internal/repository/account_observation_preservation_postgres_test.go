package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

// Exercise real JSONB and row-lock semantics with a stale quota observer after
// an independent catalog commit and operator pause. No upstream calls are made.
func TestRuntimeObservationPreservesConcurrentCatalogAndOperatorStatePostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to disposable job_test")
	}
	database := testresources.OpenSchema(t, dsn)
	ctx := context.Background()
	_, err := database.ExecContext(ctx, `CREATE TABLE accounts (LIKE public.accounts INCLUDING ALL);
CREATE TABLE scheduler_outbox (LIKE public.scheduler_outbox INCLUDING ALL);`)
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, database)))
	repository := newAccountRepositoryWithSQL(client, database, nil)

	for _, originallySchedulable := range []bool{false, true} {
		name := "already_paused"
		if originallySchedulable {
			name = "paused_during_observation"
		}
		t.Run(name, func(t *testing.T) {
			entity, err := client.Account.Create().SetName("runtime-observation-fixture").
				SetPlatform(service.PlatformCommandCode).SetType(service.AccountTypeAPIKey).
				SetStatus(service.StatusActive).SetSchedulable(originallySchedulable).
				SetCredentials(map[string]any{"api_key": "synthetic-key", "model_mapping": map[string]any{"alias": "chosen-model"}}).
				SetExtra(map[string]any{"commandcode_monthlyCredits": 1, "independent_observation": "old"}).
				Save(domain.WithWorkspaceScope(ctx, 17))
			require.NoError(t, err)
			stale := accountEntityToService(entity)
			attempted := time.Now().UTC().Format(time.RFC3339Nano)
			state := service.AccountModelCatalogState{AttemptedAt: attempted, LastSuccessAt: attempted, Result: "succeeded"}
			snapshot := &service.AccountModelCatalogSnapshot{Revision: 1, Complete: true, ObservedAt: attempted,
				DiscoveredModels: []string{"chosen-model", "new-candidate"}, EligibleModels: []service.AccountCatalogModel{{ID: "chosen-model"}, {ID: "new-candidate"}}}
			require.NoError(t, repository.CommitAccountModelCatalog(ctx, stale.ID, stale.ModelCatalogBaseline(), snapshot, state, nil, false))
			require.NoError(t, repository.UpdateExtra(ctx, stale.ID, map[string]any{"independent_observation": "fresh"}))
			require.NoError(t, repository.SetSchedulable(ctx, stale.ID, false))
			before, err := client.Account.Get(ctx, stale.ID)
			require.NoError(t, err)
			end := time.Now().Add(time.Hour)
			require.NoError(t, repository.UpdateAccountRuntimeSnapshot(ctx, stale, map[string]any{"commandcode_monthlyCredits": 2}, &end))
			after, err := client.Account.Get(ctx, stale.ID)
			require.NoError(t, err)
			require.False(t, after.Schedulable)
			require.Equal(t, before.Credentials, after.Credentials, "candidate discovery and quota refresh cannot change selections")
			require.Equal(t, before.Extra[service.UpstreamModelCatalogExtraKey], after.Extra[service.UpstreamModelCatalogExtraKey])
			require.Equal(t, before.Extra[service.UpstreamModelCatalogStateExtraKey], after.Extra[service.UpstreamModelCatalogStateExtraKey])
			require.Equal(t, "fresh", after.Extra["independent_observation"])
			require.Equal(t, float64(2), after.Extra["commandcode_monthlyCredits"])
			require.WithinDuration(t, end, *after.SessionWindowEnd, time.Microsecond)

			// Replacement credentials still revoke the old observation's ownership.
			_, err = client.Account.UpdateOneID(stale.ID).SetCredentials(map[string]any{"api_key": "synthetic-replacement"}).Save(ctx)
			require.NoError(t, err)
			require.ErrorIs(t, repository.UpdateAccountRuntimeSnapshot(ctx, stale, map[string]any{"commandcode_monthlyCredits": 99}, nil), service.ErrAccountRuntimeIdentityChanged)
		})
	}
}
