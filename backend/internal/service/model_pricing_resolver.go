package service

import (
	"context"
	"log/slog"
	"maps"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/pricing"
)

// PricingSource 定价来源标识
const (
	PricingSourceGroup    = "group"
	PricingSourceChannel  = "channel"
	PricingSourceLiteLLM  = "litellm"
	PricingSourceFallback = "fallback"
)

// ResolvedPricing 统一定价解析结果
type ResolvedPricing struct {
	Context    *pricing.PriceContext `json:"-"`
	descriptor *ModelPricingDescriptor
	pricingErr error
	// Mode 计费模式
	Mode BillingMode

	// Token 模式：基础定价（来自 LiteLLM 或 fallback）
	BasePricing *ModelPricing

	// Token 模式：区间定价列表（如有，覆盖 BasePricing 中的对应字段）
	Intervals []PricingInterval

	// 按次/图片模式：分层定价
	RequestTiers []PricingInterval

	// 按次/图片模式：默认价格（未命中层级时使用）
	DefaultPerRequestPrice      float64
	DefaultPerRequestPriceFound bool

	// 来源标识
	Source string // "channel", "litellm", "fallback"

	// 是否支持缓存细分
	SupportsCacheBreakdown bool

	// 渠道定价原始配置（用于区间模式下获取 ImageOutputPrice）
	channelPricing *ChannelModelPricing

	longContextPricingEnabled bool
}

// Provenance labels must not change the established default-card policy. Group
// and channel overrides retain their own semantics; all legacy baseline origins
// used this same policy before their source labels became distinguishable.
func (r *ResolvedPricing) usesBaselinePolicy() bool {
	return r != nil && (r.Source == PricingSourceLiteLLM || r.Source == PricingSourceFallback || r.Source == "operator_override" || r.Source == "mixed_sale")
}

// ModelPricingResolver 统一模型定价解析器。
// 解析链：Group → Channel → LiteLLM → Fallback。
type ModelPricingResolver struct {
	channelService *ChannelService
	billingService *BillingService
}

// NewModelPricingResolver 创建定价解析器实例
func NewModelPricingResolver(channelService *ChannelService, billingService *BillingService) *ModelPricingResolver {
	return &ModelPricingResolver{
		channelService: channelService,
		billingService: billingService,
	}
}

// PricingInput 定价解析输入
type PricingInput struct {
	Provider  string
	Mode      BillingMode // required billing unit; empty preserves the token entry point
	Model     string
	GroupID   *int64 // nil 表示不检查渠道
	Group     *Group
	PricingAt time.Time
}

// forPricingInstant captures the existing request service first. A normal
// calculation pins its current read-only price map and the selected clock once;
// an admitted request never replaces its already frozen prices.
func (r *ModelPricingResolver) forPricingInstant(ctx context.Context, input PricingInput) (*ModelPricingResolver, PricingInput, error) {
	input.Group = frozenSaleGroup(ctx, input.Group)
	billing := r.billingService.forGatewayRequest(ctx)
	var frozenAt time.Time
	if billing != nil {
		frozenAt = billing.frozenPricingAt
	}
	var err error
	input.PricingAt, _, err = pricingEvaluationInstant(ctx, input.PricingAt, frozenAt)
	if err != nil {
		return r, input, err
	}
	if billing != nil && billing.frozenPricingAt.IsZero() {
		billing = frozenBillingService(billing, ModelRegistrySnapshotFromContext(ctx), input.PricingAt)
	}
	if billing != r.billingService {
		frozen := *r
		frozen.billingService = billing
		r = &frozen
	}
	return r, input, nil
}

// A descriptor can choose a lifecycle/expiry-specific tariff. Its clock and
// the exported context must agree before a pre-resolved card can be reused.
func resolvedPricingInstant(resolved *ResolvedPricing) (time.Time, error) {
	if resolved == nil {
		return time.Time{}, nil
	}
	var at time.Time
	if resolved.Context != nil {
		at = resolved.Context.PricingAt
	}
	if resolved.descriptor != nil && !resolved.descriptor.PricingAt.IsZero() {
		if !at.IsZero() && !at.Equal(resolved.descriptor.PricingAt) {
			return time.Time{}, ErrSalePriceChanged
		}
		at = resolved.descriptor.PricingAt
	}
	return at, nil
}

