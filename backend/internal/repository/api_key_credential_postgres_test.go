//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/migrations"
	"github.com/stretchr/testify/require"
)

// apiKeyCredentialMigrationDatabase creates a disposable database, so the fresh and
// legacy upgrade paths never share state.
func apiKeyCredentialMigrationDatabase(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("API_KEY_CREDENTIAL_TEST_DSN")
	if dsn == "" {
		t.Skip("set API_KEY_CREDENTIAL_TEST_DSN to a disposable PostgreSQL instance")
	}
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	name := fmt.Sprintf("api_key_credentials_%d", time.Now().UnixNano())
	_, err = admin.Exec(`CREATE DATABASE "` + name + `"`)
	require.NoError(t, err)
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	parsed.Path = "/" + name
	db, err := sql.Open("postgres", parsed.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
		_, dropErr := admin.Exec(`DROP DATABASE "` + name + `" WITH (FORCE)`)
		require.NoError(t, dropErr)
		_ = admin.Close()
	})
	return db
}

// migrationsThrough mirrors every embedded migration that existed before the given
// file, so a legacy database can be built without the credential table.
func migrationsThrough(t *testing.T, before string) fstest.MapFS {
	t.Helper()
	names, err := fs.Glob(migrations.FS, "*.sql")
	require.NoError(t, err)
	result := fstest.MapFS{}
	for _, name := range names {
		if name >= before {
			continue
		}
		body, readErr := migrations.FS.ReadFile(name)
		require.NoError(t, readErr)
		result[name] = &fstest.MapFile{Data: body}
	}
	return result
}

type apiKeyCredentialFixture struct {
	ctx            context.Context
	db             *sql.DB
	client         *dbent.Client
	keys           *apiKeyRepository
	credentials    service.APIKeyCredentialRepository
	credentialLife *service.APIKeyService
}

func newAPIKeyCredentialFixture(t *testing.T, db *sql.DB) *apiKeyCredentialFixture {
	t.Helper()
	ctx := context.Background()
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	keys := newAPIKeyRepositoryWithSQL(client, db)
	credentials := NewAPIKeyCredentialRepository(db)
	lifecycle := service.NewAPIKeyService(keys, NewUserRepository(client, db), nil, nil, nil, nil, &config.Config{})
	lifecycle.SetCredentialRepository(credentials)
	return &apiKeyCredentialFixture{
		ctx:            ctx,
		db:             db,
		client:         client,
		keys:           keys,
		credentials:    credentials,
		credentialLife: lifecycle,
	}
}

func (f *apiKeyCredentialFixture) createUser(t *testing.T, email string) int64 {
	t.Helper()
	user := &service.User{
		Email:        email,
		Username:     email,
		PasswordHash: "not-a-real-hash",
		Role:         domain.RoleUser,
		Status:       service.StatusActive,
		Concurrency:  1,
	}
	require.NoError(t, NewUserRepository(f.client, f.db).Create(f.ctx, user))
	require.Positive(t, user.ID)
	return user.ID
}

// An upgrade fixture seeds a historical schema before the current onboarding
// repository and its default-workspace membership columns are available.
func (f *apiKeyCredentialFixture) createLegacyUser(t *testing.T, email string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, f.db.QueryRowContext(f.ctx,
		`INSERT INTO users(email,password_hash) VALUES ($1,'fixture') RETURNING id`, email).Scan(&id))
	return id
}

func (f *apiKeyCredentialFixture) seedKeys(t *testing.T, userID int64, name string, secret string, deleted bool) int64 {
	t.Helper()
	var id int64
	require.NoError(t, f.db.QueryRow(`
		INSERT INTO api_keys(user_id, workspace_id, key, name, status)
		VALUES ($1, $2, $3, $4, 'active') RETURNING id`,
		userID, domain.DefaultWorkspaceID, secret, name).Scan(&id))
	if deleted {
		_, err := f.db.Exec(`UPDATE api_keys SET deleted_at = NOW(), key = '__deleted__' || id || '__' WHERE id = $1`, id)
		require.NoError(t, err)
	}
	return id
}

type apiKeyCredentialSnapshot struct {
	Digest  string
	Status  string
	Suffix  string
	Revoked bool
}

