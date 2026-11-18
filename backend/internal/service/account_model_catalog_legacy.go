package service

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/claude"
)

const modelEvidenceLegacyMappingUpgrade = "legacy-mapping-upgrade"

// A seed is written once by the upgrade migration, inside the protected catalog
// field. It carries only finite historical account configuration, not a claim
// that discovery or a probe succeeded. New accounts never enter this path.
func legacyModelCatalogValidFor(account *Account, snapshot *AccountModelCatalogSnapshot) bool {
	state := account.ModelCatalogState()
	if snapshot.LegacyAccountID != account.ID || account.ID <= 0 || snapshot.SourceKind != modelEvidenceLegacyMappingUpgrade || snapshot.DiscoveryScope != modelEvidenceLegacyMappingUpgrade {
		return false
	}
	if snapshot.LegacyMappingFingerprint == "" {
		return snapshot.LegacyPolicyRevision == nil && snapshot.IdentityRevision == state.IdentityRevision && snapshot.RulesRevision == state.RulesRevision
	}
	fingerprint, valid := LegacyModelMappingFingerprint(account.Credentials)
	return valid && fingerprint == snapshot.LegacyMappingFingerprint && snapshot.LegacyPolicyRevision != nil && *snapshot.LegacyPolicyRevision == state.LegacyPolicyRevision
}

// LegacyModelMappingFingerprint hashes only saved configuration. Format 1 uses
// encoding/json's sorted object keys and JSON string escaping, without trimming
// or Unicode normalization. Missing and null encode as null; {} remains distinct.
// Only string-valued objects are accepted; arrays, scalars and null/non-string
// members fail closed. No platform defaults or runtime aliases enter this hash.
func LegacyModelMappingFingerprint(credentials map[string]any) (string, bool) {
	encoded, err := json.Marshal(credentials["model_mapping"])
	if err != nil {
		return "", false
	}
	var raw any
	if json.Unmarshal(encoded, &raw) != nil {
		return "", false
	}
	if raw != nil {
		mapping, valid := raw.(map[string]any)
		if !valid {
			return "", false
		}
		for _, target := range mapping {
			if _, valid := target.(string); !valid {
				return "", false
			}
		}
	}
	canonical, err := json.Marshal(raw)
	if err != nil {
		return "", false
	}
	return fmt.Sprintf("sha256:1:%x", sha256.Sum256(canonical)), true
}

// UpgradeLegacyModelCatalogEvidence must be persisted under the account row lock
// before applying configuration changes. It never repairs an invalid old seed.
func UpgradeLegacyModelCatalogEvidence(account *Account) bool {
	if account == nil || !account.IsSchedulable() || (account.ExpiresAt != nil && !time.Now().Before(*account.ExpiresAt)) || account.IsShadow() || account.ModelCatalogState().Result == "credentials_invalid" {
		return false
	}
	var snapshot AccountModelCatalogSnapshot
	if !decodeCatalogExtra(account.Extra, UpstreamModelCatalogExtraKey, &snapshot) || !snapshot.Complete || snapshot.LegacyMappingFingerprint != "" || snapshot.LegacyPolicyRevision != nil || snapshot.CredentialAccountID != 0 || !legacyModelCatalogValidFor(account, &snapshot) {
		return false
	}
	for _, member := range snapshot.EligibleModels {
		if !hasLegacyModelCatalogEvidence(&snapshot, member) || member.ID == "" || member.ID != strings.TrimSpace(member.ID) || strings.ContainsAny(member.ID, "*?\r\n") {
			return false
		}
	}
	fingerprint, valid := LegacyModelMappingFingerprint(account.Credentials)
	if !valid {
		return false
	}
	policyRevision := account.ModelCatalogState().LegacyPolicyRevision
	snapshot.LegacyMappingFingerprint = fingerprint
	snapshot.LegacyPolicyRevision = &policyRevision
	snapshot.Revision++
	account.Extra[UpstreamModelCatalogExtraKey] = &snapshot
	return true
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
			case account.IsOpenAIOAuth() || (account.IsCursor() && account.Type == AccountTypeUpstream):
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
