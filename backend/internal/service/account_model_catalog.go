package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

var ErrAccountModelCatalogConflict = errors.New("account model catalog changed; preview again")

func populateAccountCatalogCapabilities(account *Account, catalog *UpstreamModelCatalog, snapshot *AccountModelCatalogSnapshot) {
	snapshot.SchemaVersion = AccountModelCatalogSchemaVersion
	snapshot.EnrichmentVersion = "capability-v2"
	snapshot.RulesRevision = account.ModelCatalogBaseline().RulesRevision
	// Read the wire contract so this persistence layer remains compatible with
	// independently evolved profile fields without inventing missing evidence.
	encoded, _ := json.Marshal(catalog)
	var release AccountCapabilityRelease
	_ = json.Unmarshal(encoded, &release)
	if release.EnrichmentVersion != "" {
		snapshot.EnrichmentVersion = release.EnrichmentVersion
	}
	snapshot.SourceGeneration = release.SourceGeneration
	snapshot.SourceVersions = release.SourceVersions
	ids := make(map[string]bool)
	for _, id := range snapshot.DiscoveredModels {
		ids[id] = true
	}
	targets := make(map[string]bool)
	for _, id := range configuredModelMapping(account) {
		if id != "" && !strings.ContainsAny(id, "*?") {
			targets[id] = true
			ids[id] = true
		}
	}
	summary := &AccountCatalogCapabilitySummary{Total: len(ids), Discovered: len(snapshot.DiscoveredModels), MappingTargets: len(targets), Eligible: len(snapshot.EligibleModels)}
	for id := range ids {
		encoded, _ := json.Marshal(catalog.Metadata[id])
		var profile struct {
			MetadataStatus string `json:"metadata_status"`
		}
		_ = json.Unmarshal(encoded, &profile)
		switch profile.MetadataStatus {
		case "complete":
			summary.Complete++
		case "estimated":
			summary.Estimated++
		default:
			summary.Incomplete++
		}
	}
	snapshot.CapabilitySummary = summary
	snapshot.CapabilityStatus = "complete"
	if summary.Estimated > 0 {
		snapshot.CapabilityStatus = "estimated"
	}
	if summary.Incomplete > 0 {
		snapshot.CapabilityStatus = "incomplete"
	}
}

// AccountModelMetadataEnvelope preserves every model field (including presence,
// provenance and reasoning_control) while adding the account commit's binding.
func AccountModelMetadataEnvelope(snapshot *AccountModelCatalogSnapshot, metadata *UpstreamModelMetadataSnapshot) any {
	if snapshot == nil || snapshot.SchemaVersion < AccountModelCatalogSchemaVersion || metadata == nil {
		return metadata
	}
	return struct {
		*UpstreamModelMetadataSnapshot
		SchemaVersion              int               `json:"schema_version"`
		EnrichmentVersion          string            `json:"enrichment_version"`
		SourceGeneration           int64             `json:"source_generation"`
		SourceVersions             map[string]string `json:"source_versions"`
		IdentityRevision           int64             `json:"identity_revision"`
		RulesRevision              int64             `json:"rules_revision"`
		SnapshotRevision           int64             `json:"snapshot_revision"`
		CredentialAccountID        int64             `json:"credential_account_id,omitempty"`
		CredentialIdentityRevision int64             `json:"credential_identity_revision,omitempty"`
	}{metadata, snapshot.SchemaVersion, snapshot.EnrichmentVersion, snapshot.SourceGeneration, snapshot.SourceVersions, snapshot.IdentityRevision, snapshot.RulesRevision, snapshot.Revision, snapshot.CredentialAccountID, snapshot.CredentialIdentityRevision}
}

// ValidateAccountModelCatalogVersion must also run under the repository row lock.
// Equal generations require identical sources; advancing a generation can never
// silently change the enrichment algorithm without an explicit approval pin.
func ValidateAccountModelCatalogVersion(extra map[string]any, candidate *AccountModelCatalogSnapshot) error {
	if candidate == nil {
		return nil
	}
	var previous AccountModelCatalogSnapshot
	decodeCatalogExtra(extra, UpstreamModelCatalogExtraKey, &previous)
	if candidate.SchemaVersion < previous.SchemaVersion || candidate.SchemaVersion > AccountModelCatalogSchemaVersion || candidate.SourceGeneration < 0 {
		return ErrAccountModelCatalogConflict
	}
	if candidate.SchemaVersion >= AccountModelCatalogSchemaVersion && candidate.EnrichmentVersion == "" {
		return ErrAccountModelCatalogConflict
	}
	var pin AccountCapabilityRelease
	if _, exists := extra[ModelCapabilityReleaseExtraKey]; exists {
		if !decodeCatalogExtra(extra, ModelCapabilityReleaseExtraKey, &pin) || pin.SchemaVersion != candidate.SchemaVersion || pin.EnrichmentVersion != candidate.EnrichmentVersion || pin.SourceGeneration != candidate.SourceGeneration || !equalCatalogSourceVersions(pin.SourceVersions, candidate.SourceVersions) {
			return ErrAccountModelCatalogConflict
		}
	}
	if previous.SchemaVersion >= AccountModelCatalogSchemaVersion {
		if candidate.SourceGeneration < previous.SourceGeneration || (candidate.SourceGeneration == previous.SourceGeneration && !equalCatalogSourceVersions(previous.SourceVersions, candidate.SourceVersions)) {
			return ErrAccountModelCatalogConflict
		}
		if candidate.EnrichmentVersion != previous.EnrichmentVersion && pin.EnrichmentVersion != candidate.EnrichmentVersion {
			return ErrAccountModelCatalogConflict
		}
	}
	return nil
}

