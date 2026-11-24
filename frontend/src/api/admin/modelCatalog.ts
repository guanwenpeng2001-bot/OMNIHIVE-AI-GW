import { apiClient } from '@/api/client'
import { normalizeApiError } from '@/utils/apiError'

export type AdminCatalogSourceKind =
  | 'builtin_reviewed'
  | 'platform_preset'
  | 'builtin_supplement'
  | 'pi_ai_reference'
  | 'account_discovery'
  | 'account_declaration'
  | 'external_reference'
  | 'heuristic'
  | 'unknown'

export type AdminCatalogAvailabilityState =
  | 'live'
  | 'listed_unrouted'
  | 'catalog_only'
  | 'requires_scope'
  | 'unavailable'
  | 'unknown'

export type AdminCatalogPublicationState = 'published' | 'unpublished' | 'unknown'

export type AdminCatalogAccessState = 'allowed' | 'denied' | 'requires_scope' | 'unknown'

export type AdminCatalogBillingState =
  | 'priced'
  | 'free'
  | 'conditional'
  | 'partial'
  | 'conflict'
  | 'varies'
  | 'unpriced'
  | 'error'
  | 'unknown'

const quickFilters = [
  'routable_but_unpriced',
  'discovered_not_enabled',
  'listed_unrouted',
  'group_price_conflict',
  'pi_only',
  'stale'
] as const
export type AdminCatalogQuickFilter = typeof quickFilters[number]

export interface AdminCatalogRate {
  currency: string
  amount: string
  unit: string
  per: string
}

export interface AdminCatalogSalePrice {
  source: string
  scope: 'public' | 'authorized' | 'unknown'
  price_version: string | null
  valid_from: string | null
  valid_until: string | null
  input_rate: AdminCatalogRate | null
  output_rate: AdminCatalogRate | null
  additional_rates: unknown[]
  time_bands: unknown[]
}

export interface AdminCatalogReferencePrice {
  input_rate: AdminCatalogRate | null
  output_rate: AdminCatalogRate | null
  additional_rates: unknown[]
  time_bands: unknown[]
  state: string
  source: string
  observed_at: string | null
}

export interface AdminCatalogSourceAxis {
  kinds: AdminCatalogSourceKind[]
  candidate: boolean | null
  discovered: boolean | null
}

export interface AdminCatalogAvailability {
  state: AdminCatalogAvailabilityState
  publication: AdminCatalogPublicationState
  access: AdminCatalogAccessState
  reason: string | null
}

export interface AdminCatalogBilling {
  state: AdminCatalogBillingState
  reason: string | null
  sale_price: AdminCatalogSalePrice | null
  reference_price: AdminCatalogReferencePrice | null
}

export interface AdminCatalogModelSummary {
  id: string
  canonical_id: string
  model: string
  display_name: string
  provider: string
  source: AdminCatalogSourceAxis
  availability: AdminCatalogAvailability
  billing: AdminCatalogBilling
  aliases_count: number | null
  accounts_count: number | null
  discovered_accounts_count: number | null
  groups_count: number | null
  channels_count: number | null
  has_conflict?: boolean
  has_stale?: boolean
}

export interface AdminCatalogModelPage {
  quick_filter_counts: Partial<Record<AdminCatalogQuickFilter, number>>
  catalog_version: string
  as_of: string | null
  scope_version: string | null
  items: AdminCatalogModelSummary[]
  next_cursor: string | null
}

export type AdminCatalogProvenanceState =
  | 'known'
  | 'unknown'
  | 'stale'
  | 'conflict'
  | 'suppressed'

export interface AdminCatalogFieldProvenance {
  field: string
  state: AdminCatalogProvenanceState
  source_kind: AdminCatalogSourceKind
  artifact_version: string | null
  observed_at: string | null
  expires_at: string | null
  scope: string | null
  value: unknown
}

export interface AdminCatalogCommonFacts {
  context_tokens: string | null
  context_kind: string | null
  max_output_tokens: string | null
  embedding_dimensions: string | null
  tools: boolean | null
  vision: boolean | null
  reasoning: boolean | null
  prompt_caching: boolean | null
  pdf: boolean | null
  modalities: string[] | null
}

export type AdminCatalogDiscoveryState = 'current' | 'stale' | 'invalidated' | 'unknown'

export type AdminCatalogMembershipState = 'eligible' | 'no_eligible_binding' | 'unknown' | string

export interface AdminCatalogAccount {
  account_id: string
  label: string
  account_type: string
  discovery_state: AdminCatalogDiscoveryState
  membership_state: AdminCatalogMembershipState
  observed_at: string | null
  identity_revision: string | null
  snapshot_revision: string | null
  rules_revision: string | null
}

