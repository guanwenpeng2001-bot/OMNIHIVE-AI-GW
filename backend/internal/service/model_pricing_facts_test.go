package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func priceFactNumber(value float64) *float64 { return &value }

func pricingFactsGroup() *Group {
	return &Group{ID: 12, Hydrated: true, Platform: PlatformQianwen, Status: StatusActive, RateMultiplier: 1, ProfitControlEnabled: true, ProfitMinMargin: .2,
		ModelPricing: []ChannelModelPricing{{Platform: PlatformQianwen, Models: []string{"native"}, PriceVariants: []ModelPriceVariant{{
			ID: "native", Canonical: "canonical", Aliases: []string{"public-alias"}, Lifecycle: "active", Prices: []ModelUnitPrice{
				{Unit: "token", Dimension: "input", Currency: "USD", Sales: priceFactNumber(2), Cost: priceFactNumber(1)},
				{Unit: "token", Dimension: "output", Currency: "USD", Sales: priceFactNumber(4), Cost: priceFactNumber(2)},
				{Unit: "character", Dimension: "output", Currency: "USD", Sales: priceFactNumber(.02), Cost: priceFactNumber(.01)},
				{Unit: "image", Dimension: "output", Currency: "USD", Sales: priceFactNumber(.2), Cost: priceFactNumber(.1)},
				{Unit: "second", Dimension: "output", Currency: "USD", Sales: priceFactNumber(.04), Cost: priceFactNumber(.02)},
			},
		}}}}}
}

func TestPricingFactsUnitsAliasesExpiryAndFreeze(t *testing.T) {
	group := pricingFactsGroup()
	resolver := NewModelPricingResolver(nil, nil)
	pricingAt := time.Now()
	contextWithPlan := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{PricingAt: pricingAt}))
	for _, model := range []string{"native", "canonical", "public-alias"} {
		descriptor, err := resolver.ResolveDescriptor(contextWithPlan, PricingInput{Model: model, Group: group})
		require.NoError(t, err)
		require.Len(t, descriptor.Variants[0].Prices, 5)
		for _, unit := range []string{"token", "character", "image", "second"} {
			price, err := descriptor.Lookup("native", unit, "output", "")
			require.NoError(t, err)
			require.Greater(t, *price.Sales, *price.Cost)
		}
		before, err := json.Marshal(descriptor)
		require.NoError(t, err)
		*group.ModelPricing[0].PriceVariants[0].Prices[0].Sales = 9
		after, err := json.Marshal(descriptor)
		require.NoError(t, err)
		require.Equal(t, before, after)
	}
	expiry := pricingAt.Add(-time.Second)
	group.ModelPricing[0].PriceVariants[0].ExpiresAt = &expiry
	group.ModelPricing[0].PriceVariants = append(group.ModelPricing[0].PriceVariants, ModelPriceVariant{ID: "sibling", Canonical: "sibling", Prices: []ModelUnitPrice{{Unit: "image", Dimension: "output", Currency: "USD", Sales: priceFactNumber(1)}}})
	descriptor, err := resolver.ResolveDescriptor(contextWithPlan, PricingInput{Model: "native", Group: group})
	require.NoError(t, err)
	_, err = descriptor.UnitPrice("token", "input", "")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Nil(t, resolver.Resolve(contextWithPlan, PricingInput{Model: "native", Group: group}).BasePricing)
	sibling, err := resolver.ResolveDescriptor(contextWithPlan, PricingInput{Model: "sibling", Group: group})
	require.NoError(t, err)
	_, err = sibling.UnitPrice("image", "output", "")
	require.NoError(t, err)
}

