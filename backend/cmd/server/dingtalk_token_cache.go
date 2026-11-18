package main

import (
	"context"
	"errors"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler"
	"github.com/redis/go-redis/v9"
)

type dingTalkTokenCache struct {
	client *redis.Client
}

func provideDingTalkTokenCache(client *redis.Client) handler.DingTalkTokenCache {
	if client == nil {
		return nil
	}
	return &dingTalkTokenCache{client: client}
}

func (c *dingTalkTokenCache) Get(ctx context.Context, key string) (string, error) {
	token, err := c.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return token, err
}

func (c *dingTalkTokenCache) AcquireRefresh(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	return c.client.SetNX(ctx, key, owner, ttl).Result()
}

func (c *dingTalkTokenCache) ReleaseRefresh(ctx context.Context, key, owner string) error {
	return c.client.Eval(ctx, `if redis.call('GET', KEYS[1]) == ARGV[1] then return redis.call('DEL', KEYS[1]) end return 0`, []string{key}, owner).Err()
}

func (c *dingTalkTokenCache) Publish(ctx context.Context, lockKey, key, owner, token string, ttl time.Duration) (bool, error) {
	published, err := c.client.Eval(ctx, `if redis.call('GET', KEYS[1]) == ARGV[1] then redis.call('SET', KEYS[2], ARGV[2], 'PX', ARGV[3]); return 1 end return 0`, []string{lockKey, key}, owner, token, ttl.Milliseconds()).Int64()
	return published != 0, err
}
