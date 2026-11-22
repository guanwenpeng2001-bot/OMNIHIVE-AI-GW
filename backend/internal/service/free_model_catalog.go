package service

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"

	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
	"github.com/tidwall/gjson"
)

var zenPriceNumber = regexp.MustCompile(`^[+-]?(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)
var zenFreeName = regexp.MustCompile(`(?i)(?:^|[-_])free(?:$|[-_.])`)
var zenDecisionName = regexp.MustCompile(`(?i)^(jev|clef)(-|$)`)

func safeFreeModelID(id string) bool {
	return id != "" && id == strings.TrimSpace(id) && len(id) <= 256 && !strings.ContainsAny(id, "\r\n\t ?#") && id != "__proto__"
}

// A malformed or nonzero price cannot accidentally grant free access. Numbers
// are checked by their decimal mantissa, including strings/exponents; underflow
// must not turn a very small positive price into zero.
func zenCostVerdict(v any, depth int) (priced, zero bool) {
	if depth > 32 {
		return true, false
	}
	if v == nil {
		return false, true
	}
	if m, ok := v.(map[string]any); ok {
		zero = true
		for _, value := range m {
			p, z := zenCostVerdict(value, depth+1)
			priced = priced || p
			zero = zero && z
		}
		return
	}
	var value string
	switch n := v.(type) {
	case json.Number:
		value = n.String()
	case string:
		value = strings.TrimSpace(n)
	default:
		return true, false
	}
	if len(value) > 128 {
		return true, false
	}
	if !zenPriceNumber.MatchString(value) {
		return true, false
	}
	mantissa := strings.FieldsFunc(value, func(r rune) bool { return r == 'e' || r == 'E' })[0]
	return true, !strings.ContainsAny(mantissa, "123456789")
}
func zenFreeRow(raw json.RawMessage) bool {
	b, err := decodeFreeModelObject(raw)
	if err != nil {
		return false
	}
	id := freeString(b["id"])
	if !safeFreeModelID(id) {
		return false
	}
	base := normalizeOpenCodeGoModelID(id)
	base = regexp.MustCompile(`\([^()]+\)\s*$`).ReplaceAllString(base, "")
	if !activeFreeRow(b) {
		return false
	}
	if strings.EqualFold(freeString(b["recommended_api"]), "systemone") || zenDecisionName.MatchString(base) {
		return false
	}
	priced, zero := zenCostVerdict(b["cost"], 0)
	if !zero {
		return false
	}
	return priced || base == "big-pickle" || base == "union-alpha" || zenFreeName.MatchString(base)
}
func freeModalities(values []string) []string {
	if values == nil {
		return nil
	}
	out := []string{}
	for _, v := range values {
		v = strings.ToLower(strings.TrimSpace(v))
		switch {
		case v == "application/pdf" || v == "file":
			v = "pdf"
		case strings.HasPrefix(v, "image/"):
			v = "image"
		case strings.HasPrefix(v, "audio/"):
			v = "audio"
		case strings.HasPrefix(v, "video/"):
			v = "video"
		case strings.HasPrefix(v, "text/"):
			v = "text"
		}
		if v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}
func extractAccountModelCatalog(body []byte, a *Account) ([]string, map[string]UpstreamModelMetadata, error) {
	if !a.IsOpenCodeZenFree() && a.Platform != PlatformKilo {
		return extractUpstreamModelCatalog(body, a.IsGrok(), a.Platform, workBuddySiteFor(a).ID)
	}
	entries, err := extractUpstreamModelRawEntries(body)
	if err != nil {
		return nil, nil, err
	}
	ids := []string{}
	metadata := map[string]UpstreamModelMetadata{}
	rejected := map[string]bool{}
	// Contradictory duplicate rows fail closed, independent of listing order.
	for _, raw := range entries {
		id := gjson.GetBytes(raw, "id").String()
		if a.IsOpenCodeZenFree() && !zenFreeRow(raw) || a.IsKiloFree() && !kiloFreeRow(raw) {
			rejected[id] = true
		}
	}
	for _, raw := range entries {
		id := gjson.GetBytes(raw, "id").String()
		if !safeFreeModelID(id) || rejected[id] {
			continue
		}
		if _, exists := metadata[id]; exists {
			continue
		}
		var row struct {
			Name         string                                 `json:"name"`
			IsFree       *bool                                  `json:"isFree"`
			Context      int64                                  `json:"context_length"`
			Limit        struct{ Context, Input, Output int64 } `json:"limit"`
			Modalities   struct{ Input, Output []string }       `json:"modalities"`
			Architecture struct {
				Input  []string `json:"input_modalities"`
				Output []string `json:"output_modalities"`
			} `json:"architecture"`
			Top struct {
				Output int64 `json:"max_completion_tokens"`
			} `json:"top_provider"`
			Parameters   []string                                               `json:"supported_parameters"`
			Capabilities struct{ Reasoning, Tools upstreamSupportedCapability } `json:"capabilities"`
			Recommended  string                                                 `json:"recommended_api"`
			Pricing      map[string]json.RawMessage                             `json:"pricing"`
		}
		invalid := json.Unmarshal(raw, &row) != nil
		m := UpstreamModelMetadata{ID: id, DisplayName: firstNonEmpty(row.Name, id), TaskType: "text_generation", ContextLimitKind: "total", ProviderProfile: resolveCapabilityProfile(a)}
		if a.Platform == PlatformKilo {
			m.ContextWindow = row.Context
			m.MaxOutputTokens = row.Top.Output
			m.InputModalities = freeModalities(row.Architecture.Input)
			m.OutputModalities = freeModalities(row.Architecture.Output)
			m.SupportedParameters = row.Parameters
			m.SupportedMethods = []string{"chat_completions"}
			m.Free = row.IsFree
			m.Pricing = row.Pricing
			if row.Parameters != nil {
				reasoning, tools := slices.Contains(row.Parameters, "reasoning"), slices.Contains(row.Parameters, "tools")
				m.Reasoning = &reasoning
				m.SupportsFunctionCalling = &tools
			}
			if m.Reasoning != nil && *m.Reasoning {
				m.SupportedReasoningLevels = []string{"low", "medium", "high"}
				if !kiloMandatoryReasoning(id) {
					m.SupportedReasoningLevels = append(m.SupportedReasoningLevels, "disabled")
				}
			}
		} else {
			m.ContextWindow = row.Limit.Context
			m.MaxInputTokens = row.Limit.Input
			m.MaxOutputTokens = row.Limit.Output
			m.InputModalities = freeModalities(row.Modalities.Input)
			m.OutputModalities = freeModalities(row.Modalities.Output)
			m.Reasoning = row.Capabilities.Reasoning.Supported
			m.SupportsFunctionCalling = row.Capabilities.Tools.Supported
			free := true
			m.Free = &free
			switch row.Recommended {
			case "responses":
				m.SupportedMethods = []string{"responses"}
			case "anthropic", "messages":
				m.SupportedMethods = []string{"messages"}
			case "chat", "chat_completions", "chat/completions":
				m.SupportedMethods = []string{"chat_completions"}
			}
		}
		if m.InputModalities != nil {
			vision, pdf := slices.Contains(m.InputModalities, "image"), slices.Contains(m.InputModalities, "pdf")
			m.SupportsVision = &vision
			m.SupportsPDF = &pdf
		}
		if invalid {
			m.Conflicts = []string{"invalid_optional_metadata"}
		}
		ids = append(ids, id)
		metadata[id] = m
	}
	return dedupeAndSortModelIDs(ids), metadata, nil
}
func buildZenFreeModelsRequest(ctx context.Context, a *Account, validate func(string) (string, error)) (*http.Request, error) {
	candidate := *a
	if err := NormalizeFreeModelCredentials(&candidate); err != nil {
		if infraerrors.Reason(err) == "INVALID_PUBLIC_CREDENTIAL" {
			return nil, newUpstreamModelSyncCredentialError("Invalid Zen Free credential", err)
		}
		return nil, newUpstreamModelSyncConfigError("Invalid Zen Free configuration", err)
	}
	base, err := validate(a.GetOpenAIFormatBaseURL())
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid Zen Free base URL", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildOpenAIModelsURL(base), nil)
	if err != nil {
		return nil, err
	}
	a.ApplyHeaderOverrides(req.Header)
	applyZenFreeDiscoveryHeaders(req.Header)
	return req, nil
}
func (s *AccountTestService) buildKiloModelsRequest(ctx context.Context, a *Account) (*http.Request, error) {
	candidate := *a
	if err := NormalizeFreeModelCredentials(&candidate); err != nil {
		if infraerrors.Reason(err) == "INVALID_KILO_CREDENTIAL" {
			return nil, newUpstreamModelSyncCredentialError("Invalid Kilo credentials", err)
		}
		return nil, newUpstreamModelSyncConfigError("Invalid Kilo configuration", err)
	}
	endpoint, err := kiloEndpoint(a, "/models", s.validateUpstreamBaseURL)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid Kilo base URL", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	a.ApplyHeaderOverrides(req.Header)
	applyKiloHeaders(req.Header, a)
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func activeFreeRow(row map[string]any) bool {
	if row["isFree"] == false || row["deprecated"] == true || row["deprecated_at"] != nil || row["retirement_date"] != nil {
		return false
	}
	for _, key := range []string{"status", "lifecycle"} {
		if slices.Contains([]string{"deprecated", "retired", "disabled"}, strings.ToLower(freeString(row[key]))) {
			return false
		}
	}
	return true
}

func kiloFreeRow(raw json.RawMessage) bool {
	row, err := decodeFreeModelObject(raw)
	if err != nil || row["isFree"] != true || !activeFreeRow(row) {
		return false
	}
	// Explicit nonzero or malformed price evidence overrides the free flag.
	for _, key := range []string{"pricing", "cost"} {
		if _, zero := zenCostVerdict(row[key], 0); !zero {
			return false
		}
	}
	return true
}
