package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func deleteFreeModelHeader(h http.Header, name string) {
	for k := range h {
		if strings.EqualFold(k, name) {
			delete(h, k)
		}
	}
}
func applyKiloHeaders(h http.Header, a *Account) {
	deleteFreeModelHeader(h, "Authorization")
	deleteFreeModelHeader(h, "X-API-Key")
	if !a.IsKiloFree() {
		h.Set("Authorization", "Bearer "+strings.TrimSpace(a.GetCredential("api_key")))
	}
	deleteFreeModelHeader(h, "User-Agent")
	h.Set("User-Agent", "omnihive-kilo/1.0")
	if h.Get("Accept") == "" {
		h.Set("Accept", "application/json")
	}
}
func kiloMandatoryReasoning(id string) bool {
	return id == "kilo-auto/free" || id == "openrouter/free" || strings.HasPrefix(id, "stepfun/") || strings.HasPrefix(id, "liquid/") || strings.HasPrefix(id, "thinkingmachines/")
}
func kiloFreeModelAllowed(a *Account, model string) bool {
	if !a.IsKiloFree() {
		return true
	}
	// Persisted, identity-bound availability takes precedence over bootstrap IDs.
	if catalog := a.GetUpstreamModelCatalog(); catalog != nil && catalog.Complete && catalog.IdentityRevision == a.ModelCatalogBaseline().IdentityRevision {
		if !slices.Contains(catalog.DiscoveredModels, model) {
			return false
		}
		if m, ok := a.GetUpstreamModelMetadata(model); ok && m.Free != nil {
			return *m.Free
		}
		// Discovery on this identity admits only rows whose isFree is true.
		return true
	}
	if m, ok := a.GetUpstreamModelMetadata(model); ok && m.Free != nil {
		return *m.Free
	}
	if slices.Contains(kiloFreeModelIDs, model) {
		return true
	}
	return strings.HasSuffix(model, ":free") || strings.HasSuffix(model, "/free")
}
func prepareKiloChat(a *Account, body []byte) ([]byte, string, error) {
	b, err := decodeFreeModelObject(body)
	if err != nil {
		return nil, "", err
	}
	model, _ := b["model"].(string)
	if !safeFreeModelID(model) || !kiloFreeModelAllowed(a, model) {
		return nil, model, fmt.Errorf("%w: model is not in Kilo's free pool", ErrAccountModelUnsupported)
	}
	if stream, _ := b["stream"].(bool); stream {
		options, _ := b["stream_options"].(map[string]any)
		if options == nil {
			options = map[string]any{}
		}
		options["include_usage"] = true
		b["stream_options"] = options
	}
	reasoning, _ := b["reasoning"].(map[string]any)
	effort, _ := b["reasoning_effort"].(string)
	if effort != "" {
		if reasoning == nil {
			reasoning = map[string]any{}
		}
		if effort == "disabled" || effort == "none" {
			reasoning["enabled"] = false
			delete(reasoning, "effort")
		} else {
			reasoning["effort"] = effort
			reasoning["enabled"] = true
		}
		delete(b, "reasoning_effort")
	}
	if kiloMandatoryReasoning(model) && reasoning != nil {
		if reasoning["effort"] == "disabled" || reasoning["effort"] == "none" {
			delete(reasoning, "effort")
		}
		if reasoning["enabled"] == false {
			delete(reasoning, "enabled")
		}
	}
	if len(reasoning) > 0 {
		b["reasoning"] = reasoning
	} else {
		delete(b, "reasoning")
	}
	out, err := json.Marshal(b)
	return out, model, err
}

// Clamp only the documented router defect. Cost decimals, output and reasoning
// counters remain byte-for-byte untouched; every bridge meters the same corrected
// input count. Do not invent a successful usage report when counters are absent.
func normalizeKiloPayload(body []byte, model string) []byte {
	if !gjson.ValidBytes(body) {
		return body
	}
	if model == "kilo-auto/free" && gjson.GetBytes(body, "usage.prompt_tokens").Int() > 256000 {
		body, _ = sjson.SetBytes(body, "usage.prompt_tokens", 256000)
		if gjson.GetBytes(body, "usage.total_tokens").Exists() && gjson.GetBytes(body, "usage.completion_tokens").Exists() {
			body, _ = sjson.SetBytes(body, "usage.total_tokens", 256000+gjson.GetBytes(body, "usage.completion_tokens").Int())
		}
		// Cached input is a subset of prompt input, never a larger billable bucket.
		for _, path := range []string{"usage.prompt_tokens_details.cached_tokens", "usage.prompt_tokens_details.cache_write_tokens"} {
			if gjson.GetBytes(body, path).Int() > 256000 {
				body, _ = sjson.SetBytes(body, path, 256000)
			}
		}
	}
	if detail := kiloErrorDetail(body); detail != "" {
		body, _ = sjson.SetBytes(body, "error.message", detail)
		// Do not expose an unsanitized embedded provider response in passthrough.
		body, _ = sjson.DeleteBytes(body, "error.metadata.raw")
		body, _ = sjson.DeleteBytes(body, "metadata.raw")
	}
	return body
}

var kiloSecretPattern = regexp.MustCompile(`(?i)(bearer\s+|(?:api[_-]?key|token|authorization)\s*[:=]\s*["']?)[^\s"',;]+`)

func kiloErrorDetail(body []byte) string {
	raw := gjson.GetBytes(body, "error.metadata.raw").String()
	if raw == "" {
		raw = gjson.GetBytes(body, "metadata.raw").String()
	}
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	detail := raw
	if gjson.Valid(raw) {
		detail = ""
		for _, path := range []string{"error.message", "message", "error"} {
			v := gjson.Get(raw, path)
			if v.Type == gjson.String {
				detail = v.String()
				break
			}
		}
	}
	message := strings.TrimSpace(gjson.GetBytes(body, "error.message").String())
	if detail != "" {
		if message != "" {
			message += ": "
		}
		message += detail
	}
	message = sanitizeUpstreamErrorMessage(message)
	message = kiloSecretPattern.ReplaceAllString(message, "${1}[redacted]")
	message = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, message)
	message = strings.Join(strings.Fields(message), " ")
	chars := []rune(message)
	if len(chars) > 200 {
		message = string(chars[:200])
	}
	return message
}
