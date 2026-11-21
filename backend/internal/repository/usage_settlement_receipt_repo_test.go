//go:build unit

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/execution/journal"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/billing/readmodel"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestUsageSettlementReceiptPostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_JOB_TEST_DSN")
	if dsn == "" {
		t.Skip("set UPSTREAM_JOB_TEST_DSN to an isolated job_test PostgreSQL database")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	db.SetMaxOpenConns(32)
	var database string
	require.NoError(t, db.QueryRow(`SELECT current_database()`).Scan(&database))
	require.Equal(t, "job_test", database, "refusing to modify any non-test database")
	ctx := context.Background()
	require.NoError(t, ApplyMigrations(ctx, db))
	_, err = db.Exec(`CREATE TABLE receipt_test_effects (usage_log_id BIGINT PRIMARY KEY, request_id TEXT NOT NULL)`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := db.Exec(`DROP TABLE receipt_test_effects`)
		require.NoError(t, err)
	})
	effect := func(ctx context.Context, tx *sql.Tx, usage *service.UsageLog) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO receipt_test_effects(usage_log_id,request_id) VALUES($1,$2)`, usage.ID, usage.RequestID)
		return err
	}
	repo := NewUsageSettlementReceiptRepository(db, effect)
	fixture := func(t *testing.T, amount float64) *service.UsageSettlementReceipt {
		t.Helper()
		var userID, keyID, accountID int64
		require.NoError(t, db.QueryRow(`INSERT INTO users(email,password_hash,balance) VALUES($1,'test',100) RETURNING id`, uuid.NewString()+"@example.test").Scan(&userID))
		require.NoError(t, db.QueryRow(`INSERT INTO api_keys(user_id,key,name) VALUES($1,$2,'receipt') RETURNING id`, userID, uuid.NewString()).Scan(&keyID))
		require.NoError(t, db.QueryRow(`INSERT INTO accounts(name,platform,type) VALUES('receipt','qianwen','apikey') RETURNING id`).Scan(&accountID))
		state := service.PricingStatePriced
		if amount == 0 {
			state = service.PricingStateFree
		}
		snapshot, err := json.Marshal(service.PricingSnapshot{PricingState: state, Model: "receipt-model", Provider: "qianwen", Mode: "image", Source: "configured", UnitPrice: &amount, Units: 1, RateMultiplier: 1, TimeMultiplier: 1, ReasoningMultiplier: 1, TotalCost: amount, ActualCost: amount, ChargedAmount: service.QuantizeUsageBillingAmount(amount)})
		require.NoError(t, err)
		requestID := uuid.NewString()
		return &service.UsageSettlementReceipt{
			Key:             service.UsageSettlementKey{RequestID: requestID, UserID: userID, APIKeyID: keyID},
			Command:         service.UsageBillingCommand{RequestID: requestID, APIKeyID: keyID, UserID: userID, AccountID: accountID, AccountType: service.AccountTypeAPIKey, Model: "receipt-model", BalanceCost: amount, APIKeyQuotaCost: amount, APIKeyRateLimitCost: amount},
			Usage:           service.UsageLog{RequestID: requestID, UserID: userID, APIKeyID: keyID, AccountID: accountID, Model: "receipt-model", TotalCost: amount, ActualCost: amount, RateMultiplier: 1, PricingState: state, PricingSnapshot: string(snapshot)},
			ResolutionState: "resolved", CompletionState: "complete",
		}
	}

	t.Run("pi_reference_then_operator_price_wallet_effect", func(t *testing.T) {
		r := fixture(t, 0)
		cfg := &config.Config{}
		cfg.Default.RateMultiplier = 1
		billing := service.NewBillingService(cfg, service.NewPricingService(cfg, nil))
		gateway := service.ProvideOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, billing, nil, nil, nil, nil, nil, nil,
			service.NewModelPricingResolver(nil, billing), nil, nil, nil, nil, service.NewUsageSettlementService(repo, nil, nil))
		group := &service.Group{Platform: service.PlatformOpenAI, RateMultiplier: 1}
		require.NoError(t, db.QueryRow(`INSERT INTO groups(name,platform) VALUES($1,'openai') RETURNING id`, uuid.NewString()).Scan(&group.ID))
		const model = "gemini-1.5-flash"
		input := &service.OpenAIRecordUsageInput{
			Result: &service.OpenAIForwardResult{RequestID: uuid.NewString(), Model: model, Usage: service.OpenAIUsage{InputTokens: 100, OutputTokens: 20}},
			User:   &service.User{ID: r.Key.UserID}, APIKey: &service.APIKey{ID: r.Key.APIKeyID, UserID: r.Key.UserID, GroupID: &group.ID, Group: group},
			Account: &service.Account{ID: r.Command.AccountID, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey},
		}
		require.ErrorIs(t, gateway.RecordUsage(ctx, input), service.ErrUsageSettlementNeedsReview)
		var correct bool
		require.NoError(t, db.QueryRow(`SELECT balance=100 AND EXISTS(SELECT 1 FROM usage_settlement_receipts WHERE api_key_id=$2 AND state='needs_review') FROM users WHERE id=$1`, r.Key.UserID, r.Key.APIKeyID).Scan(&correct))
		require.True(t, correct)
		var card []byte
		require.NoError(t, db.QueryRow(`UPDATE groups SET model_pricing=$1::jsonb WHERE id=$2 RETURNING model_pricing`, `[{"models":["gemini-1.5-flash"],"billing_mode":"token","input_price":0.000002,"output_price":0.000004}]`, group.ID).Scan(&card))
		require.NoError(t, json.Unmarshal(card, &group.ModelPricing))
		input.Result.RequestID = uuid.NewString()
		require.NoError(t, gateway.RecordUsage(ctx, input))
		require.NoError(t, db.QueryRow(`SELECT balance=99.99972 AND EXISTS(SELECT 1 FROM usage_settlement_receipts WHERE api_key_id=$2 AND state='settled') FROM users WHERE id=$1`, r.Key.UserID, r.Key.APIKeyID).Scan(&correct))
		require.True(t, correct)
	})

	holdFor := func(t *testing.T, r *service.UsageSettlementReceipt, now time.Time) *service.KeyBudgetReservation {
		t.Helper()
		_, err := db.Exec(`UPDATE api_keys SET budget_period_mode='duration',budget_period_seconds=60,budget_limit=10 WHERE id=$1`, r.Key.APIKeyID)
		require.NoError(t, err)
		hold, err := repo.(service.KeyBudgetReservationRepository).ReserveKeyBudget(ctx, &service.KeyBudgetReservationCommand{APIKeyID: r.Key.APIKeyID, UserID: r.Key.UserID, RequestID: uuid.NewString(), HoldAmount: 3, Now: now, TTL: time.Minute})
		require.NoError(t, err)
		require.NotNil(t, hold.Reservation)
		r.Command.BudgetReservationID = hold.Reservation.ID
		return hold.Reservation
	}
	t.Run("pending_receipt_retains_expired_hold_across_period_boundary", func(t *testing.T) {
		r := fixture(t, 2)
		h := holdFor(t, r, time.Now().UTC().Add(-time.Hour))
		stored, err := repo.Persist(ctx, r)
		require.NoError(t, err)
		budgets := repo.(service.KeyBudgetReservationRepository)
		next, err := budgets.ReserveKeyBudget(ctx, &service.KeyBudgetReservationCommand{APIKeyID: r.Key.APIKeyID, UserID: r.Key.UserID, RequestID: uuid.NewString(), HoldAmount: 1})
		require.NoError(t, err)
		require.True(t, next.Reservation.PeriodStart.After(h.PeriodStart))
		for range 2 {
			_, err = budgets.ReleaseExpiredKeyBudgetReservations(ctx, time.Now().UTC(), 500)
			require.NoError(t, err)
			var occupied bool
			require.NoError(t, db.QueryRow(`SELECT h.status='reserved' AND h.expires_at<now() AND u.balance=96 FROM api_key_budget_reservations h JOIN users u ON u.id=h.user_id WHERE h.id=$1`, h.ID).Scan(&occupied))
			require.True(t, occupied)
		}
		_, err = repo.Settle(ctx, r.Key, stored.Command.RequestFingerprint)
		require.NoError(t, err)
		var correct bool
		require.NoError(t, db.QueryRow(`SELECT h.status='settled' AND p.used_amount=2 AND u.balance=97 FROM api_key_budget_reservations h JOIN api_key_budget_periods p USING(api_key_id,period_start) JOIN users u ON u.id=h.user_id WHERE h.id=$1`, h.ID).Scan(&correct))
		require.True(t, correct)
	})
	t.Run("active_dispatch_retains_expired_hold_before_receipt", func(t *testing.T) {
		r := fixture(t, 2)
		h := holdFor(t, r, time.Now().UTC().Add(-time.Hour))
		id := uuid.NewString()
		_, err := db.Exec(`INSERT INTO gateway_executions(request_id,workspace_id,project_id,actor_id,api_key_id,owner_id,funding_user_id,authorization_version,kind,route,model,legacy_id,reservation_ref,attempt_budget,deadline,model_version,price_version,pricing_at)
    SELECT $1,k.workspace_id,identity_default_project(k.workspace_id),$2,k.id,$2,$2,1,'request','/messages','test','',$4,1,now()-interval '1 hour','1','1',now() FROM api_keys k WHERE k.id=$3`, id, r.Key.UserID, r.Key.APIKeyID, h.RequestID)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO gateway_execution_attempts(attempt_id,request_id,ordinal,account_id,dispatched_at,acceptance,outcome) VALUES($1,$2,1,$3,now(),'unknown','dispatching')`, uuid.NewString(), id, r.Command.AccountID)
		require.NoError(t, err)
		_, err = repo.(service.KeyBudgetReservationRepository).ReleaseExpiredKeyBudgetReservations(ctx, time.Now(), 500)
		require.NoError(t, err)
		var kept bool
		require.NoError(t, db.QueryRow(`SELECT status='reserved' FROM api_key_budget_reservations WHERE id=$1`, h.ID).Scan(&kept))
		require.True(t, kept)
	})
	t.Run("disconnect_after_dispatch_keeps_wallet_for_late_settlement", func(t *testing.T) {
		r := fixture(t, 2)
		_, err := db.Exec(`UPDATE users SET balance=3 WHERE id=$1`, r.Key.UserID)
		require.NoError(t, err)
		_, err = db.Exec(`UPDATE api_keys SET budget_period_mode='duration',budget_period_seconds=3600,budget_limit=10 WHERE id=$1`, r.Key.APIKeyID)
		require.NoError(t, err)
		owner := scope.Authorized{ActorID: r.Key.UserID, KeyOwnerID: r.Key.UserID, FundingUserID: r.Key.UserID, KeyID: r.Key.APIKeyID, Version: 1}
		require.NoError(t, db.QueryRow(`SELECT workspace_id,identity_default_project(workspace_id) FROM api_keys WHERE id=$1`, r.Key.APIKeyID).Scan(&owner.WorkspaceID, &owner.ProjectID))
		request, disconnect := context.WithCancel(scope.With(ctx, owner))
		defer disconnect()
		request, finishBudget := service.WithAttemptBudget(request, 1, time.Minute)
		defer finishBudget()
		request = journal.WithReauthorization(journal.WithStore(request, journal.New(db)), func(context.Context, scope.Authorized, string) error { return nil })
		request = service.WithProjectBudgetCoordinator(request, readmodel.Store{DB: db})
		_, err = db.Exec(`INSERT INTO billing_project_budgets VALUES($1,$2,'USD','hard',10,'calendar_month','UTC',false,1) ON CONFLICT DO NOTHING`, owner.WorkspaceID, owner.ProjectID)
		require.NoError(t, err)
		cfg := &config.Config{}
		settlement := service.NewUsageSettlementService(repo, nil, nil)
		gateway := service.ProvideOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, settlement)
		request, plan := gateway.CaptureRequestPlan(request, service.GatewayRequestSpec{WorkspaceID: owner.WorkspaceID, UserID: r.Key.UserID, APIKeyID: r.Key.APIKeyID, Model: r.Command.Model, Route: "/v1/audio/translations", Protocol: "audio"})
		execution, err := plan.AdmittedExecution()
		require.NoError(t, err)
		perHour := 10800.0
		require.NoError(t, plan.ReserveBudget(request, service.RequestBudgetEstimate{Model: r.Command.Model, Group: &service.Group{AudioSTTPricePerHour: &perHour, RateMultiplier: 1}, RateMultiplier: 1, Meter: &service.BudgetMeter{Unit: "second", Count: 1, Numerator: 1, Denominator: 1}}))
		hold := plan.BudgetHold()
		require.NotNil(t, hold)
		require.Equal(t, 3.0, hold.HoldAmount)
		finishAttempt, err := service.BeginGenerationAttempt(request, r.Command.AccountID)
		require.NoError(t, err)
		ended := make(chan struct{})
		context.AfterFunc(request, func() { plan.Cancel(); close(ended) })
		disconnect()
		<-ended
		budgets := repo.(service.KeyBudgetReservationRepository)
		competing := make(chan error, 1)
		go func() {
			_, err := budgets.ReserveKeyBudget(ctx, &service.KeyBudgetReservationCommand{UserID: r.Key.UserID, APIKeyID: r.Key.APIKeyID, RequestID: uuid.NewString(), HoldAmount: 3})
			competing <- err
		}()
		finishAttempt(200, nil)
		require.ErrorIs(t, <-competing, service.ErrAPIKeyBudgetInsufficientBalance)
		plan.Finalize(false)
		var correct bool
		reclaimed, err := budgets.ReleaseExpiredKeyBudgetReservations(ctx, time.Now().Add(time.Hour), 100)
		require.NoError(t, err)
		var retained bool
		require.NoError(t, db.QueryRow(`SELECT status='reserved' FROM api_key_budget_reservations WHERE id=$1`, hold.ID).Scan(&retained))
		require.True(t, retained, "expiry sweep reclaimed the dispatched hold (other orphan holds reclaimed=%d)", reclaimed)
		require.NoError(t, db.QueryRow(`SELECT h.status='reserved' AND u.balance=0 AND k.budget_reserved=3 AND e.canceled AND p.released_at IS NULL AND NOT EXISTS(SELECT 1 FROM usage_settlement_receipts WHERE request_id=h.request_id)
FROM api_key_budget_reservations h JOIN users u ON u.id=h.user_id JOIN api_keys k ON k.id=h.api_key_id JOIN gateway_executions e ON e.reservation_ref=h.request_id JOIN billing_project_holds p ON p.request_id=e.request_id::text WHERE h.id=$1`, hold.ID).Scan(&correct))
		require.True(t, correct)
		r.Key.RequestID, r.Command.RequestID, r.Usage.RequestID = execution.RequestID, execution.RequestID, execution.RequestID
		require.NoError(t, settlement.Submit(context.WithoutCancel(request), r, nil))
		require.NoError(t, db.QueryRow(`SELECT h.status='settled' AND u.balance=1 AND k.budget_reserved=0 AND k.budget_used=2 AND k.budget_overrun=0 AND r.state='settled' AND p.released_at IS NOT NULL
FROM api_key_budget_reservations h JOIN users u ON u.id=h.user_id JOIN api_keys k ON k.id=h.api_key_id JOIN usage_settlement_receipts r ON r.request_id=h.request_id JOIN billing_project_holds p ON p.request_id=h.request_id WHERE h.id=$1`, hold.ID).Scan(&correct))
		require.True(t, correct)
	})
	t.Run("review_conflict_releases_key_and_project_exactly_once", func(t *testing.T) {
		r := fixture(t, 2)
		h := holdFor(t, r, time.Now())
		ref := uuid.NewString()
		r.Command.ProjectReservationRef = ref
		require.NoError(t, db.QueryRow(`SELECT workspace_id,identity_default_project(workspace_id) FROM api_keys WHERE id=$1`, r.Key.APIKeyID).Scan(&r.Command.ProjectWorkspaceID, &r.Command.ProjectID))
		_, err := db.Exec(`INSERT INTO billing_project_budgets VALUES($1,$2,'USD','hard',10,'calendar_month','UTC',false,1) ON CONFLICT DO NOTHING`, r.Command.ProjectWorkspaceID, r.Command.ProjectID)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO billing_project_holds VALUES($1,$2,'USD',$3,3,now(),NULL)`, r.Command.ProjectWorkspaceID, r.Command.ProjectID, ref)
		require.NoError(t, err)
		stored, err := repo.Persist(ctx, r)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO usage_billing_dedup_archive(request_id,api_key_id,request_fingerprint,created_at) VALUES($1,$2,$3,now())`, r.Key.RequestID, r.Key.APIKeyID, stored.Command.RequestFingerprint)
		require.NoError(t, err)
		// A blocked project release forces the entire review transaction to
		// roll back, including the already attempted key balance refund.
		blocker, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer func() { _ = blocker.Rollback() }()
		_, err = blocker.ExecContext(ctx, `SELECT request_id FROM billing_project_holds WHERE workspace_id=$1 AND project_id=$2 AND request_id=$3 FOR UPDATE`, r.Command.ProjectWorkspaceID, r.Command.ProjectID, ref)
		require.NoError(t, err)
		timed, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		_, err = repo.Settle(timed, r.Key, stored.Command.RequestFingerprint)
		cancel()
		require.Error(t, err)
		require.NoError(t, blocker.Rollback())
		var intact bool
		require.NoError(t, db.QueryRow(`SELECT r.state='pending' AND u.balance=97 AND h.status='reserved' FROM usage_settlement_receipts r JOIN users u ON u.id=r.user_id JOIN api_key_budget_reservations h ON h.id=$2 WHERE r.id=$1`, stored.ID, h.ID).Scan(&intact))
		require.True(t, intact)
		for range 2 {
			_, err = repo.Settle(ctx, r.Key, stored.Command.RequestFingerprint)
			require.ErrorIs(t, err, service.ErrUsageSettlementNeedsReview)
			var correct bool
			require.NoError(t, db.QueryRow(`SELECT r.state='needs_review' AND u.balance=100 AND k.budget_reserved=0 AND k.budget_used=0 AND k.quota_used=0
    AND h.status='released' AND p.released_at IS NOT NULL AND NOT EXISTS(SELECT 1 FROM usage_logs WHERE request_id=r.request_id AND api_key_id=k.id)
    FROM usage_settlement_receipts r JOIN users u ON u.id=r.user_id JOIN api_keys k ON k.id=r.api_key_id JOIN api_key_budget_reservations h ON h.id=$2
    JOIN billing_project_holds p ON p.request_id=$3 WHERE r.id=$1`, stored.ID, h.ID, ref).Scan(&correct))
			require.True(t, correct)
		}
	})
	t.Run("legacy_review_ref_releases_original_project", func(t *testing.T) {
		r := fixture(t, 0)
		r.Command.ProjectReservationRef = uuid.NewString()
		r.Usage.PricingState = service.PricingStateUnpriced
		var workspace, project int64
		require.NoError(t, db.QueryRow(`INSERT INTO projects(workspace_id,name) SELECT workspace_id,'legacy held project' FROM api_keys WHERE id=$1 RETURNING workspace_id,id`, r.Key.APIKeyID).Scan(&workspace, &project))
		_, err := db.Exec(`INSERT INTO billing_project_budgets VALUES($1,$2,'USD','hard',10,'calendar_month','UTC',false,1)`, workspace, project)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO billing_project_holds VALUES($1,$2,'USD',$3,1,now(),NULL)`, workspace, project, r.Command.ProjectReservationRef)
		require.NoError(t, err)
		stored, err := repo.Persist(ctx, r)
		require.NoError(t, err)
		require.Equal(t, service.UsageSettlementNeedsReview, stored.State)
		var released bool
		require.NoError(t, db.QueryRow(`SELECT released_at IS NOT NULL FROM billing_project_holds WHERE workspace_id=$1 AND project_id=$2 AND request_id=$3`, workspace, project, r.Command.ProjectReservationRef).Scan(&released))
		require.True(t, released)
	})
	t.Run("project_hard_budget_distinguishes_unpriced_from_explicit_free", func(t *testing.T) {
		var owner scope.Authorized
		require.NoError(t, db.QueryRow(`INSERT INTO projects(workspace_id,name) SELECT id,'hard-budget-'||$1::text FROM workspaces WHERE is_default RETURNING workspace_id,id`, uuid.NewString()).Scan(&owner.WorkspaceID, &owner.ProjectID))
		_, err := db.Exec(`INSERT INTO billing_project_budgets VALUES($1,$2,'USD','hard',0,'calendar_month','UTC',false,1)`, owner.WorkspaceID, owner.ProjectID)
		require.NoError(t, err)
		store := readmodel.Store{DB: db}
		held, err := store.ReserveProjectBudget(ctx, owner, "unknown", nil)
		require.Error(t, err)
		require.False(t, held)
		free, err := money.NewMoney("USD", "0")
		require.NoError(t, err)
		held, err = store.ReserveProjectBudget(ctx, owner, "free", &free)
		require.NoError(t, err)
		require.True(t, held)
		var onlyFree bool
		require.NoError(t, db.QueryRow(`SELECT count(*)=1 AND bool_and(request_id='free' AND amount=0 AND released_at IS NULL) FROM billing_project_holds WHERE workspace_id=$1 AND project_id=$2`, owner.WorkspaceID, owner.ProjectID).Scan(&onlyFree))
		require.True(t, onlyFree)
	})
	t.Run("large_exact_key_hold_enforces_last_ledger_unit", func(t *testing.T) {
		r := fixture(t, 0)
		_, err := db.Exec(`UPDATE api_keys SET budget_period_mode='duration',budget_period_seconds=60,budget_limit=987654321.12345678 WHERE id=$1`, r.Key.APIKeyID)
		require.NoError(t, err)
		_, err = db.Exec(`UPDATE users SET balance=1000000000 WHERE id=$1`, r.Key.UserID)
		require.NoError(t, err)
		budget := repo.(*usageSettlementReceiptRepository)
		cmd := service.KeyBudgetReservationCommand{UserID: r.Key.UserID, APIKeyID: r.Key.APIKeyID, RequestID: uuid.NewString()}
		above, err := money.NewMoney("USD", "987654321.12345679")
		require.NoError(t, err)
		_, err = budget.ReserveKeyBudgetAmount(ctx, &cmd, above)
		require.ErrorIs(t, err, service.ErrAPIKeyBudgetExceeded)
		limit, err := money.NewMoney("USD", "987654321.12345678")
		require.NoError(t, err)
		hold, err := budget.ReserveKeyBudgetAmount(ctx, &cmd, limit)
		require.NoError(t, err)
		var exact bool
		require.NoError(t, db.QueryRow(`SELECT h.hold_amount=k.budget_limit AND k.budget_reserved=k.budget_limit AND 1000000000-u.balance=k.budget_limit FROM api_key_budget_reservations h JOIN users u ON u.id=h.user_id JOIN api_keys k ON k.id=h.api_key_id WHERE h.id=$1`, hold.Reservation.ID).Scan(&exact))
		require.True(t, exact)
		require.NoError(t, budget.ReleaseKeyBudgetReservation(ctx, r.Key.APIKeyID, cmd.RequestID))
		require.NoError(t, db.QueryRow(`SELECT u.balance=1000000000 AND k.budget_reserved=0 FROM users u JOIN api_keys k ON k.user_id=u.id WHERE k.id=$1`, r.Key.APIKeyID).Scan(&exact))
		require.True(t, exact)
	})
	t.Run("frozen_decimal_matches_wallet_and_key_effects_to_last_digit", func(t *testing.T) {
		r := fixture(t, 987654321.1234568)
		r.Usage.PricingSnapshot = strings.ReplaceAll(r.Usage.PricingSnapshot, "987654321.1234568", "987654321.12345678")
		_, err := db.Exec(`UPDATE users SET balance=1000000000 WHERE id=$1`, r.Key.UserID)
		require.NoError(t, err)
		svc := service.NewUsageSettlementService(repo, nil, nil)
		require.NoError(t, svc.Submit(ctx, r, nil))
		var correct bool
		require.NoError(t, db.QueryRow(`SELECT r.state='settled' AND (r.billing_command->'charged_amount'->>'amount')::numeric=987654321.12345678
    AND 1000000000-u.balance=987654321.12345678 AND k.quota_used=987654321.12345678 AND k.usage_5h=987654321.12345678 AND (SELECT actual_cost FROM usage_logs WHERE id=r.usage_log_id)=987654321.12345678
 AND EXISTS(SELECT 1 FROM billing_wallet_events w JOIN billing_settlement_outbox o ON o.transaction_id=w.transaction_id WHERE o.receipt_id=r.id AND w.amount=-987654321.12345678)
    FROM usage_settlement_receipts r JOIN users u ON u.id=r.user_id JOIN api_keys k ON k.id=r.api_key_id WHERE r.request_id=$1`, r.Key.RequestID).Scan(&correct))
		require.True(t, correct)
	})
	t.Run("Audit TPM identity conflict survives service recovery", func(t *testing.T) {
		original := fixture(t, 0.1)
		_, err := db.Exec(`UPDATE api_keys SET tpm_limit=1000 WHERE id=$1`, original.Key.APIKeyID)
		require.NoError(t, err)
		var workspaceID int64
		require.NoError(t, db.QueryRow(`SELECT workspace_id FROM api_keys WHERE id=$1`, original.Key.APIKeyID).Scan(&workspaceID))
		tokens := repo.(service.TPMReservationRepository)
		identities := []service.TPMReservationIdentity{{WorkspaceID: workspaceID, APIKeyID: original.Key.APIKeyID, ExecutionID: "audit-first", AttemptIndex: 1}, {WorkspaceID: workspaceID, APIKeyID: original.Key.APIKeyID, ExecutionID: "audit-second", AttemptIndex: 1}}
		for _, identity := range identities {
			_, err := tokens.ReserveTPMAttempt(ctx, service.TPMReservationCommand{TPMReservationIdentity: identity, AccountID: original.Command.AccountID, Fingerprint: identity.ExecutionID, Estimate: service.TPMEstimate{InputBound: 25, Candidates: 1, Verified: true}})
			require.NoError(t, err)
		}
		original.TPMReservation = &identities[0]
		original.Usage.InputTokens = 10
		original.Command.InputTokens = 10
		_, err = repo.Persist(ctx, original)
		require.NoError(t, err)
		incoming := *original
		incoming.TPMReservation = &identities[1]
		_, conflict := repo.Persist(ctx, &incoming)
		require.ErrorIs(t, conflict, service.ErrTPMReservationConflict)
		err = service.NewUsageSettlementService(repo, nil, nil).Submit(ctx, &incoming, nil)
		var used, reserved int64
		require.NoError(t, db.QueryRow(`SELECT sum(used_tokens),sum(reserved_tokens) FROM gateway_tpm_windows WHERE scope_kind='api_key' AND scope_id=$1`, original.Key.APIKeyID).Scan(&used, &reserved))
		var balance float64
		require.NoError(t, db.QueryRow(`SELECT balance FROM users WHERE id=$1`, original.Key.UserID).Scan(&balance))
		t.Logf("repository=%v service=%v used=%d reserved=%d balance=%.2f", conflict, err, used, reserved, balance)
		require.ErrorIs(t, err, service.ErrTPMReservationConflict)
	})

	t.Run("TPM receipt replay and dollar holds share one lock order", func(t *testing.T) {
		base := fixture(t, 0.1)
		_, err := db.Exec(`UPDATE api_keys SET tpm_limit=10000,budget_period_mode='duration',budget_period_seconds=3600,budget_limit=3 WHERE id=$1`, base.Key.APIKeyID)
		require.NoError(t, err)
		var workspaceID int64
		require.NoError(t, db.QueryRow(`SELECT workspace_id FROM api_keys WHERE id=$1`, base.Key.APIKeyID).Scan(&workspaceID))
		tokens := repo.(service.TPMReservationRepository)
		budgets := repo.(service.KeyBudgetReservationRepository)
		const workers = 12
		failures := make([]error, workers)
		var pending sync.WaitGroup
		for worker := range workers {
			pending.Add(1)
			go func() {
				defer pending.Done()
				input := *base
				requestID := fmt.Sprintf("%s-tpm-%d", base.Key.RequestID, worker)
				input.Key.RequestID = requestID
				input.Command.RequestID = requestID
				input.Usage.RequestID = requestID
				input.Command.InputTokens = 12
				input.Command.OutputTokens = 8
				input.Usage.InputTokens = 12
				input.Usage.OutputTokens = 8
				hold, err := budgets.ReserveKeyBudget(ctx, &service.KeyBudgetReservationCommand{RequestID: requestID, APIKeyID: base.Key.APIKeyID, UserID: base.Key.UserID, HoldAmount: 0.2})
				if err != nil {
					failures[worker] = err
					return
				}
				input.Command.BudgetReservationID = hold.Reservation.ID
				identity := service.TPMReservationIdentity{WorkspaceID: workspaceID, APIKeyID: base.Key.APIKeyID, ExecutionID: requestID, AttemptIndex: 1}
				_, err = tokens.ReserveTPMAttempt(ctx, service.TPMReservationCommand{TPMReservationIdentity: identity, AccountID: base.Command.AccountID, Fingerprint: requestID, Estimate: service.TPMEstimate{InputBound: 20, OutputBound: 5, Candidates: 1, Verified: true}})
				if err != nil {
					failures[worker] = err
					return
				}
				input.TPMReservation = &identity
				stored, err := repo.Persist(ctx, &input)
				if err != nil {
					failures[worker] = err
					return
				}
				// A durable pending receipt does not release its token capacity.
				err = tokens.MarkTPMAttemptUnknown(ctx, identity)
				if err != nil {
					failures[worker] = err
					return
				}
				// Verified failed/partial usage is corrected by only the final delta.
				err = tokens.SettleTPMAttempt(ctx, identity, 13)
				if err != nil {
					failures[worker] = err
					return
				}
				for range 2 {
					if _, err = repo.Settle(ctx, stored.Key, stored.Command.RequestFingerprint); err != nil {
						failures[worker] = err
						return
					}
				}
			}()
		}
		pending.Wait()
		for _, err := range failures {
			require.NoError(t, err)
		}
		var used, reserved int64
		require.NoError(t, db.QueryRow(`SELECT sum(used_tokens),sum(reserved_tokens) FROM gateway_tpm_windows WHERE scope_kind='api_key' AND scope_id=$1`, base.Key.APIKeyID).Scan(&used, &reserved))
		require.Equal(t, int64(workers*20), used)
		require.Zero(t, reserved)
		var balance float64
		require.NoError(t, db.QueryRow(`SELECT balance FROM users WHERE id=$1`, base.Key.UserID).Scan(&balance))
		require.InDelta(t, 100-float64(workers)*0.1, balance, 0.000001)
	})

	t.Run("billing evidence does not double count pending and settled", func(t *testing.T) {
		input := fixture(t, 0.12345678)
		input.CompletionState = "interrupted"
		prepared, err := service.PrepareUsageSettlementReceipt(input)
		require.NoError(t, err)
		stored, err := repo.Persist(ctx, prepared)
		require.NoError(t, err)
		logs := NewUsageLogRepository(nil, db).(*usageLogRepository)
		query := "SELECT " + usageLogSelectColumns + " FROM usage_logs WHERE request_id=$1 AND api_key_id=$2"
		before, err := logs.queryUsageLogs(ctx, query, input.Key.RequestID, input.Key.APIKeyID)
		require.NoError(t, err)
		require.Empty(t, before)
		result, err := repo.Settle(ctx, stored.Key, stored.Command.RequestFingerprint)
		require.NoError(t, err)
		after, err := logs.queryUsageLogs(ctx, query, input.Key.RequestID, input.Key.APIKeyID)
		require.NoError(t, err)
		require.Len(t, after, 1)
		require.Equal(t, 0.12345678, after[0].ActualCost)
		require.Equal(t, "settled", after[0].BillingEvidence.SettlementState)
		require.Equal(t, "interrupted", after[0].BillingEvidence.CompletionState)
		require.Equal(t, stored.Usage.PricingSnapshot, after[0].BillingEvidence.Snapshot)
		replay, err := service.RecalculatePricingSnapshot(after[0].BillingEvidence.Snapshot)
		require.NoError(t, err)
		require.Equal(t, after[0].ActualCost, replay.ActualCost)
		_, err = repo.Settle(ctx, stored.Key, stored.Command.RequestFingerprint)
		require.NoError(t, err)
		loaded, err := logs.GetByID(ctx, *result.Receipt.UsageLogID)
		require.NoError(t, err)
		require.Equal(t, after[0].BillingEvidence, loaded.BillingEvidence)
		var count int
		var amount float64
		require.NoError(t, db.QueryRow(`SELECT count(*),sum(actual_cost) FROM usage_logs WHERE request_id=$1 AND api_key_id=$2`, input.Key.RequestID, input.Key.APIKeyID).Scan(&count, &amount))
		require.Equal(t, 1, count)
		require.Equal(t, 0.12345678, amount)
		require.NoError(t, db.QueryRow("SELECT count(*) FROM usage_logs ul WHERE ul.id=$1 AND "+usageLogSuccessFilterUL, loaded.ID).Scan(&count))
		require.Zero(t, count, "paid interruption is not completion")

	})
	t.Run("async billing evidence preserves actual units and completion", func(t *testing.T) {
		for _, family := range []string{"asr", "3d", "file_batch", "realtime_turn"} {
			input := fixture(t, 0)
			logs := NewUsageLogRepository(nil, db).(*usageLogRepository)
			_, err := logs.Create(ctx, &input.Usage)
			require.NoError(t, err)
			historical, err := logs.GetByID(ctx, input.Usage.ID)
			require.NoError(t, err)
			require.Nil(t, historical.BillingEvidence)
			beforeStatus, err := repo.(service.UsageSettlementStatusRepository).SettlementStatus(ctx, input.Key.RequestID, 0)
			require.NoError(t, err)
			require.Len(t, beforeStatus.HistoricalItems, 1)
			jobFamily := family
			if family == "file_batch" {
				jobFamily = "batch"
			}
			snapshot := map[string]any{"pricing_state": "free", "pricing_snapshot": input.Usage.PricingSnapshot, "realtime": map[string]string{"session_id": "entry-session"}}
			raw, err := json.Marshal(snapshot)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO upstream_jobs(public_request_id,request_fingerprint,submission_key,platform,family,account_id,user_id,api_key_id,expires_at,billing_snapshot,status,charged_usage_log_id,finished_at,result_ref,upstream_task_id)
       VALUES($1,$1,$1,'qianwen',$2,$3,$4,$5,NOW()+INTERVAL '1 hour',$6,'succeeded',$7,NOW(),'incomplete','turn-1')`, input.Key.RequestID, jobFamily, input.Usage.AccountID, input.Key.UserID, input.Key.APIKeyID, string(raw), input.Usage.ID)
			require.NoError(t, err)
			loaded, err := logs.GetByID(ctx, input.Usage.ID)
			require.NoError(t, err)
			require.Equal(t, "free", loaded.BillingEvidence.PricingState)
			afterStatus, err := repo.(service.UsageSettlementStatusRepository).SettlementStatus(ctx, input.Key.RequestID, 0)
			require.NoError(t, err)
			require.Empty(t, afterStatus.HistoricalItems, "frozen job pricing is known even without an ordinary receipt")
			require.Equal(t, beforeStatus.HistoricalZeroReview-1, afterStatus.HistoricalZeroReview)
			var completed int
			require.NoError(t, db.QueryRow("SELECT count(*) FROM usage_logs ul WHERE ul.id=$1 AND "+usageLogSuccessFilterUL, loaded.ID).Scan(&completed))
			if family == "realtime_turn" {
				require.Zero(t, completed)
			} else {
				require.Equal(t, 1, completed, "free success counts independently of price")
			}

			require.Equal(t, input.Usage.PricingSnapshot, loaded.BillingEvidence.Snapshot)
			if family == "realtime_turn" {
				require.Equal(t, "interrupted", loaded.BillingEvidence.CompletionState)
				require.Equal(t, "entry-session", loaded.BillingEvidence.SessionID)
				require.Equal(t, "turn-1", loaded.BillingEvidence.TurnID)
			} else {
				require.Equal(t, "complete", loaded.BillingEvidence.CompletionState)
			}
		}
	})
	check := func(t *testing.T, receipt *service.UsageSettlementReceipt, settled bool) {
		t.Helper()
		var balance, quota, rate float64
		require.NoError(t, db.QueryRow(`SELECT balance FROM users WHERE id=$1`, receipt.Key.UserID).Scan(&balance))
		require.NoError(t, db.QueryRow(`SELECT quota_used,usage_5h FROM api_keys WHERE id=$1`, receipt.Key.APIKeyID).Scan(&quota, &rate))
		var logs, dedup, events int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM usage_logs WHERE request_id=$1 AND api_key_id=$2`, receipt.Key.RequestID, receipt.Key.APIKeyID).Scan(&logs))
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, receipt.Key.RequestID, receipt.Key.APIKeyID).Scan(&dedup))
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM receipt_test_effects WHERE request_id=$1`, receipt.Key.RequestID).Scan(&events))
		loaded, err := repo.Get(ctx, receipt.Key)
		require.NoError(t, err)
		if settled {
			amount := service.QuantizeUsageBillingAmount(receipt.Command.BalanceCost)
			require.InDelta(t, 100-amount, balance, 1e-10)
			require.Equal(t, amount, quota)
			require.Equal(t, quota, rate)
			require.Equal(t, 1, logs)
			require.Equal(t, 1, dedup)
			require.Equal(t, 1, events)
			require.Equal(t, service.UsageSettlementSettled, loaded.State)
			require.NotNil(t, loaded.UsageLogID)
			require.NotNil(t, loaded.SettledAt)
			var loggedAmount float64
			require.NoError(t, db.QueryRow(`SELECT actual_cost FROM usage_logs WHERE id=$1`, *loaded.UsageLogID).Scan(&loggedAmount))
			require.Equal(t, amount, loggedAmount)
		} else {
			require.Equal(t, float64(100), balance)
			require.Zero(t, quota)
			require.Zero(t, rate)
			require.Zero(t, logs)
			require.Zero(t, dedup)
			require.Zero(t, events)
			require.Equal(t, service.UsageSettlementPending, loaded.State)
			require.Nil(t, loaded.UsageLogID)
			require.Nil(t, loaded.SettledAt)
		}
	}
	for _, concurrency := range []int{20, 32} {
		t.Run(fmt.Sprintf("same_identity_%d", concurrency), func(t *testing.T) {
			input := fixture(t, .000078125)
			var applied atomic.Int32
			var workers sync.WaitGroup
			errorsCh := make(chan error, concurrency)
			for range concurrency {
				workers.Add(1)
				go func() {
					defer workers.Done()
					stored, err := NewUsageSettlementReceiptRepository(db).Persist(ctx, input)
					if err != nil {
						errorsCh <- err
						return
					}
					result, err := NewUsageSettlementReceiptRepository(db, effect).Settle(ctx, stored.Key, stored.Command.RequestFingerprint)
					if err != nil {
						errorsCh <- err
						return
					}
					if result.Billing.Applied {
						applied.Add(1)
					}
				}()
			}
			workers.Wait()
			close(errorsCh)
			for err := range errorsCh {
				require.NoError(t, err)
			}
			require.Equal(t, int32(1), applied.Load())
			check(t, input, true)
			// Lost response: a fresh repository finds the committed original ID.
			loaded, err := NewUsageSettlementReceiptRepository(db).Get(ctx, input.Key)
			require.NoError(t, err)
			require.Equal(t, input.Usage.PricingSnapshot, loaded.Usage.PricingSnapshot)
			rawCommand := input.Command
			rawCommand.Normalize()
			require.Equal(t, rawCommand.RequestFingerprint, loaded.Command.RequestFingerprint)
			chargedCommand := input.Command
			chargedCommand.BalanceCost = service.QuantizeUsageBillingAmount(chargedCommand.BalanceCost)
			chargedCommand.APIKeyQuotaCost = chargedCommand.BalanceCost
			chargedCommand.APIKeyRateLimitCost = chargedCommand.BalanceCost
			chargedCommand.Normalize()
			require.NotEqual(t, chargedCommand.RequestFingerprint, loaded.Command.RequestFingerprint)
			_, err = repo.Settle(ctx, input.Key, "different")
			require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
			conflicting := *input
			conflicting.Command.RequestPayloadHash = "changed"
			_, err = repo.Persist(ctx, &conflicting)
			require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
			wrongOwner := input.Key
			wrongOwner.UserID++
			_, err = repo.Get(ctx, wrongOwner)
			require.ErrorIs(t, err, service.ErrUsageSettlementNotFound)
		})
	}
	for _, failure := range []string{"log_insert", "balance_update", "commit"} {
		t.Run(failure+"_rolls_back_all_effects", func(t *testing.T) {
			input := fixture(t, 1.25)
			stored, err := repo.Persist(ctx, input)
			require.NoError(t, err)
			// A deferred constraint trigger fails COMMIT itself, after the log,
			// balance and completion UPDATE statements have all succeeded.
			name := "receipt_failure_" + failure
			table, timing, condition := "usage_logs", "BEFORE INSERT", fmt.Sprintf("NEW.request_id = '%s'", input.Key.RequestID)
			if failure == "balance_update" {
				table, timing, condition = "users", "BEFORE UPDATE", fmt.Sprintf("NEW.id = %d", input.Key.UserID)
			}
			if failure == "commit" {
				table, timing, condition = "usage_settlement_receipts", "AFTER UPDATE", fmt.Sprintf("NEW.id = %d AND NEW.state = 'settled'", stored.ID)
			}
			_, err = db.Exec(fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF %s THEN RAISE EXCEPTION 'injected receipt %s failure'; END IF; RETURN NEW; END $$`, name, condition, failure))
			require.NoError(t, err)
			constraint, deferred := "", ""
			if failure == "commit" {
				constraint, deferred = "CONSTRAINT ", "DEFERRABLE INITIALLY DEFERRED"
			}
			_, err = db.Exec(fmt.Sprintf(`CREATE %sTRIGGER %s %s ON %s %s FOR EACH ROW EXECUTE FUNCTION %s()`, constraint, name, timing, table, deferred, name))
			require.NoError(t, err)
			cleanup := func() {
				_, err := db.Exec(fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON %s`, name, table))
				require.NoError(t, err)
				_, err = db.Exec(fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, name))
				require.NoError(t, err)
			}
			t.Cleanup(cleanup)
			_, err = repo.Settle(ctx, input.Key, stored.Command.RequestFingerprint)
			require.ErrorContains(t, err, "injected receipt "+failure+" failure")
			check(t, input, false)
			cleanup()
			result, err := NewUsageSettlementReceiptRepository(db, effect).Settle(ctx, input.Key, stored.Command.RequestFingerprint)
			require.NoError(t, err)
			require.True(t, result.Billing.Applied)
			check(t, input, true)
		})
	}
	t.Run("derived_event_failure_rolls_back", func(t *testing.T) {
		input := fixture(t, 2)
		stored, err := repo.Persist(ctx, input)
		require.NoError(t, err)
		injected := errors.New("derived event insert failed")
		failing := NewUsageSettlementReceiptRepository(db, func(context.Context, *sql.Tx, *service.UsageLog) error { return injected })
		_, err = failing.Settle(ctx, input.Key, stored.Command.RequestFingerprint)
		require.ErrorIs(t, err, injected)
		check(t, input, false)
	})
	t.Run("concurrent_different_fingerprints_conflict", func(t *testing.T) {
		input := fixture(t, 1)
		var accepted, conflicts atomic.Int32
		var workers sync.WaitGroup
		errorsCh := make(chan error, 32)
		for worker := range 32 {
			workers.Add(1)
			go func() {
				defer workers.Done()
				candidate := *input
				candidate.Command.RequestPayloadHash = fmt.Sprintf("payload-%d", worker)
				stored, err := repo.Persist(ctx, &candidate)
				if errors.Is(err, service.ErrUsageBillingRequestConflict) {
					conflicts.Add(1)
					return
				}
				if err != nil {
					errorsCh <- err
					return
				}
				_, err = repo.Settle(ctx, stored.Key, stored.Command.RequestFingerprint)
				if err != nil {
					errorsCh <- err
					return
				}
				accepted.Add(1)
			}()
		}
		workers.Wait()
		close(errorsCh)
		for err := range errorsCh {
			require.NoError(t, err)
		}
		require.Equal(t, int32(1), accepted.Load())
		require.Equal(t, int32(31), conflicts.Load())
		check(t, input, true)
	})
	t.Run("archived_different_fingerprint_conflicts", func(t *testing.T) {
		input := fixture(t, 2)
		stored, err := repo.Persist(ctx, input)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO usage_billing_dedup_archive(request_id,api_key_id,request_fingerprint,created_at) VALUES($1,$2,'other',NOW())`, input.Key.RequestID, input.Key.APIKeyID)
		require.NoError(t, err)
		_, err = repo.Settle(ctx, input.Key, stored.Command.RequestFingerprint)
		require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
		require.ErrorIs(t, err, service.ErrUsageSettlementNeedsReview)
		var intact bool
		require.NoError(t, db.QueryRow(`SELECT r.state='needs_review' AND u.balance=100 AND k.quota_used=0 AND NOT EXISTS(SELECT 1 FROM usage_billing_dedup d WHERE d.api_key_id=k.id AND d.request_id=r.request_id) FROM usage_settlement_receipts r JOIN users u ON u.id=r.user_id JOIN api_keys k ON k.id=r.api_key_id WHERE r.id=$1`, stored.ID).Scan(&intact))
		require.True(t, intact)
	})
	t.Run("existing_canonical_log_conflict_rolls_back_claim", func(t *testing.T) {
		input := fixture(t, 2)
		stored, err := repo.Persist(ctx, input)
		require.NoError(t, err)
		_, err = (&usageLogRepository{}).createSingle(ctx, db, &input.Usage)
		require.NoError(t, err)
		_, err = repo.Settle(ctx, stored.Key, stored.Command.RequestFingerprint)
		require.ErrorIs(t, err, service.ErrUsageSettlementNeedsReview)
		_, err = db.Exec(`DELETE FROM usage_logs WHERE id=$1`, input.Usage.ID)
		require.NoError(t, err)
		loaded, err := repo.Get(ctx, input.Key)
		require.NoError(t, err)
		require.Equal(t, service.UsageSettlementNeedsReview, loaded.State)
		var claims int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM usage_billing_dedup WHERE request_id=$1`, input.Key.RequestID).Scan(&claims))
		require.Zero(t, claims)
	})
	t.Run("archived_identity_is_never_charged_again", func(t *testing.T) {
		input := fixture(t, 2)
		stored, err := repo.Persist(ctx, input)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO usage_billing_dedup_archive(request_id,api_key_id,request_fingerprint,created_at) VALUES($1,$2,$3,NOW())`, input.Key.RequestID, input.Key.APIKeyID, stored.Command.RequestFingerprint)
		require.NoError(t, err)
		_, err = repo.Settle(ctx, input.Key, stored.Command.RequestFingerprint)
		require.ErrorIs(t, err, service.ErrUsageSettlementNeedsReview)
		var balance float64
		require.NoError(t, db.QueryRow(`SELECT balance FROM users WHERE id=$1`, input.Key.UserID).Scan(&balance))
		require.Equal(t, float64(100), balance)
		loaded, err := repo.Get(ctx, input.Key)
		require.NoError(t, err)
		require.Equal(t, service.UsageSettlementNeedsReview, loaded.State)
		var liveDedup int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, input.Key.RequestID, input.Key.APIKeyID).Scan(&liveDedup))
		require.Zero(t, liveDedup)
	})
	t.Run("operator_card_missing_catalog_settlement", func(testingContext *testing.T) {
		inputPrice, outputPrice := 2e-6, 6e-6
		cards := []service.ChannelModelPricing{{Models: []string{"operator-only-model"}, InputPrice: &inputPrice, OutputPrice: &outputPrice}}
		encoded, marshalErr := json.Marshal(cards)
		require.NoError(testingContext, marshalErr)
		var groupID int64
		require.NoError(testingContext, db.QueryRow(`INSERT INTO groups(name,platform,model_pricing) VALUES($1,'openai',$2) RETURNING id`, "operator-price-"+uuid.NewString(), string(encoded)).Scan(&groupID))
		billing := service.NewBillingService(nil, nil)
		for _, configured := range []bool{true, false} {
			if !configured {
				_, updateErr := db.Exec(`UPDATE groups SET model_pricing='[]' WHERE id=$1`, groupID)
				require.NoError(testingContext, updateErr)
			}
			var storedCard []byte
			require.NoError(testingContext, db.QueryRow(`SELECT model_pricing FROM groups WHERE id=$1`, groupID).Scan(&storedCard))
			group := &service.Group{ID: groupID, Platform: service.PlatformOpenAI}
			require.NoError(testingContext, json.Unmarshal(storedCard, &group.ModelPricing))
			cost, calculationErr := billing.CalculateTokenCostForRequest(service.TokenCostRequest{Ctx: ctx, Model: "operator-only-model", Group: group, Resolver: service.NewModelPricingResolver(nil, billing), Tokens: service.UsageTokens{InputTokens: 1000, OutputTokens: 100}, RateMultiplier: 1})
			receipt := fixture(testingContext, cost.ActualCost)
			receipt.Command.Model, receipt.Usage.Model = "operator-only-model", "operator-only-model"
			receipt.Usage.PricingState, receipt.Usage.PricingSnapshot = cost.PricingState, cost.PricingSnapshot
			receipt.Usage.PriceSource = &cost.PriceSource
			if configured {
				require.NoError(testingContext, calculationErr)
				require.Equal(testingContext, service.PricingStatePriced, cost.PricingState)
				require.InDelta(testingContext, .0026, cost.ActualCost, 1e-12)
			} else {
				require.ErrorIs(testingContext, calculationErr, service.ErrModelPricingUnavailable)
				require.Equal(testingContext, service.PricingStateUnpriced, cost.PricingState)
				receipt.ResolutionState = "unresolved"
			}
			stored, persistErr := repo.Persist(ctx, receipt)
			require.NoError(testingContext, persistErr)
			_, settleErr := repo.Settle(ctx, stored.Key, stored.Command.RequestFingerprint)
			if configured {
				require.NoError(testingContext, settleErr)
				check(testingContext, receipt, true)
				var source string
				require.NoError(testingContext, db.QueryRow(`SELECT price_source FROM usage_logs WHERE request_id=$1`, receipt.Key.RequestID).Scan(&source))
				require.Equal(testingContext, "group", source)
				loadedReceipt, loadErr := repo.Get(ctx, receipt.Key)
				require.NoError(testingContext, loadErr)
				require.Contains(testingContext, loadedReceipt.Usage.PricingSnapshot, `"source":"group"`)
			} else {
				require.ErrorIs(testingContext, settleErr, service.ErrUsageSettlementNeedsReview)
				require.Equal(testingContext, service.UsageSettlementNeedsReview, stored.State)
				var balance float64
				var logCount int
				require.NoError(testingContext, db.QueryRow(`SELECT balance FROM users WHERE id=$1`, receipt.Key.UserID).Scan(&balance))
				require.Equal(testingContext, 100.0, balance)
				require.NoError(testingContext, db.QueryRow(`SELECT count(*) FROM usage_logs WHERE request_id=$1`, receipt.Key.RequestID).Scan(&logCount))
				require.Zero(testingContext, logCount, "unknown price must not create a free settled usage log")
			}
		}
	})

	t.Run("free_and_unpriced_remain_distinct", func(t *testing.T) {
		free := fixture(t, 0)
		free.CompletionState = "interrupted"
		stored, err := repo.Persist(ctx, free)
		require.NoError(t, err)
		_, err = repo.Settle(ctx, free.Key, stored.Command.RequestFingerprint)
		require.NoError(t, err)
		check(t, free, true)
		for _, state := range []service.PricingState{service.PricingStateUnpriced, service.PricingStateError} {
			input := fixture(t, 0)
			input.Usage.PricingState, input.Usage.PricingSnapshot = state, ""
			input.ResolutionState = "unresolved"
			stored, err := repo.Persist(ctx, input)
			require.NoError(t, err)
			require.Equal(t, service.UsageSettlementNeedsReview, stored.State)
			_, err = repo.Settle(ctx, stored.Key, stored.Command.RequestFingerprint)
			require.ErrorIs(t, err, service.ErrUsageSettlementNeedsReview)
		}
	})
	t.Run("pending_recovery_uses_frozen_input", func(t *testing.T) {
		input := fixture(t, 3)
		input.Usage.Account = &service.Account{Credentials: map[string]any{"api_key": "must-not-persist"}}
		stored, err := repo.Persist(ctx, input)
		require.NoError(t, err)
		require.Nil(t, stored.Usage.Account)
		require.NotNil(t, input.Usage.Account)
		pending, err := repo.ListPending(ctx, stored.ID-1, 1)
		require.NoError(t, err)
		require.Len(t, pending, 1)
		require.Equal(t, stored.ID, pending[0].ID)
		_, err = NewUsageSettlementReceiptRepository(db, effect).Settle(ctx, pending[0].Key, pending[0].Command.RequestFingerprint)
		require.NoError(t, err)
		check(t, input, true)
	})
	t.Run("committed_response_lost_query_original_identity", func(t *testing.T) {
		input := fixture(t, 4)
		stored, err := repo.Persist(ctx, input)
		require.NoError(t, err)
		responseLost := errors.New("settlement response lost")
		call := func() error {
			_, err := repo.Settle(ctx, stored.Key, stored.Command.RequestFingerprint)
			if err != nil {
				return err
			}
			return responseLost
		}
		require.ErrorIs(t, call(), responseLost)
		loaded, err := NewUsageSettlementReceiptRepository(db).Get(ctx, stored.Key)
		require.NoError(t, err)
		require.Equal(t, stored.ID, loaded.ID)
		require.Equal(t, service.UsageSettlementSettled, loaded.State)
		result, err := repo.Settle(ctx, stored.Key, stored.Command.RequestFingerprint)
		require.NoError(t, err)
		require.False(t, result.Billing.Applied)
		check(t, input, true)
	})
	t.Run("finance_worker_stopped_drop_sample_and_restart", func(t *testing.T) {
		for _, policy := range []string{"drop", "sample", "sync", "stopped"} {
			t.Run(policy, func(t *testing.T) {
				input := fixture(t, 2)
				pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{WorkerCount: 1, QueueSize: 1, OverflowPolicy: policy, OverflowSamplePercent: 1})
				if policy == "stopped" {
					pool.Stop()
				} else {
					entered, release := make(chan struct{}), make(chan struct{})
					pool.Submit(func(context.Context) { close(entered); <-release })
					<-entered
					pool.Submit(func(context.Context) {})
					defer func() { close(release); pool.Stop() }()
				}
				// This wrapper rejects settlement unless a PG read sees the full receipt.
				guarded := &financeReceiptFaults{UsageSettlementReceiptRepository: repo, verifyPersisted: true}
				svc := service.NewUsageSettlementService(guarded, pool, nil)
				require.NoError(t, svc.Submit(ctx, input, nil))
				check(t, input, true)
				restarted := service.NewUsageSettlementService(NewUsageSettlementReceiptRepository(db, effect), nil, nil)
				require.NoError(t, restarted.Recover(ctx))
				check(t, input, true)
			})
		}
	})
	t.Run("finance_persist_ack_lost_and_settlement_ack_lost", func(t *testing.T) {
		input := fixture(t, 3)
		faults := &financeReceiptFaults{UsageSettlementReceiptRepository: repo, losePersistAck: true, loseSettleAck: true}
		svc := service.NewUsageSettlementService(faults, nil, nil)
		require.NoError(t, svc.Submit(ctx, input, nil))
		check(t, input, true)
		// Both first INSERT and settlement COMMIT acknowledged by same identity.
		restarted := service.NewUsageSettlementService(NewUsageSettlementReceiptRepository(db, effect), nil, nil)
		require.NoError(t, restarted.Submit(ctx, input, nil))
		check(t, input, true)
	})
	t.Run("finance_rebuild_after_receipt_before_worker", func(t *testing.T) {
		input := fixture(t, 4)
		stored, err := repo.Persist(ctx, input)
		require.NoError(t, err)
		restarted := service.NewUsageSettlementService(NewUsageSettlementReceiptRepository(db, effect), nil, nil)
		require.NoError(t, restarted.Recover(ctx))
		check(t, input, true)
		loaded, err := repo.Get(ctx, input.Key)
		require.NoError(t, err)
		require.Equal(t, stored.Command.RequestFingerprint, loaded.Command.RequestFingerprint)
		require.Equal(t, stored.Usage.PricingSnapshot, loaded.Usage.PricingSnapshot)
	})
	t.Run("finance_persistence_failure_is_visible_without_free_log", func(t *testing.T) {
		input := fixture(t, 2)
		faults := &financeReceiptFaults{UsageSettlementReceiptRepository: repo, failPersist: true}
		svc := service.NewUsageSettlementService(faults, nil, nil)
		require.ErrorContains(t, svc.Submit(ctx, input, nil), "persistence unconfirmed")
		_, err := repo.Get(ctx, input.Key)
		require.ErrorIs(t, err, service.ErrUsageSettlementNotFound)
		var logs int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM usage_logs WHERE request_id=$1`, input.Key.RequestID).Scan(&logs))
		require.Zero(t, logs)
		status, err := svc.Status(ctx, "", 0)
		require.NoError(t, err)
		require.Equal(t, uint64(1), status.PersistenceUnconfirmed)
		require.NotEmpty(t, status.RecoveryBoundary)
	})
	t.Run("finance_legacy_dedup_repair_never_charges_again", func(t *testing.T) {
		for _, archive := range []bool{false, true} {
			input := fixture(t, 2)
			input.Command.RequestPayloadHash = service.HashUsageRequestPayload([]byte("retained-original-request"))
			stored, err := repo.Persist(ctx, input)
			require.NoError(t, err)
			// The old transaction has charged but never wrote its usage log.
			_, err = NewUsageBillingRepository(nil, db).Apply(ctx, &stored.Command)
			require.NoError(t, err)
			if archive {
				_, err = db.Exec(`INSERT INTO usage_billing_dedup_archive(request_id,api_key_id,request_fingerprint,created_at) SELECT request_id,api_key_id,request_fingerprint,created_at FROM usage_billing_dedup WHERE request_id=$1`, input.Key.RequestID)
				require.NoError(t, err)
				_, err = db.Exec(`DELETE FROM usage_billing_dedup WHERE request_id=$1`, input.Key.RequestID)
				require.NoError(t, err)
			}
			_, err = repo.Settle(ctx, input.Key, stored.Command.RequestFingerprint)
			require.ErrorIs(t, err, service.ErrUsageSettlementNeedsReview)
			repaired, err := repo.(service.UsageSettlementReconciler).ReconcileLegacy(ctx, input.Key, stored.Command.RequestFingerprint)
			require.NoError(t, err)
			require.False(t, repaired.Billing.Applied)
			_, err = repo.(service.UsageSettlementReconciler).ReconcileLegacy(ctx, input.Key, stored.Command.RequestFingerprint)
			require.NoError(t, err)
			var balance float64
			require.NoError(t, db.QueryRow(`SELECT balance FROM users WHERE id=$1`, input.Key.UserID).Scan(&balance))
			require.Equal(t, float64(98), balance)
			var events, logs int
			require.NoError(t, db.QueryRow(`SELECT count(*) FROM receipt_test_effects WHERE request_id=$1`, input.Key.RequestID).Scan(&events))
			require.Zero(t, events)
			require.NoError(t, db.QueryRow(`SELECT count(*) FROM usage_logs WHERE request_id=$1`, input.Key.RequestID).Scan(&logs))
			require.Equal(t, 1, logs)
		}
	})
	t.Run("finance_legacy_zero_requires_binding_and_original_evidence", func(t *testing.T) {
		input := fixture(t, 2)
		input.Command.RequestPayloadHash = service.HashUsageRequestPayload([]byte("original-zero-failure"))
		stored, err := repo.Persist(ctx, input)
		require.NoError(t, err)
		zero := stored.Usage
		zero.ActualCost = 0
		_, err = (&usageLogRepository{}).createSingle(ctx, db, &zero)
		require.NoError(t, err)
		_, err = repo.(service.UsageSettlementReconciler).ReconcileLegacy(ctx, input.Key, stored.Command.RequestFingerprint)
		require.NoError(t, err)
		check(t, input, true)
		orphan := fixture(t, 2)
		orphan.Usage.ActualCost = 0
		_, err = (&usageLogRepository{}).createSingle(ctx, db, &orphan.Usage)
		require.NoError(t, err)
		orphan.Command.Normalize()
		_, err = repo.(service.UsageSettlementReconciler).ReconcileLegacy(ctx, orphan.Key, orphan.Command.RequestFingerprint)
		require.ErrorIs(t, err, service.ErrUsageSettlementNotFound)
		status, err := repo.(service.UsageSettlementStatusRepository).SettlementStatus(ctx, "", 0)
		require.NoError(t, err)
		require.Greater(t, status.HistoricalZeroReview, int64(0))
		noEvidence := fixture(t, 2)
		stored, err = repo.Persist(ctx, noEvidence)
		require.NoError(t, err)
		_, err = repo.(service.UsageSettlementReconciler).ReconcileLegacy(ctx, noEvidence.Key, stored.Command.RequestFingerprint)
		require.ErrorIs(t, err, service.ErrUsageSettlementNeedsReview)
	})
	t.Run("finance_quota_provider_emits_once", func(t *testing.T) {
		input := fixture(t, 2)
		input.Usage.QuotaPlatform = "qianwen"
		_, err := db.Exec(`INSERT INTO user_platform_quotas(user_id,platform,daily_limit_usd) VALUES($1,'qianwen',100)`, input.Key.UserID)
		require.NoError(t, err)
		input.Usage.CreatedAt = time.Now().UTC().Add(time.Second)
		quotaRepo := NewUsageSettlementReceiptWithQuotaRepository(db)
		svc := service.NewUsageSettlementService(quotaRepo, nil, nil)
		require.NoError(t, svc.Submit(ctx, input, nil))
		require.NoError(t, svc.Submit(ctx, input, nil))
		var events int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM user_platform_quota_events WHERE user_id=$1`, input.Key.UserID).Scan(&events))
		require.Equal(t, 1, events)
	})

}

type financeReceiptFaults struct {
	service.UsageSettlementReceiptRepository
	losePersistAck, loseSettleAck, failPersist, verifyPersisted bool
}

func (r *financeReceiptFaults) Persist(ctx context.Context, input *service.UsageSettlementReceipt) (*service.UsageSettlementReceipt, error) {
	if r.failPersist {
		return nil, errors.New("database unavailable before first receipt")
	}
	result, err := r.UsageSettlementReceiptRepository.Persist(ctx, input)
	if err == nil && r.losePersistAck {
		return nil, errors.New("INSERT commit acknowledgement lost")
	}
	return result, err
}
func (r *financeReceiptFaults) Settle(ctx context.Context, key service.UsageSettlementKey, fp string) (*service.UsageSettlementResult, error) {
	if r.verifyPersisted {
		row, err := r.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		if row.Usage.PricingSnapshot == "" {
			return nil, errors.New("queued before frozen receipt")
		}
	}
	result, err := r.UsageSettlementReceiptRepository.Settle(ctx, key, fp)
	if err == nil && r.loseSettleAck {
		return nil, errors.New("settlement commit acknowledgement lost")
	}
	return result, err
}
func (r *financeReceiptFaults) SettlementStatus(ctx context.Context, requestID string, afterID int64) (*service.UsageSettlementStatus, error) {
	return r.UsageSettlementReceiptRepository.(service.UsageSettlementStatusRepository).SettlementStatus(ctx, requestID, afterID)
}
