import { extractApiErrorCode, extractApiErrorStatus } from '@/utils/apiError'
import { selectPlatformModelPresets } from './usePlatformModelPresets'
import type { PlatformModelPreset } from '@/api/admin/platformModelPresets'
import type {
  AccountModelCatalogDetails,
  AccountModelScopeMode,
  AccountModelSelectionMode,
  ModelAllowlist,
  UpdateModelAllowlistRequest
} from '@/types/model'

export type { AccountModelSelectionMode, AccountModelScopeMode }

// =====================
// 常用错误码
// =====================

export const commonErrorCodes = [
  { value: 401, label: 'Unauthorized' },
  { value: 403, label: 'Forbidden' },
  { value: 429, label: 'Rate Limit' },
  { value: 500, label: 'Server Error' },
  { value: 502, label: 'Bad Gateway' },
  { value: 503, label: 'Unavailable' },
  { value: 529, label: 'Overloaded' }
]

// =====================
// 辅助函数
// =====================

export interface ModelCandidateIdentity {
  host?: string
  type?: string
  profile?: string
  region?: string
  mode?: string
}

export function modelCandidateKey(platform: string, identity: ModelCandidateIdentity = {}): string {
  return JSON.stringify([platform, identity.host ?? '', identity.type ?? '', identity.profile ?? '', identity.region ?? '', identity.mode ?? ''])
}

// Pure projections of the current API response; absence is an explicit empty set.
export function getModelsByPlatform(platform: string, identity: ModelCandidateIdentity = {}, presets: PlatformModelPreset[] = []): string[] {
  return [...new Set(selectPlatformModelPresets(presets, [platform], identity).flatMap(preset =>
    (preset.models ?? []).filter(model => model.lifecycle !== 'retired').map(model => model.id)))]
}

export function getPresetMappingsByPlatform(platform: string, identity: ModelCandidateIdentity = {}, presets: PlatformModelPreset[] = []) {
  const models = selectPlatformModelPresets(presets, [platform], identity)
    .flatMap(preset => preset.models ?? []).filter(model => model.lifecycle !== 'retired')
  return [...new Map(models.map(model => [model.id, {
    label: model.display_name || model.id, from: model.id, to: model.id,
    color: 'bg-purple-100 text-purple-700 hover:bg-purple-200 dark:bg-purple-900/30 dark:text-purple-400'
  }])).values()]
}

// =====================
// 构建模型映射对象（用于 API）
// =====================

// isValidWildcardPattern 校验通配符格式：* 只能放在末尾
// 导出供表单组件使用实时校验
export function isValidWildcardPattern(pattern: string): boolean {
  const starIndex = pattern.indexOf('*')
  if (starIndex === -1) return true // 无通配符，有效
  // * 必须在末尾，且只能有一个
  return starIndex === pattern.length - 1 && pattern.lastIndexOf('*') === starIndex
}

export type ModelRestrictionMode = 'whitelist' | 'mapping' | 'combined'

export interface ModelMappingEntry {
  from: string
  to: string
}

export interface AccountModelSelectionState {
  mode: AccountModelSelectionMode
  models: string[]
  mappings: ModelMappingEntry[]
  publicTargets: string[]
  reason: string
}

export function splitModelMappingObject(
  modelMapping?: Record<string, unknown> | null
): { allowedModels: string[]; modelMappings: ModelMappingEntry[] } {
  const allowedModels: string[] = []
  const modelMappings: ModelMappingEntry[] = []

  if (!modelMapping || typeof modelMapping !== 'object') {
    return { allowedModels, modelMappings }
  }

  for (const [rawFrom, rawTo] of Object.entries(modelMapping)) {
    const from = rawFrom.trim()
    const to = typeof rawTo === 'string' ? rawTo.trim() : ''

    if (from === to) {
      allowedModels.push(from)
    } else {
      modelMappings.push({ from, to })
    }
  }

  return { allowedModels, modelMappings }
}

export interface ModelRestrictionError {
  code:
    | 'exactIdRequired'
    | 'idsRequired'
    | 'invalidWildcard'
    | 'conflictingTarget'
    | 'emptyRestrictedSelection'
    | 'denyAllReasonRequired'
  row: number
  model: string
  from: string
  to: string
}

export function translateModelRestrictionErrors(
  errors: ModelRestrictionError[],
  translate: (key: string, values: Record<string, string | number>) => string
): string {
  return errors.map(error => translate(`admin.accounts.modelValidation.${error.code}`, { ...error })).join('\n')
}

