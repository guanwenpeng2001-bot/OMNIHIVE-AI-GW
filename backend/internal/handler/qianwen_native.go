package handler

import (
	"encoding/json"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// QianwenNative exposes only the verified synchronous conversation families.
func (h *OpenAIGatewayHandler) QianwenNative(c *gin.Context) {
	operation, ok := service.QianwenNativeOperationForPath(c.Request.URL.Path)
	if !ok || c.Request.Method != http.MethodPost {
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Unsupported DashScope operation; only text and image/text conversations are supported")
		return
	}
	h.chatCompletions(c, operation)
}

// Only the audit view uses Chat-shaped content. Forwarding retains native JSON.
func qianwenNativeAuditBody(body []byte) []byte {
	var messages []any
	gjson.GetBytes(body, "input.messages").ForEach(func(_, message gjson.Result) bool {
		content := message.Get("content")
		var value any = content.Value()
		if content.IsArray() {
			parts := []any{}
			content.ForEach(func(_, part gjson.Result) bool {
				if text := part.Get("text"); text.Exists() {
					parts = append(parts, gin.H{"type": "text", "text": text.String()})
				}
				if image := part.Get("image"); image.Exists() {
					parts = append(parts, gin.H{"type": "image_url", "image_url": gin.H{"url": image.String()}})
				}
				return true
			})
			value = parts
		}
		messages = append(messages, gin.H{"role": message.Get("role").String(), "content": value})
		return true
	})
	if len(messages) == 0 {
		messages = append(messages, gin.H{"role": "user", "content": gjson.GetBytes(body, "input.prompt").String()})
	}
	encoded, _ := json.Marshal(gin.H{"model": gjson.GetBytes(body, "model").String(), "messages": messages})
	return encoded
}
