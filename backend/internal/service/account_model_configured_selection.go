package service

import "strings"

const (
	ModelEvidenceConfiguredSelection        = "configured_selection"
	ModelEvidenceConfiguredCompactSelection = "configured_compact_selection"
	ModelSelectionBaselineFrozenExtraKey    = "model_selection_baseline_frozen"
)

func exactConfiguredModel(model string) bool {
	return model != "" && model == strings.TrimSpace(model) && !strings.ContainsAny(model, "*?\r\n")
}

func configuredSelectionEvidence(account *Account, member AccountCatalogModel) bool {
	return member.EvidenceKind == ModelEvidenceConfiguredSelection && concreteMappedModel(account, member.ID)
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
	endpoints := accountCatalogModelEndpoints(account, modelID, UpstreamModelMetadata{})
	if account.IsGemini() {
		endpoints = append(endpoints, "generate_content")
	} else {
		endpoints = append(endpoints, accountCatalogNativeTextEndpoints(account, modelID)...)
	}
	metadata, _ := account.GetUpstreamModelMetadata(modelID)
	if metadata.TaskType == "" {
		endpoints = append(endpoints, adapterBoundCatalogFamilies(account)...)
	}
	return dedupeAndSortModelIDs(endpoints)
}

func adapterBoundCatalogFamilies(account *Account) []string {
	endpoints := []string{}
	for _, candidate := range []struct{ endpoint, action string }{
		{"images", "image.generate"}, {"images", "video.generate"}, {"embeddings", "embeddings"},
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
	return appendConfiguredModels(account, snapshot, configuredModelMapping(account), false)
}

func appendConfiguredCompactSelectionModels(account, credentialAccount *Account, snapshot *AccountModelCatalogSnapshot) *AccountModelCatalogSnapshot {
	return appendConfiguredModels(account, snapshot, account.GetCompactModelMapping(), true)
}

// Rebuild the member slice before widening endpoints: decoded snapshots share their backing arrays.
func appendConfiguredModels(account *Account, snapshot *AccountModelCatalogSnapshot, mapping map[string]string, compact bool) *AccountModelCatalogSnapshot {
	targets := []string{}
	for _, target := range mapping {
		if exactConfiguredModel(target) {
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

// NormalizeEmptyAccountModelSelection is used only for new rows or an explicit transition to empty.
// Ordinary updates with a nonempty mapping must preserve an existing deny-all scope.
func NormalizeEmptyAccountModelSelection(account *Account) {
	if len(configuredModelMapping(account)) != 0 {
		return
	}
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{}}
	account.Extra[ModelSelectionBaselineFrozenExtraKey] = true
}

func AccountModelSelectionNeedsFreeze(account *Account) bool {
	frozen, _ := account.Extra[ModelSelectionBaselineFrozenExtraKey].(bool)
	return len(configuredModelMapping(account)) == 0 && !frozen
}

func NormalizeAccountModelSelectionUpdate(current, incoming *Account) {
	if len(configuredModelMapping(current)) > 0 && len(configuredModelMapping(incoming)) == 0 {
		NormalizeEmptyAccountModelSelection(incoming)
	}
}

// FrozenAccountModelSelection reads only the pre-commit stored catalog, never the arriving discovery.
func FrozenAccountModelSelection(account *Account, baseline AccountModelCatalogBaseline) map[string]string {
	mapping := map[string]string{}
	snapshot := account.GetUpstreamModelCatalog()
	if snapshot == nil || snapshot.IdentityRevision != baseline.IdentityRevision || account.ModelCatalogState().Result == "credentials_invalid" {
		return mapping
	}
	if snapshot.CredentialAccountID != baseline.CredentialAccountID || snapshot.CredentialIdentityRevision != baseline.CredentialIdentityRevision {
		return mapping
	}
	if snapshot.SourceKind == modelEvidenceLegacyMappingUpgrade && !legacyModelCatalogValidFor(account, snapshot) {
		return mapping
	}
	var scope AccountModelAllowlist
	decodeCatalogExtra(account.Extra, UpstreamModelAllowlistExtraKey, &scope)
	for _, member := range snapshot.EligibleModels {
		if !exactConfiguredModel(member.ID) || len(member.Endpoints) == 0 {
			continue
		}
		allowed := !scope.Enabled
		for _, model := range scope.Models {
			if model == member.ID {
				allowed = true
			}
		}
		if allowed {
			mapping[member.ID] = member.ID
		}
	}
	return mapping
}
