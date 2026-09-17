package service

import (
	"net/url"
	"strings"
)

// shouldNormalizeDeepSeekContextSuffix leaves explicit model namespaces alone.
// This is an outbound compatibility rule, not a model admission policy.
func shouldNormalizeDeepSeekContextSuffix(account *Account) bool {
	if account == nil || account.Platform != PlatformDeepseek || account.IsOpenAIPassthroughEnabled() || len(account.GetModelMapping()) != 0 {
		return false
	}
	if !isOfficialDeepSeekContextEndpoint(account.GetOpenAIBaseURL()) {
		return false
	}
	if bases, ok := account.Credentials["api_base_urls"].(map[string]any); ok {
		for _, raw := range bases {
			if base, ok := raw.(string); ok && strings.TrimSpace(base) != "" && !isOfficialDeepSeekContextEndpoint(base) {
				return false
			}
		}
	}
	return true
}

func isOfficialDeepSeekContextEndpoint(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "api.deepseek.com") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	switch strings.TrimRight(u.Path, "/") {
	case "", "/v1", "/anthropic":
		return true
	default:
		return false
	}
}