export interface AdminCatalogOffer {
  group_id: string
  channel_id: string
  account_id: string | null
  action: string
  protocol: string
  eligible: boolean
  reason: string | null
  price_context: string
  billing: AdminCatalogBilling
}

export interface AdminCatalogPublication {
  group_id: string
  channel_id: string
  state: AdminCatalogPublicationState
  basis: 'explicit' | 'legacy_default' | null
  reason: string | null
}

export interface AdminCatalogDetail {
  catalog_version: string
  as_of: string | null
  scope_version: string | null
  model: AdminCatalogModelSummary
  aliases: string[]
  common_facts: AdminCatalogCommonFacts
  provenance: AdminCatalogFieldProvenance[]
  accounts: AdminCatalogAccount[]
  offers: AdminCatalogOffer[]
  publications: AdminCatalogPublication[]
  next_accounts_cursor: string | null
  next_offers_cursor: string | null
}

export interface AdminCatalogListParams {
  workspace_id: string
  project_id?: string | null
  provider?: string
  model?: string
  q?: string
  source?: string
  publication?: AdminCatalogPublicationState | ''
  availability?: string
  billing?: string
  quick_filter?: AdminCatalogQuickFilter | ''
  cursor?: string | null
  limit?: number
}

export interface AdminCatalogDetailParams {
  workspace_id: string
  project_id?: string | null
  catalog_version?: string | null
  accounts_cursor?: string | null
  offers_cursor?: string | null
}

function asRecord(val: unknown): Record<string, unknown> | null {
  return val !== null && typeof val === 'object' && !Array.isArray(val)
    ? (val as Record<string, unknown>)
    : null
}

function asNonEmptyString(val: unknown): string | null {
  if (typeof val === 'string') {
    const trimmed = val.trim()
    return trimmed.length > 0 ? trimmed : null
  }
  if (typeof val === 'number' && Number.isFinite(val)) {
    return String(val)
  }
  return null
}

function asNullableBoolean(val: unknown): boolean | null {
  return typeof val === 'boolean' ? val : null
}

function asNullableNonNegativeInt(val: unknown): number | null {
  if (typeof val === 'number' && Number.isInteger(val) && val >= 0) return val
  if (typeof val === 'string' && /^\d+$/.test(val.trim())) {
    const parsed = Number(val.trim())
    return Number.isSafeInteger(parsed) ? parsed : null
  }
  return null
}

const KNOWN_SOURCE_KINDS = new Set<AdminCatalogSourceKind>([
  'builtin_reviewed',
  'platform_preset',
  'builtin_supplement',
  'pi_ai_reference',
  'account_discovery',
  'account_declaration',
  'external_reference',
  'heuristic'
])

function normalizeSourceKind(raw: unknown): AdminCatalogSourceKind {
  if (typeof raw === 'string') {
    const trimmed = raw.trim()
    if (trimmed === 'approved_registry') return 'builtin_reviewed'
    if (trimmed === 'pi-ai') return 'pi_ai_reference'
    if (KNOWN_SOURCE_KINDS.has(trimmed as AdminCatalogSourceKind)) {
      return trimmed as AdminCatalogSourceKind
    }
  }
  return 'unknown'
}

function normalizeRate(raw: unknown): AdminCatalogRate | null {
  const rec = asRecord(raw)
  if (!rec) return null
  const amount = asNonEmptyString(rec.amount)
  if (!amount) return null
  return {
    currency: asNonEmptyString(rec.currency) ?? 'USD',
    amount,
    unit: asNonEmptyString(rec.unit) ?? 'token',
    per: asNonEmptyString(rec.per) ?? '1000000'
  }
}

function normalizeSalePrice(raw: unknown): AdminCatalogSalePrice | null {
  const rec = asRecord(raw)
  if (!rec) return null
  const scopeRaw = asNonEmptyString(rec.scope)
  const scope: 'public' | 'authorized' | 'unknown' =
    scopeRaw === 'public' || scopeRaw === 'authorized' ? scopeRaw : 'unknown'
  return {
    source: asNonEmptyString(rec.source) ?? 'unknown',
    scope,
    price_version: asNonEmptyString(rec.price_version),
    valid_from: asNonEmptyString(rec.valid_from),
    valid_until: asNonEmptyString(rec.valid_until),
    input_rate: normalizeRate(rec.input_rate),
    output_rate: normalizeRate(rec.output_rate),
    additional_rates: Array.isArray(rec.additional_rates) ? rec.additional_rates : [],
    time_bands: Array.isArray(rec.time_bands) ? rec.time_bands : []
  }
}