// AccountModelCatalogCommitter owns the protected keys; generic extra updates cannot write them.
type AccountModelCatalogCommitter interface {
	CommitAccountModelCatalog(context.Context, int64, AccountModelCatalogBaseline, *AccountModelCatalogSnapshot, AccountModelCatalogState, *UpstreamModelMetadataSnapshot) error
}

type AccountModelAllowlistWriter interface {
	UpdateAccountModelAllowlist(context.Context, int64, AccountModelCatalogBaseline, AccountModelAllowlist) error
}

// ConfigureAccountModelAllowlist accepts concrete native targets. It never creates members.
func (service *AccountTestService) ConfigureAccountModelAllowlist(ctx context.Context, account *Account, enabled bool, models []string, reason string, actorID int64, expectedRulesRevision int64) error {
	baseline := account.ModelCatalogBaseline()
	if baseline.RulesRevision != expectedRulesRevision {
		return ErrAccountModelCatalogConflict
	}
	if actorID <= 0 || strings.TrimSpace(reason) == "" {
		return newUpstreamModelSyncConfigError("A confirmation reason and administrator identity are required", nil)
	}
	if enabled && len(models) == 0 {
		return newUpstreamModelSyncConfigError("An enabled allowlist requires at least one discovered model", nil)
	}
	discovered := make(map[string]bool)
	if snapshot := account.GetUpstreamModelCatalog(); snapshot != nil && snapshot.IdentityRevision == baseline.IdentityRevision {
		for _, model := range snapshot.DiscoveredModels {
			discovered[model] = true
		}
	}
	for _, model := range models {
		if strings.TrimSpace(model) == "" || strings.ContainsAny(model, "*?") || model != strings.TrimSpace(model) || !discovered[model] {
			return newUpstreamModelSyncConfigError("Use concrete upstream model IDs for account scope", nil)
		}
	}
	writer, ok := service.accountRepo.(AccountModelAllowlistWriter)
	if !ok {
		return newUpstreamModelSyncInternalError("Account scope persistence unavailable", nil)
	}
	return writer.UpdateAccountModelAllowlist(ctx, account.ID, baseline, AccountModelAllowlist{Enabled: enabled, Models: dedupeAndSortModelIDs(models), IdentityRevision: baseline.IdentityRevision, ConfirmedAt: time.Now().UTC().Format(time.RFC3339Nano), ConfirmedBy: actorID, Reason: strings.TrimSpace(reason)})
}

// StripAccountModelCatalogExtra also prevents copies/imports inheriting another account's evidence.
func StripAccountModelCatalogExtra(extra map[string]any) map[string]any {
	copyExtra := make(map[string]any, len(extra))
	for key, value := range extra {
		switch key {
		case UpstreamModelCatalogExtraKey, UpstreamModelCatalogStateExtraKey, UpstreamModelAllowlistExtraKey, UpstreamModelMetadataExtraKey, ModelAvailabilityExtraKey, ModelProbeBudgetExtraKey:
		default:
			copyExtra[key] = value
		}
	}
	return copyExtra
}

// Catalog revision inputs are shared by row-lock updates and atomic SQL patches.

func AccountCatalogRuleCredentialFields() []string {
	return []string{"model_mapping", "compact_model_mapping", "protocol_rules", "openai_capabilities", "aws_region", "aws_force_global"}
}

func AccountCatalogRuleExtraFields() []string {
	return []string{"mixed_scheduling", "openai_passthrough", "openai_oauth_passthrough", "anthropic_passthrough", "openai_responses_supported", "openai_responses_mode", ModelCapabilityDefaultsExtraKey, ModelCapabilityReleaseExtraKey}
}

// AdvanceAccountModelCatalogRevisions runs under the account row lock.
// Token rotation is ignored only when a stable subject is present and unchanged.
func AdvanceAccountModelCatalogRevisions(current, incoming *Account) {
	AdvanceAccountModelCatalogRevisionsForUpdate(current, incoming, false)
}

// AdvanceAccountModelCatalogRevisionsForUpdate is shared by configuration preview
// and the locked repository write. Manual token replacement invalidates identity
// even when a stale edit retains a stable subject; automatic renewal does not.
func AdvanceAccountModelCatalogRevisionsForUpdate(current, incoming *Account, configurationOnly bool) {
	state := current.ModelCatalogState()
	identityChanged := current.Platform != incoming.Platform || current.Type != incoming.Type || !reflect.DeepEqual(current.ParentAccountID, incoming.ParentAccountID)
	for _, key := range AccountCatalogIdentityCredentialFields() {
		identityChanged = identityChanged || !reflect.DeepEqual(current.Credentials[key], incoming.Credentials[key])
	}
	stableSubject := false
	for _, key := range []string{"chatgpt_account_id", "chatgpt_user_id", "sub", "account_id", "user_id"} {
		stableSubject = stableSubject || (current.GetCredential(key) != "" && current.GetCredential(key) == incoming.GetCredential(key))
	}
	if (configurationOnly || !stableSubject) && !reflect.DeepEqual(current.Credentials["access_token"], incoming.Credentials["access_token"]) {
		identityChanged = true
	}
	rulesChanged := identityChanged || !reflect.DeepEqual(current.ProxyID, incoming.ProxyID)
	for _, key := range AccountCatalogRuleCredentialFields() {
		rulesChanged = rulesChanged || !reflect.DeepEqual(current.Credentials[key], incoming.Credentials[key])
	}
	for _, key := range AccountCatalogRuleExtraFields() {
		rulesChanged = rulesChanged || !reflect.DeepEqual(current.Extra[key], incoming.Extra[key])
	}
	if identityChanged {
		state.IdentityRevision++
		state.Result = "identity_changed"
	}
	if rulesChanged {
		state.RulesRevision++
	}
	if incoming.Extra == nil {
		incoming.Extra = make(map[string]any)
	}
	incoming.Extra[UpstreamModelCatalogStateExtraKey] = state
}

