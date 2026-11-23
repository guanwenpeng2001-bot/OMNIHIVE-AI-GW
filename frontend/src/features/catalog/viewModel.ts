import type { UiTone } from '@/components/ui/types'
import type {
  CatalogAccessAxisState,
  CatalogBillingState,
  CatalogFieldProvenance,
  CatalogMetrics,
  CatalogModelDetail,
  CatalogModelSummary,
  CatalogPublicationState,
  CatalogRate,
  CatalogReferencePrice
} from './contract'
import { getCatalogCopy } from './copy'
import {
  extractDisplayModalities,
  formatCatalogOriginLabel,
  formatContextTokens,
  normalizeCatalogOriginKind,
  originTone,
  publicTokenPrice,
  publicValue,
  type CanonicalOriginKind
} from './statusPresentation'

export type ViewModelAvailabilityState =
  | 'live'
  | 'listed_unrouted'
  | 'requires_scope'
  | 'catalog_only'
  | 'unavailable'
  | 'unknown'

export type ViewModelPricingMode =
  | 'per_token'
  | 'tiered'
  | 'per_request'
  | 'free'
  | 'varies'
  | 'conflict'
  | 'conditional'
  | 'partial'
  | 'reference_only'
  | 'unpriced'
  | 'error'
  | 'unknown'

export interface OriginBadgeVM {
  kind: CanonicalOriginKind
  label: string
  tone: UiTone
}

export interface AvailabilityBadgeVM {
  state: Exclude<ViewModelAvailabilityState, 'unknown'>
  label: string
  tone: UiTone
}

export interface ReferencePriceVM {
  inputRate: CatalogRate | null
  outputRate: CatalogRate | null
  inputDisplay: string | null
  outputDisplay: string | null
  sourceRaw: string | null
  sourceLabel: string
  disclaimer: string
  disclaimerFull: string
}

export interface BillingBadgeVM {
  label: string
  tone: UiTone
}

export interface CatalogModelVM {
  id: string
  canonicalId: string | null
  model: string
  displayName: string
  provider: string
  catalogVersion: string | null
  observedAt: string | null
  contextTokens: string | null
  contextDisplay: string
  modalities: string[]
  displayModalities: string[]
  protocols: string[]
  actions: string[]
  regions: string[]
  maturity: string
  evidenceState: string
  metrics: CatalogMetrics | null
  origin: {
    state: 'known' | 'unknown'
    kinds: CanonicalOriginKind[]
    badges: OriginBadgeVM[]
    primaryKind: CanonicalOriginKind | null
    primaryBadge: OriginBadgeVM | null
    candidate: boolean | null
    discovered: boolean | null
  }
  availability: {
    state: ViewModelAvailabilityState
    publication: CatalogPublicationState
    access: CatalogAccessAxisState
    reason: string | null
    badge: AvailabilityBadgeVM | null
    summarySentence: string
    isCallable: boolean
  }
  billing: {
    state: CatalogBillingState
    pricingMode: ViewModelPricingMode
    reason: string | null
    reasonLabel: string | null
    inputRate: CatalogRate | null
    outputRate: CatalogRate | null
    inputDisplay: string
    outputDisplay: string
    modeBadge: BillingBadgeVM | null
    priceSource: string | null
    priceVersion: string | null
    hasSaleRates: boolean
    isFree: boolean
    isError: boolean
    isGroupVarying: boolean
    isEstimatable: boolean
    reference: ReferencePriceVM | null
  }
  raw: CatalogModelSummary
}

export type CatalogViewPerspective = 'public' | 'scoped' | 'admin'

const ORIGIN_PRIORITY: readonly CanonicalOriginKind[] = [
  'channel_published',
  'account_discovery',
  'reviewed_registry',
  'builtin_supplement',
  'platform_builtin',
  'pi_supplement',
  'platform_preset',
  'heuristic'
]

function normalizePublication(raw?: string | null): CatalogPublicationState {
  const val = String(raw ?? '').trim().toLowerCase()
  if (val === 'published' || val === 'unpublished') return val
  return 'unknown'
}