func TestPricingFactsMissingAndExplicitFree(t *testing.T) {
	resolver := NewModelPricingResolver(nil, nil)
	_, err := resolver.ResolveDescriptor(context.Background(), PricingInput{Model: "missing"})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	group := pricingFactsGroup()
	group.ModelPricing[0].PriceVariants[0].Prices[0].Sales = priceFactNumber(0)
	descriptor, err := resolver.ResolveDescriptor(context.Background(), PricingInput{Model: "native", Group: group})
	require.NoError(t, err)
	price, err := descriptor.UnitPrice("token", "input", "")
	require.NoError(t, err)
	require.Zero(t, *price.Sales)
	_, err = descriptor.UnitPrice("request", "output", "")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestPricingFactsProfitControlUsesSalesAndCostAcrossPlatforms(t *testing.T) {
	for _, platform := range []string{PlatformHunyuan, PlatformMistral, PlatformQianfan, PlatformVolcengine, PlatformAnthropic, PlatformBedrock} {
		t.Run(platform, func(t *testing.T) {
			group := pricingFactsGroup()
			group.Platform = platform
			group.ModelPricing[0].Platform = platform
			require.True(t, profitControlPlatformSupported(platform))
			requestContext := context.WithValue(context.Background(), ctxkey.Group, group)
			requestContext = WithGatewayRequestPlan(requestContext, NewGatewayRequestPlan(GatewayRequestSpec{Model: "native", PricingAt: time.Now()}))
			gateway := &OpenAIGatewayService{resolver: NewModelPricingResolver(nil, nil)}
			gate := gateway.resolveOpenAIProfitControlGate(requestContext, &group.ID)
			require.NotNil(t, gate)
			requestContext = context.WithValue(requestContext, openAIProfitControlGateCtxKey{}, gate)
			account := &Account{Platform: platform, Type: AccountTypeBedrock, RateMultiplier: priceFactNumber(1)}
			vetoed, reason := OpenAIProfitControlVeto(requestContext, account)
			require.False(t, vetoed, reason)
			group.ModelPricing[0].PriceVariants[0].Prices[0].Cost = priceFactNumber(3)
			vetoed, reason = OpenAIProfitControlVeto(requestContext, account)
			require.False(t, vetoed, "existing request keeps its frozen quote")
			gate = gateway.resolveOpenAIProfitControlGate(requestContext, &group.ID)
			requestContext = context.WithValue(requestContext, openAIProfitControlGateCtxKey{}, gate)
			vetoed, reason = OpenAIProfitControlVeto(requestContext, account)
			require.True(t, vetoed)
			require.Equal(t, openAIProfitFilterReasonThreshold, reason)
			group.ModelPricing[0].PriceVariants[0].Prices[0].Cost = nil
			gate = gateway.resolveOpenAIProfitControlGate(requestContext, &group.ID)
			requestContext = context.WithValue(requestContext, openAIProfitControlGateCtxKey{}, gate)
			vetoed, reason = OpenAIProfitControlVeto(requestContext, account)
			require.True(t, vetoed)
			require.Equal(t, "profit_pricing_unavailable", reason)
		})
	}
}

func TestPricingFactsCloneSeparatesConditionalPointers(t *testing.T) {
	card := pricingFactsGroup().ModelPricing[0]
	card.ThinkingPricing = &QianwenModePricing{InputPrice: priceFactNumber(1), ExplicitCachePricing: &QianwenModePricing{CacheReadPrice: priceFactNumber(2)}, Intervals: []PricingInterval{{InputPrice: priceFactNumber(3)}}}
	cloned := card.Clone()
	*card.ThinkingPricing.InputPrice = 5
	*card.ThinkingPricing.ExplicitCachePricing.CacheReadPrice = 6
	*card.ThinkingPricing.Intervals[0].InputPrice = 7
	require.Equal(t, 1.0, *cloned.ThinkingPricing.InputPrice)
	require.Equal(t, 2.0, *cloned.ThinkingPricing.ExplicitCachePricing.CacheReadPrice)
	require.Equal(t, 3.0, *cloned.ThinkingPricing.Intervals[0].InputPrice)
}

func TestPricingFactsConflictByBillingUnit(t *testing.T) {
	entries := []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"same"}, BillingMode: BillingModeToken}, {Platform: PlatformOpenAI, Models: []string{"same"}, BillingMode: BillingModeImage}}
	require.NoError(t, validateNoConflictingModels(entries))
	entries[1].BillingMode = BillingModeToken
	require.Error(t, validateNoConflictingModels(entries))
}

