//go:build unit

package service

import (
	"context"
	"time"
)

func (repo *refreshAPIAccountRepo) UpdateAccountRuntimeCredentials(ctx context.Context, expected *Account, credentials map[string]any) error {
	if repo.account != nil && AccountRuntimeIdentityKey(repo.account) != AccountRuntimeIdentityKey(expected) {
		return ErrAccountRuntimeIdentityChanged
	}
	return repo.UpdateCredentials(ctx, expected.ID, credentials)
}

func (repo *tokenRefreshAccountRepo) UpdateAccountRuntimeCredentials(ctx context.Context, expected *Account, credentials map[string]any) error {
	if current := repo.accountsByID[expected.ID]; current != nil && AccountRuntimeIdentityKey(current) != AccountRuntimeIdentityKey(expected) {
		return ErrAccountRuntimeIdentityChanged
	}
	return repo.UpdateCredentials(ctx, expected.ID, credentials)
}

func (repo *tokenRefreshAccountRepo) UpdateAccountRuntimeSnapshot(ctx context.Context, expected *Account, updates map[string]any, _ *time.Time) error {
	current := repo.accountsByID[expected.ID]
	if current == nil || AccountRuntimeIdentityKey(current) != AccountRuntimeIdentityKey(expected) {
		return ErrAccountRuntimeIdentityChanged
	}
	return repo.UpdateExtra(ctx, expected.ID, updates)
}

func (repo *grokQuotaAccountRepo) UpdateAccountRuntimeSnapshot(ctx context.Context, expected *Account, updates map[string]any, _ *time.Time) error {
	if repo.mockAccountRepoForPlatform != nil {
		if current := repo.accountsByID[expected.ID]; current != nil && AccountRuntimeIdentityKey(current) != AccountRuntimeIdentityKey(expected) {
			return ErrAccountRuntimeIdentityChanged
		}
	}
	return repo.UpdateExtra(ctx, expected.ID, updates)
}

func (repo *isolatedAGRepo) UpdateAccountRuntimeCredentials(_ context.Context, expected *Account, credentials map[string]any) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if AccountRuntimeIdentityKey(repo.accounts[expected.ID]) != AccountRuntimeIdentityKey(expected) {
		return ErrAccountRuntimeIdentityChanged
	}
	repo.accounts[expected.ID].Credentials = shallowCopyMap(credentials)
	return nil
}
func (repo *openAIAccountRepoStub) UpdateAccountRuntimeCredentials(ctx context.Context, expected *Account, credentials map[string]any) error {
	updated := snapshotOAuthRefreshAccount(expected)
	updated.Credentials = shallowCopyMap(credentials)
	return repo.Update(ctx, updated)
}
func (repo *claudeAccountRepoStub) UpdateAccountRuntimeCredentials(ctx context.Context, expected *Account, credentials map[string]any) error {
	updated := snapshotOAuthRefreshAccount(expected)
	updated.Credentials = shallowCopyMap(credentials)
	return repo.Update(ctx, updated)
}