function normalizeReferencePrice(raw: unknown): AdminCatalogReferencePrice | null {
  const rec = asRecord(raw)
  if (!rec) return null
  const inputRate = normalizeRate(rec.input_rate)
  const outputRate = normalizeRate(rec.output_rate)
  if (!inputRate && !outputRate && !asNonEmptyString(rec.source)) return null
  return {
    input_rate: inputRate,
    output_rate: outputRate,
    additional_rates: Array.isArray(rec.additional_rates) ? rec.additional_rates : [],
    time_bands: Array.isArray(rec.time_bands) ? rec.time_bands : [],
    state: asNonEmptyString(rec.state) ?? 'reference',
    source: asNonEmptyString(rec.source) ?? 'pi-ai',
    observed_at: asNonEmptyString(rec.observed_at)
  }
}

function normalizeBilling(raw: unknown): AdminCatalogBilling {
  const rec = asRecord(raw)
  if (!rec) {
    return {
      state: 'unknown',
      reason: null,
      sale_price: null,
      reference_price: null
    }
  }
  const stateRaw = asNonEmptyString(rec.state)
  const validStates = new Set<AdminCatalogBillingState>([
    'priced',
    'free',
    'conditional',
    'partial',
    'conflict',
    'varies',
    'unpriced',
    'error',
    'unknown'
  ])
  const state: AdminCatalogBillingState =
    stateRaw && validStates.has(stateRaw as AdminCatalogBillingState)
      ? (stateRaw as AdminCatalogBillingState)
      : 'unknown'
  return {
    state,
    reason: asNonEmptyString(rec.reason),
    sale_price: normalizeSalePrice(rec.sale_price),
    reference_price: normalizeReferencePrice(rec.reference_price)
  }
}

function normalizeAvailability(raw: unknown): AdminCatalogAvailability {
  const rec = asRecord(raw)
  if (!rec) {
    return {
      state: 'unknown',
      publication: 'unknown',
      access: 'unknown',
      reason: null
    }
  }
  const stateRaw = asNonEmptyString(rec.state)
  const validStates = new Set<AdminCatalogAvailabilityState>([
    'live',
    'listed_unrouted',
    'catalog_only',
    'requires_scope',
    'unavailable',
    'unknown'
  ])
  const state: AdminCatalogAvailabilityState =
    stateRaw && validStates.has(stateRaw as AdminCatalogAvailabilityState)
      ? (stateRaw as AdminCatalogAvailabilityState)
      : 'unknown'

  const pubRaw = asNonEmptyString(rec.publication)
  const publication: AdminCatalogPublicationState =
    pubRaw === 'published' || pubRaw === 'unpublished' ? pubRaw : 'unknown'

  const accessRaw = asNonEmptyString(rec.access)
  const access: AdminCatalogAccessState =
    accessRaw === 'allowed' || accessRaw === 'denied' || accessRaw === 'requires_scope'
      ? accessRaw
      : 'unknown'

  return {
    state,
    publication,
    access,
    reason: asNonEmptyString(rec.reason)
  }
}

export function normalizeAdminCatalogModelSummary(raw: unknown): AdminCatalogModelSummary {
  const rec = asRecord(raw) ?? {}
  const id = asNonEmptyString(rec.id) ?? 'unknown'
  const canonicalId = asNonEmptyString(rec.canonical_id) ?? id
  const model = asNonEmptyString(rec.model) ?? id
  const displayName = asNonEmptyString(rec.display_name) ?? model
  const provider = asNonEmptyString(rec.provider) ?? 'unknown'

  const sourceRec = asRecord(rec.source)
  const kindsRaw = Array.isArray(sourceRec?.kinds) ? sourceRec.kinds : []
  const kinds = Array.from(
    new Set(
      kindsRaw
        .map((item) => normalizeSourceKind(item))
        .filter((kind): kind is AdminCatalogSourceKind => kind !== 'unknown')
    )
  )

  const source: AdminCatalogSourceAxis = {
    kinds,
    candidate: asNullableBoolean(sourceRec?.candidate),
    discovered: asNullableBoolean(sourceRec?.discovered)
  }

  return {
    id,
    canonical_id: canonicalId,
    model,
    display_name: displayName,
    provider,
    source,
    availability: normalizeAvailability(rec.availability),
    billing: normalizeBilling(rec.billing),
    aliases_count: asNullableNonNegativeInt(rec.aliases_count),
    accounts_count: asNullableNonNegativeInt(rec.accounts_count),
    discovered_accounts_count: asNullableNonNegativeInt(rec.discovered_accounts_count),
    groups_count: asNullableNonNegativeInt(rec.groups_count),
    channels_count: asNullableNonNegativeInt(rec.channels_count),
    has_conflict: asNullableBoolean(rec.has_conflict) ?? undefined,
    has_stale: asNullableBoolean(rec.has_stale) ?? undefined
  }
}

