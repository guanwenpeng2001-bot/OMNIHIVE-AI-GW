//go:build unit

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
	"time"
)

func TestUserPendingSettlementPostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("requires assigned isolated job_test database")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	var name string
	require.NoError(t, db.QueryRow("SELECT current_database()").Scan(&name))
	require.Equal(t, "job_test", name)
	ctx := context.Background()
	repo := &usageSettlementReceiptRepository{db: db}
	var userID, keyID, accountID int64
	require.NoError(t, db.QueryRow(`INSERT INTO users(email,password_hash) VALUES($1,'test') RETURNING id`, uuid.NewString()+"@pending.test").Scan(&userID))
	require.NoError(t, db.QueryRow(`INSERT INTO api_keys(user_id,key,name) VALUES($1,$2,'pending test') RETURNING id`, userID, uuid.NewString()).Scan(&keyID))
	require.NoError(t, db.QueryRow(`INSERT INTO accounts(name,platform,type) VALUES('pending test','qianwen','apikey') RETURNING id`).Scan(&accountID))
	t.Cleanup(func() {
		for _, query := range []string{`DELETE FROM usage_settlement_receipts WHERE user_id=$1`, `DELETE FROM upstream_jobs WHERE user_id=$1`, `DELETE FROM api_keys WHERE user_id=$1`, `DELETE FROM users WHERE id=$1`} {
			_, err := db.Exec(query, userID)
			require.NoError(t, err)
		}
		_, err := db.Exec(`DELETE FROM accounts WHERE id=$1`, accountID)
		require.NoError(t, err)
	})
	created := time.Now().UTC().Truncate(time.Second)
	insertReceipt := func(model string, linked bool) int64 {
		usage, _ := json.Marshal(service.UsageLog{RequestedModel: model, Model: "legacy", InputTokens: 12, OutputTokens: 4})
		command, _ := json.Marshal(service.UsageBillingCommand{AccountID: accountID, RequestFingerprint: "fixture"})
		var id int64
		var anchor *int64
		if linked {
			n := int64(123)
			anchor = &n
		}
		require.NoError(t, db.QueryRow(`INSERT INTO usage_settlement_receipts(request_id,api_key_id,user_id,account_id,request_fingerprint,usage_json,billing_command,pricing_state,pricing_snapshot,resolution_state,completion_state,state,usage_log_id,created_at) VALUES($1,$2,$3,$4,'fixture',$5,$6,'unpriced','','unresolved','complete','needs_review',$7,$8) RETURNING id`, uuid.NewString(), keyID, userID, accountID, string(usage), string(command), anchor, created).Scan(&id))
		return id
	}
	first := insertReceipt("public", false)
	second := insertReceipt("public", false)
	insertReceipt("public", true)
	insertReceipt("different", false)
	start, end := created.Add(-time.Second), created.Add(time.Second)
	filter := usagestats.UsageLogFilters{Model: "public", APIKeyID: keyID, StartTime: &start, EndTime: &end}
	page, err := repo.ListUserPendingSettlement(ctx, userID, filter, service.UserPendingSettlementCursor{Limit: 1})
	require.NoError(t, err)
	require.Equal(t, int64(2), page.Total)
	require.Len(t, page.Items, 1)
	require.Equal(t, second, page.Items[0].ID)
	require.Equal(t, second, page.NextCursor)
	page, err = repo.ListUserPendingSettlement(ctx, userID, filter, service.UserPendingSettlementCursor{BeforeID: page.NextCursor, Limit: 1})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, first, page.Items[0].ID)
	require.Zero(t, page.NextCursor)
	page, err = repo.ListUserPendingSettlement(ctx, userID+100000, filter, service.UserPendingSettlementCursor{})
	require.NoError(t, err)
	require.Empty(t, page.Items)
	filter.Model = "legacy"
	page, err = repo.ListUserPendingSettlement(ctx, userID, filter, service.UserPendingSettlementCursor{})
	require.NoError(t, err)
	require.Empty(t, page.Items)
	filter.Model = "public"
	filter.EndTime = &created
	page, err = repo.ListUserPendingSettlement(ctx, userID, filter, service.UserPendingSettlementCursor{})
	require.NoError(t, err)
	require.Empty(t, page.Items)
	filter.EndTime = &end
	filter.APIKeyID = keyID + 100000
	page, err = repo.ListUserPendingSettlement(ctx, userID, filter, service.UserPendingSettlementCursor{})
	require.NoError(t, err)
	require.Empty(t, page.Items)
	filter.APIKeyID = keyID
	insertJob := func(result, family string, linked bool) int64 {
		billing := `{"requested_model":"public","model":"wire","pricing_state":"unpriced"}`
		status := "failed"
		var anchor *int64
		if linked {
			n := int64(123)
			anchor = &n
			status = "succeeded"
		}
		var id int64
		identity := uuid.NewString()
		require.NoError(t, db.QueryRow(`INSERT INTO upstream_jobs(public_request_id,request_fingerprint,submission_key,platform,family,account_id,user_id,api_key_id,expires_at,billing_snapshot,status,finished_at,error_code,result_ref,usage_json,charged_usage_log_id,submitted_at) VALUES($1,$1,$1,'qianwen',$2,$3,$4,$5,$6,$7,$8,$6,'settlement_needs_review',$9,'{"input_tokens":12,"output_tokens":4}',$10,$11) RETURNING id`, identity, family, accountID, userID, keyID, created.Add(time.Hour), billing, status, result, anchor, created).Scan(&id))
		return id
	}
	jobFirst := insertJob("completed", "realtime_turn", false)
	jobSecond := insertJob("incomplete", "realtime_turn", false)
	_, err = db.Exec(`UPDATE upstream_jobs SET billing_snapshot=jsonb_set(billing_snapshot,'{realtime}','{"upstream_response_model":"declared"}'::jsonb) WHERE id=$1`, jobSecond)
	require.NoError(t, err)
	insertJob("", "realtime_turn", false)
	insertJob("completed", "realtime_session", false)
	insertJob("completed", "realtime_turn", true)
	cursor := service.UserPendingSettlementCursor{Source: "upstream_job", Limit: 1}
	page, err = repo.ListUserPendingSettlement(ctx, userID, filter, cursor)
	require.NoError(t, err)
	require.Equal(t, int64(2), page.Total)
	require.Equal(t, jobSecond, page.Items[0].ID)
	require.Equal(t, "incomplete", page.Items[0].CompletionStatus)
	require.Equal(t, "declared", *page.Items[0].Usage.UpstreamResponseModel)
	cursor.BeforeID = page.NextCursor
	page, err = repo.ListUserPendingSettlement(ctx, userID, filter, cursor)
	require.NoError(t, err)
	require.Equal(t, jobFirst, page.Items[0].ID)
	require.Nil(t, page.Items[0].Usage.UpstreamResponseModel)
	require.Zero(t, page.NextCursor)
	page, err = repo.ListUserPendingSettlement(ctx, userID+100000, filter, cursor)
	require.NoError(t, err)
	require.Empty(t, page.Items)
	cancelled := insertJob("cancelled", "realtime_turn", false)
	cursor.BeforeID = 0
	page, err = repo.ListUserPendingSettlement(ctx, userID, filter, cursor)
	require.NoError(t, err)
	require.Equal(t, cancelled, page.Items[0].ID)
	require.Equal(t, "cancelled", page.Items[0].CompletionStatus)
	_, err = db.Exec(`UPDATE upstream_jobs SET usage_json='{}' WHERE id=$1`, cancelled)
	require.NoError(t, err)
	page, err = repo.ListUserPendingSettlement(ctx, userID, filter, cursor)
	require.NoError(t, err)
	require.Equal(t, int64(2), page.Total)
	cursor.BeforeID = 0
	filter.APIKeyID = keyID + 100000
	page, err = repo.ListUserPendingSettlement(ctx, userID, filter, cursor)
	require.NoError(t, err)
	require.Empty(t, page.Items)
	for range 101 {
		insertReceipt("limit-model", false)
	}
	page, err = repo.ListUserPendingSettlement(ctx, userID, usagestats.UsageLogFilters{Model: "limit-model"}, service.UserPendingSettlementCursor{Limit: 999})
	require.NoError(t, err)
	require.Len(t, page.Items, 100)
	require.NotZero(t, page.NextCursor)
}
