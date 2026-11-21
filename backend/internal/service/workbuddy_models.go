package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
)

type workBuddyModel struct {
	ID, Name      string
	Input, Output int64
	Efforts       []string
}

var workBuddyCNModels = []workBuddyModel{
	{"auto", "Auto", 168000, 0, nil}, {"hy4-preview-f", "Hy4 preview", 1000000, 0, []string{"high"}}, {"hy3", "Hy3", 192000, 0, []string{"low", "high"}}, {"hy3-x", "Hy3-X", 192000, 0, []string{"low", "high"}},
	{"deepseek-v4.1-flash", "Deepseek-V4.1-Flash", 1000000, 128000, []string{"none", "low", "high", "max"}}, {"glm-5.3", "GLM-5.3", 1000000, 0, []string{"low", "high", "max"}}, {"glm-5.3-flash", "GLM-5.3-Flash", 1000000, 131072, []string{"low", "high", "max"}}, {"glm-5.2", "GLM-5.2", 1000000, 0, []string{"high", "xhigh"}},
	{"glm-5.1", "GLM-5.1", 200000, 0, nil}, {"glm-5v-turbo", "GLM-5v-Turbo", 200000, 0, nil}, {"minimax-m3", "MiniMax-M3", 512000, 0, nil}, {"kimi-k3-1", "Kimi-K3", 1000000, 0, []string{"low", "high", "xhigh"}}, {"kimi-k2.7", "Kimi-K2.7-Code", 256000, 0, nil}, {"kimi-k2.6", "Kimi-K2.6", 256000, 0, nil}, {"deepseek-v4-pro", "Deepseek-V4-Pro", 1000000, 128000, []string{"none", "high", "xhigh"}},
}
var workBuddyGlobalModels = []workBuddyModel{
	{"default-model", "Default", 176000, 24000, nil}, {"fast-model", "Fast", 200000, 32000, nil}, {"balanced-model", "Balanced", 256000, 0, nil}, {"primary-model", "Primary", 272000, 72000, nil}, {"deep-model", "Deep", 176000, 0, nil}, {"gpt-5.5", "GPT-5.5", 1000000, 0, nil}, {"gpt-5.4", "GPT-5.4", 272000, 0, nil}, {"gpt-5.3-codex", "GPT-5.3-Codex", 272000, 0, nil}, {"gemini-3.1-pro", "Gemini-3.1-Pro", 400000, 0, nil}, {"gemini-3.5-flash", "Gemini-3.5-Flash", 1000000, 65536, nil}, {"glm-5.3", "GLM-5.3", 1000000, 0, []string{"low", "high", "max"}}, {"glm-5.2", "GLM-5.2", 1000000, 48000, []string{"high", "xhigh"}}, {"hy3", "Hy3", 192000, 64000, []string{"low", "high"}}, {"kimi-k3", "Kimi-K3", 1000000, 32000, nil}, {"kimi-k2.6", "Kimi-K2.6", 256000, 0, nil}, {"minimax-m3", "MiniMax-M3", 512000, 0, nil},
}

func workBuddyModels(site string) []workBuddyModel {
	if site == "global" {
		return workBuddyGlobalModels
	}
	return workBuddyCNModels
}
func workBuddyStaticModelMetadata(site string) ([]string, map[string]UpstreamModelMetadata) {
	ids := []string{}
	metadata := map[string]UpstreamModelMetadata{}
	for _, m := range workBuddyModels(site) {
		ids = append(ids, m.ID)
		vision := !(site == "global" && strings.HasPrefix(m.ID, "glm-"))
		reasoning := len(m.Efforts) > 0
		modalities := []string{"text"}
		if vision {
			modalities = append(modalities, "image")
		}
		metadata[m.ID] = UpstreamModelMetadata{ID: m.ID, DisplayName: m.Name, ContextWindow: m.Input, MaxOutputTokens: m.Output, ContextLimitKind: "input", SupportsVision: &vision, InputModalities: modalities, OutputModalities: []string{"text"}, Reasoning: &reasoning, SupportedReasoningLevels: append([]string(nil), m.Efforts...), ProviderProfile: "workbuddy-" + site, SupportedMethods: []string{"chat_completions"}}
	}
	return ids, metadata
}

