//go:build integration

package migrate

import (
	"errors"
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil"
	"github.com/stretchr/testify/require"
)

type broken struct{ service.SecretEncryptor }

func (b broken) Decrypt(string) (string, error) { return "", errors.New("unreadable") }
func (b broken) EncryptionKeyConfigured() bool  { return true }

func TestBackfillQuarantinesBeforeReplacingInline(t *testing.T) {
	db, _, _ := testutil.NewCapabilityPostgres(t)
	ctx := t.Context()
	enc, e := repository.NewAESEncryptor(&config.Config{Totp: config.TotpConfig{EncryptionKeyConfigured: true, EncryptionKey: strings.Repeat("4f", 32)}})
	require.NoError(t, e)
	var w, id, unsafe int64
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO workspaces(name) VALUES('migration fixture') RETURNING id").Scan(&w))
	insertLegacy := func(name, credentials string) int64 {
		t.Helper()
		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer tx.Rollback()
		var accountID int64
		require.NoError(t, tx.QueryRowContext(ctx, `INSERT INTO accounts(workspace_id,name,platform,type) VALUES($1,$2,'openai','apikey') RETURNING id`, w, name).Scan(&accountID))
		_, err = tx.ExecContext(ctx, `SELECT set_config('omnihive.account_secret_backfill',$1,true)`, accountID)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, `UPDATE accounts SET credentials=$2 WHERE id=$1`, accountID, credentials)
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
		return accountID
	}
	id = insertLegacy("migrate", `{"api_key":"test-inline-credential"}`)
	_, e = Batch(ctx, db, broken{enc}, 1000)
	require.NoError(t, e)
	var inline bool
	require.NoError(t, db.QueryRowContext(ctx, `SELECT a.credentials ? 'api_key' FROM accounts a JOIN connection_migration_quarantine q ON q.account_id=a.id WHERE a.id=$1 AND q.reason='CREDENTIAL_UNREADABLE'`, id).Scan(&inline))
	require.True(t, inline)
	_, e = db.ExecContext(ctx, "DELETE FROM connection_migration_quarantine WHERE account_id=$1", id)
	require.NoError(t, e)
	unsafe = insertLegacy("unsafe", `{"api_key":"safe-test-only","base_url":"https://up.example/?token=review"}`)
	_, e = Batch(ctx, db, enc, 1000)
	require.NoError(t, e)
	var ref string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT k.reference,a.credentials ? 'api_key' FROM provider_connections c JOIN credential_secrets k USING(account_id,workspace_id) JOIN accounts a ON a.id=c.account_id WHERE c.account_id=$1`, id).Scan(&ref, &inline))
	require.NotEmpty(t, ref)
	require.False(t, inline)
	var reason string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT reason FROM connection_migration_quarantine WHERE account_id=$1", unsafe).Scan(&reason))
	require.Equal(t, "ENDPOINT_REVIEW", reason)
	var unchanged string
	_, e = db.ExecContext(ctx, `UPDATE accounts SET name='safe rename' WHERE id=$1`, id)
	require.NoError(t, e)
	_, e = Batch(ctx, db, enc, 1000)
	require.NoError(t, e)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT k.reference FROM provider_connections c JOIN credential_secrets k USING(workspace_id,account_id) WHERE c.account_id=$1 AND NOT legacy_dirty AND c.name='safe rename'`, id).Scan(&unchanged))
	require.Equal(t, ref, unchanged)
}
