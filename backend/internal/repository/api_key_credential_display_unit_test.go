package repository

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"

	"github.com/Wei-Shaw/sub2api/ent/apikeycredential"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// The read paths that feed a masked DTO must carry the non-secret display suffix of the
// key's current credential, and they must never carry a secret.
func TestAPIKeyRepository_DisplaySuffixReachesDetailAndListReads_SQLite(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "display-suffix-unit@test.com")

	secret := "sk-display-suffix-unit-4321"
	key := &service.APIKey{
		UserID: user.ID,
		Key:    secret,
		Name:   "Display Key Unit",
		Status: service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))
	require.NotContains(t, key.Key, "sk-stored", "sanity: the request carries the secret")

	wantSuffix := service.APIKeyCredentialDisplaySuffix(secret)

	detail, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, wantSuffix, detail.DisplaySuffix)
	require.Empty(t, detail.Key, "the plaintext column is never repopulated")

	listed, _, err := repo.ListByUserID(ctx, user.ID, pagination.PaginationParams{Page: 1, PageSize: 10}, service.APIKeyListFilters{})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, wantSuffix, listed[0].DisplaySuffix)

	all, err := repo.ListAllByUserID(ctx, user.ID, service.APIKeyListFilters{})
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.Equal(t, wantSuffix, all[0].DisplaySuffix)

	// A key without any credential row (legacy row written outside the repository) must
	// still render, just without a suffix, instead of failing the whole list.
	orphan := &service.APIKey{UserID: user.ID, Name: "Orphan", Status: service.StatusActive}
	require.NoError(t, repo.Create(ctx, orphan))
	_, err = client.APIKeyCredential.Delete().
		Where(apikeycredential.APIKeyIDEQ(orphan.ID)).
		Exec(ctx)
	require.NoError(t, err)

	orphanDetail, err := repo.GetByID(ctx, orphan.ID)
	require.NoError(t, err)
	require.Empty(t, orphanDetail.DisplaySuffix)

	orphanList, _, err := repo.ListByUserID(ctx, user.ID, pagination.PaginationParams{Page: 1, PageSize: 10}, service.APIKeyListFilters{})
	require.NoError(t, err)
	require.Len(t, orphanList, 2)
}

// Rotation must move the display suffix to the new credential so the masked value the
// UI shows after a rotation describes the credential that is actually current.
func TestAPIKeyRepository_DisplaySuffixFollowsRotation_SQLite(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "display-rotation-unit@test.com")

	key := &service.APIKey{
		UserID: user.ID,
		Key:    "sk-display-rotation-unit-0001",
		Name:   "Rotating Key Unit",
		Status: service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))

	rotatedSecret := "sk-display-rotation-unit-9876"
	_, err := client.APIKeyCredential.Update().
		Where(apikeycredential.APIKeyIDEQ(key.ID)).
		SetStatus(domain.CredentialStatusGrace).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.APIKeyCredential.Create().
		SetAPIKeyID(key.ID).
		SetCredentialDigest(service.APIKeyCredentialDigest(rotatedSecret)).
		SetDisplaySuffix(service.APIKeyCredentialDisplaySuffix(rotatedSecret)).
		SetStatus(domain.CredentialStatusCurrent).
		Save(ctx)
	require.NoError(t, err)

	detail, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, service.APIKeyCredentialDisplaySuffix(rotatedSecret), detail.DisplaySuffix)
}

