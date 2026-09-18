package service

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
)

const (
	UpstreamModelCatalogExtraKey      = "upstream_model_catalog"
	UpstreamModelCatalogStateExtraKey = "upstream_model_catalog_state"
	UpstreamModelAllowlistExtraKey    = "upstream_model_allowlist"
	ModelCapabilityDefaultsExtraKey   = "model_capability_defaults"
	ModelCapabilityReleaseExtraKey    = "model_capability_release"
	AccountModelCatalogSchemaVersion  = 2
)

type AccountCatalogCapabilitySummary struct {
	Total          int `json:"total"`
	Discovered     int `json:"discovered"`
	MappingTargets int `json:"mapping_targets"`
	Eligible       int `json:"eligible"`
	Complete       int `json:"complete"`
	Estimated      int `json:"estimated"`
	Incomplete     int `json:"incomplete"`
}

type AccountCapabilityRelease struct {
	SchemaVersion     int               `json:"schema_version"`
	EnrichmentVersion string            `json:"enrichment_version"`
	SourceGeneration  int64             `json:"source_generation"`
	SourceVersions    map[string]string `json:"source_versions"`
}

type AccountModelCatalogBaseline struct {
	RequireActive              bool  `json:"-"`
	CredentialAccountID        int64 `json:"-"`
	CredentialIdentityRevision int64 `json:"-"`
	IdentityRevision           int64 `json:"identity_revision,string"`
	RulesRevision              int64 `json:"rules_revision,string"`
	SnapshotRevision           int64 `json:"snapshot_revision,string"`
}

type AccountModelCatalogState struct {
	IdentityRevision int64  `json:"identity_revision"`
	RulesRevision    int64  `json:"rules_revision"`
	AttemptedAt      string `json:"attempted_at,omitempty"`
	LastSuccessAt    string `json:"last_success_at,omitempty"`
	Result           string `json:"result,omitempty"`
	Error            string `json:"error,omitempty"`
}

type AccountCatalogModel struct {
	Category           string   `json:"category,omitempty"`
	ID                 string   `json:"id"`
	Endpoints          []string `json:"endpoints"`
	EvidenceKind       string   `json:"evidence_kind"`
	EvidenceObservedAt string   `json:"evidence_observed_at"`
}

type ExcludedCatalogModel struct {
	ID     string                 `json:"model"`
	Reason CatalogExclusionReason `json:"reason"`
}

type AccountModelCatalogSnapshot struct {
	SchemaVersion              int                              `json:"schema_version,omitempty"`
	EnrichmentVersion          string                           `json:"enrichment_version,omitempty"`
	SourceGeneration           int64                            `json:"source_generation,omitempty"`
	SourceVersions             map[string]string                `json:"source_versions,omitempty"`
	RulesRevision              int64                            `json:"rules_revision"`
	CapabilityStatus           string                           `json:"capability_status,omitempty"`
	CapabilitySummary          *AccountCatalogCapabilitySummary `json:"capability_summary,omitempty"`
	CredentialAccountID        int64                            `json:"credential_account_id,omitempty"`
	CredentialIdentityRevision int64                            `json:"credential_identity_revision,omitempty"`
	Revision                   int64                            `json:"revision"`
	IdentityRevision           int64                            `json:"identity_revision"`
	ObservedAt                 string                           `json:"observed_at"`
	SourceKind                 string                           `json:"source_kind"`
	ProviderProfile            string                           `json:"provider_profile"`
	DiscoveryScope             string                           `json:"discovery_scope"`
	Complete                   bool                             `json:"complete"`
	DiscoveredModels           []string                         `json:"discovered_models"`
	EligibleModels             []AccountCatalogModel            `json:"eligible_models"`
	ExcludedModels             []ExcludedCatalogModel           `json:"excluded_models"`
}

type AccountModelAllowlist struct {
	Enabled          bool     `json:"enabled"`
	Models           []string `json:"models"`
	IdentityRevision int64    `json:"identity_revision"`
	ConfirmedAt      string   `json:"confirmed_at"`
	ConfirmedBy      int64    `json:"confirmed_by"`
	Reason           string   `json:"reason"`
}

