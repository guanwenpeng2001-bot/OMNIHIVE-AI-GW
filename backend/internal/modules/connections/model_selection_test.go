//go:build unit

package connections

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	legacy "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/connections"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestActivationUsesUnifiedModelSelectionPostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to isolated job_test")
	}
	db := testresources.OpenSchema(t, dsn)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `CREATE TABLE accounts (LIKE public.accounts INCLUDING ALL); CREATE TABLE scheduler_outbox (LIKE public.scheduler_outbox INCLUDING ALL)`)
	require.NoError(t, err)
	require.NoError(t, repository.ApplyMigrations(ctx, db))
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	var id int64
	require.NoError(t, tx.QueryRowContext(ctx, `INSERT INTO accounts(workspace_id,name,platform,type,status,schedulable,credentials,extra) VALUES(1,'connection','openai','apikey','disabled',false,'{}','{"upstream_model_allowlist":{"enabled":false,"models":[]}}') RETURNING id`).Scan(&id))
	err = legacy.Activate(ctx, tx, 1, id, 7, "connection", "https://upstream.test/v1", "", 2, []string{"target-a", "target-b"})
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	var rawCredentials, rawExtra []byte
	require.NoError(t, db.QueryRowContext(ctx, `SELECT credentials,extra FROM accounts WHERE id=$1`, id).Scan(&rawCredentials, &rawExtra))
	var credentials, extra map[string]any
	require.NoError(t, json.Unmarshal(rawCredentials, &credentials))
	require.NoError(t, json.Unmarshal(rawExtra, &extra))
	require.Equal(t, map[string]any{"target-a": "target-a", "target-b": "target-b"}, credentials["model_mapping"])
	scope := extra[service.UpstreamModelAllowlistExtraKey].(map[string]any)
	require.Equal(t, true, scope["enabled"])
	var models []string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT ARRAY(SELECT jsonb_array_elements_text(extra->'upstream_model_allowlist'->'models')) FROM accounts WHERE id=$1`, id).Scan(pq.Array(&models)))
	require.Equal(t, []string{"target-a", "target-b"}, models)
	var outbox int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM scheduler_outbox WHERE account_id=$1`, id).Scan(&outbox))
	require.Equal(t, 1, outbox)
}
