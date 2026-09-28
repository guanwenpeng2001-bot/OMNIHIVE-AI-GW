//go:build unit

package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func plazaGroups() []service.PlazaGroup {
	return []service.PlazaGroup{
		{ID: 1, Name: "public-standard", Platform: "anthropic", SubscriptionType: "standard", RateMultiplier: 1},
		{ID: 2, Name: "exclusive-a", Platform: "anthropic", IsExclusive: true, RateMultiplier: 0.5},
		{ID: 3, Name: "public-subscription", Platform: "openai", SubscriptionType: "subscription", RateMultiplier: 1},
		{ID: 4, Name: "exclusive-b", Platform: "openai", IsExclusive: true, RateMultiplier: 0.8},
	}
}

func TestFilterPlazaVisibleGroups_AnonymousSeesOnlyNonExclusive(t *testing.T) {
	// 匿名(allowedExclusive == nil):仅非专属分组;订阅型公开分组照常可见(橱窗语义)。
	visible := filterPlazaVisibleGroups(plazaGroups(), nil, false)
	require.Len(t, visible, 2)
	ids := []int64{visible[0].ID, visible[1].ID}
	require.ElementsMatch(t, []int64{1, 3}, ids)
}

func TestFilterPlazaVisibleGroups_AuthedSeesGrantedExclusive(t *testing.T) {
	// 登录:非专属 + 授权的专属;未授权的专属仍不可见。
	allowed := map[int64]struct{}{2: {}}
	visible := filterPlazaVisibleGroups(plazaGroups(), allowed, false)
	require.Len(t, visible, 3)
	ids := make([]int64, 0, len(visible))
	for _, g := range visible {
		ids = append(ids, g.ID)
	}
	require.ElementsMatch(t, []int64{1, 2, 3}, ids)
}

func TestFilterPlazaVisibleGroups_AuthedEmptySetSeesNoExclusive(t *testing.T) {
	// 登录但无任何专属授权(空集合,非 nil):与匿名同样只见非专属,
	// 但语义区分要保持——空集合不能被当作 nil 匿名分支。
	for _, restricted := range []bool{false, true} {
		t.Run(fmt.Sprintf("restricted=%t", restricted), func(t *testing.T) {
			visible := filterPlazaVisibleGroups(plazaGroups(), map[int64]struct{}{}, restricted)
			var ids []int64
			for _, group := range visible {
				ids = append(ids, group.ID)
			}
			require.Equal(t, []int64{1, 3}, ids)
		})
	}
}

func TestFilterPlazaVisibleGroups_RestrictedUserSeesOnlyGrantedPublic(t *testing.T) {
	// 开启公开分组限制后，公开分组也必须落在授权集合内，否则用户会在广场
	// 看到自己实际绑定不了的分组。
	allowed := map[int64]struct{}{1: {}, 2: {}}
	visible := filterPlazaVisibleGroups(plazaGroups(), allowed, true)
	ids := make([]int64, 0, len(visible))
	for _, g := range visible {
		ids = append(ids, g.ID)
	}
	// 3 是未授权的公开分组，受限后不可见；4 是未授权的专属分组，一贯不可见。
	require.ElementsMatch(t, []int64{1, 2}, ids)
}

func TestFilterPlazaVisibleGroups_RestrictionDoesNotAffectAnonymous(t *testing.T) {
	// 匿名没有用户记录，限制标志无从谈起，可见性必须与未受限时一致。
	visible := filterPlazaVisibleGroups(plazaGroups(), nil, true)
	ids := make([]int64, 0, len(visible))
	for _, g := range visible {
		ids = append(ids, g.ID)
	}
	require.ElementsMatch(t, []int64{1, 3}, ids)
}

