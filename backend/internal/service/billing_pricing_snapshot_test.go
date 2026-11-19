//go:build unit

package service

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"math"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFrozenPricingRequestStatesAndBounds(t *testing.T) {
	billing := &BillingService{}
	resolver := NewModelPricingResolver(nil, billing)
	zero, paid, boundary := 0.0, 2.0, 10
	cases := []struct {
		name     string
		resolved *ResolvedPricing
		tier     string
		units    float64
		state    PricingState
		amount   float64
	}{
		{"absent", &ResolvedPricing{Mode: BillingModePerRequest}, "", 1, PricingStateUnpriced, 0},
		{"explicit free default", &ResolvedPricing{Mode: BillingModePerRequest, DefaultPerRequestPriceFound: true}, "", 1, PricingStateFree, 0},
		{"free tier wins", &ResolvedPricing{Mode: BillingModePerRequest, DefaultPerRequestPrice: paid, RequestTiers: []PricingInterval{{TierLabel: "free", PerRequestPrice: &zero}}}, "free", 1, PricingStateFree, 0},
		{"unmatched tier", &ResolvedPricing{Mode: BillingModePerRequest, RequestTiers: []PricingInterval{{TierLabel: "paid", PerRequestPrice: &paid}}}, "missing", 1, PricingStateUnpriced, 0},
		{"fractional seconds", &ResolvedPricing{Mode: BillingModeVideo, DefaultPerRequestPrice: paid}, "720p", 10.5, PricingStatePriced, 21},
		{"context boundary", &ResolvedPricing{Mode: BillingModePerRequest, RequestTiers: []PricingInterval{{MinTokens: 0, MaxTokens: &boundary, PerRequestPrice: &zero}}}, "", 1, PricingStateFree, 0},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			cost, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "exact", Resolver: resolver, Resolved: test.resolved, SizeTier: test.tier, UsageUnits: test.units, Tokens: UsageTokens{InputTokens: 10}, RateMultiplier: 1})
			require.Equal(t, test.state, cost.PricingState)
			if test.state == PricingStateUnpriced {
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.amount, cost.ActualCost)
			replay, err := RecalculatePricingSnapshot(cost.PricingSnapshot)
			require.NoError(t, err)
			require.Equal(t, cost.ActualCost, replay.ActualCost)
		})
	}
	for _, invalid := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1), 1e12, math.MaxFloat64} {
		require.ErrorIs(t, ValidateUsageBillingAmount(invalid), ErrInvalidBillingAmount)
	}
	for _, invalid := range []float64{-1, math.NaN(), math.Inf(1)} {
		cost, err := billing.CalculateCostUnified(CostInput{RateMultiplier: invalid})
		require.ErrorIs(t, err, ErrInvalidBillingAmount)
		require.Equal(t, PricingStateError, cost.PricingState)
	}
	cost, err := billing.CalculateCostUnified(CostInput{RateMultiplier: 1, Tokens: UsageTokens{InputTokens: math.MaxInt, CacheReadTokens: 1}})
	require.Error(t, err)
	require.Equal(t, PricingStateError, cost.PricingState)
}

func TestFrozenPricingDoesNotReferenceMutableCards(t *testing.T) {
	billing := &BillingService{}
	resolver := NewModelPricingResolver(nil, billing)
	prices := &ModelPricing{InputPricePerToken: .00000125, OutputPricePerToken: .00001}
	for _, multiplier := range []float64{.5, 1, 2} {
		original := *prices
		cost, err := billing.CalculateCostUnified(CostInput{Model: "exact", Provider: "provider", Tokens: UsageTokens{InputTokens: 10, OutputTokens: 5}, RateMultiplier: multiplier, Resolver: resolver, Resolved: &ResolvedPricing{BasePricing: prices, Mode: BillingModeToken}})
		require.NoError(t, err)
		encoded := cost.PricingSnapshot
		prices.InputPricePerToken = 900
		replay, err := RecalculatePricingSnapshot(encoded)
		require.NoError(t, err)
		require.Equal(t, cost.TotalCost, replay.TotalCost)
		require.Equal(t, cost.ActualCost, replay.ActualCost)
		var snapshot PricingSnapshot
		require.NoError(t, json.Unmarshal([]byte(encoded), &snapshot))
		require.Equal(t, QuantizeUsageBillingAmount(cost.ActualCost), snapshot.ChargedAmount)
		*prices = original
	}
}

