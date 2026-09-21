//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// accountAttemptCacheStub 实现并发缓存接口 + 账号 attempt 计数窄口，
// 与生产 repository 同时实现两个能力的关系一致。
type accountAttemptCacheStub struct {
	ConcurrencyCache
	acquired bool
	err      error
	attempts []int64
	failWith error
}

func (c *accountAttemptCacheStub) AcquireAccountSlot(context.Context, int64, int, string) (bool, error) {
	return c.acquired, c.err
}

func (c *accountAttemptCacheStub) ReleaseAccountSlot(context.Context, int64, string) error {
	return nil
}

func (c *accountAttemptCacheStub) CountAccountAttempt(_ context.Context, accountID int64) error {
	if c.failWith != nil {
		return c.failWith
	}
	c.attempts = append(c.attempts, accountID)
	return nil
}

// concurrentAttemptCacheStub 只实现并发缓存，不提供 attempt 计数窄口。
type concurrentAttemptCacheStub struct {
	ConcurrencyCache
}

func (c *concurrentAttemptCacheStub) AcquireAccountSlot(context.Context, int64, int, string) (bool, error) {
	return true, nil
}

func TestConcurrencyService_AcquireAccountSlotCountsEveryRealAttempt(t *testing.T) {
	cache := &accountAttemptCacheStub{acquired: true}
	svc := NewConcurrencyService(cache)

	for _, accountID := range []int64{11, 11, 12} {
		result, err := svc.AcquireAccountSlot(context.Background(), accountID, 4)
		require.NoError(t, err)
		require.True(t, result.Acquired)
		result.ReleaseFunc()
	}
	require.Equal(t, []int64{11, 11, 12}, cache.attempts,
		"账号维度按实际 attempt 计数：重试与账号切换各记一次")
}

func TestConcurrencyService_AcquireAccountSlotCountsUnboundedAccountsToo(t *testing.T) {
	cache := &accountAttemptCacheStub{acquired: true}
	svc := NewConcurrencyService(cache)

	result, err := svc.AcquireAccountSlot(context.Background(), 21, 0)
	require.NoError(t, err)
	require.True(t, result.Acquired)
	require.Equal(t, []int64{21}, cache.attempts,
		"没有并发上限的账号同样是一次真实尝试，RPM 计数不能漏")
}

func TestConcurrencyService_AcquireAccountSlotDoesNotCountDenials(t *testing.T) {
	cache := &accountAttemptCacheStub{acquired: false}
	svc := NewConcurrencyService(cache)

	result, err := svc.AcquireAccountSlot(context.Background(), 31, 4)
	require.NoError(t, err)
	require.False(t, result.Acquired)
	require.Empty(t, cache.attempts, "槽位没拿到就没有发生尝试")
}

func TestConcurrencyService_AcquireAccountSlotDoesNotCountErrors(t *testing.T) {
	cache := &accountAttemptCacheStub{acquired: true, err: errors.New("redis down")}
	svc := NewConcurrencyService(cache)

	_, err := svc.AcquireAccountSlot(context.Background(), 41, 4)
	require.Error(t, err)
	require.Empty(t, cache.attempts)
}

func TestConcurrencyService_AcquireAccountSlotToleratesCounterFailure(t *testing.T) {
	cache := &accountAttemptCacheStub{acquired: true, failWith: errors.New("redis down")}
	svc := NewConcurrencyService(cache)

	result, err := svc.AcquireAccountSlot(context.Background(), 51, 4)
	require.NoError(t, err, "RPM 计数是软限：计数失败不得阻断请求")
	require.True(t, result.Acquired)
}

func TestConcurrencyService_AcquireAccountSlotWithoutCounterPort(t *testing.T) {
	svc := NewConcurrencyService(&concurrentAttemptCacheStub{})
	result, err := svc.AcquireAccountSlot(context.Background(), 61, 4)
	require.NoError(t, err)
	require.True(t, result.Acquired, "缺少计数窄口时并发语义完全不变")
}
