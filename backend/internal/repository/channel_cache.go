package repository

import (
	"context"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const channelCachePubSubKey = "channel_cache_updated"

type channelCache struct {
	rdb *redis.Client
}

// NewChannelCache creates the Redis-backed channel cache invalidation bus.
func NewChannelCache(rdb *redis.Client) *channelCache {
	return &channelCache{rdb: rdb}
}

// NotifyUpdate notifies all instances to invalidate their local channel cache.
func (c *channelCache) NotifyUpdate(ctx context.Context) error {
	return c.rdb.Publish(ctx, channelCachePubSubKey, "refresh").Err()
}

// SubscribeUpdates subscribes to channel cache invalidation notifications.

func (c *channelCache) SubscribeUpdates(ctx context.Context, handler func()) {
	subscribeCacheUpdates(ctx, c.rdb, channelCachePubSubKey, handler)
}

// Each subscription owns its connection. Re-establishing a subscription always
// invalidates: messages lost during a disconnect cannot be replayed by Pub/Sub.
func subscribeCacheUpdates(ctx context.Context, client *redis.Client, topic string, handler func()) {
	go func() {
		delay := 100 * time.Millisecond
		for ctx.Err() == nil {
			subscription := client.Subscribe(ctx, topic)
			finished := make(chan struct{})
			go func() {
				select {
				case <-ctx.Done():
					_ = subscription.Close()
				case <-finished:
				}
			}()
			_, err := subscription.Receive(ctx)
			if err == nil {
				handler()
				delay = 100 * time.Millisecond
				for ctx.Err() == nil {
					message, receiveErr := subscription.ReceiveMessage(ctx)
					if receiveErr != nil {
						err = receiveErr
						break
					}
					if message != nil {
						handler()
					}
				}
			}
			_ = subscription.Close()
			close(finished)
			if ctx.Err() != nil {
				return
			}
			slog.Warn("cache subscription reconnecting", "topic", topic, "error", err)
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			delay *= 2
			if delay > 5*time.Second {
				delay = 5 * time.Second
			}
		}
	}()
}
