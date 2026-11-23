package service

import (
	"context"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ctxkey"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/timezone"
)

type gatewayTokenRequestPricingAtCtxKey struct{}
type gatewayTokenRequestBillingGroupCtxKey struct{}

// WithGatewayTokenRequestPricing marks a shared-gateway request as token billed
// and freezes the downstream pricing instant for its whole lifetime. Media and
// metadata-only handlers deliberately do not call this helper.
func WithGatewayTokenRequestPricing(ctx context.Context) (context.Context, time.Time) {
	if ctx == nil {
		ctx = context.Background()
	}
	pricingAt, _, err := pricingEvaluationInstant(ctx, time.Time{}, time.Time{})
	if err != nil {
		return ctx, time.Time{}
	}
	ctx = context.WithValue(ctx, gatewayTokenRequestPricingAtCtxKey{}, pricingAt)
	// 调度过程中可能因 fallback/composite 路由覆盖 ctxkey.Group；计费 D 仍必须
	// 使用认证时刻的父分组，和最终 RecordUsage 的计费归属保持一致。
	if group, ok := ctx.Value(ctxkey.Group).(*Group); ok && IsGroupContextValid(group) {
		ctx = context.WithValue(ctx, gatewayTokenRequestBillingGroupCtxKey{}, group)
	}
	return ctx, pricingAt
}

// pricingEvaluationInstant chooses the request's already captured clock before
// considering a caller's explicit instant. The bool distinguishes a frozen
// request from an ordinary calculation that may resolve again at another time.
func pricingEvaluationInstant(ctx context.Context, requested, serviceFrozen time.Time) (time.Time, bool, error) {
	var captured [4]time.Time
	if bundle := salePricingFromContext(ctx); bundle != nil {
		captured[0] = bundle.at
	}
	if plan := GatewayRequestPlanFromContext(ctx); plan != nil {
		captured[1] = plan.Snapshot().PricingAt
	}
	if at, ok := gatewayTokenRequestPricingAtFromContext(ctx); ok {
		captured[2] = at
	}
	captured[3] = serviceFrozen
	var frozen time.Time
	for _, at := range captured {
		if at.IsZero() {
			continue
		}
		if !frozen.IsZero() && !frozen.Equal(at) {
			return time.Time{}, true, ErrSalePriceChanged
		}
		frozen = at
	}
	if !frozen.IsZero() {
		return frozen, true, nil
	}
	if !requested.IsZero() {
		return requested, false, nil
	}
	return timezone.Now(), false, nil
}

func gatewayTokenRequestPricingAtFromContext(ctx context.Context) (time.Time, bool) {
	if ctx == nil {
		return time.Time{}, false
	}
	pricingAt, ok := ctx.Value(gatewayTokenRequestPricingAtCtxKey{}).(time.Time)
	return pricingAt, ok && !pricingAt.IsZero()
}

// GatewayTokenRequestPricingAtFromContext exposes the frozen instant to
// handlers before they detach asynchronous usage-recording work.
func GatewayTokenRequestPricingAtFromContext(ctx context.Context) time.Time {
	pricingAt, _ := gatewayTokenRequestPricingAtFromContext(ctx)
	return pricingAt
}

func gatewayTokenRequestBillingGroupFromContext(ctx context.Context) *Group {
	if ctx == nil {
		return nil
	}
	group, _ := ctx.Value(gatewayTokenRequestBillingGroupCtxKey{}).(*Group)
	if IsGroupContextValid(group) {
		return group
	}
	return nil
}
