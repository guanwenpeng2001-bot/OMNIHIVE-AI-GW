package service

import (
	"context"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

const (
	modelRateLimitsKey                = "model_rate_limits"
	openAIImageGenerationRateLimitKey = "openai:image_generation"
	openAICodexSparkRateLimitReason   = "openai_codex_spark_rate_limit"
	// anthropicFableRateLimitKey 是 Anthropic 7d_oi（Fable 专属 7d 窗口）限流的
	// 家族级 scope：命中后所有 Fable 变体（含 [1m] 等后缀）都不再调度到该账号。
	anthropicFableRateLimitKey = "claude-fable-5"
)

// isRateLimitActiveForKey 检查指定 key 的限流是否生效
func (a *Account) isRateLimitActiveForKey(key string) bool {
	resetAt := a.modelRateLimitResetAt(key)
	return resetAt != nil && time.Now().Before(*resetAt)
}

// getRateLimitRemainingForKey 获取指定 key 的限流剩余时间，0 表示未限流或已过期
func (a *Account) getRateLimitRemainingForKey(key string) time.Duration {
	resetAt := a.modelRateLimitResetAt(key)
	if resetAt == nil {
		return 0
	}
	remaining := time.Until(*resetAt)
	if remaining > 0 {
		return remaining
	}
	return 0
}

func (a *Account) isModelRateLimitedWithContext(ctx context.Context, requestedModel string) bool {
	for _, key := range a.modelRateLimitKeysForRequest(ctx, requestedModel) {
		if a.isRateLimitActiveForKey(key) {
			return true
		}
	}
	return false
}

// GetModelRateLimitRemainingTime 获取模型限流剩余时间
// 返回 0 表示未限流或已过期
func (a *Account) GetModelRateLimitRemainingTime(requestedModel string) time.Duration {
	return a.GetModelRateLimitRemainingTimeWithContext(context.Background(), requestedModel)
}

func (a *Account) GetModelRateLimitRemainingTimeWithContext(ctx context.Context, requestedModel string) time.Duration {
	remaining := time.Duration(0)
	for _, key := range a.modelRateLimitKeysForRequest(ctx, requestedModel) {
		if keyRemaining := a.getRateLimitRemainingForKey(key); keyRemaining > remaining {
			remaining = keyRemaining
		}
	}
	return remaining
}

func (a *Account) modelRateLimitKeysForRequest(ctx context.Context, requestedModel string) []string {
	if a == nil {
		return nil
	}

	modelKey := a.GetMappedModel(requestedModel)
	if a.Platform == PlatformAntigravity {
		modelKey = resolveFinalAntigravityModelKey(ctx, a, requestedModel)
	}
	modelKey = strings.TrimSpace(modelKey)
	if modelKey == "" {
		return nil
	}

	keys := []string{modelKey}
	switch a.Platform {
	case PlatformAntigravity:
		// thinking 变体与基础模型是同一个上游额度域，读路径同时认规范域和历史拼写；
		// 家族级 key 不再由单模型证据推断，避免 Gemini 被连坐。
		keys = antigravityModelRateLimitReadKeys(modelKey)
	case PlatformOpenAI:
		if openAIImageGenerationRateLimitApplies(ctx, requestedModel, modelKey) && modelKey != openAIImageGenerationRateLimitKey {
			keys = append(keys, openAIImageGenerationRateLimitKey)
		}
	case PlatformAnthropic:
		if isAnthropicFableModel(modelKey) && modelKey != anthropicFableRateLimitKey {
			keys = append(keys, anthropicFableRateLimitKey)
		}
	}
	return keys
}

// isAnthropicFableModel 判断是否为 Fable 模型家族（claude-fable-5、claude-fable-5[1m] 等变体）
func isAnthropicFableModel(model string) bool {
	return strings.Contains(strings.ToLower(model), "fable")
}

func openAIImageGenerationRateLimitApplies(ctx context.Context, requestedModel, modelKey string) bool {
	if isOpenAIImageGenerationModel(requestedModel) || isOpenAIImageGenerationModel(modelKey) {
		return true
	}
	return OpenAIImageGenerationIntentFromContext(ctx)
}

func WithOpenAIImageGenerationIntent(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ctxkey.OpenAIImageGenerationIntent, true)
}

func OpenAIImageGenerationIntentFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	enabled, ok := ctx.Value(ctxkey.OpenAIImageGenerationIntent).(bool)
	return ok && enabled
}

// WithOpenAIImagesEndpoint 标记请求从 /v1/images/* 专用生图端点入站。
func WithOpenAIImagesEndpoint(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ctxkey.OpenAIImagesEndpoint, true)
}

// OpenAIImagesEndpointFromContext 报告请求是否来自 /v1/images/*。
func OpenAIImagesEndpointFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	enabled, ok := ctx.Value(ctxkey.OpenAIImagesEndpoint).(bool)
	return ok && enabled
}

