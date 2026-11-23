package repository

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/enttest"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// SQLite executes the actual account mutations and commits. Only the unrelated
// PostgreSQL billing-probe lock query is adapted: these Cursor fixtures have no
// probe settings, and each interleaving is explicitly ordered before Update.
type cursorEditTestDriver struct{ *entsql.Driver }
type cursorEditTestTx struct{ dialect.Tx }

var cursorEditPostgresPlaceholder = regexp.MustCompile(`\$[0-9]+`)

func cursorEditSQLiteQuery(query string) string {
	query = strings.TrimSuffix(query, " FOR UPDATE")
	query = strings.ReplaceAll(query, "::jsonb", "")
	query = strings.ReplaceAll(query, "NOW()", "CURRENT_TIMESTAMP")
	return cursorEditPostgresPlaceholder.ReplaceAllString(query, "?")
}

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
	entTx, _ := tx.Tx.(*entsql.Tx)
	query = cursorEditSQLiteQuery(query)
	return entTx.QueryContext(ctx, query, args...)
}
func (tx cursorEditTestTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	entTx, _ := tx.Tx.(*entsql.Tx)
	query = cursorEditSQLiteQuery(query)
	return entTx.ExecContext(ctx, query, args...)
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
	require.NoError(t, client.Workspace.Create().SetName("Default").SetIsDefault(true).Exec(t.Context()))
	_, err = db.Exec(`CREATE TABLE scheduler_outbox (id INTEGER PRIMARY KEY, event_type TEXT, account_id INTEGER, group_id INTEGER, payload BLOB, dedup_key TEXT UNIQUE)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE provider_connections (account_id INTEGER PRIMARY KEY REFERENCES accounts(id), legacy_dirty BOOLEAN DEFAULT false, verified_id TEXT, version INTEGER DEFAULT 1)`)
	require.NoError(t, err)
	_, err = db.Exec(`DROP TABLE credential_secrets`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE credential_secrets(id INTEGER PRIMARY KEY, reference TEXT NOT NULL UNIQUE, workspace_id INTEGER NOT NULL DEFAULT 1, account_id INTEGER NOT NULL UNIQUE, version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0), ciphertext TEXT NOT NULL, revoked_at DATETIME, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, api_key_digest TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE audit_logs(id INTEGER PRIMARY KEY, actor_user_id INTEGER, actor_role TEXT, action TEXT, request_id TEXT, status_code INTEGER, extra BLOB)`)
	require.NoError(t, err)
	repository := newAccountRepositoryWithSQL(client, db, nil)
	repository.credentialEncryptor = persistentTestAES(t, true)
	return repository, client, db
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
				require.NotContains(t, current.Credentials, "refresh_token")
				resolved, err := repo.GetByID(ctx, entity.ID)
				require.NoError(t, err)
				require.Equal(t, snapshot.Credentials["refresh_token"], resolved.Credentials["refresh_token"])
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
				resolved, err := repo.GetByID(ctx, entity.ID)
				require.NoError(t, err)
				require.Equal(t, "synthetic-new", resolved.Credentials["refresh_token"])
				require.Equal(t, edit.Credentials[service.CredentialSecretReferenceKey], resolved.Credentials[service.CredentialSecretReferenceKey])
				require.Equal(t, service.CredentialVersion(current.Credentials), service.CredentialVersion(resolved.Credentials))
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

func TestManagedConnectionRejectsLegacyAccountEdits(t *testing.T) {
	ctx := context.Background()
	repo, client, db := newCursorEditTestRepository(t)
	original, err := client.Account.Create().SetName("original").SetPlatform(service.PlatformOpenAI).
		SetType(service.AccountTypeAPIKey).SetCredentials(map[string]any{
		"base_url": "https://old.example", "model_mapping": map[string]any{"alias": "model"},
		service.CredentialSecretReferenceKey: "ref-1", service.CredentialSecretVersionKey: 1,
	}).Save(ctx)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO provider_connections(account_id, verified_id) VALUES(?, 'verification-1')`, original.ID)
	require.NoError(t, err)
	for _, field := range []string{"name", "header_overrides", "base_url"} {
		t.Run(field, func(t *testing.T) {
			edit := accountEntityToService(original)
			edit.Credentials = map[string]any{}
			switch field {
			case "name":
				edit.Name = "renamed"
			case "header_overrides":
				edit.Credentials[field] = map[string]any{"X-Example": "test"}
			case "base_url":
				edit.Credentials[field] = "https://new.example"
			}
			err := repo.UpdateWithAccountBillingSettings(ctx, edit, nil, nil, nil)
			require.ErrorIs(t, err, service.ErrAccountConnectionManaged)
			current, err := client.Account.Get(ctx, original.ID)
			require.NoError(t, err)
			require.Equal(t, original.Name, current.Name)
			require.Equal(t, "https://old.example", current.Credentials["base_url"])
			require.Equal(t, map[string]any{"alias": "model"}, current.Credentials["model_mapping"])
			require.Equal(t, "ref-1", current.Credentials[service.CredentialSecretReferenceKey])
			require.EqualValues(t, 1, current.Credentials[service.CredentialSecretVersionKey])
			var verified string
			var version int
			require.NoError(t, db.QueryRow(`SELECT verified_id, version FROM provider_connections WHERE account_id=? AND NOT legacy_dirty`, original.ID).Scan(&verified, &version))
			require.Equal(t, "verification-1", verified)
			require.Equal(t, 1, version)
		})
	}
}