function normalizeAccess(raw?: string | null): CatalogAccessAxisState {
  const val = String(raw ?? '').trim().toLowerCase()
  if (val === 'allowed' || val === 'denied' || val === 'requires_scope') return val
  return 'unknown'
}

function normalizeBillingReasonLabel(
  reason: string | null | undefined,
  isZh: boolean
): string | null {
  if (!reason) return null
  const copy = getCatalogCopy(isZh)
  const key = String(reason).trim().toLowerCase() as keyof typeof copy.billing.reasons
  if (key in copy.billing.reasons) {
    return copy.billing.reasons[key]
  }
  return String(reason)
}

function resolveOriginAxis(
  summary: CatalogModelSummary,
  detail: CatalogModelDetail | null | undefined,
  isZh: boolean
): CatalogModelVM['origin'] {
  const rawKinds = summary.source?.kinds ?? []
  const extraOrigin = (summary as { catalog_origin?: string | null }).catalog_origin
  const candidateKinds = extraOrigin ? [...rawKinds, extraOrigin] : rawKinds
  const normalized = new Set<CanonicalOriginKind>()
  for (const kind of candidateKinds) {
    const mapped = normalizeCatalogOriginKind(kind)
    if (mapped !== 'unknown') normalized.add(mapped)
  }

  if (normalized.size === 0 && detail) {
    for (const fact of detail.model_facts ?? []) {
      const mapped = normalizeCatalogOriginKind(fact.source)
      if (mapped !== 'unknown') normalized.add(mapped)
    }
    for (const prov of detail.provenance ?? []) {
      const mapped = normalizeCatalogOriginKind(prov.source_kind)
      if (mapped !== 'unknown') normalized.add(mapped)
    }
  }

  if (summary.availability?.publication === 'published') {
    normalized.add('channel_published')
  }
  const orderedKinds = ORIGIN_PRIORITY.filter((k) => normalized.has(k))
  const badges: OriginBadgeVM[] = orderedKinds.map((kind) => ({
    kind,
    label: formatCatalogOriginLabel(kind, isZh),
    tone: originTone(kind)
  }))
  const primaryKind = orderedKinds[0] ?? null
  const primaryBadge = badges[0] ?? null

  return {
    state: summary.source || badges.length > 0 ? 'known' : 'unknown',
    kinds: orderedKinds,
    badges,
    primaryKind,
    primaryBadge,
    candidate: summary.source?.candidate ?? null,
    discovered: summary.source?.discovered ?? null
  }
}

function isCatalogOnlyReason(reason?: string | null): boolean {
  return (
    reason === 'candidate_only' ||
    reason === 'platform_catalog' ||
    reason === 'catalog_only' ||
    reason === 'discovered_without_eligible_binding' ||
    reason === 'discovered_unactivated'
  )
}

