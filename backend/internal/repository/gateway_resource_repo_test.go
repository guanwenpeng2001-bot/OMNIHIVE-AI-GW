//go:build unit

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Opt-in real PostgreSQL proof for the resource store extension. The dedicated
// database name guard prevents an accidental shared-database test run.
func TestGatewayResourcePostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to an isolated job_test PostgreSQL database")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	ctx := context.Background()
	var database string
	require.NoError(t, db.QueryRow(`SELECT current_database()`).Scan(&database))
	require.Equal(t, "job_test", database, "refusing to modify any non-test database")
	require.NoError(t, ApplyMigrations(ctx, db))

	concrete, ok := NewUpstreamJobRepository(db).(*upstreamJobRepository)
	require.True(t, ok)

	var user, key, account, group int64
	require.NoError(t, db.QueryRow(`INSERT INTO users(email,password_hash,balance) VALUES($1,'test',100) RETURNING id`, uuid.NewString()+"@example.test").Scan(&user))
	require.NoError(t, db.QueryRow(`INSERT INTO api_keys(user_id,key,name) VALUES($1,$2,'resource-test') RETURNING id`, user, uuid.NewString()).Scan(&key))
	require.NoError(t, db.QueryRow(`INSERT INTO accounts(name,platform,type) VALUES('resource-test','openai','apikey') RETURNING id`).Scan(&account))
	require.NoError(t, db.QueryRow(`INSERT INTO groups(name,platform) VALUES($1,'openai') RETURNING id`, uuid.NewString()).Scan(&group))

	run := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	named := func(name string) string { return name + "-" + run }
	insert := func(t *testing.T, publicID, family, status, purpose, inputFileID, resultRef string, deleted bool) {
		t.Helper()
		publicID, inputFileID = named(publicID), named(inputFileID)
		billing := map[string]any{}
		switch family {
		case "files":
			billing["resource_file"] = map[string]any{"purpose": purpose, "filename": "input.jsonl", "bytes": 64}
		case "batch":
			billing["resource_batch"] = map[string]any{"model": "gpt-4o", "input_file_id": inputFileID, "request_count": 2}
		}
		encodedBilling, err := json.Marshal(billing)
		require.NoError(t, err)
		resourceState := map[string]any{}
		if deleted {
			resourceState["deleted"] = true
		}
		encodedState, err := json.Marshal(resourceState)
		require.NoError(t, err)
		var chargedID, finishedAt any
		if status == "succeeded" {
			chargedID, finishedAt = int64(1), "NOW()"
		}
		_, err = db.ExecContext(ctx, `INSERT INTO upstream_jobs
 (public_request_id,request_fingerprint,submission_key,platform,family,account_id,user_id,api_key_id,group_id,upstream_task_id,status,
  next_poll_at,submitted_at,expires_at,billing_snapshot,resource_state,result_ref,charged_usage_log_id,finished_at)
 VALUES ($1,$2,$3,'openai',$4,$5,$6,$7,$8,$9,$10,NOW()+INTERVAL '24 hours',NOW(),NOW()+INTERVAL '24 hours',$11,$12,$13,$14,
  CASE WHEN $15::text='NOW()' THEN NOW() ELSE NULL END)`,
			publicID, "fp-"+publicID, "sk-"+publicID, family, account, user, key, group, "upstream-"+publicID, status,
			string(encodedBilling), string(encodedState), resultRef, chargedID, finishedAt)
		require.NoError(t, err)
	}
	insert(t, "file-batch-input", "files", "succeeded", "batch", "", "", false)
	insert(t, "file-assistants", "files", "succeeded", "assistants", "", "", false)
	insert(t, "file-deleted", "files", "succeeded", "batch", "", "", true)
	insert(t, "batch-live", "batch", "running", "", "file-batch-input", "", false)
	insert(t, "batch-done", "batch", "succeeded", "", "file-batch-input", "upstream-output", false)
	insert(t, "batch-output-deleted", "batch", "succeeded", "", "file-batch-input", "upstream-output-2", false)
	_, err = db.ExecContext(ctx, `UPDATE upstream_jobs SET resource_state = resource_state || '{"output_deleted":true}'::jsonb WHERE public_request_id=$1`, named("batch-output-deleted"))
	require.NoError(t, err)

	ids := func(jobs []*service.UpstreamJob) []string {
		out := make([]string, 0, len(jobs))
		for _, job := range jobs {
			out = append(out, job.PublicRequestID)
		}
		return out
	}

	files, err := concrete.ListGatewayResources(ctx, service.PlatformOpenAI, user, key, group, "files", "", "", 20)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{named("file-batch-input"), named("file-assistants")}, ids(files))

	batchInputs, err := concrete.ListGatewayResources(ctx, service.PlatformOpenAI, user, key, group, "files", "batch", "", 20)
	require.NoError(t, err)
	require.Equal(t, []string{named("file-batch-input")}, ids(batchInputs))

	batches, err := concrete.ListGatewayResources(ctx, service.PlatformOpenAI, user, key, group, "batch", "", "", 20)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{named("batch-live"), named("batch-done"), named("batch-output-deleted")}, ids(batches))

	outputs, err := concrete.ListGatewayResources(ctx, service.PlatformOpenAI, user, key, group, "batch_output", "", "", 20)
	require.NoError(t, err)
	require.Equal(t, []string{named("batch-done")}, ids(outputs))

	firstPage, err := concrete.ListGatewayResources(ctx, service.PlatformOpenAI, user, key, group, "batch", "", "", 1)
	require.NoError(t, err)
	require.Len(t, firstPage, 1)
	secondPage, err := concrete.ListGatewayResources(ctx, service.PlatformOpenAI, user, key, group, "batch", "", firstPage[0].PublicRequestID, 1)
	require.NoError(t, err)
	require.Len(t, secondPage, 1)
	require.NotEqual(t, firstPage[0].PublicRequestID, secondPage[0].PublicRequestID)

	foreign, err := concrete.ListGatewayResources(ctx, service.PlatformAnthropic, user, key, group, "batch", "", "", 20)
	require.NoError(t, err)
	require.Empty(t, foreign, "another platform never sees these resources")

	used, err := concrete.GatewayResourceFileInUse(ctx, named("file-batch-input"))
	require.NoError(t, err)
	require.True(t, used, "a live batch holds its input file")

	_, err = db.ExecContext(ctx, `UPDATE upstream_jobs SET status='succeeded', finished_at=NOW(), charged_usage_log_id=1 WHERE public_request_id=$1`, named("batch-live"))
	require.NoError(t, err)
	used, err = concrete.GatewayResourceFileInUse(ctx, named("file-batch-input"))
	require.NoError(t, err)
	require.False(t, used, "a finished batch releases its input file")
}
