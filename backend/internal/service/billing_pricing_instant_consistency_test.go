//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

const billingInstantConsistencyModel = "fixture-pricing-instant"

func newBillingInstantConsistencyFixture(expiry time.Time) (*BillingService, *ModelPricingResolver, *Group) {
	inputPrice, outputPrice := 0.01, 0.02
	group := &Group{
		ID: 31, Platform: PlatformOpenAI, Status: StatusActive, RateMultiplier: 1,
		ModelPricing: []ChannelModelPricing{{
			Platform: PlatformOpenAI, Models: []string{billingInstantConsistencyModel}, BillingMode: BillingModeToken,
			PriceVariants: []ModelPriceVariant{{
				ID: billingInstantConsistencyModel, Canonical: billingInstantConsistencyModel,
				Lifecycle: "active", ExpiresAt: &expiry,
				Prices: []ModelUnitPrice{
					{Unit: "token", Dimension: "input", Currency: "USD", Sales: &inputPrice},
					{Unit: "token", Dimension: "output", Currency: "USD", Sales: &outputPrice},
				},
			}},
		}},
	}
	// No baseline tariff can rescue an expired configured variant.
	billing := &BillingService{fallbackPrices: map[string]*ModelPricing{}}
	return billing, NewModelPricingResolver(nil, billing), group
}

func billingInstantConsistencyInput(ctx context.Context, group *Group, resolver *ModelPricingResolver, at time.Time) CostInput {
	return CostInput{
		Ctx: ctx, Provider: PlatformOpenAI, Model: billingInstantConsistencyModel, Mode: BillingModeToken,
		Group: group, GroupID: &group.ID, Resolver: resolver, PricingAt: at,
		Tokens: UsageTokens{InputTokens: 100, OutputTokens: 10}, RequestCount: 1, RateMultiplier: 1.5,
	}
}

func billingInstantConsistencyPricingInput(group *Group, at time.Time) PricingInput {
	return PricingInput{
		Provider: PlatformOpenAI, Model: billingInstantConsistencyModel, Mode: BillingModeToken,
		Group: group, GroupID: &group.ID, PricingAt: at,
	}
}

func requireBillingInstantResolution(t *testing.T, resolved *ResolvedPricing, at time.Time) {
	t.Helper()
	require.NotNil(t, resolved)
	require.NotNil(t, resolved.descriptor)
	require.NotNil(t, resolved.Context)
	require.True(t, at.Equal(resolved.descriptor.PricingAt), "descriptor changed the selected pricing instant")
	require.True(t, at.Equal(resolved.Context.PricingAt), "price context changed the selected pricing instant")
	require.Equal(t, PlatformOpenAI, resolved.Context.Provider)
	require.Equal(t, billingInstantConsistencyModel, resolved.Context.Model)
}

func requireBillingInstantCost(t *testing.T, cost *CostBreakdown, at time.Time) {
	t.Helper()
	require.NotNil(t, cost)
	require.Equal(t, PricingStatePriced, cost.PricingState)
	require.Equal(t, PricingSourceGroup, cost.PriceSource)
	require.InDelta(t, 1.0, cost.InputCost, 1e-12)
	require.InDelta(t, 0.2, cost.OutputCost, 1e-12)
	require.InDelta(t, 1.2, cost.TotalCost, 1e-12)
	require.InDelta(t, 1.8, cost.ActualCost, 1e-12)
	var snapshot PricingSnapshot
	require.NoError(t, json.Unmarshal([]byte(cost.PricingSnapshot), &snapshot))
	require.NotNil(t, snapshot.PriceContext)
	require.NotNil(t, snapshot.TokenPrices)
	require.True(t, at.Equal(snapshot.PricingAt), "snapshot changed the selected pricing instant")
	require.True(t, at.Equal(snapshot.PriceContext.PricingAt), "snapshot and price context must freeze the same instant")
	require.Equal(t, PlatformOpenAI, snapshot.Provider)
	require.Equal(t, PlatformOpenAI, snapshot.PriceContext.Provider)
	require.Equal(t, 0.01, snapshot.TokenPrices.InputPricePerToken)
	require.Equal(t, 0.02, snapshot.TokenPrices.OutputPricePerToken)
	require.Equal(t, cost.PriceCardVersion, snapshot.PriceCardVersion)
	replayed, err := RecalculatePricingSnapshot(cost.PricingSnapshot)
	require.NoError(t, err)
	require.InDelta(t, cost.TotalCost, replayed.TotalCost, 1e-12)
	require.InDelta(t, cost.ActualCost, replayed.ActualCost, 1e-12)
}