function resolveAvailabilityAxis(
  summary: CatalogModelSummary,
  origin: CatalogModelVM['origin'],
  isZh: boolean,
  perspective: CatalogViewPerspective
): CatalogModelVM['availability'] {
  const copy = getCatalogCopy(isZh)
  let state: ViewModelAvailabilityState = 'unknown'
  let publication: CatalogPublicationState = 'unknown'
  let access: CatalogAccessAxisState = 'unknown'
  let reason: string | null = null

  if (summary.availability) {
    const rawState = String(summary.availability.state ?? '').trim().toLowerCase()
    publication = normalizePublication(summary.availability.publication)
    access = normalizeAccess(summary.availability.access)
    reason = summary.availability.reason ?? null

    if (rawState === 'live') {
      state = 'live'
    } else if (rawState === 'requires_scope') {
      if (
        publication === 'unpublished' ||
        isCatalogOnlyReason(reason)
      ) {
        state = 'catalog_only'
      } else if (perspective === 'public') {
        // Per BE-3 + P4 reconciliation: anonymous advertised routable offers carry
        // availability.state=requires_scope on wire; public plaza/pricing renders them as normal 可调用/Available.
        state = 'live'
      } else {
        state = 'requires_scope'
      }
    } else if (rawState === 'unavailable') {
      if (
        reason === 'published_without_eligible_binding' ||
        (publication === 'published' && reason !== 'access_denied' && reason !== 'no_eligible_binding')
      ) {
        state = 'listed_unrouted'
      } else if (
        isCatalogOnlyReason(reason) ||
        (publication === 'unpublished' && origin.candidate === true && origin.discovered !== true)
      ) {
        state = 'catalog_only'
      } else if (reason === 'eligibility_incomplete') {
        state = 'unknown'
      } else {
        state = 'unavailable'
      }
    } else {
      state = 'unknown'
    }
  } else if (summary.availability_state) {
    // Legacy v0.3.3 fallback: honor ONLY explicit availability_state; NEVER guess from price_state/route_state.
    const explicit = String(summary.availability_state).trim().toLowerCase()
    if (explicit === 'live') {
      state = 'live'
      publication = 'published'
    } else if (explicit === 'catalog_only') {
      state = 'catalog_only'
    } else if (explicit === 'listed_unrouted') {
      state = 'listed_unrouted'
      publication = 'published'
    } else if (explicit === 'requires_scope') {
      state = perspective === 'public' ? 'live' : 'requires_scope'
    } else if (explicit === 'unavailable') {
      state = 'unavailable'
    } else {
      state = 'unknown'
    }
  }

  const badge = buildAvailabilityBadge(state, copy, perspective)
  const summarySentence = buildAvailabilitySentence(state, reason, copy)

  return {
    state,
    publication,
    access,
    reason,
    badge,
    summarySentence,
    isCallable: state === 'live'
  }
}

function buildAvailabilityBadge(
  state: ViewModelAvailabilityState,
  copy: ReturnType<typeof getCatalogCopy>,
  perspective: CatalogViewPerspective
): AvailabilityBadgeVM | null {
  switch (state) {
    case 'live':
      return { state, label: copy.availability.live, tone: 'success' }
    case 'listed_unrouted':
      return { state, label: copy.availability.listedUnrouted, tone: 'warning' }
    case 'requires_scope':
      if (perspective === 'public') return null
      return { state, label: copy.availability.requiresScope, tone: 'info' }
    case 'catalog_only':
      return { state, label: copy.availability.catalogOnly, tone: 'neutral' }
    case 'unavailable':
      return { state, label: copy.availability.unavailable, tone: 'danger' }
    default:
      return null
  }
}

function buildAvailabilitySentence(
  state: ViewModelAvailabilityState,
  reason: string | null,
  copy: ReturnType<typeof getCatalogCopy>
): string {
  switch (state) {
    case 'live':
      return copy.availability.sentences.live
    case 'listed_unrouted':
      return copy.availability.sentences.listedUnrouted
    case 'requires_scope':
      return copy.availability.sentences.requiresScope
    case 'catalog_only':
      if (reason === 'discovered_without_eligible_binding' || reason === 'discovered_unactivated') {
        return copy.availability.sentences.catalogOnlyDiscovered
      }
      if (reason === 'candidate_only' || reason === 'platform_catalog') {
        return copy.availability.sentences.catalogOnlyPlatform
      }
      return copy.availability.sentences.catalogOnlyDefault
    case 'unavailable':
      return copy.availability.sentences.unavailable
    default:
      return copy.availability.sentences.unknown
  }
}

function resolveReferencePriceVM(
  rawRef: CatalogReferencePrice | null | undefined,
  isZh: boolean
): ReferencePriceVM | null {
  if (!rawRef) return null
  const inputRate = rawRef.input_rate ?? null
  const outputRate = rawRef.output_rate ?? null
  if (!inputRate && !outputRate && !rawRef.source) {
    return null
  }
  const copy = getCatalogCopy(isZh)
  const inputFormatted = inputRate ? publicTokenPrice(inputRate, 'adaptive', isZh) : null
  const outputFormatted = outputRate ? publicTokenPrice(outputRate, 'adaptive', isZh) : null
  const sourceRaw = rawRef.source?.trim() || null
  const sourceLabel = formatCatalogOriginLabel(sourceRaw, isZh)
  return {
    inputRate,
    outputRate,
    inputDisplay: inputFormatted && inputFormatted !== '—' ? inputFormatted : null,
    outputDisplay: outputFormatted && outputFormatted !== '—' ? outputFormatted : null,
    sourceRaw,
    sourceLabel,
    disclaimer: copy.billing.nonSettlementShort,
    disclaimerFull: copy.billing.nonSettlementFull
  }
}

