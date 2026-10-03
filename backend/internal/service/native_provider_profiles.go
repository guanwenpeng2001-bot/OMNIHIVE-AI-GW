package service

import (
	"net/url"
	"strings"
)

// nativeProviderProfile binds a legacy platform to one exact regional/plan contract.
// Empty Capability means unknown metadata, not permission to borrow another profile.
// Sources: CC Switch 793e67d9 and provider-expansion RESEARCH-C (2026-10-03).
type nativeProviderProfile struct {
	Platform, Mode, Label, Chat, Messages, Responses, Capability string
	Discovery                                                    bool
}

var nativeProviderProfiles = []nativeProviderProfile{
	{"kimi", "payg", "Moonshot CN", "https://api.moonshot.cn/v1", "https://api.moonshot.cn/anthropic", "https://api.moonshot.cn/v1", "moonshotai-cn", true},
	{"kimi", "payg", "Moonshot Global", "https://api.moonshot.ai/v1", "https://api.moonshot.ai/anthropic", "https://api.moonshot.ai/v1", "moonshot", true},
	{"kimi", "coding", "Kimi Coding CN", "https://api.kimi.com/coding/v1", "https://api.kimi.com/coding", "https://api.kimi.com/coding/v1", "kimi-for-coding", false},
	{"kimi", "coding", "Kimi Coding Global", "https://api.kimi.ai/coding/v1", "https://api.kimi.ai/coding", "https://api.kimi.ai/coding/v1", "", false},
	{"zhipu", "payg", "GLM CN payg", "https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn/api/anthropic", "https://open.bigmodel.cn/api/v1", "zhipuai", false},
	{"zhipu", "coding", "GLM CN coding", "https://open.bigmodel.cn/api/coding/paas/v4", "https://open.bigmodel.cn/api/anthropic", "https://open.bigmodel.cn/api/v1", "zhipuai-coding-plan", false},
	{"zhipu", "payg", "GLM Global payg", "https://api.z.ai/api/paas/v4", "https://api.z.ai/api/anthropic", "https://api.z.ai/api/v1", "zai", false},
	{"zhipu", "coding", "GLM Global coding", "https://api.z.ai/api/coding/paas/v4", "https://api.z.ai/api/anthropic", "https://api.z.ai/api/v1", "zai-coding-plan", false},
	{"deepseek", "payg", "DeepSeek", "https://api.deepseek.com/v1", "https://api.deepseek.com/anthropic", "https://api.deepseek.com", "deepseek", true},
	{"minimax", "payg", "MiniMax Legacy CN payg", "https://api.minimaxi.com/v1", "https://api.minimaxi.com/anthropic", "https://api.minimaxi.com/v1", "minimax-cn", true},
	{"minimax", "coding", "MiniMax Legacy CN coding", "https://api.minimaxi.com/v1", "https://api.minimaxi.com/anthropic", "https://api.minimaxi.com/v1", "minimax-cn-coding-plan", false},
	{"minimax", "payg", "MiniMax CN payg", "https://api.minimax.cn/v1", "https://api.minimax.cn/anthropic", "https://api.minimax.cn/v1", "", true},
	{"minimax", "coding", "MiniMax CN coding", "https://api.minimax.cn/v1", "https://api.minimax.cn/anthropic", "https://api.minimax.cn/v1", "", false},
	{"minimax", "payg", "MiniMax Global payg", "https://api.minimax.io/v1", "https://api.minimax.io/anthropic", "https://api.minimax.io/v1", "minimax", true},
	{"minimax", "coding", "MiniMax Global coding", "https://api.minimax.io/v1", "https://api.minimax.io/anthropic", "https://api.minimax.io/v1", "minimax-coding-plan", false},
	{"qianfan", "payg", "Qianfan PAYG", "https://qianfan.baidubce.com/v2", "", "https://qianfan.baidubce.com/v2", "qianfan", true},
	{"qianfan", "coding", "Qianfan Coding", "https://qianfan.baidubce.com/v2/coding", "https://qianfan.baidubce.com/anthropic/coding", "", "", false},
	{"qianfan", "coding", "Qianfan Token Personal", "https://qianfan.baidubce.com/v2/tokenplan/personal", "https://qianfan.baidubce.com/anthropic/tokenplan/personal", "", "", false},
	{"qianfan", "coding", "Qianfan Token Enterprise", "https://qianfan.baidubce.com/v2/tokenplan/team", "https://qianfan.baidubce.com/anthropic/tokenplan/team", "", "", false},
	{"qianwen", "payg", "DashScope CN", "https://dashscope.aliyuncs.com/compatible-mode/v1", "https://dashscope.aliyuncs.com/apps/anthropic", "https://dashscope.aliyuncs.com/compatible-mode/v1", "alibaba-cn", true},
	{"qianwen", "payg", "DashScope Intl (QwenCloud)", "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", "https://dashscope-intl.aliyuncs.com/apps/anthropic", "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", "alibaba", true},
	{"qianwen", "coding", "DashScope Coding CN", "https://coding.dashscope.aliyuncs.com/v1", "https://coding.dashscope.aliyuncs.com/apps/anthropic", "", "alibaba-coding-plan-cn", false},
	{"qianwen", "coding", "DashScope Coding Intl (QwenCloud)", "https://coding-intl.dashscope.aliyuncs.com/v1", "https://coding-intl.dashscope.aliyuncs.com/apps/anthropic", "", "", false},
	{"qianwen", "coding", "Qianwen Token CN Beijing", "", "https://token-plan.cn-beijing.maas.aliyuncs.com/apps/anthropic", "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", "", false},
	{"qianwen", "payg", "Qianwen MaaS CN PAYG", "https://maas.qianwenaiapi.com/compatible-mode/v1", "https://maas.qianwenaiapi.com/apps/anthropic", "https://maas.qianwenaiapi.com/compatible-mode/v1", "", false},
	{"qianwen", "coding", "Qianwen MaaS CN Token", "https://token-plan.maas.qianwenaiapi.com/compatible-mode/v1", "https://token-plan.maas.qianwenaiapi.com/apps/anthropic", "https://token-plan.maas.qianwenaiapi.com/compatible-mode/v1", "", false},
	{"hunyuan", "payg", "TokenHub CN", "https://tokenhub.tencentmaas.com/v1", "https://tokenhub.tencentmaas.com/v1", "https://tokenhub.tencentmaas.com/v1", "hunyuan", false},
	{"hunyuan", "payg", "TokenHub CN alternate", "https://tokenhub.tencentmaas.cn/v1", "https://tokenhub.tencentmaas.cn/v1", "https://tokenhub.tencentmaas.cn/v1", "", false},
	{"hunyuan", "payg", "TokenHub legacy Intl", "https://tokenhub-intl.tencentmaas.com/v1", "https://tokenhub-intl.tencentmaas.com/v1", "https://tokenhub-intl.tencentmaas.com/v1", "", false},
	{"hunyuan", "payg", "TokenHub legacy Intl CN", "https://tokenhub-intl.tencentmaas.cn/v1", "https://tokenhub-intl.tencentmaas.cn/v1", "https://tokenhub-intl.tencentmaas.cn/v1", "", false},
	{"hunyuan", "payg", "TokenHub Intl", "https://tokenhub-intl.tencentcloudmaas.com/v1", "https://tokenhub-intl.tencentcloudmaas.com/v1", "https://tokenhub-intl.tencentcloudmaas.com/v1", "", false},
	{"hunyuan", "payg", "Hunyuan Legacy", "https://api.hunyuan.cloud.tencent.com/v1", "https://api.hunyuan.cloud.tencent.com/v1", "https://api.hunyuan.cloud.tencent.com/v1", "hunyuan", false},
	{"hunyuan", "coding", "TokenHub Legacy Coding", "https://api.lkeap.cloud.tencent.com/coding/v3", "https://api.lkeap.cloud.tencent.com/coding/anthropic", "", "", false},
	{"hunyuan", "coding", "Token Personal CN", "https://api.lkeap.cloud.tencent.com/plan/v3", "https://api.lkeap.cloud.tencent.com/plan/anthropic", "", "", false},
	{"hunyuan", "coding", "Token Enterprise Pro / Lite CN", "https://tokenhub.tencentmaas.com/plan/v3", "https://tokenhub.tencentmaas.com/plan/anthropic", "https://tokenhub.tencentmaas.com/plan/v3", "", false},
	{"hunyuan", "coding", "Token Personal / Enterprise Pro / Lite Intl", "https://tokenhub-intl.tencentcloudmaas.com/plan/v3", "https://tokenhub-intl.tencentcloudmaas.com/plan/anthropic", "https://tokenhub-intl.tencentcloudmaas.com/plan/v3", "", false},
	{"volcengine", "payg", "Ark PAYG", "https://ark.cn-beijing.volces.com/api/v3", "https://ark.cn-beijing.volces.com/api/compatible", "https://ark.cn-beijing.volces.com/api/v3", "volcengine", false},
	{"volcengine", "coding", "Ark Coding", "https://ark.cn-beijing.volces.com/api/coding/v3", "https://ark.cn-beijing.volces.com/api/coding", "https://ark.cn-beijing.volces.com/api/coding/v3", "", false},
	{"volcengine", "coding", "Ark Agent", "https://ark.cn-beijing.volces.com/api/plan/v3", "https://ark.cn-beijing.volces.com/api/plan", "https://ark.cn-beijing.volces.com/api/plan/v3", "", false},
}

