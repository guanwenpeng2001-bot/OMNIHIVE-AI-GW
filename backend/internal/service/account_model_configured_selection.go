package service

import (
	"maps"
	"slices"
	"strings"
)

const (
	ModelEvidenceConfiguredSelection        = "configured_selection"
	ModelEvidenceConfiguredCompactSelection = "configured_compact_selection"
	ModelSelectionBaselineFrozenExtraKey    = "model_selection_baseline_frozen"
)

func exactConfiguredModel(model string) bool {
	return model != "" && model == strings.TrimSpace(model) && !strings.ContainsAny(model, "*?\r\n")
}

// configuredConcreteModelIDs returns explicitly selected upstream IDs, including
// private alias targets. Inherit mode derives selection evidence from saved mappings.
func configuredConcreteModelIDs(account *Account) []string {
	if account == nil {
		return nil
	}
	var ids []string
	if selection := account.restrictedPublicModels(); selection != nil {
		ids = slices.Collect(maps.Keys(selection.scope))
	} else {
		ids = slices.Collect(maps.Values(configuredModelMapping(account)))
	}
	return slices.DeleteFunc(ids, func(id string) bool { return !exactConfiguredModel(id) })
}

func configuredPassthroughModel(account *Account, wireModel string) bool {
	if account == nil || account.Platform != PlatformOpenAI || !exactConfiguredModel(wireModel) {
		return false
	}
	if selection := account.restrictedPublicModels(); selection != nil {
		return selection.allowsWireModel(account, wireModel)
	}
	for name, target := range configuredModelMapping(account) {
		if exactConfiguredModel(name) && exactConfiguredModel(target) && normalizeAccountBindingWireModel(account, name) == wireModel {
			return true
		}
	}
	return false
}

func configuredSelectionEvidence(account *Account, member AccountCatalogModel) bool {
	if member.EvidenceKind != ModelEvidenceConfiguredSelection {
		return false
	}
	if selection := account.restrictedPublicModels(); selection != nil {
		return selection.scope[member.ID]
	}
	return concreteMappedModel(account, member.ID)
}

func configuredCompactSelectionEvidence(account *Account, member AccountCatalogModel) bool {
	if member.EvidenceKind != ModelEvidenceConfiguredCompactSelection || !exactConfiguredModel(member.ID) {
		return false
	}
	for _, target := range account.GetCompactModelMapping() {
		if target == member.ID {
			return true
		}
	}
	return false
}

// Configured endpoints describe installed adapters, never an upstream capability guarantee.
func configuredSelectionEndpoints(account *Account, modelID string) []string {
	metadata, _ := account.GetUpstreamModelMetadata(modelID)
	endpoints := accountCatalogModelEndpoints(account, modelID, metadata)
	if account.IsGemini() {
		endpoints = append(endpoints, "generate_content")
	} else {
		endpoints = append(endpoints, accountCatalogNativeTextEndpoints(account, modelID)...)
	}
	if metadata.TaskType == "" {
		endpoints = append(endpoints, adapterBoundCatalogFamilies(account)...)
	}
	return dedupeAndSortModelIDs(endpoints)
}

func adapterBoundCatalogFamilies(account *Account) []string {
	endpoints := []string{}
	for _, candidate := range []struct{ endpoint, action string }{
		{"images", "image.generate"}, {"images", "video.generate"},
		{"rerank", "rerank"}, {"voice", "voice"}, {"speech", "audio.tts"},
		{"transcriptions", "audio.stt"}, {string(OpenAIEndpointCapabilityLive), "realtime"},
	} {
		if platformActionAdapterBound(account, candidate.action) {
			endpoints = append(endpoints, candidate.endpoint)
		}
	}
	return dedupeAndSortModelIDs(endpoints)
}

func appendConfiguredSelectionModels(account, credentialAccount *Account, snapshot *AccountModelCatalogSnapshot) *AccountModelCatalogSnapshot {
	return appendConfiguredModelTargets(account, snapshot, configuredConcreteModelIDs(account), false)
}

func appendConfiguredCompactSelectionModels(account, credentialAccount *Account, snapshot *AccountModelCatalogSnapshot) *AccountModelCatalogSnapshot {
	return appendConfiguredModels(account, snapshot, account.GetCompactModelMapping(), true)
}

func appendConfiguredModels(account *Account, snapshot *AccountModelCatalogSnapshot, mapping map[string]string, compact bool) *AccountModelCatalogSnapshot {
	return appendConfiguredModelTargets(account, snapshot, slices.Collect(maps.Values(mapping)), compact)
}

// Rebuild the member slice before widening endpoints: decoded snapshots share their backing arrays.
func appendConfiguredModelTargets(account *Account, snapshot *AccountModelCatalogSnapshot, models []string, compact bool) *AccountModelCatalogSnapshot {
	targets := []string{}
	for _, target := range models {
		if exactConfiguredModel(target) && zenFreeModelAllowed(account, target) {
			targets = append(targets, target)
		}
	}
	targets = dedupeAndSortModelIDs(targets)
	if len(targets) == 0 {
		return snapshot
	}
	if snapshot == nil {
		snapshot = &AccountModelCatalogSnapshot{IdentityRevision: account.ModelCatalogState().IdentityRevision}
	}
	snapshot.EligibleModels = append([]AccountCatalogModel{}, snapshot.EligibleModels...)
	for _, target := range targets {
		found := false
		for index, member := range snapshot.EligibleModels {
			if member.ID != target {
				continue
			}
			if compact {
				found = true
				break
			}
			if member.EvidenceKind == ModelEvidenceAdministrator || member.EvidenceKind == ModelEvidenceProbe {
				continue
			}
			endpoints := append([]string{}, member.Endpoints...)
			snapshot.EligibleModels[index].Endpoints = dedupeAndSortModelIDs(append(endpoints, configuredSelectionEndpoints(account, target)...))
			found = true
		}
		if found {
			continue
		}
		evidence := ModelEvidenceConfiguredSelection
		if compact {
			evidence = ModelEvidenceConfiguredCompactSelection
		}
		snapshot.EligibleModels = append(snapshot.EligibleModels, AccountCatalogModel{ID: target, Endpoints: configuredSelectionEndpoints(account, target), EvidenceKind: evidence})
	}
	return snapshot
}
