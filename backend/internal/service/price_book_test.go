//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/catalog/compare"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/control/catalog/quotes"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
	modelpricing "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/resources/model-pricing"
)

type priceBookTestCatalog struct {
	snapshot catalog.Snapshot
	bindings []pricing.Binding
}

func (c priceBookTestCatalog) Snapshot(context.Context, catalog.Perspective, time.Time) (catalog.Snapshot, error) {
	return c.snapshot, nil
}

func (c priceBookTestCatalog) PriceBindings(_ context.Context, p catalog.Perspective, model string, key int64, protocol, action string, at time.Time) ([]pricing.Binding, error) {
	if key != 0 && (key != 5 || p.Actor != 4 || p.Project != 2) {
		return nil, nil
	}
	return c.bindings, nil
}

func TestPriceBookReferenceNeverFundsGateway(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 11, 19, 13, 0, 0, 0, time.UTC)
	const model = "synthetic-reference-only"
	id := catalog.StableID(PlatformOpenAI, model)
	refID := "ref_pi"
	ref, err := money.NewRate("USD", "999", "input_token", "1")
	require.NoError(t, err)
	detail := catalog.Detail{
		Model:       catalog.Summary{ID: id, Model: model, Provider: PlatformOpenAI, PriceState: "priced", Input: &ref, AccessState: "allowed", RouteState: "eligible"},
		PriceSource: "pi-ai", PriceVersion: &refID,
		ReferencePrice: &catalog.Price{State: "reference", Source: "pi-ai", Input: &ref},
		Capabilities:   []catalog.Capability{{Action: "text.chat", Protocol: "openai_chat"}},
	}
	p := catalog.Perspective{Workspace: 1, Project: 2, Actor: 4}
	binding := pricing.Binding{Provider: PlatformOpenAI, Model: model, At: at, Funding: "wallet", RateMultiplier: 1}
	reader := priceBookTestCatalog{catalog.Snapshot{AsOf: at, Details: []catalog.Detail{detail}}, []pricing.Binding{binding}}
	scenario := quotes.Scenario{Input: "1000"}
	keyID := "5"

	for _, paid := range []bool{false, true} {
		name := "reference_only"
		if paid {
			name = "same_reference_with_sale_tariff"
		}
		t.Run(name, func(t *testing.T) {
			prices := &PricingService{}
			if paid {
				data, err := prices.parsePricingData([]byte(`{"openai/synthetic-reference-only":{"litellm_provider":"openai","input_cost_per_token":0.000002,"output_cost_per_token":0.000003}}`))
				require.NoError(t, err)
				prices.replacePricingData(data)
			}
			billing := NewBillingService(nil, prices)
			resolver := NewModelPricingResolver(nil, billing)
			book := NewPlatformPriceBook(resolver)
			estimate, err := (quotes.Service{Catalog: reader, Book: book}).Quote(ctx, p, quotes.Input{ModelID: id, APIKeyID: &keyID, Protocol: "openai_chat", Action: "text.chat", Scenario: &scenario})
			require.NoError(t, err)
			compared, err := (compare.Service{Catalog: reader, Book: book}).Compare(ctx, p, compare.Input{IDs: []string{id}, Scenario: &scenario})
			require.NoError(t, err)
			require.Equal(t, estimate.Result, compared.Rows[0].Quote)

			repo := &keyBudgetReceiptStub{}
			wallet := &openAIRecordUsageUserRepoStub{}
			receipts := &recordUsageReceiptFixture{
				usage: &openAIRecordUsageLogRepoStub{inserted: true}, users: wallet,
				billing: func() UsageBillingRepository { return nil },
			}
			gateway := &GatewayService{billingService: billing, resolver: resolver, usageSettlement: NewUsageSettlementService(receipts, nil, nil)}
			requestCtx, plan := captureGatewayRequestPlan(WithResolvedTargetPlatform(ctx, PlatformOpenAI), GatewayRequestSpec{ExecutionID: "pricebook-reference", APIKeyID: 5, UserID: 4, PricingAt: at}, nil, billing, nil)
			plan.settlement = NewUsageSettlementService(repo, nil, nil)
			err = plan.ReserveBudget(requestCtx, RequestBudgetEstimate{Model: model, InputTokens: 1000, OutputTokens: 0, InputOnly: true, RateMultiplier: 1})
			key := &APIKey{ID: 5}
			usageErr := gateway.RecordUsage(requestCtx, &RecordUsageInput{
				Result: &ForwardResult{RequestID: "pricebook-reference", Model: model, Usage: ClaudeUsage{InputTokens: 1000}},
				APIKey: key, User: &User{ID: 4}, Account: &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, PricingAt: at,
			})
			require.NotNil(t, receipts.receipt)
			command, usageLog := receipts.receipt.Command, receipts.receipt.Usage
			if !paid {
				require.Error(t, err)
				require.ErrorIs(t, usageErr, ErrUsageSettlementNeedsReview)
				require.Empty(t, repo.reservations)
				require.Equal(t, pricing.Missing, estimate.Reason)
				require.Nil(t, estimate.Charge)
				require.Equal(t, PricingStateUnpriced, usageLog.PricingState)
				require.Equal(t, UsageSettlementNeedsReview, receipts.receipt.State)
				require.Zero(t, wallet.deductCalls)
				require.Zero(t, command.BalanceCost)
				require.Zero(t, command.SubscriptionCost)
				require.Zero(t, command.APIKeyQuotaCost)
				return
			}
			require.NoError(t, err)
			require.NoError(t, usageErr)
			require.Len(t, repo.reservations, 1)
			require.Equal(t, "0.00200000", estimate.Charge.String())
			require.InDelta(t, 0.002, command.BalanceCost, 1e-12)
			require.Equal(t, 1, wallet.deductCalls)
			require.InDelta(t, command.BalanceCost, wallet.lastAmount, 1e-12)
			require.InDelta(t, command.BalanceCost, repo.reservations[0].HoldAmount, 1e-12)
			require.Equal(t, *estimate.PriceVersion, optionalStringValue(usageLog.PriceCardVersion))
			require.Equal(t, *estimate.Source, optionalStringValue(usageLog.PriceSource))
		})
	}
}