// Adapter presets are explicit source candidates, separate from the signed
// registry bundle. They never grant a credential access to a model.
func workBuddyModelPresets() []PlatformModelPreset {
	out := []PlatformModelPreset{}
	for _, site := range []string{"cn", "global"} {
		p := PlatformModelPreset{Platform: PlatformWorkBuddy, ProviderProfile: "workbuddy-" + site, Revision: "opencode-workbuddy-auth-0.2.37", CoverageStatus: "partial", CheckedAt: "2026-11-22T00:00:00Z", SourceRefs: []string{"@magpie-community/opencode-workbuddy-auth@0.2.37/packages/workbuddy/index.mjs"}, Gaps: []string{"Account-specific /v3/config may advertise additional or fewer models"}}
		for _, m := range workBuddyModels(site) {
			modalities := []string{"text"}
			if !(site == "global" && strings.HasPrefix(m.ID, "glm-")) {
				modalities = append(modalities, "image")
			}
			p.Models = append(p.Models, PlatformPresetModel{ID: m.ID, DisplayName: m.Name, TaskType: "chat", InputModalities: modalities, OutputModalities: []string{"text"}, Methods: []string{"chat_completions", "messages", "responses", "generate_content"}, Lifecycle: "active", ContextWindow: m.Input, MaxInputTokens: m.Input, ContextLimitKind: "input", MaxOutputTokens: m.Output, FieldSources: map[string]string{"identity": p.SourceRefs[0], "context_window": p.SourceRefs[0], "max_output_tokens": p.SourceRefs[0]}})
		}
		out = append(out, p)
	}
	return out
}
func (s *AccountTestService) buildWorkBuddyModelsRequest(ctx context.Context, a *Account) (*http.Request, error) {
	if workBuddyToken(a) == "" {
		return nil, newUpstreamModelSyncConfigError("WorkBuddy credential is missing", nil)
	}
	base, err := s.validateUpstreamBaseURL(workBuddyEndpoint(a))
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid WorkBuddy base URL", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/v3/config", nil)
	if err != nil {
		return nil, err
	}
	applyWorkBuddySign(req.Header, a)
	req.Header.Set("Accept", "application/json")
	return req, nil
}
func extractWorkBuddyModelCatalog(body []byte, site string) ([]string, map[string]UpstreamModelMetadata, error) {
	var env struct {
		Code *int `json:"code"`
		Data *struct {
			Models []json.RawMessage `json:"models"`
			Agents []struct {
				Name   string   `json:"name"`
				Models []string `json:"models"`
			} `json:"agents"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, nil, err
	}
	if env.Code == nil || env.Data == nil {
		return nil, nil, errors.New("WorkBuddy config has no valid data envelope")
	}
	if *env.Code != 0 {
		return nil, nil, &workBuddyAPIError{http.StatusOK, *env.Code}
	}
	models := map[string]json.RawMessage{}
	ordered := []string{}
	for _, raw := range env.Data.Models {
		var m struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &m) == nil && m.ID != "" {
			models[m.ID] = raw
			ordered = append(ordered, m.ID)
		}
	}
	ids := []string{}
	seen := map[string]bool{}
	add := func(id string) {
		if id != "" && !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	cli := false
	for _, agent := range env.Data.Agents {
		if agent.Name == "cli" {
			cli = true
			for _, id := range agent.Models {
				add(id)
			}
		}
	}
	for _, id := range ordered {
		if !cli || strings.HasPrefix(id, "custom:") {
			add(id)
		}
	}
	metadata := map[string]UpstreamModelMetadata{}
	for _, id := range ids {
		var d struct {
			Name              string `json:"name"`
			Input             int64  `json:"maxInputTokens"`
			Output            int64  `json:"maxOutputTokens"`
			Images            bool   `json:"supportsImages"`
			OnlyReasoning     bool   `json:"onlyReasoning"`
			SupportsReasoning bool   `json:"supportsReasoning"`
			Reasoning         struct {
				Default          string   `json:"defaultEffort"`
				Efforts          []string `json:"efforts"`
				SupportedEfforts []string `json:"supportedEfforts"`
				CanDisable       *bool    `json:"canDisableThinking"`
			} `json:"reasoning"`
		}
		if raw := models[id]; len(raw) > 0 {
			if err := json.Unmarshal(raw, &d); err != nil {
				metadata[id] = UpstreamModelMetadata{ID: id, Conflicts: []string{"invalid_optional_metadata"}}
				continue
			}
		}
		vision := d.Images && !(site == "global" && strings.HasPrefix(id, "glm-"))
		efforts := d.Reasoning.Efforts
		if len(efforts) == 0 {
			efforts = d.Reasoning.SupportedEfforts
		}
		if len(efforts) == 0 && d.SupportsReasoning {
			efforts = []string{"low", "high", "max"}
		}
		reasoning := d.SupportsReasoning || d.OnlyReasoning || len(efforts) > 0 || d.Reasoning.Default != ""
		if len(efforts) > 0 && ((!d.OnlyReasoning && (d.Reasoning.CanDisable == nil || *d.Reasoning.CanDisable)) || id == "deepseek-v4.1-flash" || id == "deepseek-v4.1-flash-sg") {
			if !slices.Contains(efforts, "none") {
				efforts = append([]string{"none"}, efforts...)
			}
		}
		input := []string{"text"}
		if vision {
			input = append(input, "image")
		}
		metadata[id] = UpstreamModelMetadata{ID: id, DisplayName: firstNonEmpty(d.Name, id), ContextWindow: d.Input, MaxOutputTokens: d.Output, ContextLimitKind: "input", SupportsVision: &vision, InputModalities: input, OutputModalities: []string{"text"}, Reasoning: &reasoning, DefaultReasoningLevel: d.Reasoning.Default, SupportedReasoningLevels: efforts, ProviderProfile: "workbuddy-" + site, SupportedMethods: []string{"chat_completions"}}
	}
	return ids, metadata, nil
}
func workBuddyFallbackKey(a *Account) bool {
	key := workBuddyToken(a)
	return a.Type == AccountTypeAPIKey && !strings.HasPrefix(key, "ck_") && !strings.HasPrefix(key, "pt_")
}
func workBuddyConfigFallback(body []byte, a *Account) []byte {
	if a.Platform != PlatformWorkBuddy {
		return body
	}
	ids, _, err := extractWorkBuddyModelCatalog(body, workBuddySiteFor(a).ID)
	if err != nil || len(ids) > 0 || !workBuddyFallbackKey(a) {
		return body
	}
	models := []map[string]any{}
	names := []string{}
	for _, m := range workBuddyModels(workBuddySiteFor(a).ID) {
		names = append(names, m.ID)
		models = append(models, map[string]any{"id": m.ID, "name": m.Name, "maxInputTokens": m.Input, "maxOutputTokens": m.Output, "supportsImages": true, "reasoning": map[string]any{"efforts": m.Efforts}})
	}
	raw, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{"models": models, "agents": []any{map[string]any{"name": "cli", "models": names}}}})
	return raw
}

func (s *AccountTestService) fetchWorkBuddyModels(req *http.Request, a *Account) ([]string, []byte, error) {
	proxy, err := a.EgressProxyURL(req.Context())
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.doUpstreamModelsRequest(req, proxy, a)
	if err != nil {
		return nil, nil, newUpstreamModelSyncNetworkError("WorkBuddy config request failed", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil || len(body) > 8<<20 {
		return nil, nil, newUpstreamModelSyncUpstreamError("WorkBuddy config exceeds size limit", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, newUpstreamModelDiscoveryHTTPError(resp.StatusCode, body)
	}
	body = workBuddyConfigFallback(body, a)
	ids, _, err := extractWorkBuddyModelCatalog(body, workBuddySiteFor(a).ID)
	return ids, body, err
}
