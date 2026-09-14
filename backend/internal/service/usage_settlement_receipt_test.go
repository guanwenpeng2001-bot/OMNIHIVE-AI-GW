package service

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrepareUsageSettlementReceiptValidatesFrozenMoney(t *testing.T) {
	fixture := func(t *testing.T) *UsageSettlementReceipt {
		t.Helper()
		amount := .000078125
		snapshot, err := json.Marshal(PricingSnapshot{PricingState: PricingStatePriced, UnitPrice: &amount, Units: 1, RateMultiplier: 1, TotalCost: amount, ActualCost: amount, ChargedAmount: QuantizeUsageBillingAmount(amount)})
		require.NoError(t, err)
		return &UsageSettlementReceipt{
			Key:             UsageSettlementKey{RequestID: "receipt", APIKeyID: 2, UserID: 1},
			Command:         UsageBillingCommand{RequestID: "receipt", APIKeyID: 2, UserID: 1, AccountID: 3, Model: "model", BalanceCost: amount},
			Usage:           UsageLog{RequestID: "receipt", APIKeyID: 2, UserID: 1, AccountID: 3, Model: "model", PricingState: PricingStatePriced, PricingSnapshot: string(snapshot), TotalCost: amount, ActualCost: amount},
			ResolutionState: "resolved", CompletionState: "complete",
		}
	}
	t.Run("raw_fingerprint_before_quantization", func(t *testing.T) {
		input := fixture(t)
		expected := buildUsageBillingFingerprint(&input.Command)
		prepared, err := PrepareUsageSettlementReceipt(input)
		require.NoError(t, err)
		require.Equal(t, expected, prepared.Command.RequestFingerprint)
		require.Equal(t, .00007813, prepared.Command.BalanceCost)
		require.Equal(t, prepared.Command.BalanceCost, prepared.Usage.ActualCost)
		require.Equal(t, .000078125, input.Command.BalanceCost)
		replayed, err := PrepareUsageSettlementReceipt(prepared)
		require.NoError(t, err)
		require.Equal(t, expected, replayed.Command.RequestFingerprint)
	})
	for name, amount := range map[string]float64{"negative_sub_quantum": -1e-10, "negative": -1, "nan": math.NaN(), "infinity": math.Inf(1), "overflow": 1e12} {
		t.Run(name, func(t *testing.T) {
			input := fixture(t)
			input.Command.BalanceCost = amount
			_, err := PrepareUsageSettlementReceipt(input)
			require.ErrorIs(t, err, ErrInvalidBillingAmount)
		})
	}
	for name, mutate := range map[string]func(*UsageSettlementReceipt){
		"effect_different_from_frozen_charge": func(r *UsageSettlementReceipt) { r.Command.BalanceCost = 1 },
		"log_different_from_frozen_charge":    func(r *UsageSettlementReceipt) { r.Usage.ActualCost = 1 },
		"quote_different_from_frozen_quote":   func(r *UsageSettlementReceipt) { r.Usage.TotalCost = 1 },
		"negative_usage":                      func(r *UsageSettlementReceipt) { r.Usage.InputTokens = -1; r.Command.InputTokens = -1 },
		"price_state_mismatch":                func(r *UsageSettlementReceipt) { r.Usage.PricingState = PricingStateFree },
	} {
		t.Run(name, func(t *testing.T) {
			input := fixture(t)
			mutate(input)
			_, err := PrepareUsageSettlementReceipt(input)
			require.ErrorIs(t, err, ErrInvalidBillingAmount)
		})
	}
	t.Run("ownership_conflict", func(t *testing.T) {
		input := fixture(t)
		input.Usage.UserID++
		_, err := PrepareUsageSettlementReceipt(input)
		require.ErrorIs(t, err, ErrUsageBillingRequestConflict)
	})
}
