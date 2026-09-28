package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
)

const modelEvidenceLegacyMappingUpgrade = "legacy-mapping-upgrade"

// A seed is written once by the upgrade migration, inside the protected catalog
// field. It carries only finite historical account configuration, not a claim
// that discovery or a probe succeeded. New accounts never enter this path.
func legacyModelCatalogValidFor(account *Account, snapshot *AccountModelCatalogSnapshot) bool {
	state := account.ModelCatalogState()
	return snapshot.LegacyAccountID == account.ID && account.ID > 0 &&
		snapshot.DiscoveryScope == modelEvidenceLegacyMappingUpgrade &&
		snapshot.IdentityRevision == state.IdentityRevision && snapshot.RulesRevision == state.RulesRevision
}

func populateLegacyModelCatalogEndpoints(account *Account, snapshot *AccountModelCatalogSnapshot) {
	capabilities := NewModelRegistry().CapabilitySnapshot()
	profile := resolveCapabilityProfile(account)
	eligible := make([]AccountCatalogModel, 0, len(snapshot.EligibleModels))
	for _, member := range snapshot.EligibleModels {
		if member.EvidenceKind != modelEvidenceLegacyMappingUpgrade || member.EvidenceObservedAt != snapshot.ObservedAt ||
			member.ID == "" || member.ID != strings.TrimSpace(member.ID) || strings.ContainsAny(member.ID, "*?\r\n") {
			continue
		}
		metadata, _ := account.GetUpstreamModelMetadata(member.ID)
		// Facts may narrow the endpoint family of an already seeded ID. They
		// never add members, and custom identities are not matched by a prefix.
		if match := lookupCapabilityMetadata(capabilities, profile, member.ID); match.Status == "matched" {
			metadata, _ = mergeUpstreamModelMetadata(metadata, match.Metadata)
		}
		endpoints := accountCatalogModelEndpoints(account, member.ID, metadata)
		task := accountCatalogEndpointTask(account, member.ID, metadata)
		if account.Platform == PlatformOpenAI && account.Type == AccountTypeAPIKey && metadata.SupportedMethods == nil && (task == "" || task == "text_generation") {
			// The old OpenAI path uses Responses until a saved probe/mode says
			// otherwise; passthrough may also send native Chat Completions.
			endpoints = []string{CompositeRouteEndpointChatCompletions}
			if !shouldForwardOpenAIResponsesViaRawChatCompletions(account) {
				endpoints = append(endpoints, CompositeRouteEndpointResponses)
			}
		}
		if len(endpoints) == 0 && metadata.SupportedMethods == nil && task == "" {
			// Legacy explicit mappings also named private text deployments with
			// no model facts. Preserve their saved protocol, without adding media
			// endpoints or filling any unknown capability/price fields.
			switch {
			case account.IsGemini() || account.Platform == PlatformAntigravity:
				endpoints = []string{"generate_content"}
			case account.IsOpenAIOAuth() || account.IsCursor():
				endpoints = []string{CompositeRouteEndpointResponses}
			default:
				endpoints = accountCatalogNativeTextEndpoints(account, member.ID)
			}
		}
		member.Endpoints = endpoints
		eligible = append(eligible, member)
	}
	// Keep DiscoveredModels empty: imported configuration is its own evidence.
	snapshot.EligibleModels = eligible
}

func hasLegacyModelCatalogEvidence(snapshot *AccountModelCatalogSnapshot, member AccountCatalogModel) bool {
	// Imported configuration has no discovered IDs. Accept its own evidence only
	// from the seed snapshot; a later discovery must still revoke old members.
	return snapshot.SourceKind == modelEvidenceLegacyMappingUpgrade &&
		member.EvidenceKind == modelEvidenceLegacyMappingUpgrade && member.EvidenceObservedAt == snapshot.ObservedAt
}

// Compare the imported target using the forwarding path's existing model-name
// normalization. Keep the recorded ID and public aliases unchanged.
func legacyModelCatalogWireModel(account *Account, model string) string {
	switch {
	case account.Platform == PlatformOpenAI || account.Platform == PlatformDeepseek:
		return normalizeOpenAIModelForUpstream(account, model)
	case account.Platform == PlatformAnthropic && account.Type != AccountTypeAPIKey:
		model = claude.NormalizeModelID(model)
		if account.Type == AccountTypeServiceAccount {
			model = normalizeVertexAnthropicModelID(model)
		}
		return model
	case account.IsGemini():
		return strings.TrimPrefix(model, "models/")
	default:
		return model
	}
}