// Validate before serialization so an invalid restriction never becomes unrestricted.
export function validateModelRestrictions(
  mode: ModelRestrictionMode | AccountModelSelectionMode,
  allowedModels: string[],
  modelMappings: ModelMappingEntry[],
  options?: { reason?: string; requireNonEmptyRestricted?: boolean }
): ModelRestrictionError[] {
  const errors: ModelRestrictionError[] = []
  if (mode === 'deny_all') {
    if (options?.reason !== undefined && !options.reason.trim()) {
      errors.push({ code: 'denyAllReasonRequired', row: 1, model: '', from: '', to: '' })
    }
    return errors
  }
  if (mode === 'restricted' && options?.requireNonEmptyRestricted) {
    const hasModel = allowedModels.some(model => model.trim().length > 0)
    const hasMapping = modelMappings.some(entry => entry.from.trim().length > 0 || entry.to.trim().length > 0)
    if (!hasModel && !hasMapping) {
      errors.push({ code: 'emptyRestrictedSelection', row: 1, model: '', from: '', to: '' })
      return errors
    }
  }
  const targets = new Map<string, string>()
  if (mode !== 'mapping' && mode !== 'inherit') {
    allowedModels.forEach((model, index) => {
      const value = model.trim()
      if (!value || value.includes('*')) errors.push({ code: 'exactIdRequired', row: index + 1, model, from: '', to: '' })
      else targets.set(value, value)
    })
  }
  if (mode !== 'whitelist') {
    modelMappings.forEach((entry, index) => {
      const from = entry.from.trim()
      const to = entry.to.trim()
      const location = { row: index + 1, model: from, from: entry.from, to: entry.to }
      if (!from || !to) errors.push({ code: 'idsRequired', ...location })
      else if (!isValidWildcardPattern(from) || to.includes('*')) errors.push({ code: 'invalidWildcard', ...location })
      else if (targets.has(from) && targets.get(from) !== to) errors.push({ code: 'conflictingTarget', ...location })
      else targets.set(from, to)
    })
  }
  return errors
}

export function buildModelMappingObject(
  mode: ModelRestrictionMode,
  allowedModels: string[],
  modelMappings: ModelMappingEntry[]
): Record<string, string> | null {
  const mapping: Record<string, string> = {}

  if (mode === 'whitelist' || mode === 'combined') {
    for (const model of allowedModels) {
      const normalizedModel = model.trim()
      if (!normalizedModel) continue
      // whitelist 模式的本意是"精确模型列表"，如果用户输入了通配符（如 claude-*），
      // 写入 model_mapping 会导致 GetMappedModel() 把真实模型映射成 "claude-*"，从而转发失败。
      // 因此这里跳过包含通配符的条目。
      if (!normalizedModel.includes('*')) {
        mapping[normalizedModel] = normalizedModel
      }
    }
  }

  if (mode === 'mapping' || mode === 'combined') {
    for (const m of modelMappings) {
      const from = m.from.trim()
      const to = m.to.trim()
      if (!from || !to) continue
      // 校验通配符格式：* 只能放在末尾
      if (!isValidWildcardPattern(from)) {
        console.warn(`[buildModelMappingObject] Invalid wildcard pattern, skipped: ${from}`)
        continue
      }
      // to 不允许包含通配符
      if (to.includes('*')) {
        console.warn(`[buildModelMappingObject] Target model cannot contain a wildcard, skipped: ${from} -> ${to}`)
        continue
      }
      mapping[from] = to
    }
  }

  return Object.keys(mapping).length > 0 ? mapping : null
}

export function extractAliasTargets(modelMappings: readonly ModelMappingEntry[]): string[] {
  const targets = new Set<string>()
  for (const entry of modelMappings) {
    const from = entry.from.trim()
    const to = entry.to.trim()
    if (!from || !to || from === to || to.includes('*')) continue
    targets.add(to)
  }
  return [...targets]
}

export function extractPublicAliasTargets(modelMapping?: Record<string, unknown> | null): string[] {
  if (!modelMapping || typeof modelMapping !== 'object') {
    return []
  }
  const aliasTargets = new Set<string>()
  const identities = new Set<string>()
  for (const [rawFrom, rawTo] of Object.entries(modelMapping)) {
    const from = rawFrom.trim()
    const to = typeof rawTo === 'string' ? rawTo.trim() : ''
    if (!from || !to) continue
    if (from === to) {
      if (!from.includes('*')) identities.add(from)
    } else if (!to.includes('*')) {
      aliasTargets.add(to)
    }
  }
  return [...identities].filter(target => aliasTargets.has(target))
}