func TestBillingPricingInstantExplicitHistoryAndExpiry(t *testing.T) {
	expiry := time.Date(2020, 1, 2, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		at     time.Time
		active bool
	}{
		{name: "before", at: expiry.Add(-time.Nanosecond), active: true},
		{name: "at_expiry", at: expiry},
		{name: "after", at: expiry.Add(time.Nanosecond)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billing, resolver, group := newBillingInstantConsistencyFixture(expiry)
			ctx := context.Background()
			in := billingInstantConsistencyPricingInput(group, tc.at)
			descriptor, err := resolver.ResolveDescriptor(ctx, in)
			require.NoError(t, err)
			require.True(t, tc.at.Equal(descriptor.PricingAt))
			inputRate, lookupErr := descriptor.UnitPrice("token", "input", "")
			resolved := resolver.Resolve(ctx, in)
			requireBillingInstantResolution(t, resolved, tc.at)

			// Resolve again inside the public calculation entry point so its
			// provider and explicit historical instant must both propagate.
			cost, err := billing.CalculateCostUnified(billingInstantConsistencyInput(ctx, group, resolver, tc.at))
			if tc.active {
				require.NoError(t, lookupErr)
				require.NotNil(t, inputRate.Sales)
				require.Equal(t, 0.01, *inputRate.Sales)
				require.NotNil(t, resolved.BasePricing)
				require.NoError(t, err)
				requireBillingInstantCost(t, cost, tc.at)
				_, version := pricingAuditIdentity(resolved)
				require.Equal(t, version, cost.PriceCardVersion)
			} else {
				require.ErrorIs(t, lookupErr, ErrModelPricingUnavailable)
				require.Nil(t, resolved.BasePricing)
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				require.NotNil(t, cost)
				require.Equal(t, PricingStateUnpriced, cost.PricingState)
				require.Zero(t, cost.ActualCost)
				require.Zero(t, cost.TotalCost)
				require.Empty(t, cost.PricingSnapshot)
			}
		})
	}
}

func TestBillingPricingInstantPreResolvedExpiryReevaluation(t *testing.T) {
	expiry := time.Date(2020, 1, 2, 12, 0, 0, 0, time.UTC)
	before := expiry.Add(-time.Nanosecond)
	for _, tc := range []struct {
		name       string
		resolvedAt time.Time
		requested  time.Time
		wantAt     time.Time
		active     bool
	}{
		{name: "keeps_historical_preselection_without_override", resolvedAt: before, wantAt: before, active: true},
		{name: "before_to_expiry", resolvedAt: before, requested: expiry},
		{name: "before_to_after", resolvedAt: before, requested: expiry.Add(time.Nanosecond)},
		{name: "expired_to_valid_history", resolvedAt: expiry, requested: before, wantAt: before, active: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billing, resolver, group := newBillingInstantConsistencyFixture(expiry)
			ctx := context.Background()
			resolved := resolver.Resolve(ctx, billingInstantConsistencyPricingInput(group, tc.resolvedAt))
			requireBillingInstantResolution(t, resolved, tc.resolvedAt)
			originalContext := *resolved.Context
			originalDescriptor, err := json.Marshal(resolved.descriptor)
			require.NoError(t, err)
			in := billingInstantConsistencyInput(ctx, group, resolver, tc.requested)
			in.Resolved = resolved
			cost, calculationErr := billing.CalculateCostUnified(in)
			afterDescriptor, err := json.Marshal(resolved.descriptor)
			require.NoError(t, err)
			require.Equal(t, originalContext, *resolved.Context, "calculation must not relabel a shared preselection")
			require.Equal(t, originalDescriptor, afterDescriptor, "calculation must not mutate a preselected variant")
			if tc.active {
				require.NoError(t, calculationErr)
				requireBillingInstantCost(t, cost, tc.wantAt)
			} else {
				require.ErrorIs(t, calculationErr, ErrModelPricingUnavailable)
				require.NotNil(t, cost)
				require.Equal(t, PricingStateUnpriced, cost.PricingState)
				require.Zero(t, cost.ActualCost)
				require.Zero(t, cost.TotalCost)
				require.Empty(t, cost.PricingSnapshot)
			}
		})
	}
}