func accountCatalogScope(account *Account) (string, string) {
	profile := account.Platform + ":" + account.Type
	if mode := account.GetCredential("account_mode"); mode != "" {
		profile += ":" + mode
	}
	if account.Platform == PlatformQianwen {
		return "provider", profile
	}
	if account.IsOpenAIOAuth() || (account.IsCursor() && account.Type != AccountTypeUpstream) || (account.Platform == PlatformAntigravity && account.Type == AccountTypeOAuth) {
		return "account", profile
	}
	parsed, _ := url.Parse(upstreamModelRegistryBaseURL(account))
	if parsed != nil {
		for _, host := range []string{"api.openai.com", "api.anthropic.com", "generativelanguage.googleapis.com", "api.deepseek.com", "api.x.ai", "api.moonshot.cn", "api.moonshot.ai", "api.kimi.com", "open.bigmodel.cn", "api.minimax.io", "api.minimaxi.com", "opencode.ai"} {
			if strings.EqualFold(parsed.Hostname(), host) {
				return "account", profile
			}
		}
	}
	return "unknown", profile
}

func projectAccountModelCatalog(account *Account, catalog *UpstreamModelCatalog) *AccountModelCatalogSnapshot {
	baseline := account.ModelCatalogBaseline()
	catalog.AccountModelCatalogBaseline = baseline
	catalog.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	catalog.DiscoveryScope, catalog.ProviderProfile = accountCatalogScope(account)
	catalog.Complete = true
	catalog.DiscoveryComplete = true
	catalog.DiscoveredModels = dedupeAndSortModelIDs(catalog.Models)
	catalog.CandidateModels = append([]string{}, catalog.DiscoveredModels...)
	catalog.Models = []string{}
	catalog.ExcludedModels = []ExcludedCatalogModel{}
	catalog.InvalidAliases = []string{}
	snapshot := &AccountModelCatalogSnapshot{Revision: baseline.SnapshotRevision + 1, IdentityRevision: baseline.IdentityRevision, ObservedAt: catalog.ObservedAt, SourceKind: catalog.CatalogSource, ProviderProfile: catalog.ProviderProfile, DiscoveryScope: catalog.DiscoveryScope, Complete: true, DiscoveredModels: catalog.DiscoveredModels, EligibleModels: []AccountCatalogModel{}}
	var allowlist AccountModelAllowlist
	decodeCatalogExtra(account.Extra, UpstreamModelAllowlistExtraKey, &allowlist)
	for _, modelID := range catalog.DiscoveredModels {
		metadata, hasMetadata := catalog.Metadata[modelID]
		metadata.Category = ModelDisplayCategory(metadata)
		if hasMetadata {
			catalog.Metadata[modelID] = metadata
		}
		var reason CatalogExclusionReason
		evidence, observed := "native_account_catalog", catalog.ObservedAt
		confirmed := false
		for _, allowed := range allowlist.Models {
			if allowed == modelID {
				confirmed = true
			}
		}
		// Discovery uses this account's credentials; scope describes the upstream
		// catalog, not an additional administrator-confirmation requirement.
		if allowlist.Enabled && !confirmed {
			reason = CatalogReasonAccountRestricted
		}
		endpoints := accountCatalogModelEndpoints(account, modelID, catalog.Metadata[modelID])
		if account.Platform == PlatformQianwen && IsRetiredQianwenModel(modelID) {
			endpoint, _ := url.Parse(upstreamModelRegistryBaseURL(account))
			if endpoint != nil && (strings.HasPrefix(endpoint.Hostname(), "dashscope") || endpoint.Hostname() == "batch.dashscope.aliyuncs.com") && strings.HasSuffix(endpoint.Hostname(), ".aliyuncs.com") {
				reason = CatalogReasonRetired
			}
		}
		if len(endpoints) == 0 && reason == "" {
			reason = CatalogReasonMetadataIncomplete
			// Known tasks without a catalog endpoint are an adapter limitation,
			// not missing text-model metadata. Keep unknown tasks distinguishable.
			switch accountCatalogEndpointTask(account, modelID, metadata) {
			case "audio_generation", "audio", "audio_transcription", "audio_speech",
				"speech_generation", "text_to_speech", "speech_to_text", "transcription",
				"automatic_speech_recognition", "asr", "tts", "video_generation", "rerank", "reranking":
				reason = CatalogReasonEndpointUnsupported
			}
		}
		if reason != "" {
			catalog.ExcludedModels = append(catalog.ExcludedModels, ExcludedCatalogModel{ID: modelID, Reason: reason})
			continue
		}
		catalog.Models = append(catalog.Models, modelID)
		snapshot.EligibleModels = append(snapshot.EligibleModels, AccountCatalogModel{ID: modelID, Category: metadata.Category, Endpoints: endpoints, EvidenceKind: evidence, EvidenceObservedAt: observed})
	}
	snapshot.ExcludedModels = catalog.ExcludedModels
	catalog.Diff = AccountModelCatalogDiff{Added: []string{}, Removed: []string{}, Retained: []string{}}
	previous := map[string]bool{}
	if old := account.GetUpstreamModelCatalog(); old != nil {
		for _, model := range old.EligibleModels {
			previous[model.ID] = true
		}
	}
	for _, modelID := range catalog.Models {
		if previous[modelID] {
			catalog.Diff.Retained = append(catalog.Diff.Retained, modelID)
		} else {
			catalog.Diff.Added = append(catalog.Diff.Added, modelID)
		}
		delete(previous, modelID)
	}
	for modelID := range previous {
		catalog.Diff.Removed = append(catalog.Diff.Removed, modelID)
	}
	catalog.Diff.Removed = dedupeAndSortModelIDs(catalog.Diff.Removed)
	catalog.InvalidAliases = InvalidAccountCatalogAliases(account, snapshot)
	catalog.EligibleModels = snapshot.EligibleModels
	populateAccountCatalogCapabilities(account, catalog, snapshot)
	catalog.CapabilityStatus = snapshot.CapabilityStatus
	return snapshot
}