function resolveBillingAxis(
  summary: CatalogModelSummary,
  detail: CatalogModelDetail | null | undefined,
  isZh: boolean
): CatalogModelVM['billing'] {
  const copy = getCatalogCopy(isZh)
  const salePrice = summary.billing?.sale_price ?? null
  const inputRate = salePrice?.input_rate ?? summary.input_rate ?? null
  const outputRate = salePrice?.output_rate ?? summary.output_rate ?? null
  const additionalRates = salePrice?.additional_rates ?? detail?.additional_rates ?? []
  const timeBands = salePrice?.time_bands ?? detail?.time_bands ?? []
  const rawRef = summary.billing?.reference_price ?? detail?.reference_price ?? null
  const reference = resolveReferencePriceVM(rawRef, isZh)
  const priceSource = salePrice?.source ?? detail?.price_source ?? null
  const priceVersion = salePrice?.price_version ?? detail?.price_version ?? null
  const reason = summary.billing?.reason ?? null
  const reasonLabel = normalizeBillingReasonLabel(reason, isZh)

  const state = summary.billing?.state ?? summary.price_state ?? 'unknown'
  const hasPerRequestUnit = inputRate?.unit === 'request' || outputRate?.unit === 'request'
  const hasTieredSignals = additionalRates.length > 0 || timeBands.length > 0

  let pricingMode: ViewModelPricingMode = 'unknown'
  let modeBadge: BillingBadgeVM | null = null
  let inputDisplay = '—'
  let outputDisplay = '—'
  const hasSaleRates = Boolean(inputRate || outputRate)

  switch (state) {
    case 'free':
      pricingMode = 'free'
      modeBadge = { label: copy.billing.free, tone: 'success' }
      inputDisplay = copy.billing.free
      outputDisplay = copy.billing.free
      break
    case 'conflict':
      pricingMode = 'varies'
      modeBadge = { label: copy.billing.varies, tone: 'info' }
      inputDisplay = copy.billing.varies
      outputDisplay = copy.billing.varies
      break
    case 'conditional':
      pricingMode = 'conditional'
      modeBadge = { label: copy.billing.conditional, tone: 'info' }
      inputDisplay = inputRate ? publicTokenPrice(inputRate, 'adaptive', isZh) : copy.billing.conditional
      outputDisplay = outputRate ? publicTokenPrice(outputRate, 'adaptive', isZh) : copy.billing.conditional
      break
    case 'partial':
      pricingMode = 'partial'
      modeBadge = { label: copy.billing.partial, tone: 'warning' }
      inputDisplay = inputRate ? publicTokenPrice(inputRate, 'adaptive', isZh) : '—'
      outputDisplay = outputRate ? publicTokenPrice(outputRate, 'adaptive', isZh) : '—'
      break
    case 'priced':
      if (hasPerRequestUnit) {
        pricingMode = 'per_request'
        modeBadge = { label: copy.billing.perRequest, tone: 'info' }
      } else if (hasTieredSignals && !hasSaleRates) {
        pricingMode = 'tiered'
        modeBadge = { label: copy.billing.tiered, tone: 'info' }
      } else {
        pricingMode = 'per_token'
        modeBadge = { label: copy.billing.perToken, tone: 'neutral' }
      }
      inputDisplay = inputRate ? publicTokenPrice(inputRate, 'adaptive', isZh) : '—'
      outputDisplay = outputRate ? publicTokenPrice(outputRate, 'adaptive', isZh) : '—'
      break
    case 'unpriced':
      pricingMode = reference ? 'reference_only' : 'unpriced'
      modeBadge = { label: copy.billing.unpriced, tone: 'neutral' }
      inputDisplay = '—'
      outputDisplay = '—'
      break
    case 'error':
      pricingMode = 'error'
      modeBadge = { label: copy.billing.error, tone: 'warning' }
      inputDisplay = copy.billing.error
      outputDisplay = copy.billing.error
      break
    default:
      pricingMode = 'unknown'
      modeBadge = null
      inputDisplay = '—'
      outputDisplay = '—'
      break
  }

  const isGroupVarying = state === 'conflict'
  const isFree = state === 'free'
  const isError = state === 'error'
  const isEstimatable =
    isFree ||
    ((state === 'priced' || state === 'conditional') && Boolean(inputRate || outputRate))

  return {
    state,
    pricingMode,
    reason,
    reasonLabel,
    inputRate,
    outputRate,
    inputDisplay,
    outputDisplay,
    modeBadge,
    priceSource,
    priceVersion,
    hasSaleRates,
    isFree,
    isError,
    isGroupVarying,
    isEstimatable,
    reference
  }
}

