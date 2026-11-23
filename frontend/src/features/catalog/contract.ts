import type { components, paths } from '@/api/generated/openapi'

export type GeneratedModelSummary = components['schemas']['ModelSummary']
export type GeneratedModelDetail = components['schemas']['ModelDetail']
export type GeneratedCatalogModelPage = components['schemas']['CatalogModelPage']
export type GeneratedCatalogCompareInput = components['schemas']['CatalogCompareInput']
export type GeneratedCatalogCompareResult = components['schemas']['CatalogCompareResult']
export type GeneratedCatalogCompareRow = components['schemas']['CatalogCompareRow']
export type GeneratedCatalogEstimate = components['schemas']['CatalogEstimate']
export type GeneratedCatalogReferencePrice = components['schemas']['CatalogReferencePrice']

export type CatalogRate = components['schemas']['CatalogRate']
export type CatalogRateBand = components['schemas']['CatalogRateBand']
export type CatalogTimeBand = components['schemas']['CatalogTimeBand']
export type CatalogMoney = components['schemas']['CatalogMoney']
export type CatalogMetrics = components['schemas']['CatalogMetrics']
export type CatalogModelFacts = components['schemas']['CatalogModelFacts']
export type CatalogCapability = components['schemas']['CatalogCapability']
export type CatalogEndpoint = components['schemas']['CatalogEndpoint']
export type CatalogEvidence = components['schemas']['CatalogEvidence']
export type CatalogBenchmark = components['schemas']['CatalogBenchmark']
export type CatalogModelCompatibility = components['schemas']['CatalogModelCompatibility']
export type CatalogScope = components['schemas']['CatalogScope']
export type CatalogPriceScenario = components['schemas']['CatalogPriceScenario']
export type CatalogEstimateComponent = components['schemas']['CatalogEstimateComponent']

export type ExactQuoteInput = components['schemas']['ExactQuoteInput']
export type ExactQuote = components['schemas']['ExactQuote']
export type ExactQuoteLine = components['schemas']['ExactQuoteLine']
export type ExactPriceScenario = components['schemas']['ExactPriceScenario']

export type StatusWindow = NonNullable<NonNullable<
  paths['/api/v2/public/status']['get']['parameters']['query']
>['window']>
export type StatusSummary = components['schemas']['StatusSummary']
export type StatusComponent = components['schemas']['StatusComponent']
export type StatusDaily = components['schemas']['StatusDaily']
export type StatusIncidentSummary = components['schemas']['StatusIncidentSummary']
export type StatusIncident = components['schemas']['StatusIncident']
export type StatusIncidentPage =
  paths['/api/v2/public/status/incidents']['get']['responses']['200']['content']['application/json']['data']

/* @v034-temp-openapi-bridge: Temporary BE-3 v0.3.4 3-axis wire extensions.
 * Replace with direct components['schemas'] aliases once the integrator
 * regenerates frontend/src/api/generated/openapi.d.ts from BE-3 catalog.yaml.
 */
export type CatalogSourceKind =
  | 'builtin_reviewed'
  | 'approved_registry'
  | 'reviewed_registry'
  | 'builtin_supplement'
  | 'platform_builtin'
  | 'pi_ai_reference'
  | 'pi_supplement'
  | 'pi-ai'
  | 'external_reference'
  | 'account_discovery'
  | 'account_declaration'
  | 'upstream_discovered'
  | 'platform_preset'
  | 'channel_published'
  | 'operator_config'
  | 'heuristic'
  | (string & {})

/* @v034-temp-openapi-bridge */
export interface CatalogSourceAxis {
  kinds: CatalogSourceKind[]
  candidate: boolean | null
  discovered: boolean | null
}

/* @v034-temp-openapi-bridge */
export type CatalogAxisAvailabilityState = 'live' | 'unavailable' | 'requires_scope' | 'unknown'