// InvalidAccountCatalogAliases checks model-name bindings against the supplied
// evidence, for any supported endpoint. Callers must supply a fresh preview or
// the effective snapshot after identity/scope validation. Account scheduling
// health is deliberately separate from these static mapping diagnostics.
func InvalidAccountCatalogAliases(account *Account, snapshot *AccountModelCatalogSnapshot) []string {
	invalid := []string{}
	if account == nil {
		return invalid
	}
	diagnosticAccount := &Account{ID: account.ID, Platform: account.Platform, Type: account.Type,
		Credentials: account.Credentials, Extra: StripAccountModelCatalogExtra(account.Extra),
		Status: StatusActive, Schedulable: true}
	if snapshot != nil {
		evidence := *snapshot
		evidence.CredentialAccountID = 0
		evidence.CredentialIdentityRevision = 0
		diagnosticAccount.Extra[UpstreamModelCatalogExtraKey] = &evidence
		diagnosticAccount.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: evidence.IdentityRevision}
	}
	for alias := range configuredModelMapping(account) {
		if !ResolveAccountModelBinding(context.Background(), diagnosticAccount, alias, AccountModelBindingOptions{Endpoint: CompositeRouteEndpointAny}).Supported {
			invalid = append(invalid, alias)
		}
	}
	sort.Strings(invalid)
	return invalid
}

// Model declarations describe endpoints; account eligibility remains a live scheduler check.
func accountCatalogModelEndpoints(account *Account, modelID string, metadata UpstreamModelMetadata) []string {
	if metadata.SupportedMethods != nil && len(metadata.SupportedMethods) == 0 {
		// Explicitly empty endpoint evidence is a denial, including for media
		// families and accounts with broader platform-level declarations.
		return []string{}
	}
	task := accountCatalogEndpointTask(account, modelID, metadata)
	if IsChatOnlyPlatform(account.Platform) {
		return chatOnlyAccountCatalogModelEndpoints(account, task, metadata)
	}

	endpoints := []string{}
	for _, method := range metadata.SupportedMethods {
		switch method {
		case "realtime":
			endpoints = append(endpoints, string(OpenAIEndpointCapabilityLive))
		case "images":
			endpoints = append(endpoints, CompositeRouteEndpointImages)
		case "generateContent", "streamGenerateContent", "generate_content":
			endpoints = append(endpoints, "generate_content")
		case "messages", string(OpenAIEndpointCapabilityChatCompletions), string(OpenAIEndpointCapabilityResponses),
			string(OpenAIEndpointCapabilityEmbeddings), string(OpenAIEndpointCapabilityAlphaSearch),
			string(OpenAIEndpointCapabilityLive), string(OpenAIEndpointCapabilityGrokMediaGeneration):
			endpoints = append(endpoints, method)
		}
	}
	switch task {
	case "realtime":
		endpoints = append(endpoints, string(OpenAIEndpointCapabilityLive))
	case "image_generation":
		if !account.IsGrok() {
			endpoints = append(endpoints, CompositeRouteEndpointImages)
		}
	case "video_generation":
		// The OpenAI native video lifecycle executes on the media surface the
		// catalogue already publishes for image tasks, and service/openai_actions.go
		// resolves the video actions to that same family before the send boundary.
		// The CN platforms reach video through their own native ingress and keep
		// no catalogue family here, so they stay unprojected.
		if account.Platform == PlatformOpenAI {
			endpoints = append(endpoints, CompositeRouteEndpointImages)
		}
	case "alpha_search", "embeddings", "live", "grok_media_generation":
		endpoints = append(endpoints, task)
	}
	if account.IsGrok() && (xai.IsGrokImagineModel(modelID) || task == "image_generation" || task == "video_generation") {
		endpoints = append(endpoints, string(OpenAIEndpointCapabilityGrokMediaGeneration))
	}
	if len(endpoints) == 0 && (task == "" || task == "text_generation") && metadata.SupportedMethods == nil {
		switch {
		case account.Platform == PlatformQianwen || account.IsGrok() || (account.Platform == PlatformOpenAI && account.Type == AccountTypeAPIKey):
			// Mixed provider catalogs need text-task evidence before protocol fallback.
			if task == "text_generation" {
				endpoints = append(endpoints, accountCatalogNativeTextEndpoints(account, modelID)...)
			}
		case account.IsOpenAIOAuth() || account.IsCursor():
			endpoints = append(endpoints, "responses")
		case account.IsAnthropic():
			endpoints = append(endpoints, "messages")
		case account.Platform == PlatformAntigravity:
			endpoints = append(endpoints, "generate_content")
		case account.IsGemini():
			// Gemini requires an explicit supported generation method.
		case account.Platform == PlatformDeepseek || account.Platform == PlatformKimi || account.IsOpenCodeGo() || account.IsCommandCode() || task == "text_generation":
			endpoints = append(endpoints, accountCatalogNativeTextEndpoints(account, modelID)...)
		}
	}
	// An explicit account search declaration also covers its discovered models.
	// The permissive scheduler default is not evidence of upstream support.
	if configured, _ := account.openAIEndpointCapabilitySet(); configured[string(OpenAIEndpointCapabilityAlphaSearch)] && account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityAlphaSearch) {
		endpoints = append(endpoints, string(OpenAIEndpointCapabilityAlphaSearch))
	}
	return dedupeAndSortModelIDs(endpoints)
}