function unwrapEnvelope<T>(payload: unknown): T {
  const rec = asRecord(payload)
  if (rec && 'code' in rec && 'data' in rec && rec.data !== undefined && rec.data !== null) {
    return rec.data as T
  }
  return payload as T
}

export function normalizeAdminCatalogModelPage(raw: unknown): AdminCatalogModelPage {
  const unwrapped = unwrapEnvelope<Record<string, unknown>>(raw)
  const rec = asRecord(unwrapped) ?? {}
  const itemsRaw = Array.isArray(rec.items) ? rec.items : []
  const counts = asRecord(rec.quick_filter_counts)
  const quickFilterCounts: AdminCatalogModelPage['quick_filter_counts'] = {}
  for (const key of quickFilters) {
    const value = asNullableNonNegativeInt(counts?.[key])
    if (value !== null) quickFilterCounts[key] = value
  }
  return {
    quick_filter_counts: quickFilterCounts,
    catalog_version: asNonEmptyString(rec.catalog_version) ?? 'unknown',
    as_of: asNonEmptyString(rec.as_of),
    scope_version: asNonEmptyString(rec.scope_version),
    items: itemsRaw.map((item) => normalizeAdminCatalogModelSummary(item)),
    next_cursor: asNonEmptyString(rec.next_cursor)
  }
}

function normalizeCommonFacts(raw: unknown): AdminCatalogCommonFacts {
  const rec = asRecord(raw)
  if (!rec) {
    return {
      context_tokens: null,
      context_kind: null,
      max_output_tokens: null,
      embedding_dimensions: null,
      tools: null,
      vision: null,
      reasoning: null,
      prompt_caching: null,
      pdf: null,
      modalities: null
    }
  }
  const modalities = Array.isArray(rec.modalities)
    ? rec.modalities
        .map((m) => asNonEmptyString(m))
        .filter((m): m is string => m !== null)
    : null
  return {
    context_tokens: asNonEmptyString(rec.context_tokens),
    context_kind: asNonEmptyString(rec.context_kind),
    max_output_tokens: asNonEmptyString(rec.max_output_tokens),
    embedding_dimensions: asNonEmptyString(rec.embedding_dimensions),
    tools: asNullableBoolean(rec.tools),
    vision: asNullableBoolean(rec.vision),
    reasoning: asNullableBoolean(rec.reasoning),
    prompt_caching: asNullableBoolean(rec.prompt_caching),
    pdf: asNullableBoolean(rec.pdf),
    modalities
  }
}

function normalizeProvenance(raw: unknown): AdminCatalogFieldProvenance {
  const rec = asRecord(raw) ?? {}
  const stateRaw = asNonEmptyString(rec.state)
  const validStates = new Set<AdminCatalogProvenanceState>([
    'known',
    'unknown',
    'stale',
    'conflict',
    'suppressed'
  ])
  const state: AdminCatalogProvenanceState =
    stateRaw && validStates.has(stateRaw as AdminCatalogProvenanceState)
      ? (stateRaw as AdminCatalogProvenanceState)
      : 'unknown'
  return {
    field: asNonEmptyString(rec.field) ?? 'unknown',
    state,
    source_kind: normalizeSourceKind(rec.source_kind),
    artifact_version: asNonEmptyString(rec.artifact_version),
    observed_at: asNonEmptyString(rec.observed_at),
    expires_at: asNonEmptyString(rec.expires_at),
    scope: asNonEmptyString(rec.scope),
    value: rec.value
  }
}

function normalizeAccount(raw: unknown): AdminCatalogAccount {
  const rec = asRecord(raw) ?? {}
  const dsRaw = asNonEmptyString(rec.discovery_state)
  const discoveryState: AdminCatalogDiscoveryState =
    dsRaw === 'current' || dsRaw === 'stale' || dsRaw === 'invalidated' ? dsRaw : 'unknown'
  return {
    account_id: asNonEmptyString(rec.account_id) ?? 'unknown',
    label: asNonEmptyString(rec.label) ?? `#${asNonEmptyString(rec.account_id) ?? 'unknown'}`,
    account_type: asNonEmptyString(rec.account_type) ?? 'unknown',
    discovery_state: discoveryState,
    membership_state: asNonEmptyString(rec.membership_state) ?? 'unknown',
    observed_at: asNonEmptyString(rec.observed_at),
    identity_revision: asNonEmptyString(rec.identity_revision),
    snapshot_revision: asNonEmptyString(rec.snapshot_revision),
    rules_revision: asNonEmptyString(rec.rules_revision)
  }
}

