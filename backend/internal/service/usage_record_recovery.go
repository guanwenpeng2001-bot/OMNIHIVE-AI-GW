package service

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
)

var usageRecordPanicTotal atomic.Uint64

// UsageRecordPanicCount counts failures even when the bounded replay succeeds.
func UsageRecordPanicCount() uint64 { return usageRecordPanicTotal.Load() }

// RunUsageEvidenceTask retries only persistence of already-observed evidence.
// The task must never generate upstream output. Durable settlement is idempotent.
func RunUsageEvidenceTask(ctx context.Context, task UsageRecordTask) {
	for replay := 0; replay < 2; replay++ {
		panicked := false
		func() {
			defer func() {
				if recover() != nil {
					panicked = true
					usageRecordPanicTotal.Add(1)
					if plan := GatewayRequestPlanFromContext(ctx); plan != nil {
						plan.mu.Lock()
						plan.state.UsageRecoveryRequired = true
						plan.mu.Unlock()
					}
					slog.Error("usage evidence task panic", "replay", replay)
				}
			}()
			task(ctx)
		}()
		if !panicked {
			if plan := GatewayRequestPlanFromContext(ctx); plan != nil {
				plan.mu.Lock()
				plan.state.UsageRecoveryRequired = false
				plan.mu.Unlock()
			}
			return
		}
		if ctx.Err() != nil {
			break
		}
	}
	if plan := GatewayRequestPlanFromContext(ctx); plan != nil {
		plan.persistUsagePanic(ctx)
	}
}

// When calculation itself panics twice, retain an unresolved review receipt.
// Unknown quantities are never priced or debited; operator reconciliation can
// distinguish this failure from a successful zero-usage response.
func (p *GatewayRequestPlan) persistUsagePanic(ctx context.Context) {
	if p.settlement == nil || p.State().ReceiptState != "" {
		return
	}
	p.persistUnresolvedUsage(ctx, false)
}

// ReviewInterruptedStream records delivered SSE output even when the adapter returned no result.
func (p *GatewayRequestPlan) ReviewInterruptedStream(ctx context.Context) {
	if p == nil || p.settlement == nil || len(p.Attempts()) == 0 || !p.Record() {
		return
	}
	p.persistUnresolvedUsage(ctx, true)
}

// ReviewFailedGeneration closes an accepted non-stream attempt whose adapter
// failed before handing any usage evidence to settlement.
func (p *GatewayRequestPlan) ReviewFailedGeneration(ctx context.Context) {
	attempts := p.Attempts()
	if p.settlement == nil || len(attempts) == 0 {
		return
	}
	last := attempts[len(attempts)-1]
	if last.Outcome == "transport_response" && last.Status >= 200 && last.Status < 300 && p.Record() {
		p.persistUnresolvedUsage(ctx, false)
	}
}

func (p *GatewayRequestPlan) persistUnresolvedUsage(ctx context.Context, stream bool) {
	attempts := p.Attempts()
	if len(attempts) == 0 {
		return
	}
	accountID := attempts[len(attempts)-1].AccountID
	requestID := resolveUsageBillingRequestID(ctx, "")
	receipt := &UsageSettlementReceipt{
		Key:             UsageSettlementKey{RequestID: requestID, UserID: p.spec.UserID, APIKeyID: p.spec.APIKeyID},
		Command:         UsageBillingCommand{RequestID: requestID, UserID: p.spec.UserID, APIKeyID: p.spec.APIKeyID, AccountID: accountID, Model: p.spec.Model},
		Usage:           UsageLog{Stream: stream, InboundEndpoint: optionalTrimmedStringPtr(p.spec.Route), RequestID: requestID, UserID: p.spec.UserID, APIKeyID: p.spec.APIKeyID, AccountID: accountID, Model: p.spec.Model, PricingState: PricingStateError},
		ResolutionState: "unresolved", CompletionState: "interrupted",
	}
	if err := p.settlement.Submit(ctx, receipt, nil); err != nil && !errors.Is(err, ErrUsageSettlementNeedsReview) {
		slog.Error("unresolved usage receipt", "request_id", requestID, "failure_category", billingFailureCategory(err))
	}
}
