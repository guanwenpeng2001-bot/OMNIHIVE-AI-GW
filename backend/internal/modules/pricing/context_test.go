package pricing

import (
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestReferenceCannotBecomeExecutableTariff(t *testing.T) {
	rate, err := money.NewRate("USD", "0.01", "input_token", "1")
	require.NoError(t, err)
	quantity, err := money.NewQuantity("input_token", "100")
	require.NoError(t, err)
	for _, v := range []Version{{ID: "reference", Source: "pi-ai", Currency: "USD"}, {ID: "reference", Source: "operator", Kind: "reference", Currency: "USD"}} {
		v.Rules = []Rule{{Kind: "input", Rate: rate}}
		result, err := (Frozen{Version: v, Usage: Usage{Dimensions: []Dimension{{Kind: "input", Quantity: quantity}}}, At: time.Now()}).Calculate()
		require.ErrorIs(t, err, ErrReferencePrice)
		require.Nil(t, result.Charge)
		require.Empty(t, result.Components)
	}
}

func TestPriceContextSeparatesProviderGroupAndFunding(t *testing.T) {
	base := PriceContext{WorkspaceID: 1, ProjectID: 2, GroupID: 3, Provider: "provider-a", Model: "same-name", Funding: "wallet", PricingAt: time.Now()}
	require.NoError(t, base.Validate())
	for _, change := range []func(*PriceContext){func(c *PriceContext) { c.Provider = "provider-b" }, func(c *PriceContext) { c.GroupID = 4 }, func(c *PriceContext) { c.Funding = "byok" }, func(c *PriceContext) { c.WorkspaceID = 2 }, func(c *PriceContext) { c.PayerID = 8 }} {
		other := base
		change(&other)
		require.NotEqual(t, base.ScopeKey(), other.ScopeKey())
	}
	other := base
	other.PricingAt = other.PricingAt.Add(time.Hour)
	other.PolicyVersion = 10
	require.Equal(t, base.ScopeKey(), other.ScopeKey(), "policy changes must not create a competing tariff lineage")
}

func TestPriceContextCannotChargeExternalCostToWallet(t *testing.T) {
	pc := PriceContext{Provider: "provider", Model: "model", Funding: "wallet", PricingAt: time.Now()}
	result, err := (Frozen{Version: Version{ID: "cost", Kind: "external_cost"}, Context: &pc, At: pc.PricingAt}).Calculate()
	require.Error(t, err)
	require.Nil(t, result.Charge)
}
