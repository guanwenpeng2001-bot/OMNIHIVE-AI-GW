package service

import "strings"

// CapabilityMatch retains the caller ID and makes ambiguity an explicit miss.
type CapabilityMatch struct {
	Metadata    UpstreamModelMetadata
	CanonicalID string
	Rule        string
	Status      string
}

// Resolve only established first-party/hosting scopes, never the account's
// generic protocol label. OAuth hosted products have separate deployments.
func resolveCapabilityProfile(account *Account) string {
	if account == nil {
		return ""
	}
	if account.IsCursor() {
		return "cursor"
	}
	if account.IsCommandCode() {
		return "commandcode"
	}
	if account.Platform == PlatformAntigravity {
		return "antigravity"
	}
	if account.IsGrokOAuth() {
		return "grok-oauth"
	}
	if account.IsOpenAI() && account.Type == AccountTypeOAuth {
		return "openai-codex"
	}
	switch modelRegistryHostname(upstreamModelRegistryBaseURL(account)) {
	case "api.openai.com":
		return "openai"
	case "api.anthropic.com":
		return "anthropic"
	case "generativelanguage.googleapis.com":
		return "gemini"
	case "api.deepseek.com":
		return "deepseek"
	case "api.moonshot.cn", "api.moonshot.ai":
		return "moonshot"
	case "api.x.ai":
		return "xai"
	case "dashscope.aliyuncs.com":
		return "alibaba-cn"
	case "dashscope-intl.aliyuncs.com":
		return "alibaba"
	case "opencode.ai":
		return "opencode-go"
	}
	return "custom:" + normalizeModelRegistryBaseURL(upstreamModelRegistryBaseURL(account))
}

func lookupCapabilityMetadata(snapshot *ModelCapabilitySnapshot, profile, modelID string) CapabilityMatch {
	result := CapabilityMatch{Status: "not_found"}
	r, ok := snapshot.LookupExact(profile, modelID)
	rule := "exact"
	if !ok && snapshot != nil {
		targets := snapshot.aliases[capabilityKey(profile, modelID)]
		if len(targets) > 1 {
			return CapabilityMatch{Status: "ambiguous"}
		}
		if len(targets) == 1 {
			r, ok = snapshot.LookupExact(profile, targets[0])
			rule = "reviewed_alias"
		}
	}
	if !ok {
		return result
	}
	m := UpstreamModelMetadata{ID: modelID, ProviderProfile: profile, SupportsVision: r.SupportsVision, SupportsFunctionCalling: r.SupportsFunctionCalling, SupportsPromptCaching: r.SupportsPromptCaching, Reasoning: r.SupportsReasoning, InputModalities: r.InputModalities, OutputModalities: r.OutputModalities, ContextLimitKind: "input"}
	m.SupportedMethods = r.SupportedMethods
	if r.EmbeddingDimensions != nil {
		m.EmbeddingDimensions = *r.EmbeddingDimensions
	}
	if r.MaxInputTokens != nil {
		m.ContextWindow = *r.MaxInputTokens
	}
	if r.MaxOutputTokens != nil {
		m.MaxOutputTokens = *r.MaxOutputTokens
	}
	switch r.Mode {
	case "chat", "completion":
		m.TaskType = "text_generation"
	case "embedding", "rerank", "image_generation", "audio_generation", "video_generation", "transcription":
		m.TaskType = r.Mode
	}
	if m.InputModalities != nil {
		m.InputModalitiesStatus = "complete"
	} else if r.SupportsVision != nil {
		m.InputModalitiesStatus = "partial"
		if *r.SupportsVision {
			m.InputModalities = []string{"image"}
		}
	}
	if r.Control != nil {
		m.ReasoningControl = r.Control
	} else if r.ReasoningControl != "" {
		m.ReasoningControl = &ReasoningControl{Kind: r.ReasoningControl}
	}
	m.ReasoningControl = normalizeReasoningControl(m)
	projectReasoningEffort(&m)
	stampCapabilityEvidence(&m, r.Source, snapshot.Version(), profile, r.ModelID, rule, "verified")
	for target, field := range map[string]string{"context_window": "max_input_tokens", "max_output_tokens": "max_output_tokens", "reasoning": "supports_reasoning", "supports_function_calling": "supports_function_calling", "supports_prompt_caching": "supports_prompt_caching", "input_modalities": "input_modalities", "output_modalities": "output_modalities"} {
		if source, ok := r.FieldSources[field]; ok {
			e := m.FieldEvidence[target]
			e.Source = source.Source
			e.ContentHash = source.ContentHash
			m.FieldEvidence[target] = e
		}
	}
	return CapabilityMatch{Metadata: cloneUpstreamModelMetadata(m), CanonicalID: r.ModelID, Rule: rule, Status: "matched"}
}

// models.dev IDs are exact. No case folding is enabled without a provider
// contract; aliases expressed as record IDs must still be unique.
func lookupModelsDevModel(provider modelsDevProvider, modelID string) (modelsDevModel, string) {
	if m, ok := provider.Models[modelID]; ok {
		return m, "exact"
	}
	var found modelsDevModel
	count := 0
	for _, m := range provider.Models {
		if strings.TrimSpace(m.ID) == modelID {
			found = m
			count++
		}
	}
	if count == 1 {
		return found, "record_id"
	}
	if count > 1 {
		return modelsDevModel{}, "ambiguous"
	}
	return modelsDevModel{}, "not_found"
}

func buildCapabilityAliasIndex(records map[string]CapabilityRecord) map[string][]string {
	index := make(map[string][]string)
	for _, r := range records {
		for _, alias := range r.Aliases {
			key := capabilityKey(r.Profile, alias)
			exists := false
			for _, id := range index[key] {
				if id == r.ModelID {
					exists = true
				}
			}
			if !exists {
				index[key] = append(index[key], r.ModelID)
			}
		}
	}
	return index
}
func buildModelsDevIDIndex(provider modelsDevProvider) (map[string]modelsDevModel, map[string]bool) {
	result := make(map[string]modelsDevModel)
	ambiguous := make(map[string]bool)
	for _, m := range provider.Models {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			continue
		}
		if _, ok := result[id]; ok {
			ambiguous[id] = true
		}
		result[id] = m
	}
	return result, ambiguous
}
