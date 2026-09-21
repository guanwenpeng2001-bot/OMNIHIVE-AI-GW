package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// credentialSecretPostgresEnv points to a disposable PostgreSQL database; the
// whole suite is skipped unless it is set (same discipline as the workspace
// scope postgres tests).
const credentialSecretPostgresEnv = "CREDENTIAL_SECRET_TEST_DSN"

type credentialSecretFixture struct {
	ctx       context.Context
	db        *sql.DB
	client    *dbent.Client
	accounts  *accountRepository
	secrets   service.CredentialSecretRepository
	encryptor service.SecretEncryptor
}

func newCredentialSecretFixture(t *testing.T) *credentialSecretFixture {
	t.Helper()
	dsn := os.Getenv(credentialSecretPostgresEnv)
	if dsn == "" {
		t.Skipf("set %s to a disposable PostgreSQL database", credentialSecretPostgresEnv)
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	require.NoError(t, applyMigrationsFS(ctx, db, migrations.FS))
	encryptor, err := NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32)}})
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	accounts := newAccountRepositoryWithSQL(client, db, nil)
	accounts.credentialEncryptor = encryptor
	return &credentialSecretFixture{
		ctx:       ctx,
		db:        db,
		client:    client,
		accounts:  accounts,
		secrets:   NewCredentialSecretRepository(db, encryptor),
		encryptor: encryptor,
	}
}

func (f *credentialSecretFixture) createAccount(t *testing.T, ctx context.Context, name, platform, accountType string, credentials map[string]any) *service.Account {
	t.Helper()
	account := &service.Account{
		Name:        name,
		Platform:    platform,
		Type:        accountType,
		Credentials: credentials,
		Status:      service.StatusActive,
		Concurrency: 1,
	}
	require.NoError(t, f.accounts.Create(ctx, account))
	require.Positive(t, account.ID)
	return account
}

func (f *credentialSecretFixture) createWorkspace(t *testing.T, name string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, f.db.QueryRowContext(f.ctx, `INSERT INTO workspaces(name) VALUES($1) RETURNING id`, name).Scan(&id))
	return id
}

func (f *credentialSecretFixture) storedCredentials(t *testing.T, accountID int64) map[string]any {
	t.Helper()
	var payload []byte
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT credentials FROM accounts WHERE id=$1`, accountID).Scan(&payload))
	stored := map[string]any{}
	require.NoError(t, json.Unmarshal(payload, &stored))
	return stored
}

func (f *credentialSecretFixture) accountRevision(t *testing.T, accountID int64) string {
	t.Helper()
	var updatedAt time.Time
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT updated_at FROM accounts WHERE id=$1`, accountID).Scan(&updatedAt))
	return updatedAt.UTC().Format(time.RFC3339Nano)
}

type credentialAuditRow struct {
	Extra     map[string]any
	ExtraRaw  string
	ActorID   int64
	ActorRole string
	RequestID string
}

// credentialSecretTestRequestID pins the lifecycle audit assertions to rows
// written by this test's actor context, so reused disposable databases (and the
// no-actor convert/update rows of neighbouring tests) cannot leak in.
const credentialSecretTestRequestID = "req-convert"

