package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

// PrepareBedrockConverseRequest translates the existing Claude Messages surface
// for non-Claude text/tool models. Unsupported features fail before transport.
func PrepareBedrockConverseRequest(body []byte) ([]byte, error) {
	if !gjson.ValidBytes(body) {
		return nil, fmt.Errorf("invalid Messages JSON")
	}
	request := gjson.ParseBytes(body)
	allowed := map[string]bool{"model": true, "stream": true, "messages": true, "system": true, "max_tokens": true, "temperature": true, "top_p": true, "stop_sequences": true, "tools": true, "tool_choice": true, "metadata": true}
	var unsupported string
	request.ForEach(func(key, value gjson.Result) bool {
		if !allowed[key.String()] {
			unsupported = key.String()
			return false
		}
		return true
	})
	if unsupported != "" {
		return nil, fmt.Errorf("unsupported Converse request field: %s", unsupported)
	}
	messages := request.Get("messages")
	if !messages.IsArray() || len(messages.Array()) == 0 {
		return nil, fmt.Errorf("messages must be a nonempty array")
	}
	converted := make([]any, 0, len(messages.Array()))
	for _, message := range messages.Array() {
		role := message.Get("role").String()
		if role != "user" && role != "assistant" {
			return nil, fmt.Errorf("unsupported Converse message role")
		}
		content, err := bedrockConverseContent(message.Get("content"), role, false)
		if err != nil {
			return nil, err
		}
		converted = append(converted, map[string]any{"role": role, "content": content})
	}
	result := map[string]any{"messages": converted}
	if system := request.Get("system"); system.Exists() {
		content, err := bedrockConverseContent(system, "system", true)
		if err != nil {
			return nil, err
		}
		result["system"] = content
	}
	inference := make(map[string]any)
	for source, target := range map[string]string{"max_tokens": "maxTokens", "temperature": "temperature", "top_p": "topP", "stop_sequences": "stopSequences"} {
		if value := request.Get(source); value.Exists() {
			inference[target] = value.Value()
		}
	}
	if len(inference) > 0 {
		result["inferenceConfig"] = inference
	}
	if tools := request.Get("tools"); tools.Exists() {
		if !tools.IsArray() {
			return nil, fmt.Errorf("tools must be an array")
		}
		definitions := make([]any, 0, len(tools.Array()))
		for _, tool := range tools.Array() {
			if tool.Get("type").Exists() || tool.Get("name").String() == "" || !tool.Get("input_schema").IsObject() {
				return nil, fmt.Errorf("unsupported Converse tool definition")
			}
			spec := map[string]any{"name": tool.Get("name").String(), "inputSchema": map[string]any{"json": tool.Get("input_schema").Value()}}
			if description := tool.Get("description"); description.Exists() {
				spec["description"] = description.String()
			}
			definitions = append(definitions, map[string]any{"toolSpec": spec})
		}
		if len(definitions) > 0 {
			result["toolConfig"] = map[string]any{"tools": definitions}
		}
	}
	if choice := request.Get("tool_choice"); choice.Exists() {
		config, ok := result["toolConfig"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tool_choice requires tools")
		}
		if choice.Get("disable_parallel_tool_use").Exists() {
			return nil, fmt.Errorf("parallel tool control unsupported by Converse adapter")
		}
		switch choice.Get("type").String() {
		case "auto", "any":
			config["toolChoice"] = map[string]any{choice.Get("type").String(): map[string]any{}}
		case "tool":
			if choice.Get("name").String() == "" {
				return nil, fmt.Errorf("tool_choice requires name")
			}
			config["toolChoice"] = map[string]any{"tool": map[string]any{"name": choice.Get("name").String()}}
		default:
			return nil, fmt.Errorf("unsupported Converse tool choice")
		}
	}
	return json.Marshal(result)
}

func bedrockConverseContent(content gjson.Result, role string, textOnly bool) ([]any, error) {
	if content.Type == gjson.String {
		return []any{map[string]any{"text": content.String()}}, nil
	}
	if !content.IsArray() || len(content.Array()) == 0 {
		return nil, fmt.Errorf("content must be text or nonempty blocks")
	}
	blocks := make([]any, 0, len(content.Array()))
	for _, block := range content.Array() {
		if block.Get("cache_control").Exists() {
			return nil, fmt.Errorf("converse cache control is not supported")
		}
		switch block.Get("type").String() {
		case "text":
			if block.Get("text").Type != gjson.String {
				return nil, fmt.Errorf("text block requires text")
			}
			blocks = append(blocks, map[string]any{"text": block.Get("text").String()})
		case "tool_use":
			if textOnly || role != "assistant" || block.Get("id").String() == "" || block.Get("name").String() == "" || !block.Get("input").IsObject() {
				return nil, fmt.Errorf("invalid Converse tool use")
			}
			blocks = append(blocks, map[string]any{"toolUse": map[string]any{"toolUseId": block.Get("id").String(), "name": block.Get("name").String(), "input": block.Get("input").Value()}})
		case "tool_result":
			if textOnly || role != "user" || block.Get("tool_use_id").String() == "" {
				return nil, fmt.Errorf("invalid Converse tool result")
			}
			content, err := bedrockConverseContent(block.Get("content"), "user", true)
			if err != nil {
				return nil, err
			}
			status := "success"
			if block.Get("is_error").Bool() {
				status = "error"
			}
			blocks = append(blocks, map[string]any{"toolResult": map[string]any{"toolUseId": block.Get("tool_use_id").String(), "content": content, "status": status}})
		default:
			return nil, fmt.Errorf("unsupported Converse content block")
		}
	}
	return blocks, nil
}