function normalizeOffer(raw: unknown): AdminCatalogOffer {
  const rec = asRecord(raw) ?? {}
  return {
    group_id: asNonEmptyString(rec.group_id) ?? '0',
    channel_id: asNonEmptyString(rec.channel_id) ?? '0',
    account_id: asNonEmptyString(rec.account_id),
    action: asNonEmptyString(rec.action) ?? 'unknown',
    protocol: asNonEmptyString(rec.protocol) ?? 'unknown',
    eligible: rec.eligible === true,
    reason: asNonEmptyString(rec.reason),
    price_context: asNonEmptyString(rec.price_context) ?? 'group_default',
    billing: normalizeBilling(rec.billing)
  }
}

function normalizePublication(raw: unknown): AdminCatalogPublication {
  const rec = asRecord(raw) ?? {}
  const stateRaw = asNonEmptyString(rec.state)
  const state: AdminCatalogPublicationState =
    stateRaw === 'published' || stateRaw === 'unpublished' ? stateRaw : 'unknown'
  const basisRaw = asNonEmptyString(rec.basis)
  const basis: 'explicit' | 'legacy_default' | null =
    basisRaw === 'explicit' || basisRaw === 'legacy_default' ? basisRaw : null
  return {
    group_id: asNonEmptyString(rec.group_id) ?? '0',
    channel_id: asNonEmptyString(rec.channel_id) ?? '0',
    state,
    basis,
    reason: asNonEmptyString(rec.reason)
  }
}

/**
 * Deduplicate publications by (group_id, channel_id).
 */
export function deduplicateAdminPublications(
  publications: AdminCatalogPublication[]
): AdminCatalogPublication[] {
  const merged = new Map<string, AdminCatalogPublication>()
  for (const pub of publications) {
    const key = `${pub.group_id}:${pub.channel_id}`
    const existing = merged.get(key)
    if (!existing || existing.state === 'unknown') {
      merged.set(key, pub)
    }
  }
  return [...merged.values()]
}

export function normalizeAdminCatalogDetail(raw: unknown): AdminCatalogDetail {
  const unwrapped = unwrapEnvelope<Record<string, unknown>>(raw)
  const rec = asRecord(unwrapped) ?? {}
  const aliasesRaw = Array.isArray(rec.aliases) ? rec.aliases : []
  const provenanceRaw = Array.isArray(rec.provenance) ? rec.provenance : []
  const accountsRaw = Array.isArray(rec.accounts) ? rec.accounts : []
  const offersRaw = Array.isArray(rec.offers) ? rec.offers : []
  const publicationsRaw = Array.isArray(rec.publications) ? rec.publications : []

  return {
    catalog_version: asNonEmptyString(rec.catalog_version) ?? 'unknown',
    as_of: asNonEmptyString(rec.as_of),
    scope_version: asNonEmptyString(rec.scope_version),
    model: normalizeAdminCatalogModelSummary(rec.model),
    aliases: aliasesRaw
      .map((a) => asNonEmptyString(a))
      .filter((a): a is string => a !== null),
    common_facts: normalizeCommonFacts(rec.common_facts),
    provenance: provenanceRaw.map((p) => normalizeProvenance(p)),
    accounts: accountsRaw.map((a) => normalizeAccount(a)),
    offers: offersRaw.map((o) => normalizeOffer(o)),
    publications: deduplicateAdminPublications(
      publicationsRaw.map((p) => normalizePublication(p))
    ),
    next_accounts_cursor: asNonEmptyString(rec.next_accounts_cursor),
    next_offers_cursor: asNonEmptyString(rec.next_offers_cursor)
  }
}

export function isAdminCatalogSnapshotExpired(error: unknown): boolean {
  if (!error || typeof error !== 'object') return false
  const normalized = normalizeApiError(error)
  return (
    normalized.status === 410 ||
    normalized.reason === 'SNAPSHOT_EXPIRED' ||
    normalized.code === 410 ||
    String(normalized.code ?? '') === 'SNAPSHOT_EXPIRED'
  )
}

/**
 * Format a single rate as a per-1M token string (or unit string) without float rounding drift.
 */
export function formatRatePerMillion(rate: AdminCatalogRate | null): string | null {
  if (!rate || !rate.amount) return null
  const amountNum = Number(rate.amount)
  const perNum = Number(rate.per || '1000000')
  const symbol = rate.currency === 'CNY' ? '¥' : '$'
  if (Number.isFinite(amountNum) && Number.isFinite(perNum) && perNum > 0) {
    const scaled =
      perNum === 1000000
        ? amountNum
        : Number(((amountNum * 1000000) / perNum).toPrecision(10))
    return `${symbol}${scaled}`
  }
  return `${symbol}${rate.amount}`
}

