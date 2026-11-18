//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/migrations"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyCredentialMutationsEvictRevokedCredentials(t *testing.T) {
	database := apiKeyCredentialMigrationDatabase(t)
	ctx := context.Background()
	require.NoError(t, applyMigrationsFS(ctx, database, migrations.FS))
	fixture := newAPIKeyCredentialFixture(t, database)
	userID := fixture.createUser(t, "cache-revocation@example.invalid")
	cache := NewAPIKeyCache(integrationRedis)
	lifecycle := service.NewAPIKeyService(fixture.keys, nil, nil, nil, nil, cache, &config.Config{
		APIKeyAuth: config.APIKeyAuthCacheConfig{L2TTLSeconds: 300},
	})
	lifecycle.SetCredentialRepository(fixture.credentials)
	actor := service.ConfigurationAuditActor{UserID: userID, Role: service.RoleUser}

	for _, operation := range []string{"revoke_all", "delete", "revoke_grace", "rotate_immediately", "rotate_twice"} {
		t.Run(operation, func(t *testing.T) {
			secret := fmt.Sprintf("sk-%s-%d", operation, time.Now().UnixNano())
			key := fixture.createKey(t, userID, operation, secret)
			if operation == "revoke_grace" || operation == "rotate_twice" {
				_, err := lifecycle.RotateAPIKey(ctx, key.ID, time.Hour, false, actor)
				require.NoError(t, err)
			}
			_, err := lifecycle.GetByKey(ctx, secret)
			require.NoError(t, err)
			digest := service.APIKeyCredentialDigest(secret)
			entry, err := cache.GetAuthCache(ctx, digest)
			require.NoError(t, err)
			require.NotNil(t, entry, "the credential must be cached before mutation")
			t.Cleanup(func() { require.NoError(t, cache.DeleteAuthCache(ctx, digest)) })

			switch operation {
			case "revoke_all":
				_, err = lifecycle.RevokeAllAPIKeyCredentials(ctx, key.ID, actor)
			case "delete":
				err = lifecycle.DeleteAPIKeyByID(ctx, key.ID)
			case "revoke_grace":
				_, err = lifecycle.RevokeAPIKeyCredentials(ctx, key.ID, actor)
			case "rotate_immediately":
				_, err = lifecycle.RotateAPIKey(ctx, key.ID, time.Hour, true, actor)
			case "rotate_twice":
				_, err = lifecycle.RotateAPIKey(ctx, key.ID, time.Hour, false, actor)
			}
			require.NoError(t, err)
			entry, _ = cache.GetAuthCache(ctx, digest)
			require.Nil(t, entry, "the committed mutation must evict the revoked digest")
			_, err = lifecycle.GetByKey(ctx, secret)
			require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
		})
	}
}

func TestAPIKeyCredentialOutboxTracksMutationsAndRollsBack(t *testing.T) {
	database := apiKeyCredentialMigrationDatabase(t)
	ctx := context.Background()
	require.NoError(t, applyMigrationsFS(ctx, database, migrations.FS))
	fixture := newAPIKeyCredentialFixture(t, database)
	userID := fixture.createUser(t, "credential-outbox@example.invalid")
	key := fixture.createKey(t, userID, "outbox", "sk-credential-outbox-current")
	digest := service.APIKeyCredentialDigest(key.Key)
	_, err := database.Exec(`DELETE FROM auth_cache_invalidation_outbox`)
	require.NoError(t, err)

	transaction, err := database.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = transaction.Rollback() })
	_, err = transaction.Exec(`UPDATE api_key_credentials SET status = 'revoked', revoked_at = NOW() WHERE api_key_id = $1`, key.ID)
	require.NoError(t, err)
	var eventCount int
	require.NoError(t, transaction.QueryRow(`SELECT count(*) FROM auth_cache_invalidation_outbox WHERE cache_key = $1`, digest).Scan(&eventCount))
	require.Equal(t, 1, eventCount, "revocation and its digest event must be in the same transaction")
	require.NoError(t, transaction.Rollback())
	require.NoError(t, database.QueryRow(`SELECT count(*) FROM auth_cache_invalidation_outbox`).Scan(&eventCount))
	require.Zero(t, eventCount)

	actor := service.ConfigurationAuditActor{UserID: userID, Role: service.RoleUser}
	issue, err := fixture.credentialLife.RotateAPIKey(ctx, key.ID, time.Hour, false, actor)
	require.NoError(t, err)
	digests, err := queryStringColumn(ctx, database, `SELECT cache_key FROM auth_cache_invalidation_outbox`)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{digest, service.APIKeyCredentialDigest(issue.Secret)}, digests)

	_, err = database.Exec(`DELETE FROM auth_cache_invalidation_outbox`)
	require.NoError(t, err)
	_, err = fixture.credentialLife.RevokeAllAPIKeyCredentials(ctx, key.ID, actor)
	require.NoError(t, err)
	digests, err = queryStringColumn(ctx, database, `SELECT cache_key FROM auth_cache_invalidation_outbox`)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{digest, service.APIKeyCredentialDigest(issue.Secret)}, digests)

	// Physical deletion can cascade credentials before an AFTER key trigger runs.
	// The credential trigger must retain each OLD digest in the durable outbox.
	_, err = database.Exec(`DELETE FROM auth_cache_invalidation_outbox`)
	require.NoError(t, err)
	_, err = database.Exec(`DELETE FROM api_keys WHERE id = $1`, key.ID)
	require.NoError(t, err)
	digests, err = queryStringColumn(ctx, database, `SELECT DISTINCT cache_key FROM auth_cache_invalidation_outbox`)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{digest, service.APIKeyCredentialDigest(issue.Secret)}, digests)
}

