package handler

import (
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/gatewayerror"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// Both passthrough and bridge require the same positive JSON integer. Duplicate
// or case-variant fields cannot make the two parsers choose different limits.
// Model-specific limits remain enforced by the resolved upstream.
func validateMessagesMaxTokens(ctx *gin.Context, body []byte) bool {
	value := gjson.GetBytes(body, "max_tokens")
	count := 0
	gjson.ParseBytes(body).ForEach(func(field, candidate gjson.Result) bool {
		if strings.EqualFold(field.String(), "max_tokens") {
			count++
		}
		return true
	})
	parsed, err := strconv.ParseInt(value.Raw, 10, 32)
	if count != 1 || value.Type != gjson.Number || err != nil || parsed <= 0 {
		gatewayerror.Write(ctx, 400, "invalid_request_error", "max_tokens is required and must be a positive integer no greater than 2147483647")
		return false
	}
	return true
}