export function formatRatePairDisplay(
  inputRate: AdminCatalogRate | null,
  outputRate: AdminCatalogRate | null
): string {
  const inText = formatRatePerMillion(inputRate)
  const outText = formatRatePerMillion(outputRate)
  if (!inText && !outText) return '—'
  return `${inText ?? '—'} / ${outText ?? '—'}`
}

/**
 * Evaluate the diagnostic quick_filters against a normalized model summary row.
 */
export function matchesAdminCatalogQuickFilter(
  item: AdminCatalogModelSummary,
  quickFilter: AdminCatalogQuickFilter | ''
): boolean {
  if (!quickFilter) return true
  switch (quickFilter) {
    case 'routable_but_unpriced':
      return item.availability.state === 'live' && item.billing.state === 'unpriced'
    case 'discovered_not_enabled': {
      const discoveredCount = item.discovered_accounts_count ?? 0
      const eligibleCount = item.accounts_count ?? 0
      return (
        (discoveredCount > 0 && discoveredCount > eligibleCount) ||
        (item.source.discovered === true && eligibleCount === 0) ||
        item.availability.reason === 'discovered_without_eligible_binding'
      )
    }
    case 'listed_unrouted':
      return (
        item.availability.state === 'listed_unrouted' ||
        (item.availability.publication === 'published' &&
          item.availability.state === 'unavailable')
      )
    case 'group_price_conflict':
      return item.billing.state === 'conflict' || item.billing.state === 'varies'
    case 'pi_only':
      return (
        item.source.kinds.length === 1 && item.source.kinds[0] === 'pi_ai_reference'
      )
    case 'stale':
      return item.has_stale === true
    default:
      return true
  }
}

export interface AdminProvenanceMatrixRow {
  field: string
  approvedValue: string
  supplementValue: string
  piValue: string
  discoveredValue: string
  effectiveValue: string
  winningSource: AdminCatalogSourceKind
  hasConflict: boolean
  hasStale: boolean
}

export function formatProvenanceValue(value: unknown, contextKind?: string | null): string {
  if (value === undefined || value === null) return '—'
  if (typeof value === 'boolean') return value ? 'true' : 'false'
  if (typeof value === 'number') return Number.isFinite(value) ? String(value) : '—'
  if (typeof value === 'string') return value.trim() || '—'
  if (Array.isArray(value)) {
    const parts = value
      .map((item) => (typeof item === 'string' ? item.trim() : String(item)))
      .filter(Boolean)
    return parts.length > 0 ? parts.join(', ') : '—'
  }
  const rec = asRecord(value)
  if (rec) {
    const tokens = asNonEmptyString(rec.tokens)
    const basis = asNonEmptyString(rec.basis) ?? contextKind ?? null
    if (tokens) {
      return basis ? `${tokens} (${basis})` : tokens
    }
  }
  return '—'
}

const CANONICAL_PROVENANCE_FIELDS = [
  'context_window',
  'max_output_tokens',
  'modalities',
  'tools',
  'supports_vision',
  'reasoning',
  'prompt_caching',
  'embedding_dimensions',
  'pdf',
  'reference_price'
] as const

function canonicalFieldKey(field: string): string {
  const f = field.trim().toLowerCase()
  if (f === 'context_tokens') return 'context_window'
  if (f === 'vision') return 'supports_vision'
  if (f === 'supports_function_calling') return 'tools'
  return f
}

function effectiveFactForField(
  field: string,
  facts: AdminCatalogCommonFacts,
  billing?: AdminCatalogBilling
): string {
  switch (field) {
    case 'context_window':
      if (!facts.context_tokens) return '—'
      return facts.context_kind
        ? `${facts.context_tokens} (${facts.context_kind})`
        : facts.context_tokens
    case 'max_output_tokens':
      return facts.max_output_tokens ?? '—'
    case 'modalities':
      return facts.modalities && facts.modalities.length > 0
        ? facts.modalities.join(', ')
        : '—'
    case 'tools':
      return facts.tools === null ? '—' : facts.tools ? 'true' : 'false'
    case 'supports_vision':
      return facts.vision === null ? '—' : facts.vision ? 'true' : 'false'
    case 'reasoning':
      return facts.reasoning === null ? '—' : facts.reasoning ? 'true' : 'false'
    case 'prompt_caching':
      return facts.prompt_caching === null ? '—' : facts.prompt_caching ? 'true' : 'false'
    case 'embedding_dimensions':
      return facts.embedding_dimensions ?? '—'
    case 'pdf':
      return facts.pdf === null ? '—' : facts.pdf ? 'true' : 'false'
    case 'reference_price':
      if (billing?.reference_price) {
        return formatRatePairDisplay(
          billing.reference_price.input_rate,
          billing.reference_price.output_rate
        )
      }
      return '—'
    default:
      return '—'
  }
}

