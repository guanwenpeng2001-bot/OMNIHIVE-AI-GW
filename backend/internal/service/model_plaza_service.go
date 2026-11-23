package service

import (
	"context"
	"sort"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
)

// PlazaModel 模型广场中单个模型条目：按实收口径合成的展示定价。
type PlazaModel struct {
	Name     string
	Platform string
	// CatalogOnly is inventory, never an inference or billing authorization.
	CatalogOnly bool `json:"-"`
	Pricing     *ChannelModelPricing
	// LongContextBasis 多档时的计价基准（整单 / 仅超出部分），单档为空。
	LongContextBasis ContextPricingBasis
	// TimePricing 计费会生效的分时倍率时段；无分时为 nil。
	TimePricing *TimePricingSchedule
	// Capabilities 该分组下这个公开模型的实际观测规格（可能为空 = 无依据）。
	// 内部字段，只给 handler 做公开白名单投影；不参与可见集合判断，也不对外序列化。
	Capabilities    []UpstreamModelMetadata
	CatalogBindings []PlazaCatalogBinding `json:"-"`
	// Observations describe scoped discoveries without an executable binding.
	CatalogObservations []PlazaCatalogBinding `json:"-"`
	CatalogOffers       []PlazaCatalogOffer   `json:"-"`
	Discovered          bool                  `json:"-"`
	EligibilityUnknown  bool                  `json:"-"`
}

type PlazaCatalogOffer struct {
	ChannelID          int64
	PublicationBasis   string
	BillingModelSource string
	ChannelMappedModel string
	Bindings           []PlazaCatalogBinding
	Eligible           bool
	configuredPrice    *ChannelModelPricing
}

// PlazaGroup 模型广场中以分组为顶层的条目。
//
// 与 AvailableGroupRef 相比多了 Description 与 Models；Models 来自该分组关联渠道的
// 支持模型（普通分组按分组平台隔离，Composite 分组展开关联渠道已配置的
// 具体平台），与「可用渠道」页口径一致。
type PlazaGroup struct {
	ID                 int64
	Name               string
	Description        string
	Platform           string
	SubscriptionType   string
	RateMultiplier     float64
	PeakRateEnabled    bool
	PeakStart          string
	PeakEnd            string
	PeakRateMultiplier float64
	IsExclusive        bool
	// 图片按次实付倍率：ImageRateIndependent 为 true 时，图片计费模型的实付
	// = 档位价 × ImageRateMultiplier，不乘分组/用户专属倍率（与计费口径一致）。
	ImageRateIndependent bool
	ImageRateMultiplier  float64
	// 视频独立倍率与图片独立倍率分别配置，开启时覆盖分组/用户专属倍率。
	VideoRateIndependent bool
	VideoRateMultiplier  float64
	// LongContextPricingEnabled 分组是否按上下文长度应用阶梯价；关闭时模型展示的是最低档。
	LongContextPricingEnabled bool
	Models                    []PlazaModel
	catalogGroup              *Group
}

// PricingGroup returns the hydrated group from this catalog read.
func (g PlazaGroup) PricingGroup() *Group { return g.catalogGroup }

// ModelPlazaService 聚合模型广场数据。
//
// 模型枚举来自渠道配置；token 模型的展示单价与阶梯由 BillingService 的阶梯表
// 查询给出（与扣费走同一条解析链与计费函数），图片/按次模型沿用渠道/分组档位价。
type ModelPlazaService struct {
	channelRepo    ChannelRepository
	groupRepo      GroupRepository
	pricingService *PricingService
	billingService *BillingService
	resolver       *ModelPricingResolver
}

// NewModelPlazaService 创建模型广场服务。
func NewModelPlazaService(
	channelRepo ChannelRepository,
	groupRepo GroupRepository,
	pricingService *PricingService,
	billingService *BillingService,
	resolver *ModelPricingResolver,
) *ModelPlazaService {
	return &ModelPlazaService{
		channelRepo:    channelRepo,
		groupRepo:      groupRepo,
		pricingService: pricingService,
		billingService: billingService,
		resolver:       resolver,
	}
}

