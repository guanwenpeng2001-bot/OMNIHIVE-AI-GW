//go:build integration

package verification

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/migrations"
	"github.com/stretchr/testify/require"
)

type testAccess struct{}

func (testAccess) Evaluate(_ context.Context, actor, workspace, project int64, action string) (identity.Access, error) {
	return identity.Access{ActorID: actor, WorkspaceID: workspace, ProjectID: project, Actions: []string{action}}, nil
}

func TestVerificationQueuePostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to an isolated job_test database")
	}
	db := testresources.OpenSchema(t, dsn)
	ctx := t.Context()
	exec := func(statement string, args ...any) {
		t.Helper()
		_, err := db.ExecContext(ctx, statement, args...)
		require.NoError(t, err)
	}
	exec(`CREATE TABLE workspaces(id bigint PRIMARY KEY,is_default boolean); INSERT INTO workspaces VALUES(1,true);
 CREATE FUNCTION resolve_default_workspace_id() RETURNS bigint LANGUAGE sql AS 'SELECT 1::bigint';
 CREATE TABLE identity_actions(action text PRIMARY KEY, category text);
		CREATE TABLE users (id bigint PRIMARY KEY,status text,role text,deleted_at timestamptz);
		INSERT INTO users VALUES (1,'active','admin',NULL)`)
	migration, err := migrations.FS.ReadFile("316_catalog_verification.sql")
	require.NoError(t, err)
	exec(string(migration))

	store := NewStore(db, testAccess{})
	entries := make([]Matrix, maxPending+1)
	for i := range entries {
		model := fmt.Sprintf("model-%d", i)
		m := Matrix{catalog.StableID("openai", model), "ad_1", "text.chat", "api_key", "openai_chat"}
		entries[i] = m
		exec(`INSERT INTO catalog_verification_targets
			(id,cell_id,owner_id,connection_id,entry_id,provider,model,action,protocol,account_mode,adapter_version,interval_seconds)
			VALUES ($1,$2,1,1,$3,'openai',$4,$5,$6,$7,$8,0)`,
			uuid.New(), m.cellID(), m.EntryID, model, m.Action, m.Protocol, m.AccountMode, m.AdapterVersion)
	}

	var submissions sync.WaitGroup
	results := make([]error, len(entries))
	for i, m := range entries {
		submissions.Go(func() {
			// Independent Store instances use the same PostgreSQL queue limit.
			_, results[i] = NewStore(db, testAccess{}).Enqueue(ctx, 1, fmt.Sprintf("key-%d", i), m)
		})
	}
	submissions.Wait()
	var rejected int
	for i, err := range results {
		if err != nil {
			require.ErrorIs(t, err, queueFull)
			rejected++
			continue
		}
		replay, err := store.Enqueue(ctx, 1, fmt.Sprintf("key-%d", i), entries[i])
		require.NoError(t, err)
		require.Equal(t, "queued", replay.State)
	}
	require.Equal(t, 1, rejected)
	var active int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM catalog_verification_jobs WHERE state IN ('queued','running')`).Scan(&active))
	require.Equal(t, maxPending, active)

	var running []*job
	for range maxRunning {
		job, err := store.claim(ctx)
		require.NoError(t, err)
		require.NotNil(t, job)
		running = append(running, job)
	}
	job, err := store.claim(ctx)
	require.NoError(t, err)
	require.Nil(t, job)
	exec(`UPDATE catalog_verification_jobs SET lease_until=statement_timestamp()-interval '1 second' WHERE state='running'`)
	exec(`UPDATE catalog_verification_jobs SET queued_at=statement_timestamp()-interval '6 minutes' WHERE state='queued'`)
	job, err = store.claim(ctx)
	require.NoError(t, err)
	require.Nil(t, job)
	for _, j := range running {
		require.NoError(t, store.finish(ctx, j, probeResult{state: success, reason: "MODEL_OBSERVED", samples: 1}))
		op, err := store.ReadOperation(ctx, 1, 1, "cv_"+j.id.String())
		require.NoError(t, err)
		require.Equal(t, "unknown", op.State)
		require.Equal(t, "WORKER_LEASE_EXPIRED", op.Result.(*Observation).Reason)
	}
	var expired int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM catalog_verification_evidence WHERE reason='QUEUE_EXPIRED'`).Scan(&expired))
	require.Equal(t, maxPending-maxRunning, expired)

	exec(`UPDATE catalog_verification_targets SET interval_seconds=600,next_due_at=statement_timestamp()-interval '1 second'
		WHERE entry_id=$1`, entries[0].EntryID)
	scheduled, err := store.claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, scheduled)
	require.Equal(t, entries[0], scheduled.target.Matrix)
	require.Greater(t, scheduled.target.NextDueAt.Sub(time.Now()), 9*time.Minute)
	job, err = store.claim(ctx)
	require.NoError(t, err)
	require.Nil(t, job, "two workers must agree that this recurring sample is pending")
}
