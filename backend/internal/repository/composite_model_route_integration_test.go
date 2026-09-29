//go:build integration

package repository

import (
	"context"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// compositeRouteFixtureGroup 建一个本次运行独占的分组，并在用例结束后清掉它与它
// 的路由：集成库是可重复使用的，固定名字会与上一次运行留下的行冲突。
func compositeRouteFixtureGroup(t *testing.T, client *dbent.Client, label string) *service.Group {
	t.Helper()
	group := mustCreateGroup(t, client, &service.Group{Name: label + "-" + uuid.NewString()})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM composite_model_routes WHERE group_id = $1", group.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM groups WHERE id = $1", group.ID)
	})
	return group
}

// 有序目标必须真实经过 JSONB 列往返：写入按声明顺序，读回保持顺序、权重、区域/供应商
// 约束、延迟偏好与亲和性 TTL。
func TestCompositeModelRouteOrderedTargetsPersistThroughJSONB(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewCompositeModelRouteRepository(client)
	group := compositeRouteFixtureGroup(t, client, "composite-ordered-targets")

	targets := []service.CompositeRouteTarget{
		{
			Position:           0,
			TargetPlatform:     service.PlatformAnthropic,
			UpstreamModel:      "claude-sonnet-4-6",
			Region:             "eu-west-1",
			SchedulingWeight:   50,
			LatencyPreference:  service.CompositeTargetLatencyDefault,
			AffinityTTLSeconds: 300,
			Enabled:            true,
		},
		{
			Position:          1,
			TargetPlatform:    service.PlatformOpenAI,
			UpstreamModel:     "gpt-5",
			Provider:          "azure",
			SchedulingWeight:  200,
			LatencyPreference: service.CompositeTargetLatencyLowLatency,
			Enabled:           true,
		},
	}

	created := &service.CompositeModelRoute{
		GroupID:        group.ID,
		PublicModel:    "public-model",
		MatchType:      service.CompositeRouteMatchExact,
		TargetPlatform: service.PlatformAnthropic,
		UpstreamModel:  "claude-sonnet-4-6",
		Endpoint:       service.CompositeRouteEndpointAny,
		Priority:       100,
		Enabled:        true,
		Targets:        targets,
	}
	require.NoError(t, repo.Create(ctx, created))

	stored := loadCompositeRouteByID(t, ctx, repo, group.ID, created.ID)
	require.Equal(t, targets, stored.Targets)
	require.Equal(t, targets, stored.EffectiveTargets())

	// 未提供有序目标时旧单目标语义保持不变（回滚/旧客户端路径）。
	legacy := &service.CompositeModelRoute{
		GroupID:        group.ID,
		PublicModel:    "legacy-model",
		MatchType:      service.CompositeRouteMatchExact,
		TargetPlatform: service.PlatformOpenAI,
		UpstreamModel:  "gpt-5",
		Endpoint:       service.CompositeRouteEndpointAny,
		Priority:       100,
		Enabled:        true,
	}
	require.NoError(t, repo.Create(ctx, legacy))
	storedLegacy := loadCompositeRouteByID(t, ctx, repo, group.ID, legacy.ID)
	require.Empty(t, storedLegacy.Targets)
	require.Len(t, storedLegacy.EffectiveTargets(), 1)
	require.Equal(t, service.PlatformOpenAI, storedLegacy.EffectiveTargets()[0].TargetPlatform)

	// 显式空列表清空回单目标兼容模式。
	require.NoError(t, repo.Update(ctx, &service.CompositeModelRoute{
		ID:             created.ID,
		GroupID:        group.ID,
		PublicModel:    "public-model",
		MatchType:      service.CompositeRouteMatchExact,
		TargetPlatform: service.PlatformAnthropic,
		UpstreamModel:  "claude-sonnet-4-6",
		Endpoint:       service.CompositeRouteEndpointAny,
		Priority:       100,
		Enabled:        true,
		Targets:        []service.CompositeRouteTarget{},
	}))
	cleared := loadCompositeRouteByID(t, ctx, repo, group.ID, created.ID)
	require.Empty(t, cleared.Targets)
	require.Len(t, cleared.EffectiveTargets(), 1)
}

// 迁移 273 的回填：旧行（ordered_targets 为空）变成等效的单目标，行为与旧列一致。
func TestOrderedCompositeTargetsMigrationBackfillsLegacyRow(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	group := compositeRouteFixtureGroup(t, client, "composite-migration-backfill")

	// 直接以旧形态写入：只填旧列，ordered_targets 保持 NULL。
	legacy, err := client.CompositeModelRoute.Create().
		SetGroupID(group.ID).
		SetPublicModel("migrated-model").
		SetMatchType(service.CompositeRouteMatchExact).
		SetTargetPlatform(service.PlatformBedrock).
		SetUpstreamModel("anthropic.claude-sonnet-4-6").
		SetEndpoint(service.CompositeRouteEndpointAny).
		SetPriority(100).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	require.NoError(t, applyOrderedCompositeTargetsBackfill(ctx))

	repo := NewCompositeModelRouteRepository(client)
	stored := loadCompositeRouteByID(t, ctx, repo, group.ID, legacy.ID)
	require.Len(t, stored.Targets, 1)
	require.Equal(t, 0, stored.Targets[0].Position)
	require.Equal(t, service.PlatformBedrock, stored.Targets[0].TargetPlatform)
	require.Equal(t, "anthropic.claude-sonnet-4-6", stored.Targets[0].UpstreamModel)
	require.Equal(t, service.CompositeTargetDefaultWeight, stored.Targets[0].SchedulingWeight)
	require.Equal(t, service.CompositeTargetLatencyDefault, stored.Targets[0].LatencyPreference)
	require.True(t, stored.Targets[0].Enabled)
	// 幂等：重复执行不改变结果。
	require.NoError(t, applyOrderedCompositeTargetsBackfill(ctx))
	again := loadCompositeRouteByID(t, ctx, repo, group.ID, legacy.ID)
	require.Equal(t, stored.Targets, again.Targets)
}

func loadCompositeRouteByID(t *testing.T, ctx context.Context, repo service.CompositeModelRouteRepository, groupID, routeID int64) service.CompositeModelRoute {
	t.Helper()
	routes, err := repo.ListByGroup(ctx, groupID, true)
	require.NoError(t, err)
	for _, route := range routes {
		if route.ID == routeID {
			return route
		}
	}
	t.Fatalf("composite route %d not found in group %d", routeID, groupID)
	return service.CompositeModelRoute{}
}

// applyOrderedCompositeTargetsBackfill 执行 273 迁移中的幂等回填语句（迁移已在测试库
// 建表阶段执行过一次，这里验证语句本身对遗留行仍成立且可重复执行）。
func applyOrderedCompositeTargetsBackfill(ctx context.Context) error {
	_, err := integrationDB.ExecContext(ctx, `
UPDATE composite_model_routes
SET ordered_targets = jsonb_build_array(
      jsonb_build_object(
        'position', 0,
        'target_platform', target_platform,
        'account_type', account_type,
        'upstream_model', upstream_model,
        'latency_preference', 'default',
        'scheduling_weight', 100,
        'enabled', true
      )
    )
WHERE ordered_targets IS NULL`)
	return err
}
