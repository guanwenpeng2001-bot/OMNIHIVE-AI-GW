//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func projectionCard() ChannelModelPricing {
	return ChannelModelPricing{Models: []string{"projection-model"}, BillingMode: BillingModeToken,
		PriceVariants: []ModelPriceVariant{{ID: "projection-model", Canonical: "projection-model", Prices: []ModelUnitPrice{
			{Unit: "token", Dimension: "input", Currency: "USD", Sales: priceFactNumber(.000002), Cost: priceFactNumber(.000001)},
			{Unit: "token", Dimension: "output", Currency: "USD", Sales: priceFactNumber(.000004), Cost: priceFactNumber(.000002)},
			{Unit: "token", Dimension: "cache_read", Currency: "USD", Sales: priceFactNumber(.0000005), Cost: priceFactNumber(.00000025)},
			{Unit: "token", Dimension: "cache_write", Currency: "USD", Sales: priceFactNumber(.000001), Cost: priceFactNumber(.0000005)},
			{Unit: "image", Dimension: "output", Currency: "USD", Sales: priceFactNumber(.2), Cost: priceFactNumber(.1)},
			{Unit: "second", Dimension: "output", Currency: "USD", Sales: priceFactNumber(.04), Cost: priceFactNumber(.02)},
			{Unit: "request", Dimension: "output", Currency: "USD", Sales: priceFactNumber(.3), Cost: priceFactNumber(.15)},
		}}}}
}

