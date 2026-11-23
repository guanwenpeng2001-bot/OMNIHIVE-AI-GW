package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ctxkey"
)

// ErrSalePriceChanged is raised before admission when a confirmation cannot be
// bound to the exact inputs that the request will use for charging.
var ErrSalePriceChanged = &fault.Fault{Status: 409, Reason: "PRICE_QUOTE_CHANGED", Message: "The sale price changed. Obtain and confirm a new quote before running."}

type gatewaySalePricingKey struct{}

// gatewaySalePricing is a request-local immutable bundle, not an admitted plan.
// Keeping it separate prevents quote validation from claiming executions or
// reserving budgets. Publications replace their source maps; this bundle owns
// the selected pointers until the request is settled.
type gatewaySalePricing struct {
	keyID, payerID, workspaceID int64
	group                       *Group
	rate                        float64
	at                          time.Time
	registry                    *ModelRegistrySnapshot
	billing                     *BillingService
	channels                    *ChannelService
	channelSnapshot             *channelCache
	provider, model, version    string
}

func salePricingFromContext(ctx context.Context) *gatewaySalePricing {
	if ctx == nil {
		return nil
	}
	bundle, _ := ctx.Value(gatewaySalePricingKey{}).(*gatewaySalePricing)
	return bundle
}

// SalePricingInstant keeps time-dependent rules on the admission instant. A
// stream crossing a time band never changes the already accepted formula.
func SalePricingInstant(ctx context.Context, fallback time.Time) time.Time {
	if bundle := salePricingFromContext(ctx); bundle != nil {
		return bundle.at
	}
	return fallback
}

// FrozenSaleRate is used by both catalog projection and the existing gateway
// multiplier resolver. The payer check keeps act_as distinct from ownership.
func FrozenSaleRate(ctx context.Context, payer, group int64) (float64, bool) {
	if bundle := salePricingFromContext(ctx); bundle != nil && bundle.payerID == payer && bundle.group.ID == group {
		return bundle.rate, true
	}
	return 0, false
}

func frozenSaleGroup(ctx context.Context, group *Group) *Group {
	if bundle := salePricingFromContext(ctx); bundle != nil && group != nil && bundle.group.ID == group.ID {
		return bundle.group
	}
	return group
}

func frozenBillingService(billing *BillingService, registry *ModelRegistrySnapshot, at time.Time) *BillingService {
	if billing == nil {
		return nil
	}
	frozen := &BillingService{cfg: billing.cfg, fallbackPrices: billing.fallbackPrices, frozenPricingAt: at}
	if prices := billing.pricingService; prices != nil {
		prices.mu.RLock()
		copy := &PricingService{cfg: prices.cfg, pricingData: prices.pricingData, registrySnapshot: registry,
			localHash: prices.localHash, customFilesHash: prices.customFilesHash, lastUpdated: prices.lastUpdated, pricingRevision: prices.pricingRevision}
		copy.baseNameIndex.Store(prices.baseNameIndex.Load())
		prices.mu.RUnlock()
		frozen.pricingService = copy
	}
	return frozen
}

// CaptureSalePricing performs read-only capture before route/quote comparison.
// It returns a private key/group copy; live authentication caches are untouched.
func (s *GatewayService) CaptureSalePricing(ctx context.Context, key *APIKey, at time.Time) (context.Context, *APIKey, error) {
	if s == nil || s.billingService == nil || key == nil || key.ID <= 0 || key.UserID <= 0 || key.Group == nil || key.Group.ID <= 0 {
		return ctx, nil, ErrModelPricingUnavailable
	}
	if salePricingFromContext(ctx) != nil || GatewayRequestPlanFromContext(ctx) != nil {
		return ctx, nil, ErrSalePriceChanged
	}
	group := key.Group
	if s.groupRepo != nil {
		fresh, err := s.groupRepo.GetByID(ctx, group.ID)
		if err != nil {
			return ctx, nil, err
		}
		if fresh == nil || fresh.ID != group.ID || !fresh.IsActive() {
			return ctx, nil, ErrSalePriceChanged
		}
		group = fresh
	}
	// Group associations can contain back-pointers. Price/policy configuration
	// is a value tree; associations are fetched separately by the route evaluator.
	value := *group
	value.AccountGroups = nil
	body, err := json.Marshal(value)
	if err != nil {
		return ctx, nil, err
	}
	group = &Group{}
	if err := json.Unmarshal(body, group); err != nil {
		return ctx, nil, err
	}
	rate := group.RateMultiplier
	if s.userGroupRateRepo != nil {
		personal, err := s.userGroupRateRepo.GetByUserAndGroup(ctx, key.UserID, group.ID)
		if err != nil {
			return ctx, nil, fmt.Errorf("capture payer sale rate: %w", err)
		}
		if personal != nil {
			rate = *personal
		}
	}
	if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 {
		return ctx, nil, ErrInvalidBillingAmount
	}
	at, _, err = pricingEvaluationInstant(ctx, at, s.billingService.frozenPricingAt)
	if err != nil {
		return ctx, nil, err
	}
	registry := ModelRegistrySnapshotFromContext(ctx)
	bundle := &gatewaySalePricing{keyID: key.ID, payerID: key.UserID, workspaceID: key.WorkspaceID,
		group: group, rate: rate, at: at, registry: registry, billing: frozenBillingService(s.billingService, registry, at), channels: s.channelService}
	if s.channelService != nil {
		bundle.channelSnapshot, err = s.channelService.loadCache(ctx)
		if err != nil || bundle.channelSnapshot == nil {
			return ctx, nil, ErrModelPricingUnavailable
		}
	}
	copy := *key
	copy.Group = group
	ctx = WithModelRegistrySnapshot(ctx, registry)
	ctx = context.WithValue(ctx, ctxkey.Group, group)
	return context.WithValue(ctx, gatewaySalePricingKey{}, bundle), &copy, nil
}

// ConfirmSalePricing records the version computed from this captured catalog
// projection, after comparison with the user's confirmed quote. It grants no
// action and performs no journal or wallet write.
func ConfirmSalePricing(ctx context.Context, provider, model, version string) (context.Context, error) {
	bundle := salePricingFromContext(ctx)
	if bundle == nil || provider == "" || model == "" || version == "" {
		return ctx, ErrSalePriceChanged
	}
	copy := *bundle
	copy.provider, copy.model, copy.version = provider, model, version
	return context.WithValue(ctx, gatewaySalePricingKey{}, &copy), nil
}
