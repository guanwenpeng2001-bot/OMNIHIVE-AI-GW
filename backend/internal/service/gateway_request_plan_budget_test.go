//go:build unit

package service

import (
	"context"
	"strings"
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
	plan.billing = NewBillingService(nil, nil)
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
	plan.billing = NewBillingService(nil, nil)
	plan.settlement = NewUsageSettlementService(stub, nil, nil)

	err := plan.ReserveBudget(context.Background(), RequestBudgetEstimate{Model: "claude-sonnet-4"})
	require.ErrorIs(t, err, ErrAPIKeyBudgetExceeded)
	require.Nil(t, plan.BudgetHold(), "a rejected request holds nothing")
	require.False(t, plan.BudgetAdmitted())
}

func TestGatewayRequestPlan_ReserveBudgetFailsClosedWithoutSettlement(t *testing.T) {
	plan := NewGatewayRequestPlan(GatewayRequestSpec{UserID: 3, APIKeyID: 7, ExecutionID: "req-plan-3"})
	require.ErrorIs(t, plan.ReserveBudget(context.Background(), RequestBudgetEstimate{Model: "claude-sonnet-4"}), ErrAPIKeyBudgetUnavailable)
}

func TestGatewayRequestPlan_ReleaseRefundsAnUnsettledHold(t *testing.T) {
	stub := &keyBudgetReceiptStub{}
	plan := NewGatewayRequestPlan(GatewayRequestSpec{UserID: 3, APIKeyID: 7, ExecutionID: "req-plan-4"})
	plan.billing = NewBillingService(nil, nil)
	plan.settlement = NewUsageSettlementService(stub, nil, nil)
	require.NoError(t, plan.ReserveBudget(context.Background(), RequestBudgetEstimate{Model: "claude-sonnet-4"}))

	plan.Finalize(false)
	require.Equal(t, []string{"req-plan-4"}, stub.released, "an unsettled hold is refunded when the request ends")

	// A settled receipt already consumed the hold: finalize must not refund it.
	stub.released = nil
	settledPlan := NewGatewayRequestPlan(GatewayRequestSpec{UserID: 3, APIKeyID: 7, ExecutionID: "req-plan-5"})
	settledPlan.billing = NewBillingService(nil, nil)
	settledPlan.settlement = NewUsageSettlementService(stub, nil, nil)
	require.NoError(t, settledPlan.ReserveBudget(context.Background(), RequestBudgetEstimate{Model: "claude-sonnet-4"}))
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

func TestBudgetMeterUsesFrozenMediaUnitsAndCeilsOnce(t *testing.T) {
	perImage := 0.04
	perMillion := 0.00001
	perHour := 3.6
	perSecond := 0.2525
	group := &Group{ID: 1, ImagePrice1K: &perImage, AudioTTSPricePerMillionChars: &perMillion, AudioSTTPricePerHour: &perHour, VideoPrice720P: &perSecond}
	for _, tc := range []struct {
		name  string
		meter BudgetMeter
		want  string
	}{
		{"images", BudgetMeter{Unit: "image", Size: "1024x1024", Count: 3, Numerator: 1, Denominator: 1}, "0.12000000"},
		{"one character below ledger scale", BudgetMeter{Unit: "character", Count: 1, Numerator: 1, Denominator: 1}, "0.00000001"},
		{"audio seconds", BudgetMeter{Unit: "second", Count: 1, Numerator: 12345, Denominator: 1000}, "0.01234500"},
		{"video seconds", BudgetMeter{Unit: "video_second", Size: "720p", Count: 2, Numerator: 7, Denominator: 1}, "3.53500000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := NewGatewayRequestPlan(GatewayRequestSpec{})
			plan.billing = NewBillingService(nil, nil)
			amount := plan.estimateBudgetHold(t.Context(), RequestBudgetEstimate{Model: "media-fixture", Group: group, Meter: &tc.meter, RateMultiplier: 1})
			require.NotNil(t, amount)
			require.Equal(t, tc.want, amount.String())
		})
	}
}

