import type {
  AsyncErrorContext,
  CheckMixedChannelLike,
  ConnectionField,
  ConnectionResource,
  ConnectionSchema,
  ConnectionVerification,
  CredentialOperationMode,
  MixedChannelDetails,
  TempUnschedRuleInput,
  TempUnschedRulePayload
} from './types'
import { normalizeApiError } from '@/utils/apiError'

export function parsePoolModeRetryStatusCodes(input: string): number[] {
  if (!input || !input.trim()) return []
  const seen = new Set<number>()
  const out: number[] = []
  for (const token of input.split(/[,\s]+/)) {
    const trimmed = token.trim()
    if (!trimmed) continue
    const parsed = Number(trimmed)
    if (!Number.isFinite(parsed) || !Number.isInteger(parsed)) continue
    if (parsed < 100 || parsed > 599) continue
    if (seen.has(parsed)) continue
    seen.add(parsed)
    out.push(parsed)
  }
  return out.sort((left, right) => left - right)
}

export function formatPoolModeRetryStatusCodes(value: unknown): string {
  if (!Array.isArray(value)) return ''
  const out: number[] = []
  const seen = new Set<number>()
  for (const entry of value) {
    const parsed = typeof entry === 'string' ? Number(entry.trim()) : Number(entry)
    if (!Number.isFinite(parsed) || !Number.isInteger(parsed)) continue
    if (parsed < 100 || parsed > 599) continue
    if (seen.has(parsed)) continue
    seen.add(parsed)
    out.push(parsed)
  }
  return out.sort((left, right) => left - right).join(', ')
}

export type ErrorCodeValidationResult =
  | { ok: true; requiresConfirm?: 429 | 529 }
  | { ok: false; reason: 'invalid' | 'duplicate' }

export function validateCustomErrorCode(
  code: number | null,
  existing: readonly number[]
): ErrorCodeValidationResult {
  if (code === null || !Number.isFinite(code) || !Number.isInteger(code) || code < 100 || code > 599) {
    return { ok: false, reason: 'invalid' }
  }
  if (existing.includes(code)) {
    return { ok: false, reason: 'duplicate' }
  }
  if (code === 429 || code === 529) {
    return { ok: true, requiresConfirm: code }
  }
  return { ok: true }
}

export function toggleErrorCodeInList(codes: readonly number[], code: number): number[] {
  const index = codes.indexOf(code)
  if (index === -1) {
    return [...codes, code]
  }
  return [...codes.slice(0, index), ...codes.slice(index + 1)]
}

export function splitTempUnschedKeywords(value: string): string[] {
  return value
    .split(/[,;]/)
    .map((item) => item.trim())
    .filter((item) => item.length > 0)
}

export function buildTempUnschedRules(rules: readonly TempUnschedRuleInput[]): TempUnschedRulePayload[] {
  const out: TempUnschedRulePayload[] = []
  for (const rule of rules) {
    const errorCode = Number(rule.error_code)
    const duration = Number(rule.duration_minutes)
    const keywords = splitTempUnschedKeywords(rule.keywords)
    if (!Number.isFinite(errorCode) || errorCode < 100 || errorCode > 599) {
      continue
    }
    if (!Number.isFinite(duration) || duration <= 0) {
      continue
    }
    if (keywords.length === 0) {
      continue
    }
    out.push({
      error_code: Math.trunc(errorCode),
      keywords,
      duration_minutes: Math.trunc(duration),
      description: rule.description.trim()
    })
  }
  return out
}

export function buildMixedChannelDetails(resp?: CheckMixedChannelLike): MixedChannelDetails | null {
  const details = resp?.details
  if (!details) {
    return null
  }
  return {
    groupName: details.group_name || 'Unknown',
    currentPlatform: details.current_platform || 'Unknown',
    otherPlatform: details.other_platform || 'Unknown'
  }
}

export interface ProviderDisplayMeta {
  id: string
  label: string
  protocolSummary: string
  defaultEndpoint: string
}

const PROVIDER_META: Record<string, ProviderDisplayMeta> = {
  openai: {
    id: 'openai',
    label: 'OpenAI',
    protocolSummary: 'Responses · Chat Completions',
    defaultEndpoint: 'https://api.openai.com'
  },
  anthropic: {
    id: 'anthropic',
    label: 'Anthropic',
    protocolSummary: 'Messages API',
    defaultEndpoint: 'https://api.anthropic.com'
  },
  gemini: {
    id: 'gemini',
    label: 'Google Gemini',
    protocolSummary: 'GenerateContent · Vertex AI',
    defaultEndpoint: 'https://generativelanguage.googleapis.com'
  },
  deepseek: {
    id: 'deepseek',
    label: 'DeepSeek',
    protocolSummary: 'OpenAI-Compatible · Anthropic-Compatible',
    defaultEndpoint: 'https://api.deepseek.com'
  },
  azure: {
    id: 'azure',
    label: 'Azure OpenAI',
    protocolSummary: 'Azure Deployments',
    defaultEndpoint: ''
  },
  bedrock: {
    id: 'bedrock',
    label: 'AWS Bedrock',
    protocolSummary: 'Converse · InvokeModel',
    defaultEndpoint: ''
  },
  grok: {
    id: 'grok',
    label: 'xAI Grok',
    protocolSummary: 'OpenAI-Compatible',
    defaultEndpoint: 'https://api.x.ai'
  },
  compatible: {
    id: 'compatible',
    label: 'OpenAI / Claude Compatible',
    protocolSummary: 'Custom Gateway Endpoint',
    defaultEndpoint: ''
  }
}

