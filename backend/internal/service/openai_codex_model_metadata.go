package service

import (
	"bytes"
	"encoding/json"
	"strings"
)

var codexToolCapabilityFields = []string{
	"supports_search_tool", "apply_patch_tool_type", "comp_hash", "tool_mode", "use_responses_lite",
	"multi_agent_reasoning_effort", "multi_agent_version",
}

func applyCodexToolCapabilities(dst, src map[string]json.RawMessage, overwrite bool) bool {
	changed := false
	for _, field := range codexToolCapabilityFields {
		value := bytes.TrimSpace(src[field])
		if len(value) == 0 {
			continue
		}
		// These Codex fields are nullable booleans or strings, never arbitrary objects.
		if !bytes.Equal(value, []byte("null")) {
			if field == "supports_search_tool" || field == "use_responses_lite" {
				if !bytes.Equal(value, []byte("true")) && !bytes.Equal(value, []byte("false")) {
					continue
				}
			} else {
				var text string
				if json.Unmarshal(value, &text) != nil {
					continue
				}
			}
		}
		current, exists := dst[field]
		if (exists && !overwrite) || bytes.Equal(current, value) {
			continue
		}
		dst[field] = append(json.RawMessage(nil), value...)
		changed = true
	}
	return changed
}

func accountCodexToolCapabilities(account *Account, modelID string) map[string]json.RawMessage {
	capabilities := make(map[string]json.RawMessage)
	if account == nil {
		return capabilities
	}
	if metadata, ok := account.GetUpstreamModelMetadata(modelID); ok {
		applyCodexToolCapabilities(capabilities, metadata.CodexToolCapabilities, true)
	}
	if account.IsOpenAI() && shouldForwardOpenAIResponsesViaRawChatCompletions(account) {
		// This bridge implements client-side tool discovery, even without a native manifest.
		applyCodexToolCapabilities(capabilities, map[string]json.RawMessage{"supports_search_tool": json.RawMessage("true")}, false)
	}
	// Codex 0.153's bundled Astra catalog verifies these values. API-key routes
	// use standard Responses, not the ChatGPT-only Responses Lite wire.
	baseURL := strings.TrimSpace(account.GetCredential("base_url"))
	if baseURL == "" {
		baseURL = account.GetOpenAIBaseURL()
	}
	parsed, err := account.parsedBaseURL(baseURL)
	official := err == nil && (strings.EqualFold(parsed.Hostname(), "api.openai.com") ||
		(account.IsOpenAIOAuth() && strings.EqualFold(parsed.Hostname(), "chatgpt.com")))
	if account.IsOpenAI() && isOpenAIGPT6AstraModel(modelID) && official {
		defaults := map[string]json.RawMessage{
			"supports_search_tool":  json.RawMessage("true"),
			"apply_patch_tool_type": json.RawMessage(`"freeform"`),
			"comp_hash":             json.RawMessage(`"3000"`),
			"tool_mode":             json.RawMessage("null"),
			"use_responses_lite":    json.RawMessage("false"),
		}
		if account.IsOpenAIOAuth() {
			defaults["tool_mode"] = json.RawMessage(`"code_mode_only"`)
			defaults["use_responses_lite"] = json.RawMessage("true")
		}
		applyCodexToolCapabilities(capabilities, defaults, false)
	}
	if account.IsOpenAIApiKey() {
		target := modelID
		if isOpenAIGPT6AstraModel(target) {
			target = "gpt-6-astra"
		}
		_, disabled := apiKeyCodexModelsWithoutResponsesLite[target]
		if disabled && bytes.Equal(capabilities["use_responses_lite"], []byte("true")) {
			capabilities["use_responses_lite"] = json.RawMessage("false")
		}
	}
	return capabilities
}

