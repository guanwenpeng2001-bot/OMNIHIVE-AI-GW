//go:build unit

package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

const previousPricingFixture = `{"preview-model":{"litellm_provider":"test","mode":"chat","input_cost_per_token":0.000001,"output_cost_per_token":0.000002},"removed-model":{"input_cost_per_token":0}}`
const candidatePricingFixture = `{"preview-model":{"litellm_provider":"test","mode":"chat","input_cost_per_token":0.000002,"output_cost_per_token":0.000004},"added-model":{"input_cost_per_token":0},"another-model":{"input_cost_per_token":0.000003}}`

type pricingSourceRemoteStub struct {
	body []byte
	err  error
}

func (remote *pricingSourceRemoteStub) FetchPricingJSON(context.Context, string) ([]byte, error) {
	return remote.body, remote.err
}

func (remote *pricingSourceRemoteStub) FetchHashText(context.Context, string) (string, error) {
	return "", errors.New("official catalog does not require a hash endpoint")
}

func TestPricingSourceAndDifferenceContracts(t *testing.T) {
	t.Run("counts and preview have no side effects", func(t *testing.T) {
		pricing := newStubPricingServiceFromJSON(t, previousPricingFixture)
		pricing.cfg = &config.Config{Pricing: config.PricingConfig{DataDir: t.TempDir()}}
		previous := pricing.pricingData
		preview, err := pricing.PreviewSource([]byte(candidatePricingFixture))
		require.NoError(t, err)
		require.Equal(t, 2, preview.BeforeCount)
		require.Equal(t, 3, preview.AfterCount)
		require.Equal(t, []string{"added-model", "another-model"}, preview.Added)
		require.Equal(t, []string{"removed-model"}, preview.Removed)
		require.Len(t, preview.Differences, 2)
		for _, difference := range preview.Differences {
			require.True(t, difference.Warning)
			require.Equal(t, "reference_to_reference", difference.Basis)
			require.InDelta(t, 100, *difference.AbsolutePercent, 1e-10)
		}
		repeat, err := pricing.PreviewSource([]byte(candidatePricingFixture))
		require.NoError(t, err)
		require.Equal(t, preview, repeat)
		require.Equal(t, previous, pricing.pricingData)
		require.Empty(t, pricing.localHash)
		files, err := os.ReadDir(pricing.cfg.Pricing.DataDir)
		require.NoError(t, err)
		require.Empty(t, files)
	})
	t.Run("strict threshold for both bases", func(t *testing.T) {
		for _, basis := range []string{"reference_to_reference", "channel_to_reference"} {
			for _, amount := range []float64{1.0999, 1.1, 1.1001, 0.9, 0.8999} {
				baseline := PricingQuote{Value: testPtrFloat64(1), Currency: "USD", Unit: "token", Scope: "model/region/plan", Conditions: "standard"}
				candidate := baseline
				candidate.Value = testPtrFloat64(amount)
				difference := ComparePricingQuotes(basis, "model", "input", baseline, candidate)
				require.Equal(t, "comparable", difference.Status)
				require.Equal(t, amount == 1.1001 || amount == 0.8999, difference.Warning)
			}
		}
	})
	t.Run("zero missing and incomparable", func(t *testing.T) {
		baseline := PricingQuote{Value: testPtrFloat64(0), Currency: "USD", Unit: "token", Scope: "model/cn", Conditions: "standard"}
		candidate := baseline
		require.Equal(t, "comparable", ComparePricingQuotes("reference_to_reference", "model", "input", baseline, candidate).Status)
		candidate.Value = testPtrFloat64(2)
		difference := ComparePricingQuotes("reference_to_reference", "model", "input", baseline, candidate)
		require.Equal(t, "new_charge", difference.Status)
		require.Nil(t, difference.AbsolutePercent)
		baseline.Value = nil
		require.Equal(t, "missing", ComparePricingQuotes("reference_to_reference", "model", "input", baseline, candidate).Status)
		baseline.Value = testPtrFloat64(1)
		for _, mismatch := range []PricingQuote{
			{Value: testPtrFloat64(2), Currency: "CNY", Unit: "token", Scope: "model/cn", Conditions: "standard"},
			{Value: testPtrFloat64(2), Currency: "USD", Unit: "second", Scope: "model/cn", Conditions: "standard"},
			{Value: testPtrFloat64(2), Currency: "USD", Unit: "token", Scope: "model/global", Conditions: "standard"},
			{Value: testPtrFloat64(2), Currency: "USD", Unit: "token", Scope: "model/cn", Conditions: "batch"},
		} {
			difference := ComparePricingQuotes("channel_to_reference", "model", "input", baseline, mismatch)
			require.Equal(t, "incomparable", difference.Status)
			require.False(t, difference.Warning)
			require.Nil(t, difference.AbsolutePercent)
		}
	})
	t.Run("source missing is not zero", func(t *testing.T) {
		pricing := newStubPricingServiceFromJSON(t, `{"model":{"input_cost_per_token":0}}`)
		preview, err := pricing.PreviewSource([]byte(`{"model":{"input_cost_per_token":0,"output_cost_per_token":1}}`))
		require.NoError(t, err)
		require.Len(t, preview.Differences, 2)
		require.Equal(t, "comparable", preview.Differences[0].Status)
		require.Equal(t, "missing", preview.Differences[1].Status)
	})
	t.Run("refresh failure retains published prices", func(t *testing.T) {
		remote := &pricingSourceRemoteStub{body: []byte(candidatePricingFixture)}
		pricing := newStubPricingServiceFromJSON(t, previousPricingFixture)
		pricing.cfg = &config.Config{Pricing: config.PricingConfig{DataDir: t.TempDir(), RemoteURL: "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"}}
		pricing.remoteClient = remote
		previous := pricing.pricingData
		remote.err = errors.New("offline")
		require.Error(t, pricing.ForceUpdate())
		require.Equal(t, previous, pricing.pricingData)
		remote.err = nil
		remote.body = []byte(`{"invalid":{}}`)
		require.Error(t, pricing.ForceUpdate())
		require.Equal(t, previous, pricing.pricingData)
		remote.body = []byte(candidatePricingFixture)
		pricing.cfg.Pricing.DataDir = filepath.Join(t.TempDir(), "missing-parent")
		require.ErrorContains(t, pricing.ForceUpdate(), "save pricing file")
		require.Equal(t, previous, pricing.pricingData)
		pricing.cfg.Pricing.DataDir = t.TempDir()
		require.NoError(t, pricing.ForceUpdate())
		require.Len(t, pricing.pricingData, 3)
		require.NotEmpty(t, pricing.localHash)
	})
	t.Run("native import units remain strict", func(t *testing.T) {
		model := qianwenTestModel()
		parsed := parseQianwenModelPrices(model, 0.125)
		require.NotNil(t, parsed.Pricing)
		require.InDelta(t, 1.5e-6, *parsed.Pricing.InputPrice, 1e-15)
		for _, unit := range []string{"", "每秒", "每分钟", "每张", "每次", "USD/token"} {
			model.Prices[0].Prices[0].Unit = unit
			parsed = parseQianwenModelPrices(model, 0.125)
			require.Nil(t, parsed.Pricing)
			require.Equal(t, "unsupported", parsed.Status)
		}
	})
}

