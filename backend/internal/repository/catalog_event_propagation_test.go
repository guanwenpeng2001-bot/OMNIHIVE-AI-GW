package repository

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestModelCatalogEventPostgres(t *testing.T) {
	dsn := os.Getenv("CATALOG_EVENT_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("UPSTREAM_JOB_TEST_DSN")
	}
	if dsn == "" {
		t.Skip("set CATALOG_EVENT_TEST_DSN to a disposable PostgreSQL database")
	}
	database, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = database.Close() }()
	ctx := context.Background()
	var name string
	require.NoError(t, database.QueryRow("SELECT current_database()").Scan(&name))
	require.Contains(t, []string{"catalog_event_test", "job_test"}, name)
	require.NoError(t, ApplyMigrations(ctx, database))
	migration, err := os.ReadFile("../../migrations/265_group_generation.sql")
	require.NoError(t, err)
	_, err = database.ExecContext(ctx, string(migration))
	require.NoError(t, err, "migration must be reentrant")
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, database)))
	repository := newAccountRepositoryWithSQL(client, database, nil)
	repository.credentialEncryptor = persistentTestAES(t, true)
	generation := func(scope string) int64 {
		value, err := repository.ModelCatalogGeneration(ctx, scope)
		require.NoError(t, err)
		return value
	}
	create := func(t *testing.T) *service.Account {
		entity := &service.Account{Name: "catalog-cas-fixture", Platform: "openai", Type: "oauth", Status: service.StatusActive, Credentials: map[string]any{"access_token": "old-token", "chatgpt_account_id": "stable-subject"}, Extra: map[string]any{"quota_used": 12, "cursor_usage_snapshot": map[string]any{"old": true}}}
		err := repository.Create(ctx, entity)
		require.NoError(t, err)
		account, err := repository.GetByID(ctx, entity.ID)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = database.Exec("DELETE FROM credential_secrets WHERE account_id=$1", account.ID)
			_, _ = database.Exec("DELETE FROM accounts WHERE id=$1", account.ID)
		})
		return account
	}
	t.Run("rollback_and_committed_notification", func(t *testing.T) {
		account := create(t)
		listener := pq.NewListener(dsn, 10*time.Millisecond, time.Second, nil)
		defer func() { _ = listener.Close() }()
		require.NoError(t, listener.Listen("model_catalog_changed"))
		before := generation("catalog")
		transaction, err := database.BeginTx(ctx, nil)
		require.NoError(t, err)
		_, err = transaction.Exec("UPDATE accounts SET credentials=credentials || jsonb_build_object('base_url','https://replacement.invalid') WHERE id=$1", account.ID)
		require.NoError(t, err)
		require.Equal(t, before, generation("catalog"), "another instance cannot see an uncommitted version")
		require.NoError(t, transaction.Rollback())
		require.Equal(t, before, generation("catalog"))
		select {
		case notification := <-listener.Notify:
			require.Nil(t, notification)
		case <-time.After(30 * time.Millisecond):
		}
		_, err = database.Exec("UPDATE accounts SET status='disabled' WHERE id=$1", account.ID)
		require.NoError(t, err)
		require.Greater(t, generation("catalog"), before)
		select {
		case notification := <-listener.Notify:
			require.NotNil(t, notification)
		case <-time.After(3 * time.Second):
			t.Fatal("committed notification missing")
		}
		var count int
		require.NoError(t, database.QueryRow("SELECT count(*) FROM scheduler_outbox WHERE event_type='model_catalog_changed' AND payload->>'scope'='catalog' AND (payload->>'generation')::bigint=$1", generation("catalog")).Scan(&count))
		require.Equal(t, 1, count)
	})
	t.Run("snapshot_credentials_and_late_observations", func(t *testing.T) {
		account := create(t)
		end := time.Now().UTC().Truncate(time.Second)
		require.NoError(t, repository.UpdateAccountRuntimeSnapshot(ctx, account, map[string]any{"session_window_utilization": 0.5}, &end))
		require.NoError(t, repository.UpdateAccountRuntimeCredentials(ctx, account, map[string]any{"access_token": "new-token", "chatgpt_account_id": "stable-subject"}))
		require.ErrorIs(t, repository.UpdateAccountRuntimeCredentials(ctx, account, map[string]any{"access_token": "late-token"}), service.ErrAccountRuntimeIdentityChanged)
		evidence := service.WithAccountRuntimeIdentity(ctx, account)
		require.ErrorIs(t, repository.UpdateExtra(evidence, account.ID, map[string]any{"session_window_utilization": 99}), service.ErrAccountRuntimeIdentityChanged)
		require.ErrorIs(t, repository.UpdateSessionWindow(evidence, account.ID, nil, &end, "late"), service.ErrAccountRuntimeIdentityChanged)
		require.ErrorIs(t, repository.SetError(evidence, account.ID, "late error"), service.ErrAccountRuntimeIdentityChanged)
		require.ErrorIs(t, repository.SetTempUnschedulable(evidence, account.ID, end.Add(time.Hour), "late failure"), service.ErrAccountRuntimeIdentityChanged)
		current, err := repository.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.Equal(t, "new-token", current.Credentials["access_token"])
		require.Equal(t, float64(12), current.Extra["quota_used"])
		require.Equal(t, float64(0.5), current.Extra["session_window_utilization"])
		require.Equal(t, service.StatusActive, current.Status)
	})

	t.Run("secret_revision_and_concurrent_credentials", func(t *testing.T) {
		account := create(t)
		_, err := database.Exec("UPDATE accounts SET extra=extra || jsonb_build_object('credential_secret_revision',2) WHERE id=$1", account.ID)
		require.NoError(t, err)
		require.ErrorIs(t, repository.UpdateAccountRuntimeSnapshot(ctx, account, map[string]any{"session_window_utilization": 99}, nil), service.ErrAccountRuntimeIdentityChanged)
		account, err = repository.GetByID(ctx, account.ID)
		require.NoError(t, err)
		results := make(chan error, 2)
		for _, token := range []string{"winner-first", "winner-second"} {
			go func(token string) {
				results <- repository.UpdateAccountRuntimeCredentials(ctx, account, map[string]any{"access_token": token})
			}(token)
		}
		first, second := <-results, <-results
		if first == nil {
			require.ErrorIs(t, second, service.ErrAccountRuntimeIdentityChanged)
		} else {
			require.ErrorIs(t, first, service.ErrAccountRuntimeIdentityChanged)
			require.NoError(t, second)
		}
	})
	t.Run("outbox_failure_rolls_back_source_and_generation", func(t *testing.T) {
		account := create(t)
		before := generation("catalog")
		transaction, err := database.BeginTx(ctx, nil)
		require.NoError(t, err)
		_, err = transaction.Exec("ALTER TABLE scheduler_outbox ADD CONSTRAINT catalog_event_reject_test CHECK(event_type <> 'model_catalog_changed') NOT VALID")
		require.NoError(t, err)
		_, err = transaction.Exec("UPDATE accounts SET status='disabled' WHERE id=$1", account.ID)
		require.Error(t, err)
		require.NoError(t, transaction.Rollback())
		require.Equal(t, before, generation("catalog"))
		current, err := repository.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.Equal(t, service.StatusActive, current.Status)
	})
	t.Run("same_proxy_record_change_rejects", func(t *testing.T) {
		account := create(t)
		proxy, err := client.Proxy.Create().SetName("catalog-proxy").SetProtocol("http").SetHost("old.invalid").SetPort(8080).Save(ctx)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = database.Exec("DELETE FROM proxies WHERE id=$1", proxy.ID) })
		require.NoError(t, client.Account.UpdateOneID(account.ID).SetProxyID(proxy.ID).Exec(ctx))
		account, err = repository.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.NoError(t, client.Proxy.UpdateOneID(proxy.ID).SetHost("new.invalid").Exec(ctx))
		require.ErrorIs(t, repository.UpdateAccountRuntimeSnapshot(ctx, account, map[string]any{"session_window_utilization": 99}, nil), service.ErrAccountRuntimeIdentityChanged)
	})
	t.Run("parent_revision_rejects_shadow_snapshot", func(t *testing.T) {
		parent := create(t)
		shadow := create(t)
		require.NoError(t, client.Account.UpdateOneID(shadow.ID).SetParentAccountID(parent.ID).SetQuotaDimension("spark").Exec(ctx))
		shadow, err := repository.GetByID(ctx, shadow.ID)
		require.NoError(t, err)
		expected, err := service.AccountWithRuntimeCredentialOwner(shadow, parent)
		require.NoError(t, err)
		require.NoError(t, repository.UpdateAccountRuntimeSnapshot(ctx, expected, map[string]any{"session_window_utilization": 1}, nil))
		_, err = database.Exec(`UPDATE accounts SET extra=jsonb_set(extra,'{credential_secret_revision}','2') WHERE id=$1`, parent.ID)
		require.NoError(t, err)
		require.ErrorIs(t, repository.UpdateAccountRuntimeSnapshot(ctx, expected, map[string]any{"session_window_utilization": 99}, nil), service.ErrAccountRuntimeIdentityChanged)
	})
	t.Run("move_and_delete_keep_old_group_tombstone", func(t *testing.T) {
		account := create(t)
		first, err := client.Group.Create().SetName("catalog-first").SetPlatform("openai").Save(ctx)
		require.NoError(t, err)
		second, err := client.Group.Create().SetName("catalog-second").SetPlatform("openai").Save(ctx)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = database.Exec("DELETE FROM groups WHERE id IN ($1,$2)", first.ID, second.ID) })
		_, err = database.Exec("INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)", account.ID, first.ID)
		require.NoError(t, err)
		firstScope := "group:" + formatCatalogID(first.ID)
		before := generation(firstScope)
		_, err = database.Exec("UPDATE account_groups SET group_id=$1 WHERE account_id=$2", second.ID, account.ID)
		require.NoError(t, err)
		require.Greater(t, generation(firstScope), before)
		secondScope := "group:" + formatCatalogID(second.ID)
		before = generation(secondScope)
		require.NoError(t, repository.Delete(ctx, account.ID))
		require.Greater(t, generation(secondScope), before)
	})
	t.Run("independent_notification_consumer", func(t *testing.T) {
		watcher := &schedulerOutboxRepository{db: database, dsn: dsn + "&application_name=catalog_event_listener_test"}
		watchCtx, cancel := context.WithCancel(ctx)
		var calls atomic.Int64
		done := make(chan struct{})
		go func() { defer close(done); watcher.WatchModelCatalog(watchCtx, func() { calls.Add(1) }) }()
		defer func() { cancel(); <-done }()
		require.Eventually(t, func() bool { return calls.Load() > 0 }, 3*time.Second, 10*time.Millisecond)
		baseline := calls.Load()
		_, err := database.Exec("SELECT pg_notify('model_catalog_changed','out-of-order')")
		require.NoError(t, err)
		time.Sleep(50 * time.Millisecond)
		require.Equal(t, baseline, calls.Load(), "duplicate/out-of-order hints do not regress observed generation")
		_, err = database.Exec("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name='catalog_event_listener_test' AND pid <> pg_backend_pid()")
		require.NoError(t, err)
		_ = create(t)
		require.Eventually(t, func() bool { return calls.Load() > baseline }, 3*time.Second, 10*time.Millisecond)
	})
}

func formatCatalogID(id int64) string { return strconv.FormatInt(id, 10) }
