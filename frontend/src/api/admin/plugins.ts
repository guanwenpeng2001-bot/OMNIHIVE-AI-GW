import { apiClient } from '../client'

export interface PluginCapability {
  id: string
  platform: string
  account_type: string
}

export interface PluginRequirements {
  sub2api: string
  recommended_sub2api_version?: string
  tested_sub2api_versions?: string[]
  plugin_protocol: number
  transport_api: number
  ui_bridge: number
}

export interface PluginManifest {
  schema_version: number
  id: string
  name: string
  version: string
  description?: string
  author?: string
  requires: PluginRequirements
  capabilities: PluginCapability[]
  ui: { entrypoint: string }
}

export interface PluginCompatibility {
  compatible: boolean
  tested: boolean
  status: 'compatible' | 'untested' | 'incompatible'
  message: string
  current_sub2api_version: string
  required_sub2api_version: string
  recommended_sub2api_version: string
  plugin_protocol: number
  transport_api: number
  ui_bridge: number
}

export interface PluginBinding {
  id: number
  plugin_id: number
  capability: string
  platform: string
  account_type: string
  enabled: boolean
  rollout_percent: number
}

export interface PluginInstallation {
  id: number
  plugin_key: string
  name: string
  version: string
  description: string
  author: string
  manifest: PluginManifest
  binary_sha256: string
  signature_status: 'trusted' | 'unsigned'
  state: 'disabled' | 'starting' | 'enabled' | 'error' | 'incompatible'
  last_error: string
  installed_at: string
  enabled_at?: string
  updated_at: string
  bindings: PluginBinding[]
  compatibility: PluginCompatibility
  runtime_healthy: boolean
  runtime_message: string
}

export interface PluginTestResult {
  success: boolean
  message: string
  latency_ms: number
}

export interface PluginUISession {
  url: string
  bridge_token: string
  ui_bridge_version: number
  expires_at: string
}

export async function list(): Promise<PluginInstallation[]> {
  const { data } = await apiClient.get<PluginInstallation[]>('/admin/plugins')
  return data
}

export async function upload(file: File): Promise<PluginInstallation> {
  const form = new FormData()
  form.append('plugin', file)
  const { data } = await apiClient.post<PluginInstallation>('/admin/plugins/upload', form, {
    headers: { 'Content-Type': 'multipart/form-data' },
    timeout: 120000
  })
  return data
}

export async function enable(
  id: number,
  rolloutPercent: number,
  acceptUntested: boolean
): Promise<PluginInstallation> {
  const { data } = await apiClient.post<PluginInstallation>(`/admin/plugins/${id}/enable`, {
    rollout_percent: rolloutPercent,
    accept_untested: acceptUntested
  })
  return data
}

export async function disable(id: number): Promise<PluginInstallation> {
  const { data } = await apiClient.post<PluginInstallation>(`/admin/plugins/${id}/disable`)
  return data
}

export async function remove(id: number): Promise<void> {
  await apiClient.delete(`/admin/plugins/${id}`)
}

export async function getConfig(id: number): Promise<Record<string, unknown>> {
  const { data } = await apiClient.get<Record<string, unknown>>(`/admin/plugins/${id}/config`)
  return data
}

export async function saveConfig(
  id: number,
  config: Record<string, unknown>
): Promise<Record<string, unknown>> {
  const { data } = await apiClient.put<Record<string, unknown>>(`/admin/plugins/${id}/config`, config)
  return data
}

export async function test(id: number): Promise<PluginTestResult> {
  const { data } = await apiClient.post<PluginTestResult>(`/admin/plugins/${id}/test`)
  return data
}

export async function createUISession(id: number): Promise<PluginUISession> {
  const { data } = await apiClient.post<PluginUISession>(`/admin/plugins/${id}/ui-session`)
  return data
}

export async function getJsonConfig(id: number): Promise<Record<string, unknown>> {
  return getConfig(id)
}

export async function saveJsonConfig(
  id: number,
  config: Record<string, unknown>
): Promise<Record<string, unknown>> {
  return saveConfig(id, config)
}

export interface GatewayExtensionModule {
  key: string
  name: string
  version: string
  author: string
  category: string
  description: string
  enabled: boolean
  rollout_percent: number
  runtime_healthy: boolean
  last_test?: PluginTestResult | null
  config: Record<string, unknown>
  updated_at?: string
}

export interface GatewayExtensionUpdate {
  enabled?: boolean
  rollout_percent?: number
  config?: Record<string, unknown>
}

const extensionDefinitions = [
  {
    key: 'prompt-guard',
    name: '关键词与提示词风控',
    category: '安全防护',
    description: '检查请求中的关键词与提示词，按策略拦截风险内容。',
    config: { mode: 'audit', redact_pii: true, max_scan_chars: 16000 }
  },
  {
    key: 'rate-limiter',
    name: '自适应智能限流',
    category: '流量治理',
    description: '按用户与 API Key 控制请求速率和突发流量。',
    config: { burst_rpm: 120, strategy: 'fair_queue', max_wait_ms: 3000 }
  },
  {
    key: 'cache-layer',
    name: '语义与前缀响应缓存',
    category: '响应加速',
    description: '复用匹配的响应，配置缓存有效期与命中策略。',
    config: { ttl_seconds: 300, max_payload_kb: 512, scope: 'key_isolated' }
  },
  {
    key: 'cost-tracker',
    name: '多维成本追踪与预算熔断',
    category: '成本管理',
    description: '按模型、用户与 API Key 追踪成本，并执行预算控制。',
    config: { daily_alert_usd: 200, spike_ratio: 2.5, circuit_breaker: false }
  }
]

