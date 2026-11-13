import { apiClient } from '@/api/client'
import { buildGatewayUrl } from './url'

export interface MCPGrantDTO {
  id: string
  client_id: string
  name: string
  redirect_uri: string
  scope: 'user' | 'admin'
  permission: 'read' | 'write'
  status: 'active' | 'revoked'
  created_at: string
  expires_at: string
  last_used_at: string | null
}

export interface CreateMCPGrantPayload {
  client_id?: string
  name: string
  redirect_uri: string
  scope: 'user' | 'admin'
  permission: 'read' | 'write'
  totp_code?: string
}

export interface UserAuditEventDTO {
  id: string
  action: string
  resource: string
  ip: string
  status: string
  detail: string
  created_at: string
}

export interface MCPGrantsResponse {
  endpoint: string
  role: string
  grants: MCPGrantDTO[]
}

export interface AuthorizeMCPGrantPayload extends CreateMCPGrantPayload {
  decision: 'approve' | 'deny'
  client_id: string
  resource?: string
}
export type AuthorizeMCPGrantResponse =
  | { decision: 'approved'; grant: MCPGrantDTO }
  | { decision: 'denied'; client_id: string; name: string; redirect_uri: string }
export interface OnboardingDTO {
  hidden: boolean
  steps: boolean[]
  base_url: string
  snippets: { claude_code: string; codex: string; openai_sdk: string; opencode?: string; cursor?: string }
}
export interface UpdateOnboardingPayload { hidden?: boolean; step_index?: number; completed?: boolean }
export interface RankingItemDTO {
  name: string; provider: string; metric: string; delta: string
  trend: 'up' | 'down' | 'flat'; color: string
  slug?: string; tokens?: string; latency_ms?: number; uptime?: string; sparkline?: number[]
}
export interface ModelRankingsDTO { models: RankingItemDTO[]; channels: RankingItemDTO[]; cost: RankingItemDTO[] }

export interface AdminPulseDTO {
  status: 'healthy' | 'degraded' | string
  today_requests: number
  rpm: number
  tpm: number
  p95_latency_ms: number | null
  error_rate_percent: number | null
  active_accounts: number | null
  total_accounts: number | null
  quota_health_percent?: number | null
  updated_at?: string
}

export interface RoutingPolicyConfig {
  zdr_enabled: boolean
  allow_fallback: boolean
  auto_prompt_cache: boolean
  strategy_mode: 'smart' | 'price' | 'latency' | 'throughput'
  region_preference: 'global' | 'us' | 'eu' | 'ap'
  monthly_budget_usd: number
  alert_threshold_percent: number
  provider_whitelist: string[]
  updated_at?: string
}

export interface PlaygroundChatResponse {
  id: string
  model: string
  provider: string
  reply: string
  routed_group: string
  latency_ms: number
  prompt_tokens: number
  completion_tokens: number
}

// Stored compatibility data is demo metadata, never an access credential.
interface LocalState { grants: MCPGrantDTO[]; events: UserAuditEventDTO[]; onboarding: { hidden: boolean; steps: boolean[] } }
let local: LocalState | undefined
let localSession = ''
let sequence = 0
const newID = () => `local-mcp-${Date.now()}-${++sequence}`
const fail = (status: number, message: string): never => { throw Object.assign(new Error(message), { status }) }
function identity() {
  const raw = localStorage.getItem('auth_user')
  let user: { id?: number; role?: string } = {}
  try { user = JSON.parse(raw || '{}') || {} } catch { /* Treat invalid identity as logged out. */ }
  return {
    key: `${user.id ?? ''}:${localStorage.getItem('auth_session_revision') ?? localStorage.getItem('auth_token') ?? ''}`,
    id: user.id,
    role: user.role === 'admin' ? 'admin' : 'user'
  }
}
function event(action: string, resource: string, detail: string): UserAuditEventDTO {
  return { id: newID(), action, resource, ip: '127.0.0.1', status: 'success', detail, created_at: new Date().toISOString() }
}
function grant(payload: CreateMCPGrantPayload): MCPGrantDTO {
  return {
    id: newID(), client_id: payload.client_id?.trim() || newID(), name: payload.name.trim(),
    redirect_uri: payload.redirect_uri, scope: payload.scope, permission: payload.permission,
    status: 'active', created_at: new Date().toISOString(),
    expires_at: new Date(Date.now() + 30 * 86400000).toISOString(), last_used_at: null
  }
}
function state(key: string): LocalState {
  if (!local || localSession !== key) {
    localSession = key
    local = {
      onboarding: { hidden: false, steps: [false, false, false] },
      grants: ['Claude Desktop MCP', 'Cursor MCP Bridge'].map(name => grant({ name, redirect_uri: 'http://localhost:3000/callback', scope: 'user', permission: 'read' })),
      events: ['安全设置巡检', '登录控制台', '创建 API 密钥'].map(action => event(action, 'account', '工作台安全会话巡检通过'))
    }
  }
  try {
    const saved = JSON.parse(localStorage.getItem(`mcp-onboarding:${identity().id}`) || 'null')
    if (saved && typeof saved.hidden === 'boolean' && Array.isArray(saved.steps) && saved.steps.length === 3 && saved.steps.every((v: unknown) => typeof v === 'boolean')) local.onboarding = saved
  } catch { /* Invalid or unavailable storage leaves in-memory preferences intact. */ }
  return local
}