export function computePublicRequestNames(input: {
  mode: AccountModelSelectionMode
  models: readonly string[]
  mappings?: readonly ModelMappingEntry[]
  publicTargets?: readonly string[]
}): string[] {
  if (input.mode === 'deny_all') {
    return []
  }
  const aliasKeys = new Set<string>()
  const aliasTargets = new Set<string>()
  const inlineIdentities = new Set<string>()
  for (const entry of input.mappings ?? []) {
    const from = entry.from.trim()
    const to = entry.to.trim()
    if (!from || !to) continue
    if (from === to) {
      if (!from.includes('*')) inlineIdentities.add(from)
      continue
    }
    if (!isValidWildcardPattern(from) || to.includes('*')) continue
    aliasKeys.add(from)
    aliasTargets.add(to)
  }
  if (input.mode === 'inherit') {
    return [...aliasKeys].sort()
  }
  const scope = new Set<string>()
  for (const model of input.models) {
    const trimmed = model.trim()
    if (trimmed && !trimmed.includes('*') && !trimmed.includes('?')) {
      scope.add(trimmed)
    }
  }
  for (const target of aliasTargets) {
    scope.add(target)
  }
  const publicTargetSet = new Set<string>()
  for (const rawTarget of [...inlineIdentities, ...(input.publicTargets ?? [])]) {
    const target = rawTarget.trim()
    if (target && aliasTargets.has(target) && scope.has(target)) {
      publicTargetSet.add(target)
    }
  }
  const names = new Set<string>()
  for (const model of scope) {
    if (!aliasTargets.has(model) && !aliasKeys.has(model)) {
      names.add(model)
    }
  }
  for (const alias of aliasKeys) {
    names.add(alias)
  }
  for (const target of publicTargetSet) {
    if (!aliasKeys.has(target)) {
      names.add(target)
    }
  }
  return [...names].sort()
}

export function buildExplicitModelMappingObject(
  modelMappings: ModelMappingEntry[],
  publicTargets: readonly string[] = []
): Record<string, string> {
  const mapping: Record<string, string> = {}
  const aliasTargets = new Set<string>()
  const inlineIdentities = new Set<string>()
  for (const entry of modelMappings) {
    const from = entry.from.trim()
    const to = entry.to.trim()
    if (!from || !to) continue
    if (from === to) {
      if (!from.includes('*')) inlineIdentities.add(from)
      continue
    }
    if (!isValidWildcardPattern(from) || to.includes('*')) continue
    mapping[from] = to
    aliasTargets.add(to)
  }
  for (const rawTarget of [...inlineIdentities, ...publicTargets]) {
    const target = rawTarget.trim()
    if (!target || target.includes('*') || !aliasTargets.has(target)) continue
    if (!(target in mapping)) {
      mapping[target] = target
    }
  }
  return mapping
}

export function mergeIdentityMappingsIntoModels(
  models: string[],
  mappings: ModelMappingEntry[]
): string[] {
  const identities = mappings.filter(entry => entry.from.trim() === entry.to.trim())
  return [...new Set([...models, ...identities.map(entry => entry.to)].map(model => model.trim()).filter(Boolean))]
}

export function mergeMappingTargetsIntoModels(
  models: string[],
  mappings: ModelMappingEntry[]
): string[] {
  const merged = new Set<string>()
  for (const model of models) {
    const trimmed = model.trim()
    if (trimmed && !trimmed.includes('*') && !trimmed.includes('?')) {
      merged.add(trimmed)
    }
  }
  for (const entry of mappings) {
    const target = entry.to.trim()
    if (target && !target.includes('*') && !target.includes('?')) {
      merged.add(target)
    }
  }
  return [...merged]
}

export function isLegacySystemFreezeAllowlist(
  allowlist?: ModelAllowlist | null,
  extra?: Record<string, unknown> | null
): boolean {
  if (!allowlist || !allowlist.enabled || (allowlist.models?.length ?? 0) > 0) {
    return false
  }
  if (
    allowlist.reason === 'system_upgrade_baseline_freeze' &&
    (!allowlist.confirmed_by || allowlist.confirmed_by === 0) &&
    !allowlist.confirmed_at
  ) {
    return true
  }
  if (
    extra?.model_selection_baseline_frozen === true &&
    (!allowlist.confirmed_by || allowlist.confirmed_by === 0) &&
    !allowlist.confirmed_at &&
    !allowlist.reason
  ) {
    return true
  }
  return false
}