/**
 * Build the per-field provenance merge table with Approved > Supplement > Community (pi)
 * precedence and explicit conflict detection.
 */
export function buildAdminProvenanceMatrix(
  commonFacts: AdminCatalogCommonFacts,
  provenance: AdminCatalogFieldProvenance[],
  billing?: AdminCatalogBilling
): AdminProvenanceMatrixRow[] {
  const fieldOrder = new Set<string>(CANONICAL_PROVENANCE_FIELDS)
  for (const obs of provenance) {
    if (obs.field && obs.field !== 'unknown') {
      fieldOrder.add(canonicalFieldKey(obs.field))
    }
  }

  const rows: AdminProvenanceMatrixRow[] = []
  for (const field of fieldOrder) {
    const matching = provenance.filter((obs) => canonicalFieldKey(obs.field) === field)
    const byKind = new Map<AdminCatalogSourceKind, string[]>()
    let explicitConflict = false
    let hasStale = false

    for (const obs of matching) {
      if (obs.state === 'conflict') explicitConflict = true
      if (obs.state === 'stale') hasStale = true
      if (obs.state === 'unknown' || obs.state === 'suppressed') continue
      const formatted =
        field === 'reference_price' && (obs.value === undefined || obs.value === null)
          ? effectiveFactForField('reference_price', commonFacts, billing)
          : formatProvenanceValue(obs.value)
      if (formatted === '—' && obs.state !== 'conflict') continue
      const scopedText =
        obs.source_kind === 'account_discovery' && obs.scope
          ? `${formatted} (${obs.scope})`
          : formatted
      const list = byKind.get(obs.source_kind) ?? []
      if (!list.includes(scopedText)) list.push(scopedText)
      byKind.set(obs.source_kind, list)
    }

    const approvedList = [
      ...(byKind.get('builtin_reviewed') ?? []),
      ...(byKind.get('platform_preset') ?? [])
    ]
    const supplementList = byKind.get('builtin_supplement') ?? []
    const piList = [
      ...(byKind.get('pi_ai_reference') ?? []),
      ...(byKind.get('external_reference') ?? [])
    ]
    const discoveredList = [
      ...(byKind.get('account_discovery') ?? []),
      ...(byKind.get('account_declaration') ?? [])
    ]

    const approvedValue = approvedList.length > 0 ? approvedList.join(' | ') : '—'
    const supplementValue = supplementList.length > 0 ? supplementList.join(' | ') : '—'
    const piValue = piList.length > 0 ? piList.join(' | ') : '—'
    const discoveredValue = discoveredList.length > 0 ? discoveredList.join(' | ') : '—'

    let winningSource: AdminCatalogSourceKind = 'unknown'
    if (approvedValue !== '—') winningSource = 'builtin_reviewed'
    else if (supplementValue !== '—') winningSource = 'builtin_supplement'
    else if (piValue !== '—') winningSource = 'pi_ai_reference'
    else if (discoveredValue !== '—') winningSource = 'account_discovery'

    const factEffective = effectiveFactForField(field, commonFacts, billing)
    const effectiveValue =
      factEffective !== '—'
        ? factEffective
        : approvedValue !== '—'
          ? approvedValue
          : supplementValue !== '—'
            ? supplementValue
            : piValue !== '—'
              ? piValue
              : discoveredValue

    // Detect value disagreement across distinct non-empty sources
    const normalizedDistinctValues = new Set<string>()
    for (const rawVal of [approvedValue, supplementValue, piValue, discoveredValue]) {
      if (rawVal !== '—') {
        // Strip account scope tag "(#1)" when comparing raw values
        const base = rawVal.replace(/\s*\([^)]*\)\s*$/, '').trim()
        if (base && base !== '—') normalizedDistinctValues.add(base)
      }
    }
    const hasConflict = explicitConflict || normalizedDistinctValues.size > 1

    if (
      effectiveValue === '—' &&
      approvedValue === '—' &&
      supplementValue === '—' &&
      piValue === '—' &&
      discoveredValue === '—' &&
      !hasConflict
    ) {
      // Keep canonical core fields even when unknown ("—") so admins see honest unknown state
      if (
        ![
          'context_window',
          'max_output_tokens',
          'modalities',
          'tools',
          'supports_vision',
          'reasoning',
          'prompt_caching'
        ].includes(field)
      ) {
        continue
      }
    }

    rows.push({
      field,
      approvedValue,
      supplementValue,
      piValue,
      discoveredValue,
      effectiveValue,
      winningSource,
      hasConflict,
      hasStale
    })
  }
  return rows
}