// createAuditLogsTableForSQLite creates the append-only audit table the credential
// lifecycle writes to. Production gets it from migration 180; the Ent-only SQLite test
// schema does not, and a failed audit write would roll back the retirement it records.
func createAuditLogsTableForSQLite(t *testing.T, client *dbent.Client) {
	t.Helper()
	_, err := client.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS audit_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at TEXT NOT NULL,
			actor_user_id BIGINT,
			actor_email TEXT NOT NULL DEFAULT '',
			actor_role TEXT NOT NULL DEFAULT '',
			auth_method TEXT NOT NULL DEFAULT '',
			credential_masked TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL DEFAULT '',
			method TEXT NOT NULL DEFAULT '',
			path TEXT NOT NULL DEFAULT '',
			request_id TEXT NOT NULL DEFAULT '',
			client_ip TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT '',
			request_body TEXT NOT NULL DEFAULT '',
			status_code INTEGER NOT NULL DEFAULT 0,
			latency_ms BIGINT NOT NULL DEFAULT 0,
			extra TEXT NOT NULL DEFAULT '{}'
		)`)
	require.NoError(t, err)
}

// Every key lookup resolves the presented secret through its credential digest, so a
// revoked credential stops resolving while the key identity stays intact.
func TestAPIKeyRepository_GetByKeyResolvesThroughTheDigest_SQLite(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	createAuditLogsTableForSQLite(t, client)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "getbykey-digest-unit@test.com")

	secret := "sk-getbykey-digest-unit-0001"
	key := &service.APIKey{
		UserID: user.ID,
		Key:    secret,
		Name:   "Digest Lookup Unit",
		Status: service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))

	resolved, err := repo.GetByKey(ctx, secret)
	require.NoError(t, err)
	require.Equal(t, key.ID, resolved.ID)
	require.NotNil(t, resolved.User, "the full projection still loads the owner")
	require.Empty(t, resolved.Key, "the repository never returns a stored secret")

	authResolved, err := repo.GetByKeyForAuth(ctx, secret)
	require.NoError(t, err)
	require.Equal(t, key.ID, authResolved.ID)

	_, err = repo.GetByKey(ctx, "sk-getbykey-digest-unit-unknown")
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)

	_, err = client.APIKeyCredential.Update().
		Where(apikeycredential.APIKeyIDEQ(key.ID)).
		SetStatus(domain.CredentialStatusRevoked).
		Save(ctx)
	require.NoError(t, err)

	_, err = repo.GetByKey(ctx, secret)
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound, "a revoked credential stops resolving")
	_, err = repo.GetByKeyForAuth(ctx, secret)
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)

	// An expired graceful credential is rejected and retired in place.
	graceSecret := "sk-getbykey-digest-unit-0002"
	expired := time.Now().UTC().Add(-time.Minute)
	_, err = client.APIKeyCredential.Create().
		SetAPIKeyID(key.ID).
		SetCredentialDigest(service.APIKeyCredentialDigest(graceSecret)).
		SetDisplaySuffix(service.APIKeyCredentialDisplaySuffix(graceSecret)).
		SetStatus(domain.CredentialStatusGrace).
		SetValidUntil(expired).
		Save(ctx)
	require.NoError(t, err)

	_, err = repo.GetByKey(ctx, graceSecret)
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)

	expiredRow, err := client.APIKeyCredential.Query().
		Where(apikeycredential.CredentialDigestEQ(service.APIKeyCredentialDigest(graceSecret))).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, domain.CredentialStatusRevoked, expiredRow.Status)
	require.NotNil(t, expiredRow.RevokedAt)

	// The expiry is recorded once, on the existing audit channel, and a second attempt
	// neither writes again nor changes the row.
	require.Equal(t, 1, countExpiryAuditRows(t, client))
	_, err = repo.GetByKey(ctx, graceSecret)
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
	require.Equal(t, 1, countExpiryAuditRows(t, client), "an expired credential is audited exactly once")
}

func countExpiryAuditRows(t *testing.T, client *dbent.Client) int {
	t.Helper()
	rows, err := client.QueryContext(context.Background(), `SELECT count(*) FROM audit_logs WHERE action = ?`, "api_key.credential.expire")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	count := 0
	if rows.Next() {
		require.NoError(t, rows.Scan(&count))
	}
	require.NoError(t, rows.Err())
	return count
}