func TestAPIKeyCredentialOutboxCoversEveryCredentialOnPermissionChanges(t *testing.T) {
	database := apiKeyCredentialMigrationDatabase(t)
	ctx := context.Background()
	require.NoError(t, applyMigrationsFS(ctx, database, migrations.FS))
	fixture := newAPIKeyCredentialFixture(t, database)
	userID := fixture.createUser(t, "permissions-outbox@example.invalid")
	group := mustCreateGroup(t, fixture.client, &service.Group{Name: "credential-permissions", RateMultiplier: 1, IsExclusive: true})
	key := fixture.createKey(t, userID, "permissions", "sk-permissions-current")
	_, err := database.Exec(`UPDATE api_keys SET group_id = $1 WHERE id = $2`, group.ID, key.ID)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO user_allowed_groups (user_id, group_id) VALUES ($1, $2)`, userID, group.ID)
	require.NoError(t, err)
	digests := []string{service.APIKeyCredentialDigest(key.Key)}
	for _, status := range []string{"grace", "revoked"} {
		digest := service.APIKeyCredentialDigest("sk-permissions-" + status)
		digests = append(digests, digest)
		_, err = database.Exec(`INSERT INTO api_key_credentials(api_key_id, credential_digest, status, revoked_at)
			VALUES ($1, $2, $3::varchar, CASE WHEN $3::varchar = 'revoked' THEN NOW() ELSE NULL END)`, key.ID, digest, status)
		require.NoError(t, err)
	}
	_, err = database.Exec(`DELETE FROM auth_cache_invalidation_outbox`)
	require.NoError(t, err)
	for name, mutation := range map[string]string{
		"key":         `UPDATE api_keys SET status = 'disabled'`,
		"user":        `UPDATE users SET status = 'disabled'`,
		"group":       `UPDATE groups SET status = 'disabled'`,
		"grant":       `DELETE FROM user_allowed_groups`,
		"key_delete":  `UPDATE api_keys SET deleted_at = NOW()`,
		"user_delete": `DELETE FROM users`,
	} {
		t.Run(name, func(t *testing.T) {
			transaction, err := database.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = transaction.Rollback() }()
			_, err = transaction.Exec(mutation)
			require.NoError(t, err)
			events, err := queryStringColumn(ctx, transaction, `SELECT DISTINCT cache_key FROM auth_cache_invalidation_outbox`)
			require.NoError(t, err)
			require.ElementsMatch(t, digests, events)
		})
	}
}

func TestAPIKeyCredentialOutboxReplaysAfterMutatingProcessStops(t *testing.T) {
	database := apiKeyCredentialMigrationDatabase(t)
	ctx := context.Background()
	require.NoError(t, applyMigrationsFS(ctx, database, migrations.FS))
	fixture := newAPIKeyCredentialFixture(t, database)
	secret := fmt.Sprintf("sk-durable-revocation-%d", time.Now().UnixNano())
	key := fixture.createKey(t, fixture.createUser(t, "replay@example.invalid"), "replay", secret)
	cache := NewAPIKeyCache(integrationRedis)
	auth := service.NewAPIKeyService(fixture.keys, nil, nil, nil, nil, cache, &config.Config{
		APIKeyAuth: config.APIKeyAuthCacheConfig{L2TTLSeconds: 300},
	})
	_, err := auth.GetByKey(ctx, secret)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cache.DeleteAuthCache(ctx, service.APIKeyCredentialDigest(secret))) })
	_, err = database.Exec(`DELETE FROM auth_cache_invalidation_outbox;
		UPDATE api_key_credentials SET status = 'revoked', revoked_at = NOW()`)
	require.NoError(t, err)
	// No service-side invalidation runs after this commit. A later worker must
	// recover the event without access to the original credential plaintext.
	stale, err := auth.GetByKey(ctx, secret)
	require.NoError(t, err)
	require.Equal(t, key.ID, stale.ID)
	worker := service.NewAuthCacheInvalidationWorker(NewAuthCacheInvalidationOutboxRepository(database), cache)
	worker.Start()
	t.Cleanup(worker.Stop)
	require.Eventually(t, func() bool {
		entry, _ := cache.GetAuthCache(ctx, service.APIKeyCredentialDigest(secret))
		return entry == nil
	}, 5*time.Second, 20*time.Millisecond)
	_, err = auth.GetByKey(ctx, secret)
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
}
