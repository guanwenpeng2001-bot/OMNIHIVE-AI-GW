package service

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRequestPlanBudgetIncludesInitialRepairAndSwitchesWithoutThreeAttemptCap(t *testing.T) {
	ctx, cancel := WithAttemptBudget(context.Background(), DefaultGenerationAttempts, time.Minute)
	defer cancel()
	ctx, plan := (&GatewayService{}).CaptureRequestPlan(ctx, GatewayRequestSpec{MaxAccountSwitches: 5})
	installUnlimitedTPMFixture(plan)
	require.Equal(t, 6, plan.Snapshot().GenerationAttempts)
	for attempt := 0; attempt < 6; attempt++ {
		finish, err := BeginGenerationAttempt(ctx, int64(attempt/2+1))
		require.NoError(t, err)
		finish(http.StatusBadRequest, nil)
	}
	_, err := BeginGenerationAttempt(ctx, 10)
	require.ErrorIs(t, err, ErrAttemptBudgetExhausted)
	require.Len(t, plan.Attempts(), 6)
	require.Equal(t, "request_repair", plan.Attempts()[1].Reason)
	require.Equal(t, "account_switch", plan.Attempts()[2].Reason)
}

func TestRequestBudgetExplicitRejectionAllowsSwitch(t *testing.T) {
	ctx, cancel := WithAttemptBudget(context.Background(), 2, time.Minute)
	defer cancel()
	finish, err := BeginGenerationAttempt(ctx, 1)
	require.NoError(t, err)
	finish(http.StatusServiceUnavailable, nil)
	_, err = BeginGenerationAttempt(ctx, 2)
	require.NoError(t, err)
}

func TestRequestBudgetContentOrUnknownSideEffectForbidsSecondSend(t *testing.T) {
	for _, scenario := range []string{"content", "unknown_eof"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := WithAttemptBudget(context.Background(), 5, time.Minute)
			defer cancel()
			finish, err := BeginGenerationAttempt(ctx, 1)
			require.NoError(t, err)
			if scenario == "content" {
				finish(http.StatusOK, nil)
				CommitAttemptResponse(ctx)
			} else {
				finish(0, io.ErrUnexpectedEOF)
			}
			_, err = BeginGenerationAttempt(ctx, 2)
			require.True(t, errors.Is(err, ErrResponseCommitted) || errors.Is(err, ErrAttemptOutcomeUnknown))
			require.Len(t, AttemptBudgetFromContext(ctx).Snapshot(), 1)
		})
	}
}

func TestRequestBudgetUnsentDialFailureAllowsSwitch(t *testing.T) {
	ctx, cancel := WithAttemptBudget(context.Background(), 2, time.Minute)
	defer cancel()
	finish, err := BeginGenerationAttempt(ctx, 1)
	require.NoError(t, err)
	finish(0, &net.OpError{Op: "dial", Err: errors.New("connection refused")})
	_, err = BeginGenerationAttempt(ctx, 2)
	require.NoError(t, err)
}

func TestUsageEvidencePanicReplaysOnceAndRetainsFailureState(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		plan := NewGatewayRequestPlan(GatewayRequestSpec{})
		ctx := WithGatewayRequestPlan(context.Background(), plan)
		calls := 0
		before := UsageRecordPanicCount()
		RunUsageEvidenceTask(ctx, func(context.Context) {
			calls++
			if persistent || calls == 1 {
				panic("injected storage failure")
			}
		})
		require.Equal(t, 2, calls)
		require.Greater(t, UsageRecordPanicCount(), before)
		require.Equal(t, persistent, plan.State().UsageRecoveryRequired)
	}
}

func TestNilSettlementCannotDeductOrWriteUnsettledUsage(t *testing.T) {
	err := submitUsageSettlement(context.Background(), nil, &UsageLog{}, &postUsageBillingParams{}, &billingDeps{}, false)
	require.ErrorContains(t, err, "settlement repository is required")
}

type panicSettlementReceiptStore struct {
	*durableReceiptStore
	calls int
}

func (s *panicSettlementReceiptStore) Settle(ctx context.Context, key UsageSettlementKey, fingerprint string) (*UsageSettlementResult, error) {
	s.calls++
	if s.calls == 1 {
		panic("settlement transaction panic")
	}
	return s.durableReceiptStore.Settle(ctx, key, fingerprint)
}

func TestSettlementPanicRetainsReceiptAndReplaysOnlyOnce(t *testing.T) {
	repository := &panicSettlementReceiptStore{durableReceiptStore: &durableReceiptStore{}}
	settlement := NewUsageSettlementService(repository, nil, nil)
	before := UsageRecordPanicCount()
	require.NoError(t, settlement.Submit(context.Background(), durableReceiptFixture(), nil))
	require.Equal(t, 2, repository.calls)
	require.Equal(t, 1, repository.applied)
	require.Equal(t, UsageSettlementSettled, repository.row.State)
	require.Equal(t, before+1, UsageRecordPanicCount())
}

func TestRepeatedUsagePanicPersistsUnresolvedReceiptWithoutDebit(t *testing.T) {
	repository := &durableReceiptStore{}
	ctx, cancel := WithAttemptBudget(context.Background(), 1, time.Minute)
	defer cancel()
	gateway := &GatewayService{usageSettlement: NewUsageSettlementService(repository, nil, nil)}
	ctx, plan := gateway.CaptureRequestPlan(ctx, GatewayRequestSpec{UserID: 1, APIKeyID: 2, Model: "model"})
	finish, err := BeginGenerationAttempt(ctx, 3)
	require.NoError(t, err)
	finish(http.StatusOK, nil)
	RunUsageEvidenceTask(ctx, func(context.Context) { panic("usage calculation failed") })
	require.NotNil(t, repository.row)
	require.Equal(t, UsageSettlementNeedsReview, repository.row.State)
	require.Equal(t, "unresolved", repository.row.ResolutionState)
	require.Equal(t, UsageSettlementNeedsReview, plan.State().ReceiptState)
	require.Zero(t, repository.applied)
}

func TestJobPlanRetainsCancellationAndFrozenParent(t *testing.T) {
	ctx, cancel := WithAttemptBudget(context.Background(), 1, time.Minute)
	ctx, parent := (&GatewayService{}).CaptureRequestPlan(ctx, GatewayRequestSpec{Model: "public", MaxAccountSwitches: 4})
	jobCtx, finish := CaptureGatewayJobPlan(ctx, GatewayRequestSpec{ExecutionID: "job-identity", Model: "public", UpstreamModel: "provider-model"})
	defer finish()
	jobPlan := GatewayRequestPlanFromContext(jobCtx)
	require.Equal(t, "job", jobPlan.Snapshot().ExecutionKind)
	require.Equal(t, "job-identity", jobPlan.Snapshot().ExecutionID)
	require.Same(t, parent.budget, jobPlan.budget)
	require.Same(t, parent.registrySnapshot, jobPlan.registrySnapshot)
	cancel()
	require.ErrorIs(t, CheckAttemptBudget(jobCtx), context.Canceled)
}
