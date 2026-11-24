//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/stretchr/testify/require"
)

func TestGatewayBudgetBindsFreeTariffToSelectedAccount(t *testing.T) {
	const model = "kilo-auto/free"
	free := freeAccount(PlatformKilo, KiloAccountModeFree)
	paid := freeAccount(PlatformKilo, KiloAccountModeAPIKey)
	price := 1.0
	group := &Group{ID: 7, Platform: PlatformKilo, RateMultiplier: 1,
		ModelPricing: []ChannelModelPricing{{Models: []string{model}, InputPrice: &price, OutputPrice: &price}}}
	plan := NewGatewayRequestPlan(GatewayRequestSpec{Model: model, UserID: 1, APIKeyID: 2, ExecutionID: "test"})
	plan.billing = NewBillingService(&config.Config{}, nil)
	plan.catalogRepo = &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{1: free, 2: paid}}
	plan.budgetPlatform = PlatformKilo
	plan.budgetEstimate = &RequestBudgetEstimate{Group: group, InputTokens: 1, OutputTokens: 1, SkipKeyBudget: true}

	ctx := withTPMWire(WithGatewayRequestPlan(t.Context(), plan), []byte(`{"model":"kilo-auto/free"}`), "/chat/completions")
	require.NoError(t, plan.admitPublicModelLane(ctx, 1))
	require.ErrorIs(t, plan.admitPublicModelLane(ctx, 2), ErrAPIKeyBudgetUnavailable)
	require.True(t, plan.Record())
	cost, err := plan.billing.CalculateCostUnified(CostInput{Ctx: ctx, Model: model, RateMultiplier: 1,
		Tokens: UsageTokens{InputTokens: 1, OutputTokens: 1, CacheReadTokens: 1}})
	require.NoError(t, err)
	require.Equal(t, PricingStateFree, cost.PricingState)
	require.Zero(t, cost.ActualCost)
	require.False(t, verifiedKiloFreeTariff(free, "unverified/free"))
}

func TestGatewayBudgetAdmitsOpenRouterFreeTariff(t *testing.T) {
	const model = "baidu/ernie-5.5-flash-preview:free"
	account := &Account{ID: 16, Platform: PlatformOpenRouter, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "key"}}
	free := true
	account.Extra = map[string]any{UpstreamModelCatalogExtraKey: &AccountModelCatalogSnapshot{Complete: true, SourceKind: "live", DiscoveredModels: []string{model}}}
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{model: {ID: model, Free: &free}}})
	group := &Group{ID: 10, Platform: PlatformOpenRouter, RateMultiplier: 1}
	plan := NewGatewayRequestPlan(GatewayRequestSpec{Model: model, UserID: 1, APIKeyID: 2, ExecutionID: "test-openrouter-free"})
	plan.billing = NewBillingService(&config.Config{}, nil)
	plan.catalogRepo = &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{16: account}}
	plan.budgetPlatform = PlatformOpenRouter
	plan.budgetEstimate = &RequestBudgetEstimate{Group: group, InputTokens: 1, OutputTokens: 1, SkipKeyBudget: true}
	ctx := withTPMWire(WithGatewayRequestPlan(context.Background(), plan), []byte(`{"model":"baidu/ernie-5.5-flash-preview:free"}`), "/chat/completions")
	require.NoError(t, plan.admitPublicModelLane(ctx, 16))
	lane := plan.selectedPrice.Load()
	require.NotNil(t, lane)
	require.Equal(t, "official_free:openrouter", lane.resolved.Source)
	require.Equal(t, 0, lane.amount.Amount().Compare(money.Amount{}))

	account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 1}
	require.ErrorIs(t, plan.admitPublicModelLane(ctx, 16), ErrModelPricingUnavailable, "stale discovery cannot establish a free tariff")
}

func TestGatewayBudgetPricesOpenRouterMappedWireModel(t *testing.T) {
	const alias = "promo:free"
	const wireModel = "openai/gpt-5"
	account := &Account{ID: 16, Platform: PlatformOpenRouter, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "key", "model_mapping": map[string]string{alias: wireModel}}}
	group := &Group{ID: 10, Platform: PlatformOpenRouter, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenRouter, Models: []string{wireModel}, BillingMode: BillingModeToken, InputPrice: priceFactNumber(1e-6), OutputPrice: priceFactNumber(1e-6)}}}
	plan := NewGatewayRequestPlan(GatewayRequestSpec{Model: alias, UserID: 1, APIKeyID: 2, ExecutionID: "openrouter-mapped-paid"})
	plan.billing = NewBillingService(&config.Config{}, nil)
	plan.catalogRepo = &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{16: account}}
	plan.budgetPlatform = PlatformOpenRouter
	plan.budgetEstimate = &RequestBudgetEstimate{Group: group, InputTokens: 1, OutputTokens: 1, SkipKeyBudget: true}
	ctx := withTPMWire(WithGatewayRequestPlan(context.Background(), plan), []byte(`{"model":"promo:free"}`), "/chat/completions")
	require.NoError(t, plan.admitPublicModelLane(ctx, 16))
	lane := plan.selectedPrice.Load()
	require.NotNil(t, lane)
	require.Equal(t, wireModel, lane.model)
	require.NotEqual(t, "free_lane", lane.resolved.baseSource)
	require.Equal(t, "0.00000400", lane.amount.String())

	account.Credentials["model_mapping"] = map[string]string{alias: "vendor/free-model:free"}
	require.ErrorIs(t, plan.admitPublicModelLane(ctx, 16), ErrModelPricingUnavailable, "a free suffix without current discovered free metadata is unpriced")
}
