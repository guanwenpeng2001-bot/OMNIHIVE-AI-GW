package repository

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type cursorSessionCache struct{ client *redis.Client }

func NewCursorSessionCache(client *redis.Client) service.CursorSessionCache {
	return &cursorSessionCache{client: client}
}

func (cache *cursorSessionCache) Delete(ctx context.Context, key string) error {
	return cache.client.Del(ctx, key).Err()
}

func (cache *cursorSessionCache) Get(ctx context.Context, key string) (cursor.Session, error) {
	data, err := cache.client.Get(ctx, key).Bytes()
	if err != nil {
		return cursor.Session{}, err
	}
	var session cursor.Session
	err = json.Unmarshal(data, &session)
	return session, err
}

func (cache *cursorSessionCache) Set(ctx context.Context, key string, session cursor.Session) error {
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	ttl := time.Until(session.ExpiresAt)
	if ttl <= 0 {
		return nil
	}
	return cache.client.Set(ctx, key, data, ttl).Err()
}

func (cache *cursorSessionCache) Lock(ctx context.Context, key string) (func(), bool, error) {
	lockKey := key + ":refresh"
	owner := uuid.NewString()
	acquired, err := cache.client.SetNX(ctx, lockKey, owner, 30*time.Second).Result()
	release := func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := cache.client.Eval(cleanupCtx, `if redis.call('GET', KEYS[1]) == ARGV[1] then return redis.call('DEL', KEYS[1]) end return 0`, []string{lockKey}, owner).Err(); err != nil {
			slog.Warn("cursor_refresh_lock_release_failed", "error", err)
		}
	}
	return release, acquired, err
}