// codexModelRoutingAccountIDs 返回分组显式为该公开别名声明的账号集合。
//
// 返回非空表示运营者已经用 model_routing 指明“这个别名由这些账号服务”，此时别名的
// 归属不再是需要推断的未知量：能力声明只看这些账号，且不再因为它们映射到不同上游而
// 判定为冲突。返回空表示没有相关规则，保持原有的全量推断与失败即关闭行为。
func codexModelRoutingAccountIDs(group *Group, modelID string) []int64 {
	if group == nil {
		return nil
	}
	return group.GetRoutingAccountIDs(strings.TrimSpace(modelID))
}

func groupCodexModelMetadata(
	platform string,
	modelID string,
	accounts []Account,
	group *Group,
	compositeRoutes []CompositeModelRoute,
	compositeRoutesAvailable bool,
	publicSnapshots ...map[int64]map[string]codexModelMetadataOverride,
) (codexModelMetadataOverride, bool) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return codexModelMetadataOverride{}, false
	}
	// 显式路由规则本身与平台无关，先取出来供后续两处判定共用。
	routedAccountIDs := codexModelRoutingAccountIDs(group, modelID)
	routed := len(routedAccountIDs) > 0
	upstreamModel := modelID
	if platform == PlatformComposite {
		var resolved bool
		platform, upstreamModel, resolved = resolveCodexCompositeModelTarget(
			modelID,
			accounts,
			compositeRoutes,
			compositeRoutesAvailable,
		)
		if !resolved {
			if !routed && codexExplicitModelTargetsConflict(accounts, modelID) {
				return codexModelMetadataOverride{
					reasoningConflict:       true,
					inputModalitiesConflict: true,
				}, true
			}
			return codexModelMetadataOverride{}, false
		}
	}
	if !isConcreteRequestPlatform(platform) {
		return codexModelMetadataOverride{}, false
	}

	explicitClaims := false
	if upstreamModel == modelID {
		for i := range accounts {
			account := &accounts[i]
			if routed && !containsInt64(routedAccountIDs, account.ID) {
				continue
			}
			if account.Platform == platform && codexExplicitModelMappingClaims(*account, modelID) {
				explicitClaims = true
				break
			}
		}
	}
	// 别名已被显式路由声明时，"多个账号映射到不同上游"是故障转移的正常写法，不是歧义，
	// 因此不再失败即关闭；能力回落到与单账号无快照时相同的按名推导。
	explicitTargetsConflict := explicitClaims && !routed &&
		codexExplicitModelTargetsConflictForPlatform(accounts, platform, modelID)
	publicAlias := upstreamModel != modelID
	candidates := make([]codexModelMetadataOverride, 0)
	for i := range accounts {
		account := &accounts[i]
		if account.Platform != platform {
			continue
		}
		if routed && !containsInt64(routedAccountIDs, account.ID) {
			continue
		}
		var lookupModel string
		if explicitClaims {
			if !codexExplicitModelMappingClaims(*account, modelID) {
				continue
			}
			lookupModel = account.GetMappedModel(modelID)
		} else {
			if !account.SupportsRoutedModelTarget(modelID, upstreamModel) {
				continue
			}
			lookupModel = account.GetMappedModel(upstreamModel)
		}
		if strings.TrimSpace(lookupModel) != modelID {
			publicAlias = true
		}
		var metadata codexModelMetadataOverride
		var ok bool
		if len(publicSnapshots) > 0 {
			metadata, ok = publicSnapshots[0][account.ID][lookupModel]
		} else {
			metadata, ok = accountCodexMetadataSnapshot(account)[lookupModel]
		}
		if !ok {
			if explicitTargetsConflict {
				return codexModelMetadataOverride{
					reasoningConflict:       true,
					inputModalitiesConflict: true,
				}, true
			}
		}
		// Public model lists need generic capability facts only. Their request
		// snapshot avoids decoding the entire JSONB catalog for every model.
		if len(publicSnapshots) == 0 {
			metadata.CodexToolCapabilities = accountCodexToolCapabilities(account, lookupModel)
		}
		candidates = append(candidates, metadata)
	}
	if len(candidates) == 0 {
		return codexModelMetadataOverride{}, false
	}
	metadata := intersectCodexModelMetadata(modelID, candidates)
	if publicAlias {
		metadata.DisplayName = modelID
		metadata.Description = configuredCodexCustomDescription
	}
	return metadata, true
}