func pricingGroupID(key *APIKey) *int64 {
	if key == nil {
		return nil
	}
	if key.Group != nil && key.Group.ID > 0 {
		return &key.Group.ID
	}
	return key.GroupID
}

func (r *ModelPricingResolver) configuredForKey(ctx context.Context, model string, key *APIKey) *ResolvedPricing {
	if r == nil || key == nil || (key.Group == nil && key.GroupID == nil) {
		return nil
	}
	resolved := r.Resolve(ctx, PricingInput{Model: model, Group: key.Group, GroupID: pricingGroupID(key)})
	if resolved.Source == PricingSourceGroup || resolved.Source == PricingSourceChannel {
		return resolved
	}
	return nil
}

// Resolve 解析模型定价。
// 1. 获取基础定价（LiteLLM → Fallback）
// 2. 如果指定了 GroupID，查找渠道定价并覆盖
func (r *ModelPricingResolver) Resolve(ctx context.Context, input PricingInput) (result *ResolvedPricing) {
	input.Group = frozenSaleGroup(ctx, input.Group)
	if input.Provider != "" {
		ctx = WithResolvedTargetPlatform(ctx, input.Provider)
	}
	var err error
	r, input, err = r.forPricingInstant(ctx, input)
	if err != nil {
		return &ResolvedPricing{Mode: input.Mode, pricingErr: err}
	}
	if input.Provider == "" {
		input.Provider, _ = ResolvedTargetPlatformFromContext(ctx)
		if input.Provider == "" && input.Group != nil {
			input.Provider = input.Group.Platform
		}
	}
	defer func() {
		if result == nil {
			return
		}
		pc := pricing.PriceContext{Provider: input.Provider, Model: input.Model, Funding: "wallet", PricingAt: input.PricingAt}
		if input.GroupID != nil {
			pc.GroupID = *input.GroupID
		} else if input.Group != nil {
			pc.GroupID = input.Group.ID
		}
		if plan := GatewayRequestPlanFromContext(ctx); plan != nil {
			pc = plan.PriceContext(input.Provider, input.Model)
		}
		result.Context = &pc
	}()
	if descriptor, found, err := r.configuredPriceDescriptor(ctx, input); err != nil {
		return &ResolvedPricing{Mode: input.Mode, Source: PricingSourceChannel, pricingErr: err}
	} else if found {
		return descriptor.resolvePricing(input.Mode)
	}
	longContextPricingEnabled := input.Group == nil || input.Group.LongContextPricingEnabled
	platform := ""
	if input.Group != nil {
		platform = channelLookupPlatform(ctx, input.Group.Platform)
	}
	if groupPricing := matchGroupModelPricing(input.Group, input.Model, platform); groupPricing != nil {
		// Ordinary group cards override flat rates; optional long-context
		// ladders come from presets. Native Qianwen input tiers are mandatory.
		if groupPricing.NativePricingSource != QianwenPricingSource && (groupPricing.BillingMode == "" || groupPricing.BillingMode == BillingModeToken) {
			stripped := groupPricing.Clone()
			stripped.Intervals = nil
			groupPricing = &stripped
		}
		resolved := r.resolveConfiguredPricing(groupPricing, input.Model, PricingSourceGroup, input.Provider)
		resolved.longContextPricingEnabled = longContextPricingEnabled && groupPricing.NativePricingSource != QianwenPricingSource
		return resolved
	}

	var chPricing *ChannelModelPricing
	if input.GroupID != nil && r.channelService != nil {
		chPricing = r.lookupChannelPricingNormalized(ctx, *input.GroupID, input.Model)
		if chPricing != nil {
			if chPricing.NativePricingSource == QianwenPricingSource {
				resolved := r.resolveConfiguredPricing(chPricing, input.Model, PricingSourceChannel, input.Provider)
				resolved.longContextPricingEnabled = false
				return resolved
			}
			mode := chPricing.BillingMode
			if mode == "" {
				mode = BillingModeToken
			}
			if mode == BillingModePerRequest || mode == BillingModeImage || mode == BillingModeVideo {
				resolved := &ResolvedPricing{
					Mode:           mode,
					Source:         PricingSourceChannel,
					channelPricing: chPricing,
				}
				resolved.longContextPricingEnabled = longContextPricingEnabled
				r.applyRequestTierOverrides(chPricing, resolved)
				return resolved
			}
		}
	}

	// 1. 获取基础定价
	pricingModel := input.Model
	if r.billingService != nil && r.billingService.pricingService != nil {
		bound, err := r.billingService.pricingService.BoundPricingModel(input.Provider, input.Model)
		if err != nil {
			return &ResolvedPricing{Mode: BillingModeToken, Source: PricingSourceFallback, pricingErr: err}
		}
		pricingModel = bound
	}
	basePricing, source := r.resolveBasePricing(pricingModel)

	resolved := &ResolvedPricing{
		Mode:                   BillingModeToken,
		BasePricing:            basePricing,
		Source:                 source,
		SupportsCacheBreakdown: basePricing != nil && basePricing.SupportsCacheBreakdown,
	}
	resolved.longContextPricingEnabled = longContextPricingEnabled

	// 2. 如果有 GroupID，尝试渠道覆盖
	if chPricing != nil {
		resolved.Source = PricingSourceChannel
		resolved.channelPricing = chPricing
		r.applyTokenOverrides(chPricing, resolved)
	} else if input.GroupID != nil && r.channelService != nil {
		r.applyChannelOverrides(ctx, *input.GroupID, input.Model, resolved)
	}

	if platform == PlatformCommandCode && commandCodeOfficialFreeModel(input.Model) && resolved.Source != PricingSourceChannel {
		resolved.BasePricing = &ModelPricing{}
		resolved.Source = "official_free:commandcode"
	}
	return resolved
}