func TestBillingPricingInstantFrozenRequestWinsAndRejectsStalePreselection(t *testing.T) {
	for _, source := range []string{"sale_bundle", "request_plan", "token_context"} {
		t.Run(source, func(t *testing.T) {
			ctx := context.Background()
			at := time.Date(2020, 1, 2, 12, 0, 0, 0, time.UTC)
			if source == "token_context" {
				ctx, at = WithGatewayTokenRequestPricing(ctx)
			}
			expiry := at.Add(time.Hour)
			billing, resolver, group := newBillingInstantConsistencyFixture(expiry)
			switch source {
			case "sale_bundle":
				gateway := &GatewayService{billingService: billing}
				key := &APIKey{ID: 2, WorkspaceID: 1, UserID: 3, GroupID: &group.ID, Group: group}
				capturedCtx, capturedKey, err := gateway.CaptureSalePricing(ctx, key, at)
				require.NoError(t, err)
				require.Nil(t, GatewayRequestPlanFromContext(capturedCtx))
				ctx, group = capturedCtx, capturedKey.Group
			case "request_plan":
				ctx = WithGatewayRequestPlan(ctx, NewGatewayRequestPlan(GatewayRequestSpec{
					WorkspaceID: 1, UserID: 3, APIKeyID: 2, BillingGroupID: group.ID,
					Model: billingInstantConsistencyModel, PricingAt: at,
				}))
			}

			// The later caller-supplied instant would expire the tariff, but an
			// accepted request must continue using the captured rate and time.
			later := expiry.Add(time.Hour)
			resolved := resolver.Resolve(ctx, billingInstantConsistencyPricingInput(group, later))
			requireBillingInstantResolution(t, resolved, at)
			require.NotNil(t, resolved.BasePricing)
			for _, usePreselection := range []bool{false, true} {
				in := billingInstantConsistencyInput(ctx, group, resolver, later)
				if usePreselection {
					in.Resolved = resolved
				}
				cost, err := billing.CalculateCostUnified(in)
				require.NoError(t, err)
				requireBillingInstantCost(t, cost, at)
			}

			// An unrelated preselection cannot be relabeled as the admitted
			// price, even when its nominal rates happen to be identical.
			oldAt := at.Add(-time.Hour)
			stale := resolver.Resolve(context.Background(), billingInstantConsistencyPricingInput(group, oldAt))
			requireBillingInstantResolution(t, stale, oldAt)
			require.NotNil(t, stale.BasePricing)
			originalContext := *stale.Context
			originalDescriptor, err := json.Marshal(stale.descriptor)
			require.NoError(t, err)
			in := billingInstantConsistencyInput(ctx, group, resolver, later)
			in.Resolved = stale
			cost, err := billing.CalculateCostUnified(in)
			require.ErrorIs(t, err, ErrSalePriceChanged)
			require.NotNil(t, cost)
			require.Equal(t, PricingStateError, cost.PricingState)
			require.Zero(t, cost.ActualCost)
			require.Zero(t, cost.TotalCost)
			require.Empty(t, cost.PricingSnapshot)
			afterDescriptor, err := json.Marshal(stale.descriptor)
			require.NoError(t, err)
			require.Equal(t, originalContext, *stale.Context)
			require.Equal(t, originalDescriptor, afterDescriptor)
		})
	}
}