func codexExplicitModelTargetsConflict(accounts []Account, modelID string) bool {
	targets := make(map[string]struct{})
	for i := range accounts {
		account := &accounts[i]
		mappedModel, matched := account.ResolveMappedModel(modelID)
		mappedModel = strings.TrimSpace(mappedModel)
		if !matched || mappedModel == "" {
			continue
		}
		targets[strings.TrimSpace(account.Platform)+"\x00"+mappedModel] = struct{}{}
	}
	return len(targets) > 1
}

func codexExplicitModelTargetsConflictForPlatform(accounts []Account, platform, modelID string) bool {
	targets := make(map[string]struct{})
	for i := range accounts {
		account := &accounts[i]
		if account.Platform != platform {
			continue
		}
		mappedModel, matched := account.ResolveMappedModel(modelID)
		mappedModel = strings.TrimSpace(mappedModel)
		if !matched || mappedModel == "" {
			continue
		}
		targets[mappedModel] = struct{}{}
	}
	return len(targets) > 1
}

func intersectUpstreamModelMetadata(modelID string, candidates []UpstreamModelMetadata) codexModelMetadataOverride {
	overrides := make([]codexModelMetadataOverride, len(candidates))
	for i, candidate := range candidates {
		overrides[i].UpstreamModelMetadata = candidate
		// Preserve V2 fields when the shared metadata type gains them.
		if raw, err := json.Marshal(candidate); err == nil {
			_ = json.Unmarshal(raw, &overrides[i])
		}
	}
	return intersectCodexModelMetadata(modelID, overrides)
}