// chatOnlyAccountCatalogModelEndpoints keeps a chat-only platform entry
// conservative without turning the classification into a permanent capability
// denial: the declared endpoint families are admitted exactly when the platform
// action matrix binds an adapter for the action they execute on. A text model
// still requires chat-completions evidence, and an explicit declaration without
// it remains a denial.
func chatOnlyAccountCatalogModelEndpoints(account *Account, task string, metadata UpstreamModelMetadata) []string {
	endpoints := []string{}
	// Task-only preset facts are endpoint evidence too. Video executes on the
	// images catalog surface, but must prove its own video adapter binding.
	taskEndpoint, taskAction := "", ""
	switch task {
	case "image_generation":
		taskEndpoint, taskAction = CompositeRouteEndpointImages, "image.generate"
	case "video_generation":
		taskEndpoint, taskAction = CompositeRouteEndpointImages, "video.generate"
	case "rerank", "reranking":
		taskEndpoint, taskAction = "rerank", "rerank"
	case "voice_clone":
		taskEndpoint, taskAction = "voice", "voice"
	case "audio.tts", "audio_tts", "audio_speech", "speech", "speech_generation", "text_to_speech", "tts":
		taskEndpoint, taskAction = "speech", "audio.tts"
	case "audio.stt", "audio_stt", "audio_transcription", "transcription", "transcriptions", "speech_to_text", "automatic_speech_recognition", "asr":
		taskEndpoint, taskAction = "transcriptions", "audio.stt"
	}
	if taskAction != "" && platformActionAdapterBound(account, taskAction) {
		endpoints = append(endpoints, taskEndpoint)
	}
	if task == "" || task == "text_generation" {
		if metadata.SupportedMethods == nil || slices.Contains(metadata.SupportedMethods, CompositeRouteEndpointChatCompletions) {
			endpoints = append(endpoints, CompositeRouteEndpointChatCompletions)
		}
		// A chat-only entry that publishes a native text protocol on the same
		// trunk serves its text models through that family too. The family comes
		// from the platform descriptor's protocol facts and is admitted only when
		// the action matrix binds the cell, so the dispatch gate and the catalog
		// read one authority instead of two hand-maintained lists.
		for _, family := range chatOnlyPublishedTextFamilies(account) {
			if action := chatOnlyCatalogEndpointAction(family); action != "" && platformActionAdapterBound(account, action) {
				endpoints = append(endpoints, family)
			}
		}
	}
	for _, method := range metadata.SupportedMethods {
		action := chatOnlyCatalogEndpointAction(method)
		if action == "" || !platformActionAdapterBound(account, action) {
			continue
		}
		endpoints = append(endpoints, method)
	}
	return dedupeAndSortModelIDs(endpoints)
}

// chatOnlyPublishedTextFamilies lists the protocol families a chat-only platform
// publishes on its OpenAI-shaped trunk beyond Chat Completions. The platform
// descriptor is the authority: ResponsesPOST and MessagesPOST are the dated
// protocol facts, and a family whose action cell is not bound never contributes.
func chatOnlyPublishedTextFamilies(account *Account) []string {
	if account == nil {
		return nil
	}
	descriptor, known := domain.LookupPlatform(account.Platform)
	if !known {
		return nil
	}
	families := make([]string, 0, 2)
	if descriptor.ResponsesPOST != domain.EndpointRejected {
		families = append(families, CompositeRouteEndpointResponses)
	}
	if descriptor.MessagesPOST != domain.EndpointRejected {
		families = append(families, CompositeRouteEndpointMessages)
	}
	return families
}

// chatOnlyCatalogEndpointAction resolves the gateway action an endpoint family
// executes on. An unknown family returns "" and stays out of the catalog.
func chatOnlyCatalogEndpointAction(endpoint string) string {
	switch endpoint {
	case CompositeRouteEndpointChatCompletions:
		return "chat"
	case string(OpenAIEndpointCapabilityEmbeddings):
		return "embeddings"
	case CompositeRouteEndpointImages, string(OpenAIEndpointCapabilityGrokMediaGeneration):
		return "image.generate"
	case "generate_content":
		return "generate_content"
	case CompositeRouteEndpointMessages:
		return "messages"
	case CompositeRouteEndpointResponses:
		return "responses"
	case string(OpenAIEndpointCapabilityLive):
		return "live"
	case string(OpenAIEndpointCapabilityAlphaSearch):
		return "alpha_search"
	case "rerank":
		return "rerank"
	case "voice":
		return "voice"
	case "speech":
		return "audio.tts"
	case "transcriptions":
		return "audio.stt"
	default:
		return ""
	}
}

