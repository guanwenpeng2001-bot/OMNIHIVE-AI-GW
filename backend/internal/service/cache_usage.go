package service

import (
	"context"
	"errors"
)

type CacheUsageRecorder interface {
	RecordCacheHit(context.Context, UsageSettlementKey) error
}

func (s *UsageSettlementService) LoadReceipt(ctx context.Context, key UsageSettlementKey) (*UsageSettlementReceipt, error) {
	if s == nil || s.repo == nil {
		return nil, ErrUsageSettlementNotFound
	}
	return s.repo.Get(ctx, key)
}

func (s *UsageSettlementService) RecordCacheHit(ctx context.Context, sourceKey UsageSettlementKey) error {
	if s == nil || s.repo == nil {
		return ErrUsageSettlementNotFound
	}
	plan := GatewayRequestPlanFromContext(ctx)
	if plan == nil || !plan.Record() {
		return ErrUsageSettlementNeedsReview
	}
	spec := plan.Snapshot()
	source, err := s.repo.Get(ctx, sourceKey)
	if err != nil || source == nil {
		return ErrUsageSettlementNotFound
	}
	key := UsageSettlementKey{RequestID: spec.ExecutionID, UserID: spec.UserID, APIKeyID: spec.APIKeyID}
	usage := source.Usage
	usage.ID = 0
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
	if err := s.Submit(ctx, receipt, nil); err != nil && !errors.Is(err, ErrUsageSettlementNeedsReview) {
		return err
	}
	return nil
}
