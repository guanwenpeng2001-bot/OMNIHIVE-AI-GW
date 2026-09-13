package service

import (
	"fmt"
	"strings"
)

const (
	maxModelProtocolRules         = 64
	maxModelProtocolPatternLength = 128
)

type ModelProtocolRule struct {
	Pattern  string `json:"pattern"`
	Protocol string `json:"protocol"`
}

func isNativeModelProtocol(protocol string) bool {
	switch protocol {
	case APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses:
		return true
	}
	return false
}

func modelProtocolPatternMatches(pattern, model string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	model = strings.ToLower(strings.TrimSpace(model))
	if pattern == "" || model == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(model, strings.TrimSuffix(pattern, "*"))
	}
	return model == pattern
}

func matchModelProtocolRules(model string, rules []ModelProtocolRule) string {
	for _, rule := range rules {
		if !isNativeModelProtocol(rule.Protocol) {
			continue
		}
		if modelProtocolPatternMatches(rule.Pattern, model) {
			return rule.Protocol
		}
	}
	return APIProtocolChatCompletions
}

func parseModelProtocolRules(raw any) ([]ModelProtocolRule, error) {
	items, err := modelProtocolRuleItems(raw)
	if err != nil {
		return nil, err
	}
	if len(items) > maxModelProtocolRules {
		return nil, fmt.Errorf("protocol_rules supports at most %d entries", maxModelProtocolRules)
	}
	rules := make([]ModelProtocolRule, 0, len(items))
	for i, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("protocol_rules[%d] must be an object", i)
		}
		pattern, _ := entry["pattern"].(string)
		protocol, _ := entry["protocol"].(string)
		pattern, err := normalizeModelProtocolPattern(pattern)
		if err != nil {
			return nil, fmt.Errorf("protocol_rules[%d]: %w", i, err)
		}
		protocol = strings.TrimSpace(protocol)
		if !isNativeModelProtocol(protocol) {
			return nil, fmt.Errorf("protocol_rules[%d]: protocol must be chat_completions, anthropic, or responses", i)
		}
		rules = append(rules, ModelProtocolRule{Pattern: pattern, Protocol: protocol})
	}
	return rules, nil
}

func modelProtocolRuleItems(raw any) ([]any, error) {
	switch items := raw.(type) {
	case []any:
		return items, nil
	case []map[string]any:
		out := make([]any, 0, len(items))
		for _, item := range items {
			out = append(out, item)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("protocol_rules must be an array")
	}
}

func normalizeModelProtocolPattern(pattern string) (string, error) {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	if pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}
	if len(pattern) > maxModelProtocolPatternLength {
		return "", fmt.Errorf("pattern is too long")
	}
	if strings.ContainsAny(pattern, " \t") {
		return "", fmt.Errorf("pattern must not contain whitespace")
	}
	star := strings.Count(pattern, "*")
	if star > 1 || (star == 1 && !strings.HasSuffix(pattern, "*")) {
		return "", fmt.Errorf("pattern may use a single trailing * wildcard")
	}
	return pattern, nil
}