type AccountModelCatalogDiff struct {
	Added    []string `json:"added"`
	Removed  []string `json:"removed"`
	Retained []string `json:"retained"`
}

type CatalogExclusionReason string

const (
	CatalogReasonNotSynced              CatalogExclusionReason = "not_synced"
	CatalogReasonIdentityChanged        CatalogExclusionReason = "identity_changed"
	CatalogReasonEntitlementUnconfirmed CatalogExclusionReason = "entitlement_unconfirmed"
	CatalogReasonNotDiscovered          CatalogExclusionReason = "not_discovered"
	CatalogReasonAccountRestricted      CatalogExclusionReason = "account_restricted"
	CatalogReasonRouteAmbiguous         CatalogExclusionReason = "route_ambiguous"
	CatalogReasonRouteTargetUnavailable CatalogExclusionReason = "route_target_unavailable"
	CatalogReasonEndpointUnsupported    CatalogExclusionReason = "endpoint_unsupported"
	CatalogReasonRetired                CatalogExclusionReason = "retired"
	CatalogReasonGroupRestricted        CatalogExclusionReason = "group_restricted"
	CatalogReasonStaleEvidence          CatalogExclusionReason = "stale_evidence"
	CatalogReasonMetadataIncomplete     CatalogExclusionReason = "metadata_incomplete"
)

func equalCatalogSourceVersions(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if other, ok := b[key]; !ok || value != other {
			return false
		}
	}
	return true
}

func decodeCatalogExtra(extra map[string]any, key string, destination any) bool {
	value, exists := extra[key]
	if !exists || value == nil {
		return false
	}
	encoded, err := json.Marshal(value)
	return err == nil && json.Unmarshal(encoded, destination) == nil
}

func (account *Account) GetUpstreamModelCatalog() *AccountModelCatalogSnapshot {
	if account == nil {
		return nil
	}
	var snapshot AccountModelCatalogSnapshot
	if !decodeCatalogExtra(account.Extra, UpstreamModelCatalogExtraKey, &snapshot) || !snapshot.Complete {
		return nil
	}
	return &snapshot
}

func (account *Account) EffectiveUpstreamModelCatalog() *AccountModelCatalogSnapshot {
	return account.EffectiveUpstreamModelCatalogForCredentialAccount(nil)
}

func (account *Account) EffectiveUpstreamModelCatalogForCredentialAccount(credentialAccount *Account) *AccountModelCatalogSnapshot {
	if account == nil || account.ModelCatalogState().Result == "credentials_invalid" {
		return nil
	}
	snapshot := account.GetUpstreamModelCatalog()
	if snapshot != nil && snapshot.IdentityRevision != account.ModelCatalogState().IdentityRevision {
		snapshot = nil
	}
	if snapshot != nil && (account.IsShadow() || snapshot.CredentialAccountID != 0) {
		if credentialAccount == nil || credentialAccount.ID != snapshot.CredentialAccountID || !credentialAccount.IsOpenAIOAuth() || credentialAccount.IsShadow() || credentialAccount.ModelCatalogState().IdentityRevision != snapshot.CredentialIdentityRevision {
			snapshot = nil
		}
	}
	snapshot = appendAvailabilityModels(account, credentialAccount, snapshot)
	if snapshot == nil {
		return nil
	}
	var allowlist AccountModelAllowlist
	decodeCatalogExtra(account.Extra, UpstreamModelAllowlistExtraKey, &allowlist)
	eligible := make([]AccountCatalogModel, 0, len(snapshot.EligibleModels))
	for _, model := range snapshot.EligibleModels {
		allowed := false
		for _, id := range allowlist.Models {
			if id == model.ID {
				allowed = true
			}
		}
		if allowlist.Enabled && !allowed {
			continue
		}
		eligible = append(eligible, model)
	}
	snapshot.EligibleModels = eligible
	return snapshot
}

func (account *Account) ModelCatalogState() AccountModelCatalogState {
	var state AccountModelCatalogState
	if account != nil {
		decodeCatalogExtra(account.Extra, UpstreamModelCatalogStateExtraKey, &state)
	}
	return state
}