func (r *ModelPricingResolver) resolveConfiguredPricing(config *ChannelModelPricing, model, source string, providers ...string) *ResolvedPricing {
	mode := config.BillingMode
	if mode == "" {
		mode = BillingModeToken
	}
	resolved := &ResolvedPricing{Mode: mode, Source: source, channelPricing: config}
	if mode == BillingModePerRequest || mode == BillingModeImage || mode == BillingModeVideo {
		r.applyRequestTierOverrides(config, resolved)
		return resolved
	}
	if config.NativePricingSource != QianwenPricingSource {
		provider := ""
		if len(providers) > 0 {
			provider = providers[0]
		}
		bound := model
		var err error
		if r.billingService != nil && r.billingService.pricingService != nil {
			bound, err = r.billingService.pricingService.BoundPricingModel(provider, model)
		}
		// Complete operator cards work without a baseline. An ambiguous baseline
		// contributes no inherited dimensions; presence validation fails closed
		// when the request actually consumes an unspecified rate.
		if err == nil {
			resolved.BasePricing, _ = r.resolveBasePricing(bound)
		}
	}
	resolved.SupportsCacheBreakdown = resolved.BasePricing != nil && resolved.BasePricing.SupportsCacheBreakdown
	r.applyTokenOverrides(config, resolved)
	return resolved
}

func matchGroupModelPricing(group *Group, model string, platforms ...string) *ChannelModelPricing {
	if group == nil {
		return nil
	}
	model = normalizeChannelPricingModelName(model)
	platform := group.Platform
	if len(platforms) > 0 {
		platform = platforms[0]
	}
	var wildcard *ChannelModelPricing
	for i := range group.ModelPricing {
		entry := &group.ModelPricing[i]
		// An unresolved composite request must never borrow an arbitrary provider's card.
		if entry.Platform != "" && platform != "" && entry.Platform != platform {
			continue
		}
		for _, pattern := range entry.Models {
			normalized := normalizeChannelPricingModelName(pattern)
			if normalized == model {
				cp := entry.Clone()
				return &cp
			}
			if strings.HasSuffix(normalized, "*") && strings.HasPrefix(model, strings.TrimSuffix(normalized, "*")) && wildcard == nil {
				cp := entry.Clone()
				wildcard = &cp
			}
		}
	}
	return wildcard
}

