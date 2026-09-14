//go:build unit

package repository

import (
	"context"
	"database/sql"
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
	t.Run("discovery_and_key_rotation_merge_only_changed_fields", func(t *testing.T) {
		edit := create(t)
		before := edit.Credentials
		edit.Credentials = map[string]any{"api_key": "rotated", "model_mapping": before["model_mapping"]}
		deltaCtx := service.WithAccountCredentialChanges(ctx, before, edit.Credentials)
		require.NoError(t, repo.MergeDiscoveredModelMapping(ctx, edit.ID, []string{"alias", "discovered"}))
		require.NoError(t, repo.UpdateWithAccountBillingSettings(deltaCtx, edit, nil, nil, nil))
		got, err := client.Account.Get(ctx, edit.ID)
		require.NoError(t, err)
		require.Equal(t, "rotated", got.Credentials["api_key"])
		require.Equal(t, map[string]any{"alias": "target", "discovered": "discovered"}, got.Credentials["model_mapping"])
		// Discovery waits for a credential rotation transaction, then merges its current row.
		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, `UPDATE accounts SET credentials=jsonb_set(jsonb_set(credentials,'{api_key}','"newest"'),'{model_mapping,alias}','"new-target"') WHERE id=$1`, edit.ID)
		require.NoError(t, err)
		done := make(chan error, 1)
		go func() { done <- repo.MergeDiscoveredModelMapping(ctx, edit.ID, []string{"another"}) }()
		require.NoError(t, tx.Commit())
		require.NoError(t, <-done)
		got, err = client.Account.Get(ctx, edit.ID)
		require.NoError(t, err)
		require.Equal(t, "newest", got.Credentials["api_key"])
		require.Contains(t, got.Credentials["model_mapping"], "another")
		require.Equal(t, "new-target", got.Credentials["model_mapping"].(map[string]any)["alias"])
	})
	t.Run("explicit_null_mapping_is_treated_as_empty", func(t *testing.T) {
		edit := create(t)
		_, err := client.Account.UpdateOneID(edit.ID).SetCredentials(map[string]any{"api_key": "keep", "model_mapping": nil}).Save(ctx)
		require.NoError(t, err)
		require.NoError(t, repo.MergeDiscoveredModelMapping(ctx, edit.ID, []string{"model"}))
		got, err := client.Account.Get(ctx, edit.ID)
		require.NoError(t, err)
		require.Equal(t, map[string]any{"api_key": "keep", "model_mapping": map[string]any{"model": "model"}}, got.Credentials)
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
		require.ErrorContains(t, repo.MergeDiscoveredModelMapping(ctx, edit.ID, []string{"must-rollback"}), "test outbox failure")
		require.Equal(t, calls, cache.calls)
		got, err := client.Account.Get(ctx, edit.ID)
		require.NoError(t, err)
		require.NotContains(t, got.Credentials["model_mapping"], "must-rollback")
	})
	require.Positive(t, cache.calls, "successful commits must refresh the scheduler cache")
}