func TestPricingFactsCatalogRefreshKeepsRequestAndCostFrozen(t *testing.T) {
	group := pricingFactsGroup()
	entry := &LiteLLMModelPricing{LiteLLMProvider: PlatformQianwen, PriceVariants: cloneModelPriceVariants(group.ModelPricing[0].PriceVariants), TokenPricingAbsent: true}
	prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{"native": entry}}
	billing := &BillingService{pricingService: prices}
	channels := &ChannelService{}
	cache := newEmptyChannelCache()
	cache.loadedAt = time.Now()
	cache.channelByGroupID[group.ID] = &Channel{ID: 1, Status: StatusActive}
	cache.groupPlatform[group.ID] = PlatformQianwen
	channels.cache.Store(cache)
	requestContext, _ := captureGatewayRequestPlan(context.Background(), GatewayRequestSpec{Model: "native"}, nil, billing, channels)
	resolver := NewModelPricingResolver(channels, billing)
	descriptor, err := resolver.ResolveDescriptor(requestContext, PricingInput{Model: "native", Group: &Group{Platform: PlatformQianwen}})
	require.NoError(t, err)
	prices.mu.Lock()
	prices.pricingData = map[string]*LiteLLMModelPricing{}
	prices.mu.Unlock()
	resolved := resolver.Resolve(requestContext, PricingInput{Model: "native", Group: &Group{Platform: PlatformQianwen}})
	require.Equal(t, 2.0, resolved.BasePricing.InputPricePerToken)
	cost := resolveAccountStatsCost(requestContext, channels, billing, 19, group.ID, "native", UsageTokens{InputTokens: 3, OutputTokens: 2}, 1, 14, "", time.Now())
	require.NotNil(t, cost)
	require.Equal(t, 7.0, *cost)
	price, err := descriptor.UnitPrice("image", "output", "")
	require.NoError(t, err)
	require.Equal(t, .2, *price.Sales)
	_, err = resolver.ResolveDescriptor(context.Background(), PricingInput{Model: "native", Group: &Group{Platform: PlatformQianwen}})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestPricingFactsRealtimeReceiptRetainsSeparateFrozenCost(t *testing.T) {
	group := pricingFactsGroup()
	model := "qwen3-tts-flash-realtime"
	group.ModelPricing[0].Models = []string{model}
	group.ModelPricing[0].PriceVariants[0].ID = model
	account := &Account{ID: 19, Platform: PlatformQianwen, Type: AccountTypeAPIKey, RateMultiplier: priceFactNumber(1)}
	key := &APIKey{ID: 2, Group: group, GroupID: &group.ID, Quota: 10}
	snapshot, err := QianwenRealtimeBillingSnapshot(group, account, key, model, nil)
	require.NoError(t, err)
	snapshot.Realtime.SessionID = "frozen-session"
	job := &UpstreamJob{Platform: PlatformQianwen, PublicRequestID: "receipt", UserID: 1, APIKeyID: 2, AccountID: 19, Billing: snapshot, UsageJSON: json.RawMessage(`{"characters":25}`)}
	*group.ModelPricing[0].PriceVariants[0].Prices[2].Sales = 90
	*group.ModelPricing[0].PriceVariants[0].Prices[2].Cost = 80
	log, _, err := QianwenRealtimeCharge(job)
	require.NoError(t, err)
	require.Equal(t, .5, log.TotalCost)
	require.Equal(t, .25, *log.AccountStatsCost)
	replayed, _, err := QianwenRealtimeCharge(job)
	require.NoError(t, err)
	require.Equal(t, log.TotalCost, replayed.TotalCost)
	require.Equal(t, *log.AccountStatsCost, *replayed.AccountStatsCost)
}

