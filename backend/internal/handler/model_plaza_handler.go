package handler

import (
	"log/slog"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// ModelPlazaHandler 处理「模型广场」查询。
//
// 广场路由挂 OptionalJWT 中间件：匿名可访问（除非 require_auth 开启），带 token 则
// 识别用户。可见性规则（橱窗语义，与「可用渠道」的可绑定语义不同）：
//   - 匿名：仅非专属分组（订阅型照常展示）；
//   - 登录：非专属分组 + user_allowed_groups 授权或持有有效订阅的专属分组；
//     非管理员用户开启公开分组限制且有授权集合时，公开分组也需在集合内。
type ModelPlazaHandler struct {
	plazaService   *service.ModelPlazaService
	apiKeyService  *service.APIKeyService
	settingService *service.SettingService
}

// NewModelPlazaHandler 创建模型广场 handler。
func NewModelPlazaHandler(
	plazaService *service.ModelPlazaService,
	apiKeyService *service.APIKeyService,
	settingService *service.SettingService,
) *ModelPlazaHandler {
	return &ModelPlazaHandler{
		plazaService:   plazaService,
		apiKeyService:  apiKeyService,
		settingService: settingService,
	}
}

// modelPlazaTimePricingPeriod 分时倍率时段（配置时区当天 [start, end)）。
type modelPlazaTimePricingPeriod struct {
	StartTime  string  `json:"start_time"`
	EndTime    string  `json:"end_time"`
	Multiplier float64 `json:"multiplier"`
}

// modelPlazaTimePricing 计费会生效的分时倍率（仅倍率 ≠ 1 的时段）。
// WeekdaysOnly 为 true 时时段仅周一至周五生效，周末整天按标准价计费。
type modelPlazaTimePricing struct {
	Timezone     string                        `json:"timezone"`
	WeekdaysOnly bool                          `json:"weekdays_only,omitempty"`
	Periods      []modelPlazaTimePricingPeriod `json:"periods"`
}

// modelPlazaModel 广场模型条目：实收口径展示定价（白名单形态）。
type modelPlazaModel struct {
	Name     string                     `json:"name"`
	Platform string                     `json:"platform"`
	Pricing  *userSupportedModelPricing `json:"pricing"`
	// Capabilities 该模型在当前分组下的有效观测（账号证据聚合）。
	// 只在至少有一项可核实事实时返回；缺失/冲突的字段保持 unknown，不当作零值。
	Capabilities *publicModelMetadata `json:"capabilities,omitempty"`
	// LongContextBasis 多档时的计价基准："whole_request"（整单按档）| "marginal"（仅超出部分）。
	LongContextBasis string `json:"long_context_basis,omitempty"`
	// TimePricing 分时倍率时段，落在时段内的请求整单乘倍率；无分时省略。
	TimePricing *modelPlazaTimePricing `json:"time_pricing,omitempty"`
}

// modelPlazaGroup 广场分组条目（白名单字段）。
type modelPlazaGroup struct {
	ID                 int64    `json:"id"`
	Name               string   `json:"name"`
	Description        string   `json:"description"`
	Platform           string   `json:"platform"`
	SubscriptionType   string   `json:"subscription_type"`
	RateMultiplier     float64  `json:"rate_multiplier"`
	UserRateMultiplier *float64 `json:"user_rate_multiplier,omitempty"`
	PeakRateEnabled    bool     `json:"peak_rate_enabled"`
	PeakStart          string   `json:"peak_start"`
	PeakEnd            string   `json:"peak_end"`
	PeakRateMultiplier float64  `json:"peak_rate_multiplier"`
	IsExclusive        bool     `json:"is_exclusive"`
	// 生图独立倍率：为 true 时图片计费模型的实付倍率取 ImageRateMultiplier，
	// 不取分组/用户专属倍率。
	ImageRateIndependent bool    `json:"image_rate_independent"`
	ImageRateMultiplier  float64 `json:"image_rate_multiplier"`
	VideoRateIndependent bool    `json:"video_rate_independent"`
	VideoRateMultiplier  float64 `json:"video_rate_multiplier"`
	// 分组是否启用长上下文阶梯计费；关闭时模型实付列只展示最低档/基础价。
	LongContextPricingEnabled bool              `json:"long_context_pricing_enabled"`
	Models                    []modelPlazaModel `json:"models"`
}

// modelPlazaResponse 广场页响应。
type modelPlazaResponse struct {
	Description string            `json:"description"`
	Groups      []modelPlazaGroup `json:"groups"`
}

// Get 返回模型广场数据。
// GET /api/v1/model-plaza
func (h *ModelPlazaHandler) Get(c *gin.Context) {
	if h.settingService == nil {
		response.NotFound(c, "Model plaza is not enabled")
		return
	}
	rt := h.settingService.GetModelPlazaRuntime(c.Request.Context())
	if !rt.Enabled {
		response.NotFound(c, "Model plaza is not enabled")
		return
	}

	subject, authed := middleware.GetAuthSubjectFromContext(c)
	if rt.RequireAuth && !authed {
		response.Unauthorized(c, "Authentication required")
		return
	}

	var err error
	// allowedGroups == nil 表示匿名；登录用户恒为非 nil（可能为空集合）。
	var allowedGroups map[int64]struct{}
	var restrictPublicGroups bool
	var userRates map[int64]float64
	if authed {
		allowedGroups, restrictPublicGroups, err = h.apiKeyService.GetUserGroupVisibility(c.Request.Context(), subject.UserID)
		if err != nil {
			// 可见性数据拿不到时不能静默降级成匿名视图（会错漏专属分组），直接报错。
			response.ErrorFrom(c, err)
			return
		}
		userRates, err = h.apiKeyService.GetUserGroupRates(c.Request.Context(), subject.UserID)
		if err != nil {
			// 专属倍率仅是展示增强，失败降级为分组默认倍率。
			slog.Warn("model_plaza_user_rates_failed", "error", err, "user_id", subject.UserID)
			userRates = nil
		}
	}

	visible, err := h.plazaService.ListVisibleGroups(c.Request.Context(), allowedGroups, restrictPublicGroups)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]modelPlazaGroup, 0, len(visible))
	for i := range visible {
		out = append(out, toModelPlazaGroupDTO(&visible[i], userRates))
	}
	response.Success(c, modelPlazaResponse{
		Description: rt.Description,
		Groups:      out,
	})
}

