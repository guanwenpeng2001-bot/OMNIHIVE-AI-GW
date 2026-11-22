package service

import (
	"context"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/tidwall/gjson"
)

type requestLanePricing struct {
	model    string
	resolved *ResolvedPricing
	resolver *ModelPricingResolver
	amount   *money.Money
}

func publicModelPricingPlatform(platform string) bool {
	return platform == PlatformKilo || platform == PlatformOpenCodeGo
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
	platform, _ := ResolvedTargetPlatformFromContext(ctx)
	if platform == "" {
		platform = p.budgetPlatform
	}
	if !publicModelPricingPlatform(platform) {
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
	if account == nil || account.Platform != platform {
		return ErrModelPricingUnavailable
	}

	estimate := *p.budgetEstimate
	estimate.Model = model
	resolver := NewModelPricingResolver(p.channels, p.billing)
	var resolved *ResolvedPricing
	var amount *money.Money
	if publicModelLane(account) {
		if account.IsOpenCodeZenFree() && !zenFreeModelAllowed(account, model) || account.IsKiloFree() && !verifiedKiloFreeTariff(account, model) {
			return ErrModelPricingUnavailable
		}
		price := &ModelPricing{SupportsCacheBreakdown: true, TokenPricePresence: map[string]bool{
			"input": true, "output": true, "cache_read": true, "cache_write": true, "cache_write_1h": true,
			"image_input": true, "image_output": true, "image_cache_read": true,
		}}
		resolved = &ResolvedPricing{Mode: BillingModeToken, BasePricing: price, Source: "official_free:" + platform}
		free, err := money.NewMoney("USD", "0")
		if err != nil {
			return err
		}
		amount = &free
	} else {
		resolved = resolver.Resolve(ctx, PricingInput{Model: model, Group: estimate.Group, GroupID: pricingGroupID(&APIKey{Group: estimate.Group})})
		if resolved.pricingErr != nil || resolved.Mode != BillingModeToken {
			return ErrModelPricingUnavailable
		}
		amount = p.estimateBudgetHold(ctx, estimate)
		if amount == nil || amount.Amount().Compare(money.Amount{}) <= 0 {
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
	p.selectedPrice.Store(&requestLanePricing{model, resolved, resolver, ceiling})
	return nil
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