// platformActionAdapterBound reports whether the authoritative platform×action
// matrix binds an adapter for this account's identity.
func platformActionAdapterBound(account *Account, action string) bool {
	if account == nil {
		return false
	}
	return domain.PlatformAction(domain.ActionIdentity{Platform: account.Platform, HostedAccountType: account.Type}, action).Status == domain.ActionImplemented
}

// Task evidence precedes platform defaults. Output text alone does not turn an
// explicitly declared ASR, reranking or other non-chat task into a conversation.
func accountCatalogEndpointTask(account *Account, modelID string, metadata UpstreamModelMetadata) string {
	task := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(metadata.TaskType)), "-", "_")
	// The upstream parser can derive text_generation from a sole text output,
	// which is also what ASR produces. Known speech families still need their
	// dedicated endpoint; text output is not evidence of chat compatibility.
	if account.Platform == PlatformQianwen && (task == "" || task == "text_generation") && isQianwenNativeAudioFamily(modelID) {
		return "audio_generation"
	}
	switch task {
	case "chat", "chat_completion", "chat_completions":
		return "text_generation"
	case "embedding", "text_embedding", "feature_extraction":
		return "embeddings"
	case "image", "text_to_image", "image_editing":
		return "image_generation"
	case "video", "text_to_video", "image_to_video":
		return "video_generation"
	}
	if task != "" {
		return task
	}
	for _, modality := range metadata.OutputModalities {
		switch modality {
		case "image", "audio", "video":
			return modality + "_generation"
		}
	}
	if len(metadata.OutputModalities) == 1 && metadata.OutputModalities[0] == "text" {
		return "text_generation"
	}
	// Only fill absent declarations for bounded, known model families. Do not
	// infer chat from an arbitrary qwen/grok prefix or a presentation category.
	if len(metadata.OutputModalities) != 0 || len(metadata.SupportedMethods) != 0 {
		return ""
	}
	if account.IsGrok() && xai.IsGrokTextResponsesModelID(modelID) {
		return "text_generation"
	}
	if account.Platform == PlatformQianwen {
		if IsQianwenNativeImageModel(modelID) {
			return "image_generation"
		}
		if strings.HasPrefix(modelID, "qwen-vl-") || strings.HasPrefix(modelID, "qwen3-vl-") {
			return "text_generation"
		}
		switch modelID {
		case "text-embedding-v1", "text-embedding-v2", "text-embedding-v3", "text-embedding-v4",
			"qwen3-embedding-0.6b", "qwen3-embedding-4b", "qwen3-embedding-8b":
			return "embeddings"
		case "qwen-flash", "qwen-plus", "qwen-max", "qwen-turbo", "qwen-long",
			"qwen-1.8b-chat", "qwen-7b-chat", "qwen-14b-chat", "qwen-72b-chat",
			"qwen-coder-plus", "qwen-coder-turbo":
			return "text_generation"
		}
	}
	return ""
}

