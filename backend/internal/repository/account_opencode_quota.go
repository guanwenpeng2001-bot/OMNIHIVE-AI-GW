package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ApplyOpenCodeQuotaProbe reuses the runtime identity transaction so a response
// for a replaced key cannot revive an account or poison the replacement quota.
func (r *accountRepository) ApplyOpenCodeQuotaProbe(ctx context.Context, expected *service.Account, result *service.CNProviderQuotaProbeResult) error {
	if expected == nil || result == nil {
		return service.ErrAccountNilInput
	}
	return r.withRuntimeIdentity(ctx, expected.ID, service.AccountRuntimeIdentityKey(expected), func(lockedCtx context.Context, repository *accountRepository) error {
		entity, err := repository.client.Account.Get(lockedCtx, expected.ID)
		if err != nil {
			return err
		}
		current := accountEntityToService(entity)
		if stamp, ok := current.Extra["opencode_go_usage_updated_at"].(string); ok {
			if fetched, err := time.Parse(time.RFC3339, stamp); err == nil && fetched.Unix() > result.FetchedAt {
				return errors.New("newer OpenCode quota snapshot already persisted")
			}
		}
		if result.Success && result.CredentialValid {
			updates := map[string]any{"opencode_go_usage_updated_at": time.Unix(result.FetchedAt, 0).UTC().Format(time.RFC3339)}
			// Missing windows are unknown, never inherit an old snapshot as current.
			for _, window := range []string{"5h", "weekly", "monthly"} {
				updates["opencode_go_"+window+"_used_percent"] = nil
				updates["opencode_go_"+window+"_reset_at"] = nil
			}
			for _, tier := range result.Tiers {
				switch tier.Window {
				case "5h", "weekly", "monthly":
					updates["opencode_go_"+tier.Window+"_used_percent"] = tier.UsedPercent
					updates["opencode_go_"+tier.Window+"_reset_at"] = tier.ResetAt
				}
			}
			if err := repository.UpdateExtra(lockedCtx, expected.ID, updates); err != nil {
				return err
			}
		}
		service.ReconcileOpenCodeQuotaProbe(current, expected, result, time.Now())
		builder := repository.client.Account.UpdateOneID(expected.ID).
			SetStatus(current.Status).SetErrorMessage(current.ErrorMessage)
		if current.RateLimitResetAt == nil {
			builder.ClearRateLimitedAt().ClearRateLimitResetAt()
		}
		if current.TempUnschedulableUntil == nil {
			builder.ClearTempUnschedulableUntil().ClearTempUnschedulableReason()
		} else {
			builder.SetTempUnschedulableUntil(*current.TempUnschedulableUntil).SetTempUnschedulableReason(current.TempUnschedulableReason)
		}
		if _, err := builder.Save(lockedCtx); err != nil {
			return err
		}
		return enqueueSchedulerOutbox(lockedCtx, repository.sql, service.SchedulerOutboxEventAccountChanged, &expected.ID, nil, nil)
	})
}
