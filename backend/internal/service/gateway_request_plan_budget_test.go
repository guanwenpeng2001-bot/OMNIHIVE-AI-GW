//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// keyBudgetReceiptStub is a settlement receipt repository that also implements
// the request-hold port, mirroring the production SQL repository.
type keyBudgetReceiptStub struct {
	reservations []KeyBudgetReservationCommand
	released     []string
	reserveErr   error
	outcome      *KeyBudgetReservationOutcome
}

func (s *keyBudgetReceiptStub) Persist(context.Context, *UsageSettlementReceipt) (*UsageSettlementReceipt, error) {
	panic("unexpected Persist call")
}

func (s *keyBudgetReceiptStub) Get(context.Context, UsageSettlementKey) (*UsageSettlementReceipt, error) {
	panic("unexpected Get call")
}

func (s *keyBudgetReceiptStub) Settle(context.Context, UsageSettlementKey, string) (*UsageSettlementResult, error) {
	panic("unexpected Settle call")
}

func (s *keyBudgetReceiptStub) ListPending(context.Context, int64, int) ([]*UsageSettlementReceipt, error) {
	return nil, nil
}

func (s *keyBudgetReceiptStub) ReserveKeyBudget(_ context.Context, cmd *KeyBudgetReservationCommand) (*KeyBudgetReservationOutcome, error) {
	if s.reserveErr != nil {
		return nil, s.reserveErr
	}
	s.reservations = append(s.reservations, *cmd)
	if s.outcome != nil {
		return s.outcome, nil
	}
	return &KeyBudgetReservationOutcome{
		Reservation: &KeyBudgetReservation{
			ID: int64(len(s.reservations)), RequestID: cmd.RequestID, APIKeyID: cmd.APIKeyID,
			UserID: cmd.UserID, HoldAmount: cmd.HoldAmount, BalanceHeld: !cmd.SkipBalanceHold,
		},
	}, nil
}

func (s *keyBudgetReceiptStub) ReleaseKeyBudgetReservation(_ context.Context, apiKeyID int64, requestID string) error {
	s.released = append(s.released, requestID)
	return nil
}

func (s *keyBudgetReceiptStub) ReleaseExpiredKeyBudgetReservations(context.Context, time.Time, int) (int, error) {
	return 0, nil
}

func TestGatewayRequestPlan_ReserveBudgetIsIdempotentAndCarriesTheHold(t *testing.T) {
	stub := &keyBudgetReceiptStub{}
	settlement := NewUsageSettlementService(stub, nil, nil)
	plan := NewGatewayRequestPlan(GatewayRequestSpec{
		UserID: 3, APIKeyID: 7, ExecutionID: "req-plan-1", Model: "claude-sonnet-4",
	})
	plan.settlement = settlement

	estimate := RequestBudgetEstimate{Model: "claude-sonnet-4", InputTokens: 1000, OutputTokens: 500, RateMultiplier: 1}
	require.NoError(t, plan.ReserveBudget(context.Background(), estimate))
	require.Len(t, stub.reservations, 1)
	require.Equal(t, "req-plan-1", stub.reservations[0].RequestID)
	require.Equal(t, int64(7), stub.reservations[0].APIKeyID)
	require.Equal(t, int64(3), stub.reservations[0].UserID)
	require.False(t, stub.reservations[0].SkipBalanceHold)
	require.NotNil(t, plan.BudgetHold())
	require.True(t, plan.BudgetAdmitted())

	// Failover retries reuse the first admission instead of holding twice.
	require.NoError(t, plan.ReserveBudget(context.Background(), estimate))
	require.Len(t, stub.reservations, 1)
}

func TestGatewayRequestPlan_ReserveBudgetPropagatesRejection(t *testing.T) {
	stub := &keyBudgetReceiptStub{reserveErr: ErrAPIKeyBudgetExceeded}
	plan := NewGatewayRequestPlan(GatewayRequestSpec{UserID: 3, APIKeyID: 7, ExecutionID: "req-plan-2"})
	plan.settlement = NewUsageSettlementService(stub, nil, nil)

	err := plan.ReserveBudget(context.Background(), RequestBudgetEstimate{Model: "gpt-5"})
	require.ErrorIs(t, err, ErrAPIKeyBudgetExceeded)
	require.Nil(t, plan.BudgetHold(), "a rejected request holds nothing")
	require.False(t, plan.BudgetAdmitted())
}

func TestGatewayRequestPlan_ReserveBudgetFailsClosedWithoutSettlement(t *testing.T) {
	plan := NewGatewayRequestPlan(GatewayRequestSpec{UserID: 3, APIKeyID: 7, ExecutionID: "req-plan-3"})
	require.ErrorIs(t, plan.ReserveBudget(context.Background(), RequestBudgetEstimate{Model: "gpt-5"}), ErrAPIKeyBudgetUnavailable)
}

func TestGatewayRequestPlan_ReleaseRefundsAnUnsettledHold(t *testing.T) {
	stub := &keyBudgetReceiptStub{}
	plan := NewGatewayRequestPlan(GatewayRequestSpec{UserID: 3, APIKeyID: 7, ExecutionID: "req-plan-4"})
	plan.settlement = NewUsageSettlementService(stub, nil, nil)
	require.NoError(t, plan.ReserveBudget(context.Background(), RequestBudgetEstimate{Model: "gpt-5"}))

	plan.Finalize(false)
	require.Equal(t, []string{"req-plan-4"}, stub.released, "an unsettled hold is refunded when the request ends")

	// A settled receipt already consumed the hold: finalize must not refund it.
	stub.released = nil
	settledPlan := NewGatewayRequestPlan(GatewayRequestSpec{UserID: 3, APIKeyID: 7, ExecutionID: "req-plan-5"})
	settledPlan.settlement = NewUsageSettlementService(stub, nil, nil)
	require.NoError(t, settledPlan.ReserveBudget(context.Background(), RequestBudgetEstimate{Model: "gpt-5"}))
	settledPlan.setReceiptState(UsageSettlementSettled)
	settledPlan.Finalize(true)
	require.Empty(t, stub.released)
}

func TestUsageSettlementService_ReserveKeyBudgetValidatesIdentity(t *testing.T) {
	stub := &keyBudgetReceiptStub{}
	settlement := NewUsageSettlementService(stub, nil, nil)

	_, err := settlement.ReserveKeyBudget(context.Background(), &KeyBudgetReservationCommand{APIKeyID: 7, UserID: 3})
	require.ErrorIs(t, err, ErrAPIKeyBudgetInvalid)
	_, err = settlement.ReserveKeyBudget(context.Background(), &KeyBudgetReservationCommand{RequestID: "r", APIKeyID: 0, UserID: 3})
	require.ErrorIs(t, err, ErrAPIKeyBudgetInvalid)

	outcome, err := settlement.ReserveKeyBudget(context.Background(), &KeyBudgetReservationCommand{RequestID: " r ", APIKeyID: 7, UserID: 3, HoldAmount: 0.25})
	require.NoError(t, err)
	require.NotNil(t, outcome.Reservation)
	require.Equal(t, "r", stub.reservations[0].RequestID)
	require.Equal(t, DefaultKeyBudgetReservationTTL, stub.reservations[0].TTL)
}
