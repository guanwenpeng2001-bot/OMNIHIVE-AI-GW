package repository

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

const leaderLockKeyPrefix = "leader:lock:"

type leaderLockCache struct {
	rdb *redis.Client
}

// NewLeaderLockCache returns a Redis-backed implementation of
// service.LeaderLockCache used by periodic background jobs to elect a single
// runner across instances.
func NewLeaderLockCache(rdb *redis.Client) *leaderLockCache {
	return &leaderLockCache{rdb: rdb}
}

func (c *leaderLockCache) TryAcquireLeaderLock(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	return c.rdb.SetNX(ctx, leaderLockKeyPrefix+key, owner, ttl).Result()
}

func (c *leaderLockCache) ReleaseLeaderLock(ctx context.Context, key, owner string) error {
	return compareAndDelete(ctx, c.rdb, leaderLockKeyPrefix+key, owner)
}
