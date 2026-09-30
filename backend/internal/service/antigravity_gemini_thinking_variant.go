package service

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
)

// Bare Gemini requests express thinking separately from the upstream model ID.
// Only current account catalog evidence may resolve a suffixed variant; bundled
// default names never grant availability. Explicit account mappings win.

var geminiThinkingVariantSuffixes = []string{"-low", "-medium", "-high", "-tiered"}

const (
	geminiThinkingBudgetLowMax    = 1024
	geminiThinkingBudgetMediumMax = 8192
)

type geminiThinkingConfigProbe struct {
	GenerationConfig struct {
		ThinkingConfig *struct {
			ThinkingBudget *json.Number `json:"thinkingBudget"`
			ThinkingLevel  string       `json:"thinkingLevel"`
		} `json:"thinkingConfig"`
	} `json:"generationConfig"`
}

// hasGeminiThinkingVariantSuffix 判断模型名是否已经带了思考深度后缀。
func hasGeminiThinkingVariantSuffix(model string) bool {
	for _, suffix := range geminiThinkingVariantSuffixes {
		if strings.HasSuffix(model, suffix) {
			return true
		}
	}
	return false
}

// geminiThinkingLevelFromBody 从 Gemini 原生请求体推导期望的思考档位（low/medium/high）。
// 解析失败或未携带 thinkingConfig 时返回 "high"。
func geminiThinkingLevelFromBody(body []byte) string {
	if len(body) == 0 {
		return "high"
	}
	var probe geminiThinkingConfigProbe
	if err := json.Unmarshal(body, &probe); err != nil || probe.GenerationConfig.ThinkingConfig == nil {
		return "high"
	}
	thinkingConfig := probe.GenerationConfig.ThinkingConfig
	switch strings.ToLower(strings.TrimSpace(thinkingConfig.ThinkingLevel)) {
	case "low":
		return "low"
	case "medium":
		return "medium"
	case "high":
		return "high"
	}
	if thinkingConfig.ThinkingBudget == nil {
		return "high"
	}
	budget, err := thinkingConfig.ThinkingBudget.Float64()
	if err != nil {
		return "high"
	}
	switch {
	case budget < 0:
		return "high" // -1 = 动态思考
	case budget <= geminiThinkingBudgetLowMax:
		return "low" // 含 0（关闭思考）：上游没有"无思考"变体，取最浅档，budget 本身仍原样透传
	case budget <= geminiThinkingBudgetMediumMax:
		return "medium"
	default:
		return "high"
	}
}

// geminiThinkingLevelFromClaudeThinking 用 Claude Messages 协议的 thinking 配置推导档位，
// 阈值与 geminiThinkingLevelFromBody 保持一致，使同一请求无论走 Gemini 原生还是
// Chat Completions / Messages 兼容层都落到同一个上游变体。
func geminiThinkingLevelFromClaudeThinking(thinking *antigravity.ThinkingConfig) string {
	if thinking == nil {
		return "high"
	}
	if strings.EqualFold(strings.TrimSpace(thinking.Type), "disabled") {
		return "low"
	}
	budget := thinking.BudgetTokens
	switch {
	case budget <= 0:
		return "high" // 动态思考 / 未指定预算
	case budget <= geminiThinkingBudgetLowMax:
		return "low"
	case budget <= geminiThinkingBudgetMediumMax:
		return "medium"
	default:
		return "high"
	}
}

// resolveGeminiThinkingVariantForLevel selects only a variant backed by the
// account's current approved catalog. Defaults and wildcard matches alone are
// not evidence that an upstream model exists or that this account can use it.
func resolveGeminiThinkingVariantForLevel(account *Account, requestedModel string, preferred string) (string, bool) {
	if account == nil || account.Platform != PlatformAntigravity {
		return "", false
	}
	model := strings.TrimPrefix(strings.TrimSpace(requestedModel), "models/")
	if !strings.HasPrefix(model, "gemini-") || hasGeminiThinkingVariantSuffix(model) {
		return "", false
	}
	mapping := configuredModelMapping(account)
	if _, explicit := resolveRequestedModelInMapping(mapping, model); explicit {
		return "", false
	}
	snapshot := account.EffectiveUpstreamModelCatalog()
	if snapshot == nil {
		return "", false
	}
	if preferred == "" {
		preferred = "high"
	}
	order := []string{preferred}
	for _, level := range []string{"high", "medium", "low", "tiered"} {
		if level != preferred {
			order = append(order, level)
		}
	}
	for _, level := range order {
		candidate := model + "-" + level
		target := candidate
		mapped, matched := resolveRequestedModelInMapping(mapping, candidate)
		if matched {
			target = strings.TrimSpace(mapped)
		}
		for _, member := range snapshot.EligibleModels {
			if member.ID == target && member.EvidenceKind != "" && catalogEndpointsContain(member.Endpoints, "generate_content") &&
				(matched || configuredSelectionEvidence(account, member) || hasAvailabilityEvidence(account, nil, member) || (len(mapping) == 0 && slices.Contains(snapshot.DiscoveredModels, member.ID))) {
				return target, true
			}
		}
	}
	return "", false
}