// ListGroups 返回模型广场数据：每个活跃分组附带其可用模型与定价。
//
// 模型枚举口径与 ListAvailable 一致（Active 渠道、SupportedModels ∪ 全局定价回落、
// 平台隔离），仅把顶层从渠道换成分组：
//   - 渠道按 lower(name) 排序后遍历，保证同名模型去重结果确定；
//   - 同分组同名模型「先见者胜」，仅当已存条目无定价而新条目有定价时升级替换；
//   - token 模型的单价与阶梯按实收口径合成（见 ResolveContextPricingSchedule），
//     图片计费模型的档位价按实收口径合成（见 plazaImageDisplayPricing）；
//   - 不读取或返回目录参考价；
//   - 只返回 Models 非空的分组；分组按 RateMultiplier 升序（同倍率按名称），
//     组内模型按名称排序。
//
// 可见性过滤（专属分组）不在此层做，由 handler 按登录态裁剪。
func (s *ModelPlazaService) ListGroups(ctx context.Context) ([]PlazaGroup, error) {
	return s.listGroups(ctx, nil, nil, false)
}

// ListVisibleGroups applies the freshly read user's group visibility before
// loading accounts or computing prices. nil allowedGroups denotes anonymous.
func (service *ModelPlazaService) ListVisibleGroups(ctx context.Context, allowedGroups map[int64]struct{}, restrictPublicGroups bool) ([]PlazaGroup, error) {
	return service.listGroups(ctx, plazaGroupVisibility(allowedGroups, restrictPublicGroups), nil, false)
}

func plazaGroupVisibility(allowedGroups map[int64]struct{}, restrictPublicGroups bool) func(Group) bool {
	return func(group Group) bool {
		if group.IsExclusive || (restrictPublicGroups && len(allowedGroups) > 0) {
			_, allowed := allowedGroups[group.ID]
			return allowed
		}
		return true
	}
}

type PlazaCatalogSelection struct {
	Groups, Channels  map[int64]bool
	Accounts          map[int64]domain.ActionIdentity
	IncludeHistorical bool
	SelectedKey       bool
}

type PlazaCatalogBinding struct {
	AccountModelBinding
	Identity       domain.ActionIdentity
	Metadata       UpstreamModelMetadata
	DiscoveryState string
	AccountLabel   string
}

func (s *ModelPlazaService) ListProjectCatalogGroups(ctx context.Context, allowed map[int64]struct{}, restrict bool, selection PlazaCatalogSelection) ([]PlazaGroup, error) {
	visible := plazaGroupVisibility(allowed, restrict)
	return s.listGroups(ctx, func(g Group) bool { return selection.Groups[g.ID] && visible(g) }, &selection, true)
}

func (s *ModelPlazaService) catalogResolver() *VisibleModelResolver {
	if s.resolver != nil && s.resolver.channelService != nil {
		return s.resolver.channelService.visibleModels
	}
	return nil
}

