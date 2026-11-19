package apicompat

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/gateway/protocol"
)

func NormalizeGeminiRequestForAIStudio(body []byte) []byte {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}

	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) == 0 {
		return body
	}

	modified := false
	for _, rawTool := range tools {
		tool, ok := rawTool.(map[string]any)
		if !ok {
			continue
		}
		googleSearch, ok := tool["googleSearch"]
		if !ok {
			continue
		}
		if _, exists := tool["google_search"]; exists {
			continue
		}
		tool["google_search"] = googleSearch
		delete(tool, "googleSearch")
		modified = true
	}

	if !modified {
		return body
	}

	normalized, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return normalized
}

func MessagesToGeminiPlan(body []byte) protocol.ConversionPlan {
	plan := protocol.ConversionPlan{
		Mode: protocol.Bridge, SourceProtocol: protocol.AnthropicMessages, TargetProtocol: protocol.GeminiGenerate,
		Diagnostics: []protocol.Diagnostic{},
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return plan
	}
	for field := range fields {
		switch field {
		case "model", "stream", "messages", "system", "tools", "tool_choice",
			"max_tokens", "temperature", "top_p", "top_k", "output_config", "thinking", "stop_sequences":
			continue
		}
		field = strings.ReplaceAll(strings.ReplaceAll(field, "~", "~0"), "/", "~1")
		plan.Diagnostics = append(plan.Diagnostics, protocol.Diagnostic{
			Field: "/" + field, Reason: "field_not_forwarded", Severity: "warning",
		})
	}
	sort.Slice(plan.Diagnostics, func(i, j int) bool {
		return plan.Diagnostics[i].Field < plan.Diagnostics[j].Field
	})
	return plan
}
