package quotes

import (
	"context"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
	"github.com/stretchr/testify/require"
)

type storedSaleVersions []pricing.Version

func (s storedSaleVersions) At(context.Context, string, time.Time) ([]pricing.Version, error) {
	return s, nil
}

func TestPublishedWalletVersionMustMatchOperatorSaleBundle(t *testing.T) {
	input, err := money.NewRate("USD", "0.01", "input_token", "1")
	require.NoError(t, err)
	pc := pricing.PriceContext{Provider: "p", Model: "same", GroupID: 7, Funding: "wallet", PricingAt: time.Now()}
	d := catalog.Detail{Model: catalog.Summary{Provider: "p", Model: "same"}, GroupPrices: map[int64]catalog.Price{7: {Kind: "sale", State: "priced", Source: "group", Input: &input}}}
	projected, err := ResolveSaleVersion(context.Background(), nil, pc, d)
	require.NoError(t, err)
	published := projected
	published.ID = "published-compatible"
	resolved, err := ResolveSaleVersion(context.Background(), storedSaleVersions{published}, pc, d)
	require.NoError(t, err)
	require.Equal(t, published.ID, resolved.ID)
	changed, err := money.NewRate("USD", "10", "input_token", "1")
	require.NoError(t, err)
	published.Rules = []pricing.Rule{{Kind: "input", Rate: changed}}
	_, err = ResolveSaleVersion(context.Background(), storedSaleVersions{published}, pc, d)
	var conflict *fault.Fault
	require.ErrorAs(t, err, &conflict)
	require.Equal(t, 409, conflict.Status)
	require.Equal(t, "PRICE_QUOTE_CHANGED", conflict.Reason)
}

func TestKeySaleUsesOnlyItsGroupAndStableContentVersion(t *testing.T) {
	input, _ := money.NewRate("USD", "0.01", "input_token", "1")
	output, _ := money.NewRate("USD", "0.02", "output_token", "1")
	pc := pricing.PriceContext{WorkspaceID: 1, ProjectID: 2, KeyID: 4, GroupID: 7, Provider: "p", Model: "same", Funding: "wallet", PricingAt: time.Now()}
	d := catalog.Detail{Model: catalog.Summary{Provider: "p", Model: "same"}, GroupPrices: map[int64]catalog.Price{7: {Kind: "sale", State: "priced", Source: "group", Input: &input, Output: &output}, 8: {Kind: "reference", State: "reference", Source: "pi-ai", Input: &input}}}
	a, err := ResolveSaleVersion(context.Background(), nil, pc, d)
	require.NoError(t, err)
	require.NotEmpty(t, a.ID)
	pc.PricingAt = pc.PricingAt.Add(time.Second)
	b, err := ResolveSaleVersion(context.Background(), nil, pc, d)
	require.NoError(t, err)
	require.Equal(t, a.ID, b.ID)
	pc.GroupID = 8
	_, err = ResolveSaleVersion(context.Background(), nil, pc, d)
	require.ErrorIs(t, err, pricing.ErrReferencePrice)
	pc.GroupID = 9
	missing, err := ResolveSaleVersion(context.Background(), nil, pc, d)
	require.NoError(t, err)
	require.Empty(t, missing.ID)
	pc.GroupID = 7
	pc.Provider = "other"
	_, err = ResolveSaleVersion(context.Background(), nil, pc, d)
	require.Error(t, err)
}

func TestReferenceOnlyCatalogCannotQuoteOrProduceCharge(t *testing.T) {
	d := catalog.Detail{Model: catalog.Summary{Provider: "p", Model: "reference-only", PriceState: "unpriced"}, ReferencePrice: &catalog.Price{Kind: "reference", State: "reference", Source: "pi-ai"}}
	pc := pricing.PriceContext{Provider: "p", Model: "reference-only", GroupID: 1, Funding: "wallet", PricingAt: time.Now()}
	v, err := ResolveSaleVersion(context.Background(), nil, pc, d)
	require.NoError(t, err)
	require.Empty(t, v.ID)
	result, err := (pricing.Frozen{Version: v, At: pc.PricingAt}).Calculate()
	require.NoError(t, err)
	require.Equal(t, "unpriced", result.State)
	require.Nil(t, result.Charge)
}
