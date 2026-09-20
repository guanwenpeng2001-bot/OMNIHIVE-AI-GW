package service

import "strings"

const (
	DefaultQianwenBaseURL          = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	DefaultQianwenAnthropicBaseURL = "https://dashscope.aliyuncs.com/apps/anthropic"
	DefaultQianwenDashScopeBaseURL = "https://dashscope.aliyuncs.com/api/v1"
	DefaultQianwenTestModel        = "qwen-flash"
)

// DefaultQianwenModelIDs returns reviewed administrative candidates only.
// Discovery remains authoritative, including case-sensitive third-party IDs.
func DefaultQianwenModelIDs() []string {
	return NewModelRegistry().Snapshot().CandidateIDs(PlatformQianwen, "")
}

// Token Plan keys (sk-sp-*) are accepted like other API keys. Operators must
// review the provider's interactive-use restrictions before using that plan.
// No balance/remaining-quota capability is inferred from a key prefix.

// Shared model families need an explicit CompositeRoute; the same canonical
// name is served by multiple platforms. Explicit provider prefixes remain valid.
func qianwenSharedModelRequiresRoute(model string) bool {
	if strings.Contains(model, "/") {
		return false
	}
	for _, prefix := range []string{"qwen", "deepseek-", "glm-", "kimi-", "minimax-"} {
		if strings.HasPrefix(model, prefix) {
			return true
		}
	}
	return false
}
