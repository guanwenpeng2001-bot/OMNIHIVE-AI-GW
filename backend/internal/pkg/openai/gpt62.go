package openai

import (
	"fmt"
	"strings"
)

// GPT62ModelID recognizes published IDs and the gateway's effort/compact aliases.
// It does not infer dated releases, other GPT-6 families, prices or account grants.
func GPT62ModelID(model string) string {
	canonical := CanonicalizeOpenAIModelAliasSpelling(model)
	for _, base := range []string{"gpt-6.2-sol", "gpt-6.2-astra"} {
		if canonical == base {
			return base
		}
		if suffix, ok := strings.CutPrefix(canonical, base+"-"); ok {
			switch suffix {
			case "none", "minimal", "low", "medium", "high", "xhigh", "max", "openai-compact":
				return base
			}
		}
	}
	return ""
}

// ValidateGPT62ReasoningEffort keeps disabled reasoning from silently becoming
// the model default. Source: OpenAI's GPT-6.2 migration guide, 2026-11-18.
func ValidateGPT62ReasoningEffort(model, effort string) error {
	if base := GPT62ModelID(model); base != "" {
		switch strings.ToLower(strings.TrimSpace(effort)) {
		case "none", "minimal":
			return fmt.Errorf("%s does not support reasoning effort %q; use low, medium, high, xhigh or max", base, effort)
		}
	}
	return nil
}