func TestPricingInheritedChargeImpact(t *testing.T) {
	pricing := newStubPricingServiceFromJSON(t, previousPricingFixture)
	billing := NewBillingService(&config.Config{}, pricing)
	manual := &ChannelModelPricing{Models: []string{"preview-model"}, BillingMode: BillingModeToken, InputPrice: testPtrFloat64(7e-6), OutputPrice: testPtrFloat64(9e-6)}
	partial := &ChannelModelPricing{Models: []string{"preview-model"}, BillingMode: BillingModeToken, InputPrice: testPtrFloat64(7e-6)}
	channels := newTestChannelServiceWithCache(t, &channelCache{
		pricingByGroupModel:     map[channelModelKey]*ChannelModelPricing{{groupID: 2, model: "preview-model"}: manual, {groupID: 3, model: "preview-model"}: partial},
		channelByGroupID:        map[int64]*Channel{1: {ID: 1, Status: StatusActive}, 2: {ID: 2, Status: StatusActive}, 3: {ID: 3, Status: StatusActive}},
		groupPlatform:           map[int64]string{1: "", 2: "", 3: ""},
		wildcardByGroupPlatform: map[channelGroupPlatformKey][]*wildcardPricingEntry{}, mappingByGroupModel: map[channelModelKey]string{},
		wildcardMappingByGP: map[channelGroupPlatformKey][]*wildcardMappingEntry{}, byID: map[int64]*Channel{},
	})
	manualBefore, partialBefore := manual.Clone(), partial.Clone()
	samples := []CostInput{}
	for groupID := int64(1); groupID <= 3; groupID++ {
		groupIDCopy := groupID
		samples = append(samples, CostInput{Model: "preview-model", GroupID: &groupIDCopy, Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 500}, RateMultiplier: 1})
	}
	impacts, err := pricing.PreviewChargeImpact([]byte(candidatePricingFixture), billing, channels, samples)
	require.NoError(t, err)
	require.Len(t, impacts, 3)
	for _, impact := range impacts {
		require.True(t, impact.Comparable, "%+v", impact)
	}
	require.InDelta(t, 0.002, impacts[0].Before.ActualCost, 1e-12)
	require.InDelta(t, 0.004, impacts[0].After.ActualCost, 1e-12)
	require.True(t, impacts[0].ChargeChanged)
	require.InDelta(t, 0.0115, impacts[1].Before.ActualCost, 1e-12)
	require.Equal(t, impacts[1].Before.ActualCost, impacts[1].After.ActualCost)
	require.False(t, impacts[1].ChargeChanged)
	require.True(t, impacts[2].ChargeChanged, "partial channel price inherits output")
	require.Equal(t, manualBefore, *manual)
	require.Equal(t, partialBefore, *partial)
	require.InDelta(t, 1e-6, pricing.GetModelPricing("preview-model").InputCostPerToken, 1e-15)
	// Apply only to this isolated in-memory fixture and its temporary cache.
	// Actual billing after a source refresh must agree with the preview.
	pricing.cfg = &config.Config{Pricing: config.PricingConfig{DataDir: t.TempDir(), RemoteURL: "https://pricing.example.test/catalog.json"}}
	pricing.remoteClient = &pricingSourceRemoteStub{body: []byte(candidatePricingFixture)}
	require.NoError(t, pricing.ForceUpdate())
	for index, sample := range samples {
		sample.Ctx = context.Background()
		sample.Resolver = NewModelPricingResolver(channels, billing)
		actual, err := billing.CalculateCostUnified(sample)
		require.NoError(t, err)
		require.Equal(t, impacts[index].After.ActualCost, actual.ActualCost)
	}
	require.Equal(t, manualBefore, *manual, "refresh must not rewrite channel prices")
	require.Equal(t, partialBefore, *partial)
	require.InDelta(t, 0.002, impacts[0].Before.ActualCost, 1e-12, "previous bill remains frozen")
	// The preview neither changes the live service nor reuses a frozen card.
	samples[0].Resolved = &ResolvedPricing{}
	_, err = pricing.PreviewChargeImpact([]byte(candidatePricingFixture), billing, channels, samples)
	require.ErrorContains(t, err, "unfrozen")
}