func (account *Account) ModelCatalogBaseline() AccountModelCatalogBaseline {
	state := account.ModelCatalogState()
	baseline := AccountModelCatalogBaseline{IdentityRevision: state.IdentityRevision, RulesRevision: state.RulesRevision}
	if snapshot := account.GetUpstreamModelCatalog(); snapshot != nil {
		baseline.SnapshotRevision = snapshot.Revision
	}
	return baseline
}

func AccountCatalogIdentityCredentialFields() []string {
	return []string{"api_key", "base_url", "account_mode", "api_protocol", "api_base_urls", "project_id", "organization_id", "chatgpt_account_id", "chatgpt_user_id", "sub", "account_id", "user_id", "credential_source_account_id", "credential_shadow_account_id", "header_overrides"}
}

func configuredModelMapping(account *Account) map[string]string {
	encoded, _ := json.Marshal(account.Credentials["model_mapping"])
	var mapping map[string]string
	_ = json.Unmarshal(encoded, &mapping)
	return mapping
}

func catalogProtocolEndpoint(protocol string) string {
	if protocol == APIProtocolAnthropic {
		return CompositeRouteEndpointMessages
	}
	return protocol
}

// AccountModelBinding is internal evidence for a concrete outbound target.
// It must never be serialized into a public model-list response.
type AccountModelBinding struct {
	PublicModel         string
	AccountID           int64
	CredentialAccountID int64
	UpstreamModel       string
	Endpoint            string
	RouteSource         string
	EvidenceKind        string
	EvidenceObservedAt  string
	SnapshotRevision    int64
	Supported           bool
}

// AccountModelBindingOptions carries resolved request decisions. FinalModel
// bypasses name rewriting so a later forwarding caller can validate exactly once.
type AccountModelBindingOptions struct {
	Endpoint              string
	FinalModel            string
	MessagesDispatchModel string
	Compact               bool
	CredentialAccount     *Account
}

type modelCatalogEndpointContextKey struct{}

