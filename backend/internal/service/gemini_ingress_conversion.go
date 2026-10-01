package service

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// ProtoJSON accepts the original proto field names as well as lowerCamelCase.
// Traverse protocol envelopes only: tool argument/result keys and schema
// property names are user data and must never be normalized.
func normalizeGeminiProtoJSON(value any) error {
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			if err := normalizeGeminiProtoJSON(item); err != nil {
				return err
			}
		}
	case map[string]any:
		for from, to := range map[string]string{
			"system_instruction": "systemInstruction", "generation_config": "generationConfig", "tool_config": "toolConfig", "cached_content": "cachedContent", "safety_settings": "safetySettings",
			"inline_data": "inlineData", "file_data": "fileData", "function_call": "functionCall", "function_response": "functionResponse", "thought_signature": "thoughtSignature", "mime_type": "mimeType", "file_uri": "fileUri",
			"function_declarations": "functionDeclarations", "parameters_json_schema": "parametersJsonSchema", "function_calling_config": "functionCallingConfig", "allowed_function_names": "allowedFunctionNames",
			"max_output_tokens": "maxOutputTokens", "top_p": "topP", "top_k": "topK", "stop_sequences": "stopSequences", "candidate_count": "candidateCount", "response_schema": "responseSchema", "response_json_schema": "responseJsonSchema", "response_mime_type": "responseMimeType", "response_modalities": "responseModalities", "thinking_config": "thinkingConfig", "speech_config": "speechConfig", "audio_timestamp": "audioTimestamp", "response_logprobs": "responseLogprobs", "presence_penalty": "presencePenalty", "frequency_penalty": "frequencyPenalty",
			"output_dimensionality": "outputDimensionality", "task_type": "taskType", "will_continue": "willContinue", "executable_code": "executableCode", "code_execution_result": "codeExecutionResult", "video_metadata": "videoMetadata",
		} {
			if item, ok := v[from]; ok {
				if other, exists := v[to]; exists && !reflect.DeepEqual(item, other) {
					return fmt.Errorf("conflicting %s and %s", from, to)
				}
				v[to] = item
				delete(v, from)
			}
		}
		for key, item := range v {
			switch key {
			case "args", "response", "parameters", "parametersJsonSchema", "responseSchema", "responseJsonSchema", "metadata":
				continue
			}
			if err := normalizeGeminiProtoJSON(item); err != nil {
				return err
			}
		}
	}
	return nil
}