export function toCatalogModelVM(
  summary: CatalogModelSummary,
  detail?: CatalogModelDetail | null,
  options: { isZh?: boolean; perspective?: CatalogViewPerspective } = {}
): CatalogModelVM {
  const isZh = Boolean(options.isZh)
  const perspective: CatalogViewPerspective = options.perspective ?? 'public'
  const origin = resolveOriginAxis(summary, detail, isZh)
  const availability = resolveAvailabilityAxis(summary, origin, isZh, perspective)
  const billing = resolveBillingAxis(summary, detail, isZh)
  const commonFact =
    detail?.common_facts && !Array.isArray(detail.common_facts) ? detail.common_facts : null
  const primaryModelFact =
    detail?.model_facts?.find((entry) => entry.state === 'fresh') ?? detail?.model_facts?.[0] ?? null
  const contextTokens = commonFact?.context_tokens ?? primaryModelFact?.context_tokens ?? summary.context_tokens ?? null
  const resolvedModalities = commonFact?.modalities?.length
    ? commonFact.modalities
    : primaryModelFact?.modalities?.length
      ? primaryModelFact.modalities
    : (summary.modalities ?? [])

  return {
    id: summary.id,
    canonicalId: summary.canonical_id ?? null,
    model: summary.model,
    displayName: summary.display_name || summary.model || summary.id,
    provider: publicValue(summary.provider, isZh ? '未声明' : 'Undeclared'),
    catalogVersion: summary.catalog_version ?? null,
    observedAt: summary.observed_at ?? null,
    contextTokens,
    contextDisplay: formatContextTokens(contextTokens),
    modalities: resolvedModalities,
    displayModalities: extractDisplayModalities({
      modalities: resolvedModalities,
      actions: summary.actions
    }),
    protocols: summary.protocols ?? [],
    actions: summary.actions ?? [],
    regions: summary.regions ?? [],
    maturity: summary.maturity ?? 'unknown',
    evidenceState: summary.evidence_state ?? 'unknown',
    metrics: detail?.metrics ?? null,
    origin,
    availability,
    billing,
    raw: summary
  }
}

export interface CatalogProvenanceItemVM {
  field: string
  state: string
  sourceLabel: string
  observedAt: string | null
}

export function resolveDetailProvenanceMap(
  detail: CatalogModelDetail | null | undefined,
  isZh = false
): Record<string, CatalogProvenanceItemVM> {
  const map: Record<string, CatalogProvenanceItemVM> = {}
  for (const item of (detail?.provenance ?? []) as CatalogFieldProvenance[]) {
    if (!item?.field) continue
    const rawSource = item.source_kind ?? null
    map[item.field] = {
      field: item.field,
      state: item.state || 'unknown',
      sourceLabel: formatCatalogOriginLabel(rawSource, isZh),
      observedAt: item.observed_at ?? null
    }
  }
  return map
}