func (service *AccountTestService) ApplyAccountModelCatalog(ctx context.Context, account *Account, expectedRulesRevision *int64) (*UpstreamModelCatalog, error) {
	if account == nil || account.ID <= 0 {
		return nil, newUpstreamModelSyncConfigError("A saved account is required", nil)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if _, busy := service.catalogInFlight.LoadOrStore(account.ID, struct{}{}); busy {
		return nil, ErrAccountModelCatalogConflict
	}
	defer service.catalogInFlight.Delete(account.ID)
	releaseBudget, err := service.acquireCatalogApplyBudget(ctx)
	if err != nil {
		return nil, err
	}
	defer releaseBudget()
	if locker, ok := service.accountRepo.(AccountModelCatalogLocker); ok {
		release, err := locker.TryLockAccountModelCatalog(ctx, account.ID)
		if err != nil {
			return nil, err
		}
		defer release()
		// Reload after acquiring the cross-instance lock; callers may hold old credentials.
		fresh, err := service.accountRepo.GetByID(ctx, account.ID)
		if err != nil {
			return nil, err
		}
		account = fresh
		if scheduled, _ := ctx.Value(catalogScheduledRefreshKey{}).(bool); scheduled && !accountCatalogRefreshDue(account, time.Now()) {
			return nil, nil
		}
	}
	baseline := account.ModelCatalogBaseline()
	if expectedRulesRevision != nil && *expectedRulesRevision != baseline.RulesRevision {
		return nil, ErrAccountModelCatalogConflict
	}
	if account.IsShadow() {
		parent, err := resolveCredentialAccount(ctx, service.accountRepo, account)
		if err != nil {
			return nil, newUpstreamModelSyncConfigError("Credential identity unavailable", err)
		}
		baseline.CredentialAccountID = parent.ID
		baseline.CredentialIdentityRevision = parent.ModelCatalogState().IdentityRevision
	}
	review, reviewed := ctx.Value(catalogReviewKey{}).(*AccountCatalogReview)
	if reviewed {
		if account.Status != StatusActive || review.AccountID != account.ID ||
			review.IdentityRevision != baseline.IdentityRevision || review.RulesRevision != baseline.RulesRevision ||
			review.SnapshotRevision != baseline.SnapshotRevision || review.CredentialID != baseline.CredentialAccountID ||
			review.CredentialRevision != baseline.CredentialIdentityRevision {
			return nil, ErrAccountModelCatalogConflict
		}
	}
	if scheduled, _ := ctx.Value(catalogScheduledRefreshKey{}).(bool); scheduled {
		baseline.RequireActive = true
	}
	committer, ok := service.accountRepo.(AccountModelCatalogCommitter)
	if !ok {
		return nil, newUpstreamModelSyncInternalError("Account model catalog persistence unavailable", nil)
	}
	catalog, err := service.SyncUpstreamModelCatalog(ctx, account)
	state := account.ModelCatalogState()
	state.AttemptedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err != nil {
		if reviewed {
			return nil, err
		}
		state.Result, state.Error = "failed", "upstream_model_discovery_failed"
		var syncError *UpstreamModelSyncError
		if errors.As(err, &syncError) {
			switch syncError.Kind {
			case UpstreamModelSyncErrorCredentials:
				state.Result, state.Error = "credentials_invalid", "upstream_model_credentials_invalid"
			case UpstreamModelSyncErrorUnsupported:
				state.Result, state.Error = "unsupported", "upstream_model_discovery_unsupported"
			case UpstreamModelSyncErrorConfiguration:
				state.Error = "upstream_model_configuration_invalid"
			case UpstreamModelSyncErrorInternal:
				state.Error = "upstream_model_internal_error"
			}
		}
		if commitErr := committer.CommitAccountModelCatalog(ctx, account.ID, baseline, nil, state, nil); commitErr != nil {
			return nil, newUpstreamModelSyncInternalError("Failed to save model discovery state", commitErr)
		}
		if account.Extra == nil {
			account.Extra = make(map[string]any)
		}
		account.Extra[UpstreamModelCatalogStateExtraKey] = state
		if staleness, _ := account.ModelCatalogFreshness(time.Now()); staleness == "stale" {
			logger.LegacyPrintf("service.model_catalog_refresh", "warning: account=%d model catalog data may be stale (last successful discovery over 24h ago)", account.ID)
		}
		return nil, err
	}
	snapshot := catalog.snapshot
	if reviewed {
		current, reviewErr := newAccountCatalogReview(account.ID, baseline, catalog)
		if reviewErr != nil {
			return nil, reviewErr
		}
		if current.Digest != review.Digest || !reflect.DeepEqual(current.Release, review.Release) {
			return nil, ErrAccountModelCatalogConflict
		}
		baseline.RequireActive = true
	}
	snapshot.CredentialAccountID = baseline.CredentialAccountID
	snapshot.CredentialIdentityRevision = baseline.CredentialIdentityRevision
	state.Result, state.Error = "succeeded", ""
	state.LastSuccessAt = catalog.ObservedAt
	metadata := &UpstreamModelMetadataSnapshot{Source: "enriched", SyncedAt: catalog.ObservedAt, Models: catalog.Metadata}
	if err := ValidateAccountModelCatalogVersion(account.Extra, snapshot); err != nil {
		return nil, err
	}
	if err := committer.CommitAccountModelCatalog(ctx, account.ID, baseline, snapshot, state, metadata); err != nil {
		return nil, newUpstreamModelSyncInternalError("Failed to save account model catalog", err)
	}
	// Production repositories reread after CAS while retaining the discovery lock.
	if reader, ok := service.accountRepo.(ModelCatalogAccountRepository); ok {
		saved, readErr := reader.GetModelCatalogAccount(ctx, account.ID)
		if readErr != nil {
			return nil, newUpstreamModelSyncInternalError("Catalog committed but readback failed; reload the account catalog", readErr)
		}
		if saved == nil || saved.ModelCatalogBaseline().IdentityRevision != baseline.IdentityRevision || saved.ModelCatalogBaseline().RulesRevision != baseline.RulesRevision || saved.ModelCatalogBaseline().SnapshotRevision != snapshot.Revision {
			return nil, ErrAccountModelCatalogConflict
		}
	}
	catalog.Applied, catalog.SnapshotRevision = true, snapshot.Revision
	if account.Extra == nil {
		account.Extra = make(map[string]any)
	}
	account.Extra[UpstreamModelCatalogExtraKey] = snapshot
	account.Extra[UpstreamModelCatalogStateExtraKey] = state
	account.Extra[UpstreamModelMetadataExtraKey] = AccountModelMetadataEnvelope(snapshot, metadata)
	InvalidateAvailableModelsCacheForAccount(account)
	return catalog, nil
}

// accountCatalogNativeTextEndpoints uses the same saved protocol decisions as forwarding.
func accountCatalogNativeTextEndpoints(account *Account, model string) []string {
	if account.UsesBedrockCatalog() {
		return []string{CompositeRouteEndpointMessages}
	}
	if account.Platform == PlatformOpenAI && account.Type == AccountTypeAPIKey {
		// An unobserved permissive forwarding default is not Responses evidence.
		if openai_compat.ResolveResponsesSupport(account.Extra) == openai_compat.ResponsesSupportYes {
			return []string{CompositeRouteEndpointResponses}
		}
		return []string{CompositeRouteEndpointChatCompletions}
	}
	if account.IsOpenCodeGo() || account.IsCommandCode() {
		return []string{catalogProtocolEndpoint(modelRoutedNativeProtocol(account, model))}
	}
	if account.IsAnthropicProtocol() {
		return []string{CompositeRouteEndpointMessages}
	}
	if account.UsesNativeCNResponses() {
		if account.IsAdaptiveAPIProtocol() {
			return []string{CompositeRouteEndpointChatCompletions, CompositeRouteEndpointResponses}
		}
		return []string{CompositeRouteEndpointResponses}
	}
	return []string{CompositeRouteEndpointChatCompletions}
}

func ModelDisplayCategory(metadata UpstreamModelMetadata) string {
	switch metadata.Category {
	case "chat", "image", "video", "embedding", "audio", "memory", "other":
		return metadata.Category
	}
	switch strings.ReplaceAll(strings.ToLower(strings.TrimSpace(metadata.TaskType)), "-", "_") {
	case "chat", "text_generation", "chat_completion", "chat_completions":
		return "chat"
	case "image", "image_generation", "text_to_image", "image_editing":
		return "image"
	case "video", "video_generation", "text_to_video", "image_to_video":
		return "video"
	case "embedding", "embeddings", "text_embedding", "feature_extraction":
		return "embedding"
	case "audio", "audio_generation", "speech_generation", "text_to_speech", "speech_to_text", "transcription", "automatic_speech_recognition":
		return "audio"
	case "memory":
		return "memory"
	}
	for _, category := range []string{"video", "image", "audio"} {
		for _, modality := range metadata.OutputModalities {
			if modality == category {
				return category
			}
		}
	}
	return "other"
}

type catalogDiscoveryKey struct{}

func applyCapabilityEndpointFacts(account *Account, m *UpstreamModelMetadata) {
	if account == nil || m.TaskType == "" || m.SupportedMethods != nil {
		return
	}
	if m.TaskType == "text_generation" && accountCatalogEndpointTask(account, m.ID, *m) == "text_generation" && !account.IsGemini() && account.Platform != PlatformAntigravity {
		m.SupportedMethods = accountCatalogNativeTextEndpoints(account, m.ID)
		if account.IsOpenAIOAuth() || account.IsCursor() {
			m.SupportedMethods = []string{CompositeRouteEndpointResponses}
		}
		if m.FieldEvidence == nil {
			m.FieldEvidence = make(map[string]ModelCapabilityEvidence)
		}
		m.FieldEvidence["supported_methods"] = ModelCapabilityEvidence{Source: "gateway", SourceVersion: "endpoint-contract-v1", ProviderProfile: m.ProviderProfile, MatchedModelID: m.ID, MatchRule: "account_protocol", ObservedAt: time.Now().UTC().Format(time.RFC3339), Confidence: "verified"}
	}
}

func extractCatalogModelIDsWithSelector(body []byte, selectID func(upstreamModelEntry) string) ([]string, error) {
	entries, err := extractUpstreamModelRawEntries(body)
	if err != nil {
		return nil, err
	}
	models := make([]string, 0, len(entries))
	for _, raw := range entries {
		var entry upstreamModelEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return nil, err
		}
		id := strings.TrimSpace(selectID(entry))
		if id == "" {
			return nil, fmt.Errorf("model ID missing")
		}
		models = append(models, id)
	}
	return dedupeAndSortModelIDs(models), nil
}

