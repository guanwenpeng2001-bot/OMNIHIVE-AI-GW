package service

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

// IsQianwenNativeImageModel is deliberately bounded to the per-image models.
// New families (including 3.0 with distinct usage units) need their own adapter.
func IsQianwenNativeImageModel(model string) bool {
	switch model {
	case "qwen-image", "qwen-image-plus", "qwen-image-max", "qwen-image-max-2025-12-30",
		"qwen-image-plus-2026-01-09", "qwen-image-2.0", "qwen-image-2.0-pro",
		"qwen-image-2.0-2026-03-03", "qwen-image-2.0-pro-2026-03-03",
		"qwen-image-2.0-pro-2026-04-22", "qwen-image-2.0-pro-2026-06-22",
		"qwen-image-edit", "qwen-image-edit-plus", "qwen-image-edit-max",
		"qwen-image-edit-plus-2025-12-15", "qwen-image-edit-plus-2025-10-30",
		"qwen-image-edit-max-2026-01-16":
		return true
	}
	return false
}

// ValidateQianwenNativeOperation must also run after account model mapping.
// Voice enrollment models collide with the audio family name check, so the
// voice hook runs first; rerank and embedding families are disjoint.
func ValidateQianwenNativeOperation(operation QianwenNativeOperation, model string, body []byte, stream bool) error {
	if handled, err := validateQianwenVoiceEnrollmentOperation(operation, model, body, stream); handled {
		return err
	}
	if handled, err := validateQianwenNativeAudioOperation(operation, model, body, stream); handled {
		return err
	}
	if handled, err := validateQianwenNativeRerankOperation(operation, model, body, stream); handled {
		return err
	}
	if handled, err := validateQianwenNativeEmbeddingOperation(operation, model, body, stream); handled {
		return err
	}
	if !IsQianwenNativeImageModel(model) {
		if strings.HasPrefix(model, "qwen-image") || strings.HasPrefix(model, "wan") ||
			strings.HasPrefix(model, "z-image") || IsQianwenNative3DModel(model) ||
			strings.HasPrefix(model, "cosyvoice") || strings.HasPrefix(model, "fun-") ||
			strings.HasPrefix(model, "qwen-voice") ||
			strings.Contains(model, "tts") || strings.Contains(model, "omni") ||
			strings.Contains(model, "audio") || strings.Contains(model, "realtime") {
			return fmt.Errorf("unsupported DashScope model family: %s", model)
		}
		return nil
	}
	if operation != QianwenNativeMultimodal || stream || gjson.GetBytes(body, "stream").Bool() {
		return fmt.Errorf("DashScope image generation/editing requires synchronous multimodal-generation")
	}
	if !gjson.GetBytes(body, "input.messages").IsArray() || len(gjson.GetBytes(body, "input.messages").Array()) == 0 {
		return fmt.Errorf("DashScope image generation/editing requires input.messages")
	}
	if gjson.GetBytes(body, "parameters.enable_thinking").Exists() {
		return fmt.Errorf("DashScope image generation/editing does not support enable_thinking")
	}
	return nil
}

// observeQianwenNativeImage counts successful outputs, never the requested n.
// Some synchronous SDK/API responses omit usage; count the returned image URLs
// in that case. Preserve provider usage untouched in the client response.
func observeQianwenNativeImage(body, request []byte, result *OpenAIForwardResult) error {
	if gjson.GetBytes(body, "output.task_id").Exists() {
		return fmt.Errorf("unexpected asynchronous DashScope image task")
	}
	count := 0
	gjson.GetBytes(body, "output.choices").ForEach(func(_, choice gjson.Result) bool {
		choice.Get("message.content").ForEach(func(_, part gjson.Result) bool {
			if value := part.Get("image"); value.Type == gjson.String && strings.TrimSpace(value.String()) != "" {
				count++
			}
			return true
		})
		return true
	})
	if count == 0 {
		return fmt.Errorf("DashScope image response has no successful image outputs")
	}
	usage := gjson.GetBytes(body, "usage.image_count")
	if usage.Exists() && (usage.Type != gjson.Number || usage.Float() != float64(count)) {
		return fmt.Errorf("DashScope image usage does not match successful output count")
	}
	result.ImageCount = count
	result.ImageInputSize = strings.ReplaceAll(gjson.GetBytes(request, "parameters.size").String(), "*", "x")
	w, h := gjson.GetBytes(body, "usage.width").Int(), gjson.GetBytes(body, "usage.height").Int()
	if w > 0 && h > 0 {
		for i := 0; i < count; i++ {
			result.ImageOutputSizes = append(result.ImageOutputSizes, fmt.Sprintf("%dx%d", w, h))
		}
	}
	ApplyOpenAIImageBillingResolution(result)
	return nil
}
