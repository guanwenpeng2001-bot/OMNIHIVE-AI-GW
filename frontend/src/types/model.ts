/** Model domain contracts. */
import type { AccountPlatform } from './admin'

export type GroupPlatform = import('@/constants/platforms').GroupPlatform

export type VideoModelPrices = Record<string, Record<string, number>>

export interface OpenAIMessagesDispatchModelConfig {
  opus_mapped_model?: string
  sonnet_mapped_model?: string
  haiku_mapped_model?: string
  exact_model_mappings?: Record<string, string>
}

export type ReasoningEffortMatchType = 'exact' | 'prefix' | 'suffix'

export interface ReasoningEffortMapping {
  from: string
  to: string
  match_type?: ReasoningEffortMatchType
  model?: string
}

export interface ModelAllowlist {
  enabled: boolean
  models: string[]
}

// 固定账号获取 Codex Model Manifest 配置（仅 openai 分组）

export interface CodexModelsManifestConfig {
  enabled: boolean
  account_ids: number[]
  fallback_to_scheduler: boolean
}

export type CompositeRouteMatchType = 'exact' | 'prefix'

export type CompositeRouteEndpoint =
  | 'any'
  | 'messages'
  | 'count_tokens'
  | 'responses'
  | 'chat_completions'
  | 'embeddings'
  | 'images'
  | 'gemini'

export type CompositeRouteSource = 'route' | 'detector' | string

export interface CompositeModelRoute {
  account_type?: '' | 'bedrock' | 'service_account'
  id: number
  group_id: number
  public_model: string
  match_type: CompositeRouteMatchType
  target_platform: Exclude<GroupPlatform, 'composite'>
  upstream_model: string
  endpoint: CompositeRouteEndpoint
  priority: number
  enabled: boolean
  notes: string
  created_at?: string
  updated_at?: string
}

export interface CompositeModelRouteInput {
  account_type?: '' | 'bedrock' | 'service_account'
  public_model: string
  match_type: CompositeRouteMatchType
  target_platform: Exclude<GroupPlatform, 'composite'>
  upstream_model?: string
  endpoint: CompositeRouteEndpoint
  priority?: number
  enabled?: boolean
  notes?: string
}

export interface CompositeRoutePreviewRequest {
  model: string
  endpoint: CompositeRouteEndpoint
}

export interface CompositeRouteDecision {
  account_type?: '' | 'bedrock' | 'service_account'
  matched: boolean
  source: CompositeRouteSource
  group_id: number
  public_model: string
  target_platform: Exclude<GroupPlatform, 'composite'> | ''
  upstream_model: string
  endpoint: CompositeRouteEndpoint
  route?: CompositeModelRoute
  reason?: string
}

export type ModelCatalogReason =
  | 'not_synced' | 'identity_changed' | 'entitlement_unconfirmed' | 'not_discovered'
  | 'account_restricted' | 'route_ambiguous' | 'route_target_unavailable'
  | 'endpoint_unsupported' | 'retired' | 'group_restricted' | 'stale_evidence'
  | 'metadata_incomplete'

export interface ModelCatalogDiff {
  added: string[] | null
  removed: string[] | null
  retained: string[] | null
}

export interface ModelCatalogExclusion {
  model: string
  reason: ModelCatalogReason
}

export interface AccountCatalogModel {
  id: string
  category?: string
  endpoints: string[] | null
  evidence_kind: string
  evidence_observed_at: string
}

export interface AccountCatalogCapabilitySummary {
  total: number
  discovered: number
  mapping_targets: number
  eligible: number
  complete: number
  estimated: number
  incomplete: number
}

export interface AccountCapabilityRelease {
  schema_version: number
  enrichment_version: string
  source_generation: number
  source_versions: Record<string, string> | null
}

/** HTTP revisions are lossless decimal strings; never coerce them to Number. */

export interface AccountModelCatalogBaseline {
  identity_revision: string
  rules_revision: string
  snapshot_revision: string
}

export interface AccountModelCatalogState {
  identity_revision: string
  rules_revision: string
  attempted_at?: string
  last_success_at?: string
  result?: string
  error?: string
}