export function buildUnpricedModelsCsv(items: AdminCatalogModelSummary[]): string {
  const header = [
    'id',
    'canonical_id',
    'provider',
    'model',
    'display_name',
    'availability_state',
    'billing_state',
    'reference_input_per_1m',
    'reference_output_per_1m',
    'accounts_count',
    'groups_count'
  ].join(',')
  const escapeCell = (val: string | number | null | undefined): string => {
    const text = val === null || val === undefined ? '' : String(val)
    if (/[",\n\r]/.test(text)) {
      return `"${text.replace(/"/g, '""')}"`
    }
    return text
  }
  const targetItems = items.filter(
    (item) =>
      item.billing.state === 'unpriced' ||
      matchesAdminCatalogQuickFilter(item, 'routable_but_unpriced')
  )
  const rows = (targetItems.length > 0 ? targetItems : items).map((item) =>
    [
      escapeCell(item.id),
      escapeCell(item.canonical_id),
      escapeCell(item.provider),
      escapeCell(item.model),
      escapeCell(item.display_name),
      escapeCell(item.availability.state),
      escapeCell(item.billing.state),
      escapeCell(formatRatePerMillion(item.billing.reference_price?.input_rate ?? null)),
      escapeCell(formatRatePerMillion(item.billing.reference_price?.output_rate ?? null)),
      escapeCell(item.accounts_count ?? ''),
      escapeCell(item.groups_count ?? '')
    ].join(',')
  )
  return [header, ...rows].join('\n')
}

function buildListQueryParams(params: AdminCatalogListParams): Record<string, string | number> {
  const query: Record<string, string | number> = {
    workspace_id: String(params.workspace_id).trim()
  }
  if (params.project_id && String(params.project_id).trim()) {
    query.project_id = String(params.project_id).trim()
  }
  if (params.provider && params.provider.trim()) {
    query.provider = params.provider.trim()
  }
  if (params.model && params.model.trim()) {
    query.model = params.model.trim()
  }
  if (params.q && params.q.trim()) {
    query.q = params.q.trim()
  }
  if (params.source && params.source.trim()) {
    query.source = params.source.trim()
  }
  if (params.publication && params.publication.trim()) {
    query.publication = params.publication.trim()
  }
  if (params.availability && params.availability.trim()) {
    query.availability = params.availability.trim()
  }
  if (params.billing && params.billing.trim()) {
    query.billing = params.billing.trim()
  }
  if (params.quick_filter && params.quick_filter.trim()) {
    query.quick_filter = params.quick_filter.trim()
  }
  if (params.cursor && params.cursor.trim()) {
    query.cursor = params.cursor.trim()
  }
  query.limit = params.limit && params.limit >= 1 && params.limit <= 100 ? params.limit : 50
  return query
}

function buildDetailQueryParams(params: AdminCatalogDetailParams): Record<string, string> {
  const query: Record<string, string> = {
    workspace_id: String(params.workspace_id).trim()
  }
  if (params.project_id && String(params.project_id).trim()) {
    query.project_id = String(params.project_id).trim()
  }
  if (params.catalog_version && params.catalog_version.trim()) {
    query.catalog_version = params.catalog_version.trim()
  }
  if (params.accounts_cursor && params.accounts_cursor.trim()) {
    query.accounts_cursor = params.accounts_cursor.trim()
  }
  if (params.offers_cursor && params.offers_cursor.trim()) {
    query.offers_cursor = params.offers_cursor.trim()
  }
  return query
}

export async function listAdminCatalogModels(
  params: AdminCatalogListParams,
  signal?: AbortSignal
): Promise<AdminCatalogModelPage> {
  const { data } = await apiClient.get('/api/v2/admin/catalog/models', {
    params: buildListQueryParams(params),
    signal
  })
  return normalizeAdminCatalogModelPage(data)
}

export async function getAdminCatalogModelDetail(
  id: string,
  params: AdminCatalogDetailParams,
  signal?: AbortSignal
): Promise<AdminCatalogDetail> {
  const { data } = await apiClient.get(
    `/api/v2/admin/catalog/models/${encodeURIComponent(id.trim())}`,
    {
      params: buildDetailQueryParams(params),
      signal
    }
  )
  return normalizeAdminCatalogDetail(data)
}

export const adminModelCatalogAPI = {
  listModels: listAdminCatalogModels,
  getModelDetail: getAdminCatalogModelDetail
}

export default adminModelCatalogAPI
