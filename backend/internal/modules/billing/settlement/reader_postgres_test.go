//go:build integration

package settlement_test

import (
	"database/sql"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/billing/settlement"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/testutil/testresources"
	"github.com/stretchr/testify/require"
)

func TestBillingWalletPostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to a disposable job_test database")
	}
	ctx := t.Context()
	db, err := testresources.OpenPostgres(ctx, dsn, "job_test")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, repository.ApplyMigrations(ctx, db))
	id := func(query string, args ...any) int64 {
		t.Helper()
		var id int64
		require.NoError(t, db.QueryRowContext(ctx, query, args...).Scan(&id))
		return id
	}
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.ExecContext(ctx, query, args...)
		require.NoError(t, err)
	}
	wallet := func() int64 {
		return id(`INSERT INTO users(email,password_hash,balance) VALUES($1,'fixture',100) RETURNING id`, uuid.NewString()+"@example.test")
	}

	payer, newOwner := wallet(), wallet()
	workspace := id(`INSERT INTO workspaces(name) VALUES('BE-11') RETURNING id`)
	project := id(`SELECT id FROM projects WHERE workspace_id=$1 AND is_default`, workspace)
	key := id(`INSERT INTO api_keys(workspace_id,user_id,name) VALUES($1,$2,'BE-11') RETURNING id`, workspace, payer)
	account := id(`INSERT INTO accounts(name,platform,type) VALUES('BE-11','qianwen','apikey') RETURNING id`)
	requestID, attemptID := uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO gateway_executions
	    (request_id,workspace_id,project_id,actor_id,api_key_id,owner_id,funding_user_id,
	     authorization_version,kind,route,model,legacy_id,reservation_ref,attempt_budget,
	     deadline,model_version,price_version,pricing_at)
	    VALUES ($1,$2,$3,$4,$5,$4,$4,1,'request','/v1/responses','fixture','','',1,
	            now()-interval '1 second','catalog-1','price-1',now())`, requestID, workspace, project, payer, key)
	exec(`INSERT INTO gateway_execution_attempts
	    (attempt_id,request_id,ordinal,account_id,dispatched_at,acceptance,outcome)
	    VALUES ($1,$2,1,$3,now(),'accepted','transport_response')`, attemptID, requestID, account)

	reader := settlement.FinancialReader{DB: db}
	scope := settlement.Scope{WorkspaceID: workspace, ProjectID: project}
	missing, err := reader.Read(ctx, scope, requestID, attemptID)
	require.NoError(t, err)
	require.Equal(t, "unconfirmed", missing.ReceiptState)
	require.Equal(t, "unknown", missing.State)
	require.Nil(t, missing.PlatformCharge)
	require.Nil(t, missing.InputTokens)
	require.NotNil(t, missing.ReviewReason)
	require.Equal(t, "missing_receipt", *missing.ReviewReason)
	_, err = reader.Read(ctx, settlement.Scope{WorkspaceID: workspace, ProjectID: project + 1}, requestID, attemptID)
	require.ErrorIs(t, err, sql.ErrNoRows)

	exec(`UPDATE api_keys SET user_id=$1 WHERE id=$2`, newOwner, key)
	const amount = 0.00000001
	receiptKey := service.UsageSettlementKey{RequestID: requestID, APIKeyID: key, UserID: payer}
	first := repository.NewUsageSettlementReceiptRepository(db)
	receipt, err := first.Persist(ctx, &service.UsageSettlementReceipt{
		Key: receiptKey,
		Command: service.UsageBillingCommand{
			RequestID: requestID, APIKeyID: key, UserID: payer, AccountID: account, Model: "fixture",
			RequestFingerprint: "original-fingerprint", BalanceCost: amount,
		},
		Usage: service.UsageLog{
			RequestID: requestID, APIKeyID: key, UserID: payer, AccountID: account, Model: "fixture",
			RateMultiplier: 1, ActualCost: amount, TotalCost: amount, PricingState: service.PricingStatePriced,
			PricingSnapshot:       `{"pricing_state":"priced","model":"fixture","provider":"qianwen","mode":"image","source":"configured","unit_price":0.00000001,"units":1,"rate_multiplier":1,"time_multiplier":1,"reasoning_multiplier":1,"total_cost":0.00000001,"actual_cost":0.00000001,"charged_amount":0.00000001}`,
			TokenUsageObservation: &service.TokenUsageObservation{InputTokensKnown: true},
		},
		ResolutionState: "resolved",
		CompletionState: "complete",
	})
	require.NoError(t, err)

	second := repository.NewUsageSettlementReceiptRepository(db)
	type outcome struct {
		applied bool
		err     error
	}
	results := make(chan outcome, 12)
	var workers sync.WaitGroup
	for i := range cap(results) {
		worker := first
		if i%2 == 1 {
			worker = second
		}
		workers.Go(func() {
			result, err := worker.Settle(ctx, receiptKey, receipt.Command.RequestFingerprint)
			results <- outcome{applied: result != nil && result.Billing.Applied, err: err}
		})
	}
	workers.Wait()
	close(results)
	applied := 0
	for result := range results {
		require.NoError(t, result.err)
		if result.applied {
			applied++
		}
	}
	require.Equal(t, 1, applied)
	require.EqualValues(t, 1, id(`SELECT count(*) FROM billing_settlement_outbox WHERE receipt_id=$1`, receipt.ID))

	evidence, err := reader.Read(ctx, scope, requestID, attemptID)
	require.NoError(t, err)
	require.Equal(t, missing.FundingAccountID, evidence.FundingAccountID)
	require.Equal(t, "settled", evidence.ReceiptState)
	require.Equal(t, "price-1", evidence.PriceVersion)
	require.NotNil(t, evidence.PlatformCharge)
	require.Equal(t, "0.00000001", evidence.PlatformCharge.String())
	require.NotNil(t, evidence.InputTokens)
	require.Zero(t, *evidence.InputTokens)
	require.Nil(t, evidence.OutputTokens)
	require.Len(t, evidence.LedgerEventIDs, 1)

	var balance, walletSum, allPostings, newOwnerBalance string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT balance::text,
	    (SELECT sum(signed_amount)::text FROM billing_wallet_postings WHERE funding_account_id=$2 AND currency='USD' AND side='wallet'),
	    (SELECT sum(signed_amount)::text FROM billing_wallet_postings WHERE funding_account_id=$2 AND currency='USD'),
	    (SELECT balance::text FROM users WHERE id=$3)
	    FROM users WHERE id=$1`, payer, evidence.FundingAccountID, newOwner).Scan(&balance, &walletSum, &allPostings, &newOwnerBalance))
	require.Equal(t, "99.99999999", balance)
	require.Equal(t, balance, walletSum)
	require.Equal(t, "0.00000000", allPostings)
	require.Equal(t, "100.00000000", newOwnerBalance)
}