func (s *ModelPlazaService) listGroups(ctx context.Context, permitted func(Group) bool, selection *PlazaCatalogSelection, fullCatalog bool) ([]PlazaGroup, error) {
	visibility := s.catalogResolver()
	ctx, panels, err := acquireCatalogPanels(ctx, visibility, s.channelRepo, s.groupRepo, permitted, selection)
	if err != nil {
		return nil, err
	}
	channels, groups := panels.channels, panels.groups
	visible, observations, bindings := panels.visible, panels.observations, panels.bindings

	sort.SliceStable(channels, func(i, j int) bool {
		return strings.ToLower(channels[i].Name) < strings.ToLower(channels[j].Name)
	})

	byGroup := make(map[int64]*PlazaGroup, len(groups))
	groupEnt := make(map[int64]*Group, len(groups))
	order := make([]int64, 0, len(groups))
	for i := range groups {
		g := &groups[i]
		byGroup[g.ID] = &PlazaGroup{
			ID:                        g.ID,
			Name:                      g.Name,
			Description:               g.Description,
			Platform:                  g.Platform,
			SubscriptionType:          g.SubscriptionType,
			RateMultiplier:            g.RateMultiplier,
			PeakRateEnabled:           g.PeakRateEnabled,
			PeakStart:                 g.PeakStart,
			PeakEnd:                   g.PeakEnd,
			PeakRateMultiplier:        g.PeakRateMultiplier,
			IsExclusive:               g.IsExclusive,
			ImageRateIndependent:      g.ImageRateIndependent,
			ImageRateMultiplier:       g.ImageRateMultiplier,
			VideoRateIndependent:      g.VideoRateIndependent,
			VideoRateMultiplier:       g.VideoRateMultiplier,
			LongContextPricingEnabled: g.LongContextPricingEnabled,
		}
		if fullCatalog {
			byGroup[g.ID].catalogGroup = g
		}
		groupEnt[g.ID] = g
		order = append(order, g.ID)
	}

	// modelIdx[groupID][platform+modelName] = index into byGroup[groupID].Models
	modelIdx := make(map[int64]map[catalog.ModelIdentity]int, len(groups))
	for i := range channels {
		ch := &channels[i]
		if ch.Status != StatusActive || (selection != nil && !selection.Channels[ch.ID]) {
			continue
		}
		ch.normalizeBillingModelSource()

		for _, gid := range ch.GroupIDs {
			pg, ok := byGroup[gid]
			if !ok {
				continue
			}
			idx := modelIdx[gid]
			if idx == nil {
				idx = make(map[catalog.ModelIdentity]int)
				modelIdx[gid] = idx
			}
			supported := PlazaVisibleModels(ch, gid)
			var configured map[catalog.ModelIdentity]*ChannelModelPricing
			if fullCatalog {
				configured = make(map[catalog.ModelIdentity]*ChannelModelPricing, len(supported))
				for _, model := range supported {
					configured[catalog.NewModelIdentity(model.Platform, model.Name)] = model.Pricing
				}
			}
			fillGlobalPricingFallback(s.pricingService, supported)
			for j := range supported {
				m := supported[j]
				if !panelModelVisible(ctx, groupEnt[gid], m.Name, visible) {
					continue
				}
				if pg.Platform == PlatformComposite {
					if !isConcreteRequestPlatform(m.Platform) {
						continue
					}
				} else {
					group := groupEnt[gid]
					if domain.CanonicalPlatform(m.Platform) != domain.CanonicalPlatform(group.Platform) {
						continue
					}
					// Product-only channel entries need a real account binding to
					// prove a hosted subtype. Without account visibility, retain
					// the restriction encoded by the channel's platform identity.
					if visibility == nil && !domain.MatchesPlatformIdentity(m.Platform, domain.HostedAccountType(m.Platform), group.Platform, group.RequiredAccountType()) {
						continue
					}
				}
				var catalogBindings []PlazaCatalogBinding
				if visibility != nil {
					for _, binding := range bindings[gid][m.Name] {
						if domain.MatchesPlatformIdentity(binding.Identity.Platform, binding.Identity.HostedAccountType, m.Platform, domain.HostedAccountType(m.Platform)) && (pg.Platform == PlatformComposite || domain.MatchesPlatformIdentity(binding.Identity.Platform, binding.Identity.HostedAccountType, groupEnt[gid].Platform, groupEnt[gid].RequiredAccountType())) {
							catalogBindings = append(catalogBindings, binding)
						}
					}
					if len(catalogBindings) == 0 {
						continue
					}
				}
				capabilities := observations[gid][m.Name]
				if visibility != nil {
					capabilities = nil
					for _, binding := range catalogBindings {
						capabilities = append(capabilities, binding.Metadata)
					}
				}
				key := catalog.NewModelIdentity(m.Platform, m.Name)
				var catalogOffers []PlazaCatalogOffer
				if fullCatalog {
					offer := plazaOffer(ch, m, catalogBindings, true)
					offer.configuredPrice = configured[key]
					if s.resolver != nil && s.resolver.channelService != nil {
						mapping := s.resolver.channelService.ResolveChannelMapping(WithResolvedTargetPlatform(ctx, m.Platform), gid, m.Name)
						if mapping.ChannelID == ch.ID {
							offer.ChannelMappedModel = mapping.MappedModel
							offer.BillingModelSource = mapping.BillingModelSource
						}
					}
					catalogOffers = []PlazaCatalogOffer{offer}
				}
				if at, seen := idx[key]; seen {
					pg.Models[at].CatalogOffers = append(pg.Models[at].CatalogOffers, catalogOffers...)
					// 先见者胜；仅当已存条目无定价而新条目有定价时升级。
					if pg.Models[at].Pricing == nil && m.Pricing != nil {
						pg.Models[at].Pricing = m.Pricing
					}
					continue
				}
				idx[key] = len(pg.Models)
				pg.Models = append(pg.Models, PlazaModel{
					Name:            m.Name,
					Platform:        m.Platform,
					Pricing:         m.Pricing,
					Capabilities:    capabilities,
					CatalogBindings: catalogBindings,
					CatalogOffers:   catalogOffers,
				})
			}
		}
	}

	if fullCatalog {
		if err := s.expandConfiguredCatalog(ctx, groups, channels, byGroup, selection, visibility); err != nil {
			return nil, err
		}
	}

	out := make([]PlazaGroup, 0, len(order))
	for _, gid := range order {
		pg := byGroup[gid]
		if len(pg.Models) == 0 {
			continue
		}
		sort.SliceStable(pg.Models, func(i, j int) bool {
			if pg.Models[i].Name != pg.Models[j].Name {
				return pg.Models[i].Name < pg.Models[j].Name
			}
			return pg.Models[i].Platform < pg.Models[j].Platform
		})
		g := groupEnt[gid]
		for j := range pg.Models {
			if !pg.Models[j].CatalogOnly {
				s.fillDisplayPricing(ctx, &pg.Models[j], g)
			}
			pg.Models[j].EligibilityUnknown = panels.incompleteGroups[gid] && pg.Models[j].CatalogOnly
		}
		out = append(out, *pg)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RateMultiplier != out[j].RateMultiplier {
			return out[i].RateMultiplier < out[j].RateMultiplier
		}
		return out[i].Name < out[j].Name
	})
	if err := visibility.validate(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// plazaVisibleModelsForGroups mirrors visibleModelsForGroups but also keeps the
// per-model account observations from the same resolution pass. The plaza shows
// exactly the evidence the gateway list uses and never performs a second read.
func plazaVisibleModelsForGroups(ctx context.Context, resolver *VisibleModelResolver, panels *catalogPanels, selection *PlazaCatalogSelection) (context.Context, error) {
	if resolver == nil {
		return ctx, nil
	}
	frozen, err := resolver.preloadCatalogGroups(ctx, panels.groups)
	if err != nil {
		return ctx, err
	}
	panels.visible = make(map[int64]map[string]bool)
	panels.observations = make(map[int64]map[string][]UpstreamModelMetadata)
	panels.bindings = make(map[int64]map[string][]PlazaCatalogBinding)
	panels.incompleteGroups = make(map[int64]bool)
	for _, group := range panels.groups {
		metadata := make(map[string][]UpstreamModelMetadata)
		view, err := resolver.resolve(frozen, &group.ID, group.Platform, metadata)
		if err != nil {
			return ctx, err
		}
		members := make(map[string]bool, len(view.Models))
		for _, model := range view.Models {
			members[model] = true
		}
		panels.visible[group.ID] = members
		panels.observations[group.ID] = metadata
		panels.incompleteGroups[group.ID] = !view.FullyEnumerable
		identities := map[int64]domain.ActionIdentity{}
		if batch, ok := frozen.Value(catalogGroupSnapshotKey{}).(*catalogGroupSnapshot); ok {
			for _, account := range batch.accounts[group.ID] {
				identities[account.ID] = domain.ActionIdentity{Platform: account.Platform, HostedAccountType: account.Type}
			}
		}
		groupBindings := make(map[string][]PlazaCatalogBinding)
		for model, resolved := range view.Bindings {
			for i, binding := range resolved {
				identity, allowed := identities[binding.AccountID]
				if !allowed && selection == nil {
					if reader, ok := resolver.gateway.accountRepo.(ModelCatalogAccountRepository); ok {
						account, readErr := reader.GetModelCatalogAccount(frozen, binding.AccountID)
						if readErr != nil {
							return ctx, readErr
						}
						if account != nil {
							identity = domain.ActionIdentity{Platform: account.Platform, HostedAccountType: account.Type}
							allowed = true
							identities[binding.AccountID] = identity
						}
					}
				}
				if selection != nil {
					identity, allowed = selection.Accounts[binding.AccountID]
				}
				if !allowed {
					continue
				}
				if selection != nil && binding.CredentialAccountID != 0 && binding.CredentialAccountID != binding.AccountID {
					if _, allowed := selection.Accounts[binding.CredentialAccountID]; !allowed {
						continue
					}
				}
				groupBindings[model] = append(groupBindings[model], PlazaCatalogBinding{AccountModelBinding: binding, Identity: identity, Metadata: metadata[model][i]})
			}
		}
		panels.bindings[group.ID] = groupBindings
	}
	return frozen, nil
}

// fillDisplayPricing 把模型的展示定价换成实收口径：
// token 模型取计费阶梯表（单价与档位均由真实计费函数得出），
// 图片/按次模型（或阶梯表不可用时）沿用渠道定价与分组图片档位价。
func (s *ModelPlazaService) fillDisplayPricing(ctx context.Context, m *PlazaModel, g *Group) {
	if s.billingService != nil && s.resolver != nil {
		sched, err := s.billingService.ResolveContextPricingSchedule(ctx, s.resolver, ContextPricingScheduleInput{
			Model:    m.Name,
			Group:    g,
			Platform: m.Platform,
		})
		if err == nil && sched != nil && len(sched.Tiers) > 0 {
			m.Pricing = withDefaultMaxReasoningEffortMultiplier(plazaPricingFromSchedule(m.Pricing, sched), m.Name)
			if len(sched.Tiers) > 1 {
				m.LongContextBasis = sched.Basis
			}
			m.TimePricing = sched.TimePricing
			return
		}
	}
	m.Pricing = withDefaultMaxReasoningEffortMultiplier(plazaImageDisplayPricing(m.Pricing, g), m.Name)
}

// DescribePlazaCatalogPrice adapts the existing resolver to a resolved billing
// identity while retaining the legacy plaza's display schedule format.
func (s *ModelPlazaService) DescribePlazaCatalogPrice(ctx context.Context, model PlazaModel, group PlazaGroup, offer PlazaCatalogOffer, billing catalog.ModelIdentity) PlazaModel {
	g := group.catalogGroup
	if g == nil {
		return model
	}
	prices := s.pricingService
	if s.billingService != nil {
		prices = s.billingService.forGatewayRequest(ctx).pricingService
	}
	if prices != nil {
		base := prices.GetModelPricing(billing.Model)
		if base != nil && base.LiteLLMProvider != "" &&
			catalog.NewModelIdentity(base.LiteLLMProvider, billing.Model) != billing &&
			matchGroupModelPricing(g, billing.Model, billing.Provider) == nil && pricingNeedsFallback(offer.configuredPrice) {
			model.Pricing, model.TimePricing, model.LongContextBasis = nil, nil, ""
			return model
		}
	}
	if model.Name == billing.Model && model.Platform == billing.Provider {
		return model
	}
	model.Name, model.Platform, model.Pricing = billing.Model, billing.Provider, offer.configuredPrice
	model.LongContextBasis, model.TimePricing = "", nil
	s.fillDisplayPricing(ctx, &model, g)
	return model
}

func withDefaultMaxReasoningEffortMultiplier(pricing *ChannelModelPricing, model string) *ChannelModelPricing {
	if pricing == nil || pricing.MaxReasoningEffortMultiplier != nil {
		return pricing
	}
	multiplier := defaultMaxReasoningEffortMultiplier(model)
	if multiplier == nil {
		return pricing
	}
	cloned := pricing.Clone()
	cloned.MaxReasoningEffortMultiplier = multiplier
	return &cloned
}

// plazaPricingFromSchedule 把阶梯表压成展示用的 ChannelModelPricing：
// 平价取首档单价，多档时 Intervals 逐档给出绝对单价；图片/按次字段沿用原始定价。
func plazaPricingFromSchedule(raw *ChannelModelPricing, sched *ContextPricingSchedule) *ChannelModelPricing {
	out := &ChannelModelPricing{BillingMode: BillingModeToken}
	if raw != nil {
		out.ImageInputPrice = raw.ImageInputPrice
		out.ImageOutputPrice = raw.ImageOutputPrice
		out.PerRequestPrice = raw.PerRequestPrice
		out.MaxReasoningEffortMultiplier = raw.MaxReasoningEffortMultiplier
	}
	first := sched.Tiers[0]
	out.InputPrice = first.Input
	out.OutputPrice = first.Output
	out.CacheWritePrice = first.CacheWrite
	out.CacheWrite1hPrice = first.CacheWrite1h
	out.CacheReadPrice = first.CacheRead
	if len(sched.Tiers) > 1 {
		out.Intervals = plazaIntervalsFromTiers(sched.Tiers)
	}
	return out
}

func plazaIntervalsFromTiers(tiers []ContextPricingTier) []PricingInterval {
	intervals := make([]PricingInterval, 0, len(tiers))
	for i, t := range tiers {
		intervals = append(intervals, PricingInterval{
			MinTokens:         t.MinTokens,
			MaxTokens:         t.MaxTokens,
			TierLabel:         t.Label,
			InputPrice:        t.Input,
			OutputPrice:       t.Output,
			CacheWritePrice:   t.CacheWrite,
			CacheWrite1hPrice: t.CacheWrite1h,
			CacheReadPrice:    t.CacheRead,
			SortOrder:         i,
		})
	}
	return intervals
}

// plazaImageDisplayPricing 为图片计费模型合成展示定价，使档位价与实收口径一致：
// 每档（1K/2K/4K）单价 = 分组图片价 > 渠道同档位价 > 渠道默认按次价，无价的档不展示。
// 分组未配任何图片价、或定价非图片模式时原样返回。返回克隆，不修改入参
// （渠道定价指针指向缓存共享数据）。
func plazaImageDisplayPricing(p *ChannelModelPricing, g *Group) *ChannelModelPricing {
	if p == nil || g == nil || p.BillingMode != BillingModeImage {
		return p
	}
	if g.ImagePrice1K == nil && g.ImagePrice2K == nil && g.ImagePrice4K == nil {
		return p
	}
	channelTierPrice := func(label string) *float64 {
		for i := range p.Intervals {
			if p.Intervals[i].TierLabel == label && p.Intervals[i].PerRequestPrice != nil {
				return p.Intervals[i].PerRequestPrice
			}
		}
		return p.PerRequestPrice
	}
	tiers := []struct {
		label      string
		groupPrice *float64
	}{
		{"1K", g.ImagePrice1K},
		{"2K", g.ImagePrice2K},
		{"4K", g.ImagePrice4K},
	}
	clone := *p
	clone.Intervals = make([]PricingInterval, 0, len(tiers))
	for i, t := range tiers {
		price := t.groupPrice
		if price == nil {
			price = channelTierPrice(t.label)
		}
		if price == nil {
			continue
		}
		v := *price
		clone.Intervals = append(clone.Intervals, PricingInterval{
			TierLabel:       t.label,
			PerRequestPrice: &v,
			SortOrder:       i,
		})
	}
	return &clone
}