func intersectCodexModelMetadata(modelID string, candidates []codexModelMetadataOverride) codexModelMetadataOverride {
	result := codexModelMetadataOverride{UpstreamModelMetadata: UpstreamModelMetadata{ID: strings.TrimSpace(modelID)}}
	if len(candidates) == 0 {
		return result
	}
	candidates = append([]codexModelMetadataOverride(nil), candidates...)
	for i := range candidates {
		candidates[i] = codexMetadataWithReasoningControl(candidates[i])
	}
	result.ReasoningControl = intersectCodexReasoningControl(candidates)
	allTools := true
	for _, candidate := range candidates {
		if candidate.SupportsFunctionCalling == nil {
			allTools = false
			continue
		}
		if !*candidate.SupportsFunctionCalling {
			value := false
			result.SupportsFunctionCalling = &value
			break
		}
	}
	if result.SupportsFunctionCalling == nil && allTools {
		value := true
		result.SupportsFunctionCalling = &value
	}
	result.CodexToolCapabilities = make(map[string]json.RawMessage)
	for _, field := range codexToolCapabilityFields {
		value := bytes.TrimSpace(candidates[0].CodexToolCapabilities[field])
		shared := len(value) > 0
		declared := shared
		for _, candidate := range candidates[1:] {
			declared = declared || len(candidate.CodexToolCapabilities[field]) > 0
			if !bytes.Equal(value, bytes.TrimSpace(candidate.CodexToolCapabilities[field])) {
				shared = false
			}
		}
		if shared {
			result.CodexToolCapabilities[field] = append(json.RawMessage(nil), value...)
		} else if declared {
			fallback := json.RawMessage("null")
			if field == "supports_search_tool" || field == "use_responses_lite" {
				fallback = json.RawMessage("false")
			}
			result.CodexToolCapabilities[field] = fallback
		}
	}
	for _, candidate := range candidates {
		if result.DisplayName == "" && strings.TrimSpace(candidate.DisplayName) != "" {
			result.DisplayName = strings.TrimSpace(candidate.DisplayName)
		}
		if result.Description == "" && strings.TrimSpace(candidate.Description) != "" {
			result.Description = strings.TrimSpace(candidate.Description)
		}
	}

	reasoningKnown := true
	reasoningValue := false
	for i, candidate := range candidates {
		if candidate.Reasoning == nil {
			reasoningKnown = false
			break
		}
		if i == 0 {
			reasoningValue = *candidate.Reasoning
			continue
		}
		if reasoningValue != *candidate.Reasoning {
			reasoningKnown = false
			result.reasoningConflict = true
			break
		}
	}
	if !reasoningKnown {
		for _, candidate := range candidates {
			if (candidate.Reasoning != nil && *candidate.Reasoning) || len(candidate.ReasoningControl) > 0 {
				result.reasoningConflict = true
				break
			}
		}
	}
	if reasoningKnown {
		result.Reasoning = &reasoningValue
		if reasoningValue {
			levels := normalizeReasoningLevels(candidates[0].SupportedReasoningLevels)
			for _, candidate := range candidates[1:] {
				levels = intersectOrderedStrings(levels, normalizeReasoningLevels(candidate.SupportedReasoningLevels))
			}
			result.SupportedReasoningLevels = levels
			if len(levels) > 0 {
				sharedDefault := normalizeReasoningLevel(candidates[0].DefaultReasoningLevel)
				for _, candidate := range candidates[1:] {
					if normalizeReasoningLevel(candidate.DefaultReasoningLevel) != sharedDefault {
						sharedDefault = ""
						break
					}
				}
				if !stringSliceContains(levels, sharedDefault) {
					sharedDefault = ""
				}
				result.DefaultReasoningLevel = sharedDefault
			}
		}
	}

	modalitiesKnown := true
	modalities := normalizeCodexInputModalities(candidates[0].InputModalities)
	if len(modalities) == 0 {
		modalitiesKnown = false
	}
	for _, candidate := range candidates[1:] {
		candidateModalities := normalizeCodexInputModalities(candidate.InputModalities)
		if len(candidateModalities) == 0 {
			modalitiesKnown = false
			break
		}
		modalities = intersectOrderedStrings(modalities, candidateModalities)
	}
	if modalitiesKnown && len(modalities) > 0 {
		result.InputModalities = modalities
	} else if modalitiesKnown {
		result.inputModalitiesConflict = true
	}

	// Missing peer metadata must not erase a known upper bound or denial.
	for _, candidate := range candidates {
		if candidate.Reasoning != nil && !*candidate.Reasoning && !result.reasoningConflict {
			disabled := false
			result.Reasoning = &disabled
			result.reasoningConflict = false
			result.SupportedReasoningLevels = nil
			result.DefaultReasoningLevel = ""
		}
		if candidate.MaxOutputTokens > 0 && (result.MaxOutputTokens == 0 || candidate.MaxOutputTokens < result.MaxOutputTokens) {
			result.MaxOutputTokens = candidate.MaxOutputTokens
		}
		if candidate.ContextWindow > 0 && (result.ContextWindow == 0 || candidate.ContextWindow < result.ContextWindow) {
			result.ContextWindow = candidate.ContextWindow
		}
	}
	for _, candidate := range candidates {
		maxContextWindow := candidate.MaxContextWindow
		if maxContextWindow <= 0 {
			// Older snapshots only stored the default window. Keep that bound
			// until a sync supplies the upstream's explicit maximum.
			maxContextWindow = candidate.ContextWindow
		}
		if maxContextWindow <= 0 {
			continue
		}
		if result.MaxContextWindow == 0 || maxContextWindow < result.MaxContextWindow {
			result.MaxContextWindow = maxContextWindow
		}
	}
	// Explicit empty sets are restrictions, even when a peer has no snapshot.
	for _, candidate := range candidates {
		if candidate.InputModalities != nil && len(candidate.InputModalities) == 0 {
			result.InputModalities = []string{}
			result.inputModalitiesConflict = false
		}
		if candidate.SupportedReasoningLevels != nil && len(candidate.SupportedReasoningLevels) == 0 {
			result.SupportedReasoningLevels = []string{}
			result.DefaultReasoningLevel = ""
		}
	}
	return result
}