func isNativeProfilePlatform(platform string) bool {
	switch platform {
	case PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformQianwen, PlatformHunyuan, PlatformQianfan, PlatformVolcengine:
		return true
	}
	return false
}

func nativeProfileURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() != "" {
		return ""
	}
	return "https://" + strings.ToLower(u.Host) + strings.TrimRight(u.EscapedPath(), "/")
}

func (p *nativeProviderProfile) base(protocol string) string {
	switch protocol {
	case APIProtocolChatCompletions:
		return p.Chat
	case APIProtocolAnthropic:
		return p.Messages
	case APIProtocolResponses:
		return p.Responses
	}
	return ""
}

func nativeProfileForURL(account *Account, raw string) *nativeProviderProfile {
	if account == nil || account.Type != AccountTypeAPIKey {
		return nil
	}
	normalized := nativeProfileURL(raw)
	if normalized == "" {
		return nil
	}
	mode := account.GetAccountMode()
	if mode == "" && strings.TrimSpace(account.GetCredential("account_mode")) != "" {
		return nil
	}
	if mode == "" {
		mode = AccountModePayG
	}
	for i := range nativeProviderProfiles {
		p := &nativeProviderProfiles[i]
		if p.Platform != account.Platform || (p.Mode != mode && account.GetAccountMode() != "") {
			continue
		}
		for _, base := range []string{p.Chat, p.Messages, p.Responses} {
			if base != "" && nativeProfileURL(base) == normalized {
				return p
			}
		}
		// Documented legacy aliases retain their site and plan, without rewriting storage.
		if account.Platform == PlatformDeepseek && normalized == "https://api.deepseek.com" {
			return p
		}
		if account.Platform == PlatformKimi && p.Mode == AccountModeCoding && normalized == p.Messages+"/v1" {
			return p
		}
	}
	return nil
}

