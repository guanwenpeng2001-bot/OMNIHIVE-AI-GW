package service

import (
	"net/url"
	"regexp"
	"strings"
)

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
	if account.Type == AccountTypeBedrock || account.Platform == PlatformBedrock {
		return "bedrock"
	}
	baseURL := upstreamModelRegistryBaseURL(account)
	host := modelRegistryHostname(baseURL)
	// Regional hosting domains must be matched on DNS-label boundaries.
	if strings.HasSuffix(host, ".aiplatform.googleapis.com") || strings.HasSuffix(host, "-aiplatform.googleapis.com") || host == "aiplatform.googleapis.com" {
		if account.Type == AccountTypeServiceAccount {
			return "vertex_ai"
		}
	}
	if bedrockAPIHostname.MatchString(host) {
		return "bedrock"
	}
	if strings.HasPrefix(host, "ark.") && strings.HasSuffix(host, ".volces.com") {
		return "volcengine"
	}
	switch host {
	case "open.bigmodel.cn":
		return "zhipu"
	case "api.minimax.io", "api.minimaxi.com", "api.minimax.chat":
		return "minimax"
	case "qianfan.baidubce.com", "aip.baidubce.com":
		return "qianfan"
	case "api.hunyuan.cloud.tencent.com", "hunyuan.tencentcloudapi.com", "hunyuan.ai.tencentcloudapi.com":
		return "hunyuan"
	case "tokenhub.tencentmaas.com", "tokenhub.tencentmaas.cn", "tokenhub-intl.tencentmaas.com", "tokenhub-intl.tencentmaas.cn":
		return "hunyuan"
	case "api.mistral.ai":
		return "mistral"
	case "api.openai.com":
		return "openai"
	case "api.anthropic.com":
		return "anthropic"
	case "generativelanguage.googleapis.com":
		return "gemini"
	case "api.deepseek.com":
		return "deepseek"
	case "api.moonshot.cn", "api.moonshot.ai":
		parsed, err := url.Parse(baseURL)
		if err == nil && account.GetAccountMode() != AccountModeCoding {
			switch strings.TrimRight(parsed.Path, "/") {
			case "", "/v1", "/anthropic":
				return "moonshot"
			}
		}
	case "api.kimi.com":
		parsed, err := url.Parse(baseURL)
		if err == nil && account.GetAccountMode() != AccountModePayG {
			switch strings.TrimRight(parsed.Path, "/") {
			case "/coding", "/coding/v1":
				return "kimi-for-coding"
			}
		}
	case "api.x.ai":
		return "xai"
	case "dashscope.aliyuncs.com":
		return "alibaba-cn"
	case "dashscope-intl.aliyuncs.com":
		return "alibaba"
	case "opencode.ai":
		return "opencode-go"
	}
	return "custom:" + normalizeModelRegistryBaseURL(baseURL)
}

// Match service.region.partition exactly: AWS tenant resources (S3, EC2,
// API Gateway) share the partition suffix but are not Bedrock endpoints.
var bedrockAPIHostname = regexp.MustCompile(`^bedrock(?:-runtime|-mantle)?(?:-fips)?\.(?:(?:us|eu|ap|sa|ca|me|af|il|mx)(?:-gov)?-[a-z]+-[0-9]+\.(?:amazonaws\.com|api\.aws)|cn-[a-z]+-[0-9]+\.amazonaws\.com\.cn)$`)

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
	m.TaskLimits = r.TaskLimits
	if r.EmbeddingDimensions != nil {
		m.EmbeddingDimensions = *r.EmbeddingDimensions
	}
	if r.MaxInputTokens != nil {
		m.ContextWindow = *r.MaxInputTokens
	} else {
		m.ContextWindow, m.ContextLimitKind = r.ContextWindow, r.ContextLimitKind
	}
	if r.MaxOutputTokens != nil {
		m.MaxOutputTokens = *r.MaxOutputTokens
	}
	switch r.Mode {
	case "chat", "completion", "text_generation":
		m.TaskType = "text_generation"
	case "embedding", "rerank", "image_generation", "audio_generation", "video_generation", "transcription":
		m.TaskType = r.Mode
	}
	if m.InputModalities != nil {
		m.InputModalitiesStatus = "complete"
		if r.InputModalitiesStatus != "" {
			m.InputModalitiesStatus = r.InputModalitiesStatus
		}
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
	for target, field := range map[string]string{"task_type": "mode", "supported_methods": "supported_methods", "embedding_dimensions": "embedding_dimensions", "reasoning_control": "control", "context_limit_kind": "context_limit_kind"} {
		if source, ok := r.FieldSources[field]; ok && source.Source != "" {
			evidence := m.FieldEvidence[target]
			evidence.Source, evidence.ContentHash = source.Source, source.ContentHash
			m.FieldEvidence[target] = evidence
		}
	}
	for name := range r.TaskLimits {
		if source, ok := r.FieldSources["task_limits."+name]; ok {
			evidence := m.FieldEvidence["task_limits."+name]
			evidence.Source, evidence.ContentHash = source.Source, source.ContentHash
			m.FieldEvidence["task_limits."+name] = evidence
		}
	}
	if r.MaxInputTokens == nil && r.ContextWindow > 0 {
		evidence := m.FieldEvidence["context_window"]
		source := r.FieldSources["context_window"]
		if source.Source != "" {
			evidence.Source, evidence.ContentHash = source.Source, source.ContentHash
			m.FieldEvidence["context_window"] = evidence
		}
	}
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