export function normalizeAccountModelSelection(input?: {
  account?: {
    platform?: string
    credentials?: Record<string, unknown> | null
    extra?: Record<string, unknown> | null
  } | null
  catalog?: Partial<AccountModelCatalogDetails> | null
  modelAllowlist?: ModelAllowlist | null
  modelMapping?: Record<string, unknown> | null
}): AccountModelSelectionState {
  const allowlist = input?.modelAllowlist ?? accountModelAllowlist(input?.account)
  const rawMapping =
    (input?.catalog?.model_mapping ??
      input?.modelMapping ??
      (input?.account?.credentials?.model_mapping as Record<string, unknown> | undefined)) ??
    null
  const { allowedModels, modelMappings } = splitModelMappingObject(rawMapping)
  const publicTargets = extractPublicAliasTargets(rawMapping)
  const catalogModels = Array.isArray(input?.catalog?.models)
    ? input!.catalog!.models!.map(model => String(model).trim()).filter(Boolean)
    : []
  const isFrozen = isLegacySystemFreezeAllowlist(allowlist, input?.account?.extra)
  const allowlistModels =
    allowlist?.enabled && !isFrozen
      ? (allowlist.models ?? []).map(model => String(model).trim()).filter(Boolean)
      : []
  const reason =
    typeof allowlist?.reason === 'string' && !isFrozen ? allowlist.reason.trim() : ''

  if (
    input?.catalog?.mode === 'deny_all' ||
    (input?.catalog?.mode === undefined &&
      allowlist?.enabled === true &&
      !isFrozen &&
      allowlistModels.length === 0 &&
      catalogModels.length === 0)
  ) {
    return {
      mode: 'deny_all',
      models: [],
      mappings: modelMappings,
      publicTargets,
      reason
    }
  }

  const mergedModels = [...new Set([...allowedModels, ...catalogModels, ...allowlistModels])]

  if (input?.catalog?.mode === 'restricted') {
    return {
      mode: 'restricted',
      models: mergedModels,
      mappings: modelMappings,
      publicTargets: publicTargets.filter(target => mergedModels.includes(target)),
      reason
    }
  }

  if (input?.catalog?.mode === 'inherit') {
    if (mergedModels.length === 0 && modelMappings.length === 0) {
      return { mode: 'inherit', models: [], mappings: [], publicTargets: [], reason }
    }
    if (mergedModels.length === 0 && allowlist?.enabled === false) {
      return { mode: 'inherit', models: [], mappings: modelMappings, publicTargets: [], reason }
    }
  }

  if (
    (allowlist?.enabled === true && !isFrozen && mergedModels.length > 0) ||
    mergedModels.length > 0 ||
    modelMappings.length > 0
  ) {
    return {
      mode: 'restricted',
      models: mergedModels,
      mappings: modelMappings,
      publicTargets: publicTargets.filter(target => mergedModels.includes(target)),
      reason
    }
  }

  return {
    mode: 'inherit',
    models: [],
    mappings: [],
    publicTargets: [],
    reason
  }
}

export function buildUpdateModelAllowlistRequest(input: {
  mode: AccountModelSelectionMode
  models: string[]
  mappings?: ModelMappingEntry[]
  publicTargets?: readonly string[]
  reason?: string
  expectedRulesRevision: string | number
}): UpdateModelAllowlistRequest {
  const reason = (input.reason ?? '').trim()
  if (input.mode === 'inherit') {
    return {
      mode: 'inherit',
      enabled: false,
      models: [],
      reason,
      expected_rules_revision: input.expectedRulesRevision,
      model_mapping: buildExplicitModelMappingObject(input.mappings ?? [])
    }
  }
  if (input.mode === 'deny_all') {
    return {
      mode: 'deny_all',
      enabled: true,
      models: [],
      reason,
      confirm_deny_all: true,
      expected_rules_revision: input.expectedRulesRevision
    }
  }
  const mappings = input.mappings ?? []
  const mappingObject = buildExplicitModelMappingObject(mappings, input.publicTargets ?? [])
  const explicitPublicTargets = Object.entries(mappingObject)
    .filter(([from, to]) => from === to)
    .map(([from]) => from)
  return {
    mode: 'restricted',
    enabled: true,
    models: [
      ...new Set([...mergeIdentityMappingsIntoModels(input.models, mappings), ...explicitPublicTargets])
    ],
    reason,
    expected_rules_revision: input.expectedRulesRevision,
    model_mapping: mappingObject
  }
}

