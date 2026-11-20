//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGatewaysShareConfiguredPriceSelection(t *testing.T) {
	rate := 0.01
	resolver := NewModelPricingResolver(nil, NewBillingService(nil, nil))
	g, o := &GatewayService{resolver: resolver}, &OpenAIGatewayService{resolver: resolver}
	group := &Group{ID: 2, ModelPricing: []ChannelModelPricing{{Models: []string{"custom"}, InputPrice: &rate, OutputPrice: &rate}}}
	for _, key := range []*APIKey{nil, {}, {Group: group}} {
		a := g.resolveChannelPricing(t.Context(), "custom", key)
		b := o.resolveOpenAIChannelPricing(t.Context(), "custom", key)
		require.Equal(t, a, b)
		if key != nil && key.Group != nil {
			require.NotNil(t, a)
			require.Equal(t, PricingSourceGroup, a.Source)
		} else {
			require.Nil(t, a)
		}
	}
	channel := newResolverWithChannel(t, []ChannelModelPricing{{Platform: PlatformAnthropic, Models: []string{"custom"}, InputPrice: &rate, OutputPrice: &rate}})
	g.resolver, o.resolver = channel, channel
	for _, key := range []*APIKey{{GroupID: groupIDPtr()}, {Group: &Group{ID: *groupIDPtr(), Platform: PlatformAnthropic}}} {
		left := g.resolveChannelPricing(t.Context(), "custom", key)
		right := o.resolveOpenAIChannelPricing(t.Context(), "custom", key)
		require.NotNil(t, left)
		require.Equal(t, PricingSourceChannel, left.Source)
		require.Equal(t, left, right)
	}

}

func TestGatewaysAssembleIdenticalFrozenReceiptFromEquivalentUsage(t *testing.T) {
	ctx := t.Context()
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	logs := &openAIRecordUsageLogRepoStub{inserted: true}
	g, key := newGatewayRecordUsageServiceWithResolverForTest(logs)
	o := newOpenAIRecordUsageServiceForTest(logs, nil, nil, nil)
	o.resolver = g.resolver
	account := &Account{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	user := &User{ID: 2}
	groupPrice := 0.00001
	key.Group.ModelPricing = []ChannelModelPricing{{Models: []string{"custom-meter"}, InputPrice: &groupPrice, OutputPrice: &groupPrice, CacheReadPrice: &groupPrice}}
	effort := "high"
	require.NoError(t, g.RecordUsage(ctx, &RecordUsageInput{APIKey: key, User: user, Account: account, PricingAt: when,
		Result: &ForwardResult{RequestID: "same", Model: "custom-meter", UpstreamModel: "custom-meter", UpstreamResponseModel: "custom-meter", ReasoningEffort: &effort,
			Usage: ClaudeUsage{InputTokens: 7, CacheReadInputTokens: 3, OutputTokens: 4}}}))
	require.NoError(t, o.RecordUsage(ctx, &OpenAIRecordUsageInput{APIKey: key, User: user, Account: account, PricingAt: when,
		Result: &OpenAIForwardResult{RequestID: "same", Model: "custom-meter", UpstreamModel: "custom-meter", UpstreamResponseModel: "custom-meter", ReasoningEffort: &effort,
			Usage: OpenAIUsage{InputTokens: 10, CacheReadInputTokens: 3, OutputTokens: 4}}}))
	first := g.usageSettlement.repo.(*recordUsageReceiptFixture).receipt
	second := receiptFixture(t, o).receipt
	require.NotNil(t, first)
	require.NotNil(t, second)
	require.Equal(t, UsageSettlementSettled, first.State)
	require.Equal(t, first.Command, second.Command)
	require.Equal(t, 7, first.Usage.InputTokens)
	require.Equal(t, 3, first.Usage.CacheReadTokens)
	require.Equal(t, first.Usage.PricingState, second.Usage.PricingState)
	require.Equal(t, first.Usage.PricingSnapshot, second.Usage.PricingSnapshot)
	require.Equal(t, first.Usage.UpstreamResponseModel, second.Usage.UpstreamResponseModel)
	require.Equal(t, first.Usage.ReasoningEffort, second.Usage.ReasoningEffort)
}
