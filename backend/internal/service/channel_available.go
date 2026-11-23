package service

import (
	"context"
	"sort"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
)

// AvailableGroupRef 渠道视图中关联分组的简要信息。
//
// 用户侧「可用渠道」页面据此展示：专属分组 vs 公开分组（IsExclusive）、
// 订阅 vs 标准（SubscriptionType）、默认倍率（RateMultiplier）与高峰倍率规则。
// 用户专属倍率不在这里暴露，前端自己通过 /groups/rates 拉取，和 API 密钥页面保持一致。
type AvailableGroupRef struct {
	ID                 int64
	Name               string
	Platform           string
	SubscriptionType   string
	RateMultiplier     float64
	PeakRateEnabled    bool
	PeakStart          string
	PeakEnd            string
	PeakRateMultiplier float64
	IsExclusive        bool
}

// AvailableChannel 可用渠道视图：用于「可用渠道」页面展示渠道基础信息 +
// 关联的分组 + 推导出的支持模型列表（无通配符）。
type AvailableChannel struct {
	ID                 int64
	Name               string
	Description        string
	Status             string
	BillingModelSource string
	RestrictModels     bool
	Groups             []AvailableGroupRef
	SupportedModels    []SupportedModel
}

// ListAvailable 返回所有渠道的可用视图：每个渠道附带关联分组信息与支持模型列表。
//
// 支持模型通过 (*Channel).SupportedModels() 计算（mapping ∪ pricing 并联）。
// 对于渠道未配置定价的模型，进一步用 PricingService 的全局 LiteLLM 数据合成
// 一份展示用定价，让用户看到默认价格而非"未配置"。
//
// 关联分组信息通过 groupRepo.ListActive 查询后按 ID 映射；渠道 GroupIDs 中未在活跃列表中
// 的分组（已停用或删除）会被忽略。
//
// 前置条件：s.groupRepo 必须非 nil（由 wire DI 保证）。直接 nil-deref 用于 fail-fast，
// 避免静默掩盖注入缺失。
func (s *ChannelService) ListAvailable(ctx context.Context) ([]AvailableChannel, error) {
	return s.listAvailable(ctx, nil)
}

func (s *ChannelService) ListAvailableForGroups(ctx context.Context, allowed map[int64]struct{}) ([]AvailableChannel, error) {
	return s.listAvailable(ctx, func(group Group) bool { _, ok := allowed[group.ID]; return ok })
}