func TestFrozenPricingImageNeverBorrowsModel(t *testing.T) {
	prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{"gpt-image-1": {InputCostPerToken: .1, OutputCostPerToken: .2}}}
	billing := &BillingService{pricingService: prices}
	_, err := billing.GetModelPricing("gpt-image-unknown")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	cost := billing.CalculateImageCost("gpt-image-1", "1K", 1, nil, 1)
	require.Equal(t, PricingStateUnpriced, cost.PricingState)
	zero := 0.0
	cost = billing.CalculateImageCost("gpt-image-1", "1K", 1, &ImagePriceConfig{Price1K: &zero}, 1)
	require.Equal(t, PricingStateFree, cost.PricingState)
}

func TestFrozenPricingBatchAdmissionAndAccountCost(t *testing.T) {
	for _, multiplier := range []float64{.5, 1, 2} {
		job, account := paidBatchFixture(t)
		original := job.Billing.FileBatch
		group := &Group{RateMultiplier: 9}
		at := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
		billing, err := QianwenFileBatchBillingSnapshotAt(group, account, nil, nil, original, FileBatchPricingAdmission{UserGroupRateMultiplier: multiplier, PricingAt: at})
		require.NoError(t, err)
		billing.AccountRateMultiplier = 2
		billing.AccountQuota = true
		job.Billing = billing
		original.InputPrice = 500
		group.RateMultiplier = 100
		log, cmd, err := chargeQianwenFileBatch(job, 100, 20)
		require.NoError(t, err)
		require.Equal(t, multiplier, log.RateMultiplier)
		require.Equal(t, log.TotalCost, *log.AccountStatsCost)
		require.Equal(t, QuantizeUsageBillingAmount(log.TotalCost*2), cmd.AccountQuotaCost)
		require.Equal(t, QuantizeUsageBillingAmount(log.TotalCost*multiplier), cmd.BalanceCost)
		require.NotEmpty(t, job.Billing.PricingSnapshot)
	}
}

func TestFrozenPricingAudioSearchInvalidAndFree(t *testing.T) {
	billing := &BillingService{}
	zero, negative := 0.0, -1.0
	require.Equal(t, PricingStateFree, billing.CalculateAudioCost("tts", .5, &audioPriceConfig{TTSPerMChars: &zero}, 1).PricingState)
	require.Equal(t, PricingStateFree, billing.CalculateSearchCost(1, &zero, 1).PricingState)
	require.Equal(t, PricingStateError, billing.CalculateSearchCost(1, &negative, 1).PricingState)
	require.Equal(t, PricingStateError, billing.CalculateAudioCost("stt", math.Inf(1), nil, 1).PricingState)
	require.Equal(t, PricingStateError, billing.CalculateWebSearchCost(1, nil, math.MaxFloat64).PricingState)
	require.Equal(t, .05, billing.CalculateAudioCost("stt", .5, nil, 1).TotalCost)
}

func TestFrozenPricingRealtimeAndFractionalVideoAccountMultiplier(t *testing.T) {
	for _, multiplier := range []float64{.5, 1, 2} {
		realtime := &UpstreamJob{Platform: PlatformQianwen, Family: "realtime_turn", UsageJSON: json.RawMessage(`{"characters":1000}`), Billing: UpstreamJobBilling{Model: "qwen3-tts-flash-realtime", UnitPrice: .13 / 10000, RateMultiplier: multiplier, AccountRateMultiplier: 2, AccountQuota: true, Realtime: &QianwenRealtimeSnapshot{SessionID: "session", Region: "singapore", PriceVersion: QianwenRealtimePriceVersion}}}
		log, command, err := QianwenRealtimeCharge(realtime)
		require.NoError(t, err)
		require.Equal(t, .013, *log.AccountStatsCost, "USD realtime rates must not be divided by CNY FX")
		require.Equal(t, .026, command.AccountQuotaCost)
		replay, err := RecalculatePricingSnapshot(realtime.Billing.PricingSnapshot)
		require.NoError(t, err)
		require.Equal(t, log.ActualCost, replay.ActualCost)
		video := &UpstreamJob{Platform: PlatformQianwen, Family: "video", ResultRef: "https://example.test/video", UsageJSON: json.RawMessage(`{"video_count":1,"duration":10.5,"SR":720}`), Billing: UpstreamJobBilling{Model: "wan2.6-r2v", Resolution: "720p", UnitPrice: .1, RateMultiplier: multiplier, AccountRateMultiplier: 2, AccountQuota: true}}
		log, command, err = QianwenVideoJobCharge(video)
		require.NoError(t, err)
		require.Equal(t, 1.05, *log.AccountStatsCost)
		require.Equal(t, 2.1, command.AccountQuotaCost)
		require.Nil(t, log.VideoDurationSeconds)
		replay, err = RecalculatePricingSnapshot(video.Billing.PricingSnapshot)
		require.NoError(t, err)
		require.Equal(t, log.ActualCost, replay.ActualCost)
	}
}