func applyUpstreamModelMetadataToCodexDescriptor(
	descriptor *configuredCodexModelDescriptor,
	metadata codexModelMetadataOverride,
) {
	if descriptor == nil {
		return
	}
	if strings.TrimSpace(metadata.DisplayName) != "" {
		descriptor.DisplayName = strings.TrimSpace(metadata.DisplayName)
	}
	if strings.TrimSpace(metadata.Description) != "" {
		descriptor.Description = strings.TrimSpace(metadata.Description)
	}
	metadata = codexMetadataWithReasoningControl(metadata)
	descriptor.ReasoningControl = append(json.RawMessage(nil), metadata.ReasoningControl...)
	descriptor.SupportsFunctionCalling = metadata.SupportsFunctionCalling
	if metadata.SupportsFunctionCalling != nil && !*metadata.SupportsFunctionCalling {
		descriptor.SupportsParallelToolCalls = false
	}
	if metadata.MaxOutputTokens > 0 {
		descriptor.MaxOutputTokens = metadata.MaxOutputTokens
	}
	if metadata.reasoningConflict || metadata.Reasoning != nil || len(metadata.ReasoningControl) > 0 || metadata.SupportedReasoningLevels != nil {
		descriptor.DefaultReasoningLevel = nil
		descriptor.SupportedReasoningLevels = []configuredCodexReasoningLevel{}
		if !metadata.reasoningConflict && (metadata.Reasoning == nil || *metadata.Reasoning) {
			levels := normalizeReasoningLevels(metadata.SupportedReasoningLevels)
			defaultLevel := normalizeReasoningLevel(metadata.DefaultReasoningLevel)
			// Keep the established fallback for legacy snapshots only. An explicit
			// capability control must never acquire an invented default.
			if len(metadata.ReasoningControl) == 0 && len(levels) > 0 && !stringSliceContains(levels, defaultLevel) {
				defaultLevel = levels[0]
			}
			if stringSliceContains(levels, defaultLevel) {
				descriptor.DefaultReasoningLevel = &defaultLevel
			}
			for _, level := range levels {
				descriptor.SupportedReasoningLevels = append(descriptor.SupportedReasoningLevels, configuredCodexReasoningLevel{
					Effort: level, Description: configuredCodexReasoningLevelDescription(level),
				})
			}
		}
	}
	// Legacy snapshots explicitly declaring false retain their established
	// non-reasoning sentinel. V2 controls never invent an effort value.
	if len(metadata.ReasoningControl) == 0 && !metadata.reasoningConflict && metadata.Reasoning != nil && !*metadata.Reasoning && metadata.SupportedReasoningLevels == nil {
		none := "none"
		descriptor.DefaultReasoningLevel = &none
		descriptor.SupportedReasoningLevels = []configuredCodexReasoningLevel{{Effort: none, Description: configuredCodexReasoningLevelDescription(none)}}
	}
	if metadata.InputModalities != nil && len(metadata.InputModalities) == 0 {
		descriptor.InputModalities = []string{}
	} else if metadata.inputModalitiesConflict {
		descriptor.InputModalities = []string{"text"}
	} else if modalities := normalizeCodexInputModalities(metadata.InputModalities); len(modalities) > 0 {
		descriptor.InputModalities = modalities
	}
	if metadata.ContextWindow > 0 {
		descriptor.ContextWindow = metadata.ContextWindow
		descriptor.MaxContextWindow = metadata.ContextWindow
	}
	if metadata.MaxContextWindow > 0 {
		descriptor.MaxContextWindow = metadata.MaxContextWindow
		descriptor.ContextWindow = min(descriptor.ContextWindow, metadata.MaxContextWindow)
	}
}

