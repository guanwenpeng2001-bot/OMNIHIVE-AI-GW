package repository

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestChannelCachePubSub(t *testing.T) {
	redisServer := miniredis.RunT(t)
	publisherRedis := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	subscriberRedis := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() {
		_ = publisherRedis.Close()
		_ = subscriberRedis.Close()
	})

	publisher := NewChannelCache(publisherRedis)
	subscriber := NewChannelCache(subscriberRedis)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	received := make(chan struct{}, 1)
	subscriber.SubscribeUpdates(ctx, func() {
		select {
		case received <- struct{}{}:
		default:
		}
	})

	require.Eventually(t, func() bool {
		return redisServer.PubSubNumSub(channelCachePubSubKey)[channelCachePubSubKey] == 1
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, publisher.NotifyUpdate(context.Background()))

	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("channel cache invalidation notification was not received")
	}
}

func TestCacheSubscribersReconnectAndInvalidate(t *testing.T) {
	for _, topic := range []string{channelCachePubSubKey, errorPassthroughPubSubKey} {
		t.Run(topic, func(t *testing.T) {
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			defer func() { _ = client.Close() }()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var received atomic.Int64
			subscribeCacheUpdates(ctx, client, topic, func() { received.Add(1) })
			require.Eventually(t, func() bool { return received.Load() > 0 }, time.Second, 10*time.Millisecond)
			before := received.Load()
			server.Close()
			require.NoError(t, server.Restart())
			require.Eventually(t, func() bool { return received.Load() > before }, 5*time.Second, 10*time.Millisecond,
				"reconnection must invalidate even with no replayable message")
			before = received.Load()
			require.NoError(t, client.Publish(ctx, topic, "refresh").Err())
			require.Eventually(t, func() bool { return received.Load() > before }, time.Second, 10*time.Millisecond)
			cancel()
			require.Eventually(t, func() bool { return server.PubSubNumSub(topic)[topic] == 0 }, time.Second, 10*time.Millisecond,
				"quiet subscription must close when its owner stops")

		})
	}
}