export function buildAccountWorkflowModelPayload(input: {
  mode?: AccountModelSelectionMode
  models: string[]
  mappings?: ModelMappingEntry[]
  publicTargets?: readonly string[]
  reason?: string
  clearMappingsOnInherit?: boolean
}): {
  mode: AccountModelSelectionMode
  models: string[]
  model_mapping?: Record<string, string>
  reason?: string
} {
  const reason = (input.reason ?? '').trim()
  if (input.mode === 'deny_all') {
    return {
      mode: 'deny_all',
      models: [],
      reason
    }
  }
  const hasSelection =
    input.models.some(model => model.trim().length > 0) ||
    (input.mappings ?? []).some(entry => entry.from.trim().length > 0 && entry.to.trim().length > 0)
  if (input.mode === 'restricted' || (input.mode === undefined && hasSelection)) {
    const mergedModels = mergeMappingTargetsIntoModels(input.models, input.mappings ?? [])
    const mapping = buildExplicitModelMappingObject(input.mappings ?? [], input.publicTargets ?? [])
    return {
      mode: 'restricted',
      models: mergedModels,
      ...(Object.keys(mapping).length > 0 ? { model_mapping: mapping } : {}),
      ...(reason ? { reason } : {})
    }
  }
  return {
    mode: 'inherit',
    models: [],
    ...(input.clearMappingsOnInherit ? { model_mapping: {} } : {})
  }
}

export function modelSyncWarningCode(warning: string | { code: string }): string {
  return typeof warning === 'string' ? warning : warning.code
}

export function modelSyncErrorKey(error: unknown): string {
  const reason = extractApiErrorCode(error) ?? ''
  if (reason === 'MODEL_SELECTION_DENIED') return 'modelSync.deniedError'
  if (extractApiErrorStatus(error) === 409) return 'modelSync.conflict'
  if (reason === 'MODEL_SELECTION_REASON_REQUIRED') return 'modelValidation.denyAllReasonRequired'
  if (reason === 'INVALID_MODEL_SELECTION' || reason === 'MODEL_SELECTION_CONFIRMATION_REQUIRED') {
    return 'modelSync.invalidSelection'
  }
  const reasons = ['not_synced', 'identity_changed', 'entitlement_unconfirmed', 'not_discovered', 'account_restricted', 'route_ambiguous', 'route_target_unavailable', 'endpoint_unsupported', 'retired', 'group_restricted', 'stale_evidence', 'metadata_incomplete']
  if (reasons.includes(reason)) return `modelSync.reasons.${reason}`
  const keys: Record<string, string> = {
    upstream_model_sync_configuration: 'syncUpstreamModelsConfiguration',
    upstream_model_sync_account_type: 'syncUpstreamModelsAccountType',
    upstream_model_sync_not_adapted: 'syncUpstreamModelsNotAdapted',
    upstream_model_sync_discovery_unconfirmed: 'syncUpstreamModelsDiscoveryUnconfirmed',
    upstream_model_sync_endpoint_not_found: 'syncUpstreamModelsEndpointNotFound',
    upstream_model_sync_method_not_allowed: 'syncUpstreamModelsMethodNotAllowed',
    upstream_model_sync_permission: 'syncUpstreamModelsPermission',
    upstream_model_sync_rate_limited: 'syncUpstreamModelsRateLimited',
    upstream_model_sync_network: 'syncUpstreamModelsNetwork',
    upstream_model_sync_credentials: 'syncUpstreamModelsCredentials',
    upstream_model_sync_unsupported: 'syncUpstreamModelsUnsupported',
    upstream_model_sync_upstream: 'syncUpstreamModelsUnavailable',
    upstream_model_sync_internal: 'syncUpstreamModelsFailed'
  }
  return keys[reason] || 'syncUpstreamModelsFailed'
}

/** Account filters live in protected extra, unlike group/API-key allowlists. */
export function accountModelAllowlist(
  account: { extra?: Record<string, unknown> | null } | null | undefined
): ModelAllowlist | undefined {
  const value = account?.extra?.upstream_model_allowlist
  if (!value || typeof value !== 'object') return undefined
  const filter = value as {
    enabled?: boolean
    models?: unknown
    reason?: unknown
    confirmed_by?: unknown
    confirmed_at?: unknown
    identity_revision?: unknown
  }
  return {
    enabled: filter.enabled === true,
    models: Array.isArray(filter.models) ? filter.models.filter((id): id is string => typeof id === 'string') : [],
    ...(typeof filter.reason === 'string' ? { reason: filter.reason } : {}),
    ...(typeof filter.confirmed_by === 'number' ? { confirmed_by: filter.confirmed_by } : {}),
    ...(typeof filter.confirmed_at === 'string' ? { confirmed_at: filter.confirmed_at } : {}),
    ...(typeof filter.identity_revision === 'number' ? { identity_revision: filter.identity_revision } : {})
  }
}
