package apicompat

import (
	"encoding/base64"
	"fmt"
	"strings"
)

func GeminiToMessages(geminiResp map[string]any, model, messageID string, includeInlineData bool, toolID func(map[string]any) string, usage map[string]any) map[string]any {
	contentBlocks := make([]any, 0)
	sawToolUse := false
	if candidates, ok := geminiResp["candidates"].([]any); ok && len(candidates) > 0 {
		if cand, ok := candidates[0].(map[string]any); ok {
			if content, ok := cand["content"].(map[string]any); ok {
				if parts, ok := content["parts"].([]any); ok {
					for _, part := range parts {
						pm, ok := part.(map[string]any)
						if !ok {
							continue
						}
						if text, ok := pm["text"].(string); ok && text != "" {
							contentBlocks = append(contentBlocks, map[string]any{
								"type": "text",
								"text": text,
							})
						}
						if inlineData, ok := pm["inlineData"].(map[string]any); includeInlineData && ok {
							mimeType, _ := inlineData["mimeType"].(string)
							data, _ := inlineData["data"].(string)
							if IsGeminiInlineImageMIMEType(mimeType) && IsValidGeminiInlineBase64(data) {
								contentBlocks = append(contentBlocks, map[string]any{
									"type": "text",
									"text": fmt.Sprintf("![image](data:%s;base64,%s)", mimeType, data),
								})
							}
						}
						if fc, ok := pm["functionCall"].(map[string]any); ok {
							name, _ := fc["name"].(string)
							if strings.TrimSpace(name) == "" {
								name = "tool"
							}
							args := fc["args"]
							sawToolUse = true
							contentBlocks = append(contentBlocks, map[string]any{
								"type":  "tool_use",
								"id":    toolID(fc),
								"name":  name,
								"input": args,
							})
						}
					}
				}
			}
		}
	}

	stopReason := GeminiFinishReasonToMessagesStopReason(GeminiFinishReason(geminiResp))
	if sawToolUse {
		stopReason = "tool_use"
	}

	return map[string]any{
		"id":            messageID,
		"type":          "message",
		"role":          "assistant",
		"model":         model,
		"content":       contentBlocks,
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage":         usage,
	}
}

func IsGeminiInlineImageMIMEType(mimeType string) bool {
	switch mimeType {
	case "image/gif", "image/jpeg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}

func IsValidGeminiInlineBase64(data string) bool {
	if data == "" {
		return false
	}
	_, err := base64.StdEncoding.DecodeString(data)
	return err == nil
}

func GeminiFinishReason(geminiResp map[string]any) string {
	if candidates, ok := geminiResp["candidates"].([]any); ok && len(candidates) > 0 {
		if cand, ok := candidates[0].(map[string]any); ok {
			if fr, ok := cand["finishReason"].(string); ok {
				return fr
			}
		}
	}
	return ""
}

func GeminiFinishReasonToMessagesStopReason(finishReason string) string {
	switch strings.ToUpper(strings.TrimSpace(finishReason)) {
	case "MAX_TOKENS":
		return "max_tokens"
	case "STOP":
		return "end_turn"
	default:
		return "end_turn"
	}
}
