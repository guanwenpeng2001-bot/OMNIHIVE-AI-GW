package service

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// Inspect only known wire locations; text containing a field name is not a
// cache field. Presence says nothing about the provider's cache semantics.
func openAIExplicitCacheFieldPresence(body []byte) (options, breakpoint bool) {
	root := gjson.ParseBytes(body)
	options = root.Get("prompt_cache_options").Exists()
	breakpoint = root.Get("prompt_cache_breakpoint").Exists()
	if !breakpoint {
		root.Get("input").ForEach(func(_, item gjson.Result) bool {
			breakpoint = item.Get("prompt_cache_breakpoint").Exists()
			if !breakpoint {
				item.Get("content").ForEach(func(_, part gjson.Result) bool {
					breakpoint = part.Get("prompt_cache_breakpoint").Exists()
					return !breakpoint
				})
			}
			return !breakpoint
		})
	}
	return options, breakpoint
}

func logOpenAIRejectedFieldRetry(c *gin.Context, accountID int64, status, ordinal int, reason string) {
	// The normalizer returns this closed classification, never error.message or
	// arbitrary error.param values. Fail closed if another caller passes text.
	switch reason {
	case "tool parameter root type rejection", "content prompt_cache_breakpoint parameter rejection",
		"prompt_cache_breakpoint parameter rejection", "prompt_cache_options parameter rejection",
		"max_output_tokens parameter rejection", "truncation parameter rejection",
		"indexed status parameter rejection", "indexed prompt_cache_breakpoint parameter rejection",
		"indexed reasoning null content rejection", "indexed message null content rejection",
		"indexed reasoning content maximum-length rejection", "indexed namespace parameter rejection":
	default:
		reason = "rejected field"
	}
	logger.FromContext(c.Request.Context()).Info("openai: retrying rejected Responses field",
		zap.Int64("account_id", accountID), zap.Int("upstream_status_code", status),
		zap.Int("retry_ordinal", ordinal), zap.String("reason", reason))
}