// resolveBasePricing 从 LiteLLM 或 Fallback 获取基础定价
func (r *ModelPricingResolver) resolveBasePricing(model string) (*ModelPricing, string) {
	if r.billingService == nil {
		return nil, PricingSourceFallback
	}
	pricing, err := r.billingService.GetModelPricing(model)
	if err != nil {
		slog.Debug("failed to get model pricing from LiteLLM, using fallback",
			"model", model, "error", err)
		return nil, PricingSourceFallback
	}
	source := pricing.Source
	if source == "" {
		source = PricingSourceLiteLLM
	}
	return pricing, source
}

// lookupChannelPricingNormalized 查找渠道定价：先用字面模型名做精确/通配匹配，
// 未命中时用与官方兜底价一致的归一化模型名再查一次。
//
// 官方兜底价对 OpenAI/Codex 族会把 gpt-5.6-luna-high 这类变体名归一化到基名
// （billing_service.go 的 normalizeKnownOpenAICodexModel 分支），而渠道定价此前
// 只认字面名。两者不对称导致：管理员只配基名、请求模型带 effort 后缀时，渠道定价
// 未命中而官方兜底命中，计费候选循环首个成功即返回，渠道定价永远轮不到（issue #5256）。
//
// 字面名优先，保证管理员对具体变体的显式配价不被基名覆盖；非 OpenAI 模型
// normalizeKnownOpenAICodexModel 返回空串，此处天然 no-op。
func (r *ModelPricingResolver) lookupChannelPricingNormalized(ctx context.Context, groupID int64, model string) *ChannelModelPricing {
	if r.channelService == nil {
		return nil
	}
	if pricing := r.channelService.GetChannelModelPricing(ctx, groupID, model); pricing != nil {
		return pricing
	}
	normalized := normalizeKnownOpenAICodexModel(model)
	if normalized == "" || strings.EqualFold(normalized, strings.TrimSpace(model)) {
		return nil
	}
	return r.channelService.GetChannelModelPricing(ctx, groupID, normalized)
}

// applyChannelOverrides 应用渠道定价覆盖
func (r *ModelPricingResolver) applyChannelOverrides(ctx context.Context, groupID int64, model string, resolved *ResolvedPricing) {
	chPricing := r.lookupChannelPricingNormalized(ctx, groupID, model)
	if chPricing == nil {
		return
	}

	resolved.Source = PricingSourceChannel
	resolved.channelPricing = chPricing
	resolved.Mode = chPricing.BillingMode
	if resolved.Mode == "" {
		resolved.Mode = BillingModeToken
	}

	switch resolved.Mode {
	case BillingModeToken:
		r.applyTokenOverrides(chPricing, resolved)
	case BillingModePerRequest, BillingModeImage, BillingModeVideo:
		r.applyRequestTierOverrides(chPricing, resolved)
	}
}

