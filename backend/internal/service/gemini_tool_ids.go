package service

import (
	"errors"
	"net/url"
	"strings"
)

type geminiToolIDProfile string

const (
	geminiToolIDNative  geminiToolIDProfile = "native"
	geminiToolIDLegacy  geminiToolIDProfile = "legacy_no_id"
	geminiToolIDUnknown geminiToolIDProfile = "unknown"
)

// Explicit account metadata overrides the transport defaults. Existing Code
// Assist, Vertex and custom endpoint accounts retain legacy ID-free requests
// without requiring new metadata. The public Gemini v1beta schema supports IDs:
// https://ai.google.dev/api/generate-content#FunctionCall
func geminiToolIDProfileForAccount(account *Account) geminiToolIDProfile {
	if account == nil {
		return geminiToolIDUnknown
	}
	if value, exists := account.Extra["gemini_tool_id_profile"]; exists {
		switch value {
		case string(geminiToolIDNative):
			return geminiToolIDNative
		case string(geminiToolIDLegacy):
			return geminiToolIDLegacy
		}
		// Unknown optional metadata must not revoke the transport's established
		// tool support. Fall back to the same profile as an existing account.
	}
	// Preserve the legacy wire format where native ID support is not established.
	// This is a compatibility fallback, not a claim of native schema support.
	if account.Type == AccountTypeServiceAccount || (account.Type == AccountTypeOAuth && strings.TrimSpace(account.GetCredential("project_id")) != "") {
		return geminiToolIDLegacy
	}
	if account.Type != AccountTypeAPIKey && account.Type != AccountTypeOAuth {
		return geminiToolIDUnknown
	}
	base := strings.TrimSpace(account.GetCredential("base_url"))
	if base == "" {
		return geminiToolIDNative
	}
	u, err := url.Parse(base)
	if err == nil && u.Scheme == "https" && u.Host == "generativelanguage.googleapis.com" && (u.Path == "" || u.Path == "/") {
		return geminiToolIDNative
	}
	return geminiToolIDLegacy
}

func geminiFunctionCallID(call map[string]any) string {
	if id, _ := call["id"].(string); id != "" {
		return id
	}
	return "toolu_" + randomHex(8)
}

// A stream may continue an explicitly identified open call. Reopening a closed
// ID, changing its name, or sending unidentified argument fragments is ambiguous.
func validateGeminiStreamToolCall(call map[string]any, openID, openName string, seen map[string]bool) error {
	id, _ := call["id"].(string)
	name, _ := call["name"].(string)
	if id == "" {
		if _, fragmented := call["args"].(string); fragmented {
			return errors.New("ambiguous Gemini tool argument fragment without an id")
		}
		return nil
	}
	if seen[id] && (openID != id || openName != name) {
		return errors.New("ambiguous Gemini tool stream: repeated or mismatched call id")
	}
	seen[id] = true
	return nil
}

func validateGeminiResponseToolIDs(response map[string]any) error {
	seen := map[string]bool{}
	for _, part := range extractGeminiParts(response) {
		call, ok := part["functionCall"].(map[string]any)
		if !ok {
			continue
		}
		if err := validateGeminiStreamToolCall(call, "", "", seen); err != nil {
			return err
		}
	}
	return nil
}

// prepareGeminiToolIDs operates only on the freshly converted request. IDs stay
// in the local call/result maps until serialization. Legacy result order is
// reconstructed from these IDs, never from names or argument equality. There is
// deliberately no process-global or cross-request correlation cache.
func prepareGeminiToolIDs(req map[string]any, profile geminiToolIDProfile) error {
	known := profile == geminiToolIDNative || profile == geminiToolIDLegacy
	if tools, ok := req["tools"].([]any); ok && !known {
		for _, raw := range tools {
			tool, _ := raw.(map[string]any)
			if declarations, ok := tool["functionDeclarations"].([]any); ok && len(declarations) > 0 {
				return errors.New("unknown Gemini tool ID profile; configure gemini_tool_id_profile")
			}
		}
	}
	contents, _ := req["contents"].([]any)
	pending := map[string]map[string]any{}
	var order []string
	var strip []map[string]any
	for _, raw := range contents {
		content, _ := raw.(map[string]any)
		parts, _ := content["parts"].([]any)
		results := map[string]any{}
		var slots []int
		priorPending := len(pending)
		for i, rawPart := range parts {
			part, _ := rawPart.(map[string]any)
			call, isCall := part["functionCall"].(map[string]any)
			result, isResult := part["functionResponse"].(map[string]any)
			if !isCall && !isResult {
				continue
			}
			if !known {
				return errors.New("unknown Gemini tool ID profile; configure gemini_tool_id_profile")
			}
			if isCall {
				if profile == geminiToolIDLegacy && priorPending > 0 {
					return errors.New("ambiguous legacy Gemini call batch: previous calls are unresolved")
				}
				id, _ := call["id"].(string)
				if id == "" || pending[id] != nil {
					return errors.New("missing or duplicate Gemini function call id")
				}
				pending[id] = call
				order = append(order, id)
				strip = append(strip, call)
			}
			if isResult {
				id, _ := result["id"].(string)
				call := pending[id]
				if call == nil || results[id] != nil || call["name"] != result["name"] {
					return errors.New("unknown, duplicate or mismatched Gemini function response id")
				}
				results[id] = rawPart
				slots = append(slots, i)
				strip = append(strip, result)
			}
		}
		if len(results) > 0 {
			if profile == geminiToolIDLegacy {
				if len(results) != len(pending) {
					return errors.New("ambiguous legacy Gemini tool results: a complete call batch is required")
				}
				for i, id := range order {
					parts[slots[i]] = results[id]
				}
			}
			remaining := order[:0]
			for _, id := range order {
				if results[id] != nil {
					delete(pending, id)
				} else {
					remaining = append(remaining, id)
				}
			}
			order = remaining
		}
	}
	if profile == geminiToolIDLegacy {
		for _, function := range strip {
			delete(function, "id")
		}
	}
	return nil
}
