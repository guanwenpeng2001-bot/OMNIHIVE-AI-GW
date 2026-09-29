//go:build unit

package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// userRPMCacheStub 记录每种计数器被调用的次数，并可注入返回值与错误。
type userRPMCacheStub struct {
	userGroupCalls int32
	userCalls      int32

	userGroupCounts []int // 依次返回的计数值
	userGroupErr    error
	userCounts      []int
	userErr         error

	windowResetAt   time.Time
	windowResetErr  error
	windowResetCall int32
}

func (s *userRPMCacheStub) IncrementUserGroupRPM(_ context.Context, _, _ int64) (int, error) {
	idx := int(atomic.AddInt32(&s.userGroupCalls, 1)) - 1
	if s.userGroupErr != nil {
		return 0, s.userGroupErr
	}
	if idx < len(s.userGroupCounts) {
		return s.userGroupCounts[idx], nil
	}
	return 1, nil
}

func (s *userRPMCacheStub) IncrementUserRPM(_ context.Context, _ int64) (int, error) {
	idx := int(atomic.AddInt32(&s.userCalls, 1)) - 1
	if s.userErr != nil {
		return 0, s.userErr
	}
	if idx < len(s.userCounts) {
		return s.userCounts[idx], nil
	}
	return 1, nil
}

func (s *userRPMCacheStub) GetUserGroupRPM(_ context.Context, _, _ int64) (int, error) {
	return 0, nil
}

func (s *userRPMCacheStub) GetUserRPM(_ context.Context, _ int64) (int, error) {
	return 0, nil
}

func (s *userRPMCacheStub) WindowResetAt(_ context.Context) (time.Time, error) {
	atomic.AddInt32(&s.windowResetCall, 1)
	if s.windowResetErr != nil {
		return time.Time{}, s.windowResetErr
	}
	return s.windowResetAt, nil
}

// rpmOverrideRepoStub 专用于 checkRPM 分支测试，只实现必要方法。
type rpmOverrideRepoStub struct {
	UserGroupRateRepository

	override *int
	err      error
	calls    int32
}

func (s *rpmOverrideRepoStub) GetRPMOverrideByUserAndGroup(_ context.Context, _, _ int64) (*int, error) {
	atomic.AddInt32(&s.calls, 1)
	if s.err != nil {
		return nil, s.err
	}
	return s.override, nil
}

