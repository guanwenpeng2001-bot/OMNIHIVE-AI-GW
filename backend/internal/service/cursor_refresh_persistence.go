package service

import (
	"context"
	"errors"
	"time"
)

// CursorOAuthRefreshRepository compares the complete attempted credential
// document and publishes scheduler invalidation atomically with each mutation.
type CursorOAuthRefreshRepository interface {
	UpdateCursorOAuthCredentialsIfUnchanged(context.Context, int64, map[string]any, map[string]any) (bool, error)
	SetCursorOAuthRefreshFailureIfUnchanged(context.Context, int64, map[string]any, bool, time.Time) (bool, error)
}

// CursorAccountRecoveryRepository commits reauthorization and targeted refresh-state
// recovery in one transaction, including any explicit billing settings.
type CursorAccountRecoveryRepository interface {
	UpdateWithCursorRefreshRecovery(context.Context, *Account, *bool, *bool, *float64) error
}

func (api *OAuthRefreshAPI) persistCursorRefresh(ctx context.Context, attempted *Account, credentials map[string]any) (*OAuthRefreshResult, error) {
	repo, ok := api.accountRepo.(CursorOAuthRefreshRepository)
	if !ok {
		return nil, &providerConfigurationRefreshError{err: errors.New("Cursor refresh CAS repository is not configured")}
	}
	applied, err := repo.UpdateCursorOAuthCredentialsIfUnchanged(ctx, attempted.ID, attempted.Credentials, credentials)
	if err != nil {
		// Do not retry a possibly consumed refresh token, or expose DB arguments.
		return nil, &providerCycleContainmentRefreshError{err: errors.New("Cursor refresh credential persistence failed")}
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultRefreshPostPersistCleanupTimeout)
	defer cancel()
	current, err := api.accountRepo.GetByID(cleanupCtx, attempted.ID)
	if err != nil || current == nil {
		return nil, &providerCycleContainmentRefreshError{err: errors.New("Cursor refresh durable account is unavailable")}
	}
	result := &OAuthRefreshResult{Account: current, Refreshed: applied}
	if applied {
		result.NewCredentials = credentials
	}
	return result, nil
}

func (api *OAuthRefreshAPI) recordCursorRefreshFailure(ctx context.Context, attempted *Account, refreshErr error) (*OAuthRefreshResult, error) {
	repo, ok := api.accountRepo.(CursorOAuthRefreshRepository)
	if !ok {
		return nil, &providerConfigurationRefreshError{err: errors.New("Cursor refresh CAS repository is not configured")}
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	applied, err := repo.SetCursorOAuthRefreshFailureIfUnchanged(cleanupCtx, attempted.ID, attempted.Credentials,
		isNonRetryableRefreshError(refreshErr), time.Now().Add(tokenRefreshTempUnschedDuration))
	if err != nil {
		return nil, &providerCycleContainmentRefreshError{err: errors.New("Cursor refresh failure persistence failed")}
	}
	if !applied {
		current, err := api.accountRepo.GetByID(cleanupCtx, attempted.ID)
		if err != nil || current == nil {
			return nil, &providerCycleContainmentRefreshError{err: errors.New("Cursor refresh durable account is unavailable")}
		}
		return &OAuthRefreshResult{Account: current}, nil
	}
	return &OAuthRefreshResult{Account: attempted}, refreshErr
}