func TestFrozenPricingGPTImagePreflightHasNoOutboundOnMissingPrice(t *testing.T) {
	upstream := &httpUpstreamRecorder{}
	service := &OpenAIGatewayService{billingService: &BillingService{}, httpUpstream: upstream}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("POST", "/v1/images/generations", nil)
	context.Set("api_key", &APIKey{})
	result, err := service.ForwardImages(context.Request.Context(), context, &Account{Type: AccountTypeAPIKey}, nil, &OpenAIImagesRequest{Model: "gpt-image-1", Size: "1024x1024"}, "")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Nil(t, result)
	require.Empty(t, upstream.requests)
	zero := 0.0
	key := &APIKey{Group: &Group{ImagePrice1K: &zero}}
	require.NoError(t, service.validateOpenAIImagePricing(context.Request.Context(), key, "gpt-image-1", "1K"))
	key.Group.ImagePrice1K = nil
	key.Group.ModelPricing = []ChannelModelPricing{{Models: []string{"gpt-image-1"}, BillingMode: BillingModeImage, PerRequestPrice: &zero}}
	require.NoError(t, service.validateOpenAIImagePricing(context.Request.Context(), key, "gpt-image-1", "1K"))
	key.Group.ModelPricing[0].PerRequestPrice = nil
	key.Group.ModelPricing[0].Intervals = []PricingInterval{{TierLabel: "2K", PerRequestPrice: &zero}}
	require.ErrorIs(t, service.validateOpenAIImagePricing(context.Request.Context(), key, "gpt-image-1", "1K"), ErrModelPricingUnavailable)
	service.resolver = NewModelPricingResolver(nil, service.billingService)
	cost := service.calculateOpenAIImageCost(context.Request.Context(), "gpt-image-1", key, &OpenAIForwardResult{ImageCount: 1, ImageSize: "1K"}, 1)
	require.Equal(t, PricingStateUnpriced, cost.PricingState)

}

func TestBE07ExactLegacyGolden(t *testing.T) {
	const snapshot = `{"model":"fixture","mode":"per_request","source":"group","unit_price":0.000078125,"units":1,"rate_multiplier":1,"time_multiplier":1,"reasoning_multiplier":1}`
	legacy, err := RecalculatePricingSnapshot(snapshot)
	require.NoError(t, err)
	exact, err := ReplayExactPricing(snapshot)
	require.NoError(t, err)
	require.InDelta(t, 0.000078125, legacy.ActualCost, 1e-16)
	require.Equal(t, "0.00007813", exact.Charge.Amount().String())
}

func TestBE07HistoricalReplay(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/migrations/BE-02/v1-replay.json")
	require.NoError(t, err)
	var fixtures []struct {
		Name   string
		Frozen json.RawMessage `json:"frozen_snapshot"`
	}
	require.NoError(t, json.Unmarshal(raw, &fixtures))
	expected := map[string]string{"half-tie": "0.00007813", "float-tail": "0.30000000", "free": "0.00000000"}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			legacy, err := RecalculatePricingSnapshot(string(fixture.Frozen))
			require.NoError(t, err)
			require.NotNil(t, legacy)
			exact, err := ReplayExactPricing(string(fixture.Frozen))
			if fixture.Name == "quantized-quote" {
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				return
			}
			require.NoError(t, err)
			require.Equal(t, expected[fixture.Name], exact.Charge.String())
		})
	}
}