func TestPriceBookProviderOwnersOverridesAndFallback(t *testing.T) {
	service := newPricingServiceWithOverride(t, `{
		"shared": {"input_cost_per_token": 100},
		"openai/shared": {"output_cost_per_token": null},
		"anthropic/shared": {"input_cost_per_token": 4},
		"unowned": {"litellm_provider":"openai", "input_cost_per_token": 100}
	}`)
	data, _, err := service.buildPricingData([]byte(`{
		"shared": {"litellm_provider":"openai", "input_cost_per_token":1,"output_cost_per_token":2},
		"anthropic/shared": {"litellm_provider":"anthropic", "input_cost_per_token":3,"output_cost_per_token":5},
		"ambiguous": {"litellm_provider":"openai", "input_cost_per_token":1},
		"Ambiguous": {"litellm_provider":"openai", "input_cost_per_token":3}
	}`))
	require.NoError(t, err)
	service.replacePricingData(data)
	resolver := NewModelPricingResolver(nil, NewBillingService(nil, service))
	ctx := context.Background()
	oa := resolver.Resolve(ctx, PricingInput{Provider: PlatformOpenAI, Model: "shared"})
	require.Equal(t, PricingSourceLiteLLM, oa.Source)
	require.Equal(t, float64(1), oa.BasePricing.InputPricePerToken)
	require.False(t, oa.BasePricing.TokenPricePresence["output"])
	an := resolver.Resolve(ctx, PricingInput{Provider: PlatformAnthropic, Model: "shared"})
	require.Equal(t, float64(4), an.BasePricing.InputPricePerToken)
	require.Equal(t, float64(5), an.BasePricing.OutputPricePerToken)
	require.Nil(t, resolver.Resolve(ctx, PricingInput{Provider: PlatformOpenRouter, Model: "shared"}).BasePricing)
	require.Nil(t, resolver.Resolve(ctx, PricingInput{Provider: PlatformOpenAI, Model: "unowned"}).BasePricing)
	require.ErrorIs(t, resolver.Resolve(ctx, PricingInput{Provider: PlatformOpenAI, Model: "ambiguous"}).pricingErr, pricing.ErrAmbiguous)

	book := NewPlatformPriceBook(resolver)
	operator := &Group{ID: 5, Platform: PlatformOpenAI, ModelPricing: []ChannelModelPricing{{Models: []string{"ambiguous"}, InputPrice: priceFactNumber(2)}}}
	override, err := book.ResolveForGroup(ctx, pricing.Binding{GroupID: 5, Provider: PlatformOpenAI, Model: "ambiguous", At: time.Now(), RateMultiplier: 1}, operator)
	require.NoError(t, err)
	scenario := quotes.Scenario{Input: "10"}
	usage, err := scenario.Usage()
	require.NoError(t, err)
	price, err := override.Evaluate(scenario.PriceScenario(usage))
	require.NoError(t, err)
	require.Equal(t, "20.00000000", price.Charge.String())

	group := &Group{ID: 3, Platform: PlatformAnthropic, ModelPricing: []ChannelModelPricing{{Models: []string{"claude-sonnet-4"}, InputPrice: priceFactNumber(0)}}}
	tariff, err := book.ResolveForGroup(ctx, pricing.Binding{GroupID: 3, Provider: PlatformAnthropic, Model: "claude-sonnet-4", At: time.Now(), RateMultiplier: 1}, group)
	require.NoError(t, err)
	d := tariff.Describe()
	require.Equal(t, PricingSourceGroup, d.Source)
	require.Equal(t, PricingSourceStaticFallback, d.BaseSource)
	require.Equal(t, PricingSourceGroup, d.Rates[0].Origin)
	require.Equal(t, "0", d.Rates[0].Rate.Amount().String())
	require.Equal(t, PricingSourceStaticFallback, d.Rates[1].Origin)
}