func configuredCodexReasoningLevelDescription(level string) string {
	switch level {
	case "none":
		return "Use the model's default behavior without configurable reasoning"
	case "minimal":
		return "Minimal reasoning for the fastest responses"
	case "low":
		return "Fast responses with lighter reasoning"
	case "medium":
		return "Balanced reasoning for most coding tasks"
	case "high":
		return "Greater reasoning depth for coding and agent tasks"
	case "xhigh":
		return "Extra-high reasoning depth for difficult tasks"
	case "max":
		return "Maximum reasoning depth for complex tasks"
	case "ultra":
		return "Maximum reasoning with automatic task delegation"
	default:
		return "Reasoning effort supported by the upstream model"
	}
}

func intersectOrderedStrings(left, right []string) []string {
	rightSet := make(map[string]struct{}, len(right))
	for _, value := range right {
		rightSet[value] = struct{}{}
	}
	intersection := make([]string, 0, len(left))
	for _, value := range left {
		if _, ok := rightSet[value]; ok {
			intersection = append(intersection, value)
		}
	}
	return intersection
}

func stringSliceContains(values []string, target string) bool {
	if target == "" {
		return false
	}
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// Read the wire contract directly: legacy snapshot types discard V2 fields.
func accountCodexMetadataSnapshot(account *Account) map[string]codexModelMetadataOverride {
	if account == nil {
		return nil
	}
	body, err := json.Marshal(account.Extra[UpstreamModelMetadataExtraKey])
	if err != nil {
		return nil
	}
	var snapshot struct {
		Models map[string]json.RawMessage `json:"models"`
	}
	if json.Unmarshal(body, &snapshot) != nil {
		return nil
	}
	result := make(map[string]codexModelMetadataOverride, len(snapshot.Models))
	for id, raw := range snapshot.Models {
		var metadata codexModelMetadataOverride
		if json.Unmarshal(raw, &metadata) != nil {
			continue
		}
		// Generic tools never imply search, execution, parallel calls or apply_patch.
		if metadata.SupportsFunctionCalling == nil {
			var fields struct {
				Tools json.RawMessage `json:"tools"`
			}
			if json.Unmarshal(raw, &fields) == nil && len(fields.Tools) > 0 && !bytes.Equal(fields.Tools, []byte("null")) {
				var supported bool
				if json.Unmarshal(fields.Tools, &supported) == nil {
					metadata.SupportsFunctionCalling = &supported
				} else {
					var tools struct {
						FunctionCalling *bool `json:"function_calling"`
					}
					if json.Unmarshal(fields.Tools, &tools) == nil {
						metadata.SupportsFunctionCalling = tools.FunctionCalling
					}
				}
			}
		}
		result[id] = codexMetadataWithReasoningControl(metadata)
	}
	return result
}

type codexReasoningControl struct {
	Kind           string          `json:"kind"`
	EffortValues   []string        `json:"effort_values,omitempty"`
	DefaultEffort  string          `json:"default_effort,omitempty"`
	Default        json.RawMessage `json:"default,omitempty"`
	SupportedKinds []string        `json:"supported_kinds,omitempty"`
	BudgetMin      *int64          `json:"budget_min,omitempty"`
	BudgetMax      *int64          `json:"budget_max,omitempty"`
}

func decodeCodexReasoningControl(raw json.RawMessage) codexReasoningControl {
	var control codexReasoningControl
	if json.Unmarshal(raw, &control) != nil {
		_ = json.Unmarshal(raw, &control.Kind)
	}
	return control
}

func codexMetadataWithReasoningControl(metadata codexModelMetadataOverride) codexModelMetadataOverride {
	if len(metadata.ReasoningControl) == 0 || bytes.Equal(bytes.TrimSpace(metadata.ReasoningControl), []byte("null")) {
		metadata.ReasoningControl = nil
		return metadata
	}
	control := decodeCodexReasoningControl(metadata.ReasoningControl)
	if control.DefaultEffort == "" && len(control.Default) > 0 {
		_ = json.Unmarshal(control.Default, &control.DefaultEffort)
	}
	if stringSliceContains(control.SupportedKinds, "effort") && control.Kind != "unknown" && control.Kind != "none" {
		control.Kind = "effort"
	}
	metadata.SupportedReasoningLevels = nil
	if metadata.Reasoning != nil && !*metadata.Reasoning {
		metadata.DefaultReasoningLevel = ""
		metadata.ReasoningControl = json.RawMessage(`{"kind":"none"}`)
		return metadata
	}
	switch control.Kind {
	case "effort":
		metadata.SupportedReasoningLevels = normalizeReasoningLevels(control.EffortValues)
		if control.DefaultEffort != "" {
			metadata.DefaultReasoningLevel = control.DefaultEffort
		}
		value := true
		metadata.Reasoning = &value
	case "toggle", "budget", "adaptive", "fixed":
		value := true
		metadata.Reasoning = &value
	case "none":
		value := false
		metadata.Reasoning = &value
	default:
		metadata.ReasoningControl = json.RawMessage(`{"kind":"unknown"}`)
	}
	if !stringSliceContains(metadata.SupportedReasoningLevels, metadata.DefaultReasoningLevel) {
		metadata.DefaultReasoningLevel = ""
	}
	return metadata
}

// Shared non-effort controls retain their complete semantics. Different shapes
// remain unknown rather than guessing a request protocol translation.
func intersectCodexReasoningControl(candidates []codexModelMetadataOverride) json.RawMessage {
	anyControl, allEffort, identical := false, true, true
	var shared json.RawMessage
	levels := normalizeReasoningLevels(candidates[0].SupportedReasoningLevels)
	defaultLevel := candidates[0].DefaultReasoningLevel
	for i, candidate := range candidates {
		anyControl = anyControl || len(candidate.ReasoningControl) > 0
		control := decodeCodexReasoningControl(candidate.ReasoningControl)
		allEffort = allEffort && (control.Kind == "effort" || (len(candidate.ReasoningControl) == 0 && len(candidate.SupportedReasoningLevels) > 0))
		var object any
		_ = json.Unmarshal(candidate.ReasoningControl, &object)
		canonical, _ := json.Marshal(object)
		if i == 0 {
			shared = canonical
		} else if !bytes.Equal(shared, canonical) {
			identical = false
		}
		levels = intersectOrderedStrings(levels, normalizeReasoningLevels(candidate.SupportedReasoningLevels))
		if defaultLevel != candidate.DefaultReasoningLevel {
			defaultLevel = ""
		}
	}
	if !anyControl {
		return nil
	}
	if identical {
		return append(json.RawMessage(nil), shared...)
	}
	if allEffort {
		if !stringSliceContains(levels, defaultLevel) {
			defaultLevel = ""
		}
		control := codexReasoningControl{Kind: "effort", EffortValues: levels}
		if defaultLevel != "" {
			control.Default, _ = json.Marshal(defaultLevel)
		}
		raw, _ := json.Marshal(control)
		return raw
	}
	// Budget ranges are safe only within every declared route's bounds.
	budget := codexReasoningControl{Kind: "budget"}
	allBudget := true
	for _, candidate := range candidates {
		control := decodeCodexReasoningControl(candidate.ReasoningControl)
		if control.Kind != "budget" || control.BudgetMin == nil || control.BudgetMax == nil {
			allBudget = false
			break
		}
		if budget.BudgetMin == nil || *control.BudgetMin > *budget.BudgetMin {
			value := *control.BudgetMin
			budget.BudgetMin = &value
		}
		if budget.BudgetMax == nil || *control.BudgetMax < *budget.BudgetMax {
			value := *control.BudgetMax
			budget.BudgetMax = &value
		}
	}
	if allBudget && *budget.BudgetMin <= *budget.BudgetMax {
		raw, _ := json.Marshal(budget)
		return raw
	}
	return json.RawMessage(`{"kind":"unknown"}`)
}

type codexModelMetadataOverride struct {
	UpstreamModelMetadata
	ReasoningControl        json.RawMessage `json:"reasoning_control,omitempty"`
	SupportsFunctionCalling *bool           `json:"supports_function_calling,omitempty"`
	reasoningConflict       bool
	inputModalitiesConflict bool
}

type configuredCodexModelDescriptor struct {
	Slug                              string                          `json:"slug"`
	DisplayName                       string                          `json:"display_name"`
	Description                       string                          `json:"description"`
	DefaultReasoningLevel             *string                         `json:"default_reasoning_level,omitempty"`
	SupportedReasoningLevels          []configuredCodexReasoningLevel `json:"supported_reasoning_levels"`
	MultiAgentReasoningEffort         *string                         `json:"multi_agent_reasoning_effort,omitempty"`
	ShellType                         string                          `json:"shell_type"`
	Visibility                        string                          `json:"visibility"`
	SupportedInAPI                    bool                            `json:"supported_in_api"`
	Priority                          int                             `json:"priority"`
	AdditionalSpeedTiers              []string                        `json:"additional_speed_tiers"`
	ServiceTiers                      []configuredCodexServiceTier    `json:"service_tiers"`
	DefaultServiceTier                any                             `json:"default_service_tier"`
	AvailabilityNUX                   any                             `json:"availability_nux"`
	Upgrade                           any                             `json:"upgrade"`
	ModelMessages                     configuredCodexModelMessages    `json:"model_messages"`
	IncludeSkillsUsageInstructions    bool                            `json:"include_skills_usage_instructions"`
	IncludePluginUsageInstructions    bool                            `json:"include_plugin_usage_instructions"`
	IncludeAppsUsageInstructions      bool                            `json:"include_apps_usage_instructions"`
	SupportsReasoningSummaryParameter bool                            `json:"supports_reasoning_summary_parameter"`
	DefaultReasoningSummary           string                          `json:"default_reasoning_summary"`
	SupportVerbosity                  bool                            `json:"support_verbosity"`
	DefaultVerbosity                  *string                         `json:"default_verbosity"`
	ApplyPatchToolType                *string                         `json:"apply_patch_tool_type"`
	WebSearchToolType                 string                          `json:"web_search_tool_type"`
	TruncationPolicy                  configuredCodexTruncationPolicy `json:"truncation_policy"`
	SupportsImageDetailOriginal       bool                            `json:"supports_image_detail_original"`
	SupportsParallelToolCalls         bool                            `json:"supports_parallel_tool_calls"`
	ContextWindow                     int64                           `json:"context_window"`
	MaxOutputTokens                   int64                           `json:"max_output_tokens,omitempty"`
	ReasoningControl                  json.RawMessage                 `json:"reasoning_control,omitempty"`
	SupportsFunctionCalling           *bool                           `json:"supports_function_calling,omitempty"`
	MaxContextWindow                  int64                           `json:"max_context_window"`
	AutoCompactTokenLimit             any                             `json:"auto_compact_token_limit"`
	CompHash                          any                             `json:"comp_hash"`
	EffectiveContextWindowPercent     int64                           `json:"effective_context_window_percent"`
	ExperimentalSupportedTools        []string                        `json:"experimental_supported_tools"`
	InputModalities                   []string                        `json:"input_modalities"`
	SupportsSearchTool                bool                            `json:"supports_search_tool"`
	UseResponsesLite                  bool                            `json:"use_responses_lite"`
	NodeREPLAutoReviewRequired        bool                            `json:"node_repl_auto_review_required"`
	NodeREPLDisabled                  bool                            `json:"node_repl_disabled"`
	AutoReviewModelOverride           any                             `json:"auto_review_model_override"`
	ModelSpecialty                    any                             `json:"model_specialty"`
	ToolMode                          any                             `json:"tool_mode"`
	MultiAgentVersion                 any                             `json:"multi_agent_version"`
}
