package service

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/claude"
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
	// Unlike identity/rules, this excludes pure access-token renewal. Mapping
	// changes and explicit revocations advance it even if content later returns.
	LegacyPolicyRevision int64  `json:"legacy_policy_revision"`
	IdentityRevision     int64  `json:"identity_revision"`
	RulesRevision        int64  `json:"rules_revision"`
	AttemptedAt          string `json:"attempted_at,omitempty"`
	// Older rows have no attempt binding, so they are immediately due if their
	// snapshot belongs to an earlier identity.
	AttemptedIdentityRevision *int64 `json:"attempted_identity_revision,omitempty"`
	LastSuccessAt             string `json:"last_success_at,omitempty"`
	Result                    string `json:"result,omitempty"`
	Error                     string `json:"error,omitempty"`
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
	LegacyMappingFingerprint string `json:"legacy_mapping_fingerprint,omitempty"`
	// A missing policy binding must not be mistaken for a trusted zero revision.
	LegacyPolicyRevision       *int64                           `json:"legacy_policy_revision,omitempty"`
	LegacyAccountID            int64                            `json:"legacy_account_id,omitempty"`
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
	raw, exists := account.Extra[UpstreamModelCatalogExtraKey]
	if !exists || raw == nil {
		return nil
	}
	ptr, sig, identifiable := extraValueIdentity(raw)
	var snapshot *AccountModelCatalogSnapshot
	if identifiable && account.catalogMemoReady && account.catalogMemoPtr == ptr && account.catalogMemoKeysSig == sig {
		snapshot = account.catalogMemoValue
	} else {
		var decoded AccountModelCatalogSnapshot
		valid := decodeCatalogExtra(account.Extra, UpstreamModelCatalogExtraKey, &decoded) && decoded.Complete
		if identifiable {
			account.catalogMemoReady = true
			account.catalogMemoPtr = ptr
			account.catalogMemoKeysSig = sig
			if valid {
				account.catalogMemoValue = &decoded
			} else {
				account.catalogMemoValue = nil
			}
		}
		if !valid {
			return nil
		}
		snapshot = &decoded
	}
	if snapshot == nil {
		return nil
	}
	// 返回浅副本：populate、可用性追加、白名单过滤都会改写 EligibleModels，
	// 不能污染 memo 中的解码快照。JSON 解码的切片 cap==len，后续 append 会
	// 重新分配，不会写穿 memo 的底层数组。
	cp := *snapshot
	if cp.SourceKind == modelEvidenceLegacyMappingUpgrade {
		populateLegacyModelCatalogEndpoints(account, &cp)
	}
	return &cp
}

func (account *Account) EffectiveUpstreamModelCatalog() *AccountModelCatalogSnapshot {
	return account.EffectiveUpstreamModelCatalogForCredentialAccount(nil)
}

// Keep the existing token-rotation exception and credential-owner checks shared
// by catalog projection and configured-selection admission.
func modelCatalogIdentityMatches(account, credentialAccount *Account, snapshot *AccountModelCatalogSnapshot) bool {
	if snapshot == nil {
		return true
	}
	if snapshot.IdentityRevision != account.ModelCatalogState().IdentityRevision {
		legacyRotationEvidence := snapshot.SourceKind == modelEvidenceLegacyMappingUpgrade && snapshot.LegacyMappingFingerprint != "" && legacyModelCatalogValidFor(account, snapshot)
		if !legacyRotationEvidence {
			return false
		}
	}
	if account.IsShadow() || snapshot.CredentialAccountID != 0 {
		return credentialAccount != nil && credentialAccount.ID == snapshot.CredentialAccountID && credentialAccount.IsOpenAIOAuth() && !credentialAccount.IsShadow() && credentialAccount.ModelCatalogState().IdentityRevision == snapshot.CredentialIdentityRevision
	}
	return true
}

func configuredSelectionIdentityMatches(account, credentialAccount *Account, snapshot *AccountModelCatalogSnapshot) bool {
	// A migration seed describes historical configuration, not a publication.
	// Its current explicit selection keeps the existing legacy edit semantics.
	// A real publication, however, cannot be replaced with synthetic configured
	// evidence after its account or credential-owner identity has been revoked.
	return snapshot == nil || snapshot.SourceKind == modelEvidenceLegacyMappingUpgrade || modelCatalogIdentityMatches(account, credentialAccount, snapshot)
}

