import type {
  AsyncErrorContext,
  CheckMixedChannelLike,
  ConnectionField,
  ConnectionResource,
  ConnectionSchema,
  ConnectionVerification,
  CredentialOperationMode,
  HeaderOverrideDraftRow,
  MixedChannelDetails,
  TempUnschedRuleInput,
  TempUnschedRulePayload,
  ValidationFieldIssue
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
  kimi: {
    id: 'kimi',
    label: 'Moonshot Kimi',
    protocolSummary: 'OpenAI-Compatible · Anthropic-Compatible',
    defaultEndpoint: 'https://api.moonshot.cn'
  },
  zhipu: {
    id: 'zhipu',
    label: 'Zhipu GLM',
    protocolSummary: 'OpenAI-Compatible',
    defaultEndpoint: 'https://open.bigmodel.cn'
  },
  minimax: {
    id: 'minimax',
    label: 'MiniMax',
    protocolSummary: 'OpenAI-Compatible · Anthropic-Compatible',
    defaultEndpoint: 'https://api.minimax.chat'
  },
  qianwen: {
    id: 'qianwen',
    label: 'Alibaba Qwen',
    protocolSummary: 'DashScope · OpenAI-Compatible',
    defaultEndpoint: 'https://dashscope.aliyuncs.com/compatible-mode'
  },
  openrouter: {
    id: 'openrouter',
    label: 'OpenRouter',
    protocolSummary: 'OpenAI-Compatible',
    defaultEndpoint: 'https://openrouter.ai/api'
  },
  mistral: {
    id: 'mistral',
    label: 'Mistral AI',
    protocolSummary: 'OpenAI-Compatible',
    defaultEndpoint: 'https://api.mistral.ai'
  },
  volcengine: {
    id: 'volcengine',
    label: 'Volcengine Ark',
    protocolSummary: 'OpenAI-Compatible',
    defaultEndpoint: 'https://ark.cn-beijing.volces.com/api/v3'
  },
  qianfan: {
    id: 'qianfan',
    label: 'Baidu Qianfan',
    protocolSummary: 'OpenAI-Compatible',
    defaultEndpoint: 'https://qianfan.baidubce.com/v2'
  },
  hunyuan: {
    id: 'hunyuan',
    label: 'Tencent Hunyuan',
    protocolSummary: 'OpenAI-Compatible',
    defaultEndpoint: 'https://api.hunyuan.cloud.tencent.com'
  },
  antigravity: {
    id: 'antigravity',
    label: 'Antigravity',
    protocolSummary: 'Managed Upstream Protocol',
    defaultEndpoint: ''
  },
  cursor: {
    id: 'cursor',
    label: 'Cursor',
    protocolSummary: 'OAuth · Relay Protocol',
    defaultEndpoint: ''
  },
  opencode_go: {
    id: 'opencode_go',
    label: 'OpenCode',
    protocolSummary: 'OpenAI-Compatible',
    defaultEndpoint: ''
  },
  commandcode: {
    id: 'commandcode',
    label: 'Command Code',
    protocolSummary: 'OpenAI-Compatible',
    defaultEndpoint: ''
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

export function getAuthModeLabel(authType: string, isZh = false): string {
  const normalized = authType.trim().toLowerCase()
  if (normalized === 'api_key') return 'API Key'
  if (normalized === 'oauth') return 'OAuth 2.0'
  if (normalized === 'session_token' || normalized === 'setup-token') {
    return isZh ? '会话令牌 (Setup Token)' : 'Session Token'
  }
  if (normalized === 'service_account') {
    return isZh ? '服务账号 (Service Account)' : 'Service Account'
  }
  if (normalized === 'bedrock') return 'Amazon Bedrock'
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

const ENDPOINT_SUFFIX_PATTERNS: readonly RegExp[] = [
  /\/v1\/chat\/completions\/?$/i,
  /\/chat\/completions\/?$/i,
  /\/v1\/responses\/?$/i,
  /\/responses\/?$/i,
  /\/v1\/messages\/?$/i,
  /\/messages\/?$/i,
  /\/v1\/embeddings\/?$/i,
  /\/embeddings\/?$/i
]

export function normalizeConnectionEndpointInput(endpoint: string): {
  normalized: string
  strippedSuffix?: string
} {
  let cleaned = endpoint.trim()
  if (!cleaned) {
    return { normalized: '' }
  }
  let strippedSuffix: string | undefined
  for (const pattern of ENDPOINT_SUFFIX_PATTERNS) {
    const match = cleaned.match(pattern)
    if (match && match[0]) {
      strippedSuffix = match[0].replace(/\/+$/, '')
      cleaned = cleaned.slice(0, cleaned.length - match[0].length)
      break
    }
  }
  cleaned = cleaned.replace(/\/+$/, '')
  return { normalized: cleaned, strippedSuffix }
}

export function validateConnectionEndpoint(
  endpoint: string,
  required: boolean,
  isZh = false
): string | null {
  const trimmed = endpoint.trim()
  if (!trimmed) {
    if (!required) return null
    return isZh ? '当前协议要求填写上游服务地址。' : 'Endpoint URL is required by provider schema.'
  }
  if (trimmed.length > 2048) {
    return isZh ? '上游服务地址长度不能超过 2048 个字符。' : 'Endpoint URL must not exceed 2048 characters.'
  }
  if (/[?#\\]/.test(trimmed)) {
    return isZh
      ? '上游服务地址不能包含查询参数 (?)、锚点 (#) 或反斜杠 (\\)。'
      : 'Endpoint URL must not contain query parameters (?), fragments (#), or backslashes (\\).'
  }
  try {
    const parsed = new URL(trimmed)
    if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') {
      return isZh ? '上游服务地址必须以 https:// 或 http:// 开头' : 'Endpoint must use https:// or http://'
    }
    if (!parsed.hostname) {
      return isZh
        ? '请填写包含有效主机名的服务地址（例如 https://api.openai.com）'
        : 'Enter a valid URL (for example https://api.openai.com)'
    }
    if (parsed.username || parsed.password) {
      return isZh
        ? '上游服务地址中禁止内嵌用户名或密码，请在下一步配置认证凭据。'
        : 'Do not embed credentials in the endpoint URL; configure secrets in the next step.'
    }
    for (const pattern of ENDPOINT_SUFFIX_PATTERNS) {
      if (pattern.test(trimmed)) {
        return isZh
          ? '请填写基础服务地址，无需包含 /chat/completions、/responses 或 /messages 后缀。'
          : 'Enter the base endpoint URL without /chat/completions, /responses, or /messages suffixes.'
      }
    }
    return null
  } catch {
    return isZh
      ? '请填写合法的 http:// 或 https:// 地址（例如 https://api.openai.com 或 http://127.0.0.1:3425）'
      : 'Enter a valid URL (for example https://api.openai.com)'
  }
}

export function validateNetworkPolicyId(policyId: string, isZh = false): string | null {
  const trimmed = policyId.trim()
  if (!trimmed) return null
  if (!/^[1-9]\d*$/.test(trimmed)) {
    return isZh
      ? '出站代理策略需留空（使用默认直连）或填写有效的正整数代理 ID。'
      : 'Leave Egress Proxy ID blank for direct egress or enter a valid numeric proxy ID.'
  }
  return null
}

const BLOCKED_HEADER_OVERRIDE_NAMES = new Set([
  'host',
  'content-length',
  'content-type',
  'transfer-encoding',
  'connection',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'proxy-connection',
  'te',
  'trailer',
  'upgrade',
  'authorization',
  'api-key',
  'x-api-key',
  'x-goog-api-key',
  'cookie',
  'accept-encoding',
  'sec-websocket-key',
  'sec-websocket-version',
  'sec-websocket-extensions',
  'sec-websocket-protocol',
  'sec-websocket-accept',
  'session_id',
  'conversation_id',
  'x-codex-turn-state',
  'x-codex-turn-metadata',
  'chatgpt-account-id',
  'x-claude-code-session-id',
  'x-client-request-id',
  'x-grok-conv-id'
])

const API_KEY_HEADER_OVERRIDE_PROVIDERS = new Set([
  'anthropic',
  'openai',
  'kimi',
  'zhipu',
  'deepseek',
  'minimax',
  'opencode_go',
  'opencodego',
  'qianwen',
  'commandcode'
])

export function isHeaderOverrideSupported(provider: string, authType: string): boolean {
  const p = provider.trim().toLowerCase()
  const a = authType.trim().toLowerCase()
  if (API_KEY_HEADER_OVERRIDE_PROVIDERS.has(p)) {
    return a === 'api_key' || a === 'apikey'
  }
  if (p === 'grok') {
    return a === 'api_key' || a === 'apikey' || a === 'oauth'
  }
  if (p === 'cursor') {
    return a === 'upstream'
  }
  return false
}

export function validateHeaderOverrideRows(
  rows: readonly HeaderOverrideDraftRow[],
  provider: string,
  authType: string,
  isZh = false
): string | null {
  const activeRows = rows.filter((row) => row.key.trim() !== '' || row.value.trim() !== '')
  if (activeRows.length === 0) return null
  if (!isHeaderOverrideSupported(provider, authType)) {
    return isZh
      ? '当前提供商或认证模式不支持自定义请求头覆写，请清空请求头或切换为兼容的 API Key 模式。'
      : 'Custom header overrides are not supported for the selected provider and authentication mode.'
  }
  if (activeRows.length > 64) {
    return isZh ? '自定义请求头最多支持 64 项。' : 'At most 64 custom headers are allowed.'
  }
  const seen = new Set<string>()
  for (const row of activeRows) {
    const name = row.key.trim()
    const value = row.value.trim()
    if (!name) {
      return isZh ? '自定义请求头名称不能为空。' : 'Custom header name cannot be empty.'
    }
    if (name.length > 200 || !/^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/.test(name)) {
      return isZh
        ? `请求头名称 "${name}" 格式不合法，仅允许标准 HTTP Header 字符。`
        : `Invalid HTTP header name "${name}".`
    }
    const lower = name.toLowerCase()
    if (BLOCKED_HEADER_OVERRIDE_NAMES.has(lower)) {
      return isZh
        ? `禁止覆写受保护的请求头 "${name}"（如认证头、Host 或传输控制头）。`
        : `Header "${name}" is protected and cannot be overridden.`
    }
    if (seen.has(lower)) {
      return isZh
        ? `存在重复的请求头名称 "${name}"（不区分大小写）。`
        : `Duplicate header name "${name}" (case-insensitive).`
    }
    if (value.length > 8192) {
      return isZh
        ? `请求头 "${name}" 的值超过最大长度限制 (8192)。`
        : `Header "${name}" value exceeds 8192 characters.`
    }
    seen.add(lower)
  }
  return null
}

export function buildHeaderOverridesMap(
  rows: readonly HeaderOverrideDraftRow[]
): Record<string, string> {
  const out: Record<string, string> = {}
  for (const row of rows) {
    const key = row.key.trim()
    const value = row.value.trim()
    if (!key || !value) continue
    out[key] = value
  }
  return out
}

const FIELD_ERROR_DESCRIPTORS: Record<
  string,
  { labelZh: string; labelEn: string; hintZh: string; hintEn: string }
> = {
  '/endpoint': {
    labelZh: '上游服务地址 (/endpoint)',
    labelEn: 'Upstream Endpoint (/endpoint)',
    hintZh: '请填写合法的 http:// 或 https:// 基础地址（不含查询参数或接口路径后缀）',
    hintEn: 'Enter a valid http:// or https:// base URL without query strings or route suffixes'
  },
  '/name': {
    labelZh: '连接名称 (/name)',
    labelEn: 'Connection Name (/name)',
    hintZh: '请填写 1–100 个字符的名称，首尾不含多余空白或控制字符',
    hintEn: 'Enter a 1–100 character name without leading/trailing whitespace or control characters'
  },
  '/header_overrides': {
    labelZh: '自定义请求头 (/header_overrides)',
    labelEn: 'Custom Headers (/header_overrides)',
    hintZh: '请确认当前平台与认证模式支持请求头覆写，且未包含 Authorization / Host / Content-Type 等受保护请求头',
    hintEn: 'Verify provider support and avoid protected headers such as Authorization, Host, or Content-Type'
  },
  '/network_policy_id': {
    labelZh: '出站代理策略 (/network_policy_id)',
    labelEn: 'Egress Proxy Policy (/network_policy_id)',
    hintZh: '请留空使用默认直连，或填写当前工作空间内已启用的数字代理 ID',
    hintEn: 'Leave blank for direct connection or enter an active numeric proxy ID in this workspace'
  },
  '/provider': {
    labelZh: '提供商与认证方式 (/provider)',
    labelEn: 'Provider & Auth Mode (/provider)',
    hintZh: '请选择受支持的上游提供商与认证模式组合',
    hintEn: 'Select a supported upstream provider and authentication mode combination'
  },
  '/limits/concurrency': {
    labelZh: '并发上限 (/limits/concurrency)',
    labelEn: 'Concurrency Cap (/limits/concurrency)',
    hintZh: '并发上限需设置在 1–10000 之间',
    hintEn: 'Concurrency cap must be an integer between 1 and 10000'
  },
  '/pool_ids': {
    labelZh: '出站资源池 (/pool_ids)',
    labelEn: 'Resource Pools (/pool_ids)',
    hintZh: '请选择有效的资源池 ID，避免重复或无效条目',
    hintEn: 'Select valid numeric resource pool IDs without duplicates'
  },
  '/assignments': {
    labelZh: '模型与资源池分配 (/assignments)',
    labelEn: 'Model & Pool Assignments (/assignments)',
    hintZh: '请检查模型列表与资源池绑定格式，避免空白或重复项',
    hintEn: 'Verify model IDs and pool assignments have valid non-duplicate values'
  },
  '/credential_draft': {
    labelZh: '授权草稿 (/credential_draft)',
    labelEn: 'Authorization Draft (/credential_draft)',
    hintZh: '授权会话已过期或与当前提供商不匹配，请重新发起授权',
    hintEn: 'Authorization draft expired or mismatched; restart the OAuth flow'
  },
  '/If-Match': {
    labelZh: '配置修订号 (/If-Match)',
    labelEn: 'Configuration Revision (/If-Match)',
    hintZh: '配置修订号无效或缺失，请重新加载最新状态后重试',
    hintEn: 'Missing or invalid configuration revision; reload latest state and retry'
  }
}

export function parseValidationFieldIssues(rawFields: unknown): ValidationFieldIssue[] {
  if (!Array.isArray(rawFields)) return []
  const issues: ValidationFieldIssue[] = []
  for (const entry of rawFields) {
    if (!entry || typeof entry !== 'object') continue
    const rec = entry as Record<string, unknown>
    const path = typeof rec.path === 'string' && rec.path.trim() ? rec.path.trim() : ''
    if (!path) continue
    const reason = typeof rec.reason === 'string' && rec.reason.trim() ? rec.reason.trim() : 'INVALID_INPUT'
    const descriptor = FIELD_ERROR_DESCRIPTORS[path] ?? {
      labelZh: `字段 (${path})`,
      labelEn: `Field (${path})`,
      hintZh: '请检查该字段的填写格式是否符合要求',
      hintEn: 'Check that this field has a valid value'
    }
    issues.push({
      path,
      reason,
      ...descriptor
    })
  }
  return issues
}

export function formatValidationIssuesMessage(
  issues: readonly ValidationFieldIssue[],
  isZh: boolean
): string {
  if (issues.length === 0) return ''
  if (issues.length === 1) {
    const first = issues[0]
    return isZh
      ? `字段校验未通过：${first.labelZh}，${first.hintZh}。`
      : `Field validation failed: ${first.labelEn} — ${first.hintEn}.`
  }
  const summary = issues
    .map((item) => (isZh ? `${item.labelZh}（${item.hintZh}）` : `${item.labelEn} (${item.hintEn})`))
    .join(isZh ? '；' : '; ')
  return isZh ? `多处字段校验未通过：${summary}。` : `Multiple fields failed validation: ${summary}.`
}

export function mapApiErrorToAsyncContext(
  error: unknown,
  fallbackMessage = 'Request failed',
  isZh = false
): AsyncErrorContext {
  const normalized = normalizeApiError(error)
  const requestId =
    typeof normalized.metadata?.request_id === 'string'
      ? normalized.metadata.request_id
      : typeof normalized.metadata?.requestId === 'string'
        ? normalized.metadata.requestId
        : undefined
  const serverVersion =
    typeof normalized.metadata?.current_version === 'string'
      ? normalized.metadata.current_version
      : typeof normalized.metadata?.version === 'string'
        ? normalized.metadata.version
        : undefined
  const requiredRole =
    typeof normalized.metadata?.required_role === 'string'
      ? normalized.metadata.required_role
      : isZh
        ? '工作空间管理员'
        : 'Workspace Admin'
  const statusUrl =
    typeof normalized.metadata?.status_url === 'string' ? normalized.metadata.status_url : undefined
  const fields = parseValidationFieldIssues(normalized.metadata?.fields)

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
      message:
        normalized.message ||
        (isZh ? '服务端配置修订已变更，请重新加载最新状态。' : 'Resource revision changed on the server.'),
      requestId,
      serverVersion
    }
  }
  if (normalized.status === 504 || normalized.reason === 'TIMEOUT' || normalized.reason === 'OUTCOME_UNKNOWN') {
    return {
      state: 'unknown_outcome',
      status: normalized.status || 504,
      reason: normalized.reason,
      message:
        normalized.message ||
        (isZh ? '操作结果正在等待服务端确认。' : 'Operation outcome is pending verification.'),
      requestId,
      statusUrl
    }
  }
  if (normalized.reason === 'STALE_VERIFICATION' || normalized.reason === 'STALE_PREVIEW') {
    return {
      state: 'stale',
      status: normalized.status || 412,
      reason: normalized.reason,
      message:
        normalized.message ||
        (isZh ? '验证或预览凭据已过期，请重新执行检查。' : 'Evidence is stale and must be refreshed.'),
      requestId
    }
  }

  const formattedFieldMessage = fields.length > 0 ? formatValidationIssuesMessage(fields, isZh) : ''
  return {
    state: 'partial_failure',
    status: normalized.status,
    reason: normalized.reason,
    message: formattedFieldMessage || normalized.message || fallbackMessage,
    requestId,
    ...(fields.length > 0 ? { fields } : {})
  }
}
