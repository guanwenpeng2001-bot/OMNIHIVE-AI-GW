package service

import (
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/apicompat"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Public API support is not evidence of support on Codex OAuth or a compatible
// gateway. Keep automatic boundary mapping off those transports.
func isPublicOpenAIResponsesAccount(account *Account) bool {
	if account == nil || !account.IsOpenAIApiKey() {
		return false
	}
	base := strings.TrimRight(account.GetOpenAIBaseURL(), "/")
	return base == "https://api.openai.com" || base == "https://api.openai.com/v1"
}

func clearOpenAIExplicitCacheModeWithoutMarkers(body []byte) ([]byte, error) {
	if gjson.GetBytes(body, "prompt_cache_options.mode").String() != "explicit" ||
		gjson.GetBytes(body, "prompt_cache_breakpoint").Exists() ||
		apicompat.HasResponsesPromptCacheBreakpoint([]byte(gjson.GetBytes(body, "input").Raw)) {
		return body, nil
	}
	return sjson.DeleteBytes(body, "prompt_cache_options.mode")
}
