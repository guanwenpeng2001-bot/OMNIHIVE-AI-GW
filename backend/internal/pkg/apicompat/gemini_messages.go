package apicompat

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

const GeminiDummyThoughtSignature = "skip_thought_signature_validator"

func MessagesToGemini(body []byte, prepareToolIDs func(map[string]any) error) ([]byte, error) {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}

	toolUseIDToName := make(map[string]string)

	systemText := extractClaudeSystemText(req["system"])
	contents, err := convertClaudeMessagesToGeminiContents(req["messages"], toolUseIDToName)
	if err != nil {
		return nil, err
	}

	out := make(map[string]any)
	if systemText != "" {
		out["systemInstruction"] = map[string]any{
			"parts": []any{map[string]any{"text": systemText}},
		}
	}
	out["contents"] = contents

	if tools := ConvertClaudeToolsToGeminiTools(req["tools"]); tools != nil {
		out["tools"] = tools
	}
	if choice, ok := req["tool_choice"].(map[string]any); ok {
		config := map[string]any{}
		switch choice["type"] {
		case "auto":
			config["mode"] = "AUTO"
		case "none":
			config["mode"] = "NONE"
		case "any":
			config["mode"] = "ANY"
		case "tool":
			config["mode"] = "ANY"
			config["allowedFunctionNames"] = []any{choice["name"]}
		default:
			return nil, errors.New("unsupported tool_choice for Gemini")
		}
		out["toolConfig"] = map[string]any{"functionCallingConfig": config}
	}

	generationConfig, err := ConvertClaudeGenerationConfig(req)
	if err != nil {
		return nil, err
	}
	if generationConfig != nil {
		out["generationConfig"] = generationConfig
	}

	if err := prepareToolIDs(out); err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

