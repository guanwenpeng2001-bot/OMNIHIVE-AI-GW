package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestReviewCatalogIdentityWritesClearPersistentObservations(t *testing.T) {
	dsn := os.Getenv("CATALOG_EVENT_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("UPSTREAM_JOB_TEST_DSN")
	}
	if dsn == "" {
		t.Skip("set CATALOG_EVENT_TEST_DSN to a disposable PostgreSQL database")
	}
	database, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	ctx := context.Background()
	var name string
	require.NoError(t, database.QueryRow("SELECT current_database()").Scan(&name))
	require.Contains(t, []string{"catalog_event_test", "job_test"}, name)
	require.NoError(t, ApplyMigrations(ctx, database))
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, database)))
	repository := newAccountRepositoryWithSQL(client, database, nil)
	for _, mode := range []string{"single_update", "bulk_update", "extra_update"} {
		t.Run(mode, func(t *testing.T) {
			entity, err := client.Account.Create().SetName("review-catalog-runtime").SetPlatform("cursor").SetType("oauth").
				SetCredentials(map[string]any{"access_token": "old"}).
				SetExtra(map[string]any{"cursor_usage_snapshot": map[string]any{"usage": 12}, "cursor_usage_updated_at": time.Now().Format(time.RFC3339), "quota_used": 17}).Save(ctx)
			require.NoError(t, err)
			t.Cleanup(func() {
				_, cleanupErr := database.Exec("DELETE FROM accounts WHERE id=$1", entity.ID)
				require.NoError(t, cleanupErr)
			})
			account, err := repository.GetByID(ctx, entity.ID)
			require.NoError(t, err)
			oldIdentity := service.AccountRuntimeIdentityKey(account)
			switch mode {
			case "single_update":
				account.Credentials = map[string]any{"access_token": "replacement"}
				require.NoError(t, repository.Update(ctx, account))
			case "bulk_update":
				count, err := repository.BulkUpdate(ctx, []int64{entity.ID}, service.AccountBulkUpdate{Credentials: map[string]any{"access_token": "replacement"}})
				require.NoError(t, err)
				require.Equal(t, int64(1), count)
			case "extra_update":
				require.NoError(t, repository.UpdateExtra(ctx, entity.ID, map[string]any{"credential_secret_revision": 2}))
			}
			current, err := repository.GetByID(ctx, entity.ID)
			require.NoError(t, err)
			require.NotEqual(t, oldIdentity, service.AccountRuntimeIdentityKey(current))
			t.Logf("mode=%s old snapshot=%v financial counter=%v", mode, current.Extra["cursor_usage_snapshot"], current.Extra["quota_used"])
			require.Equal(t, float64(17), current.Extra["quota_used"])
			require.NotContains(t, current.Extra, "cursor_usage_snapshot", "new identity must not reuse previous identity's observations")
		})
	}
}