func (s *ChannelService) listAvailable(ctx context.Context, permitted func(Group) bool) ([]AvailableChannel, error) {
	ctx, panels, err := acquireCatalogPanels(ctx, s.visibleModels, s.repo, s.groupRepo, permitted, nil)
	if err != nil {
		return nil, err
	}
	channels, groups, visible := panels.channels, panels.groups, panels.visible
	policyGroups := make(map[int64]*Group, len(groups))
	for index := range groups {
		policyGroups[groups[index].ID] = &groups[index]
	}
	groupByID := make(map[int64]AvailableGroupRef, len(groups))
	for i := range groups {
		g := groups[i]
		groupByID[g.ID] = AvailableGroupRef{
			ID:                 g.ID,
			Name:               g.Name,
			Platform:           g.Platform,
			SubscriptionType:   g.SubscriptionType,
			RateMultiplier:     g.RateMultiplier,
			PeakRateEnabled:    g.PeakRateEnabled,
			PeakStart:          g.PeakStart,
			PeakEnd:            g.PeakEnd,
			PeakRateMultiplier: g.PeakRateMultiplier,
			IsExclusive:        g.IsExclusive,
		}
	}

	out := make([]AvailableChannel, 0, len(channels))
	for i := range channels {
		ch := &channels[i]
		groups := make([]AvailableGroupRef, 0, len(ch.GroupIDs))
		for _, gid := range ch.GroupIDs {
			if ref, ok := groupByID[gid]; ok {
				groups = append(groups, ref)
			}
		}
		sort.SliceStable(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })

		ch.normalizeBillingModelSource()

		supported := make([]SupportedModel, 0)
		for _, model := range ch.SupportedModels() {
			for _, groupID := range ch.GroupIDs {
				group := policyGroups[groupID]
				if group == nil || (group.Platform != PlatformComposite && !domain.MatchesPlatformIdentity(model.Platform, domain.HostedAccountType(model.Platform), group.Platform, group.RequiredAccountType())) {
					continue
				}
				if panelModelVisible(ctx, group, model.Name, visible) {
					if s.visibleModels != nil && group.Platform == PlatformComposite && !catalogGroupProviderMatches(panels.bindings[groupID][model.Name], model.Platform) {
						continue
					}
					supported = append(supported, model)
					break
				}
			}
		}
		fillGlobalPricingFallback(s.pricingService, supported)

		out = append(out, AvailableChannel{
			ID:                 ch.ID,
			Name:               ch.Name,
			Description:        ch.Description,
			Status:             ch.Status,
			BillingModelSource: ch.BillingModelSource,
			RestrictModels:     ch.RestrictModels,
			Groups:             groups,
			SupportedModels:    supported,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	if err := s.visibleModels.validate(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func catalogGroupProviderMatches(bindings []PlazaCatalogBinding, provider string) bool {
	for _, binding := range bindings {
		if domain.MatchesPlatformIdentity(binding.Identity.Platform, binding.Identity.HostedAccountType, provider, domain.HostedAccountType(provider)) {
			return true
		}
	}
	return false
}

// fillGlobalPricingFallback 对未命中渠道定价的支持模型，从全局 LiteLLM 数据合成一份
// 展示用定价。仅用于「可用渠道」展示，不影响真实计费链路。
//
// 触发条件：
//  1. Pricing == nil（渠道完全没声明该模型的定价条目）
//  2. Pricing 非 nil 但所有价格字段为空（admin UI 建了条目但没填价格）
//
// 当 pricingService 为 nil（测试场景），跳过价格回落，但仍补充内置模型倍率。
// 可用渠道与模型广场共用。
func fillGlobalPricingFallback(pricingService *PricingService, models []SupportedModel) {
	for i := range models {
		if pricingService != nil && pricingNeedsFallback(models[i].Pricing) {
			if lp := pricingService.GetModelPricing(models[i].Name); lp != nil {
				models[i].Pricing = synthesizePricingFromLiteLLM(lp, models[i].Pricing)
			}
		}
		models[i].Pricing = withDefaultMaxReasoningEffortMultiplier(models[i].Pricing, models[i].Name)
	}
}

// pricingNeedsFallback 判定一个 ChannelModelPricing 是否需要走全局回落。
// 价格全部缺失（无 flat 字段且无任何带价 interval）即视为未配置。
func pricingNeedsFallback(p *ChannelModelPricing) bool {
	if p == nil {
		return true
	}
	if p.InputPrice != nil || p.OutputPrice != nil ||
		p.CacheWritePrice != nil || p.CacheWrite1hPrice != nil || p.CacheReadPrice != nil ||
		p.ImageOutputPrice != nil || p.PerRequestPrice != nil {
		return false
	}
	for _, iv := range p.Intervals {
		if iv.InputPrice != nil || iv.OutputPrice != nil ||
			iv.CacheWritePrice != nil || iv.CacheWrite1hPrice != nil || iv.CacheReadPrice != nil ||
			iv.PerRequestPrice != nil {
			return false
		}
	}
	return true
}

// synthesizePricingFromLiteLLM 把 LiteLLM 的定价数据转成 ChannelModelPricing 形态，
// 仅用于展示。
//
// 计费模式优先级：
//  1. 渠道已选 BillingMode（admin 在 UI 里选了 image / per_request 但没填价的场景，
//     按选定模式合成对应字段）
//  2. LiteLLM mode="image_generation" → image
//  3. 默认 token
//
// LiteLLM 中字段 0 视为未配置，不带入展示。
func synthesizePricingFromLiteLLM(lp *LiteLLMModelPricing, existing *ChannelModelPricing) *ChannelModelPricing {
	if lp == nil {
		return existing
	}

	mode := BillingModeToken
	switch {
	case existing != nil && existing.BillingMode != "":
		mode = existing.BillingMode
	case lp.Mode == "image_generation":
		mode = BillingModeImage
	}

	if mode == BillingModeImage || mode == BillingModePerRequest {
		return &ChannelModelPricing{
			BillingMode:                  mode,
			PerRequestPrice:              nonZeroPtr(lp.OutputCostPerImage),
			ImageOutputPrice:             nonZeroPtr(lp.OutputCostPerImageToken),
			InputPrice:                   nonZeroPtr(lp.InputCostPerToken),
			OutputPrice:                  nonZeroPtr(lp.OutputCostPerToken),
			MaxReasoningEffortMultiplier: maxReasoningEffortMultiplierFromPricing(existing),
		}
	}
	return &ChannelModelPricing{
		BillingMode:                  mode,
		InputPrice:                   nonZeroPtr(lp.InputCostPerToken),
		OutputPrice:                  nonZeroPtr(lp.OutputCostPerToken),
		CacheWritePrice:              nonZeroPtr(lp.CacheCreationInputTokenCost),
		CacheWrite1hPrice:            nonZeroPtr(lp.CacheCreationInputTokenCostAbove1hr),
		CacheReadPrice:               nonZeroPtr(lp.CacheReadInputTokenCost),
		ImageOutputPrice:             nonZeroPtr(lp.OutputCostPerImageToken),
		MaxReasoningEffortMultiplier: maxReasoningEffortMultiplierFromPricing(existing),
	}
}

func maxReasoningEffortMultiplierFromPricing(pricing *ChannelModelPricing) *float64 {
	if pricing == nil {
		return nil
	}
	return pricing.MaxReasoningEffortMultiplier
}

func nonZeroPtr(v float64) *float64 {
	if v == 0 {
		return nil
	}
	return &v
}
