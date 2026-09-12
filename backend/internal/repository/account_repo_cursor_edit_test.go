package repository

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// SQLite executes the actual account mutations and commits. Only the unrelated
// PostgreSQL billing-probe lock query is adapted: these Cursor fixtures have no
// probe settings, and each interleaving is explicitly ordered before Update.
type cursorEditTestDriver struct{ *entsql.Driver }
type cursorEditTestTx struct{ dialect.Tx }

func (d cursorEditTestDriver) Tx(ctx context.Context) (dialect.Tx, error) {
	tx, err := d.Driver.Tx(ctx)
	if err != nil {
		return nil, err
	}
	return cursorEditTestTx{tx}, nil
}
func (tx cursorEditTestTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, "FOR NO KEY UPDATE") {
		query = "SELECT true, false, true, NULL, NULL, NULL, NULL, NULL, NULL FROM accounts WHERE id = ?"
		args = args[:1]
	}
	return tx.Tx.(*entsql.Tx).QueryContext(ctx, query, args...)
}
func (tx cursorEditTestTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.Tx.(*entsql.Tx).ExecContext(ctx, query, args...)
}

func newCursorEditTestRepository(t *testing.T) (*accountRepository, *dbent.Client, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(cursorEditTestDriver{entsql.OpenDB(dialect.SQLite, db)})))
	t.Cleanup(func() { _ = client.Close() })
	_, err = db.Exec(`CREATE TABLE scheduler_outbox (id INTEGER PRIMARY KEY, event_type TEXT, account_id INTEGER, group_id INTEGER, payload BLOB, dedup_key TEXT UNIQUE)`)
	require.NoError(t, err)
	return newAccountRepositoryWithSQL(client, db, nil), client, db
}

func TestCursorMetadataEditRefreshRacePreservesDatabaseCooldown(t *testing.T) {
	for _, billing := range []bool{false, true} {
		for _, cleared := range []bool{false, true} {
			name := "new_cooldown_survives_old_edit"
			if cleared {
				name = "cleared_cooldown_not_restored_by_old_edit"
			}
			if billing {
				name += "/billing_settings"
			} else {
				name += "/update"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				repo, client, _ := newCursorEditTestRepository(t)
				entity, err := client.Account.Create().SetName("before").SetPlatform(service.PlatformCursor).
					SetType(service.AccountTypeOAuth).SetStatus(service.StatusActive).SetSchedulable(true).
					SetCredentials(map[string]any{"refresh_token": "synthetic-refresh"}).Save(ctx)
				require.NoError(t, err)
				until := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
				reason := "Cursor token refresh temporarily unavailable"
				if cleared {
					_, err = client.Account.UpdateOneID(entity.ID).SetTempUnschedulableUntil(until).SetTempUnschedulableReason(reason).Save(ctx)
					require.NoError(t, err)
				}
				// Admin has read, but has not entered its write transaction yet.
				snapshot, err := client.Account.Get(ctx, entity.ID)
				require.NoError(t, err)
				staleEdit := accountEntityToService(snapshot)
				staleEdit.Name = "edited"
				if cleared {
					_, err = client.Account.UpdateOneID(entity.ID).ClearTempUnschedulableUntil().ClearTempUnschedulableReason().Save(ctx)
				} else {
					// Commit the runtime fields written by a temporary refresh failure, after
					// the admin read and before its write. CAS predicates are tested separately.
					_, err = client.Account.UpdateOneID(entity.ID).SetTempUnschedulableUntil(until).SetTempUnschedulableReason(reason).Save(ctx)
				}
				require.NoError(t, err)
				if billing {
					require.NoError(t, repo.UpdateWithAccountBillingSettings(ctx, staleEdit, nil, nil, nil))
				} else {
					require.NoError(t, repo.Update(ctx, staleEdit))
				}
				current, err := client.Account.Get(ctx, entity.ID)
				require.NoError(t, err)
				require.Equal(t, "edited", current.Name)
				require.Equal(t, snapshot.Credentials, current.Credentials)
				if cleared {
					require.Nil(t, current.TempUnschedulableUntil)
					require.Nil(t, current.TempUnschedulableReason)
				} else {
					require.NotNil(t, current.TempUnschedulableUntil)
					require.True(t, until.Equal(*current.TempUnschedulableUntil))
					require.Equal(t, reason, *current.TempUnschedulableReason)
				}
			})
		}
	}
}

func TestCursorReauthRecoveryUsesCurrentReasonAndRollsBackWithCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		rollback     bool
	}{
		{"cursor_cooldown", "Cursor token refresh temporarily unavailable", false},
		{"legacy_refresh_cooldown", "token refresh retry exhausted", false},
		{"concurrent_unrelated_cooldown", "unrelated provider cooldown", false},
		{"outbox_failure", "Cursor token refresh temporarily unavailable", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo, client, db := newCursorEditTestRepository(t)
			until := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
			entity, err := client.Account.Create().SetName("before").SetPlatform(service.PlatformCursor).
				SetType(service.AccountTypeOAuth).SetStatus(service.StatusError).SetSchedulable(false).
				SetErrorMessage("Cursor reauth required").SetCredentials(map[string]any{"refresh_token": "synthetic-old"}).Save(ctx)
			require.NoError(t, err)
			edit := accountEntityToService(entity)
			edit.Credentials = map[string]any{"refresh_token": "synthetic-new"}
			edit.Status, edit.ErrorMessage, edit.Schedulable = service.StatusActive, "", true
			// The admin's cleared snapshot is older than this committed cooldown.
			_, err = client.Account.UpdateOneID(entity.ID).SetTempUnschedulableUntil(until).SetTempUnschedulableReason(tc.reason).Save(ctx)
			require.NoError(t, err)
			if tc.rollback {
				_, err = db.Exec(`CREATE TRIGGER reject_outbox BEFORE INSERT ON scheduler_outbox BEGIN SELECT RAISE(ABORT, 'synthetic outbox failure'); END`)
				require.NoError(t, err)
			}
			err = repo.UpdateWithCursorRefreshRecovery(ctx, edit, nil, nil, nil)
			if tc.rollback {
				require.ErrorContains(t, err, "synthetic outbox failure")
			} else {
				require.NoError(t, err)
			}
			current, err := client.Account.Get(ctx, entity.ID)
			require.NoError(t, err)
			if tc.rollback {
				require.Equal(t, entity.Credentials, current.Credentials)
				require.Equal(t, service.StatusError, current.Status)
				require.False(t, current.Schedulable)
			} else {
				require.Equal(t, edit.Credentials, current.Credentials)
				require.Equal(t, service.StatusActive, current.Status)
				require.True(t, current.Schedulable)
			}
			if tc.rollback || tc.name == "concurrent_unrelated_cooldown" {
				require.NotNil(t, current.TempUnschedulableUntil)
				require.True(t, until.Equal(*current.TempUnschedulableUntil))
				require.Equal(t, tc.reason, *current.TempUnschedulableReason)
			} else {
				require.Nil(t, current.TempUnschedulableUntil)
				require.Nil(t, current.TempUnschedulableReason)
			}
			var events int
			require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM scheduler_outbox").Scan(&events))
			if tc.rollback {
				require.Zero(t, events)
			} else {
				require.Equal(t, 1, events)
			}
		})
	}
}
