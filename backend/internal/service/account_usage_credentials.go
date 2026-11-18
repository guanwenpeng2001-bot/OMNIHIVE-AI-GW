package service

import (
	"context"
	"fmt"
)

// Recovery is exceptional: one strict batch read, then at most one sequential
// read per unique account under the existing overall usage deadline. Larger
// batches retain the original failure instead of amplifying it into unbounded SQL.
const usageCredentialFallbackMaxAccounts = 1000

// Load all accounts before starting any usage probes. Infrastructure failures
// during recovery abort the request rather than producing misleading partial
// results. Missing accounts (including concurrent deletion) remain per-ID errors.
func (s *AccountUsageService) loadUsageBatchAccounts(ctx context.Context, ids []int64) ([]*Account, map[int64]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	accounts, err := s.accountRepo.GetByIDs(ctx, ids)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, nil, ctxErr
	}
	failures := make(map[int64]string)
	if err == nil {
		return accounts, failures, nil
	}
	referenceErr := accountCredentialReferenceError(err)
	if referenceErr == nil {
		return nil, nil, err
	}
	if len(ids) > usageCredentialFallbackMaxAccounts {
		return nil, nil, fmt.Errorf("credential recovery requires at most %d accounts: %w", usageCredentialFallbackMaxAccounts, referenceErr)
	}
	accounts = make([]*Account, 0, len(ids))
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		account, err := s.accountRepo.GetByID(ctx, id)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, nil, ctxErr
		}
		if err != nil {
			if referenceErr := accountCredentialReferenceError(err); referenceErr != nil {
				failures[id] = referenceErr.Error()
				continue
			}
			if accountReadMissing(err) {
				failures[id] = ErrAccountNotFound.Error()
				continue
			}
			return nil, nil, err
		}
		if account == nil {
			failures[id] = ErrAccountNotFound.Error()
			continue
		}
		accounts = append(accounts, account)
	}
	return accounts, failures, nil
}