func TestBillingPricingInstantTokenCaptureOrderIsIdempotent(t *testing.T) {
	for _, target := range []string{"sale_bundle", "request_plan"} {
		for _, explicit := range []bool{false, true} {
			name := target + "/default"
			if explicit {
				name = target + "/later_explicit"
			}
			t.Run(name, func(t *testing.T) {
				ctx, at := WithGatewayTokenRequestPricing(context.Background())
				ctx, repeatedAt := WithGatewayTokenRequestPricing(ctx)
				require.True(t, at.Equal(repeatedAt), "repeated token context must retain the original instant")
				billing, resolver, group := newBillingInstantConsistencyFixture(at.Add(time.Hour))
				var requested time.Time
				if explicit {
					requested = at.Add(2 * time.Hour)
				}
				switch target {
				case "sale_bundle":
					gateway := &GatewayService{billingService: billing}
					key := &APIKey{ID: 2, WorkspaceID: 1, UserID: 3, GroupID: &group.ID, Group: group}
					capturedCtx, capturedKey, err := gateway.CaptureSalePricing(ctx, key, requested)
					require.NoError(t, err)
					ctx, group = capturedCtx, capturedKey.Group
					require.True(t, at.Equal(SalePricingInstant(ctx, time.Time{})))
				case "request_plan":
					spec := GatewayRequestSpec{
						WorkspaceID: 1, UserID: 3, APIKeyID: 2, BillingGroupID: group.ID,
						Model: billingInstantConsistencyModel, PricingAt: requested,
					}
					capturedCtx, plan := captureGatewayRequestPlan(ctx, spec, nil, billing, nil)
					require.NoError(t, plan.admissionErr)
					ctx = capturedCtx
					require.True(t, at.Equal(plan.Snapshot().PricingAt))
				}
				ctx, finalAt := WithGatewayTokenRequestPricing(ctx)
				require.True(t, at.Equal(finalAt), "capture must not split the existing request clock")
				resolved := resolver.Resolve(ctx, billingInstantConsistencyPricingInput(group, at.Add(2*time.Hour)))
				requireBillingInstantResolution(t, resolved, at)
				cost, err := billing.CalculateCostUnified(billingInstantConsistencyInput(ctx, group, resolver, at.Add(2*time.Hour)))
				require.NoError(t, err)
				requireBillingInstantCost(t, cost, at)
			})
		}
	}
}

