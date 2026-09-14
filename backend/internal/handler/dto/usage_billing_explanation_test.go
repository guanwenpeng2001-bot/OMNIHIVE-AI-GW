package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageBillingExplanationHistoricalAndFreeInterrupted(t *testing.T) {
	log := &service.UsageLog{ActualCost: 4}
	require.Nil(t, UsageLogFromService(log).BillingExplanation)
	log.ActualCost = 0
	require.Nil(t, UsageLogFromService(log).BillingExplanation)
	log.BillingEvidence = &service.UsageBillingEvidence{PricingState: "free", SettlementState: "settled", CompletionState: "interrupted", Snapshot: `{"model":"internal-model","provider":"internal-provider","account_rate_multiplier":3,"account_stats_cost":9,"source":"frozen-source","price_card_version":"v1","charged_amount":0}`}
	user := UsageLogFromService(log)
	require.Equal(t, "free", user.BillingExplanation.PricingState)
	require.Equal(t, "interrupted", user.BillingExplanation.CompletionState)
	encoded, err := json.Marshal(user)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "internal-model")
	require.NotContains(t, string(encoded), "account_stats_cost")
	require.Contains(t, string(encoded), "frozen-source")
	admin := UsageLogFromServiceAdmin(log)
	require.Equal(t, 3.0, admin.BillingExplanation.Snapshot.AccountRateMultiplier)
	log.BillingEvidence.CompletionState = "complete"
	require.Equal(t, "complete", UsageLogFromService(log).BillingExplanation.CompletionState)
}
