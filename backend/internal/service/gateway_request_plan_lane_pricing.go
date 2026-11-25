package service

import (
	"context"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/tidwall/gjson"
)

type requestLanePricing struct {
	model, provider string
	resolved        *ResolvedPricing
	resolver        *ModelPricingResolver
	amount          *money.Money
	multiplier      float64
	longContext     *bool
	version         string
}

func publicModelPricingPlatform(platform string) bool {
	return platform == PlatformKilo || platform == PlatformOpenCodeGo || platform == PlatformOpenRouter
}

func (p *GatewayRequestPlan) RequiresLanePricing() bool {
	if p == nil || p.store == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return publicModelPricingPlatform(p.budgetPlatform)
}

// Bind the tariff before recording a dispatch attempt, using the wire model.
func (p *GatewayRequestPlan) admitPublicModelLane(ctx context.Context, accountID int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if playgroundPriceFromContext(ctx) != nil {
		return nil
	}
	platform, _ := ResolvedTargetPlatformFromContext(ctx)
	if platform == "" {
		platform = p.budgetPlatform
	}
	composite := p.budgetEstimate != nil && p.budgetEstimate.Group != nil && p.budgetEstimate.Group.Platform == PlatformComposite
	if !publicModelPricingPlatform(platform) && !composite {
		p.selectedPrice.Store(nil)
		return nil
	}
	if p.catalogRepo == nil || p.billing == nil || p.budgetEstimate == nil {
		return ErrModelPricingUnavailable
	}
	wire, _ := ctx.Value(tpmWireKey{}).(tpmWireRequest)
	model := strings.TrimSpace(gjson.GetBytes(wire.body, "model").String())
	if model == "" {
		return ErrModelPricingUnavailable
	}
	account, err := p.catalogRepo.GetByID(ctx, accountID)
	if err != nil {
		return err
	}
	if account == nil || platform != "" && platform != PlatformComposite && account.Platform != platform {
		return ErrModelPricingUnavailable
	}
	platform = account.Platform
	ctx = WithResolvedTargetPlatform(ctx, platform)
	model = strings.TrimSpace(account.GetMappedModel(model))
	if model == "" {
		return ErrModelPricingUnavailable
	}

	estimate := *p.budgetEstimate
	estimate.Model = model
	resolver := NewModelPricingResolver(p.channels, p.billing)
	_, _, base := NormalizeBudgetEstimateBounds(estimate)
	key := &APIKey{Group: estimate.Group}
	multiplier, _ := computePeakAwareMultipliers(key, p.budgetBaseMultiplier(ctx, estimate.Group, base), p.spec.PricingAt)
	lane := requestLanePricing{provider: platform, resolver: resolver, multiplier: multiplier, longContext: openAILongContextBillingGate(account)}
	var resolved *ResolvedPricing
	var amount *money.Money
	if publicModelLane(account) {
		if account.IsOpenCodeZenFree() && !zenFreeModelAllowed(account, model) || account.IsKiloFree() && !verifiedKiloFreeTariff(account, model) {
			return ErrModelPricingUnavailable
		}
		resolved = newOfficialFreeTariff(platform)
		free, err := money.NewMoney("USD", "0")
		if err != nil {
			return err
		}
		amount = &free
	} else if account.IsOpenRouterFreeModel(model) {
		if !verifiedOpenRouterFreeTariff(account, model) {
			return ErrModelPricingUnavailable
		}
		resolved = newOfficialFreeTariff(platform)
		free, err := money.NewMoney("USD", "0")
		if err != nil {
			return err
		}
		amount = &free
		lane.model = model
	} else {
		if composite {
			model = p.compositeLanePriceModel(ctx, resolver, estimate.Group, model)
			estimate.Model = model
		}
		resolved = resolver.Resolve(ctx, PricingInput{Model: model, Provider: platform, Group: estimate.Group, GroupID: pricingGroupID(&APIKey{Group: estimate.Group})})
		if resolved.pricingErr != nil || resolved.Mode != BillingModeToken {
			return ErrModelPricingUnavailable
		}
		lane.model, lane.resolved = model, resolved
		amount = p.estimateBudgetHoldForLane(ctx, estimate, &lane)
		if amount == nil || publicModelPricingPlatform(platform) && amount.Amount().Compare(money.Amount{}) <= 0 {
			return ErrModelPricingUnavailable
		}
	}

	previous := p.selectedPrice.Load()
	if p.budgetAdmitted && previous != nil && amount.Amount().Compare(previous.amount.Amount()) > 0 {
		return ErrAPIKeyBudgetUnavailable
	}
	if !p.budgetAdmitted {
		estimate.frozenJob, estimate.frozenAmount = true, amount
		if err := p.reserveBudgetLocked(ctx, estimate); err != nil {
			return err
		}
	}
	// A paid-to-free switch can still reuse the original paid ceiling.
	ceiling := amount
	if previous != nil {
		ceiling = previous.amount
	}
	lane.model, lane.resolved, lane.amount = model, resolved, ceiling
	_, lane.version = pricingEvaluationIdentity(resolved, CostInput{Provider: platform, Model: model, Group: estimate.Group, PricingAt: p.spec.PricingAt,
		RateMultiplier: multiplier, ServiceTier: estimate.ServiceTier, ReasoningEffort: estimate.ReasoningEffort, LongContextBillingEnabled: lane.longContext})
	p.selectedPrice.Store(&lane)
	return nil
}

func (p *GatewayRequestPlan) compositeLanePriceModel(ctx context.Context, resolver *ModelPricingResolver, group *Group, outbound string) string {
	if p.channels == nil {
		return outbound
	}
	mapping := p.channels.ResolveChannelMapping(ctx, group.ID, p.spec.Model)
	var model string
	switch mapping.BillingModelSource {
	case BillingModelSourceRequested:
		model = p.spec.Model
	case BillingModelSourceChannelMapped:
		model = mapping.MappedModel
	}
	if model != "" && model != outbound && resolver.configuredForKey(ctx, model, &APIKey{Group: group}) != nil {
		return model
	}
	return outbound
}

func newOfficialFreeTariff(platform string) *ResolvedPricing {
	price := &ModelPricing{SupportsCacheBreakdown: true, TokenPricePresence: map[string]bool{
		"input": true, "output": true, "cache_read": true, "cache_write": true, "cache_write_1h": true,
		"image_input": true, "image_output": true, "image_cache_read": true,
	}}
	return &ResolvedPricing{Mode: BillingModeToken, BasePricing: price, Source: "official_free:" + platform, baseSource: "free_lane"}
}

func verifiedKiloFreeTariff(account *Account, model string) bool {
	if !kiloFreeModelAllowed(account, model) {
		return false
	}
	if catalog := account.GetUpstreamModelCatalog(); catalog != nil && catalog.Complete &&
		catalog.IdentityRevision == account.ModelCatalogBaseline().IdentityRevision {
		return true
	}
	// A suffix alone does not establish eligibility for the zero tariff.
	for _, supported := range kiloFreeModelIDs {
		if supported == model {
			return true
		}
	}
	return false
}

func selectedRequestLanePricing(ctx context.Context) *requestLanePricing {
	if plan := GatewayRequestPlanFromContext(ctx); plan != nil && plan.usage.Load() {
		return plan.selectedPrice.Load()
	}
	return nil
}