// toModelPlazaGroupDTO 将 service 层广场分组映射为白名单 DTO,并合并用户专属倍率。
func toModelPlazaGroupDTO(g *service.PlazaGroup, userRates map[int64]float64) modelPlazaGroup {
	models := make([]modelPlazaModel, 0, len(g.Models))
	for i := range g.Models {
		m := &g.Models[i]
		models = append(models, modelPlazaModel{
			Name:             m.Name,
			Platform:         m.Platform,
			Pricing:          toUserPricing(m.Pricing),
			Capabilities:     plazaCapabilities(m.Capabilities),
			LongContextBasis: string(m.LongContextBasis),
			TimePricing:      toModelPlazaTimePricing(m.TimePricing),
		})
	}
	dto := modelPlazaGroup{
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
		Models:                    models,
	}
	if rate, ok := userRates[g.ID]; ok {
		dto.UserRateMultiplier = &rate
	}
	return dto
}

// toModelPlazaTimePricing 转换分时倍率；nil 透传（JSON 省略）。
func toModelPlazaTimePricing(p *service.TimePricingSchedule) *modelPlazaTimePricing {
	if p == nil || len(p.Periods) == 0 {
		return nil
	}
	periods := make([]modelPlazaTimePricingPeriod, 0, len(p.Periods))
	for _, period := range p.Periods {
		periods = append(periods, modelPlazaTimePricingPeriod{
			StartTime:  period.StartTime,
			EndTime:    period.EndTime,
			Multiplier: period.Multiplier,
		})
	}
	return &modelPlazaTimePricing{Timezone: p.Timezone, WeekdaysOnly: p.WeekdaysOnly, Periods: periods}
}

// plazaCapabilities 把每个公开模型名下同一次可见性解析得到的账号观测投影为
// 公开白名单。没有任何可核实事实时返回 nil，而不是一份全未知对象：前端看到
// 缺字段就应该显示未提供，而不是把它当成零值或默认能力。
func plazaCapabilities(observations []service.UpstreamModelMetadata) *publicModelMetadata {
	if len(observations) == 0 {
		return nil
	}
	facts := aggregatePublicMetadata(observations)
	if !plazaCapabilityHasEvidence(facts) {
		return nil
	}
	return &facts
}

func plazaCapabilityHasEvidence(facts publicModelMetadata) bool {
	if plazaLimitHasEvidence(facts.InputContext) || plazaLimitHasEvidence(facts.TotalContext) ||
		plazaLimitHasEvidence(facts.UnspecifiedContext) || plazaLimitHasEvidence(facts.MaxOutputTokens) {
		return true
	}
	if plazaBooleanHasEvidence(facts.Reasoning) || plazaBooleanHasEvidence(facts.SupportsVision) ||
		plazaBooleanHasEvidence(facts.SupportsFunctionCalling) || plazaBooleanHasEvidence(facts.SupportsPromptCaching) ||
		plazaBooleanHasEvidence(facts.SupportsPDF) || plazaModalitiesHasEvidence(facts.InputModalities) ||
		plazaModalitiesHasEvidence(facts.OutputModalities) {
		return true
	}
	return facts.EmbeddingDimensions.Guaranteed != nil || len(facts.EmbeddingDimensions.Available) > 0
}

func plazaLimitHasEvidence(limit publicLimit) bool {
	return limit.Guaranteed != nil || limit.ObservedMinimum != nil || len(limit.Available) > 0
}

func plazaBooleanHasEvidence(fact publicFact[bool]) bool {
	return fact.Guaranteed != nil || len(fact.Available) > 0
}

func plazaModalitiesHasEvidence(modalities publicModalities) bool {
	return modalities.Guaranteed != nil || len(modalities.Available) > 0
}
