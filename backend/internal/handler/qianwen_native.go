package handler

import (
	"encoding/json"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// QianwenNative exposes conversations and synchronous Qwen image generation/editing.
func (h *OpenAIGatewayHandler) QianwenNative(c *gin.Context) {
	if h.rejectQianwenNativeVideo(c) {
		return
	}
	operation, ok := service.QianwenNativeOperationForPath(c.Request.URL.Path)
	if !ok || c.Request.Method != http.MethodPost {
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Unsupported DashScope operation; supported: text conversations, image/text conversations, synchronous Qwen image generation/editing on multimodal-generation")
		return
	}
	h.chatCompletions(c, operation)
}

// Only the audit view uses Chat-shaped content. Forwarding retains native JSON.
func qianwenNativeAuditBody(body []byte) []byte {
	// Match encoding/json and upstream last-key semantics when clients send
	// duplicate input/content members; the routing model is checked separately.
	var canonical any
	if json.Unmarshal(body, &canonical) == nil {
		body, _ = json.Marshal(canonical)
	}
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