export function getProviderDisplayMeta(provider: string): ProviderDisplayMeta {
  const key = provider.trim().toLowerCase()
  if (PROVIDER_META[key]) {
    return PROVIDER_META[key]
  }
  return {
    id: key,
    label: provider.toUpperCase(),
    protocolSummary: 'Custom Schema',
    defaultEndpoint: ''
  }
}

export function getAuthModeLabel(authType: string): string {
  const normalized = authType.trim().toLowerCase()
  if (normalized === 'api_key') return 'API Key'
  if (normalized === 'oauth') return 'OAuth 2.0'
  if (normalized === 'session_token' || normalized === 'setup-token') return 'Session Token'
  if (normalized === 'service_account') return 'Service Account'
  return authType
}

export function resolveSchemaFields(schema: ConnectionSchema | null | undefined): ConnectionField[] {
  if (!schema || !Array.isArray(schema.fields)) return []
  return schema.fields
}

export function isFieldRequired(schema: ConnectionSchema | null | undefined, path: string): boolean {
  return resolveSchemaFields(schema).some((field) => field.path === path && field.required)
}

export function isOAuthWorkflowSupported(schema: ConnectionSchema | null | undefined, authType: string): boolean {
  if (!schema) return false
  if (authType.trim().toLowerCase() === 'oauth') return true
  const workflows = schema.authorization_workflows
  return Boolean(workflows && Object.keys(workflows).length > 0 && workflows[authType])
}

export function isVerificationFresh(
  verification: ConnectionVerification | null | undefined,
  nowMs: number = Date.now()
): boolean {
  if (!verification || !verification.expires_at) return false
  const expiresMs = Date.parse(verification.expires_at)
  if (!Number.isFinite(expiresMs)) return false
  return expiresMs > nowMs
}

export function isConnectionRedacted(result: unknown): result is { version: string; redactions: string[] } {
  if (!result || typeof result !== 'object') return false
  const record = result as Record<string, unknown>
  return Array.isArray(record.redactions) && !('id' in record)
}

export function isConnectionResource(result: unknown): result is ConnectionResource {
  if (!result || typeof result !== 'object') return false
  const record = result as Record<string, unknown>
  return typeof record.id === 'string' && Array.isArray(record.redactions)
}

export function canPerformConnectionAction(
  connection: Pick<ConnectionResource, 'allowed_actions'> | null | undefined,
  action: string
): boolean {
  if (!connection || !Array.isArray(connection.allowed_actions)) return false
  return connection.allowed_actions.includes(action)
}

export function credentialOperationRequiresSecret(mode: CredentialOperationMode): boolean {
  return mode === 'set'
}

export function validateConnectionEndpoint(endpoint: string, required: boolean): string | null {
  const trimmed = endpoint.trim()
  if (!trimmed) {
    return required ? 'Endpoint URL is required by provider schema.' : null
  }
  try {
    const parsed = new URL(trimmed)
    if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') {
      return 'Endpoint must use https:// or http://'
    }
    return null
  } catch {
    return 'Enter a valid URL (for example https://api.openai.com)'
  }
}

export function mapApiErrorToAsyncContext(error: unknown, fallbackMessage = 'Request failed'): AsyncErrorContext {
  const normalized = normalizeApiError(error)
  const requestId =
    typeof normalized.metadata?.request_id === 'string'
      ? normalized.metadata.request_id
      : typeof normalized.metadata?.requestId === 'string'
        ? normalized.metadata.requestId
        : undefined
  const serverVersion =
    typeof normalized.metadata?.version === 'string' ? normalized.metadata.version : undefined
  const requiredRole =
    typeof normalized.metadata?.required_role === 'string'
      ? normalized.metadata.required_role
      : 'Workspace Admin'
  const statusUrl =
    typeof normalized.metadata?.status_url === 'string' ? normalized.metadata.status_url : undefined

  if (normalized.status === 403) {
    return {
      state: 'permission_denied',
      status: 403,
      reason: normalized.reason,
      message: normalized.message || fallbackMessage,
      requestId,
      requiredRole
    }
  }
  if (normalized.status === 409) {
    return {
      state: 'version_conflict',
      status: 409,
      reason: normalized.reason,
      message: normalized.message || 'Resource version changed on the server.',
      requestId,
      serverVersion
    }
  }
  if (normalized.status === 504 || normalized.reason === 'TIMEOUT' || normalized.reason === 'OUTCOME_UNKNOWN') {
    return {
      state: 'unknown_outcome',
      status: normalized.status || 504,
      reason: normalized.reason,
      message: normalized.message || 'Operation outcome is pending verification.',
      requestId,
      statusUrl
    }
  }
  if (normalized.reason === 'STALE_VERIFICATION' || normalized.reason === 'STALE_PREVIEW') {
    return {
      state: 'stale',
      status: normalized.status || 412,
      reason: normalized.reason,
      message: normalized.message || 'Evidence is stale and must be refreshed.',
      requestId
    }
  }
  return {
    state: 'partial_failure',
    status: normalized.status,
    reason: normalized.reason,
    message: normalized.message || fallbackMessage,
    requestId
  }
}