// applyTokenOverrides 应用 token 模式的渠道覆盖
func (r *ModelPricingResolver) applyTokenOverrides(chPricing *ChannelModelPricing, resolved *ResolvedPricing) {
	if resolved.BasePricing == nil {
		// Presence validation checks only the dimensions actually consumed.
		resolved.BasePricing = &ModelPricing{TokenPricePresence: map[string]bool{}}
	} else {
		// 防止修改 fallbackPrices 中的共享指针
		cloned := *resolved.BasePricing
		resolved.BasePricing = &cloned
	}

	applyChannelTokenPriceOverrides(resolved.BasePricing, chPricing)
	resolved.BasePricing.FieldSources = maps.Clone(resolved.BasePricing.FieldSources)
	if resolved.BasePricing.FieldSources == nil {
		resolved.BasePricing.FieldSources = map[string]string{}
	}
	for field, value := range map[string]*float64{"input_cost_per_token": chPricing.InputPrice, "output_cost_per_token": chPricing.OutputPrice, "cache_read_input_token_cost": chPricing.CacheReadPrice, "cache_creation_input_token_cost": chPricing.CacheWritePrice, "cache_creation_input_token_cost_above_1hr": chPricing.CacheWrite1hPrice, "input_cost_per_image_token": chPricing.ImageInputPrice, "output_cost_per_image_token": chPricing.ImageOutputPrice} {
		if value != nil {
			resolved.BasePricing.FieldSources[field] = resolved.Source
		}
	}
	if chPricing.CacheWrite1hPrice != nil {
		resolved.SupportsCacheBreakdown = true
		resolved.BasePricing.SupportsCacheBreakdown = true
	}
	for i := range chPricing.Intervals {
		if chPricing.Intervals[i].CacheWrite1hPrice != nil {
			resolved.SupportsCacheBreakdown = true
			resolved.BasePricing.SupportsCacheBreakdown = true
			break
		}
	}
	resolved.BasePricing.FastMultiplier = chPricing.FastMultiplier
	resolved.BasePricing.FlexMultiplier = chPricing.FlexMultiplier
	if chPricing.MaxReasoningEffortMultiplier != nil {
		resolved.BasePricing.MaxReasoningEffortMultiplier = chPricing.MaxReasoningEffortMultiplier
	}
	applyChannelImagePriceOverrides(chPricing, resolved.BasePricing)

	// 区间未命中时回退到上面已经应用渠道覆盖的基础价。
	resolved.Intervals = filterValidIntervals(chPricing.Intervals)
}

// applyChannelImagePriceOverrides preserves catalog prices for unset fields.
// An explicit zero output price remains free; absent catalog image prices keep
// the existing text-price fallback in computeTokenBreakdown.
func applyChannelImagePriceOverrides(chPricing *ChannelModelPricing, pricing *ModelPricing) {
	if chPricing != nil && (chPricing.ImageInputPrice != nil || chPricing.ImageOutputPrice != nil) {
		pricing.ExactTokenRates = nil
	}
	if chPricing == nil || pricing == nil {
		return
	}
	if chPricing.ImageOutputPrice != nil {
		pricing.markTokenPrices("image_output")
		pricing.ImageOutputPricePerToken = *chPricing.ImageOutputPrice
		pricing.ImageOutputPriceExplicit = true
	}
	if chPricing.ImageInputPrice != nil {
		pricing.markTokenPrices("image_input")
		pricing.ImageInputPricePerToken = *chPricing.ImageInputPrice
	}
}

// applyRequestTierOverrides 应用按次/图片模式的渠道覆盖
func (r *ModelPricingResolver) applyRequestTierOverrides(chPricing *ChannelModelPricing, resolved *ResolvedPricing) {
	resolved.RequestTiers = filterValidIntervals(chPricing.Intervals)
	if chPricing.PerRequestPrice != nil {
		resolved.DefaultPerRequestPrice = *chPricing.PerRequestPrice
		resolved.DefaultPerRequestPriceFound = true
	}
}

// filterValidIntervals 过滤掉所有价格字段都为空的无效 interval。
// 前端可能创建了只有 min/max 但无价格的空 interval。
func filterValidIntervals(intervals []PricingInterval) []PricingInterval {
	var valid []PricingInterval
	for _, iv := range intervals {
		if iv.InputPrice != nil || iv.OutputPrice != nil ||
			iv.CacheWritePrice != nil || iv.CacheWrite1hPrice != nil || iv.CacheReadPrice != nil ||
			iv.PerRequestPrice != nil || iv.InputMultiplier != nil ||
			iv.OutputMultiplier != nil || iv.CacheWriteMultiplier != nil ||
			iv.CacheReadMultiplier != nil {
			valid = append(valid, iv)
		}
	}
	return valid
}