func TestPriceBookFreeConflictAndSnapshotReplay(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 11, 19, 13, 0, 0, 0, time.UTC)
	billing := NewBillingService(nil, nil)
	book := NewPlatformPriceBook(NewModelPricingResolver(nil, billing))
	model := "configured-free"
	group := &Group{ID: 2, Platform: PlatformOpenAI, ModelPricing: []ChannelModelPricing{{Models: []string{model}, InputPrice: priceFactNumber(0), OutputPrice: priceFactNumber(0)}}}
	binding := pricing.Binding{Provider: PlatformOpenAI, Model: model, GroupID: 2, At: at, RateMultiplier: 1}
	tariff, err := book.ResolveForGroup(ctx, binding, group)
	require.NoError(t, err)
	*group.ModelPricing[0].InputPrice = 5
	scenario := quotes.Scenario{Input: "10"}
	usage, err := scenario.Usage()
	require.NoError(t, err)
	result, err := tariff.Evaluate(scenario.PriceScenario(usage))
	require.NoError(t, err)
	require.Equal(t, "free", result.State)
	require.Equal(t, "0.00000000", result.Charge.String())

	const receipt = `{"model":"retired-model","mode":"token","source":"fallback","price_card_version":"migration-245-original","tokens":{"InputTokens":13},"token_prices":{"InputPricePerToken":0.00001},"rate_multiplier":2,"time_multiplier":1,"reasoning_multiplier":1,"charged_amount":0.00026}`
	replayed, err := RecalculatePricingSnapshot(receipt)
	require.NoError(t, err)
	require.Equal(t, "migration-245-original", replayed.PriceCardVersion)
	require.Equal(t, "fallback", replayed.PriceSource)
	require.InDelta(t, 0.00026, replayed.ActualCost, 1e-12)

	late := at.Add(time.Hour)
	v := pricing.Version{ID: "migration-296-original", Source: "published", Currency: "USD", EffectiveFrom: at, EffectiveUntil: &late}
	rate, err := money.NewRate("USD", "0.01", "input_token", "1")
	require.NoError(t, err)
	v.Rules = []pricing.Rule{{Kind: "input", Rate: rate}}
	reference := v
	reference.ID, reference.Source = "ref_migration", "reference"
	version, err := pricing.ResolveExact([]pricing.Version{v, reference}, at)
	require.NoError(t, err)
	result, err = pricing.NewExactTariff(*version, at).Evaluate(scenario.PriceScenario(usage))
	require.NoError(t, err)
	require.Equal(t, "migration-296-original", *result.PriceVersion)
	require.Equal(t, "0.10000000", result.Charge.String())
	after, err := pricing.ResolveExact([]pricing.Version{v, reference}, late)
	require.NoError(t, err)
	require.Nil(t, after)
}

func TestPriceBookMissingProviderDoesNotTakeUnqualifiedCard(t *testing.T) {
	prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{"unowned-model": {InputCostPerToken: 9}}}
	billing := NewBillingService(nil, prices)
	_, err := billing.calculateCostWithServiceTierPolicy("unowned-model", UsageTokens{InputTokens: 1}, 1, "", true)
	require.NoError(t, err)
	_, err = billing.CalculateCostUnified(CostInput{Provider: PlatformOpenAI, Model: "unowned-model", Tokens: UsageTokens{InputTokens: 1}, RateMultiplier: 1})
	require.True(t, errors.Is(err, ErrModelPricingUnavailable))
}

