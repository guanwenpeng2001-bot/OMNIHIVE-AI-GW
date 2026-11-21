package service

import (
	"context"
	"time"
)

type CacheUsageRecorder interface {
	LoadReceipt(context.Context, UsageSettlementKey) (*UsageSettlementReceipt, error)
	RecordCacheHit(context.Context, *UsageSettlementReceipt) error
}

func (r *UsageSettlementReceipt) Cacheable() bool {
	return r != nil && (r.State == UsageSettlementSettled || r.State == UsageSettlementPending) && r.CompletionState == "complete" && r.ResolutionState == "resolved"
}

func (s *UsageSettlementService) LoadReceipt(ctx context.Context, key UsageSettlementKey) (*UsageSettlementReceipt, error) {
	if s == nil || s.repo == nil {
		return nil, ErrUsageSettlementNotFound
	}
	return s.repo.Get(ctx, key)
}

func (s *UsageSettlementService) RecordCacheHit(ctx context.Context, source *UsageSettlementReceipt) error {
	if s == nil || s.repo == nil {
		return ErrUsageSettlementNotFound
	}
	plan := GatewayRequestPlanFromContext(ctx)
	if plan == nil || !source.Cacheable() {
		return ErrUsageSettlementNeedsReview
	}
	spec := plan.Snapshot()
	if source.Key.UserID != spec.UserID || source.Key.APIKeyID != spec.APIKeyID || !plan.Record() {
		return ErrUsageSettlementNotFound
	}
	key := UsageSettlementKey{RequestID: spec.ExecutionID, UserID: spec.UserID, APIKeyID: spec.APIKeyID}
	usage := source.Usage
	usage.ID = 0
	usage.CreatedAt = time.Now()
	usage.RequestID = key.RequestID
	usage.UserID, usage.APIKeyID = key.UserID, key.APIKeyID
	usage.InboundEndpoint = optionalTrimmedStringPtr(spec.Route)
	usage.BillingEvidence = &UsageBillingEvidence{CacheHit: true}
	command := source.Command
	command.RequestID, command.UserID, command.APIKeyID = key.RequestID, key.UserID, key.APIKeyID
	command.RequestFingerprint = ""
	command.BudgetReservationID = 0
	command.ProjectReservationRef = ""
	command.ProjectWorkspaceID, command.ProjectID = 0, 0
	command.Estimate, command.Reserved = nil, nil
	receipt := &UsageSettlementReceipt{Key: key, Command: command, Usage: usage, ResolutionState: "resolved", CompletionState: "complete"}
	return s.Submit(ctx, receipt, nil)
}