// GetIntervalPricing 根据 context token 数获取区间定价。
// 如果有区间列表，找到匹配区间并构造 ModelPricing；否则直接返回 BasePricing。
func (r *ModelPricingResolver) GetIntervalPricing(resolved *ResolvedPricing, totalContextTokens int) *ModelPricing {
	if resolved.channelPricing != nil && resolved.channelPricing.NativePricingSource == QianwenPricingSource && len(resolved.channelPricing.Intervals) > 0 {
		// Native ladders are mandatory input-length prices, independent of the
		// optional long-context surcharge switch. No first-tier/base fallback.
		var match *PricingInterval
		for i := range resolved.Intervals {
			iv := &resolved.Intervals[i]
			if totalContextTokens >= 0 && (totalContextTokens > iv.MinTokens || totalContextTokens == 0 && iv.MinTokens == 0) && (iv.MaxTokens == nil || totalContextTokens <= *iv.MaxTokens) {
				if match != nil {
					return nil
				}
				match = iv
			}
		}
		if match == nil {
			return nil
		}
		return intervalToModelPricing(match, nil, resolved.channelPricing)
	}
	if len(resolved.Intervals) == 0 {
		return resolved.BasePricing
	}

	iv := FindMatchingInterval(resolved.Intervals, totalContextTokens)
	if iv == nil {
		return resolved.BasePricing
	}
	pricing := intervalToModelPricing(iv, resolved.BasePricing, resolved.channelPricing)
	// BasePricing 为 nil（仅配置区间）时拷贝不到该标志，从 resolved 回填，
	// 保证 computeCacheCreationCost 的 5m/1h 分档判断不被区间路径吞掉。
	pricing.SupportsCacheBreakdown = pricing.SupportsCacheBreakdown || resolved.SupportsCacheBreakdown
	return pricing
}