func TestPriceBookSaleImporterRejectsReferenceDocument(t *testing.T) {
	s := &PricingService{}
	_, err := s.parsePricingData(modelpricing.PiSupplementJSON())
	require.Error(t, err)
	prices, err := s.parsePricingData([]byte(`{
		"openai/reference-candidate":{"source":"pi-ai","litellm_provider":"openai","input_cost_per_token":99},
		"openai/paid-candidate":{"litellm_provider":"openai","input_cost_per_token":1}
	}`))
	require.NoError(t, err)
	require.NotContains(t, prices, "openai/reference-candidate")
	require.Contains(t, prices, "openai/paid-candidate")
}

func TestPriceBookQuoteConflictDoesNotChooseOffer(t *testing.T) {
	at := time.Date(2026, 11, 19, 13, 0, 0, 0, time.UTC)
	id := catalog.StableID(PlatformOpenAI, "shared")
	data := []byte(`{
		"openai/shared":{"litellm_provider":"openai","input_cost_per_token":1},
		"anthropic/shared":{"litellm_provider":"anthropic","input_cost_per_token":5}
	}`)
	prices := &PricingService{}
	cards, err := prices.parsePricingData(data)
	require.NoError(t, err)
	prices.replacePricingData(cards)
	resolver := NewModelPricingResolver(nil, NewBillingService(nil, prices))
	d := catalog.Detail{Model: catalog.Summary{ID: id, AccessState: "allowed", RouteState: "eligible"}}
	scenario := quotes.Scenario{Input: "3"}
	usage, err := scenario.Usage()
	require.NoError(t, err)
	reader := priceBookTestCatalog{bindings: []pricing.Binding{
		{Provider: PlatformOpenAI, Model: "shared", RateMultiplier: 1},
		{Provider: PlatformAnthropic, Model: "shared", RateMultiplier: 1},
	}}
	service := quotes.Service{Book: NewPlatformPriceBook(resolver), Bindings: reader}
	result, err := service.EstimateFor(t.Context(), catalog.Perspective{Workspace: 1}, d, 0, "", "", scenario.PriceScenario(usage), at, at)
	require.NoError(t, err)
	require.Equal(t, pricing.Conflict, result.Reason)
	require.Nil(t, result.Charge)
}

func TestPriceBookCompositeDispatchKeepsSuccessfulTariffAndCeiling(t *testing.T) {
	const model = "composite-price-fixture"
	group := &Group{ID: 2, Platform: PlatformComposite, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{
		{Platform: PlatformAnthropic, Models: []string{model}, InputPrice: priceFactNumber(5), OutputPrice: priceFactNumber(5)},
		{Platform: PlatformOpenAI, Models: []string{model}, InputPrice: priceFactNumber(2), OutputPrice: priceFactNumber(2)},
	}}
	at := time.Date(2026, 11, 19, 13, 0, 0, 0, time.UTC)
	plan := NewGatewayRequestPlan(GatewayRequestSpec{Model: model, ExecutionID: "composite-price", UserID: 1, APIKeyID: 2, PricingAt: at})
	plan.billing = NewBillingService(nil, nil)
	plan.catalogRepo = &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{
		1: {ID: 1, Platform: PlatformAnthropic},
		2: {ID: 2, Platform: PlatformOpenAI},
	}}
	plan.budgetEstimate = &RequestBudgetEstimate{Group: group, InputTokens: 1, OutputTokens: 1, SkipKeyBudget: true, RateMultiplier: 1}
	plan.budgetPlatform = PlatformComposite
	ctx := withTPMWire(WithGatewayRequestPlan(t.Context(), plan), []byte(`{"model":"composite-price-fixture"}`), "/chat/completions")
	require.NoError(t, plan.admitPublicModelLane(ctx, 1))
	require.Equal(t, "10.00000000", plan.selectedPrice.Load().amount.String())
	require.NoError(t, plan.admitPublicModelLane(ctx, 2))
	lane := plan.selectedPrice.Load()
	require.Equal(t, PlatformOpenAI, lane.provider)
	require.Equal(t, "10.00000000", lane.amount.String())
	require.True(t, plan.Record())
	cost, err := plan.billing.CalculateCostUnified(CostInput{Ctx: ctx, Model: "upstream-response-alias", Group: group, RateMultiplier: 99,
		Tokens: UsageTokens{InputTokens: 1, OutputTokens: 1}})
	require.NoError(t, err)
	require.Equal(t, float64(4), cost.ActualCost)
	require.Equal(t, PricingSourceGroup, cost.PriceSource)
	require.Equal(t, lane.version, cost.PriceCardVersion)
}