func (f *apiKeyCredentialFixture) credentialRows(t *testing.T, apiKeyID int64) []apiKeyCredentialSnapshot {
	t.Helper()
	rows, err := f.db.Query(`
		SELECT credential_digest, status, display_suffix, revoked_at IS NOT NULL
		FROM api_key_credentials WHERE api_key_id = $1 ORDER BY id`, apiKeyID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	out := make([]apiKeyCredentialSnapshot, 0, 2)
	for rows.Next() {
		var row apiKeyCredentialSnapshot
		require.NoError(t, rows.Scan(&row.Digest, &row.Status, &row.Suffix, &row.Revoked))
		out = append(out, row)
	}
	require.NoError(t, rows.Err())
	return out
}

func (f *apiKeyCredentialFixture) auditCount(t *testing.T, action string) int {
	t.Helper()
	var count int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM audit_logs WHERE action = $1`, action).Scan(&count))
	return count
}

func (f *apiKeyCredentialFixture) createKey(t *testing.T, userID int64, name string, secret string) *service.APIKey {
	t.Helper()
	key := &service.APIKey{UserID: userID, Key: secret, Name: name, Status: service.StatusAPIKeyActive}
	require.NoError(t, f.keys.Create(f.ctx, key))
	return key
}

// A brand new database needs no plaintext read at all: creating a key writes exactly
// one current digest credential and the plaintext column stays unused.
func TestAPIKeyCredentialMigrationFreshDatabaseStoresOnlyDigests(t *testing.T) {
	db := apiKeyCredentialMigrationDatabase(t)
	require.NoError(t, applyMigrationsFS(context.Background(), db, migrations.FS))
	fixture := newAPIKeyCredentialFixture(t, db)

	secret := "sk-fresh-0123456789"
	key := fixture.createKey(t, fixture.createUser(t, "fresh@example.invalid"), "fresh", secret)

	rows := fixture.credentialRows(t, key.ID)
	require.Len(t, rows, 1)
	require.Equal(t, service.APIKeyCredentialDigest(secret), rows[0].Digest)
	require.Equal(t, domain.CredentialStatusCurrent, rows[0].Status)
	require.Equal(t, service.APIKeyCredentialDisplaySuffix(secret), rows[0].Suffix)

	var storedKey string
	require.NoError(t, fixture.db.QueryRow(`SELECT COALESCE(key, '') FROM api_keys WHERE id = $1`, key.ID).Scan(&storedKey))
	require.Empty(t, storedKey, "the plaintext column must stay unused for new keys")

	// The digest is the only auth input: the presented secret resolves to the key.
	resolved, err := fixture.keys.GetByKeyForAuth(fixture.ctx, secret)
	require.NoError(t, err)
	require.Equal(t, key.ID, resolved.ID)
	require.Nil(t, resolved.CredentialValidUntil, "a current credential has no deadline")

	// A duplicate custom secret is rejected through the digest unique index.
	duplicate := &service.APIKey{UserID: key.UserID, Key: secret, Name: "duplicate", Status: service.StatusAPIKeyActive}
	require.ErrorIs(t, fixture.keys.Create(fixture.ctx, duplicate), service.ErrAPIKeyExists)

	// Rotation keeps the identity, opens one grace credential and keeps the old one
	// usable for the grace window.
	issue, err := fixture.credentialLife.RotateAPIKey(fixture.ctx, key.ID, 0, false, service.ConfigurationAuditActor{UserID: key.UserID, Role: domain.RoleUser})
	require.NoError(t, err)
	require.Equal(t, key.ID, issue.APIKey.ID, "rotation must keep the key identity")
	require.NotEqual(t, secret, issue.Secret)
	require.NotNil(t, issue.GraceUntil)

	rows = fixture.credentialRows(t, key.ID)
	require.Len(t, rows, 2)
	require.Equal(t, domain.CredentialStatusGrace, rows[0].Status)
	require.Equal(t, domain.CredentialStatusCurrent, rows[1].Status)
	require.Equal(t, service.APIKeyCredentialDigest(issue.Secret), rows[1].Digest)

	// Both secrets authenticate the same key, so usage and billing stay on one key.
	for _, presented := range []string{secret, issue.Secret} {
		rotationResolved, resolveErr := fixture.keys.GetByKeyForAuth(fixture.ctx, presented)
		require.NoError(t, resolveErr)
		require.Equal(t, key.ID, rotationResolved.ID)
	}

	// A second rotation revokes the older grace credential: at most one grace survives.
	second, err := fixture.credentialLife.RotateAPIKey(fixture.ctx, key.ID, 0, false, service.ConfigurationAuditActor{UserID: key.UserID, Role: domain.RoleUser})
	require.NoError(t, err)
	rows = fixture.credentialRows(t, key.ID)
	require.Len(t, rows, 3)
	require.True(t, rows[0].Revoked, "the older grace credential must be revoked")
	require.Equal(t, domain.CredentialStatusGrace, rows[1].Status)
	require.Equal(t, domain.CredentialStatusCurrent, rows[2].Status)
	require.Equal(t, service.APIKeyCredentialDigest(second.Secret), rows[2].Digest)

	// Everything is audited on the existing channel and no secret leaks into it.
	require.Equal(t, 2, fixture.auditCount(t, "api_key.credential.rotate"))
	var auditExtra string
	require.NoError(t, fixture.db.QueryRow(`SELECT extra::text FROM audit_logs WHERE action = 'api_key.credential.rotate' ORDER BY id LIMIT 1`).Scan(&auditExtra))
	require.NotContains(t, auditExtra, secret)
	require.NotContains(t, auditExtra, issue.Secret)
	require.NotContains(t, auditExtra, second.Secret)

	// Revoking the grace credential stops the previous secret only.
	revoked, err := fixture.credentialLife.RevokeAPIKeyCredentials(fixture.ctx, key.ID, service.ConfigurationAuditActor{UserID: key.UserID, Role: domain.RoleUser})
	require.NoError(t, err)
	require.Equal(t, int64(1), revoked)
	_, err = fixture.keys.GetByKeyForAuth(fixture.ctx, issue.Secret)
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
	stillResolved, err := fixture.keys.GetByKeyForAuth(fixture.ctx, second.Secret)
	require.NoError(t, err)
	require.Equal(t, key.ID, stillResolved.ID)
	require.Equal(t, 1, fixture.auditCount(t, "api_key.credential.revoke"))

	// Retiring the key revokes every credential and keeps one audit record per action.
	require.NoError(t, fixture.credentialLife.DeleteAPIKeyByID(fixture.ctx, key.ID))
	_, err = fixture.keys.GetByKeyForAuth(fixture.ctx, second.Secret)
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
	require.Equal(t, 1, fixture.auditCount(t, "api_key.credential.revoke_all"))
}

// A database created before the credential table keeps the original key ids: each live
// plaintext key is backfilled to a digest credential, then the plaintext is cleared.
func TestAPIKeyCredentialMigrationBackfillsLegacyPlaintextAndClearsIt(t *testing.T) {
	db := apiKeyCredentialMigrationDatabase(t)
	ctx := context.Background()
	require.NoError(t, applyMigrationsFS(ctx, db, migrationsThrough(t, "271_add_api_key_credentials.sql")))

	setup := newAPIKeyCredentialFixture(t, db)
	userID := setup.createLegacyUser(t, "legacy@example.invalid")
	liveSecret := "sk-legacy-live-0001"
	liveID := setup.seedKeys(t, userID, "legacy-live", liveSecret, false)
	prefixedSecret := "__deleted__audit_live_key"
	prefixedID := setup.seedKeys(t, userID, "legacy-live-deleted-prefix", prefixedSecret, false)
	tombstoneID := setup.seedKeys(t, userID, "legacy-deleted", "sk-legacy-deleted-0002", true)
	require.Zero(t, setup.auditCount(t, "api_key.credential.rotate"))

	// The upgrade runs on top of the legacy schema.
	require.NoError(t, applyMigrationsFS(ctx, db, migrations.FS))

	rows := setup.credentialRows(t, liveID)
	require.Len(t, rows, 1)
	require.Equal(t, service.APIKeyCredentialDigest(liveSecret), rows[0].Digest)
	require.Equal(t, domain.CredentialStatusCurrent, rows[0].Status)
	require.Equal(t, service.APIKeyCredentialDisplaySuffix(liveSecret), rows[0].Suffix)

	var liveStoredKey, tombstoneStoredKey string
	require.NoError(t, db.QueryRow(`SELECT COALESCE(key, '') FROM api_keys WHERE id = $1`, liveID).Scan(&liveStoredKey))
	require.NoError(t, db.QueryRow(`SELECT COALESCE(key, '') FROM api_keys WHERE id = $1`, tombstoneID).Scan(&tombstoneStoredKey))
	require.Empty(t, liveStoredKey, "the legacy plaintext must be cleared")
	require.Empty(t, tombstoneStoredKey, "tombstoned keys are cleared as well")

	// The stable mapping is the point of the migration: the legacy key id still works.
	resolved, err := setup.keys.GetByKeyForAuth(ctx, liveSecret)
	require.NoError(t, err)
	require.Equal(t, liveID, resolved.ID, "the digest must map back to the original key id")
	prefixed, err := setup.keys.GetByKeyForAuth(ctx, prefixedSecret)
	require.NoError(t, err, "a custom secret prefix cannot identify a deleted key")
	require.Equal(t, prefixedID, prefixed.ID)
	require.Empty(t, setup.credentialRows(t, tombstoneID), "deleted_at identifies the real tombstone")

	var plaintextLeft, uncovered int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM api_keys WHERE deleted_at IS NULL AND COALESCE(key, '') <> ''`).Scan(&plaintextLeft))
	require.Zero(t, plaintextLeft)
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM api_keys a WHERE a.deleted_at IS NULL AND NOT EXISTS (
		SELECT 1 FROM api_key_credentials c WHERE c.api_key_id = a.id)`).Scan(&uncovered))
	require.Zero(t, uncovered)

	// The plaintext column is now nullable, so new keys never write it.
	var nullable string
	require.NoError(t, db.QueryRow(`SELECT is_nullable FROM information_schema.columns WHERE table_name = 'api_keys' AND column_name = 'key'`).Scan(&nullable))
	require.Equal(t, "YES", nullable)

	// Re-running the runner is a no-op: the ledger keeps its checksums and the
	// migration body cannot duplicate credentials.
	ledgerBefore := migrationLedgerSnapshot(t, db)
	require.NoError(t, applyMigrationsFS(ctx, db, migrations.FS))
	require.Equal(t, ledgerBefore, migrationLedgerSnapshot(t, db))
	require.Len(t, setup.credentialRows(t, liveID), 1)
}

func TestAPIKeyCredentialMigrationRefusesDigestOwnedByAnotherKey(t *testing.T) {
	database := apiKeyCredentialMigrationDatabase(t)
	ctx := context.Background()
	require.NoError(t, applyMigrationsFS(ctx, database, migrationsThrough(t, "271_add_api_key_credentials.sql")))
	fixture := newAPIKeyCredentialFixture(t, database)
	userID := fixture.createLegacyUser(t, "digest-owner@example.invalid")
	secret := "__deleted__still_live_custom_key"
	keyID := fixture.seedKeys(t, userID, "expected-owner", secret, false)
	otherID := fixture.seedKeys(t, userID, "wrong-owner", "sk-other-legacy-owner", false)
	body, err := migrations.FS.ReadFile("271_add_api_key_credentials.sql")
	require.NoError(t, err)
	// Simulate an interrupted/manual expansion whose digest belongs to the wrong
	// identity. Verification must stop before any plaintext is withdrawn.
	expansion, _, found := strings.Cut(string(body), "-- Backfill:")
	require.True(t, found)
	_, err = database.Exec(expansion)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO api_key_credentials(api_key_id, credential_digest) VALUES ($1, $2)`,
		otherID, service.APIKeyCredentialDigest(secret))
	require.NoError(t, err)
	err = applyMigrationsFS(ctx, database, migrations.FS)
	require.ErrorContains(t, err, "refusing to clear plaintext")
	var retained string
	require.NoError(t, database.QueryRow(`SELECT key FROM api_keys WHERE id = $1`, keyID).Scan(&retained))
	require.Equal(t, secret, retained)
}