// ModelCatalogAccountRepository projects evidence and resolution rules without authentication secrets.
type ModelCatalogAccountRepository interface {
	// ListModelCatalogAccounts matches an explicit set of concrete platforms.
	// Nil and empty sets both match no accounts; callers requesting all platforms
	// must pass domain.ConcretePlatforms(). Group isolation still applies, and
	// includeGrouped only affects queries without a group ID.
	ListModelCatalogAccounts(context.Context, *int64, []string, bool) ([]Account, error)
	GetModelCatalogAccount(context.Context, int64) (*Account, error)
}

// Catalog previews use the saved token; discovery must not rotate durable credentials.
func (s *AccountTestService) fetchAntigravityCatalog(ctx context.Context, account *Account) ([]string, []byte, error) {
	accessToken := account.GetCredential("access_token")
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return nil, nil, newUpstreamModelSyncConfigError("No Antigravity access token is available", nil)
	}

	client, err := antigravity.NewClient(upstreamModelsProxyURL(account))
	if err != nil {
		return nil, nil, newUpstreamModelSyncConfigError("Failed to configure Antigravity client", err)
	}
	modelsResp, raw, err := client.FetchAvailableModels(
		ctx,
		accessToken,
		strings.TrimSpace(account.GetCredential("project_id")),
		resolveModelsListReadLimit(s.cfg),
	)
	if err != nil {
		return nil, nil, newUpstreamModelSyncUpstreamError("Failed to fetch Antigravity available models", err)
	}
	if modelsResp == nil || modelsResp.Models == nil {
		return nil, nil, newUpstreamModelSyncUpstreamError("Upstream returned no supported models", nil)
	}
	models, body, err := normalizeAntigravityUpstreamModels(modelsResp.Models, raw)
	if err != nil {
		return nil, nil, newUpstreamModelSyncUpstreamError("Failed to normalize Antigravity available models", err)
	}
	return models, body, nil
}

func (s *AccountTestService) fetchCursorOAuthCatalog(ctx context.Context, account *Account) ([]string, []byte, error) {
	gateway := s.cursorGatewayService
	if gateway == nil || gateway.native == nil {
		return nil, nil, newUpstreamModelSyncConfigError("Cursor service unavailable", nil)
	}
	ctx = cursorAccountContext(ctx, account)
	models, err := gateway.native.AvailableModels(ctx, gateway.credentials(ctx, account, account.GetCredential("access_token")))
	if err != nil {
		if cursorAuthFailure(err) {
			return nil, nil, &UpstreamModelSyncError{Kind: UpstreamModelSyncErrorCredentials, Message: "Upstream rejected the account credentials", Err: err}
		}
		return nil, nil, newUpstreamModelSyncUpstreamError("Failed to request Cursor model list", err)
	}
	ids := make([]string, 0, len(models))
	for _, model := range models {
		if strings.TrimSpace(model.ID) == "" {
			return nil, nil, newUpstreamModelSyncUpstreamError("Cursor model ID missing", nil)
		}
		ids = append(ids, model.ID)
	}
	return dedupeAndSortModelIDs(ids), nil, nil
}
