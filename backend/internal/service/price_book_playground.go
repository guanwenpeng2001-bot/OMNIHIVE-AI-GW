package service

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/shopspring/decimal"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/money"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
)

type playgroundPriceKey struct{}

type PlaygroundPrice struct {
	price *platformTariff
	at    time.Time
}

func (p PlaygroundPrice) At() time.Time {
	return p.at
}

func (p PlaygroundPrice) Version() string {
	if p.price == nil {
		return ""
	}
	return p.price.description.VersionID
}

func (p PlaygroundPrice) InputRate(inputBound string) (*money.Amount, error) {
	if p.price == nil || p.price.input.Mode != BillingModeToken ||
		p.price.description.Outcome == pricing.Missing || p.price.description.Outcome == pricing.Conflict ||
		needsConditionScenario(p.price.input.Resolved) {
		return nil, nil
	}
	tokens, err := strconv.Atoi(inputBound)
	if err != nil || tokens <= 0 {
		return nil, err
	}
	input := p.price.input
	input.Tokens.InputTokens = tokens
	input.displayProbe = true
	cost, err := p.price.billing.CalculateCostUnified(input)
	if errors.Is(err, ErrModelPricingUnavailable) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return unitRatePerMillion(decimal.NewFromFloat(cost.ActualCost), int64(tokens))
}

// PlaygroundPriceContext pins the same source snapshots for all candidates.
// The chosen tariff is passed on to the native gateway when it admits the run.
func (s *GatewayService) PlaygroundPriceContext(ctx context.Context, at time.Time) context.Context {
	return NewPlatformPriceBook(NewModelPricingResolver(s.channelService, s.billingService)).Snapshot(ctx, at)
}

func (s *GatewayService) ResolvePlaygroundPrice(ctx context.Context, key *APIKey, accountID int64, provider, model, funding, action, protocol string, at time.Time) (PlaygroundPrice, error) {
	decision := PlaygroundPrice{at: at}
	if key == nil || key.Group == nil || s.accountRepo == nil || s.billingService == nil {
		return decision, nil
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return decision, err
	}
	if account == nil || account.Platform != provider {
		return decision, nil
	}
	group := key.Group
	ctx = WithResolvedTargetPlatform(ctx, provider)
	gid := pricingGroupID(key)
	mapping := ChannelMappingResult{MappedModel: model}
	if gid != nil && s.channelService != nil {
		mapping = s.channelService.ResolveChannelMapping(ctx, *gid, model)
	}
	outbound := account.GetMappedModel(mapping.MappedModel)
	if provider == PlatformOpenAI {
		outbound = resolveOpenAIForwardModel(account, mapping.MappedModel, group.DefaultMappedModel)
	}
	billingModel := outbound
	resolver := NewModelPricingResolver(s.channelService, s.billingService)
	switch mapping.BillingModelSource {
	case BillingModelSourceRequested:
		billingModel = model
	case BillingModelSourceChannelMapped:
		billingModel = mapping.MappedModel
	}
	base := s.ResolveUserGroupRateMultiplier(ctx, key.UserID, group.ID, group.RateMultiplier)
	book := NewPlatformPriceBook(resolver)
	binding := pricing.Binding{Provider: provider, Model: billingModel, WireModel: outbound, Funding: funding, GroupID: group.ID, ChannelID: mapping.ChannelID, Action: action, Protocol: protocol, Mode: string(BillingModeToken), RateMultiplier: base, At: at}
	tariff, err := book.ResolveForGroup(ctx, binding, group)
	if err != nil {
		return decision, err
	}
	price, ok := tariff.(*platformTariff)
	if !ok {
		return decision, errors.New("playground price book returned an unexpected tariff")
	}
	if publicModelLane(account) {
		if account.IsOpenCodeZenFree() && !zenFreeModelAllowed(account, outbound) || account.IsKiloFree() && !verifiedKiloFreeTariff(account, outbound) {
			return decision, nil
		}
		if price.billing == nil {
			return decision, nil
		}
		price.input.Model = outbound
		price.input.Resolved = newOfficialFreeTariff(provider)
	} else if verifiedOpenRouterFreeTariff(account, outbound) {
		price.input.Model = outbound
		price.input.Resolved = newOfficialFreeTariff(provider)
	}
	price.input.LongContextBillingEnabled = openAILongContextBillingGate(account)
	if price.input.Resolved != nil {
		price.description = describePlatformTariff(price.input)
		decision.price = price
	}
	return decision, nil
}

func WithPlaygroundPrice(ctx context.Context, price PlaygroundPrice) context.Context {
	if price.price == nil {
		return ctx
	}
	return context.WithValue(ctx, playgroundPriceKey{}, price)
}

func playgroundPriceFromContext(ctx context.Context) *platformTariff {
	if ctx != nil {
		if p, ok := ctx.Value(playgroundPriceKey{}).(PlaygroundPrice); ok {
			return p.price
		}
	}
	return nil
}