func TestBillingPricingInstantBundleKeepsClockAndGroupSnapshot(t *testing.T) {
	at := time.Date(2020, 1, 2, 12, 0, 0, 0, time.UTC)
	billing, resolver, liveGroup := newBillingInstantConsistencyFixture(at.Add(time.Hour))
	gateway := &GatewayService{billingService: billing}
	key := &APIKey{ID: 2, WorkspaceID: 1, UserID: 3, GroupID: &liveGroup.ID, Group: liveGroup}
	ctx, capturedKey, err := gateway.CaptureSalePricing(context.Background(), key, at)
	require.NoError(t, err)
	require.NotSame(t, liveGroup, capturedKey.Group)
	ctx, tokenAt := WithGatewayTokenRequestPricing(ctx)
	require.True(t, at.Equal(tokenAt), "adding token pricing must retain the captured sale instant")
	ctx, repeatedAt := WithGatewayTokenRequestPricing(ctx)
	require.True(t, at.Equal(repeatedAt))

	// Mutate the caller's original group without changing its ID. Both the
	// descriptor path and billing must keep using the bundle's private copy.
	*liveGroup.ModelPricing[0].PriceVariants[0].Prices[0].Sales = 9
	*liveGroup.ModelPricing[0].PriceVariants[0].Prices[1].Sales = 11
	liveDescriptor, err := resolver.ResolveDescriptor(context.Background(), billingInstantConsistencyPricingInput(liveGroup, at))
	require.NoError(t, err)
	liveInput, err := liveDescriptor.UnitPrice("token", "input", "")
	require.NoError(t, err)
	require.NotNil(t, liveInput.Sales)
	require.Equal(t, 9.0, *liveInput.Sales, "the live card really changed")

	later := at.Add(2 * time.Hour)
	descriptor, err := resolver.ResolveDescriptor(ctx, billingInstantConsistencyPricingInput(liveGroup, later))
	require.NoError(t, err)
	require.True(t, at.Equal(descriptor.PricingAt))
	for dimension, expected := range map[string]float64{"input": 0.01, "output": 0.02} {
		price, lookupErr := descriptor.UnitPrice("token", dimension, "")
		require.NoError(t, lookupErr)
		require.NotNil(t, price.Sales)
		require.Equal(t, expected, *price.Sales)
	}
	resolved := resolver.Resolve(ctx, billingInstantConsistencyPricingInput(liveGroup, later))
	requireBillingInstantResolution(t, resolved, at)
	cost, err := billing.CalculateCostUnified(billingInstantConsistencyInput(ctx, liveGroup, resolver, later))
	require.NoError(t, err)
	requireBillingInstantCost(t, cost, at)
	require.Equal(t, 9.0, *liveGroup.ModelPricing[0].PriceVariants[0].Prices[0].Sales, "calculation must not rewrite the live card")
}