// GeminiToMessages is the inverse of convertClaudeMessagesToGeminiGenerateContent.
// Signatures belong to a Gemini account and are deliberately not exported to
// another provider. Unknown metadata is ignored; unsupported semantic parts fail.
func GeminiToMessages(body []byte, model string, stream bool) ([]byte, error) {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, fmt.Errorf("request must be an object")
	}
	if err := normalizeGeminiProtoJSON(req); err != nil {
		return nil, err
	}
	out := map[string]any{"model": model, "stream": stream, "max_tokens": 4096}
	for _, key := range []string{"cachedContent", "safetySettings"} {
		if v := req[key]; v != nil {
			return nil, fmt.Errorf("%s cannot be represented by the Messages bridge", key)
		}
	}
	if cfg, ok := req["generationConfig"].(map[string]any); ok {
		// The shared Messages -> Responses converter has no stop-sequence or
		// top-k representation. Until that selected-trunk contract is available,
		// fail this request instead of silently removing caller controls.
		if cfg["topK"] != nil {
			return nil, fmt.Errorf("generationConfig.topK cannot be preserved by the shared Messages bridge")
		}
		if stops, ok := cfg["stopSequences"].([]any); ok && len(stops) > 0 {
			return nil, fmt.Errorf("generationConfig.stopSequences cannot be preserved by the shared Messages bridge")
		}
		for from, to := range map[string]string{"maxOutputTokens": "max_tokens", "temperature": "temperature", "topP": "top_p"} {
			if v, exists := cfg[from]; exists {
				out[to] = v
			}
		}
		if n, exists := cfg["candidateCount"]; exists && n != float64(1) {
			return nil, fmt.Errorf("candidateCount must be 1 for this bridge")
		}
		for _, key := range []string{"responseSchema", "responseJsonSchema", "thinkingConfig", "speechConfig", "audioTimestamp", "seed", "presencePenalty", "frequencyPenalty", "logprobs", "responseLogprobs"} {
			if cfg[key] != nil {
				return nil, fmt.Errorf("generationConfig.%s cannot be represented by this bridge", key)
			}
		}
		if mime, _ := cfg["responseMimeType"].(string); mime != "" && mime != "text/plain" {
			return nil, fmt.Errorf("responseMimeType %s cannot be represented by this bridge", mime)
		}
		if modes, ok := cfg["responseModalities"].([]any); ok {
			for _, mode := range modes {
				if mode != "TEXT" {
					return nil, fmt.Errorf("non-text responseModalities cannot be represented by this bridge")
				}
			}
		}
	}
	if sys, exists := req["systemInstruction"]; exists {
		text, err := geminiTextContent(sys)
		if err != nil {
			return nil, fmt.Errorf("systemInstruction: %w", err)
		}
		out["system"] = text
	}
	contents, ok := req["contents"].([]any)
	if !ok || len(contents) == 0 {
		return nil, fmt.Errorf("contents must be a nonempty array")
	}
	pending := map[string]string{}
	seen := map[string]bool{}
	messages := make([]any, 0, len(contents))
	for _, raw := range contents {
		content, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid content")
		}
		role, _ := content["role"].(string)
		switch role {
		case "", "user":
			role = "user"
		case "model":
			role = "assistant"
		default:
			return nil, fmt.Errorf("unsupported content role %q", role)
		}
		parts, ok := content["parts"].([]any)
		if !ok || len(parts) == 0 {
			return nil, fmt.Errorf("content parts must be nonempty")
		}
		blocks := make([]any, 0, len(parts))
		for _, rawPart := range parts {
			part, ok := rawPart.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid part")
			}
			semanticFields := 0
			for _, key := range []string{"text", "functionCall", "functionResponse", "inlineData", "fileData", "executableCode", "codeExecutionResult", "videoMetadata"} {
				if part[key] != nil {
					semanticFields++
				}
			}
			if semanticFields != 1 {
				return nil, fmt.Errorf("part must contain exactly one supported content field")
			}
			switch {
			case part["text"] != nil:
				text, ok := part["text"].(string)
				if !ok {
					return nil, fmt.Errorf("part text must be a string")
				}
				if part["thought"] == true {
					return nil, fmt.Errorf("Gemini thought replay cannot be authenticated by another provider")
				} else {
					blocks = append(blocks, map[string]any{"type": "text", "text": text})
				}
			case part["functionCall"] != nil:
				fc, ok := part["functionCall"].(map[string]any)
				if !ok || role != "assistant" {
					return nil, fmt.Errorf("functionCall requires a model content")
				}
				name, _ := fc["name"].(string)
				if name == "" {
					return nil, fmt.Errorf("functionCall requires name")
				}
				id, _ := fc["id"].(string)
				if id == "" {
					id = geminiFunctionCallID(fc)
				}
				if seen[id] {
					return nil, fmt.Errorf("duplicate functionCall id")
				}
				seen[id] = true
				pending[id] = name
				args := fc["args"]
				if args == nil {
					args = map[string]any{}
				}
				if _, ok := args.(map[string]any); !ok {
					return nil, fmt.Errorf("functionCall args must be an object")
				}
				blocks = append(blocks, map[string]any{"type": "tool_use", "id": id, "name": name, "input": args})
			case part["functionResponse"] != nil:
				fr, ok := part["functionResponse"].(map[string]any)
				if !ok || role != "user" {
					return nil, fmt.Errorf("functionResponse requires a user content")
				}
				name, _ := fr["name"].(string)
				id, _ := fr["id"].(string)
				if id == "" {
					for candidate, n := range pending {
						if n == name {
							if id != "" {
								return nil, fmt.Errorf("ambiguous functionResponse requires id")
							}
							id = candidate
						}
					}
				}
				if id == "" || pending[id] != name {
					return nil, fmt.Errorf("functionResponse does not match a pending functionCall")
				}
				delete(pending, id)
				if fr["parts"] != nil || fr["scheduling"] != nil || fr["willContinue"] != nil {
					return nil, fmt.Errorf("incremental or media functionResponse cannot be represented")
				}
				data, err := json.Marshal(fr["response"])
				if err != nil {
					return nil, err
				}
				blocks = append(blocks, map[string]any{"type": "tool_result", "tool_use_id": id, "content": string(data)})
			case part["inlineData"] != nil:
				data, ok := part["inlineData"].(map[string]any)
				if !ok {
					return nil, fmt.Errorf("invalid inlineData")
				}
				mime, _ := data["mimeType"].(string)
				value, _ := data["data"].(string)
				if !strings.HasPrefix(mime, "image/") || !isValidBase64(value) {
					return nil, fmt.Errorf("only valid inline images are supported by this text bridge")
				}
				blocks = append(blocks, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": mime, "data": value}})
			default:
				return nil, fmt.Errorf("unsupported Gemini content part")
			}
		}
		messages = append(messages, map[string]any{"role": role, "content": blocks})
	}
	out["messages"] = messages
	if tools, ok := req["tools"].([]any); ok {
		var declarations []any
		for _, raw := range tools {
			tool, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid tool")
			}
			fns, ok := tool["functionDeclarations"].([]any)
			if !ok || len(tool) != 1 {
				return nil, fmt.Errorf("only functionDeclarations are supported by this bridge")
			}
			for _, rawFn := range fns {
				fn, ok := rawFn.(map[string]any)
				if !ok || fn["name"] == nil {
					return nil, fmt.Errorf("invalid function declaration")
				}
				if fn["behavior"] != nil {
					return nil, fmt.Errorf("function behavior cannot be represented")
				}
				schema := fn["parametersJsonSchema"]
				if schema == nil {
					schema = fn["parameters"]
				}
				if schema == nil {
					schema = map[string]any{"type": "object"}
				}
				declarations = append(declarations, map[string]any{"name": fn["name"], "description": fn["description"], "input_schema": geminiSchemaToJSON(schema)})
			}
		}
		out["tools"] = declarations
	}
	if config, ok := req["toolConfig"].(map[string]any); ok {
		fc, ok := config["functionCallingConfig"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("unsupported toolConfig")
		}
		mode, _ := fc["mode"].(string)
		choice := map[string]any{}
		switch mode {
		case "", "AUTO":
			choice["type"] = "auto"
		case "NONE":
			choice["type"] = "none"
		case "ANY":
			choice["type"] = "any"
		default:
			return nil, fmt.Errorf("unsupported function calling mode")
		}
		if names, ok := fc["allowedFunctionNames"].([]any); ok && len(names) > 0 {
			if mode != "ANY" || len(names) != 1 {
				return nil, fmt.Errorf("function name subset cannot be represented")
			}
			choice = map[string]any{"type": "tool", "name": names[0]}
		}
		out["tool_choice"] = choice
	}
	return json.Marshal(out)
}

