package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type tpmPlanRepository struct {
	UsageSettlementReceiptRepository
	reservations map[int]TPMReservationCommand
	actual       map[int]int64
	released     map[int]bool
	unknown      map[int]bool
}

func (r *tpmPlanRepository) ReserveTPMAttempt(_ context.Context, command TPMReservationCommand) (*TPMReservation, error) {
	if previous, found := r.reservations[command.AttemptIndex]; found && previous.Fingerprint != command.Fingerprint {
		return nil, ErrTPMReservationConflict
	}
	tokens, err := command.Estimate.Tokens()
	if err != nil {
		return nil, err
	}
	r.reservations[command.AttemptIndex] = command
	return &TPMReservation{TPMReservationIdentity: command.TPMReservationIdentity, Estimate: tokens, Fingerprint: command.Fingerprint, WindowStart: time.Now().UTC().Truncate(time.Minute)}, nil
}

func (r *tpmPlanRepository) SettleTPMAttempt(_ context.Context, identity TPMReservationIdentity, actual int64) error {
	r.actual[identity.AttemptIndex] = actual
	return nil
}

func (r *tpmPlanRepository) ReleaseUnsentTPMAttempt(_ context.Context, identity TPMReservationIdentity) error {
	r.released[identity.AttemptIndex] = true
	return nil
}

func (r *tpmPlanRepository) MarkTPMAttemptUnknown(_ context.Context, identity TPMReservationIdentity) error {
	r.unknown[identity.AttemptIndex] = true
	return nil
}

func newTPMPlanFixture() (*GatewayRequestPlan, *tpmPlanRepository) {
	repository := &tpmPlanRepository{reservations: make(map[int]TPMReservationCommand), actual: make(map[int]int64), released: make(map[int]bool), unknown: make(map[int]bool)}
	plan := NewGatewayRequestPlan(GatewayRequestSpec{WorkspaceID: 1, APIKeyID: 2, BillingGroupID: 3, ExecutionID: "logical-request"})
	plan.settlement = NewUsageSettlementService(repository, nil, nil)
	return plan, repository
}

func TestGatewayRequestPlan_TPMOnlyKeyStillReserves(t *testing.T) {
	plan, repository := newTPMPlanFixture()
	for range 2 {
		_, err := plan.ReserveTPMAttempt(context.Background(), 1, 4, "fingerprint", TPMEstimate{InputBound: 12, OutputBound: 8, Candidates: 1, Verified: true})
		require.NoError(t, err)
	}
	require.Nil(t, plan.BudgetHold())
	require.Len(t, repository.reservations, 1)
	require.Equal(t, int64(3), repository.reservations[1].BillingGroupID)
	plan.Finalize(false)
	require.True(t, repository.released[1])
}

func TestGatewayRequestPlan_TPMRetryKeepsConsumedAttempt(t *testing.T) {
	plan, repository := newTPMPlanFixture()
	_, err := plan.ReserveTPMAttempt(context.Background(), 1, 4, "first-account", TPMEstimate{InputBound: 12, OutputBound: 8, Candidates: 1, Verified: true})
	require.NoError(t, err)
	plan.MarkTPMAttemptSent(1)
	require.NoError(t, plan.SettleTPMAttempt(context.Background(), 1, UsageTokens{InputTokens: 12, OutputTokens: 2}))
	_, err = plan.ReserveTPMAttempt(context.Background(), 2, 5, "second-account", TPMEstimate{InputBound: 12, OutputBound: 8, Candidates: 1, Verified: true})
	require.NoError(t, err)
	plan.Cancel()
	require.Equal(t, int64(14), repository.actual[1])
	require.False(t, repository.released[1])
	require.True(t, repository.released[2])
	require.Len(t, repository.reservations, 2)
}

func TestGatewayRequestPlan_TPMCancelIsNotZeroUsage(t *testing.T) {
	plan, repository := newTPMPlanFixture()
	_, err := plan.ReserveTPMAttempt(context.Background(), 1, 4, "fingerprint", TPMEstimate{InputBound: 12, OutputBound: 8, Candidates: 1, Verified: true})
	require.NoError(t, err)
	plan.MarkTPMAttemptSent(1)
	plan.Cancel()
	require.True(t, repository.unknown[1])
	require.False(t, repository.released[1])
	require.Empty(t, repository.actual)
}