// intervalToModelPricing 将区间定价转换为 ModelPricing
func intervalToModelPricing(iv *PricingInterval, base *ModelPricing, chPricing *ChannelModelPricing) *ModelPricing {
	pricing := &ModelPricing{TokenPricePresence: map[string]bool{}}
	if base != nil {
		*pricing = *base
	}
	pricing.ExactTokenRates = nil
	applyMultiplier := func(value float64, multiplier *float64) float64 {
		if multiplier == nil {
			return value
		}
		return value * *multiplier
	}
	if iv.InputPrice != nil {
		pricing.markTokenPrices("input")
		pricing.InputPricePerTokenPriority = channelTierOverridePrice(pricing.InputPricePerToken, pricing.InputPricePerTokenPriority, *iv.InputPrice)
		pricing.resetPriorityPresence("input_priority", pricing.InputPricePerTokenPriority)
		pricing.InputPricePerToken = *iv.InputPrice
	} else if iv.InputMultiplier != nil {
		pricing.InputPricePerToken = applyMultiplier(pricing.InputPricePerToken, iv.InputMultiplier)
		pricing.InputPricePerTokenPriority = applyMultiplier(pricing.InputPricePerTokenPriority, iv.InputMultiplier)
	}
	if iv.OutputPrice != nil {
		pricing.markTokenPrices("output")
		pricing.OutputPricePerTokenPriority = channelTierOverridePrice(pricing.OutputPricePerToken, pricing.OutputPricePerTokenPriority, *iv.OutputPrice)
		pricing.resetPriorityPresence("output_priority", pricing.OutputPricePerTokenPriority)
		pricing.OutputPricePerToken = *iv.OutputPrice
	} else if iv.OutputMultiplier != nil {
		pricing.OutputPricePerToken = applyMultiplier(pricing.OutputPricePerToken, iv.OutputMultiplier)
		pricing.OutputPricePerTokenPriority = applyMultiplier(pricing.OutputPricePerTokenPriority, iv.OutputMultiplier)
	}
	if iv.CacheWritePrice != nil {
		pricing.markTokenPrices("cache_write")
		pricing.CacheCreationPricePerTokenPriority = channelTierOverridePrice(pricing.CacheCreationPricePerToken, pricing.CacheCreationPricePerTokenPriority, *iv.CacheWritePrice)
		pricing.resetPriorityPresence("cache_write_priority", pricing.CacheCreationPricePerTokenPriority)
		pricing.CacheCreationPricePerToken = *iv.CacheWritePrice
		pricing.CacheCreationPriceExplicit = true
		pricing.CacheCreation5mPrice = *iv.CacheWritePrice
		if iv.CacheWrite1hPrice == nil {
			pricing.CacheCreation1hPrice = *iv.CacheWritePrice
			pricing.markTokenPrices("cache_write_1h")
		}
	} else if iv.CacheWriteMultiplier != nil {
		pricing.CacheCreationPricePerToken = applyMultiplier(pricing.CacheCreationPricePerToken, iv.CacheWriteMultiplier)
		pricing.CacheCreationPricePerTokenPriority = applyMultiplier(pricing.CacheCreationPricePerTokenPriority, iv.CacheWriteMultiplier)
		pricing.CacheCreation5mPrice = applyMultiplier(pricing.CacheCreation5mPrice, iv.CacheWriteMultiplier)
		pricing.CacheCreation1hPrice = applyMultiplier(pricing.CacheCreation1hPrice, iv.CacheWriteMultiplier)
	}
	if iv.CacheWrite1hPrice != nil {
		pricing.markTokenPrices("cache_write_1h")
		pricing.CacheCreation1hPrice = *iv.CacheWrite1hPrice
		pricing.SupportsCacheBreakdown = true
	}
	if iv.CacheReadPrice != nil {
		pricing.markTokenPrices("cache_read")
		pricing.CacheReadPricePerTokenPriority = channelTierOverridePrice(pricing.CacheReadPricePerToken, pricing.CacheReadPricePerTokenPriority, *iv.CacheReadPrice)
		pricing.resetPriorityPresence("cache_read_priority", pricing.CacheReadPricePerTokenPriority)
		pricing.CacheReadPricePerToken = *iv.CacheReadPrice
	} else if iv.CacheReadMultiplier != nil {
		pricing.CacheReadPricePerToken = applyMultiplier(pricing.CacheReadPricePerToken, iv.CacheReadMultiplier)
		pricing.CacheReadPricePerTokenPriority = applyMultiplier(pricing.CacheReadPricePerTokenPriority, iv.CacheReadMultiplier)
	}
	// Intervals inherit channel image overrides, or the catalog when unset.
	applyChannelImagePriceOverrides(chPricing, pricing)
	return pricing
}

// GetRequestTierPrice 根据层级标签获取按次价格
func (r *ModelPricingResolver) GetRequestTierPrice(resolved *ResolvedPricing, tierLabel string) float64 {
	for _, tier := range resolved.RequestTiers {
		if tier.TierLabel == tierLabel && tier.PerRequestPrice != nil {
			return *tier.PerRequestPrice
		}
	}
	return 0
}

// GetRequestTierPriceByContext 根据 context token 数获取按次价格
func (r *ModelPricingResolver) GetRequestTierPriceByContext(resolved *ResolvedPricing, totalContextTokens int) float64 {
	iv := FindMatchingInterval(resolved.RequestTiers, totalContextTokens)
	if iv != nil && iv.PerRequestPrice != nil {
		return *iv.PerRequestPrice
	}
	return 0
}

// LookupRequestTierPrice preserves explicit zero prices.
func (r *ModelPricingResolver) LookupRequestTierPrice(resolved *ResolvedPricing, label string) (float64, bool) {
	for _, tier := range resolved.RequestTiers {
		if tier.TierLabel == label && tier.PerRequestPrice != nil {
			return *tier.PerRequestPrice, true
		}
	}
	return 0, false
}
func (r *ModelPricingResolver) LookupRequestTierPriceByContext(resolved *ResolvedPricing, tokens int) (float64, bool) {
	interval := FindMatchingInterval(resolved.RequestTiers, tokens)
	if interval != nil && interval.PerRequestPrice != nil {
		return *interval.PerRequestPrice, true
	}
	return 0, false
}