func (account *Account) EffectiveUpstreamModelCatalogForCredentialAccount(credentialAccount *Account) *AccountModelCatalogSnapshot {
	if account != nil && account.modelCatalogReadEvidence != nil && account.modelCatalogCredentialAccount == credentialAccount {
		return account.modelCatalogReadEvidence.catalog
	}
	if account == nil {
		return nil
	}
	snapshot := account.GetUpstreamModelCatalog()
	identityMatches := modelCatalogIdentityMatches(account, credentialAccount, snapshot)
	configuredIdentityMatches := configuredSelectionIdentityMatches(account, credentialAccount, snapshot)
	if account.ModelCatalogState().Result == "credentials_invalid" || !identityMatches {
		snapshot = nil
	}
	if snapshot != nil && snapshot.SourceKind == modelEvidenceLegacyMappingUpgrade && !legacyModelCatalogValidFor(account, snapshot) {
		snapshot = nil
	}
	snapshot = appendAvailabilityModels(account, credentialAccount, snapshot)
	if configuredIdentityMatches {
		snapshot = appendConfiguredSelectionModels(account, credentialAccount, snapshot)
		snapshot = appendConfiguredCompactSelectionModels(account, credentialAccount, snapshot)
	}
	if snapshot == nil {
		return nil
	}
	var allowlist AccountModelAllowlist
	decodeCatalogExtra(account.Extra, UpstreamModelAllowlistExtraKey, &allowlist)
	if legacyUpgradeFreeze(allowlist) {
		// This marker was written when an upgraded account had no saved explicit
		// selection. Native discovery was the legacy authorization, not a new
		// administrator-approved expansion. Only an explicit admin deny-all may
		// suppress those discoveries.
		allowlist.Enabled = false
	}
	eligible := make([]AccountCatalogModel, 0, len(snapshot.EligibleModels))
	for _, model := range snapshot.EligibleModels {
		if !zenFreeModelAllowed(account, model.ID) {
			continue
		}
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

func legacyUpgradeFreeze(scope AccountModelAllowlist) bool {
	return scope.Enabled && len(scope.Models) == 0 && scope.Reason == "system_upgrade_baseline_freeze" && scope.ConfirmedBy == 0 && scope.ConfirmedAt == ""
}

func (account *Account) ModelCatalogState() AccountModelCatalogState {
	if account != nil && account.modelCatalogReadEvidence != nil {
		return account.modelCatalogReadEvidence.state
	}
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

func WorkBuddyCatalogIdentityFields() []string {
	return []string{"site", "domain", "uid", "enterprise_id"}
}

func AccountCatalogIdentityCredentialFields(platform string) []string {
	fields := []string{"api_key", "base_url", "account_mode", "api_protocol", "api_base_urls", "project_id", "organization_id", "chatgpt_account_id", "chatgpt_user_id", "sub", "account_id", "user_id", "credential_source_account_id", "credential_shadow_account_id", "header_overrides"}
	if platform == PlatformWorkBuddy {
		fields = append(fields, WorkBuddyCatalogIdentityFields()...)
	}
	if platform == PlatformAntigravity {
		fields = append(fields, "email")
	}
	if platform == PlatformGrok {
		fields = append(fields, "team_id")
	}
	return fields
}

// Changes to any identity field already revoke the catalog. These nonempty
// subject identifiers allow an otherwise unchanged OAuth identity to renew.
func AccountCatalogSubjectFields(platform string) []string {
	if platform == PlatformWorkBuddy {
		return []string{"uid"}
	}
	if platform == PlatformAntigravity {
		return []string{"project_id"}
	}
	return []string{"chatgpt_account_id", "chatgpt_user_id", "sub", "account_id", "user_id"}
}

func configuredModelMapping(account *Account) map[string]string {
	if account.modelCatalogReadEvidence != nil {
		return account.modelCatalogReadEvidence.mapping
	}
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
	PublicModel                string
	AccountID                  int64
	CredentialAccountID        int64
	UpstreamModel              string
	Endpoint                   string
	RouteSource                string
	EvidenceKind               string
	EvidenceObservedAt         string
	SnapshotRevision           int64
	IdentityRevision           int64
	RulesRevision              int64
	CredentialIdentityRevision int64
	Supported                  bool
}

// AccountModelBindingOptions carries resolved request decisions. FinalModel
// bypasses name rewriting so a later forwarding caller can validate exactly once.
type AccountModelBindingOptions struct {
	Endpoint              string
	FinalModel            string
	MessagesDispatchModel string
	Compact               bool
	CredentialAccount     *Account
	// Passthrough carries the actual Responses WS ingress/forwarding decision.
	Passthrough        bool
	allowInternalModel bool
}

type modelCatalogEndpointContextKey struct{}

// Carries the authenticated dispatch configuration without altering catalog retry group reads.
type accountMessagesDispatchGroupKey struct{}

func ResolveAccountModelBinding(ctx context.Context, account *Account, publicModel string, options AccountModelBindingOptions) AccountModelBinding {
	if options.Endpoint == "" && ctx != nil {
		options.Endpoint, _ = ctx.Value(modelCatalogEndpointContextKey{}).(string)
	}
	binding := AccountModelBinding{PublicModel: publicModel, Endpoint: options.Endpoint, RouteSource: CompositeRouteSourceAccount}
	if !catalogAccountEnabled(account) || account.ModelSelectionDeniesAll() || !accountImplementsGatewayAction(ctx, account) {
		return binding
	}
	binding.AccountID = account.ID
	binding.CredentialAccountID = account.ID
	if account.IsShadow() {
		parent := options.CredentialAccount
		if parent == nil || parent.ID != *account.ParentAccountID || !parent.IsCredentialUsableForShadow() {
			return binding
		}
		binding.CredentialAccountID = parent.ID
	}
	if !accountAllowedByHostedRequest(ctx, account) {
		return binding
	}
	if platform, ok := ResolvedTargetPlatformFromContext(ctx); ok && !account.MatchesPlatform(platform, RequiredRouteAccountType(ctx)) {
		return binding
	}
	model := strings.TrimSpace(publicModel)
	if original, ok := RequestedPublicModelFromContext(ctx); ok {
		binding.PublicModel = original
	}
	if !visibleModelPolicyAllows(ctx, binding.PublicModel) || !modelAccessAccountAllows(ctx, account.ID, binding.PublicModel) {
		return binding
	}
	if options.MessagesDispatchModel == "" && account.Platform == PlatformOpenAI {
		if plan := GatewayRequestPlanFromContext(ctx); plan != nil && plan.Snapshot().Protocol == "anthropic" {
			if group, ok := ctx.Value(accountMessagesDispatchGroupKey{}).(*Group); ok && group.AllowMessagesDispatch && (group.Platform == PlatformOpenAI || group.Platform == PlatformComposite) {
				options.MessagesDispatchModel = group.ResolveMessagesDispatchModel(binding.PublicModel)
			}
		}
	}
	if resolved, ok := ResolvedUpstreamModelFromContext(ctx); ok {
		model = resolved
	}
	if source, ok := CompositeRouteSourceFromContext(ctx); ok {
		binding.RouteSource = source
	}
	if forward, ok := openAIForwardModelFromContext(ctx); ok {
		model = forward.model
		options.Compact = options.Compact || forward.useCompactModelMapping
	}
	selection := account.restrictedPublicModels()
	if selection != nil {
		options.allowInternalModel = accountContextRewritesModel(ctx, model, binding.PublicModel)
		if !selection.allows(account, model, options.allowInternalModel) {
			return binding
		}
	}
	if defaultMode, websocket := ctx.Value(openAIWebSocketSelectionKey{}).(string); websocket && options.FinalModel == "" && account.ResolveOpenAIResponsesWebSocketV2Mode(defaultMode) == OpenAIWSIngressModePassthrough && (selection != nil || configuredPassthroughModel(account, model)) {
		options.Passthrough = true
		options.FinalModel = model
	}
	// A passthrough request deliberately does not apply account aliases. Its
	// exact configured key is therefore a selected wire name, even when the
	// same mapping has a different value for the ordinary forwarding path.
	// HTTP passthrough also sends the configured key unchanged. Compact targets
	// still use the ordinary compact membership checks below.
	httpPassthrough := accountUsesHTTPResponsesPassthrough(ctx, account, options.Endpoint) && !options.Passthrough
	if httpPassthrough {
		wireModel := options.FinalModel
		if wireModel == "" {
			wireModel, _ = computeAccountBindingModel(ctx, account, model, options)
		}
		if configuredPassthroughModel(account, wireModel) {
			options.FinalModel = wireModel
		}
	}
	if (options.Passthrough && options.Endpoint == CompositeRouteEndpointResponses || httpPassthrough) && options.FinalModel != "" && configuredPassthroughModel(account, options.FinalModel) {
		if !configuredSelectionIdentityMatches(account, options.CredentialAccount, account.GetUpstreamModelCatalog()) {
			return binding
		}
		binding.UpstreamModel = options.FinalModel
		binding.IdentityRevision = account.ModelCatalogState().IdentityRevision
		binding.RulesRevision = account.ModelCatalogState().RulesRevision
		if options.CredentialAccount != nil {
			binding.CredentialIdentityRevision = options.CredentialAccount.ModelCatalogState().IdentityRevision
		}
		var scope AccountModelAllowlist
		decodeCatalogExtra(account.Extra, UpstreamModelAllowlistExtraKey, &scope)
		if scope.Enabled && !slices.Contains(scope.Models, options.FinalModel) {
			return binding
		}
		// The installed WS relay supplies its endpoint directly. HTTP retains
		// the account's existing adapter endpoint calculation.
		member := AccountCatalogModel{ID: options.FinalModel, Endpoints: []string{CompositeRouteEndpointResponses}, EvidenceKind: ModelEvidenceConfiguredSelection}
		if !options.Passthrough {
			member.Endpoints = configuredSelectionEndpoints(account, options.FinalModel)
		}
		uncached := *account
		uncached.modelCatalogReadEvidence = nil
		binding.Supported = computeAccountBindingEndpoint(&uncached, options.FinalModel, options, 0, member)
		binding.EvidenceKind = member.EvidenceKind
		return binding
	}
	var mapped bool
	if account.IsOpenAIPassthroughEnabled() {
		// The shared name memo predates transport-specific mapping and omits
		// protocol/WS mode from its key. Do not publish this local decision there.
		model, mapped = computeAccountBindingModel(ctx, account, model, options)
	} else {
		model, mapped = memoizedAccountBindingModel(ctx, account, model, options)
	}
	if !mapped {
		return binding
	}
	binding.UpstreamModel = strings.TrimSpace(model)
	snapshot := account.EffectiveUpstreamModelCatalogForCredentialAccount(options.CredentialAccount)
	if snapshot == nil {
		return binding
	}
	binding.SnapshotRevision = snapshot.Revision
	binding.IdentityRevision = account.ModelCatalogState().IdentityRevision
	binding.RulesRevision = account.ModelCatalogState().RulesRevision
	if options.CredentialAccount != nil {
		binding.CredentialIdentityRevision = options.CredentialAccount.ModelCatalogState().IdentityRevision
	}
	members := snapshot.EligibleModels
	if account.modelCatalogReadEvidence != nil {
		members = account.modelCatalogReadEvidence.members[binding.UpstreamModel]
		for _, member := range snapshot.EligibleModels {
			if member.ID != binding.UpstreamModel && normalizeAccountBindingWireModel(account, member.ID) == binding.UpstreamModel && (configuredSelectionEvidence(account, member) || configuredCompactSelectionEvidence(account, member)) {
				// These protocol-normalized members are indexed by their literal saved ID.
				members = append(append([]AccountCatalogModel{}, members...), member)
			}
		}
	}
	for memberIndex, member := range members {
		legacyEvidence := hasLegacyModelCatalogEvidence(snapshot, member)
		memberModel := member.ID
		if configuredSelectionEvidence(account, member) || configuredCompactSelectionEvidence(account, member) {
			memberModel = normalizeAccountBindingWireModel(account, memberModel)
		}
		if legacyEvidence {
			memberModel = legacyModelCatalogWireModel(account, memberModel)
		}
		discovered := false
		if account.modelCatalogReadEvidence != nil {
			discovered = account.modelCatalogReadEvidence.discovered[member.ID]
		} else {
			discovered = slices.Contains(snapshot.DiscoveredModels, member.ID)
		}
		if memberModel != binding.UpstreamModel || member.EvidenceKind == "" || (!discovered && !hasAvailabilityEvidence(account, options.CredentialAccount, member) && !legacyEvidence && !configuredSelectionEvidence(account, member) && (!options.Compact || !configuredCompactSelectionEvidence(account, member))) {
			continue
		}
		executable := false
		if member.ID != binding.UpstreamModel && (configuredSelectionEvidence(account, member) || configuredCompactSelectionEvidence(account, member)) {
			normalized := *account
			normalized.modelCatalogReadEvidence = nil
			executable = computeAccountBindingEndpoint(&normalized, binding.UpstreamModel, options, memberIndex, member)
		} else {
			executable = memoizedAccountBindingEndpoint(ctx, account, binding.UpstreamModel, options, memberIndex, member)
		}
		if !executable {
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

// The account flag controls HTTP Responses forwarding only. WS ingress has its
// own adapter decision, and compatibility endpoints apply account mappings even
// when their serialized upstream request happens to use /responses.
func accountUsesHTTPResponsesPassthrough(ctx context.Context, account *Account, endpoint string) bool {
	if account == nil || !account.IsOpenAIPassthroughEnabled() {
		return false
	}
	// Forward chooses these installed compatibility adapters before inspecting
	// the passthrough flag. They still send the account mapping's target.
	if account.Platform == PlatformOpenRouter || account.IsAnthropicProtocol() || shouldForwardOpenAIResponsesViaRawChatCompletions(account) {
		return false
	}
	if _, websocket := ctx.Value(openAIWebSocketSelectionKey{}).(string); websocket {
		return false
	}
	if plan := GatewayRequestPlanFromContext(ctx); plan != nil {
		spec := plan.Snapshot()
		if spec.ExecutionKind == "turn" || spec.Protocol != "" && spec.Protocol != "responses" {
			return false
		}
	}
	return endpoint == "" || endpoint == CompositeRouteEndpointAny || endpoint == CompositeRouteEndpointResponses || endpoint == "responses/compact"
}

func computeAccountBindingModel(ctx context.Context, account *Account, model string, options AccountModelBindingOptions) (string, bool) {
	if options.FinalModel != "" {
		if selection := account.restrictedPublicModels(); selection != nil {
			if !selection.allowsWireModel(account, options.FinalModel) {
				return "", false
			}
		} else if len(configuredModelMapping(account)) > 0 {
			selected := false
			for _, target := range configuredConcreteModelIDs(account) {
				if normalizeAccountBindingWireModel(account, target) == options.FinalModel {
					selected = true
					break
				}
			}
			if !selected && options.Compact {
				for _, target := range account.GetCompactModelMapping() {
					if exactConfiguredModel(target) && normalizeAccountBindingWireModel(account, target) == options.FinalModel {
						selected = true
						break
					}
				}
			}
			if !selected {
				requestedOptions := options
				requestedOptions.FinalModel = ""
				resolved, allowed := computeAccountBindingModel(ctx, account, model, requestedOptions)
				selected = allowed && resolved == options.FinalModel
			}
			if !selected {
				return "", false
			}
		}
		model = options.FinalModel
	} else {
		var variant string
		var variantResolved bool
		if selection := account.restrictedPublicModels(); selection != nil {
			if !selection.allows(account, model, options.allowInternalModel) {
				return "", false
			}
		} else {
			variant, variantResolved = resolveGeminiThinkingVariantForLevel(account, model, "")
			_, matched := resolveRequestedModelInMapping(configuredModelMapping(account), model)
			if !matched {
				normalized := normalizeRequestedModelForLookup(account.Platform, model)
				if normalized != model {
					_, matched = resolveRequestedModelInMapping(configuredModelMapping(account), normalized)
				}
			}
			discoveredAntigravity := false
			if account.Platform == PlatformAntigravity && !matched && !variantResolved {
				var scope AccountModelAllowlist
				decodeCatalogExtra(account.Extra, UpstreamModelAllowlistExtraKey, &scope)
				if legacyUpgradeFreeze(scope) {
					if snapshot := account.GetUpstreamModelCatalog(); snapshot != nil && slices.Contains(snapshot.DiscoveredModels, model) {
						discoveredAntigravity = true
					}
				}
			}
			if len(configuredModelMapping(account)) > 0 && !matched && !variantResolved && !discoveredAntigravity && options.MessagesDispatchModel == "" {
				return "", false
			}
		}
		switch {
		case account.IsBedrock():
			var ok bool
			model, ok = ResolveBedrockModelID(account, model)
			if !ok {
				return "", false
			}
		case account.Platform == PlatformAntigravity:
			if variantResolved {
				model = variant
			} else {
				model = mapAntigravityModel(account, model)
			}
			if thinking, ok := ThinkingEnabledFromContext(ctx); ok {
				model = applyThinkingModelSuffix(model, thinking)
			}
		case account.Platform == PlatformOpenAI:
			if !accountUsesHTTPResponsesPassthrough(ctx, account, options.Endpoint) {
				model = resolveOpenAIForwardModel(account, model, options.MessagesDispatchModel)
			}
			if options.Compact {
				model = resolveOpenAICompactForwardModel(account, model)
			}
			model = normalizeOpenAIModelForUpstream(account, model)
		default:
			model = account.GetMappedModel(model)
			model = normalizeAccountBindingWireModel(account, model)
		}
	}
	return model, true
}

// Use the same protocol normalization for configured members and outbound mapping targets.
func normalizeAccountBindingWireModel(account *Account, model string) string {
	if account.Platform == PlatformOpenAI || account.Platform == PlatformDeepseek {
		return normalizeOpenAIModelForUpstream(account, model)
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
	return model
}

func computeAccountBindingEndpoint(account *Account, model string, options AccountModelBindingOptions, memberIndex int, member AccountCatalogModel) bool {
	endpoint := options.Endpoint
	if IsCompatibleProvider(account.Platform) && (endpoint != "" || options.Compact) {
		if options.Compact {
			return false
		}
		if options.FinalModel == "" {
			action := chatOnlyCatalogEndpointAction(endpoint)
			if action == "" {
				switch endpoint {
				case "messages":
					action = "messages"
				case "responses":
					action = "responses"
				case "chat_completions":
					action = "chat"
				case "gemini", "generate_content":
					action = "generate_content"
				}
			}
			ep, err := account.compatibleEndpoint(action, model)
			if err != nil {
				// Kaggle notebook image/video cells are bound in the provider
				// action matrix, not as CompatibleEndpoint protocol rows.
				if !platformActionAdapterBound(account, action) {
					return false
				}
			} else {
				endpoint = catalogProtocolEndpoint(ep.Protocol)
			}
		} else if !catalogEndpointsContain(accountCatalogNativeTextEndpoints(account, model), endpoint) {
			action := chatOnlyCatalogEndpointAction(endpoint)
			if action == "" || !platformActionAdapterBound(account, action) {
				return false
			}
		}
	}
	if options.FinalModel != "" && account.IsMultiProtocolAPIKey() && !account.IsAdaptiveAPIProtocol() && chatOnlyCatalogHasTextMethod([]string{endpoint}) && !catalogEndpointsContain(accountCatalogNativeTextEndpoints(account, model), endpoint) {
		// At the send boundary this is the actual wire family, not an ingress
		// that can still be converted by a compatibility adapter.
		return false
	}
	endpointDeclarationChanged := false
	if endpoint == "embeddings" && member.EvidenceKind == ModelEvidenceConfiguredSelection {
		// Older configuration-only projections included every installed adapter.
		// Recompute this family from model evidence before using a saved projection.
		metadata, _ := account.GetUpstreamModelMetadata(member.ID)
		if !catalogEndpointsContain(accountCatalogModelEndpoints(account, member.ID, metadata), endpoint) {
			return false
		}
	}
	if concreteMappedModel(account, member.ID) {
		// Saved manual selections may name private or newly released media
		// models. Optional task facts cannot remove installed action adapters.
		member.Endpoints = append(append([]string(nil), member.Endpoints...), adapterBoundCatalogFamilies(account)...)
		endpointDeclarationChanged = true
	}
	if len(member.Endpoints) == 0 {
		// A previously selected catalog member remains selected when optional
		// endpoint metadata was absent at discovery time.
		metadata, _ := account.GetUpstreamModelMetadata(member.ID)
		member.Endpoints = accountCatalogModelEndpoints(account, member.ID, metadata)
		endpointDeclarationChanged = true
	}
	if account.IsAdaptiveAPIProtocol() && chatOnlyCatalogHasTextMethod(member.Endpoints) {
		member.Endpoints = append(append([]string(nil), member.Endpoints...), accountCatalogNativeTextEndpoints(account, model)...)
		endpointDeclarationChanged = true
	}
	if account.IsCursor() && account.Type != AccountTypeUpstream {
		// Older snapshots called the native Cursor text adapter Responses.
		// Normalize that declaration without changing the stored model selection.
		member.Endpoints = append([]string(nil), member.Endpoints...)
		for index, family := range member.Endpoints {
			if family == CompositeRouteEndpointResponses {
				member.Endpoints[index] = CompositeRouteEndpointChatCompletions
			}
		}
		endpointDeclarationChanged = true
	}
	if endpoint == "realtime" {
		endpoint = string(OpenAIEndpointCapabilityLive)
	}
	// Reuse the actual Responses bridge decision; native chat evidence can
	// support that adapter, but never an unrelated endpoint or compact call.
	if options.FinalModel == "" && !options.Compact && endpoint == CompositeRouteEndpointResponses {
		switch {
		case account.IsOpenCodeGo() || account.IsCommandCode():
			endpoint = catalogProtocolEndpoint(modelRoutedNativeProtocol(account, model))
		case account.IsAnthropicProtocol() || account.IsAnthropic() || account.IsBedrock():
			endpoint = CompositeRouteEndpointMessages
		case account.IsOpenAICompatible() && shouldForwardOpenAIResponsesViaRawChatCompletions(account):
			endpoint = CompositeRouteEndpointChatCompletions
		}
	}
	if options.FinalModel == "" && endpoint == CompositeRouteEndpointChatCompletions && (account.IsAnthropicProtocol() || account.IsAnthropic() || account.IsBedrock()) {
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
	var implemented []string
	if account.modelCatalogReadEvidence != nil && !endpointDeclarationChanged {
		implemented = account.modelCatalogReadEvidence.implemented[model][memberIndex]
	} else {
		implemented = accountCatalogModelEndpoints(account, member.ID, UpstreamModelMetadata{SupportedMethods: member.Endpoints})
	}
	executable := false
	for _, action := range implemented {
		if catalogEndpointsContain(member.Endpoints, action) && (endpoint == "" || endpoint == CompositeRouteEndpointAny || catalogEndpointsContain([]string{action}, endpoint)) {
			executable = true
			break
		}
	}
	return executable
}

// openAIAPIKeyCatalogIsChatOnly reports whether discovery proved that every
// text model on an OpenAI API-key upstream speaks Chat Completions and none
// speaks Responses. A custom OpenAI-compatible gateway often exposes only
// /v1/chat/completions; treating its missing Responses probe as "unknown"
// would route the request to an endpoint the gateway never advertised.
func openAIAPIKeyCatalogIsChatOnly(account *Account) bool {
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey {
		return false
	}
	var snapshot AccountModelCatalogSnapshot
	if !decodeCatalogExtra(account.Extra, UpstreamModelCatalogExtraKey, &snapshot) || !snapshot.Complete {
		return false
	}
	// Legacy mapping snapshots synthesize endpoint evidence by asking the
	// forwarding policy whether it needs Chat Completions. Reading the effective
	// projection here would re-enter that synthesis without a recursion step.
	if snapshot.SourceKind == modelEvidenceLegacyMappingUpgrade || len(snapshot.EligibleModels) == 0 {
		return false
	}
	hasChat := false
	for _, member := range snapshot.EligibleModels {
		if catalogEndpointsContain(member.Endpoints, CompositeRouteEndpointResponses) {
			return false
		}
		if catalogEndpointsContain(member.Endpoints, CompositeRouteEndpointChatCompletions) {
			hasChat = true
		}
	}
	return hasChat
}