func extractClaudeSystemText(system any) string {
	switch v := system.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		var parts []string
		for _, p := range v {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := pm["type"].(string); t != "text" {
				continue
			}
			if text, ok := pm["text"].(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	default:
		return ""
	}
}

func convertClaudeMessagesToGeminiContents(messages any, toolUseIDToName map[string]string) ([]any, error) {
	arr, ok := messages.([]any)
	if !ok {
		return nil, errors.New("messages must be an array")
	}

	out := make([]any, 0, len(arr))
	for _, m := range arr {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role, _ := mm["role"].(string)
		role = strings.ToLower(strings.TrimSpace(role))
		gRole := "user"
		if role == "assistant" {
			gRole = "model"
		}

		parts := make([]any, 0)
		switch content := mm["content"].(type) {
		case string:
			// 字符串形式的 content，保留所有内容（包括空白）
			parts = append(parts, map[string]any{"text": content})
		case []any:
			// 如果只有一个 block，不过滤空白（让上游 API 报错）
			singleBlock := len(content) == 1

			for _, block := range content {
				bm, ok := block.(map[string]any)
				if !ok {
					continue
				}
				bt, _ := bm["type"].(string)
				switch bt {
				case "text":
					if text, ok := bm["text"].(string); ok {
						// 单个 block 时保留所有内容（包括空白）
						// 多个 blocks 时过滤掉空白
						if singleBlock || strings.TrimSpace(text) != "" {
							parts = append(parts, map[string]any{"text": text})
						}
					}
				case "tool_use":
					id, _ := bm["id"].(string)
					name, _ := bm["name"].(string)
					if strings.TrimSpace(id) == "" || strings.TrimSpace(name) == "" {
						return nil, errors.New("tool_use requires a nonempty id and name")
					}
					if _, exists := toolUseIDToName[id]; exists {
						return nil, errors.New("duplicate tool_use id")
					}
					toolUseIDToName[id] = name
					signature, _ := bm["signature"].(string)
					signature = strings.TrimSpace(signature)
					if signature == "" {
						signature = GeminiDummyThoughtSignature
					}
					parts = append(parts, map[string]any{
						"thoughtSignature": signature,
						"functionCall": map[string]any{
							"id":   id,
							"name": name,
							"args": bm["input"],
						},
					})
				case "tool_result":
					toolUseID, _ := bm["tool_use_id"].(string)
					name := toolUseIDToName[toolUseID]
					if name == "" {
						return nil, errors.New("tool_result references an unknown or already completed tool_use id")
					}
					delete(toolUseIDToName, toolUseID)
					parts = append(parts, map[string]any{
						"functionResponse": map[string]any{
							"id":   toolUseID,
							"name": name,
							"response": map[string]any{
								"content": extractClaudeContentText(bm["content"]),
							},
						},
					})
				case "image", "document":
					if src, ok := bm["source"].(map[string]any); ok {
						if srcType, _ := src["type"].(string); srcType == "base64" {
							mediaType, _ := src["media_type"].(string)
							data, _ := src["data"].(string)
							if mediaType != "" && data != "" {
								parts = append(parts, map[string]any{
									"inlineData": map[string]any{
										"mimeType": mediaType,
										"data":     data,
									},
								})
							}
						}
					}
				default:
					// best-effort: preserve unknown blocks as text
					if b, err := json.Marshal(bm); err == nil {
						parts = append(parts, map[string]any{"text": string(b)})
					}
				}
			}
		default:
			// ignore
		}

		out = append(out, map[string]any{
			"role":  gRole,
			"parts": parts,
		})
	}
	return out, nil
}

func extractClaudeContentText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		var sb strings.Builder
		for _, part := range t {
			pm, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if pm["type"] == "text" {
				if text, ok := pm["text"].(string); ok {
					_, _ = sb.WriteString(text)
				}
			}
		}
		return sb.String()
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func ConvertClaudeToolsToGeminiTools(tools any) []any {
	arr, ok := tools.([]any)
	if !ok || len(arr) == 0 {
		return nil
	}

	hasWebSearch := false
	funcDecls := make([]any, 0, len(arr))
	for _, t := range arr {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		if isClaudeWebSearchToolMap(tm) {
			hasWebSearch = true
			continue
		}

		var name, desc string
		var params any

		// 检查是否为 custom 类型工具 (MCP)
		toolType, _ := tm["type"].(string)
		if toolType == "custom" {
			// Custom 格式: 从 custom 字段获取 description 和 input_schema
			custom, ok := tm["custom"].(map[string]any)
			if !ok {
				continue
			}
			name, _ = tm["name"].(string)
			desc, _ = custom["description"].(string)
			params = custom["input_schema"]
		} else {
			// 标准格式: 从顶层字段获取
			name, _ = tm["name"].(string)
			desc, _ = tm["description"].(string)
			params = tm["input_schema"]
		}

		if name == "" {
			continue
		}

		// 为 nil params 提供默认值
		if params == nil {
			params = map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			}
		}
		// 清理 JSON Schema
		cleanedParams := CleanToolSchema(params)

		funcDecls = append(funcDecls, map[string]any{
			"name":        name,
			"description": desc,
			"parameters":  cleanedParams,
		})
	}

	out := make([]any, 0, 2)
	if len(funcDecls) > 0 {
		out = append(out, map[string]any{
			"functionDeclarations": funcDecls,
		})
	}
	if hasWebSearch {
		out = append(out, map[string]any{
			"googleSearch": map[string]any{},
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isClaudeWebSearchToolMap(tool map[string]any) bool {
	toolType, _ := tool["type"].(string)
	// A function named web_search is still a client-side function. This is
	// especially important for Chat Completions clients such as Hermes, whose
	// built-in runtime tools are represented as ordinary function tools.
	// Promote only explicitly typed server-side search tools to Gemini's
	// built-in googleSearch tool.
	return strings.HasPrefix(toolType, "web_search") || toolType == "google_search"
}

// CleanToolSchema 清理工具的 JSON Schema，移除 Gemini 不支持的字段
func CleanToolSchema(schema any) any {
	if schema == nil {
		return nil
	}

	switch v := schema.(type) {
	case map[string]any:
		cleaned := make(map[string]any)
		for key, value := range v {
			// 跳过不支持的字段
			if key == "$schema" || key == "$id" || key == "$ref" ||
				key == "$defs" || key == "definitions" ||
				key == "additionalProperties" || key == "patternProperties" || key == "minLength" ||
				key == "maxLength" || key == "minItems" || key == "maxItems" || key == "exclusiveMinimum" ||
				key == "deprecated" {
				continue
			}
			// 递归清理嵌套对象
			cleaned[key] = CleanToolSchema(value)
		}
		if enum, ok := cleaned["enum"].([]any); ok {
			if normalized, ok := normalizeGeminiEnum(enum); ok {
				cleaned["enum"] = normalized
			} else {
				delete(cleaned, "enum")
			}
		}
		// 规范化 type 字段为大写
		if typeVal, ok := cleaned["type"].(string); ok {
			cleaned["type"] = strings.ToUpper(typeVal)
		} else if typeValues, ok := cleaned["type"].([]any); ok {
			for _, typeValue := range typeValues {
				typeName, ok := typeValue.(string)
				if ok && !strings.EqualFold(typeName, "null") {
					cleaned["type"] = strings.ToUpper(typeName)
					break
				}
			}
			if _, ok := cleaned["type"].([]any); ok {
				delete(cleaned, "type")
			}
		}
		if cleaned["type"] == "INTEGER" {
			if minimum, ok := incrementIntegralSchemaBound(v["exclusiveMinimum"]); ok {
				if existing, exists := cleaned["minimum"]; !exists || schemaNumberLess(existing, minimum) {
					cleaned["minimum"] = minimum
				}
			}
		}
		return cleaned
	case []any:
		cleaned := make([]any, len(v))
		for i, item := range v {
			cleaned[i] = CleanToolSchema(item)
		}
		return cleaned
	default:
		return v
	}
}

func normalizeGeminiEnum(values []any) ([]any, bool) {
	normalized := make([]any, len(values))
	for i, value := range values {
		if stringValue, ok := value.(string); ok {
			normalized[i] = stringValue
			continue
		}

		switch value.(type) {
		case nil, bool, float32, float64, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, json.Number:
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, false
			}
			normalized[i] = string(encoded)
		default:
			return nil, false
		}
	}
	return normalized, true
}

func incrementIntegralSchemaBound(value any) (any, bool) {
	switch v := value.(type) {
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || v != math.Trunc(v) || v+1 <= v {
			return nil, false
		}
		return v + 1, true
	case int:
		if v == math.MaxInt {
			return nil, false
		}
		return v + 1, true
	case int64:
		if v == math.MaxInt64 {
			return nil, false
		}
		return v + 1, true
	case json.Number:
		i, err := v.Int64()
		if err != nil || i == math.MaxInt64 {
			return nil, false
		}
		return json.Number(fmt.Sprintf("%d", i+1)), true
	default:
		return nil, false
	}
}

func schemaNumberLess(left, right any) bool {
	leftNumber, leftOK := schemaNumberFloat64(left)
	rightNumber, rightOK := schemaNumberFloat64(right)
	return leftOK && rightOK && leftNumber < rightNumber
}

func schemaNumberFloat64(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, !math.IsNaN(v) && !math.IsInf(v, 0)
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil && !math.IsInf(n, 0)
	default:
		return 0, false
	}
}

func ConvertClaudeGenerationConfig(req map[string]any) (map[string]any, error) {
	out := make(map[string]any)
	if mt, ok := geminiInt(req["max_tokens"]); ok && mt > 0 {
		out["maxOutputTokens"] = mt
	}
	if temp, ok := req["temperature"].(float64); ok {
		out["temperature"] = temp
	}
	if topP, ok := req["top_p"].(float64); ok {
		out["topP"] = topP
	}
	if topK, ok := req["top_k"].(float64); ok {
		out["topK"] = topK
	}
	if config, ok := req["output_config"].(map[string]any); ok {
		if effort, exists := config["effort"]; exists {
			level, ok := effort.(string)
			if !ok {
				return nil, fmt.Errorf("output_config.effort %v cannot be represented by Gemini", effort)
			}
			switch level {
			case "minimal", "low", "medium", "high":
				out["thinkingConfig"] = map[string]any{"thinkingLevel": strings.ToUpper(level)}
			default:
				return nil, fmt.Errorf("output_config.effort %v cannot be represented by Gemini", effort)
			}
		}
		if format, ok := config["format"].(map[string]any); ok {
			if format["type"] == "json_schema" || format["type"] == "json_object" {
				out["responseMimeType"] = "application/json"
				if format["schema"] != nil {
					out["responseJsonSchema"] = format["schema"]
				}
			}
		}
	}
	if raw, exists := req["thinking"]; exists {
		thinking, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("thinking must be an object")
		}
		switch thinking["type"] {
		case "disabled":
			if out["thinkingConfig"] != nil {
				return nil, errors.New("disabled thinking and output_config.effort cannot both be set for Gemini")
			}
			out["thinkingConfig"] = map[string]any{"thinkingBudget": 0}
		case "adaptive":
			if out["thinkingConfig"] == nil {
				out["thinkingConfig"] = map[string]any{"thinkingBudget": -1}
			}
		case "enabled":
			budget, ok := thinking["budget_tokens"].(float64)
			if !ok || budget < 0 || budget > math.MaxInt32 || math.Trunc(budget) != budget {
				return nil, errors.New("thinking.budget_tokens must be a non-negative integer for Gemini")
			}
			if out["thinkingConfig"] != nil {
				return nil, errors.New("thinking.budget_tokens and output_config.effort cannot both be set for Gemini")
			}
			out["thinkingConfig"] = map[string]any{"thinkingBudget": int(budget)}
		default:
			return nil, errors.New("thinking.type must be disabled, adaptive, or enabled for Gemini")
		}
	}
	if stopSeq, ok := req["stop_sequences"].([]any); ok && len(stopSeq) > 0 {
		out["stopSequences"] = stopSeq
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func geminiInt(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	case int64:
		return int(t), true
	case json.Number:
		i, err := t.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}