func TestBillingPricingInstantRecordUsagePeakAmountUsesFrozenClock(t *testing.T) {
	for _, adapter := range []string{"gateway", "openai"} {
		for _, missing := range []bool{false, true} {
			name := adapter + "/late_record_at"
			if missing {
				name = adapter + "/missing_record_at"
			}
			t.Run(name, func(t *testing.T) {
				at := time.Date(2020, 1, 2, 8, 30, 0, 0, timezone.Location())
				recordAt := at.Add(2 * time.Hour)
				if missing {
					recordAt = time.Time{}
				}
				billing, resolver, group := newBillingInstantConsistencyFixture(at.Add(24 * time.Hour))
				group.RateMultiplier = 1.5
				group.SubscriptionType = SubscriptionTypeSubscription
				group.PeakRateEnabled = true
				group.PeakStart, group.PeakEnd, group.PeakRateMultiplier = "08:00", "09:00", 3
				key := &APIKey{ID: 1, UserID: 2, GroupID: &group.ID, Group: group}
				user := &User{ID: 2}
				account := &Account{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
				plan := NewGatewayRequestPlan(GatewayRequestSpec{
					UserID: user.ID, APIKeyID: key.ID, BillingGroupID: group.ID,
					Model: billingInstantConsistencyModel, PricingAt: at,
				})
				ctx := WithGatewayRequestPlan(context.Background(), plan)
				logs := &openAIRecordUsageLogRepoStub{inserted: true}
				users := &openAIRecordUsageUserRepoStub{}
				subscriptions := &openAIRecordUsageSubRepoStub{}
				var receipt *UsageSettlementReceipt
				if adapter == "gateway" {
					svc := newGatewayRecordUsageServiceForTest(logs, users, subscriptions)
					svc.billingService, svc.resolver = billing, resolver
					err := svc.RecordUsage(ctx, &RecordUsageInput{
						APIKey: key, User: user, Account: account, PricingAt: recordAt,
						Result: &ForwardResult{
							RequestID: "frozen-peak-gateway", Model: billingInstantConsistencyModel,
							UpstreamModel: billingInstantConsistencyModel,
							Usage: ClaudeUsage{InputTokens: 100, OutputTokens: 10},
						},
					})
					require.NoError(t, err)
					fixture, ok := svc.usageSettlement.repo.(*recordUsageReceiptFixture)
					require.True(t, ok)
					receipt = fixture.receipt
				} else {
					svc := newOpenAIRecordUsageServiceForTest(logs, users, subscriptions, nil)
					svc.billingService, svc.resolver = billing, resolver
					err := svc.RecordUsage(ctx, &OpenAIRecordUsageInput{
						APIKey: key, User: user, Account: account, PricingAt: recordAt,
						Result: &OpenAIForwardResult{
							RequestID: "frozen-peak-openai", Model: billingInstantConsistencyModel,
							UpstreamModel: billingInstantConsistencyModel,
							Usage: OpenAIUsage{InputTokens: 100, OutputTokens: 10},
						},
					})
					require.NoError(t, err)
					receipt = receiptFixture(t, svc).receipt
				}
				require.NotNil(t, receipt)
				require.Equal(t, UsageSettlementSettled, receipt.State)
				require.Equal(t, PricingStatePriced, receipt.Usage.PricingState)
				require.InDelta(t, 1.2, receipt.Usage.TotalCost, 1e-12)
				require.InDelta(t, 4.5, receipt.Usage.RateMultiplier, 1e-12)
				require.InDelta(t, 5.4, receipt.Usage.ActualCost, 1e-12)
				require.InDelta(t, 5.4, receipt.Command.BalanceCost, 1e-12)
				require.Equal(t, 1, users.deductCalls)
				require.InDelta(t, 5.4, users.lastAmount, 1e-12)
				require.Equal(t, 1, logs.calls)
				var snapshot PricingSnapshot
				require.NoError(t, json.Unmarshal([]byte(receipt.Usage.PricingSnapshot), &snapshot))
				require.NotNil(t, snapshot.PriceContext)
				require.True(t, at.Equal(snapshot.PricingAt))
				require.True(t, at.Equal(snapshot.PriceContext.PricingAt))
				replayed, err := RecalculatePricingSnapshot(receipt.Usage.PricingSnapshot)
				require.NoError(t, err)
				require.InDelta(t, 5.4, replayed.ActualCost, 1e-12)
			})
		}
	}
}

func TestBillingPricingInstantLegacyTokenAdapterKeepsExplicitHistory(t *testing.T) {
	at := time.Date(2020, 1, 2, 12, 0, 0, 0, time.UTC)
	billing := &BillingService{pricingService: &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			billingInstantConsistencyModel: {
				LiteLLMProvider: PlatformOpenAI, InputCostPerToken: 0.01, OutputCostPerToken: 0.02,
			},
		},
	}}
	cost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{
		Ctx: context.Background(), Model: billingInstantConsistencyModel, PricingAt: at,
		Tokens: UsageTokens{InputTokens: 100, OutputTokens: 10}, RateMultiplier: 1.5,
	})
	require.NoError(t, err)
	require.InDelta(t, 1.2, cost.TotalCost, 1e-12)
	require.InDelta(t, 1.8, cost.ActualCost, 1e-12)
	var snapshot PricingSnapshot
	require.NoError(t, json.Unmarshal([]byte(cost.PricingSnapshot), &snapshot))
	require.True(t, at.Equal(snapshot.PricingAt), "the no-group legacy adapter must preserve the explicit instant")
	require.NotNil(t, snapshot.TokenPrices)
	require.Equal(t, 0.01, snapshot.TokenPrices.InputPricePerToken)
	require.Equal(t, 0.02, snapshot.TokenPrices.OutputPricePerToken)
	replayed, err := RecalculatePricingSnapshot(cost.PricingSnapshot)
	require.NoError(t, err)
	require.InDelta(t, 1.8, replayed.ActualCost, 1e-12)
}