func nativeProtocolOverride(account *Account, protocol string) string {
	switch bases := account.Credentials["api_base_urls"].(type) {
	case map[string]any:
		if value, ok := bases[protocol].(string); ok {
			return strings.TrimSpace(value)
		}
	case map[string]string:
		return strings.TrimSpace(bases[protocol])
	}
	return ""
}

// Only synthesize a missing protocol inside the same exact known profile.
// An explicit map always wins; a custom root never selects an official root.
func nativeConfiguredProtocolBase(account *Account, protocol string) (string, bool) {
	if account == nil || !isNativeProfilePlatform(account.Platform) {
		return "", false
	}
	if base := nativeProtocolOverride(account, protocol); base != "" {
		return base, true
	}
	base := strings.TrimSpace(account.GetCredential("base_url"))
	if base != "" {
		if account.GetCredential("api_protocol") == protocol {
			return base, true
		}
		if p := nativeProfileForURL(account, base); p != nil {
			for _, other := range []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses} {
				if configured := nativeProtocolOverride(account, other); configured != "" && nativeProfileForURL(account, configured) != p {
					return "", true
				}
			}
			if protocol == APIProtocolChatCompletions && account.Platform == PlatformDeepseek && nativeProfileURL(base) == "https://api.deepseek.com" {
				return base, true
			}
			return p.base(protocol), true
		}
		return base, true
	}
	for _, candidate := range []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses} {
		if base := nativeProtocolOverride(account, candidate); base != "" {
			if p := nativeAccountProfile(account); p != nil {
				return p.base(protocol), true
			}
			return "", true
		}
	}
	return "", false
}

func (a *Account) nativeSelectedOpenAIProtocol() string {
	// Read the stored choice directly to avoid recursion through capability checks.
	if a.GetCredential("api_protocol") == APIProtocolResponses {
		return APIProtocolResponses
	}
	return APIProtocolChatCompletions
}

// NativeAnthropicProfileTargetURL accepts the caller's already validated base.
// Kimi OpenCode cards include /v1; Claude cards do not. Append the suffix once.
func NativeAnthropicProfileTargetURL(base string) string {
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/v1") {
		return base + "/messages"
	}
	return base + "/v1/messages"
}

// All configured roots must belong to one profile before borrowing its metadata
// or using an official management endpoint. Mixed/custom maps remain unknown.
func nativeAccountProfile(a *Account) *nativeProviderProfile {
	if a == nil || !isNativeProfilePlatform(a.Platform) {
		return nil
	}
	var selected *nativeProviderProfile
	configured := false
	bases := []string{strings.TrimSpace(a.GetCredential("base_url"))}
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses} {
		bases = append(bases, nativeProtocolOverride(a, protocol))
	}
	for _, base := range bases {
		if base == "" {
			continue
		}
		configured = true
		p := nativeProfileForURL(a, base)
		if p == nil || (selected != nil && selected != p) {
			return nil
		}
		selected = p
	}
	if !configured {
		return nativeProfileForURL(a, a.GetOpenAIBaseURL())
	}
	return selected
}

func nativeProfileDiscoveryError(a *Account) error {
	if a == nil || !isNativeProfilePlatform(a.Platform) {
		return nil
	}
	if p := nativeAccountProfile(a); p != nil {
		if p.Discovery {
			return nil
		}
		return newUpstreamModelSyncUnsupportedError("This regional/plan profile has no documented model discovery endpoint; declare model IDs manually", nil)
	}
	// Custom Chat discovery stays on that exact override. A Messages-only custom
	// account must not fall through to the platform's domestic Chat catalog.
	if nativeProtocolOverride(a, APIProtocolChatCompletions) != "" {
		return nil
	}
	if a.GetCredential("api_protocol") != APIProtocolAnthropic && strings.TrimSpace(a.GetCredential("base_url")) != "" {
		return nil
	}
	return newUpstreamModelSyncUnsupportedError("No model discovery endpoint is known for this profile; declare model IDs manually", nil)
}