func (f *credentialSecretFixture) auditRows(t *testing.T, action string) []credentialAuditRow {
	t.Helper()
	rows, err := f.db.QueryContext(f.ctx, `SELECT extra::text, COALESCE(actor_user_id, 0), actor_role, request_id FROM audit_logs WHERE action=$1 AND request_id=$2 ORDER BY id`, action, credentialSecretTestRequestID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var out []credentialAuditRow
	for rows.Next() {
		var entry credentialAuditRow
		require.NoError(t, rows.Scan(&entry.ExtraRaw, &entry.ActorID, &entry.ActorRole, &entry.RequestID))
		require.NoError(t, json.Unmarshal([]byte(entry.ExtraRaw), &entry.Extra))
		out = append(out, entry)
	}
	require.NoError(t, rows.Err())
	return out
}

func uniqueName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func TestCredentialSecretLifecyclePostgres(t *testing.T) {
	f := newCredentialSecretFixture(t)
	ctx := service.WithConfigurationAuditActor(f.ctx, service.ConfigurationAuditActor{UserID: 9, Role: "admin", RequestID: credentialSecretTestRequestID})
	plaintextKey := "sk-plaintext-" + strings.Repeat("7", 16)
	account := f.createAccount(t, ctx, uniqueName("cred-lifecycle"), service.PlatformOpenAI, service.AccountTypeAPIKey,
		map[string]any{"api_key": plaintextKey, "base_url": "https://api.example.invalid"})

	t.Run("convert_requires_fresh_review", func(t *testing.T) {
		_, err := f.secrets.Convert(ctx, account.ID, "stale-revision")
		require.ErrorIs(t, err, service.ErrCredentialSecretConflict)
	})

	var reference string
	t.Run("explicit_convert_moves_secrets_out", func(t *testing.T) {
		metadata, err := f.secrets.Convert(ctx, account.ID, f.accountRevision(t, account.ID))
		require.NoError(t, err)
		require.Equal(t, int64(1), metadata.Version)
		require.Equal(t, account.ID, metadata.AccountID)
		require.Equal(t, domain.DefaultWorkspaceID, metadata.WorkspaceID)
		reference = metadata.Reference
		require.NotEmpty(t, reference)

		stored := f.storedCredentials(t, account.ID)
		require.NotContains(t, stored, "api_key", "converted row must not keep inline secrets")
		require.Equal(t, reference, stored[service.CredentialSecretReferenceKey])
		require.Equal(t, "https://api.example.invalid", stored["base_url"], "non-secret config stays inline")

		resolved, err := f.accounts.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.Equal(t, plaintextKey, resolved.Credentials["api_key"], "reads resolve back to plaintext for runtime")
		require.Equal(t, reference, resolved.Credentials[service.CredentialSecretReferenceKey])
	})

	t.Run("convert_is_idempotent", func(t *testing.T) {
		again, err := f.secrets.Convert(ctx, account.ID, f.accountRevision(t, account.ID))
		require.NoError(t, err)
		require.Equal(t, reference, again.Reference)
		require.Equal(t, int64(1), again.Version, "idempotent convert must not mint a new secret")
	})

	t.Run("trigger_blocks_inline_secret_writes", func(t *testing.T) {
		_, err := f.db.ExecContext(ctx, `UPDATE accounts SET credentials = credentials || '{"api_key":"smuggled"}'::jsonb WHERE id=$1`, account.ID)
		require.Error(t, err, "referenced account must not regain inline secrets even via raw SQL")
		_, err = f.db.ExecContext(ctx, `UPDATE accounts SET credentials = credentials - 'credential_secret_reference' WHERE id=$1`, account.ID)
		require.Error(t, err, "reference cannot be stripped")
	})

	t.Run("rotate_cas_single_winner", func(t *testing.T) {
		metadata, err := f.secrets.Rotate(ctx, account.ID, 1, map[string]any{"api_key": "sk-rotated-winner"})
		require.NoError(t, err)
		require.Equal(t, int64(2), metadata.Version)

		const contenders = 2
		results := make(chan error, contenders)
		var wg sync.WaitGroup
		for i := 0; i < contenders; i++ {
			wg.Add(1)
			go func(attempt int) {
				defer wg.Done()
				_, rotateErr := f.secrets.Rotate(ctx, account.ID, 2, map[string]any{"api_key": fmt.Sprintf("sk-attempt-%d", attempt)})
				results <- rotateErr
			}(i)
		}
		wg.Wait()
		close(results)
		var successes, conflicts int
		for rotateErr := range results {
			if rotateErr == nil {
				successes++
			} else if errors.Is(rotateErr, service.ErrCredentialSecretConflict) {
				conflicts++
			} else {
				t.Fatalf("unexpected rotate error: %v", rotateErr)
			}
		}
		require.Equal(t, 1, successes, "concurrent rotation must have a single winner")
		require.Equal(t, contenders-1, conflicts)
	})

	t.Run("revoke_is_terminal_and_explicit", func(t *testing.T) {
		_, err := f.secrets.Revoke(ctx, account.ID, 2)
		require.ErrorIs(t, err, service.ErrCredentialSecretConflict, "stale version cannot revoke")

		metadata, err := f.secrets.Revoke(ctx, account.ID, 3)
		require.NoError(t, err)
		require.NotNil(t, metadata.RevokedAt)

		_, err = f.accounts.GetByID(ctx, account.ID)
		require.ErrorIs(t, err, service.ErrCredentialSecretRevoked, "revoked reference must fail explicitly, never fall back inline")
		_, err = f.secrets.Rotate(ctx, account.ID, 4, map[string]any{"api_key": "sk-after-revoke"})
		require.ErrorIs(t, err, service.ErrCredentialSecretRevoked)

		replay, err := f.secrets.Revoke(ctx, account.ID, 3)
		require.NoError(t, err, "revoke replay with the winning version is idempotent")
		require.NotNil(t, replay.RevokedAt)
	})

	t.Run("audit_diff_is_redacted_and_attributed", func(t *testing.T) {
		converts := f.auditRows(t, "credential_secret.convert")
		require.NotEmpty(t, converts)
		rotates := f.auditRows(t, "credential_secret.rotate")
		require.NotEmpty(t, rotates)
		revokes := f.auditRows(t, "credential_secret.revoke")
		require.NotEmpty(t, revokes)

		for _, rows := range [][]credentialAuditRow{converts, rotates, revokes} {
			for _, row := range rows {
				for _, plaintext := range []string{plaintextKey, "sk-rotated-winner", "sk-attempt-0", "sk-attempt-1"} {
					require.NotContains(t, row.ExtraRaw, plaintext, "audit rows must never contain plaintext secrets")
				}
				require.Equal(t, "admin", row.ActorRole)
				require.Equal(t, credentialSecretTestRequestID, row.RequestID)
				require.Equal(t, int64(9), row.ActorID)
				require.Contains(t, row.Extra, "diff")
			}
		}
	})
}

func TestCredentialSecretCrossWorkspacePostgres(t *testing.T) {
	f := newCredentialSecretFixture(t)
	ctx := f.ctx
	foreignWorkspace := f.createWorkspace(t, uniqueName("foreign"))
	home := f.createAccount(t, ctx, uniqueName("cred-home"), service.PlatformOpenAI, service.AccountTypeAPIKey,
		map[string]any{"api_key": "sk-home-workspace"})
	metadata, err := f.secrets.Convert(ctx, home.ID, f.accountRevision(t, home.ID))
	require.NoError(t, err)

	foreignCtx := domain.WithWorkspaceScope(ctx, foreignWorkspace)
	foreignAccount := f.createAccount(t, foreignCtx, uniqueName("cred-foreign"), service.PlatformOpenAI, service.AccountTypeAPIKey,
		map[string]any{"api_key": "sk-foreign-workspace"})

	t.Run("metadata_read_is_scoped", func(t *testing.T) {
		_, err := f.secrets.Metadata(foreignCtx, home.ID)
		require.ErrorIs(t, err, service.ErrCredentialSecretNotFound, "a foreign workspace must not read the reference")
		visible, err := f.secrets.Metadata(ctx, home.ID)
		require.NoError(t, err)
		require.Equal(t, metadata.Reference, visible.Reference)
	})

	t.Run("account_resolution_is_scoped", func(t *testing.T) {
		_, err := f.accounts.GetByID(foreignCtx, home.ID)
		require.Error(t, err, "cross-workspace account reads stay rejected")
	})

	t.Run("reference_cannot_be_smuggled_into_foreign_account", func(t *testing.T) {
		stored := f.storedCredentials(t, foreignAccount.ID)
		stored[service.CredentialSecretReferenceKey] = metadata.Reference
		err := f.accounts.UpdateCredentials(foreignCtx, foreignAccount.ID, stored)
		require.ErrorIs(t, err, service.ErrCredentialSecretInvalid, "inline account must not accept a reference on update")

		_, err = f.db.ExecContext(ctx, `UPDATE accounts SET credentials = credentials || $2::jsonb WHERE id=$1`,
			foreignAccount.ID, fmt.Sprintf(`{"credential_secret_reference":%q}`, metadata.Reference))
		require.Error(t, err, "trigger rejects binding another workspace's reference")
	})
}

func TestCredentialSecretOAuthRefreshPostgres(t *testing.T) {
	f := newCredentialSecretFixture(t)
	ctx := f.ctx
	account := f.createAccount(t, ctx, uniqueName("cred-grok"), service.PlatformGrok, service.AccountTypeOAuth,
		map[string]any{"access_token": "grok-access-1", "refresh_token": "grok-refresh-1"})
	_, err := f.secrets.Convert(ctx, account.ID, f.accountRevision(t, account.ID))
	require.NoError(t, err)

	t.Run("refresh_cas_updates_secret_version", func(t *testing.T) {
		loaded, err := f.accounts.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.Equal(t, "grok-access-1", loaded.Credentials["access_token"])

		replacement := map[string]any{"access_token": "grok-access-2", "refresh_token": "grok-refresh-2"}
		applied, err := f.accounts.UpdateGrokOAuthCredentialsIfUnchanged(ctx, account.ID, loaded.Credentials, loaded.ProxyID, replacement)
		require.NoError(t, err)
		require.True(t, applied)

		reloaded, err := f.accounts.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.Equal(t, "grok-access-2", reloaded.Credentials["access_token"])
		require.Equal(t, int64(2), service.CredentialVersion(reloaded.Credentials), "refresh rotation bumps the secret version")

		stored := f.storedCredentials(t, account.ID)
		require.NotContains(t, stored, "access_token", "rotated tokens stay out of the accounts row")

		stale, err := f.accounts.UpdateGrokOAuthCredentialsIfUnchanged(ctx, account.ID, loaded.Credentials, loaded.ProxyID,
			map[string]any{"access_token": "grok-access-3", "refresh_token": "grok-refresh-3"})
		require.NoError(t, err)
		require.False(t, stale, "stale refresh snapshot loses the CAS without erroring")

		unchanged, err := f.accounts.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.Equal(t, "grok-access-2", unchanged.Credentials["access_token"], "loser refresh must not overwrite the winner")
	})

	t.Run("refresh_error_status_cas_lands", func(t *testing.T) {
		loaded, err := f.accounts.GetByID(ctx, account.ID)
		require.NoError(t, err)
		applied, err := f.accounts.SetGrokOAuthRefreshErrorIfCredentialsUnchanged(ctx, account.ID, loaded.Credentials, loaded.ProxyID, "refresh rejected")
		require.NoError(t, err)
		require.True(t, applied, "status-only CAS writes must work for referenced accounts")

		var status, message string
		require.NoError(t, f.db.QueryRowContext(ctx, `SELECT status, error_message FROM accounts WHERE id=$1`, account.ID).Scan(&status, &message))
		require.Equal(t, service.StatusError, status)
		require.Equal(t, "refresh rejected", message)
	})
}

func TestCredentialSecretInlineCompatibilityPostgres(t *testing.T) {
	f := newCredentialSecretFixture(t)
	ctx := f.ctx
	account := f.createAccount(t, ctx, uniqueName("cred-inline"), service.PlatformOpenAI, service.AccountTypeAPIKey,
		map[string]any{"api_key": "sk-inline-keep"})

	t.Run("inline_reads_and_writes_stay_inline", func(t *testing.T) {
		require.NoError(t, f.accounts.UpdateCredentials(ctx, account.ID, map[string]any{"api_key": "sk-inline-updated", "base_url": "https://new.example.invalid"}))
		stored := f.storedCredentials(t, account.ID)
		require.Equal(t, "sk-inline-updated", stored["api_key"])
		require.NotContains(t, stored, service.CredentialSecretReferenceKey)

		loaded, err := f.accounts.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.Equal(t, "sk-inline-updated", loaded.Credentials["api_key"])

		_, err = f.secrets.Metadata(ctx, account.ID)
		require.ErrorIs(t, err, service.ErrCredentialSecretNotFound, "inline accounts have no secret metadata")
	})

	t.Run("inline_account_rejects_reference_smuggling", func(t *testing.T) {
		err := f.accounts.UpdateCredentials(ctx, account.ID, map[string]any{
			"api_key":                            "sk-inline-updated",
			service.CredentialSecretReferenceKey: "nonexistent-reference",
		})
		require.ErrorIs(t, err, service.ErrCredentialSecretInvalid)
	})
}

func TestCredentialSecretMigrationIdempotentPostgres(t *testing.T) {
	f := newCredentialSecretFixture(t)
	migration, err := migrations.FS.ReadFile("269_add_credential_secret_references.sql")
	require.NoError(t, err)
	for attempt := 0; attempt < 2; attempt++ {
		_, err = f.db.ExecContext(f.ctx, string(migration))
		require.NoError(t, err, "269 migration must be replay-safe (attempt %d)", attempt)
	}
	var regclass sql.NullString
	require.NoError(t, f.db.QueryRowContext(f.ctx, `SELECT to_regclass('public.credential_secrets')`).Scan(&regclass))
	require.True(t, regclass.Valid)
}