func ResolveAccountModelBinding(ctx context.Context, account *Account, publicModel string, options AccountModelBindingOptions) AccountModelBinding {
	if options.Endpoint == "" && ctx != nil {
		options.Endpoint, _ = ctx.Value(modelCatalogEndpointContextKey{}).(string)
	}
	binding := AccountModelBinding{PublicModel: publicModel, Endpoint: options.Endpoint, RouteSource: CompositeRouteSourceAccount}
	if !catalogAccountEnabled(account) {
		return binding
	}
	binding.AccountID = account.ID
	binding.CredentialAccountID = account.ID
	if account.IsShadow() {
		parent := options.CredentialAccount
		if parent == nil || parent.ID != *account.ParentAccountID || !parent.IsCredentialUsableForShadow() || parent.ModelCatalogState().Result == "credentials_invalid" {
			return binding
		}
		binding.CredentialAccountID = parent.ID
	}
	if platform, ok := ResolvedTargetPlatformFromContext(ctx); ok && platform != account.Platform {
		return binding
	}
	model := strings.TrimSpace(publicModel)
	if original, ok := RequestedPublicModelFromContext(ctx); ok {
		binding.PublicModel = original
	}
	if resolved, ok := ResolvedUpstreamModelFromContext(ctx); ok {
		model = resolved
	}
	if source, ok := CompositeRouteSourceFromContext(ctx); ok {
		binding.RouteSource = source
	}
	if forward, ok := openAIForwardModelFromContext(ctx); ok {
		model = forward.model
		options.Compact = forward.useCompactModelMapping
	}
	if options.FinalModel != "" {
		model = options.FinalModel
	} else {
		_, matched := account.ResolveMappedModel(model)
		if len(configuredModelMapping(account)) > 0 && !matched && !account.IsOpenAIPassthroughEnabled() && options.MessagesDispatchModel == "" {
			return binding
		}
		switch {
		case account.IsBedrock():
			var ok bool
			model, ok = ResolveBedrockModelID(account, model)
			if !ok {
				return binding
			}
		case account.Platform == PlatformAntigravity:
			model = mapAntigravityModel(account, model)
			if thinking, ok := ThinkingEnabledFromContext(ctx); ok {
				model = applyThinkingModelSuffix(model, thinking)
			}
		case account.Platform == PlatformOpenAI:
			if !account.IsOpenAIPassthroughEnabled() {
				model = resolveOpenAIForwardModel(account, model, options.MessagesDispatchModel)
			}
			if options.Compact {
				model = resolveOpenAICompactForwardModel(account, model)
			}
			model = normalizeOpenAIModelForUpstream(account, model)
		default:
			model = account.GetMappedModel(model)
			if account.Platform == PlatformDeepseek {
				model = normalizeOpenAIModelForUpstream(account, model)
			}
			if account.Platform == PlatformAnthropic && account.Type != AccountTypeAPIKey {
				model = claude.NormalizeModelID(model)
				if account.Type == AccountTypeServiceAccount {
					model = normalizeVertexAnthropicModelID(model)
				}
			}
			if account.IsGemini() {
				model = strings.TrimPrefix(model, "models/")
			}
		}
	}
	binding.UpstreamModel = strings.TrimSpace(model)
	snapshot := account.EffectiveUpstreamModelCatalogForCredentialAccount(options.CredentialAccount)
	if snapshot == nil {
		return binding
	}
	binding.SnapshotRevision = snapshot.Revision
	for _, member := range snapshot.EligibleModels {
		if member.ID != binding.UpstreamModel || member.EvidenceKind == "" || (!slices.Contains(snapshot.DiscoveredModels, member.ID) && !hasAvailabilityEvidence(account, options.CredentialAccount, member)) {
			continue
		}
		endpoint := options.Endpoint
		if endpoint == "realtime" {
			endpoint = string(OpenAIEndpointCapabilityLive)
		}
		// Reuse the actual Responses bridge decision; native chat evidence can
		// support that adapter, but never an unrelated endpoint or compact call.
		if options.FinalModel == "" && !options.Compact && endpoint == CompositeRouteEndpointResponses {
			switch {
			case account.IsOpenCodeGo() || account.IsCommandCode():
				endpoint = catalogProtocolEndpoint(modelRoutedNativeProtocol(account, binding.UpstreamModel))
			case account.IsAnthropicProtocol() || account.IsAnthropic():
				endpoint = CompositeRouteEndpointMessages
			case account.IsOpenAICompatible() && shouldForwardOpenAIResponsesViaRawChatCompletions(account):
				endpoint = CompositeRouteEndpointChatCompletions
			}
		}
		if options.FinalModel == "" && endpoint == CompositeRouteEndpointChatCompletions && (account.IsAnthropicProtocol() || account.IsAnthropic()) {
			endpoint = CompositeRouteEndpointMessages
		}
		if endpoint == CompositeRouteEndpointGemini {
			endpoint = "generate_content"
		}
		if account.Platform == PlatformAntigravity && endpoint == CompositeRouteEndpointMessages && options.FinalModel == "" {
			endpoint = "generate_content"
		}
		if endpoint == CompositeRouteEndpointCountTokens {
			endpoint = CompositeRouteEndpointMessages
		}
		if len(member.Endpoints) == 0 || (endpoint != "" && endpoint != CompositeRouteEndpointAny && !catalogEndpointsContain(member.Endpoints, endpoint)) {
			continue
		}
		binding.EvidenceKind, binding.EvidenceObservedAt = member.EvidenceKind, member.EvidenceObservedAt
		binding.Supported = true
		break
	}
	return binding
}

func AccountSupportsResolvedModel(ctx context.Context, account *Account, publicModel string, options AccountModelBindingOptions) bool {
	return ResolveAccountModelBinding(ctx, account, publicModel, options).Supported
}

func catalogAccountEnabled(account *Account) bool {
	return account != nil && account.IsActive() && account.Schedulable && (account.ExpiresAt == nil || time.Now().Before(*account.ExpiresAt))
}

func catalogEndpointsContain(endpoints []string, endpoint string) bool {
	for _, candidate := range endpoints {
		if candidate == "realtime" {
			candidate = string(OpenAIEndpointCapabilityLive)
		}
		if candidate == endpoint {
			return true
		}
	}
	return false
}