func TestVariantProjectionAmountsAndFrozenReceipts(t *testing.T) {
	card := projectionCard()
	require.NoError(t, validatePricingEntries([]ChannelModelPricing{card}))
	prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{"projection-model": {PriceVariants: cloneModelPriceVariants(card.PriceVariants), TokenPricingAbsent: true}}}
	billing := &BillingService{pricingService: prices}
	channels := &ChannelService{}
	cache := newEmptyChannelCache()
	cache.loadedAt = time.Now()
	cache.channelByGroupID[12] = &Channel{ID: 1, Status: StatusActive}
	channels.cache.Store(cache)
	resolver := NewModelPricingResolver(channels, billing)
	oldContext, _ := captureGatewayRequestPlan(context.Background(), GatewayRequestSpec{Model: "projection-model"}, nil, billing, channels)
	tokens := UsageTokens{InputTokens: 3, OutputTokens: 2, CacheReadTokens: 10, CacheCreationTokens: 4}
	calculate := func(ctx context.Context, expected float64) *CostBreakdown {
		cost, err := billing.CalculateCostUnified(CostInput{Ctx: ctx, Model: "projection-model", Tokens: tokens, RateMultiplier: 2, Resolver: resolver})
		require.NoError(t, err)
		require.InDelta(t, expected, cost.TotalCost, 1e-12)
		require.InDelta(t, expected*2, cost.ActualCost, 1e-12)
		replayed, err := RecalculatePricingSnapshot(cost.PricingSnapshot)
		require.NoError(t, err)
		require.InDelta(t, expected*2, replayed.ActualCost, 1e-12)
		return cost
	}
	cost := calculate(oldContext, .000023)
	require.InDelta(t, .000005, cost.CacheReadCost, 1e-12)
	require.InDelta(t, .000004, cost.CacheCreationCost, 1e-12)
	direct, err := billing.CalculateCost("projection-model", tokens, 2)
	require.NoError(t, err)
	require.Equal(t, cost.ActualCost, direct.ActualCost)
	descriptor, err := resolver.ResolveDescriptor(oldContext, PricingInput{Model: "projection-model"})
	require.NoError(t, err)
	accountCost, err := descriptor.tokenCost(tokens)
	require.NoError(t, err)
	require.InDelta(t, .0000115, accountCost, 1e-12)
	statsCost := resolveAccountStatsCost(oldContext, channels, billing, 19, 12, "projection-model", tokens, 1, cost.TotalCost, "", time.Now(), false)
	require.NotNil(t, statsCost)
	require.InDelta(t, accountCost, *statsCost, 1e-12)

	for index := range card.PriceVariants[0].Prices {
		*card.PriceVariants[0].Prices[index].Sales *= 3
		*card.PriceVariants[0].Prices[index].Cost *= 3
	}
	prices.mu.Lock()
	prices.pricingData = map[string]*LiteLLMModelPricing{"projection-model": {PriceVariants: cloneModelPriceVariants(card.PriceVariants)}}
	prices.mu.Unlock()
	newContext, _ := captureGatewayRequestPlan(context.Background(), GatewayRequestSpec{Model: "projection-model"}, nil, billing, nil)
	calculate(oldContext, .000023)
	calculate(newContext, .000069)
	oldAccountCost, err := descriptor.tokenCost(tokens)
	require.NoError(t, err)
	require.InDelta(t, accountCost, oldAccountCost, 1e-12)
	newDescriptor, err := resolver.ResolveDescriptor(newContext, PricingInput{Model: "projection-model"})
	require.NoError(t, err)
	newAccountCost, err := newDescriptor.tokenCost(tokens)
	require.NoError(t, err)
	require.InDelta(t, .0000345, newAccountCost, 1e-12)
	statsCost = resolveAccountStatsCost(oldContext, channels, billing, 19, 12, "projection-model", tokens, 1, cost.TotalCost, "", time.Now(), false)
	require.NotNil(t, statsCost)
	require.InDelta(t, accountCost, *statsCost, 1e-12)

	for _, tc := range []struct {
		mode         BillingMode
		units, total float64
	}{{BillingModeImage, 3, .6}, {BillingModeVideo, 10.5, .42}, {BillingModePerRequest, 2, .6}} {
		for _, contextCase := range []struct {
			ctx    context.Context
			factor float64
		}{{oldContext, 1}, {newContext, 3}} {
			media, err := billing.CalculateCostUnified(CostInput{Ctx: contextCase.ctx, Model: "projection-model", Mode: tc.mode, UsageUnits: tc.units, RateMultiplier: 2, Resolver: resolver})
			require.NoError(t, err)
			require.InDelta(t, tc.total*contextCase.factor, media.TotalCost, 1e-12)
			replayed, err := RecalculatePricingSnapshot(media.PricingSnapshot)
			require.NoError(t, err)
			require.InDelta(t, tc.total*contextCase.factor*2, replayed.ActualCost, 1e-12)
		}
	}
	batchResolver := &BatchImageModelPricingResolver{Resolver: resolver}
	unit, err := batchResolver.BatchImageUnitPrice(oldContext, &BatchImageJob{Model: "projection-model"})
	require.NoError(t, err)
	require.Equal(t, .2, unit)
	newUnit, err := batchResolver.BatchImageUnitPrice(newContext, &BatchImageJob{Model: "projection-model"})
	require.NoError(t, err)
	require.InDelta(t, .6, newUnit, 1e-12)
	repo := newFakeBatchImageRepository()
	job := testSettlingBatchImageJob("variant-frozen")
	job.Model, job.PricingSnapshotVersion, job.BillableUnitPrice = "projection-model", 1, unit
	repo.jobs[job.BatchID] = job
	charges := &fakeBatchImageBillingRepo{}
	logs := &openAIRecordUsageLogRepoStub{}
	settlement := &BatchImageSettlementService{Repo: repo, BillingRepo: charges, UsageLogRepo: logs, Pricing: batchResolver}
	result, err := settlement.Settle(newContext, job.BatchID)
	require.NoError(t, err)
	require.InDelta(t, float64(job.SuccessCount)*unit, result.ActualCost, 1e-12)
	require.Equal(t, result.ActualCost, logs.lastLog.ActualCost)
	require.Equal(t, result.ActualCost, charges.captures[0].ActualAmount)
}