func TestPricingFactsNativeAudioExactUnitsAndMissing(t *testing.T) {
	group := pricingFactsGroup()
	model := "qwen3-tts-flash"
	group.ModelPricing[0].PriceVariants[0].ID = model
	gateway := &OpenAIGatewayService{resolver: NewModelPricingResolver(nil, nil)}
	key := &APIKey{Group: group, GroupID: &group.ID}
	config, err := gateway.qianwenNativeAudioPrice(context.Background(), key, model)
	require.NoError(t, err)
	require.Equal(t, 20000.0, *config.TTSPerMChars)
	group.ModelPricing[0].PriceVariants[0].Prices[2].Sales = nil
	_, err = gateway.qianwenNativeAudioPrice(context.Background(), key, model)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestPricingFactsLatePlanAndTinyPriceProfitMargin(t *testing.T) {
	group := pricingFactsGroup()
	requestContext := context.WithValue(context.Background(), ctxkey.Group, group)
	gateway := &OpenAIGatewayService{resolver: NewModelPricingResolver(nil, nil)}
	gate := gateway.resolveOpenAIProfitControlGate(requestContext, &group.ID)
	require.NotNil(t, gate)
	require.Nil(t, gate.descriptor)
	requestContext = context.WithValue(requestContext, openAIProfitControlGateCtxKey{}, gate)
	requestContext = WithGatewayRequestPlan(requestContext, NewGatewayRequestPlan(GatewayRequestSpec{Model: "native", PricingAt: time.Now()}))
	account := &Account{RateMultiplier: priceFactNumber(2)}
	vetoed, reason := OpenAIProfitControlVeto(requestContext, account)
	require.True(t, vetoed)
	require.Equal(t, openAIProfitFilterReasonThreshold, reason)
	require.NotNil(t, gate.descriptor)
	require.True(t, profitPriceOverThreshold(2e-12, 1e-12))
	require.False(t, profitPriceOverThreshold(1e-12, 1e-12))
}

func TestPricingFactsMergeUnitsWithoutOverwritingVariantLifecycle(t *testing.T) {
	group := pricingFactsGroup()
	second := group.ModelPricing[0].Clone()
	second.BillingMode = BillingModeImage
	second.PriceVariants[0].Prices = second.PriceVariants[0].Prices[3:]
	group.ModelPricing[0].PriceVariants[0].Prices = group.ModelPricing[0].PriceVariants[0].Prices[:3]
	group.ModelPricing = append(group.ModelPricing, second)
	resolver := NewModelPricingResolver(nil, nil)
	descriptor, err := resolver.ResolveDescriptor(context.Background(), PricingInput{Model: "native", Group: group})
	require.NoError(t, err)
	require.Len(t, descriptor.Variants, 1)
	require.Len(t, descriptor.Variants[0].Prices, 5)
	group.ModelPricing[1].PriceVariants[0].Lifecycle = "retired"
	_, err = resolver.ResolveDescriptor(context.Background(), PricingInput{Model: "native", Group: group})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestPricingFactsSourceIndexesOnlyUnambiguousAliases(t *testing.T) {
	prices := &PricingService{}
	data, err := prices.parsePricingData([]byte(`{"native":{"litellm_provider":"qianwen","price_variants":[{"id":"native","canonical":"canonical","aliases":["alias"],"prices":[{"unit":"token","dimension":"input","currency":"USD","sales":1,"cost":0.5},{"unit":"character","dimension":"output","currency":"USD","sales":0.1,"cost":0.01}]}]}}`))
	require.NoError(t, err)
	prices.pricingData = data
	for _, name := range []string{"native", "canonical", "alias"} {
		require.Len(t, prices.modelPriceVariants(name, PlatformQianwen), 1)
		require.Empty(t, prices.modelPriceVariants(name, PlatformOpenAI))
	}
	data["alias"] = &LiteLLMModelPricing{}
	require.Error(t, indexModelPriceVariants(data))
}
