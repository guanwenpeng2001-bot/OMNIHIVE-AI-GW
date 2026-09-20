package service

import (
	"context"
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
	attempts := p.Attempts()
	if len(attempts) == 0 {
		return
	}
	accountID := attempts[len(attempts)-1].AccountID
	requestID := resolveUsageBillingRequestID(ctx, "")
	receipt := &UsageSettlementReceipt{
		Key:             UsageSettlementKey{RequestID: requestID, UserID: p.spec.UserID, APIKeyID: p.spec.APIKeyID},
		Command:         UsageBillingCommand{RequestID: requestID, UserID: p.spec.UserID, APIKeyID: p.spec.APIKeyID, AccountID: accountID, Model: p.spec.Model},
		Usage:           UsageLog{RequestID: requestID, UserID: p.spec.UserID, APIKeyID: p.spec.APIKeyID, AccountID: accountID, Model: p.spec.Model, PricingState: PricingStateError},
		ResolutionState: "unresolved", CompletionState: "interrupted",
	}
	if err := p.settlement.Submit(ctx, receipt, nil); err != nil {
		slog.Error("usage panic recovery receipt", "request_id", requestID, "failure_category", billingFailureCategory(err))
	}
}