func TestVariantProjectionMissingAndExplicitZero(t *testing.T) {
	for _, free := range []bool{false, true} {
		card := projectionCard()
		for i := range card.PriceVariants[0].Prices {
			card.PriceVariants[0].Prices[i].Sales = nil
			if free {
				card.PriceVariants[0].Prices[i].Sales = priceFactNumber(0)
			}
		}
		// Keep input/output paid to detect accidental cache fallback or omission.
		card.PriceVariants[0].Prices[0].Sales = priceFactNumber(.000002)
		card.PriceVariants[0].Prices[1].Sales = priceFactNumber(.000004)
		group := &Group{ModelPricing: []ChannelModelPricing{card}}
		resolver := NewModelPricingResolver(nil, nil)
		billing := &BillingService{}
		cost, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "projection-model", Group: group, Tokens: UsageTokens{InputTokens: 3, OutputTokens: 2, CacheReadTokens: 10, CacheCreationTokens: 4}, RateMultiplier: 1, Resolver: resolver})
		if free {
			require.NoError(t, err)
			require.InDelta(t, .000014, cost.TotalCost, 1e-12)
			require.Zero(t, cost.CacheCreationCost)
			require.Zero(t, cost.CacheReadCost)
		} else {
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
			require.Equal(t, PricingStateUnpriced, cost.PricingState)
		}
		for _, mode := range []BillingMode{BillingModeImage, BillingModeVideo, BillingModePerRequest} {
			group.ModelPricing[0].BillingMode = mode
			validation := validatePricingEntries(group.ModelPricing)
			media, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "projection-model", Group: group, Mode: mode, UsageUnits: 5, RateMultiplier: 1, Resolver: resolver})
			if free {
				require.NoError(t, validation)
				require.NoError(t, err)
				require.Equal(t, PricingStateFree, media.PricingState)
				require.Zero(t, media.ActualCost)
			} else {
				require.Error(t, validation)
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
			}
		}
	}
}

func TestVariantProjectionGatewayMediaDoesNotBorrowLegacyPrices(t *testing.T) {
	card := projectionCard()
	group := &Group{ID: 1, ModelPricing: []ChannelModelPricing{card}}
	key := &APIKey{Group: group, GroupID: &group.ID}
	billing := &BillingService{}
	gateway := &OpenAIGatewayService{billingService: billing, resolver: NewModelPricingResolver(nil, billing)}
	result := &OpenAIForwardResult{ImageCount: 3, VideoCount: 2, VideoDurationSeconds: 5}
	image := gateway.calculateOpenAIImageCost(context.Background(), "projection-model", key, result, 2)
	require.InDelta(t, 1.2, image.ActualCost, 1e-12)
	generic := &GatewayService{billingService: billing, resolver: gateway.resolver}
	image = generic.calculateImageCost(context.Background(), &ForwardResult{ImageCount: 3}, key, "projection-model", 2)
	require.InDelta(t, 1.2, image.ActualCost, 1e-12)
	video := gateway.calculateOpenAIVideoCost(context.Background(), "projection-model", key, result, 2)
	require.InDelta(t, .8, video.ActualCost, 1e-12)
	group.ModelPricing[0].PriceVariants[0].Prices[4].Sales = nil
	group.ModelPricing[0].PerRequestPrice = priceFactNumber(99)
	image = gateway.calculateOpenAIImageCost(context.Background(), "projection-model", key, result, 2)
	require.Equal(t, PricingStateUnpriced, image.PricingState)
	require.Empty(t, image.PricingSnapshot)
}

