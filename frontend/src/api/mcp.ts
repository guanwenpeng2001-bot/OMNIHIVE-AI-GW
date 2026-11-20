import { apiClient } from '@/api/client'

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

export interface MCPUnavailableState<T = never> {
  items: T[]
  state: 'unavailable'
  error: string
  status?: number
}

export class MCPApiUnavailableError extends Error {
  readonly status?: number
  readonly state = 'unavailable' as const
  readonly code?: string
  readonly response?: { status?: number; data?: unknown }
  readonly cause?: unknown

  constructor(
    message: string,
    options: {
      status?: number
      code?: string
      response?: { status?: number; data?: unknown }
      cause?: unknown
    } = {}
  ) {
    super(message)
    this.name = 'MCPApiUnavailableError'
    this.status = options.status
    this.code = options.code
    this.response = options.response
    if (options.cause !== undefined) {
      this.cause = options.cause
    }
  }
}

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

function rethrowHonestMcpError(error: unknown, endpointLabel: string): never {
  const errObj = (error && typeof error === 'object' ? error : {}) as {
    status?: number
    code?: string
    message?: string
    response?: { status?: number; data?: { message?: string } | unknown }
  }
  const status = errObj.response?.status ?? errObj.status
  const isUnavailableStatus =
    status === undefined || status === 404 || status === 502 || status === 503 || status === 504
  if (isUnavailableStatus) {
    const responseData = errObj.response?.data as { message?: string } | undefined
    const rawMessage =
      (typeof responseData?.message === 'string' && responseData.message.trim()) ||
      (typeof errObj.message === 'string' && errObj.message.trim()) ||
      `${endpointLabel} is currently unavailable`
    throw new MCPApiUnavailableError(rawMessage, {
      status,
      code: errObj.code ?? 'SERVICE_UNAVAILABLE',
      response: errObj.response,
      cause: error
    })
  }
  throw error
}

async function requestHonest<T>(request: () => Promise<{ data: T }>, endpointLabel: string): Promise<T> {
  try {
    return (await request()).data
  } catch (error) {
    return rethrowHonestMcpError(error, endpointLabel)
  }
}

export const mcpAPI = {
  async getRoutingPolicies(): Promise<RoutingPolicyConfig> {
    return requestHonest(
      () => apiClient.get<RoutingPolicyConfig>('/user/routing-policies'),
      'Routing policy endpoint'
    )
  },
  async updateRoutingPolicies(payload: Partial<RoutingPolicyConfig>): Promise<RoutingPolicyConfig> {
    const session = identity().key
    const current = await mcpAPI.getRoutingPolicies()
    if (identity().key !== session) return fail(401, 'Authentication session changed')
    return requestHonest(
      () => apiClient.put<RoutingPolicyConfig>('/user/routing-policies', { ...current, ...payload }),
      'Routing policy endpoint'
    )
  },
  async sendPlaygroundChat(payload: { model: string; message: string; strategy?: string }): Promise<PlaygroundChatResponse> {
    return requestHonest(
      () => apiClient.post<PlaygroundChatResponse>('/public/playground/chat', payload),
      'Playground chat endpoint'
    )
  },
  authorizeGrant(payload: AuthorizeMCPGrantPayload): Promise<AuthorizeMCPGrantResponse> {
    return requestHonest(
      () => apiClient.post<AuthorizeMCPGrantResponse>('/user/mcp/authorize', payload),
      'MCP authorization endpoint'
    )
  },
  getOnboarding(): Promise<OnboardingDTO> {
    return requestHonest(
      () => apiClient.get<OnboardingDTO>('/user/onboarding'),
      'User onboarding endpoint'
    )
  },
  updateOnboarding(payload: UpdateOnboardingPayload): Promise<OnboardingDTO> {
    return requestHonest(
      () => apiClient.put<OnboardingDTO>('/user/onboarding', payload),
      'User onboarding endpoint'
    )
  },
  async getModelRankings(): Promise<ModelRankingsDTO> {
    return requestHonest(
      () => apiClient.get<ModelRankingsDTO>('/model-plaza/rankings'),
      'Model rankings endpoint'
    )
  },
  async getAdminPulse(): Promise<AdminPulseDTO | null> {
    try {
      const data = (await apiClient.get<AdminPulseDTO>('/admin/pulse')).data
      return data && typeof data === 'object' ? data : null
    } catch {
      return null
    }
  },
  listGrants(): Promise<MCPGrantsResponse> {
    return requestHonest(
      () => apiClient.get<MCPGrantsResponse>('/user/mcp/grants'),
      'MCP grants endpoint'
    )
  },
  createGrant(payload: CreateMCPGrantPayload): Promise<MCPGrantDTO> {
    return requestHonest(
      () => apiClient.post<MCPGrantDTO>('/user/mcp/grants', payload),
      'MCP grants endpoint'
    )
  },
  revokeGrant(id: string): Promise<MCPGrantDTO> {
    return requestHonest(
      () => apiClient.delete<MCPGrantDTO>(`/user/mcp/grants/${encodeURIComponent(id)}`),
      'MCP grants endpoint'
    )
  },
  async listUserAuditLogs(options?: { signal?: AbortSignal }): Promise<UserAuditEventDTO[]> {
    const data = await requestHonest(
      () =>
        options?.signal
          ? apiClient.get<UserAuditEventDTO[]>('/user/audit-logs', { signal: options.signal })
          : apiClient.get<UserAuditEventDTO[]>('/user/audit-logs'),
      'User security audit endpoint'
    )
    if (!Array.isArray(data)) {
      throw new MCPApiUnavailableError('User security audit response is invalid', {
        status: 502,
        code: 'INVALID_RESPONSE'
      })
    }
    return data
  }
}

export default mcpAPI