// BuildBedrockAdapterRequest takes the already resolved upstream target. It never
// rewrites profile IDs or ARNs or derives authorization from catalogue entries.
// Claude callers explicitly select Invoke to preserve the existing preparation.
func BuildBedrockAdapterRequest(ctx context.Context, account *Account, target string, body []byte, stream, claudeInvoke bool, betaHeader string) (*http.Request, error) {
	if account == nil || strings.TrimSpace(target) == "" {
		return nil, fmt.Errorf("bedrock account and target required")
	}
	endpoint, err := bedrockEndpoint(bedrockRuntimeRegion(account), true)
	if err != nil {
		return nil, err
	}
	operation := "converse"
	var prepared []byte
	if claudeInvoke {
		operation = "invoke"
		if stream {
			operation = "invoke-with-response-stream"
		}
		if !gjson.ValidBytes(body) {
			return nil, fmt.Errorf("invalid Messages JSON")
		}
		prepared, err = PrepareBedrockRequestBody(body, target, betaHeader)
	} else {
		if betaHeader != "" {
			return nil, fmt.Errorf("anthropic beta unsupported for Converse")
		}
		if stream {
			operation = "converse-stream"
		}
		prepared, err = PrepareBedrockConverseRequest(body)
	}
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/model/"+bedrockEscapeTarget(target)+"/"+operation, bytes.NewReader(prepared))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if stream {
		request.Header.Set("Accept", "application/vnd.amazon.eventstream")
	}
	if apiKey := account.GetCredential("api_key"); apiKey != "" && (account.Platform != PlatformBedrock || account.IsBedrockAPIKey()) {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	} else {
		signer, err := NewBedrockSignerFromAccount(account)
		if err != nil {
			return nil, err
		}
		if err := signer.SignRequest(ctx, request, prepared); err != nil {
			return nil, err
		}
	}
	return request, nil
}

// Validate at the Converse boundary, without changing legacy Invoke parsing.
// Missing usage is unknown, never a known zero. Optional cache/total counters
// must satisfy the same integer contract when supplied.
func bedrockConverseUsage(usage gjson.Result) (map[string]any, error) {
	if !usage.IsObject() {
		return nil, fmt.Errorf("missing Converse usage")
	}
	result := make(map[string]any)
	for source, target := range map[string]string{"inputTokens": "input_tokens", "outputTokens": "output_tokens", "totalTokens": "total_tokens", "cacheReadInputTokens": "cache_read_input_tokens", "cacheWriteInputTokens": "cache_creation_input_tokens"} {
		value := usage.Get(source)
		if !value.Exists() && source != "inputTokens" && source != "outputTokens" {
			continue
		}
		// Parse the raw JSON integer to avoid floating-point rounding accepting
		// fractional or overflowing values before ClaudeUsage converts to int.
		count, err := strconv.ParseInt(value.Raw, 10, strconv.IntSize)
		if value.Type != gjson.Number || err != nil || count < 0 {
			return nil, fmt.Errorf("invalid Converse usage counter: %s", source)
		}
		result[target] = count
	}
	return result, nil
}

func bedrockConverseStopReason(reason string) (string, error) {
	switch reason {
	case "end_turn", "tool_use", "max_tokens", "stop_sequence":
		return reason, nil
	case "guardrail_intervened", "content_filtered":
		return "refusal", nil
	default:
		return "", fmt.Errorf("unsupported Converse stop reason: %s", reason)
	}
}

// ConvertBedrockConverseResponse returns the existing Messages response surface;
// the caller supplies its request ID and requested (client-visible) model ID.
func ConvertBedrockConverseResponse(body []byte, requestID, model string) ([]byte, error) {
	if !gjson.ValidBytes(body) {
		return nil, fmt.Errorf("invalid Converse response")
	}
	response := gjson.ParseBytes(body)
	message := response.Get("output.message")
	if message.Get("role").String() != "assistant" || !message.Get("content").IsArray() {
		return nil, fmt.Errorf("missing Converse assistant output")
	}
	content := make([]any, 0)
	for _, block := range message.Get("content").Array() {
		if text := block.Get("text"); text.Type == gjson.String {
			content = append(content, map[string]any{"type": "text", "text": text.String()})
		} else if tool := block.Get("toolUse"); tool.IsObject() && tool.Get("toolUseId").String() != "" && tool.Get("name").String() != "" && tool.Get("input").IsObject() {
			content = append(content, map[string]any{"type": "tool_use", "id": tool.Get("toolUseId").String(), "name": tool.Get("name").String(), "input": tool.Get("input").Value()})
		} else {
			return nil, fmt.Errorf("unsupported Converse response content")
		}
	}
	stopReason, err := bedrockConverseStopReason(response.Get("stopReason").String())
	if err != nil {
		return nil, err
	}
	result := map[string]any{"id": requestID, "type": "message", "role": "assistant", "model": model, "content": content, "stop_reason": stopReason, "stop_sequence": nil}
	usage, err := bedrockConverseUsage(response.Get("usage"))
	if err != nil {
		return nil, err
	}
	result["usage"] = usage
	return json.Marshal(result)
}
