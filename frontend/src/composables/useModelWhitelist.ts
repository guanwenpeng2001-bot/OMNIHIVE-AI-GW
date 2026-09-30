import { extractApiErrorCode, extractApiErrorStatus } from '@/utils/apiError'
import { selectPlatformModelPresets } from './usePlatformModelPresets'
import type { PlatformModelPreset } from '@/api/admin/platformModelPresets'

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
    (preset.models ?? []).map(model => model.id)))]
}

export function getPresetMappingsByPlatform(platform: string, identity: ModelCandidateIdentity = {}, presets: PlatformModelPreset[] = []) {
  const models = selectPlatformModelPresets(presets, [platform], identity).flatMap(preset => preset.models ?? [])
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
  code: 'exactIdRequired' | 'idsRequired' | 'invalidWildcard' | 'conflictingTarget'
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
  mode: ModelRestrictionMode,
  allowedModels: string[],
  modelMappings: ModelMappingEntry[]
): ModelRestrictionError[] {
  const errors: ModelRestrictionError[] = []
  const targets = new Map<string, string>()
  if (mode !== 'mapping') {
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


export function modelSyncWarningCode(warning: string | { code: string }): string {
  return typeof warning === 'string' ? warning : warning.code
}

export function modelSyncErrorKey(error: unknown): string {
  if (extractApiErrorStatus(error) === 409) return 'modelSync.conflict'
  const reason = extractApiErrorCode(error) ?? ''
  const reasons = ['not_synced', 'identity_changed', 'entitlement_unconfirmed', 'not_discovered', 'account_restricted', 'route_ambiguous', 'route_target_unavailable', 'endpoint_unsupported', 'retired', 'group_restricted', 'stale_evidence', 'metadata_incomplete']
  if (reasons.includes(reason)) return `modelSync.reasons.${reason}`
  const keys: Record<string, string> = {
    upstream_model_sync_configuration: 'syncUpstreamModelsConfiguration',
    upstream_model_sync_credentials: 'syncUpstreamModelsCredentials',
    upstream_model_sync_unsupported: 'syncUpstreamModelsUnsupported',
    upstream_model_sync_upstream: 'syncUpstreamModelsUnavailable',
    upstream_model_sync_internal: 'syncUpstreamModelsFailed'
  }
  return keys[reason] || 'syncUpstreamModelsFailed'
}

/** Account filters live in protected extra, unlike group/API-key allowlists. */
export function accountModelAllowlist(account: { extra?: Record<string, unknown> | null } | null | undefined) {
  const value = account?.extra?.upstream_model_allowlist
  if (!value || typeof value !== 'object') return undefined
  const filter = value as { enabled?: boolean; models?: unknown }
  return { enabled: filter.enabled === true, models: Array.isArray(filter.models) ? filter.models.filter((id): id is string => typeof id === 'string') : [] }
}