func TestImageBudgetHonorsFrozenQualityTariff(t *testing.T) {
	low, high := 0.01, 0.08
	group := &Group{ID: 1, ModelPricing: []ChannelModelPricing{{
		Models: []string{"image-fixture"}, BillingMode: BillingModeImage,
		PriceVariants: []ModelPriceVariant{{ID: "image-fixture", Canonical: "image-fixture", Prices: []ModelUnitPrice{
			{Unit: "image", Dimension: "output", Currency: "USD", Sales: &low},
			{Unit: "image", Dimension: "output", Currency: "USD", Conditions: "high", Sales: &high},
		}}},
	}}}
	plan := NewGatewayRequestPlan(GatewayRequestSpec{})
	plan.billing = NewBillingService(nil, nil)
	for _, tc := range []struct{ quality, want string }{{"high", "0.16000000"}, {"low", "0.02000000"}, {"auto", "0.16000000"}} {
		amount := plan.estimateBudgetHold(t.Context(), RequestBudgetEstimate{Model: "image-fixture", Group: group, Meter: &BudgetMeter{Unit: "image", Size: "1024x1024", Quality: tc.quality, Count: 2, Numerator: 1, Denominator: 1}})
		require.NotNil(t, amount)
		require.Equal(t, tc.want, amount.String())
	}
}

func TestKeyHardBudgetRejectsMissingPriceAndAdmitsExplicitFree(t *testing.T) {
	zero := 0.
	for _, tc := range []struct {
		name     string
		group    *Group
		admitted bool
	}{
		{"unknown model", nil, false}, {"unknown prefix match", nil, false},
		{"explicitly free", &Group{ID: 1, ModelPricing: []ChannelModelPricing{{Models: []string{"unknown model"}, InputPrice: &zero, OutputPrice: &zero}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &keyBudgetReceiptStub{}
			plan := NewGatewayRequestPlan(GatewayRequestSpec{ExecutionID: "budget", UserID: 1, APIKeyID: 2})
			plan.settlement = NewUsageSettlementService(repo, nil, nil)
			plan.billing = NewBillingService(nil, nil)
			model := "unknown model"
			if tc.name == "unknown prefix match" {
				model = "claude-unknown model"
			}
			err := plan.ReserveBudget(t.Context(), RequestBudgetEstimate{Model: model, Group: tc.group})
			if tc.admitted {
				require.NoError(t, err)
				require.Len(t, repo.reservations, 1)
				require.Zero(t, repo.reservations[0].HoldAmount)
			} else {
				require.ErrorIs(t, err, ErrGatewayBudgetModelNotFound)
				require.Empty(t, repo.reservations)
			}
		})
	}
}

func TestBudgetUpperBoundIncludesCandidatesAndHighestCacheWriteRate(t *testing.T) {
	input, output, write, write1h := 0.000001, 0.000002, 0.000003, 0.00001
	group := &Group{ID: 1, ModelPricing: []ChannelModelPricing{{Models: []string{"bound-model"}, InputPrice: &input, OutputPrice: &output, CacheWritePrice: &write, CacheWrite1hPrice: &write1h}}}
	plan := NewGatewayRequestPlan(GatewayRequestSpec{})
	plan.billing = NewBillingService(nil, nil)
	amount := plan.estimateBudgetHold(t.Context(), RequestBudgetEstimate{Model: "bound-model", Group: group, InputTokens: 100, OutputTokens: 300, CacheCreationTokens: 100})
	require.NotNil(t, amount)
	require.Equal(t, "0.00170000", amount.String())
}

func TestGatewayBudgetModelUnknownRequiresBillingAndRegistryEvidence(t *testing.T) {
	plan := NewGatewayRequestPlan(GatewayRequestSpec{})
	require.False(t, GatewayBudgetModelUnknown(plan, RequestBudgetEstimate{Model: "unknown model"}), "unwired billing is an availability fault")

	plan.billing = NewBillingService(nil, nil)
	require.True(t, GatewayBudgetModelUnknown(plan, RequestBudgetEstimate{Model: "unknown model"}))
	require.False(t, GatewayBudgetModelUnknown(plan, RequestBudgetEstimate{Model: "unknown model", Meter: &BudgetMeter{Unit: "image"}}))

	known := ""
	for key, record := range plan.ModelRegistrySnapshot().CapabilitySnapshot().records {
		if _, model, ok := strings.Cut(key, "\x00"); ok && (known == "" || record.MaxOutputTokens == nil) {
			known = model
		}
	}
	require.NotEmpty(t, known)
	require.False(t, GatewayBudgetModelUnknown(plan, RequestBudgetEstimate{Model: known}), "a registered model is never model_not_found")

	plan.registrySnapshot = nil
	require.False(t, GatewayBudgetModelUnknown(plan, RequestBudgetEstimate{Model: "unknown model"}), "a missing registry is an availability fault")
}