func resolveFinalAntigravityModelKey(ctx context.Context, account *Account, requestedModel string) string {
	modelKey := mapAntigravityModel(account, requestedModel)
	if modelKey == "" {
		return ""
	}
	// thinking 会影响 Antigravity 最终模型名（例如 claude-sonnet-4-5 -> claude-sonnet-4-5-thinking）
	if enabled, ok := ThinkingEnabledFromContext(ctx); ok {
		modelKey = applyThinkingModelSuffix(modelKey, enabled)
	}
	return modelKey
}

func isAntigravityGeminiModel(model string) bool {
	return strings.HasPrefix(normalizeAntigravityModelName(model), "gemini-")
}

// antigravityModelRateLimitKeys 返回一条模型级限流证据实际证明的冷却 scope（写路径）。
//
// 规则（T3-2.7）：
//   - 上游证据只证明"这一个模型"被限流，因此 scope 就是该模型自己；
//   - antigravityThinkingAliasBase 里的名字是同一上游模型的 thinking 别名，
//     统一写基础模型名，让两种拼写共享一个冷却；
//   - 不再由单模型 429/503 推断整族 Gemini 不可用：家族级连坐需要家族级证据，
//     单模型证据提供不了，就不推断。
func antigravityModelRateLimitKeys(model string) []string {
	domain := antigravityModelRateLimitDomain(model)
	if domain == "" {
		return nil
	}
	return []string{domain}
}

// antigravityModelRateLimitReadKeys 是读路径：先认规范化后的额度域，再认历史拼写。
// 旧版本把 thinking 别名写成 claude-sonnet-4-5-thinking，这些行仍然代表同一个
// 上游额度域，因此在重置时间到期前继续生效，不需要一次数据回填。
func antigravityModelRateLimitReadKeys(model string) []string {
	normalized := normalizeAntigravityModelName(model)
	if normalized == "" {
		return nil
	}
	domain := antigravityModelRateLimitDomain(normalized)
	if domain == normalized {
		return []string{domain}
	}
	return []string{domain, normalized}
}

// antigravityModelRateLimitDomain 把 Antigravity 模型名规范化为共享额度域。
func antigravityModelRateLimitDomain(model string) string {
	normalized := normalizeAntigravityModelName(model)
	if normalized == "" {
		return ""
	}
	if base, ok := antigravityThinkingAliasBase[normalized]; ok {
		return base
	}
	return normalized
}

// antigravityThinkingModelBase 是唯一带 thinking 别名的上游模型。
// 与 applyThinkingModelSuffix 共用，保证"别名对"只有一处定义。
const antigravityThinkingModelBase = "claude-sonnet-4-5"

// antigravityThinkingModelAlias 返回 thinking 打开时的上游模型名。
func antigravityThinkingModelAlias() string {
	return antigravityThinkingModelBase + "-thinking"
}

// antigravityThinkingAliasBase 把网关自己制造的 thinking 别名映射回基础模型。
// 只有这一对名字是"同一上游模型、仅 thinking 配置不同"；其它以 -thinking 结尾的
// 名字（如 claude-opus-4-6-thinking）是不同的真实上游模型，不参与规范化。
var antigravityThinkingAliasBase = map[string]string{
	antigravityThinkingModelBase + "-thinking": antigravityThinkingModelBase,
}

func (a *Account) modelRateLimitResetAt(scope string) *time.Time {
	if a == nil || a.Extra == nil || scope == "" {
		return nil
	}
	rawLimits, ok := a.Extra[modelRateLimitsKey].(map[string]any)
	if !ok {
		return nil
	}
	rawLimit, ok := rawLimits[scope].(map[string]any)
	if !ok {
		return nil
	}
	resetAtRaw, ok := rawLimit["rate_limit_reset_at"].(string)
	if !ok || strings.TrimSpace(resetAtRaw) == "" {
		return nil
	}
	resetAt, err := time.Parse(time.RFC3339, resetAtRaw)
	if err != nil {
		return nil
	}
	return &resetAt
}

func setAccountModelRateLimitSnapshot(account *Account, scope string, resetAt time.Time, reason string, now time.Time) {
	if account == nil || strings.TrimSpace(scope) == "" {
		return
	}
	if account.Extra == nil {
		account.Extra = make(map[string]any)
	}
	limits, ok := account.Extra[modelRateLimitsKey].(map[string]any)
	if !ok {
		limits = make(map[string]any)
		account.Extra[modelRateLimitsKey] = limits
	}
	payload := map[string]any{
		"rate_limited_at":     now.UTC().Format(time.RFC3339),
		"rate_limit_reset_at": resetAt.UTC().Format(time.RFC3339),
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		payload["reason"] = reason
	}
	limits[scope] = payload
}