func TestModelPlazaHandler_NilSettingServiceFailsClosed404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &ModelPlazaHandler{} // settingService == nil → fail-closed
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/model-plaza", nil)

	h.Get(c)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestToModelPlazaGroupDTO_UserRateAndFieldWhitelist(t *testing.T) {
	g := service.PlazaGroup{
		ID: 2, Name: "vip", Description: "d", Platform: "anthropic",
		SubscriptionType: "standard", RateMultiplier: 1, IsExclusive: true,
		VideoRateIndependent: true, VideoRateMultiplier: 0.7,
		Models: []service.PlazaModel{{
			Name:     "claude-sonnet",
			Platform: "anthropic",
			Pricing: &service.ChannelModelPricing{
				BillingMode: service.BillingModeToken,
				InputPrice:  testPtr(3e-6),
			},
		}},
	}

	// 有专属倍率:user_rate_multiplier 序列化输出
	dto := toModelPlazaGroupDTO(&g, map[int64]float64{2: 0.5})
	raw, err := json.Marshal(dto)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	for _, key := range []string{
		"id", "name", "description", "platform", "subscription_type",
		"rate_multiplier", "user_rate_multiplier", "is_exclusive", "models",
		"peak_rate_enabled", "peak_start", "peak_end", "peak_rate_multiplier",
		"image_rate_independent", "image_rate_multiplier", "long_context_pricing_enabled",
		"video_rate_independent", "video_rate_multiplier",
	} {
		_, exists := decoded[key]
		require.Truef(t, exists, "plaza group DTO must expose %q", key)
	}
	require.InDelta(t, 0.5, decoded["user_rate_multiplier"].(float64), 1e-9)
	require.Equal(t, true, decoded["video_rate_independent"])
	require.InDelta(t, 0.7, decoded["video_rate_multiplier"].(float64), 1e-9)

	// 模型条目保留渠道定价，已退役的参考价字段不再输出。
	models := decoded["models"].([]any)
	require.Len(t, models, 1)
	model := models[0].(map[string]any)
	require.Contains(t, model, "pricing")
	require.NotContains(t, model, "official_pricing")
	_, hasBasis := model["long_context_basis"]
	require.False(t, hasBasis, "单档模型不输出 long_context_basis")
	_, hasTimePricing := model["time_pricing"]
	require.False(t, hasTimePricing, "无分时时不输出 time_pricing")

	// 无专属倍率:user_rate_multiplier 整个字段省略
	dtoNoRate := toModelPlazaGroupDTO(&g, nil)
	rawNoRate, err := json.Marshal(dtoNoRate)
	require.NoError(t, err)
	var decodedNoRate map[string]any
	require.NoError(t, json.Unmarshal(rawNoRate, &decodedNoRate))
	_, hasRate := decodedNoRate["user_rate_multiplier"]
	require.False(t, hasRate, "无专属倍率时 user_rate_multiplier 应 omitempty")
}

// 广场只展示同一次可见性解析得到的、可核实的规格：没有依据就不返回
// capabilities（前端保持未提供），有观测冲突的字段保持 unknown 而不是零值。
func TestPlazaCapabilitiesOnlyProjectsSourcedFacts(t *testing.T) {
	vision := true
	pdfMissing := (*bool)(nil)

	single := plazaCapabilities([]service.UpstreamModelMetadata{{
		ContextLimitKind:    "input",
		ContextWindow:       200000,
		MaxOutputTokens:     64000,
		SupportsVision:      &vision,
		SupportsPDF:         pdfMissing,
		InputModalities:     []string{"text", "image"},
		OutputModalities:    []string{"text"},
		EmbeddingDimensions: 0,
	}})
	require.NotNil(t, single)
	require.NotNil(t, single.InputContext.Guaranteed)
	require.EqualValues(t, 200000, *single.InputContext.Guaranteed)
	require.NotNil(t, single.SupportsVision.Guaranteed)
	require.True(t, *single.SupportsVision.Guaranteed)
	require.True(t, single.SupportsPDF.Unknown, "缺少观测的字段必须保持 unknown")
	require.Nil(t, single.SupportsPDF.Guaranteed)
	require.True(t, single.TotalContext.Unknown, "没有 total 观测时该维度保持 unknown")
	require.Nil(t, single.TotalContext.Guaranteed)

	// 多个账号给出不同上限时，同量纲上限取最小值作为保证值，并同时给出观测最小值；
	// 不会把冲突当成“未知零”或取最大值冒充能力。
	conflict := plazaCapabilities([]service.UpstreamModelMetadata{
		{ContextLimitKind: "input", ContextWindow: 200000},
		{ContextLimitKind: "input", ContextWindow: 100000},
	})
	require.NotNil(t, conflict)
	require.NotNil(t, conflict.InputContext.Guaranteed)
	require.EqualValues(t, 100000, *conflict.InputContext.Guaranteed)
	require.NotNil(t, conflict.InputContext.ObservedMinimum)
	require.EqualValues(t, 100000, *conflict.InputContext.ObservedMinimum)
	require.Len(t, conflict.InputContext.Available, 2)

	// 没有任何观测或观测里没有任何事实 → 不返回 capabilities。
	require.Nil(t, plazaCapabilities(nil))
	require.Nil(t, plazaCapabilities([]service.UpstreamModelMetadata{{}}))
}

