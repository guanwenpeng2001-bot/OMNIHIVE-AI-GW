package service

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// GeminiTokenCache stores short-lived access tokens and coordinates refresh to avoid stampedes.
type GeminiTokenCache interface {
	// cacheKey should be stable for the token scope; for GeminiCli OAuth we primarily use project_id.
	GetAccessToken(ctx context.Context, cacheKey string) (string, error)
	SetAccessToken(ctx context.Context, cacheKey string, token string, ttl time.Duration) error
	DeleteAccessToken(ctx context.Context, cacheKey string) error

	// owner must be unique for each acquisition attempt; release must use the same owner.
	AcquireRefreshLock(ctx context.Context, cacheKey string, owner string, ttl time.Duration) (bool, error)
	ReleaseRefreshLock(ctx context.Context, cacheKey string, owner string) error
}

func newRefreshLockOwner() string {
	return uuid.NewString()
}