func TestVariantProjectionCacheTTLExplicitFree(t *testing.T) {
	card := projectionCard()
	card.PriceVariants[0].Prices = append(card.PriceVariants[0].Prices, ModelUnitPrice{Unit: "token", Dimension: "cache_write_1h", Currency: "USD", Sales: priceFactNumber(0), Cost: priceFactNumber(0)})
	descriptor := &ModelPricingDescriptor{Model: "projection-model", PricingAt: time.Now(), Variants: card.PriceVariants}
	resolver := NewModelPricingResolver(nil, nil)
	tokens := UsageTokens{CacheCreationTokens: 4, CacheCreation5mTokens: 1, CacheCreation1hTokens: 3}
	cost, err := (&BillingService{}).CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "projection-model", Tokens: tokens, RateMultiplier: 1, Resolver: resolver, Resolved: descriptor.resolvePricing(BillingModeToken)})
	require.NoError(t, err)
	require.InDelta(t, .000001, cost.CacheCreationCost, 1e-12)
	accountCost, err := descriptor.tokenCost(tokens)
	require.NoError(t, err)
	require.InDelta(t, .0000005, accountCost, 1e-12)
	card.PriceVariants[0].Prices[len(card.PriceVariants[0].Prices)-1].Sales = nil
	_, err = (&BillingService{}).CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "projection-model", Tokens: tokens, RateMultiplier: 1, Resolver: resolver, Resolved: descriptor.resolvePricing(BillingModeToken)})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestVariantProjectionMediaOnlyAndTokenPricesNeverBecomeImagePrices(t *testing.T) {
	card := projectionCard()
	card.PriceVariants[0].Prices = card.PriceVariants[0].Prices[4:5]
	card.BillingMode = BillingModeImage
	require.NoError(t, validatePricingEntries([]ChannelModelPricing{card}))
	entry := &LiteLLMModelPricing{PriceVariants: card.PriceVariants, TokenPricingAbsent: true}
	billing := &BillingService{pricingService: &PricingService{pricingData: map[string]*LiteLLMModelPricing{"projection-model": entry}}}
	resolver := &BatchImageModelPricingResolver{Resolver: NewModelPricingResolver(nil, billing)}
	for _, price := range []float64{.2, 0} {
		entry.PriceVariants[0].Prices[0].Sales = priceFactNumber(price)
		unit, err := resolver.BatchImageUnitPrice(context.Background(), &BatchImageJob{Model: "projection-model"})
		require.NoError(t, err)
		require.Equal(t, price, unit)
		cost := billing.CalculateImageCost("projection-model", "1K", 3, nil, 2)
		require.InDelta(t, price*6, cost.ActualCost, 1e-12)
		zero := billing.CalculateImageCost("projection-model", "1K", 0, nil, 2)
		require.Zero(t, zero.ActualCost)
	}
	entry.PriceVariants[0].Prices[0].Sales = nil
	_, err := resolver.BatchImageUnitPrice(context.Background(), &BatchImageJob{Model: "projection-model"})
	require.ErrorIs(t, err, ErrBatchImageSettlementPricingMissing)
	entry.PriceVariants = nil
	entry.TokenPricingAbsent = false
	entry.OutputCostPerImageToken = .2
	_, err = resolver.BatchImageUnitPrice(context.Background(), &BatchImageJob{Model: "projection-model"})
	require.ErrorIs(t, err, ErrBatchImageSettlementPricingMissing)
}

func TestVariantProjectionProfitGateUsesCacheCostAndSales(t *testing.T) {
	card := projectionCard()
	descriptor := &ModelPricingDescriptor{configured: true, Model: "projection-model", PricingAt: time.Now(), Variants: card.PriceVariants}
	gate := &openAIProfitControlGate{descriptor: descriptor, model: descriptor.Model, threshold: 1}
	account := &Account{RateMultiplier: priceFactNumber(1)}
	veto, reason := gate.descriptorVeto(context.Background(), account)
	require.False(t, veto, reason)
	*descriptor.Variants[0].Prices[2].Cost = .000001
	veto, reason = gate.descriptorVeto(context.Background(), account)
	require.True(t, veto)
	require.Equal(t, openAIProfitFilterReasonThreshold, reason)
}