func TestToModelPlazaGroupDTO_LongContextTiersAndBasis(t *testing.T) {
	maxTokens := 272000
	g := service.PlazaGroup{
		ID: 3, Name: "ladder", Platform: "openai", SubscriptionType: "standard", RateMultiplier: 1,
		LongContextPricingEnabled: true,
		Models: []service.PlazaModel{{
			Name:     "gpt-5.4",
			Platform: "openai",
			Pricing: &service.ChannelModelPricing{
				BillingMode: service.BillingModeToken,
				InputPrice:  testPtr(2.5e-6),
				Intervals: []service.PricingInterval{
					{MinTokens: 0, MaxTokens: &maxTokens, TierLabel: "≤272K", InputPrice: testPtr(2.5e-6)},
					{MinTokens: 272000, TierLabel: ">272K", InputPrice: testPtr(5e-6)},
				},
			},
			LongContextBasis: service.ContextPricingBasisWholeRequest,
		}},
	}

	raw, err := json.Marshal(toModelPlazaGroupDTO(&g, nil))
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Equal(t, true, decoded["long_context_pricing_enabled"])

	model := decoded["models"].([]any)[0].(map[string]any)
	require.Equal(t, "whole_request", model["long_context_basis"])

	pricing := model["pricing"].(map[string]any)
	paidTiers := pricing["intervals"].([]any)
	require.Len(t, paidTiers, 2)
	require.Equal(t, ">272K", paidTiers[1].(map[string]any)["tier_label"])

	require.NotContains(t, model, "official_pricing")
}

func testPtr(v float64) *float64 { return &v }

func TestToModelPlazaGroupDTO_TimePricing(t *testing.T) {
	g := service.PlazaGroup{
		ID: 4, Name: "cn", Platform: "deepseek", SubscriptionType: "standard", RateMultiplier: 1,
		Models: []service.PlazaModel{{
			Name:     "deepseek-chat",
			Platform: "deepseek",
			Pricing:  &service.ChannelModelPricing{BillingMode: service.BillingModeToken, InputPrice: testPtr(0.28e-6)},
			TimePricing: &service.TimePricingSchedule{Timezone: "Asia/Shanghai", Periods: []service.TimePricingPeriod{
				{StartTime: "00:30", EndTime: "08:30", Multiplier: 0.5},
			}},
		}, {
			Name:     "deepseek-reasoner",
			Platform: "deepseek",
			Pricing:  &service.ChannelModelPricing{BillingMode: service.BillingModeToken, InputPrice: testPtr(0.56e-6)},
			TimePricing: &service.TimePricingSchedule{Timezone: "Asia/Shanghai", WeekdaysOnly: true, Periods: []service.TimePricingPeriod{
				{StartTime: "00:30", EndTime: "08:30", Multiplier: 0.5},
			}},
		}},
	}
	raw, err := json.Marshal(toModelPlazaGroupDTO(&g, nil))
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	model := decoded["models"].([]any)[0].(map[string]any)
	tp := model["time_pricing"].(map[string]any)
	require.Equal(t, "Asia/Shanghai", tp["timezone"])
	_, hasWeekdaysOnly := tp["weekdays_only"]
	require.False(t, hasWeekdaysOnly, "未开启仅工作日时字段省略")
	periods := tp["periods"].([]any)
	require.Len(t, periods, 1)
	first := periods[0].(map[string]any)
	require.Equal(t, "00:30", first["start_time"])
	require.Equal(t, "08:30", first["end_time"])
	require.InDelta(t, 0.5, first["multiplier"].(float64), 1e-12)

	weekdaysModel := decoded["models"].([]any)[1].(map[string]any)
	weekdaysTP := weekdaysModel["time_pricing"].(map[string]any)
	require.Equal(t, true, weekdaysTP["weekdays_only"])
}

func TestFilterPlazaVisibleGroups_SubscribedExclusiveGroup(t *testing.T) {
	groups := []service.PlazaGroup{
		{ID: 42, IsExclusive: true, SubscriptionType: "subscription"},
		{ID: 43, IsExclusive: true, SubscriptionType: "subscription"},
		{ID: 44, IsExclusive: true, SubscriptionType: "standard"},
	}
	require.Empty(t, filterPlazaVisibleGroups(groups, nil, false))
	for _, restricted := range []bool{false, true} {
		visible := filterPlazaVisibleGroups(groups, map[int64]struct{}{42: {}}, restricted)
		require.Len(t, visible, 1)
		require.Equal(t, int64(42), visible[0].ID)
	}
}
