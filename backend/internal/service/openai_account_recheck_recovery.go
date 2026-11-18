package service

import (
	"context"
	"fmt"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
)

const (
	openAIAccountRecoveryMaxAccounts = 128
	openAIAccountRecoveryTimeout     = 2 * time.Second
)

// Filter before scoring or acquiring capacity, including advanced weighted
// sticky candidates. Returned rows are the authoritative read, not cache copies.
func openAIRecheckedCandidates(candidates []*Account, batch map[int64]*Account) []*Account {
	current := make([]*Account, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate != nil {
			if account := batch[candidate.ID]; account != nil {
				current = append(current, account)
			}
		}
	}
	return current
}

// Keep error matching without exposing driver/wrapper text in selection logs.
type accountRecheckReadError struct{ cause error }

func (e *accountRecheckReadError) Error() string { return "load account recheck batch failed" }
func (e *accountRecheckReadError) Unwrap() error { return e.cause }

// General GetByIDs remains strict. Selection alone may discard account-local
// credential failures, before acquiring slots or publishing any sticky binding.
func (s *OpenAIGatewayService) recoverOpenAIAccountRecheckBatch(ctx context.Context, ids []int64, batchErr error) (map[int64]*Account, error) {
	referenceErr := accountCredentialReferenceError(batchErr)
	if referenceErr == nil {
		return nil, &accountRecheckReadError{cause: batchErr}
	}
	if len(ids) > openAIAccountRecoveryMaxAccounts {
		return nil, &accountRecheckReadError{cause: fmt.Errorf("credential recovery exceeds %d accounts: %w", openAIAccountRecoveryMaxAccounts, referenceErr)}
	}
	// Inherit cancellation, workspace authorization and request-local CAS state.
	ctx, cancel := context.WithTimeout(ctx, openAIAccountRecoveryTimeout)
	defer cancel()
	batch := make(map[int64]*Account, len(ids))
	workspaceID, scoped := domain.WorkspaceScope(ctx)
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		batch[id] = nil // rejected IDs must never fall back to a stale snapshot
		account, err := s.accountRepo.GetByID(ctx, id)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if err != nil {
			if accountCredentialReferenceError(err) != nil || accountReadMissing(err) {
				continue
			}
			// Do not publish an earlier partial success if a later read fails.
			return nil, &accountRecheckReadError{cause: err}
		}
		if account == nil {
			continue
		}
		if account.ID != id || (scoped && account.WorkspaceID != workspaceID) {
			// A broken repository boundary is not proof that a row is absent.
			return nil, &accountRecheckReadError{cause: ErrAccountNotFound}
		}
		batch[id] = account
	}
	return batch, nil
}
