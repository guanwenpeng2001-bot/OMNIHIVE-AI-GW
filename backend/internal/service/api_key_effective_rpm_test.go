//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
)

// effectiveRPMGroupRepoStub 只为 EffectiveRPM 提供分组限额与 (用户,分组) 覆盖。
type effectiveRPMGroupRepoStub struct {
	GroupRepository
	group    *Group
	override *int
}

func (s *effectiveRPMGroupRepoStub) GetByIDLite(context.Context, int64) (*Group, error) {
	return s.group, nil
}

func (s *effectiveRPMGroupRepoStub) GetRPMOverrideByUserAndGroup(context.Context, int64, int64) (*int, error) {
	return s.override, nil
}

// effectiveRPMCacheStub 实现 RPM 窗口窄口，模拟 Redis 服务端时间。
type effectiveRPMCacheStub struct {
	APIKeyCache
	resetAt time.Time
	err     error
}

func (s *effectiveRPMCacheStub) RPMWindowResetAt(context.Context) (time.Time, error) {
	return s.resetAt, s.err
}

// effectiveRPMEmptyCacheStub 不实现窗口窄口：重置时间必须留空。
type effectiveRPMEmptyCacheStub struct {
	APIKeyCache
}

func newEffectiveRPMService(cache APIKeyCache, groups *effectiveRPMGroupRepoStub) *APIKeyService {
	return NewAPIKeyService(nil, nil, groups, nil,
		&effectiveRPMUserGroupRateRepoStub{groups: groups}, cache, &config.Config{})
}

// effectiveRPMUserGroupRateRepoStub 复用同一份覆盖数据，保持用户侧与分组侧同源。
type effectiveRPMUserGroupRateRepoStub struct {
	UserGroupRateRepository
	groups *effectiveRPMGroupRepoStub
}

func (s *effectiveRPMUserGroupRateRepoStub) GetRPMOverrideByUserAndGroup(_ context.Context, _, _ int64) (*int, error) {
	return s.groups.override, nil
}

func TestAPIKeyService_EffectiveRPM_ReportsSourceLimitAndReset(t *testing.T) {
	resetAt := time.Now().Add(40 * time.Second).Truncate(time.Second)
	override := 25
	groups := &effectiveRPMGroupRepoStub{
		group:    &Group{ID: 5, Name: "team", RPMLimit: 100},
		override: &override,
	}
	svc := newEffectiveRPMService(&effectiveRPMCacheStub{resetAt: resetAt}, groups)

	key := &APIKey{
		ID: 7, UserID: 3, GroupID: ptrInt64(5),
		User:  &User{ID: 3, RPMLimit: 300},
		Group: &Group{ID: 5, Name: "team", RPMLimit: 100},
	}

	doc := svc.EffectiveRPM(context.Background(), key)
	require.NotNil(t, doc)
	require.Equal(t, EffectiveRPMPriority, doc.Priority)
	require.Len(t, doc.Limits, 2)
	require.Equal(t, AdmissionRateLimitSourceUserGroupOverride, doc.Limits[0].Source,
		"the per-(user, group) override is the effective group-side limit")
	require.Equal(t, 25, doc.Limits[0].Limit)
	require.Equal(t, AdmissionRateLimitSourceUser, doc.Limits[1].Source,
		"the user ceiling is listed separately and is never replaced")
	require.Equal(t, 300, doc.Limits[1].Limit)

	require.NotNil(t, doc.WindowResetsAt)
	require.WithinDuration(t, resetAt, *doc.WindowResetsAt, time.Second)
}

func TestAPIKeyService_EffectiveRPM_FallsBackToGroupLimitWithoutOverride(t *testing.T) {
	svc := newEffectiveRPMService(&effectiveRPMEmptyCacheStub{}, &effectiveRPMGroupRepoStub{group: &Group{ID: 5, RPMLimit: 80}})

	doc := svc.EffectiveRPM(context.Background(), &APIKey{
		ID: 7, UserID: 3, GroupID: ptrInt64(5),
		User:  &User{ID: 3, RPMLimit: 0},
		Group: &Group{ID: 5, RPMLimit: 80},
	})
	require.Len(t, doc.Limits, 1)
	require.Equal(t, AdmissionRateLimitSourceGroup, doc.Limits[0].Source)
	require.Equal(t, 80, doc.Limits[0].Limit)
	require.Nil(t, doc.WindowResetsAt,
		"without the server-time port there is no trustworthy reset, so none is shown")
}

func TestAPIKeyService_EffectiveRPM_ZeroOverrideExemptsTheGroupOnly(t *testing.T) {
	zero := 0
	groups := &effectiveRPMGroupRepoStub{group: &Group{ID: 5, RPMLimit: 80}, override: &zero}
	svc := newEffectiveRPMService(&effectiveRPMEmptyCacheStub{}, groups)

	doc := svc.EffectiveRPM(context.Background(), &APIKey{
		ID: 7, UserID: 3, GroupID: ptrInt64(5),
		User:  &User{ID: 3, RPMLimit: 90},
		Group: &Group{ID: 5, RPMLimit: 80},
	})
	require.Len(t, doc.Limits, 1)
	require.Equal(t, AdmissionRateLimitSourceUser, doc.Limits[0].Source,
		"an override of zero exempts the group dimension but never the user ceiling")
	require.Equal(t, 90, doc.Limits[0].Limit)
}

func TestAPIKeyService_EffectiveRPM_NoLimitsIsAnExplicitEmptyList(t *testing.T) {
	svc := newEffectiveRPMService(&effectiveRPMEmptyCacheStub{}, &effectiveRPMGroupRepoStub{})
	doc := svc.EffectiveRPM(context.Background(), &APIKey{ID: 7, UserID: 3, User: &User{ID: 3}})
	require.NotNil(t, doc)
	require.NotNil(t, doc.Limits)
	require.Empty(t, doc.Limits, "no configured limit is reported as an empty list, not as a fabricated zero")
	require.Nil(t, svc.EffectiveRPM(context.Background(), nil))
}
