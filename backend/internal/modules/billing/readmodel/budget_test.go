package readmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCalendarReset(t *testing.T) {
	for _, tc := range []struct{ instant, period, start, end string }{
		{"2026-03-08T16:00:00Z", "calendar_day", "2026-03-08T05:00:00Z", "2026-03-09T04:00:00Z"},
		{"2026-11-01T06:30:00Z", "calendar_day", "2026-11-01T04:00:00Z", "2026-11-02T05:00:00Z"},
		{"2026-11-02T05:00:00Z", "calendar_week", "2026-11-02T05:00:00Z", "2026-11-09T05:00:00Z"},
		{"2026-03-31T23:30:00Z", "calendar_month", "2026-03-01T05:00:00Z", "2026-04-01T04:00:00Z"},
	} {
		at, e := time.Parse(time.RFC3339, tc.instant)
		require.NoError(t, e)
		a, b, e := bounds(at, "America/New_York", tc.period)
		require.NoError(t, e)
		require.Equal(t, tc.start, a.Format(time.RFC3339))
		require.Equal(t, tc.end, b.Format(time.RFC3339))
	}
}

func TestRequestPlanChecksProjectWithoutKeyBudget(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	owner := scope.Authorized{WorkspaceID: 1, ProjectID: 2, KeyID: 3, KeyOwnerID: 4}
	ctx := service.WithProjectBudgetCoordinator(scope.With(t.Context(), owner), Store{DB: db})
	spec := service.GatewayRequestSpec{WorkspaceID: 1, UserID: 4, APIKeyID: 3, Model: "unpriced"}
	var gateway *service.GatewayService
	for _, tc := range []struct {
		mode    string
		blocked bool
	}{
		{"", false}, {"soft", false}, {"hard", true},
	} {
		rows := sqlmock.NewRows([]string{"mode"})
		if tc.mode != "" {
			rows.AddRow(tc.mode)
		}
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT mode FROM billing_project_budgets").WithArgs(int64(1), int64(2), "USD").WillReturnRows(rows)
		if tc.blocked {
			mock.ExpectRollback()
		} else {
			mock.ExpectCommit()
		}
		child, plan := gateway.CaptureRequestPlan(ctx, spec)
		err := plan.ReserveBudget(child, service.RequestBudgetEstimate{Model: "unpriced", SkipKeyBudget: true})
		if tc.blocked {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			require.NoError(t, plan.ReserveBudget(child, service.RequestBudgetEstimate{SkipKeyBudget: true}))
		}
		plan.Finalize(false)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

type budgetReceiptCapture struct {
	service.UsageSettlementReceiptRepository
	persisted *service.UsageSettlementReceipt
}

func (r *budgetReceiptCapture) Persist(_ context.Context, input *service.UsageSettlementReceipt) (*service.UsageSettlementReceipt, error) {
	r.persisted = input
	return input, nil
}
func (r *budgetReceiptCapture) Settle(context.Context, service.UsageSettlementKey, string) (*service.UsageSettlementResult, error) {
	return &service.UsageSettlementResult{Receipt: r.persisted, Billing: &service.UsageBillingApplyResult{}}, nil
}

func TestBudgetReceiptPreservesFrozenDecimal(t *testing.T) {
	const text = "9000000000.12345679"
	legacy, err := strconv.ParseFloat(text, 64)
	require.NoError(t, err)
	require.NotEqual(t, text, strconv.FormatFloat(legacy, 'f', 8, 64))
	receipt := &service.UsageSettlementReceipt{
		Key:     service.UsageSettlementKey{RequestID: "precision", APIKeyID: 2, UserID: 1},
		Command: service.UsageBillingCommand{RequestID: "precision", APIKeyID: 2, UserID: 1, AccountID: 3, Model: "m", BalanceCost: legacy},
		Usage: service.UsageLog{RequestID: "precision", APIKeyID: 2, UserID: 1, AccountID: 3, Model: "m", PricingState: service.PricingStatePriced, ActualCost: legacy, TotalCost: legacy,
			PricingSnapshot: fmt.Sprintf(`{"pricing_state":"priced","unit_price":%[1]s,"units":1,"rate_multiplier":1,"total_cost":%[1]s,"actual_cost":%[1]s,"charged_amount":%[1]s}`, text)},
		ResolutionState: "resolved", CompletionState: "complete",
	}
	repo := &budgetReceiptCapture{}
	require.NoError(t, service.NewUsageSettlementService(repo, nil, nil).Submit(t.Context(), receipt, nil))
	raw, err := json.Marshal(repo.persisted.Command)
	require.NoError(t, err)
	var replay service.UsageBillingCommand
	require.NoError(t, json.Unmarshal(raw, &replay))
	require.NotNil(t, replay.ChargedAmount)
	require.Equal(t, text, replay.ChargedAmount.String())
}

func TestProjectBudgetRejectsUnsupportedCurrencyAndExternalCosts(t *testing.T) {
	for _, input := range []struct {
		currency money.Currency
		external bool
	}{
		{"CNY", false}, {"USD", true},
	} {
		limit, err := money.NewMoney(input.currency, "10")
		require.NoError(t, err)
		_, err = (Store{}).UpdateBudget(t.Context(), Scope{}, "project-"+string(input.currency), "", 0,
			Budget{Limit: limit, Mode: "hard", Period: "calendar_day", Timezone: "UTC", External: input.external})
		require.Error(t, err)
	}
}

func TestProjectBudgetRecoveryRechecksEvidenceAfterJournalLock(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectQuery("(?s)SELECT e.request_id::text.*FOR UPDATE OF e SKIP LOCKED").
		WithArgs(int64(1), int64(2), "USD").
		WillReturnRows(sqlmock.NewRows([]string{"request_id"}).AddRow("expired-execution"))
	mock.ExpectExec("(?s)UPDATE billing_project_holds.*NOT EXISTS.*gateway_execution_attempts.*NOT EXISTS.*usage_settlement_receipts").
		WithArgs(int64(1), int64(2), "USD", `["expired-execution"]`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	tx, err := db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	require.NoError(t, recoverProjectHolds(t.Context(), tx, Scope{Workspace: 1, Project: 2}, "USD"))
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}