async function compatible<T>(request: () => Promise<{ data: T }>, fallback: (s: LocalState, role: string) => T): Promise<T> {
  const session = identity()
  try {
    return (await request()).data
  } catch (error) {
    const e = error as { status?: number; response?: { status?: number } }
    const status = e.response?.status ?? e.status
    if (status !== 404 && status !== 502) throw error
    if (identity().key !== session.key) return fail(401, 'Authentication session changed')
    if (!session.id) return fail(401, 'User not authenticated')
    return fallback(state(session.key), session.role)
  }
}

function validateGrant(payload: CreateMCPGrantPayload, role: string, deny = false) {
      if (!payload.name?.trim()) return fail(400, 'name is required')
      let url: URL
      try { url = new URL(payload.redirect_uri) } catch { return fail(400, 'Invalid redirect_uri') }
      if (!/^https?:\/\//.test(payload.redirect_uri) || !['http:', 'https:'].includes(url.protocol) || !url.hostname || url.username || url.password) return fail(400, 'Invalid redirect_uri')
      if (!['user', 'admin'].includes(payload.scope)) return fail(400, 'Invalid scope')
      if (!deny && payload.scope === 'admin' && role !== 'admin') return fail(403, 'Administrator scope requires an administrator')
      if (!['read', 'write'].includes(payload.permission)) return fail(400, 'Invalid permission')
      if (!deny && payload.scope === 'admin' && payload.permission === 'write' && !/^[0-9]{6}$/.test(payload.totp_code || '')) return fail(428, 'A six-digit two-step verification code is required')
}
function onboarding(s: LocalState): OnboardingDTO {
  const origin = buildGatewayUrl('/').replace(/\/$/, '')
  const base = buildGatewayUrl('/v1')
  return { ...s.onboarding, steps: [...s.onboarding.steps], base_url: base, snippets: {
    claude_code: JSON.stringify({ env: { ANTHROPIC_BASE_URL: origin, ANTHROPIC_AUTH_TOKEN: 'YOUR_API_KEY' } }, null, 2),
    codex: `model_provider = "omnihive"\n[model_providers.omnihive]\nname = "OmniHive"\nbase_url = ${JSON.stringify(base)}\nenv_key = "OPENAI_API_KEY"\nwire_api = "responses"`,
    openai_sdk: `from openai import OpenAI\nclient = OpenAI(api_key="YOUR_API_KEY", base_url=${JSON.stringify(base)})`,
    opencode: JSON.stringify({
      $schema: 'https://opencode.ai/config.json',
      provider: {
        omnihive: {
          npm: '@ai-sdk/openai-compatible',
          name: 'OmniHive Gateway',
          options: { baseURL: base, apiKey: '{env:OMNIHIVE_API_KEY}' }
        }
      }
    }, null, 2),
    cursor: `Base URL: ${base}\nAPI Key: YOUR_API_KEY`
  } }
}
function demoRankings(): ModelRankingsDTO {
  return {
    models: [
      { name: 'claude-sonnet-4-6', provider: 'Anthropic', metric: '4.82B tokens', delta: '+18.4%', trend: 'up', color: '#d97706', sparkline: [42, 48, 51, 59, 64, 72, 81] },
      { name: 'gpt-5.4', provider: 'OpenAI', metric: '3.96B tokens', delta: '+14.2%', trend: 'up', color: '#10b981', sparkline: [38, 41, 45, 49, 56, 61, 68] },
      { name: 'gemini-3.1-pro', provider: 'Google', metric: '2.74B tokens', delta: '+22.6%', trend: 'up', color: '#3b82f6', sparkline: [24, 29, 35, 42, 49, 58, 66] },
      { name: 'deepseek-r1', provider: 'DeepSeek', metric: '2.18B tokens', delta: '+9.5%', trend: 'up', color: '#6366f1', sparkline: [31, 34, 33, 39, 44, 47, 52] }
    ],
    channels: [
      { name: 'Anthropic Direct Pool', provider: 'Anthropic', metric: '99.98% · 168ms P95', delta: '+0.04%', trend: 'up', color: '#10b981', sparkline: [99, 100, 100, 99, 100, 100, 100] },
      { name: 'OpenAI Tier-5 Cluster', provider: 'OpenAI', metric: '99.95% · 142ms P95', delta: '+0.02%', trend: 'up', color: '#10b981', sparkline: [98, 99, 100, 100, 99, 100, 100] },
      { name: 'Vertex AI Global', provider: 'Google', metric: '99.92% · 195ms P95', delta: '0.00%', trend: 'flat', color: '#3b82f6', sparkline: [99, 99, 99, 100, 99, 99, 100] },
      { name: 'DeepSeek High-Concurrency', provider: 'DeepSeek', metric: '99.89% · 210ms P95', delta: '+0.11%', trend: 'up', color: '#f59e0b', sparkline: [97, 98, 99, 99, 99, 100, 100] }
    ],
    cost: [
      { name: 'deepseek-v3.2', provider: 'DeepSeek', metric: '$0.14 / 1M input', delta: '-12%', trend: 'down', color: '#10b981', sparkline: [22, 20, 19, 18, 16, 15, 14] },
      { name: 'gemini-3-flash', provider: 'Google', metric: '$0.15 / 1M input', delta: '-8%', trend: 'down', color: '#3b82f6', sparkline: [20, 19, 18, 17, 16, 15, 15] },
      { name: 'qwen3-coder-plus', provider: 'Qwen', metric: '$0.28 / 1M input', delta: '-5%', trend: 'down', color: '#f59e0b', sparkline: [32, 31, 30, 29, 29, 28, 28] },
      { name: 'gpt-5.4-mini', provider: 'OpenAI', metric: '$0.30 / 1M input', delta: '0%', trend: 'flat', color: '#6366f1', sparkline: [30, 30, 30, 30, 30, 30, 30] }
    ]
  }
}

export const mcpAPI = {
  async getRoutingPolicies(): Promise<RoutingPolicyConfig> {
    return (await apiClient.get<RoutingPolicyConfig>('/user/routing-policies')).data
  },
  async updateRoutingPolicies(payload: Partial<RoutingPolicyConfig>): Promise<RoutingPolicyConfig> {
    const session = identity().key
    const current = await mcpAPI.getRoutingPolicies()
    if (identity().key !== session) return fail(401, 'Authentication session changed')
    return (await apiClient.put<RoutingPolicyConfig>('/user/routing-policies', { ...current, ...payload })).data
  },
  async sendPlaygroundChat(payload: { model: string; message: string; strategy?: string }): Promise<PlaygroundChatResponse> {
    return (await apiClient.post<PlaygroundChatResponse>('/public/playground/chat', payload)).data
  },
  authorizeGrant(payload: AuthorizeMCPGrantPayload): Promise<AuthorizeMCPGrantResponse> {
    return compatible(() => apiClient.post<AuthorizeMCPGrantResponse>('/user/mcp/authorize', payload), (s, role) => {
      if (!['approve', 'deny'].includes(payload.decision)) return fail(400, 'Invalid decision')
      if (!payload.client_id?.trim()) return fail(400, 'client_id is required')
      validateGrant(payload, role, payload.decision === 'deny')
      if (payload.resource) validateGrant({ ...payload, redirect_uri: payload.resource }, role, payload.decision === 'deny')
      if (payload.decision === 'deny') {
        s.events.unshift({ ...event('mcp.grant.denied', payload.client_id, payload.name.trim()), status: 'denied' })
        return { decision: 'denied', client_id: payload.client_id, name: payload.name.trim(), redirect_uri: payload.redirect_uri }
      }
      const created = grant(payload)
      s.grants.push(created)
      s.events.unshift(event('mcp.grant.created', created.id, created.name))
      return { decision: 'approved', grant: { ...created } }
    })
  },
  getOnboarding(): Promise<OnboardingDTO> {
    return compatible(() => apiClient.get<OnboardingDTO>('/user/onboarding'), s => onboarding(s))
  },
  updateOnboarding(payload: UpdateOnboardingPayload): Promise<OnboardingDTO> {
    return compatible(() => apiClient.put<OnboardingDTO>('/user/onboarding', payload), s => {
      const { hidden, step_index: i, completed } = payload
      if (hidden !== undefined && typeof hidden !== 'boolean') return fail(400, 'Invalid hidden')
      if ((i === undefined) !== (completed === undefined) || (i !== undefined && (!Number.isInteger(i) || i < 0 || i > 2 || typeof completed !== 'boolean'))) return fail(400, 'Invalid step update')
      if (hidden !== undefined) s.onboarding.hidden = hidden
      if (i !== undefined && completed !== undefined) {
        if (completed && !s.onboarding.steps[i]) s.events.unshift(event('onboarding.step.completed', `onboarding.step.${i}`, `Completed onboarding step ${i + 1}`))
        s.onboarding.steps[i] = completed
      }
      try { localStorage.setItem(`mcp-onboarding:${identity().id}`, JSON.stringify(s.onboarding)) } catch { /* Memory fallback when storage is unavailable. */ }
      return onboarding(s)
    })
  },
  async getModelRankings(): Promise<ModelRankingsDTO> {
    try { return (await apiClient.get<ModelRankingsDTO>('/model-plaza/rankings')).data }
    catch (error) {
      const e = error as { status?: number; response?: { status?: number } }
      if (![404, 502].includes(e.response?.status ?? e.status ?? 0)) throw error
      return demoRankings()
    }
  },
  async getAdminPulse(): Promise<AdminPulseDTO | null> {
    try {
      return (await apiClient.get<AdminPulseDTO>('/admin/pulse')).data
    } catch {
      return null
    }
  },
  listGrants(): Promise<MCPGrantsResponse> {
    return compatible(() => apiClient.get<MCPGrantsResponse>('/user/mcp/grants'), (s, role) => ({
      endpoint: '/mcp', role, grants: s.grants.map(g => ({ ...g }))
    }))
  },
  createGrant(payload: CreateMCPGrantPayload): Promise<MCPGrantDTO> {
    return compatible(() => apiClient.post<MCPGrantDTO>('/user/mcp/grants', payload), (s, role) => {
      validateGrant(payload, role)
      const created = grant(payload)
      s.grants.push(created)
      s.events.unshift(event('mcp.grant.created', created.id, created.name))
      return { ...created }
    })
  },
  revokeGrant(id: string): Promise<MCPGrantDTO> {
    return compatible(() => apiClient.delete<MCPGrantDTO>(`/user/mcp/grants/${encodeURIComponent(id)}`), s => {
      const found = s.grants.find(g => g.id === id)
      if (!found) return fail(404, 'Grant not found')
      if (found.status !== 'revoked') {
        found.status = 'revoked'
        s.events.unshift(event('mcp.grant.revoked', found.id, found.name))
      }
      return { ...found }
    })
  },
  listUserAuditLogs(): Promise<UserAuditEventDTO[]> {
    return compatible(() => apiClient.get<UserAuditEventDTO[]>('/user/audit-logs'), s => s.events.map(e => ({ ...e })))
  }
}

export default mcpAPI