const localExtensions = new Map<string, GatewayExtensionModule>(
  extensionDefinitions.map(definition => [definition.key, {
    ...definition,
    version: '1.0.0',
    author: 'OmniHive',
    enabled: false,
    rollout_percent: 100,
    runtime_healthy: false,
    last_test: null
  }])
)
const localExtensionKeys = new Set<string>()

function cloneExtension(module: GatewayExtensionModule): GatewayExtensionModule {
  return JSON.parse(JSON.stringify(module)) as GatewayExtensionModule
}

function isNotFound(error: unknown): boolean {
  if (!error || typeof error !== 'object') return false
  const response = error as { status?: number; response?: { status?: number } }
  return (response.response?.status ?? response.status) === 404
}

function localExtension(key: string): GatewayExtensionModule {
  const module = localExtensions.get(key)
  if (!module) throw new Error('扩展模块不存在，请刷新列表。')
  localExtensionKeys.add(key)
  return module
}

function validLocalConfig(key: string, config: Record<string, unknown>): boolean {
  const defaults = extensionDefinitions.find(definition => definition.key === key)?.config as Record<string, unknown> | undefined
  return !!defaults && Object.entries(config).every(([field, value]) => {
    if (!Object.prototype.hasOwnProperty.call(defaults, field) || typeof value !== typeof defaults[field]) return false
    if (typeof value === 'number') {
      return Number.isFinite(value) && value >= 0 && value <= 1e9 &&
        (['daily_alert_usd', 'spike_ratio'].includes(field) || Number.isInteger(value)) &&
        (field === 'max_wait_ms' || value > 0) && (field !== 'spike_ratio' || value >= 1)
    }
    if (typeof value === 'string') return field === 'mode' ? ['audit', 'block'].includes(value) : value === defaults[field]
    return true
  })
}

export function isExtensionLocal(key: string): boolean {
  return localExtensionKeys.has(key)
}

export async function listExtensions(): Promise<GatewayExtensionModule[]> {
  try {
    const { data } = await apiClient.get<GatewayExtensionModule[]>('/admin/plugins/extensions')
    localExtensionKeys.clear()
    return data
  } catch (error: unknown) {
    if (!isNotFound(error)) throw error
    return [...localExtensions.keys()].map(key => cloneExtension(localExtension(key)))
  }
}

export async function updateExtension(
  key: string,
  payload: GatewayExtensionUpdate
): Promise<GatewayExtensionModule> {
  if (payload.enabled !== undefined && typeof payload.enabled !== 'boolean') {
    throw new Error('扩展启用状态必须是布尔值。')
  }
  if (payload.rollout_percent !== undefined &&
      (!Number.isInteger(payload.rollout_percent) || payload.rollout_percent < 1 || payload.rollout_percent > 100)) {
    throw new Error('流量覆盖比例必须为 1–100 的整数。')
  }
  if (payload.config !== undefined &&
      (!payload.config || typeof payload.config !== 'object' || Array.isArray(payload.config))) {
    throw new Error('配置必须是 JSON 对象。')
  }
  try {
    const { data } = await apiClient.put<GatewayExtensionModule>(
      `/admin/plugins/extensions/${encodeURIComponent(key)}`, payload
    )
    localExtensionKeys.delete(key)
    return data
  } catch (error: unknown) {
    if (!isNotFound(error)) throw error
    const module = localExtension(key)
    const updated = cloneExtension({
      ...module,
      enabled: payload.enabled ?? module.enabled,
      rollout_percent: payload.rollout_percent ?? module.rollout_percent,
      config: { ...module.config, ...payload.config },
      runtime_healthy: false,
      last_test: null,
      updated_at: new Date().toISOString()
    })
    if (!validLocalConfig(key, updated.config)) throw new Error('扩展参数无效，请检查字段名称、类型与取值范围。')
    localExtensions.set(key, updated)
    return cloneExtension(updated)
  }
}

export async function testExtension(key: string): Promise<PluginTestResult> {
  try {
    const { data } = await apiClient.post<PluginTestResult>(
      `/admin/plugins/extensions/${encodeURIComponent(key)}/test`
    )
    localExtensionKeys.delete(key)
    return data
  } catch (error: unknown) {
    if (!isNotFound(error)) throw error
    const started = performance.now()
    const module = localExtension(key)
    const success = Number.isInteger(module.rollout_percent) &&
      module.rollout_percent >= 1 && module.rollout_percent <= 100 &&
      validLocalConfig(key, module.config)
    const result = {
      success,
      message: success ? '本地配置自检通过；尚未连接网关运行时' : '本地配置自检未通过',
      latency_ms: Math.round((performance.now() - started) * 100) / 100
    }
    module.last_test = result
    module.runtime_healthy = false
    return { ...result }
  }
}

export default {
  list,
  upload,
  enable,
  disable,
  remove,
  getConfig,
  saveConfig,
  test,
  createUISession,
  getJsonConfig,
  saveJsonConfig,
  listExtensions,
  updateExtension,
  testExtension,
  isExtensionLocal
}