/* @v034-temp-openapi-bridge */
export type CatalogPublicationState = 'published' | 'unpublished' | 'unknown'

/* @v034-temp-openapi-bridge */
export type CatalogAccessAxisState = 'allowed' | 'denied' | 'requires_scope' | 'unknown'

/* @v034-temp-openapi-bridge */
export type CatalogAvailabilityReason =
  | 'candidate_only'
  | 'published_without_eligible_binding'
  | 'discovered_without_eligible_binding'
  | 'scope_required'
  | 'access_denied'
  | 'no_eligible_binding'
  | 'eligibility_incomplete'
  | 'platform_catalog'
  | 'discovered_unactivated'
  | (string & {})

/* @v034-temp-openapi-bridge */
export interface CatalogAvailabilityAxis {
  state: CatalogAxisAvailabilityState | (string & {})
  publication: CatalogPublicationState | (string & {})
  access: CatalogAccessAxisState | (string & {})
  reason?: CatalogAvailabilityReason | null
}

/* @v034-temp-openapi-bridge */
export type CatalogBillingState =
  | 'priced'
  | 'free'
  | 'conditional'
  | 'partial'
  | 'conflict'
  | 'varies'
  | 'unpriced'
  | 'error'
  | 'unknown'

/* @v034-temp-openapi-bridge */
export type CatalogBillingReason =
  | 'no_sale_tariff'
  | 'missing_price'
  | 'billing_provider_undetermined'
  | 'required_dimension_missing'
  | 'offer_price_missing'
  | 'partial_price'
  | 'offers_disagree'
  | 'conflicting_prices'
  | 'scenario_required'
  | 'price_read_failed'
  | (string & {})

/* @v034-temp-openapi-bridge */
export interface CatalogReferencePrice {
  state?: string
  source?: string
  source_label?: string | null
  observed_at?: string | null
  input_rate?: CatalogRate | null
  output_rate?: CatalogRate | null
  additional_rates?: CatalogRateBand[] | null
  context_basis?: string
  time_bands?: CatalogTimeBand[] | null
}

/* @v034-temp-openapi-bridge */
export interface CatalogSalePrice {
  source?: string
  scope?: 'public' | 'authorized' | (string & {})
  scope_class?: 'public' | 'project' | 'workspace' | 'group' | (string & {})
  valid_from?: string | null
  valid_until?: string | null
  valid_interval?: { start?: string | null; end?: string | null } | null
  price_version?: string | null
  price_source?: string | null
  input_rate?: CatalogRate | null
  output_rate?: CatalogRate | null
  additional_rates?: CatalogRateBand[] | null
  time_bands?: CatalogTimeBand[] | null
  context_basis?: string
  min_input_rate?: CatalogRate | null
  max_input_rate?: CatalogRate | null
  min_output_rate?: CatalogRate | null
  max_output_rate?: CatalogRate | null
}

/* @v034-temp-openapi-bridge */
export interface CatalogBillingAxis {
  state: CatalogBillingState | (string & {})
  reason?: CatalogBillingReason | null
  sale_price?: CatalogSalePrice | null
  reference_price?: CatalogReferencePrice | null
}

/* @v034-temp-openapi-bridge */
export interface CatalogFieldProvenance {
  field: string
  state: 'known' | 'unknown' | 'stale' | 'conflict' | 'suppressed' | (string & {})
  source?: string | null
  source_kind?: CatalogSourceKind | null
  artifact_version?: string | null
  observed_at?: string | null
  provider?: string | null
  profile?: string | null
  match_rule?: string | null
  trust?: string | null
  scope?: string | null
  expires_at?: string | null
  value?: unknown
}

/* @v034-temp-openapi-bridge */
export interface CatalogCommonFacts {
  context_tokens: string | null
  context_kind: 'input' | 'total' | 'unspecified' | ''
  max_output_tokens: string | null
  embedding_dimensions: string | null
  tools: boolean | null
  vision: boolean | null
  reasoning: boolean | null
  prompt_caching: boolean | null
  pdf: boolean | null
  modalities: string[] | null
}

