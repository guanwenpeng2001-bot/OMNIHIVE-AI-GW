//go:build integration

package repository

import (
	"os"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

func TestOpsLegacyIDDrillsIntoCanonicalExecution(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	db, err := testresources.OpenPostgres(t.Context(), dsn, "job_test")
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, ApplyMigrations(t.Context(), db))
	var workspace, user, key int64
	require.NoError(t, db.QueryRow("INSERT INTO workspaces(name) VALUES('ops drilldown') RETURNING id").Scan(&workspace))
	require.NoError(t, db.QueryRow("INSERT INTO users(email,password_hash) VALUES('ops-drilldown@example.test','fixture') RETURNING id").Scan(&user))
	require.NoError(t, db.QueryRow("INSERT INTO api_keys(workspace_id,user_id,name) VALUES($1,$2,'ops drilldown') RETURNING id", workspace, user).Scan(&key))
	legacy := journal.NewID()
	execution := journal.NewID()
	_, err = db.Exec(`INSERT INTO gateway_executions(request_id,workspace_id,project_id,actor_id,api_key_id,owner_id,funding_user_id,authorization_version,kind,route,model,legacy_id,attempt_budget,deadline,finished_at)
 SELECT $1,workspace_id,identity_default_project(workspace_id),$2,$3,$2,$2,1,'request','/v1/chat/completions','gpt-mock',$4,1,now()+interval '1 minute',now() FROM workspaces WHERE id=$5`, execution, user, key, legacy, workspace)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO ops_error_logs(request_id,api_key_id,user_id,account_id,status_code,upstream_status_code,error_message,created_at)
 VALUES($1,$2,$3,1,502,500,'upstream failed',now())`, legacy, key, user)
	require.NoError(t, err)
	start, end := time.Now().Add(-time.Minute), time.Now().Add(time.Minute)
	items, _, err := NewOpsRepository(db).ListRequestDetails(t.Context(), &service.OpsRequestDetailFilter{Kind: "error", RequestID: legacy, StartTime: &start, EndTime: &end})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, execution, items[0].RequestID)
	require.Contains(t, items[0].DetailURL, "/requests/"+execution)
}
