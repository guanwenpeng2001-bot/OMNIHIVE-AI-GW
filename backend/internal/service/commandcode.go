package service

import (
	"github.com/tidwall/gjson"
	"net/http"
	"strings"
)

const DefaultCommandCodeBaseURL = "https://api.commandcode.ai/provider/v1"
const DefaultCommandCodeTestModel = "gpt-5.6-luna"

// Catalog IDs remain intact on the wire; only comparisons are normalized.
func normalizeCommandCodeModelID(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}
func IsCommandCode(platform string) bool { return platform == PlatformCommandCode }
func (a *Account) IsCommandCode() bool   { return a != nil && IsCommandCode(a.Platform) }
func DefaultCommandCodeModelIDs() []string {
	return NewModelRegistry().Snapshot().CandidateIDs(PlatformCommandCode, "")
}
func DefaultCommandCodeProtocolRules() []ModelProtocolRule {
	return []ModelProtocolRule{{Pattern: "claude-*", Protocol: APIProtocolAnthropic}, {Pattern: "anthropic/claude-*", Protocol: APIProtocolAnthropic}}
}
func CommandCodeModelProtocol(model string) string {
	return matchModelProtocolRules(normalizeCommandCodeModelID(model), DefaultCommandCodeProtocolRules())
}

// Responses is never a native endpoint, even when stale credentials request it.
// Explicit Chat/Messages pins are advanced overrides; adaptive rules otherwise
// select by the fully mapped model ID without stripping vendor prefixes.
func (a *Account) ResolveCommandCodeUpstreamProtocol(model string) string {
	if !a.IsCommandCode() {
		return ""
	}
	switch a.GetAPIProtocol() {
	case APIProtocolChatCompletions, APIProtocolAnthropic:
		return a.GetAPIProtocol()
	}
	if raw, ok := a.Credentials["protocol_rules"]; ok && raw != nil {
		if rules, err := parseModelProtocolRules(raw); err == nil {
			for _, rule := range rules {
				if !modelProtocolPatternMatches(rule.Pattern, normalizeCommandCodeModelID(model)) {
					continue
				}
				if rule.Protocol == APIProtocolResponses {
					return CommandCodeModelProtocol(model)
				}
				return rule.Protocol
			}
			return APIProtocolChatCompletions
		}
	}
	return CommandCodeModelProtocol(model)
}

func modelRoutedNativeProtocol(account *Account, model string) string {
	if account.IsCommandCode() {
		return account.ResolveCommandCodeUpstreamProtocol(model)
	}
	return openCodeGoNativeProtocol(account, model)
}

func setModelProviderChatAuthHeader(header http.Header, account *Account, token string) {
	if account.IsCommandCode() {
		header.Del("Authorization")
		header.Set("x-api-key", token)
		return
	}
	header.Set("Authorization", "Bearer "+token)
}

// Both wire envelopes use error.code when available. The observed Messages
// envelope instead embeds MODEL_NOT_IN_PLAN at the start of error.message.
// Other 403s retain existing handling; status alone cannot identify a plan.
func commandCodePlanError(status int, body []byte) string {
	if status != http.StatusForbidden {
		return ""
	}
	code := strings.ToUpper(strings.TrimSpace(gjson.GetBytes(body, "error.code").String()))
	switch code {
	case "MODEL_NOT_IN_PLAN", "UPGRADE_REQUIRED":
		return code
	}
	msg := strings.TrimSpace(gjson.GetBytes(body, "error.message").String())
	if strings.HasPrefix(strings.ToUpper(msg), "MODEL_NOT_IN_PLAN:") {
		return "MODEL_NOT_IN_PLAN"
	}
	return ""
}
func isCommandCodeTransientUpstreamError(status int, body []byte) bool {
	if status == http.StatusForbidden {
		if commandCodePlanError(status, body) != "" || isCommandCodeInvalidCredential(status, body) {
			return false
		}
		text := strings.ToLower(string(body))
		return strings.Contains(text, "site owner has blocked access") ||
			((strings.Contains(text, "<html") || strings.Contains(text, "<!doctype html")) &&
				(strings.Contains(text, "cf-chl-") || strings.Contains(text, "cloudflare") && strings.Contains(text, "just a moment"))) ||
			strings.Contains(text, "blocked access based on your browser") ||
			(strings.Contains(text, "browser") && strings.Contains(text, "signature") && (strings.Contains(text, "blocked") || strings.Contains(text, "forbidden")))
	}
	return status == http.StatusNotFound && strings.Contains(strings.ToLower(string(body)), "is not a registered api route")
}