/* @v034-temp-openapi-bridge */
export interface CatalogModelSummary extends Omit<
  GeneratedModelSummary,
  'availability_state' | 'price_state' | 'access_state' | 'route_state'
> {
  canonical_id?: string | null
  source?: CatalogSourceAxis | null
  availability?: CatalogAvailabilityAxis | null
  billing?: CatalogBillingAxis | null
  availability_state?: 'live' | 'catalog_only' | 'listed_unrouted' | 'unavailable' | 'requires_scope' | (string & {}) | null
  access_state?: 'allowed' | 'denied' | 'unknown' | 'requires_scope' | (string & {}) | null
  route_state?: 'eligible' | 'ineligible' | 'unknown' | 'requires_scope' | (string & {}) | null
  price_state?: 'priced' | 'free' | 'varies' | 'unpriced' | 'error' | (string & {}) | null
  pricing_mode?: string | null
  catalog_only_reason?: string | null
  reference_price?: CatalogReferencePrice | null
  metrics?: CatalogMetrics | null
}

/* @v034-temp-openapi-bridge */
export interface CatalogModelDetail extends Omit<GeneratedModelDetail, 'model' | 'reference_price'> {
  model: CatalogModelSummary
  reference_price?: CatalogReferencePrice | null
  common_facts?: CatalogCommonFacts | CatalogModelFacts | CatalogModelFacts[] | null
  provenance?: CatalogFieldProvenance[] | null
  scope_version?: string | null
  publication_status?: string | null
}

/* @v034-temp-openapi-bridge */
export interface CatalogModelPage extends Omit<GeneratedCatalogModelPage, 'items'> {
  items: CatalogModelSummary[]
  scope_version?: string | null
}

/* @v034-temp-openapi-bridge */
export interface CatalogCompareScenario extends CatalogPriceScenario {
  service_tier?: string
  reasoning_effort?: string
  thinking_enabled?: boolean
  explicit_cache?: boolean
  batch?: boolean
}

/* @v034-temp-openapi-bridge */
export interface CatalogCompareInput extends Omit<GeneratedCatalogCompareInput, 'scenario'> {
  scenario?: CatalogCompareScenario
  api_key_id?: string
}

/* @v034-temp-openapi-bridge */
export interface CatalogEstimate extends Omit<GeneratedCatalogEstimate, 'state'> {
  state: 'estimated' | 'free' | 'unpriced' | 'conditional' | 'partial' | 'conflict' | 'error' | (string & {})
  billing_state?: CatalogBillingState | (string & {})
  reason?: CatalogBillingReason | null
  price_at?: string | null
}

/* @v034-temp-openapi-bridge */
export interface CatalogCompareRow extends Omit<GeneratedCompareRowFix, 'quote'> {
  quote?: CatalogEstimate | null
}
type GeneratedCompareRowFix = GeneratedCatalogCompareRow

/* @v034-temp-openapi-bridge */
export interface CatalogCompareResult extends Omit<GeneratedCatalogCompareResult, 'rows'> {
  rows: CatalogCompareRow[]
  scope_version?: string | null
}

/* @v034-temp-openapi-bridge */
export interface CatalogListParams {
  q?: string
  provider?: string[]
  protocol?: string[]
  action?: string[]
  modality?: string[]
  region?: string[]
  retention?: string[]
  stability?: string[]
  availability_state?: 'live' | 'catalog_only' | (string & {})
  availability?: CatalogAxisAvailabilityState | (string & {})
  billing?: CatalogBillingState | (string & {})
  source?: string[]
  api_key_id?: string
  accessible?: boolean
  sort?: 'name' | 'price_input' | 'context' | 'ttft' | 'availability'
  cursor?: string | null
  limit?: number
}