export interface AccountModelCatalogSnapshot extends Partial<AccountCapabilityRelease> {
  revision: string
  identity_revision: string
  rules_revision: string
  credential_identity_revision?: string
  credential_account_id?: number
  capability_status?: string
  capability_summary?: AccountCatalogCapabilitySummary
  observed_at: string
  source_kind: string
  provider_profile: string
  discovery_scope: string
  complete: boolean
  discovered_models: string[] | null
  eligible_models: AccountCatalogModel[] | null
  excluded_models: ModelCatalogExclusion[] | null
}

export interface AccountModelCatalogSummary {
  staleness: 'fresh' | 'stale' | 'unknown'
  last_success_at: string | null
  last_attempt_at: string | null
  synced: boolean
  complete: boolean
  observed_at: string | null
  discovered_count: number
  eligible_count: number
  excluded_count: number
  last_error: string | null
  snapshot_revision: string | null
  warnings: string[]
}

/** Historical evidence remains visible when the effective snapshot is invalid. */

export interface AccountModelCatalogDetails extends AccountModelCatalogSummary {
  display_metadata?: Record<string, Partial<import('@/api/admin/accounts').UpstreamModelMetadata>>
  snapshot: AccountModelCatalogSnapshot | null
  effective_snapshot: AccountModelCatalogSnapshot | null
  state: AccountModelCatalogState
  baseline: AccountModelCatalogBaseline
  invalid_aliases: string[] | null
  alias_endpoint: string
  eligible_models: AccountCatalogModel[] | null
}

/** Return the complete review object unchanged when applying a preview. */

export interface AccountCatalogReview extends AccountModelCatalogBaseline {
  account_id: number
  credential_account_id: number
  credential_identity_revision: string
  release: AccountCapabilityRelease
  digest: string
}

export interface AccountModelCatalog extends AccountModelCatalogBaseline, AccountCapabilityRelease {
  /** Selectable preview IDs; capability gaps do not establish or deny routing eligibility. */
  candidate_models: string[]
  /** Native discovery completeness, independent of capability_status. */
  discovery_complete: boolean
  capability_status: 'complete' | 'estimated' | 'incomplete'
  review?: AccountCatalogReview
  models: string[] | null
  metadata?: Record<string, Partial<import('@/api/admin/accounts').UpstreamModelMetadata>>
  metadata_sources?: string[]
  warnings?: { code: string; message: string }[]
  applied: boolean
  catalog_source: string
  discovery_scope: string
  provider_profile: string
  complete: boolean
  observed_at: string
  discovered_models: string[] | null
  eligible_models: AccountCatalogModel[] | null
  diff: ModelCatalogDiff
  excluded_models: ModelCatalogExclusion[] | null
  invalid_aliases: string[] | null
}

/** expected_rules_revision is also accepted as a number for legacy clients. */

export interface UpdateModelAllowlistRequest {
  enabled: boolean
  models: string[]
  reason: string
  expected_rules_revision: string | number
}

export interface UpdateModelAllowlistResponse {
  rules_revision: string
}

export interface ApplyAccountModelCatalogRequest {
  review: AccountCatalogReview
}

export interface AccountModelCatalogSyncParams {
  apply?: boolean
  expected_rules_revision?: string | number
}

export interface PlatformPresetModel {
  id: string
  display_name: string
  task_type: string
  input_modalities: string[] | null
  output_modalities: string[] | null
  methods: string[] | null
  lifecycle: string
  context_window?: number
  max_output_tokens?: number
  field_sources: Record<string, string> | null
}

export interface PlatformModelPreset {
  platform: AccountPlatform
  provider_profile: string
  revision: string
  coverage_status: 'complete' | 'partial' | 'unconfigured'
  checked_at: string
  source_refs: string[] | null
  gaps?: string[]
  models: PlatformPresetModel[] | null
}

// Account models span multiple providers; protocol-specific fields may be absent.

export interface AvailableAccountModel {
  category?: string
  task_type?: string
  output_modalities?: string[]
  catalog_source?: 'live' | 'configured' | 'preset' | 'applied'
  metadata_source?: string
  probe_supported?: boolean
  id: string
  type?: string
  display_name: string
  created_at?: string
  object?: string
  created?: number
  owned_by?: string
  evidence_kind?: string
  evidence_observed_at?: string
}

/** Legacy import compatibility; available models span all account platforms. */
export type ClaudeModel = AvailableAccountModel