func TestBulkUpdateAndUpdateExtraSQLClearRuntimeObservationsOnIdentityChange(t *testing.T) {
	t.Run("bulk_credentials", func(t *testing.T) {
		exec := &recordingSQLExecutor{result: rowsAffectedResult(1)}
		repository := newAccountRepositoryWithSQL(nil, exec, nil)
		count, err := repository.BulkUpdate(context.Background(), []int64{42}, service.AccountBulkUpdate{Credentials: map[string]any{"access_token": "replacement"}})
		require.NoError(t, err)
		require.Equal(t, int64(1), count)
		require.NotEmpty(t, exec.execQueries)
		query := normalizeSQLWhitespace(exec.execQueries[0])
		require.Contains(t, query, "cursor_usage_snapshot")
		require.Contains(t, query, "credentials IS DISTINCT FROM")
		extraIndex := strings.Index(query, "extra =")
		credentialsIndex := strings.Index(query, "credentials = COALESCE")
		require.GreaterOrEqual(t, extraIndex, 0)
		require.GreaterOrEqual(t, credentialsIndex, 0)
		require.Less(t, extraIndex, credentialsIndex, "extra must be assigned before credentials so identity comparisons still see the old row")
	})
	t.Run("extra_revision", func(t *testing.T) {
		database, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = database.Close() })
		client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, database)))
		t.Cleanup(func() { _ = client.Close() })
		mock.ExpectBegin()
		mock.ExpectExec(`(?s)UPDATE accounts SET extra = CASE WHEN \(.*cursor_usage_snapshot.*WHERE id = \$2 AND deleted_at IS NULL`).
			WithArgs(`{"credential_secret_revision":2}`, int64(42)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox")).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()
		repository := newAccountRepositoryWithSQL(client, database, nil)
		require.NoError(t, repository.UpdateExtra(context.Background(), 42, map[string]any{"credential_secret_revision": 2}))
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestCatalogIdentityWritesClearPersistentObservations(t *testing.T) {
	dsn := os.Getenv("CATALOG_EVENT_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("UPSTREAM_JOB_TEST_DSN")
	}
	if dsn == "" {
		t.Skip("set CATALOG_EVENT_TEST_DSN to a disposable PostgreSQL database")
	}
	database, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	ctx := context.Background()
	var name string
	require.NoError(t, database.QueryRow("SELECT current_database()").Scan(&name))
	require.Contains(t, []string{"catalog_event_test", "job_test"}, name)
	require.NoError(t, ApplyMigrations(ctx, database))
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, database)))
	repository := newAccountRepositoryWithSQL(client, database, nil)
	t.Run("observation_removal_matches_service", func(t *testing.T) {
		expression := clearChangedAccountRuntimeObservationsSQL("credentials", "extra", "TRUE")
		_, fieldList, found := strings.Cut(expression, "ARRAY['")
		require.True(t, found)
		fieldList, _, found = strings.Cut(fieldList, "']::text[]")
		require.True(t, found)
		extra := map[string]any{"quota_used": float64(17), "quota_limit": float64(100), "note": "keep"}
		for _, field := range strings.Split(fieldList, "','") {
			extra[field] = float64(12)
		}
		payload, err := json.Marshal(extra)
		require.NoError(t, err)
		var cleaned []byte
		require.NoError(t, database.QueryRow("SELECT "+expression+" FROM (SELECT '{}'::jsonb AS credentials, $1::jsonb AS extra) AS fixture", string(payload)).Scan(&cleaned))
		var actual map[string]any
		require.NoError(t, json.Unmarshal(cleaned, &actual))
		expected := &service.Account{Extra: extra}
		service.ClearAccountRuntimeObservations(expected)
		require.Equal(t, expected.Extra, actual)
	})
	for _, mode := range []string{"single_update", "bulk_credentials", "bulk_extra", "extra_revision", "bulk_same_identity", "extra_same_identity", "bulk_note", "extra_note", "bulk_status", "rollback_bulk", "rollback_extra"} {
		t.Run(mode, func(t *testing.T) {
			entity, err := client.Account.Create().SetName("catalog-runtime-observations").SetPlatform("cursor").SetType("oauth").
				SetStatus(service.StatusError).SetSchedulable(false).
				SetCredentials(map[string]any{"access_token": "old"}).
				SetExtra(map[string]any{"credential_secret_revision": 1, "cursor_usage_snapshot": map[string]any{"usage": 12}, "cursor_usage_updated_at": time.Now().Format(time.RFC3339), "quota_used": 17, "quota_limit": 100}).Save(ctx)
			require.NoError(t, err)
			t.Cleanup(func() {
				_, cleanupErr := database.Exec("DELETE FROM accounts WHERE id=$1", entity.ID)
				require.NoError(t, cleanupErr)
			})
			before, err := repository.GetByID(ctx, entity.ID)
			require.NoError(t, err)
			writeCtx := ctx
			var transaction *dbent.Tx
			if mode == "rollback_bulk" || mode == "rollback_extra" {
				transaction, err = client.Tx(ctx)
				require.NoError(t, err)
				defer transaction.Rollback()
				writeCtx = dbent.NewTxContext(ctx, transaction)
			}
			changed := true
			expectedStatus := service.StatusError
			switch mode {
			case "single_update":
				before.Credentials = map[string]any{"access_token": "replacement"}
				require.NoError(t, repository.Update(writeCtx, before))
			case "bulk_credentials", "rollback_bulk":
				count, err := repository.BulkUpdate(writeCtx, []int64{entity.ID}, service.AccountBulkUpdate{Credentials: map[string]any{"access_token": "replacement"}})
				require.NoError(t, err)
				require.Equal(t, int64(1), count)
			case "bulk_extra":
				_, err := repository.BulkUpdate(writeCtx, []int64{entity.ID}, service.AccountBulkUpdate{Extra: map[string]any{"credential_secret_revision": 2}})
				require.NoError(t, err)
			case "extra_revision", "rollback_extra":
				require.NoError(t, repository.UpdateExtra(writeCtx, entity.ID, map[string]any{"credential_secret_revision": 2}))
			case "bulk_same_identity":
				changed = false
				_, err := repository.BulkUpdate(writeCtx, []int64{entity.ID}, service.AccountBulkUpdate{Credentials: map[string]any{"access_token": "old"}, Extra: map[string]any{"credential_secret_revision": 1}})
				require.NoError(t, err)
			case "extra_same_identity":
				changed = false
				require.NoError(t, repository.UpdateExtra(writeCtx, entity.ID, map[string]any{"credential_secret_revision": 1}))
			case "bulk_note":
				changed = false
				_, err := repository.BulkUpdate(writeCtx, []int64{entity.ID}, service.AccountBulkUpdate{Extra: map[string]any{"note": "edited"}})
				require.NoError(t, err)
			case "extra_note":
				changed = false
				require.NoError(t, repository.UpdateExtra(writeCtx, entity.ID, map[string]any{"note": "edited"}))
			case "bulk_status":
				changed = false
				expectedStatus = service.StatusDisabled
				_, err := repository.BulkUpdate(writeCtx, []int64{entity.ID}, service.AccountBulkUpdate{Status: &expectedStatus})
				require.NoError(t, err)
			}
			currentEntity, err := clientFromContext(writeCtx, client).Account.Get(writeCtx, entity.ID)
			require.NoError(t, err)
			current := accountEntityToService(currentEntity)
			require.Equal(t, float64(17), current.Extra["quota_used"])
			require.Equal(t, float64(100), current.Extra["quota_limit"])
			require.Equal(t, expectedStatus, current.Status)
			require.False(t, current.Schedulable)
			if changed {
				require.NotContains(t, current.Extra, "cursor_usage_snapshot")
				require.NotContains(t, current.Extra, "cursor_usage_updated_at")
			} else {
				require.Equal(t, before.Extra["cursor_usage_snapshot"], current.Extra["cursor_usage_snapshot"])
				require.Equal(t, before.Extra["cursor_usage_updated_at"], current.Extra["cursor_usage_updated_at"])
			}
			if transaction != nil {
				require.NoError(t, transaction.Rollback())
				current, err = repository.GetByID(ctx, entity.ID)
				require.NoError(t, err)
				require.Equal(t, before.Credentials, current.Credentials)
				require.Equal(t, before.Extra, current.Extra)
			}
		})
	}
}