func newBillingServiceForRPM(t *testing.T, cache UserRPMCache, rateRepo UserGroupRateRepository) *BillingCacheService {
	t.Helper()
	// 用 nil BillingCache 走 "无缓存" 分支，避免 CheckBillingEligibility 副作用。
	// 我们只直接测 checkRPM。
	svc := NewBillingCacheService(nil, nil, nil, nil, cache, rateRepo, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	return svc
}

func TestBillingCacheService_CheckRPM_OverrideTakesPrecedenceOverGroup(t *testing.T) {
	override := 2
	// user-group 计数: 1, 2, 3；user 计数: 默认返回 1（远小于 RPMLimit=100，不干扰）
	cache := &userRPMCacheStub{userGroupCounts: []int{1, 2, 3}}
	repo := &rpmOverrideRepoStub{override: &override}
	svc := newBillingServiceForRPM(t, cache, repo)

	user := &User{ID: 1, RPMLimit: 100} // 全局上限设高，不干扰 override 测试
	group := &Group{ID: 10, RPMLimit: 100}

	require.NoError(t, svc.checkRPM(context.Background(), user, group))
	require.NoError(t, svc.checkRPM(context.Background(), user, group))
	require.ErrorIs(t, svc.checkRPM(context.Background(), user, group), ErrGroupRPMExceeded)

	require.EqualValues(t, 3, atomic.LoadInt32(&cache.userGroupCalls), "override 命中分支应走 user-group 计数")
	// 并行设计：前 2 次 override 未超→继续检查 user；第 3 次 override 超了→直接 return，不检查 user
	require.EqualValues(t, 2, atomic.LoadInt32(&cache.userCalls), "override 超限前 user 计数器应被调用")
	require.EqualValues(t, 3, atomic.LoadInt32(&repo.calls))
}

func TestBillingCacheService_CheckRPM_UserLimitIsGlobalHardCap(t *testing.T) {
	override := 100 // override 很高
	// user-group 计数: 默认返回 1（远小于 override）；user 计数: 1, 2, 3
	cache := &userRPMCacheStub{userCounts: []int{1, 2, 3}}
	repo := &rpmOverrideRepoStub{override: &override}
	svc := newBillingServiceForRPM(t, cache, repo)

	user := &User{ID: 1, RPMLimit: 2} // 全局硬上限=2，应覆盖 override=100
	group := &Group{ID: 10, RPMLimit: 100}

	require.NoError(t, svc.checkRPM(context.Background(), user, group))
	require.NoError(t, svc.checkRPM(context.Background(), user, group))
	require.ErrorIs(t, svc.checkRPM(context.Background(), user, group), ErrUserRPMExceeded, "user 全局硬上限应优先于 override")
}

func TestBillingCacheService_CheckRPM_OverrideZeroSkipsGroupButUserStillApplies(t *testing.T) {
	zero := 0
	// user 计数: 依次返回 1..6
	cache := &userRPMCacheStub{userCounts: []int{1, 2, 3, 4, 5, 6}}
	repo := &rpmOverrideRepoStub{override: &zero}
	svc := newBillingServiceForRPM(t, cache, repo)

	user := &User{ID: 1, RPMLimit: 5}
	group := &Group{ID: 10, RPMLimit: 100}

	// override=0 跳过分组计数，但 user.RPMLimit=5 仍生效
	for i := 0; i < 5; i++ {
		require.NoError(t, svc.checkRPM(context.Background(), user, group), "request %d should pass", i+1)
	}
	require.ErrorIs(t, svc.checkRPM(context.Background(), user, group), ErrUserRPMExceeded,
		"override=0 跳过分组但 user 全局上限仍应生效")
	require.EqualValues(t, 0, atomic.LoadInt32(&cache.userGroupCalls), "override=0 不应触发分组计数器")
	require.EqualValues(t, 6, atomic.LoadInt32(&cache.userCalls), "user 计数器应被调用")
}

func TestBillingCacheService_CheckRPM_OverrideZeroAndUserZeroIsFullyUnlimited(t *testing.T) {
	zero := 0
	cache := &userRPMCacheStub{}
	repo := &rpmOverrideRepoStub{override: &zero}
	svc := newBillingServiceForRPM(t, cache, repo)

	user := &User{ID: 1, RPMLimit: 0} // user 也不限
	group := &Group{ID: 10, RPMLimit: 100}

	for i := 0; i < 50; i++ {
		require.NoError(t, svc.checkRPM(context.Background(), user, group))
	}
	require.EqualValues(t, 0, atomic.LoadInt32(&cache.userGroupCalls), "override=0 不触发分组计数")
	require.EqualValues(t, 0, atomic.LoadInt32(&cache.userCalls), "user.RPMLimit=0 也不触发用户计数")
}

func TestBillingCacheService_CheckRPM_NilOverrideFallsThroughToGroup(t *testing.T) {
	// user-group 计数: 5, 6；user 计数: 默认 1（不干扰）
	cache := &userRPMCacheStub{userGroupCounts: []int{5, 6}}
	repo := &rpmOverrideRepoStub{override: nil}
	svc := newBillingServiceForRPM(t, cache, repo)

	user := &User{ID: 1, RPMLimit: 999} // 全局上限很高，group 先超
	group := &Group{ID: 10, RPMLimit: 5}

	require.NoError(t, svc.checkRPM(context.Background(), user, group))                      // ug=5, user=1, 都没超
	require.ErrorIs(t, svc.checkRPM(context.Background(), user, group), ErrGroupRPMExceeded) // ug=6 > 5

	require.EqualValues(t, 2, atomic.LoadInt32(&cache.userGroupCalls))
	// 并行模式：第 1 次 group 没超 → 继续检查 user；第 2 次 group 超了 → 直接 return，不检查 user
	require.EqualValues(t, 1, atomic.LoadInt32(&cache.userCalls), "group 未超时 user 也应检查；group 超时直接返回")
}

func TestBillingCacheService_CheckRPM_OverrideLookupErrorFallsThroughToGroup(t *testing.T) {
	cache := &userRPMCacheStub{userGroupCounts: []int{3}}
	repo := &rpmOverrideRepoStub{err: errors.New("db down")}
	svc := newBillingServiceForRPM(t, cache, repo)

	user := &User{ID: 1, RPMLimit: 0}
	group := &Group{ID: 10, RPMLimit: 10}

	// override 查询失败后应继续尝试 group 分支（不直接拒绝）
	require.NoError(t, svc.checkRPM(context.Background(), user, group))
	require.EqualValues(t, 1, atomic.LoadInt32(&cache.userGroupCalls))
	require.EqualValues(t, 1, atomic.LoadInt32(&repo.calls))
}

func TestBillingCacheService_CheckRPM_UserLevelFallbackWhenGroupUnlimited(t *testing.T) {
	cache := &userRPMCacheStub{userCounts: []int{1, 2, 3}}
	repo := &rpmOverrideRepoStub{override: nil}
	svc := newBillingServiceForRPM(t, cache, repo)

	user := &User{ID: 1, RPMLimit: 2}
	group := &Group{ID: 10, RPMLimit: 0} // 分组未设限

	require.NoError(t, svc.checkRPM(context.Background(), user, group))
	require.NoError(t, svc.checkRPM(context.Background(), user, group))
	require.ErrorIs(t, svc.checkRPM(context.Background(), user, group), ErrUserRPMExceeded)

	require.EqualValues(t, 0, atomic.LoadInt32(&cache.userGroupCalls), "group 未设限时不应 INCR user-group 键")
	require.EqualValues(t, 3, atomic.LoadInt32(&cache.userCalls))
}

func TestBillingCacheService_CheckRPM_NoLimitsConfiguredIsNoop(t *testing.T) {
	cache := &userRPMCacheStub{}
	repo := &rpmOverrideRepoStub{override: nil}
	svc := newBillingServiceForRPM(t, cache, repo)

	user := &User{ID: 1, RPMLimit: 0}
	group := &Group{ID: 10, RPMLimit: 0}

	for i := 0; i < 10; i++ {
		require.NoError(t, svc.checkRPM(context.Background(), user, group))
	}
	require.EqualValues(t, 0, atomic.LoadInt32(&cache.userGroupCalls))
	require.EqualValues(t, 0, atomic.LoadInt32(&cache.userCalls))
}

func TestBillingCacheService_CheckRPM_RedisErrorFailOpen(t *testing.T) {
	cache := &userRPMCacheStub{userGroupErr: errors.New("redis unavailable")}
	repo := &rpmOverrideRepoStub{override: nil}
	svc := newBillingServiceForRPM(t, cache, repo)

	user := &User{ID: 1, RPMLimit: 0}
	group := &Group{ID: 10, RPMLimit: 5}

	// Redis 故障时应 fail-open，不拒绝请求
	require.NoError(t, svc.checkRPM(context.Background(), user, group))
	require.EqualValues(t, 1, atomic.LoadInt32(&cache.userGroupCalls))
}

func TestBillingCacheService_CheckRPM_NoGroupUsesUserOnly(t *testing.T) {
	cache := &userRPMCacheStub{userCounts: []int{1, 2, 3}}
	repo := &rpmOverrideRepoStub{}
	svc := newBillingServiceForRPM(t, cache, repo)

	user := &User{ID: 1, RPMLimit: 2}

	// 无 group（纯用户级限流场景），不应查询 rpm_override。
	require.NoError(t, svc.checkRPM(context.Background(), user, nil))
	require.NoError(t, svc.checkRPM(context.Background(), user, nil))
	require.ErrorIs(t, svc.checkRPM(context.Background(), user, nil), ErrUserRPMExceeded)

	require.EqualValues(t, 0, atomic.LoadInt32(&repo.calls), "无 group 时不应查询 rpm_override")
	require.EqualValues(t, 3, atomic.LoadInt32(&cache.userCalls))
}

func TestBillingCacheService_CheckRPM_NilUserIsNoop(t *testing.T) {
	cache := &userRPMCacheStub{}
	repo := &rpmOverrideRepoStub{}
	svc := newBillingServiceForRPM(t, cache, repo)

	require.NoError(t, svc.checkRPM(context.Background(), nil, &Group{ID: 1, RPMLimit: 10}))
	require.EqualValues(t, 0, atomic.LoadInt32(&cache.userGroupCalls))
	require.EqualValues(t, 0, atomic.LoadInt32(&cache.userCalls))
	require.EqualValues(t, 0, atomic.LoadInt32(&repo.calls))
}

// --- T2-2.6 / T2-2.7 / T3-2.13：限额事实与计次口径 ---

func TestBillingCacheService_CheckRPM_CountsOneLogicalRequestPerPlan(t *testing.T) {
	cache := &userRPMCacheStub{userCounts: []int{1, 2, 3, 4, 5}}
	svc := newBillingServiceForRPM(t, cache, nil)

	user := &User{ID: 1, RPMLimit: 100}
	group := &Group{ID: 10, RPMLimit: 100}

	ctx, plan := captureGatewayRequestPlan(context.Background(), GatewayRequestSpec{APIKeyID: 1, UserID: 1}, nil, nil, nil)
	require.NotNil(t, plan)

	// 同一逻辑请求内的重试/账号切换/同组兜底都复用同一个计划。
	require.NoError(t, svc.checkRPM(ctx, user, group))
	require.NoError(t, svc.checkRPM(ctx, user, group))
	require.NoError(t, svc.checkRPM(ctx, user, group))

	require.EqualValues(t, 1, atomic.LoadInt32(&cache.userCalls),
		"用户级 RPM 每个逻辑请求只计一次，重试不得重复计入")
	require.EqualValues(t, 1, atomic.LoadInt32(&cache.userGroupCalls),
		"同一分组内的重试不得重复计入分组计数")

	// 兜底到另一个分组：那个分组的容量确实被占用，单独计一次。
	fallback := &Group{ID: 11, RPMLimit: 100}
	require.NoError(t, svc.checkRPM(ctx, user, fallback))
	require.EqualValues(t, 2, atomic.LoadInt32(&cache.userGroupCalls))
	require.EqualValues(t, 1, atomic.LoadInt32(&cache.userCalls))

	// 新的逻辑请求（新计划）重新计数。
	_, nextPlan := captureGatewayRequestPlan(context.Background(), GatewayRequestSpec{APIKeyID: 1, UserID: 1}, nil, nil, nil)
	require.NotNil(t, nextPlan)
	require.NoError(t, svc.checkRPM(WithGatewayRequestPlan(context.Background(), nextPlan), user, group))
	require.EqualValues(t, 2, atomic.LoadInt32(&cache.userCalls))
}

func TestBillingCacheService_CheckRPM_RejectionCarriesTheRealWindowReset(t *testing.T) {
	resetAt := time.Now().Add(30 * time.Second).Truncate(time.Second)
	cache := &userRPMCacheStub{userCounts: []int{99}, windowResetAt: resetAt}
	svc := newBillingServiceForRPM(t, cache, nil)

	err := svc.checkRPM(context.Background(), &User{ID: 1, RPMLimit: 10}, nil)
	require.ErrorIs(t, err, ErrUserRPMExceeded)

	facts, ok := AdmissionRateLimitFacts(err)
	require.True(t, ok, "an RPM rejection must carry its real window facts")
	require.Equal(t, 10, facts.Limit)
	require.Equal(t, 99, facts.Limit-0-facts.Remaining,
		"remaining is derived from the counter that caused the rejection")
	require.False(t, facts.ResetAt.IsZero())
	require.WithinDuration(t, resetAt, facts.ResetAt, time.Second)
}

func TestBillingCacheService_CheckRPM_NoServerTimeMeansNoRetryAfter(t *testing.T) {
	cache := &userRPMCacheStub{userCounts: []int{99}, windowResetErr: errors.New("redis down")}
	svc := newBillingServiceForRPM(t, cache, nil)

	err := svc.checkRPM(context.Background(), &User{ID: 1, RPMLimit: 10}, nil)
	require.ErrorIs(t, err, ErrUserRPMExceeded)

	facts, _ := AdmissionRateLimitFacts(err)
	require.True(t, facts.ResetAt.IsZero(),
		"without server time there is no trustworthy reset, so none is invented")
	require.Equal(t, 0, RetryAfterSecondsUntil(facts.ResetAt, time.Now()))
}
