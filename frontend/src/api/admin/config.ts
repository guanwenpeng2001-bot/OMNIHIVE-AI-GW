import { apiClient } from '../client'
import type { SystemSettings } from './settings'

export type ConfigSource =
  | 'database'
  | 'environment'
  | 'yaml'
  | 'default'
  | 'derived'
  | 'artifact'

export type ConfigAuthority =
  | 'deployment'
  | 'database_override'
  | 'database_only'
  | 'versioned_artifact'

export type ConfigState =
  | 'effective'
  | 'invalid'
  | 'unavailable'
  | 'blocked_by_hard_gate'

export type ConfigValueType =
  | 'bool'
  | 'int'
  | 'float'
  | 'string'
  | 'string_list'
  | 'json'
  | 'object'

export type ConfigInstallSeedSource =
  | 'install_seed'
  | 'environment'
  | 'yaml'
  | 'default'
  | 'admin'
  | 'unknown'

export type KnownConfigErrorCode =
  | 'CONFIG_INVALID'
  | 'CONFIG_UNAVAILABLE'
  | 'TURNSTILE_REQUIRED_NOT_CONFIGURED'

export type ConfigErrorCode = KnownConfigErrorCode | string

export type ConfigResetErrorReason =
  | 'CONFIG_RESET_REQUEST_INVALID'
  | 'CONFIG_KEYS_REQUIRED'
  | 'CONFIG_KEY_UNKNOWN'
  | 'CONFIG_KEY_NOT_RESETTABLE'
  | 'CONFIG_RESET_UNAVAILABLE'
  | 'CONFIG_FILTER_INVALID'
  | 'CONFIG_LIMIT_INVALID'

export interface EffectiveConfigItem {
  key: string
  group: string
  value_type: ConfigValueType
  value: unknown
  source: ConfigSource
  authority: ConfigAuthority
  fallback_chain: ConfigSource[]
  restart_required: boolean
  resettable: boolean
  sensitive: boolean
  state: ConfigState
  configured: boolean | null
  blocked_by?: string
  install_seed_source?: string
  baseline?: unknown
  error_code?: ConfigErrorCode
}

export interface EffectiveConfigResponse {
  config_file: string
  generated_at: string
  items: EffectiveConfigItem[]
  next_cursor?: string
}

export interface EffectiveConfigQuery {
  group?: string
  q?: string
  cursor?: string
  limit?: number
  source?: ConfigSource | ''
  state?: ConfigState | ''
}

export interface ResetConfigRequest {
  keys: string[]
}

export interface ResetConfigResponse {
  settings: SystemSettings
  items: EffectiveConfigItem[]
  generated_at: string
}

export const DEFAULT_EFFECTIVE_CONFIG_LIMIT = 50
export const MAX_EFFECTIVE_CONFIG_LIMIT = 200

/**
 * Defense-in-depth sanitizer ensuring sensitive entries never retain plaintext
 * `value` or `baseline` in client memory even if a malformed payload arrives.
 */
export function sanitizeEffectiveConfigItem(item: EffectiveConfigItem): EffectiveConfigItem {
  if (!item.sensitive) {
    return {
      ...item,
      fallback_chain: Array.isArray(item.fallback_chain) ? [...item.fallback_chain] : []
    }
  }
  const configured = typeof item.configured === 'boolean' ? item.configured : null
  const sanitized: EffectiveConfigItem = {
    ...item,
    value: null,
    configured,
    fallback_chain: Array.isArray(item.fallback_chain) ? [...item.fallback_chain] : []
  }
  delete sanitized.baseline
  return sanitized
}

function clampLimit(limit?: number): number {
  if (typeof limit !== 'number' || !Number.isFinite(limit)) {
    return DEFAULT_EFFECTIVE_CONFIG_LIMIT
  }
  return Math.min(MAX_EFFECTIVE_CONFIG_LIMIT, Math.max(1, Math.trunc(limit)))
}

export async function getEffectiveConfig(
  query: EffectiveConfigQuery = {}
): Promise<EffectiveConfigResponse> {
  const params: Record<string, string | number> = {
    limit: clampLimit(query.limit)
  }
  if (query.group && query.group.trim()) {
    params.group = query.group.trim()
  }
  if (query.q && query.q.trim()) {
    params.q = query.q.trim()
  }
  if (query.cursor !== undefined && query.cursor !== '') {
    // Pass next_cursor verbatim
    params.cursor = query.cursor
  }
  if (query.source) {
    params.source = query.source
  }
  if (query.state) {
    params.state = query.state
  }

  const { data } = await apiClient.get<EffectiveConfigResponse>('/admin/config/effective', {
    params
  })
  return {
    config_file: data?.config_file ?? '',
    generated_at: data?.generated_at ?? '',
    items: Array.isArray(data?.items) ? data.items.map(sanitizeEffectiveConfigItem) : [],
    next_cursor: data?.next_cursor || undefined
  }
}

export async function resetConfigOverrides(keys: string[]): Promise<ResetConfigResponse> {
  const normalizedKeys = Array.from(
    new Set(keys.map((key) => key.trim()).filter((key) => key.length > 0))
  )
  const payload: ResetConfigRequest = { keys: normalizedKeys }
  const { data } = await apiClient.post<ResetConfigResponse>('/admin/config/reset', payload)
  return {
    settings: (data?.settings ?? {}) as SystemSettings,
    items: Array.isArray(data?.items) ? data.items.map(sanitizeEffectiveConfigItem) : [],
    generated_at: data?.generated_at ?? ''
  }
}

export const configAPI = {
  getEffectiveConfig,
  resetConfigOverrides
}

export default configAPI
