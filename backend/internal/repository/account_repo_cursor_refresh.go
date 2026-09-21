package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.CursorOAuthRefreshRepository = (*accountRepository)(nil)
var _ service.CursorAccountRecoveryRepository = (*accountRepository)(nil)

func (r *accountRepository) UpdateCursorOAuthCredentialsIfUnchanged(ctx context.Context, id int64, expected, credentials map[string]any) (bool, error) {
	if _, referenced := service.CredentialReference(expected); referenced {
		return r.updateReferencedOAuthCredentials(ctx, id, service.PlatformCursor, expected, nil, false, credentials)
	}
	replacement, err := json.Marshal(normalizeJSONMap(credentials))
	if err != nil {
		return false, err
	}
	return r.mutateCursorOAuthIfUnchanged(ctx, id, expected, "credentials = $5::jsonb", string(replacement))
}

func (r *accountRepository) SetCursorOAuthRefreshFailureIfUnchanged(ctx context.Context, id int64, expected map[string]any, permanent bool, until time.Time) (bool, error) {
	if _, referenced := service.CredentialReference(expected); referenced {
		if permanent {
			return r.mutateReferencedOAuthIfUnchanged(ctx, id, service.PlatformCursor, expected, nil, false,
				func(lockedCtx context.Context, repository *accountRepository, _ *service.Account) (bool, error) {
					return repository.markOAuthErrorLocked(lockedCtx, id, "Cursor reauth required: token refresh rejected", false)
				})
		}
		return r.mutateReferencedOAuthIfUnchanged(ctx, id, service.PlatformCursor, expected, nil, false,
			func(lockedCtx context.Context, repository *accountRepository, _ *service.Account) (bool, error) {
				return repository.markOAuthTempUnschedulableLocked(lockedCtx, id, until, "Cursor token refresh temporarily unavailable", false)
			})
	}
	if permanent {
		return r.mutateCursorOAuthIfUnchanged(ctx, id, expected,
			"status = 'error', error_message = $5, schedulable = false", "Cursor reauth required: token refresh rejected")
	}
	return r.mutateCursorOAuthIfUnchanged(ctx, id, expected,
		"temp_unschedulable_until = $5, temp_unschedulable_reason = 'Cursor token refresh temporarily unavailable'", until)
}

// assignments is supplied only by the fixed statements above. The JSONB CAS and
// outbox insert share one statement so scheduler publication cannot partially fail.
func (r *accountRepository) mutateCursorOAuthIfUnchanged(ctx context.Context, id int64, expected map[string]any, assignments string, value any) (bool, error) {
	if r == nil || r.sql == nil {
		return false, errors.New("account repository SQL executor is not configured")
	}
	expectedJSON, err := json.Marshal(normalizeJSONMap(expected))
	if err != nil {
		return false, err
	}
	result, err := r.sql.ExecContext(ctx, `
		WITH updated AS (
			UPDATE accounts SET `+assignments+`, updated_at = NOW()
			WHERE id = $1 AND deleted_at IS NULL
				AND platform = $2 AND type = $3 AND credentials = $4::jsonb
			RETURNING id
		)
		INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
		SELECT $6, updated.id, NULL, NULL FROM updated
	`, id, service.PlatformCursor, service.AccountTypeOAuth, string(expectedJSON), value, service.SchedulerOutboxEventAccountChanged)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows == 0 {
		return false, err
	}
	r.syncSchedulerAccountSnapshotDetached(ctx, id)
	return true, nil
}