// A credential that reaches its grace deadline is retired once, audited, and rejected
// from then on even while a warm cache entry still exists.
func TestAPIKeyCredentialExpiryRetiresOnceAndIsAudited(t *testing.T) {
	db := apiKeyCredentialMigrationDatabase(t)
	ctx := context.Background()
	require.NoError(t, applyMigrationsFS(ctx, db, migrations.FS))
	fixture := newAPIKeyCredentialFixture(t, db)

	userID := fixture.createUser(t, "expiry@example.invalid")
	keyID := fixture.seedKeys(t, userID, "expiry", "", false)
	expiredSecret := "sk-expired-grace-0003"
	_, err := db.Exec(`
		INSERT INTO api_key_credentials(workspace_id, api_key_id, credential_digest, display_suffix, status, valid_until)
		VALUES ($1, $2, $3, $4, 'grace', NOW() - INTERVAL '1 minute')`,
		domain.DefaultWorkspaceID, keyID, service.APIKeyCredentialDigest(expiredSecret), service.APIKeyCredentialDisplaySuffix(expiredSecret))
	require.NoError(t, err)

	_, err = fixture.keys.GetByKeyForAuth(ctx, expiredSecret)
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound, "an expired grace credential must be rejected")

	rows := fixture.credentialRows(t, keyID)
	require.Len(t, rows, 1)
	require.True(t, rows[0].Revoked)
	require.Equal(t, domain.CredentialStatusRevoked, rows[0].Status)
	require.Equal(t, 1, fixture.auditCount(t, "api_key.credential.expire"))

	// Re-presenting the expired secret must not write a second audit record.
	_, err = fixture.keys.GetByKeyForAuth(ctx, expiredSecret)
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
	require.Equal(t, 1, fixture.auditCount(t, "api_key.credential.expire"))
}
