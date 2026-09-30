package repository

import (
	"context"
	"errors"
	"strings"
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
			if fetched, err := time.Parse(time.RFC3339, stamp); err == nil && (fetched.Unix() > result.FetchedAt || (fetched.Unix() == result.FetchedAt && (stamp != expected.Extra["opencode_go_usage_updated_at"] || current.UpdatedAt.After(expected.UpdatedAt)))) {
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

// SetOpenCodeModelQuotaLimit uses the existing model cooldown storage under the
// runtime lock, so old-key responses and stale same-model snapshots cannot win.
func (r *accountRepository) SetOpenCodeModelQuotaLimit(ctx context.Context, expected *service.Account, model string, until time.Time, reason string) error {
	return r.withRuntimeIdentity(ctx, expected.ID, service.AccountRuntimeIdentityKey(expected), func(lockedCtx context.Context, repository *accountRepository) error {
		entity, err := repository.client.Account.Get(lockedCtx, expected.ID)
		if err != nil {
			return err
		}
		limits, _ := entity.Extra["model_rate_limits"].(map[string]any)
		limit, _ := limits[model].(map[string]any)
		stamp, _ := limit["rate_limit_reset_at"].(string)
		if previous, err := time.Parse(time.RFC3339, stamp); err == nil && previous.After(until) {
			until = previous
			if previousReason, ok := limit["reason"].(string); ok {
				reason = previousReason
			}
		}
		return repository.SetModelRateLimit(lockedCtx, expected.ID, model, until, reason)
	})
}

func openCodeQuotaOwnedPause(account *service.Account) bool {
	return account.IsOpenCodeGoPlan() && (strings.HasPrefix(account.TempUnschedulableReason, "opencode_payment_required") || strings.HasPrefix(account.TempUnschedulableReason, "cn_quota_exhausted"))
}

func openCodeQuotaOwnedRateLimit(account *service.Account) bool {
	if !account.IsOpenCodeGoPlan() || account.RateLimitResetAt == nil {
		return false
	}
	for _, window := range []string{"5h", "weekly", "monthly"} {
		stamp, _ := account.Extra["opencode_go_"+window+"_reset_at"].(string)
		if reset, err := time.Parse(time.RFC3339Nano, stamp); err == nil && reset.Equal(*account.RateLimitResetAt) {
			return true
		}
	}
	return false
}

// Bulk identity updates read the pre-update columns for ownership. PostgreSQL
// evaluates every SET expression against that same old row.
func openCodeQuotaIdentityChangeSQL(incomingCredentials, incomingExtra, proxyChanged string) string {
	changes := []string{"credentials IS DISTINCT FROM (" + incomingCredentials + ")"}
	for _, field := range []string{"secret_revision", "secret_reference", "credential_secret_id", "credential_secret_revision", "header_overrides", "base_url", "protocol"} {
		changes = append(changes, "COALESCE(extra->'"+field+"','null'::jsonb) IS DISTINCT FROM COALESCE(("+incomingExtra+")->'"+field+"','null'::jsonb)")
	}
	if proxyChanged != "" {
		changes = append(changes, proxyChanged)
	}
	return "(platform='opencode_go' AND BTRIM(COALESCE(credentials->>'account_mode','go')) <> 'zen' AND (" + strings.Join(changes, " OR ") + "))"
}

func openCodeQuotaInvalidationClausesSQL(identityChanged string) []string {
	pause := "(" + identityChanged + " AND (temp_unschedulable_reason LIKE 'opencode_payment_required%' OR temp_unschedulable_reason LIKE 'cn_quota_exhausted%'))"
	matches := []string{}
	for _, window := range []string{"5h", "weekly", "monthly"} {
		// Quota APIs emit both seconds and fractional UTC RFC3339 timestamps.
		// Normalize trailing fractional zeros without casting arbitrary metadata.
		stamp := "extra->>'opencode_go_" + window + "_reset_at'"
		matches = append(matches, `replace(regexp_replace(replace(`+stamp+`, '+00:00', 'Z'), '(\.[0-9]*?)0+Z$', '\1Z'), '.Z', 'Z') = rtrim(rtrim(to_char(rate_limit_reset_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US'), '0'), '.') || 'Z'`)
	}
	rate := "(" + identityChanged + " AND (" + strings.Join(matches, " OR ") + "))"
	return []string{
		"temp_unschedulable_until = CASE WHEN " + pause + " THEN NULL ELSE temp_unschedulable_until END",
		"temp_unschedulable_reason = CASE WHEN " + pause + " THEN NULL ELSE temp_unschedulable_reason END",
		"rate_limited_at = CASE WHEN " + rate + " THEN NULL ELSE rate_limited_at END",
		"rate_limit_reset_at = CASE WHEN " + rate + " THEN NULL ELSE rate_limit_reset_at END",
	}
}

func clearOpenCodeModelQuotaLimitsSQL(extraExpression, identityChanged string) string {
	return "CASE WHEN " + identityChanged + " AND jsonb_typeof((" + extraExpression + ")->'model_rate_limits')='object' THEN jsonb_set((" + extraExpression + "), '{model_rate_limits}', COALESCE((SELECT jsonb_object_agg(key,value) FROM jsonb_each((" + extraExpression + ")->'model_rate_limits') WHERE COALESCE(value->>'reason','') NOT LIKE 'opencode_payment_required%' AND COALESCE(value->>'reason','') NOT LIKE 'opencode_usage_limit%'), '{}'::jsonb)) ELSE (" + extraExpression + ") END"
}
