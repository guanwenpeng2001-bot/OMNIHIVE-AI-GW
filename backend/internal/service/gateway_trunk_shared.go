package service

// 本文件集中 Claude 主干（GatewayService）与 OpenAI 主干（OpenAIGatewayService）
// 共用的资格判定、准入与调度决策实现。两条主干各自保留同名入口方法，但决策只在这里
// 实现一次，避免同一规则在两条主干上漂移；这里没有第三条流水线，只有被两条主干
// 共同调用的纯函数。

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// resolveChannelMapping 解析渠道级模型映射；未配置渠道服务时原样返回请求模型。
func resolveChannelMapping(ctx context.Context, channelService *ChannelService, groupID int64, model string) ChannelMappingResult {
	if channelService == nil {
		return ChannelMappingResult{MappedModel: model}
	}
	return channelService.ResolveChannelMapping(ctx, groupID, model)
}

// channelModelRestricted 检查模型是否被渠道限制。
func channelModelRestricted(ctx context.Context, channelService *ChannelService, groupID int64, model string) bool {
	if channelService == nil {
		return false
	}
	return channelService.IsModelRestricted(ctx, groupID, model)
}

// resolveChannelMappingAndRestriction 解析渠道映射；模型限制检查已移到调度阶段的
// channelPricingRestrictionBlocks，因此这里的 restricted 恒为 false。
func resolveChannelMappingAndRestriction(ctx context.Context, channelService *ChannelService, groupID *int64, model string) (ChannelMappingResult, bool) {
	if channelService == nil {
		return ChannelMappingResult{MappedModel: model}, false
	}
	return channelService.ResolveChannelMappingAndRestrict(ctx, groupID, model)
}

// channelPricingRestrictionBlocks 按渠道计费基准检查模型是否受定价列表限制，
// 供两条主干的调度阶段做同一份准入预检查。
func channelPricingRestrictionBlocks(ctx context.Context, channelService *ChannelService, groupID *int64, requestedModel string) bool {
	if groupID == nil || channelService == nil || requestedModel == "" {
		return false
	}
	mapping := channelService.ResolveChannelMapping(ctx, *groupID, requestedModel)
	billingModel := billingModelForRestriction(mapping.BillingModelSource, requestedModel, mapping.MappedModel)
	if billingModel == "" {
		return false
	}
	return channelService.IsModelRestricted(ctx, *groupID, billingModel)
}

// accountBlockedBySchedulingThreshold 返回该账号是否被限流服务的调度阈值挡住；
// 未配置限流服务时视为不阻塞。
func accountBlockedBySchedulingThreshold(ctx context.Context, rateLimitService *RateLimitService, account *Account) bool {
	if rateLimitService == nil || account == nil {
		return false
	}
	return rateLimitService.ApplyAccountSchedulingThreshold(ctx, account)
}

// acquireAccountSlot 申请账号并发槽位；未配置并发服务时视为无限制并返回空释放函数，
// 使两条主干在未接线场景下保持同一行为。
func acquireAccountSlot(ctx context.Context, concurrencyService *ConcurrencyService, accountID int64, maxConcurrency int) (*AcquireResult, error) {
	if concurrencyService == nil {
		return &AcquireResult{Acquired: true, ReleaseFunc: func() {}}, nil
	}
	return concurrencyService.AcquireAccountSlot(ctx, accountID, maxConcurrency)
}

// gatewaySchedulingConfig 返回两条主干共用的调度配置及其内置默认值。
func gatewaySchedulingConfig(cfg *config.Config) config.GatewaySchedulingConfig {
	if cfg != nil {
		return cfg.Gateway.Scheduling
	}
	return config.GatewaySchedulingConfig{
		StickySessionMaxWaiting:  3,
		StickySessionWaitTimeout: 45 * time.Second,
		FallbackWaitTimeout:      30 * time.Second,
		FallbackMaxWaiting:       100,
		LoadBatchEnabled:         true,
		SlotCleanupInterval:      30 * time.Second,
	}
}