func geminiSchemaToJSON(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			switch k {
			case "type":
				if s, ok := item.(string); ok {
					item = strings.ToLower(s)
				}
			case "properties", "$defs", "definitions", "patternProperties":
				if properties, ok := item.(map[string]any); ok {
					mapped := make(map[string]any, len(properties))
					for name, schema := range properties {
						mapped[name] = geminiSchemaToJSON(schema)
					}
					item = mapped
				}
			case "items", "additionalProperties", "anyOf", "oneOf", "allOf", "not", "prefixItems":
				item = geminiSchemaToJSON(item)
			}
			out[k] = item
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = geminiSchemaToJSON(item)
		}
		return out
	default:
		return value
	}
}

func geminiTextContent(value any) (string, error) {
	content, ok := value.(map[string]any)
	if !ok {
		return "", fmt.Errorf("content must be an object")
	}
	parts, ok := content["parts"].([]any)
	if !ok || len(parts) == 0 {
		return "", fmt.Errorf("parts must be nonempty")
	}
	var text strings.Builder
	for _, raw := range parts {
		part, ok := raw.(map[string]any)
		if !ok {
			return "", fmt.Errorf("invalid part")
		}
		s, ok := part["text"].(string)
		if !ok || part["inlineData"] != nil || part["fileData"] != nil || part["functionCall"] != nil || part["functionResponse"] != nil {
			return "", fmt.Errorf("this action requires text parts")
		}
		text.WriteString(s)
	}
	return text.String(), nil
}

// GeminiToEmbeddings preserves batch order and rejects provider-specific task
// options that the bound OpenAI embedding adapter cannot express.
func GeminiToEmbeddings(body []byte, model string, batch bool) ([]byte, int, error) {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, 0, err
	}
	if err := normalizeGeminiProtoJSON(req); err != nil {
		return nil, 0, err
	}
	requests := []any{req}
	if batch {
		var ok bool
		requests, ok = req["requests"].([]any)
		if !ok || len(requests) == 0 {
			return nil, 0, fmt.Errorf("requests must be nonempty")
		}
	}
	inputs := make([]string, 0, len(requests))
	var dimensions any
	for _, raw := range requests {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, 0, fmt.Errorf("invalid embedding request")
		}
		if m, _ := item["model"].(string); m != "" && strings.TrimPrefix(m, "models/") != strings.TrimPrefix(model, "models/") {
			return nil, 0, fmt.Errorf("batch model must match URL model")
		}
		if item["taskType"] != nil || item["title"] != nil {
			return nil, 0, fmt.Errorf("embedding taskType/title cannot be represented by this adapter")
		}
		if value := item["outputDimensionality"]; value != nil {
			n, ok := value.(float64)
			if !ok || n <= 0 || n != float64(int(n)) {
				return nil, 0, fmt.Errorf("outputDimensionality must be a positive integer")
			}
		}
		if len(inputs) > 0 && item["outputDimensionality"] != dimensions {
			return nil, 0, fmt.Errorf("batch dimensions must match")
		}
		dimensions = item["outputDimensionality"]
		text, err := geminiTextContent(item["content"])
		if err != nil {
			return nil, 0, err
		}
		inputs = append(inputs, text)
	}
	out := map[string]any{"model": model, "input": inputs, "encoding_format": "float"}
	if dimensions != nil {
		out["dimensions"] = dimensions
	}
	data, err := json.Marshal(out)
	return data, len(inputs), err
}
