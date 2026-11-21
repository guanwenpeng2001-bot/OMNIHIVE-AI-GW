import { compatibleProvider, compatibleMode } from '@/constants/compatibleProviders'
import { NATIVE_PROFILE_PRESETS } from './nativeProfilePresets'
import { CHAT_PLATFORM_BASE_URLS } from '@/constants/platforms'
import { openAIPlanTypeLabel } from '@/utils/planType'
import {
  defaultWorkBuddyDomain,
  normalizeWorkBuddySite,
  type WorkBuddySite
} from '@/api/admin/workbuddy'

export interface WorkBuddyOAuthDraft {
  access_token: string
  refresh_token: string
  uid: string
  domain: string
  expires_at: string
  refresh_expires_at?: string
  enterprise_id?: string
}

export function extractWorkBuddySite(
  credentials?: Record<string, unknown> | null,
  extra?: Record<string, unknown> | null
): WorkBuddySite {
  const credSite = normalizeWorkBuddySite(credentials?.site)
  if (credSite) return credSite
  const snapshot = extra?.workbuddy_usage_snapshot as Record<string, unknown> | undefined
  const snapshotSite = normalizeWorkBuddySite(snapshot?.site)
  return snapshotSite ?? 'cn'
}

export function extractWorkBuddyOAuthDraft(
  credentials?: Record<string, unknown> | null,
  fallbackSite: WorkBuddySite = 'cn'
): WorkBuddyOAuthDraft {
  const site = normalizeWorkBuddySite(credentials?.site) ?? fallbackSite
  return {
    access_token: '',
    refresh_token: '',
    uid: typeof credentials?.uid === 'string' ? credentials.uid : '',
    domain:
      typeof credentials?.domain === 'string' && credentials.domain.trim()
        ? credentials.domain.trim()
        : defaultWorkBuddyDomain(site),
    expires_at: typeof credentials?.expires_at === 'string' ? credentials.expires_at : '',
    refresh_expires_at:
      typeof credentials?.refresh_expires_at === 'string'
        ? credentials.refresh_expires_at
        : undefined,
    enterprise_id:
      typeof credentials?.enterprise_id === 'string' ? credentials.enterprise_id : undefined
  }
}

export function buildWorkBuddyCredentials(input: {
  mode: 'oauth' | 'apikey'
  site: WorkBuddySite
  oauthCredentials?: WorkBuddyOAuthDraft
  apiKey?: string
  baseUrl?: string
  existing?: Record<string, unknown>
}): Record<string, unknown> {
  const credentials: Record<string, unknown> = {
    ...(input.existing ? { ...input.existing } : {}),
    site: input.site
  }
  if (input.mode === 'oauth') {
    const draft = input.oauthCredentials
    if (draft?.access_token.trim()) credentials.access_token = draft.access_token.trim()
    if (draft?.refresh_token.trim()) credentials.refresh_token = draft.refresh_token.trim()
    if (draft?.uid.trim()) credentials.uid = draft.uid.trim()
    credentials.domain = draft?.domain.trim() || defaultWorkBuddyDomain(input.site)
    if (draft?.expires_at.trim()) credentials.expires_at = draft.expires_at.trim()
    if (draft?.refresh_expires_at?.trim()) {
      credentials.refresh_expires_at = draft.refresh_expires_at.trim()
    }
    if (draft?.enterprise_id?.trim()) {
      credentials.enterprise_id = draft.enterprise_id.trim()
    }
    return credentials
  }
  if (input.apiKey?.trim()) {
    credentials.api_key = input.apiKey.trim()
  }
  const trimmedBaseUrl = input.baseUrl?.trim() || ''
  if (trimmedBaseUrl) {
    credentials.base_url = trimmedBaseUrl
  } else {
    delete credentials.base_url
  }
  return credentials
}

/** Cursor Relay 上游协议：与后端 NormalizeCursorAccount 的两个字面量保持一致。 */
export type CursorRelayProtocol = 'adaptive' | 'chat_completions'

function isCursorRelayProtocol(value: unknown): value is CursorRelayProtocol {
  return value === 'adaptive' || value === 'chat_completions'
}

/**
 * 解释 Cursor Relay 已保存的协议：
 * - 合法值原样返回；
 * - 缺失（undefined/null）或空串按后端回落语义返回 Adaptive；
 * - 其余非空值（含纯空白、带空格以及 anthropic/responses 等历史值）返回 null，
 *   表示需要用户重新选择，既不裁剪成合法值也不静默回落。
 * 后端此处是精确匹配，`protocol != ""` 即进入校验分支，所以空串之外的空白串同样非法。
 */
export function resolveCursorRelayProtocol(storedProtocol: unknown): CursorRelayProtocol | null {
  if (storedProtocol == null || storedProtocol === '') return 'adaptive'
  return isCursorRelayProtocol(storedProtocol) ? storedProtocol : null
}

/**
 * Relay builder 取值顺序：当前显式合法选择 → 已有合法值 → 缺失默认 Adaptive。
 * 非空非法且没有明确覆盖时拒绝构造：只改名称不应改变上游路由，
 * 也不应把后端必然拒绝的值提交上去。表单层需先校验并显示本地化原因。
 */
function resolveRelayProtocolForBuild(
  selectedProtocol: CursorRelayProtocol | undefined,
  storedProtocol: unknown
): CursorRelayProtocol {
  if (isCursorRelayProtocol(selectedProtocol)) return selectedProtocol
  const stored = resolveCursorRelayProtocol(storedProtocol)
  if (stored === null) {
    throw new Error('Cursor relay protocol must be adaptive or chat_completions')
  }
  return stored
}

export function buildCursorCredentials(input: {
  mode: 'official' | 'relay' | 'auto'
  apiKey: string
  baseUrl?: string
  clientVersion?: string
  existing?: Record<string, unknown>
  autoCredentials?: Record<string, string>
  apiProtocol?: CursorRelayProtocol
}): Record<string, unknown> {
  const credentials: Record<string, unknown> = {}
  if (input.mode !== 'auto' && input.apiKey.trim()) credentials.api_key = input.apiKey.trim()
  if (input.existing?.model_mapping) credentials.model_mapping = input.existing.model_mapping
  if (input.mode === 'relay') {
    credentials.base_url = input.baseUrl?.trim() || ''
    credentials.api_protocol = resolveRelayProtocolForBuild(input.apiProtocol, input.existing?.api_protocol)
  } else {
    // Auto / 官方 Key 走原生协议：不写 base_url、api_protocol、api_base_urls，
    // 也不回带历史的 Relay 字段（后端会拒绝这些组合）。
    credentials.client_version = input.clientVersion?.trim() || ''
    for (const field of ['machine_id', 'mac_machine_id']) {
      if (input.existing?.[field]) credentials[field] = input.existing[field]
    }
    if (input.mode === 'auto') {
      for (const field of ['access_token', 'refresh_token', 'machine_id', 'mac_machine_id']) {
        const value = input.autoCredentials?.[field]?.trim()
        if (value) credentials[field] = value
      }
    }
  }
  return credentials
}

export function applyInterceptWarmup(
  credentials: Record<string, unknown>,
  enabled: boolean,
  mode: 'create' | 'edit'
): void {
  if (enabled) {
    credentials.intercept_warmup_requests = true
  } else if (mode === 'edit') {
    delete credentials.intercept_warmup_requests
  }
}

export const ANTIGRAVITY_PROJECT_ID_CREDENTIAL_KEY = 'antigravity_project_id'

export function applyAntigravityProjectID(
  credentials: Record<string, unknown>,
  projectId: string,
  mode: 'create' | 'edit'
): void {
  const trimmed = projectId.trim()
  if (trimmed) {
    credentials[ANTIGRAVITY_PROJECT_ID_CREDENTIAL_KEY] = trimmed
  } else if (mode === 'edit') {
    delete credentials[ANTIGRAVITY_PROJECT_ID_CREDENTIAL_KEY]
  }
}

// ========== 请求头覆写（API-key 平台 + grok 的 api_key/oauth 账号） ==========

export const HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY = 'header_override_enabled'
export const HEADER_OVERRIDES_CREDENTIAL_KEY = 'header_overrides'

export interface HeaderOverrideRow {
  name: string
  value: string
}

/** 请求头覆写资格（与后端 IsHeaderOverrideEligible 保持一致） */
export function isHeaderOverrideCapable(platform: string, type: string): boolean {
  if (platform === 'cursor') return type === 'upstream'
  if (
    platform === 'anthropic' ||
    platform === 'openai' ||
    platform === 'kimi' ||
    platform === 'zhipu' ||
    platform === 'deepseek' ||
    platform === 'minimax' ||
    platform === 'qianwen' ||
    platform === 'commandcode' ||
    platform === 'opencode_go' ||
    platform === 'workbuddy' ||
    platform === 'kilo'
  ) {
    return type === 'apikey'
  }
  if (platform === 'grok') {
    return type === 'apikey' || type === 'oauth'
  }
  return false
}

/** 禁止覆写的请求头（与后端 headerOverrideBlockedNames 保持一致） */
const HEADER_OVERRIDE_BLOCKED_NAMES = new Set([
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

/** RFC 7230 token：合法的 HTTP header 名称字符集 */
const HEADER_NAME_PATTERN = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/

function isValidHeaderOverrideName(name: string): boolean {
  return HEADER_NAME_PATTERN.test(name)
}

/** 与后端 maxHeaderOverride* 常量保持一致 */
const HEADER_OVERRIDE_MAX_ENTRIES = 64
const HEADER_OVERRIDE_MAX_NAME_LENGTH = 200
const HEADER_OVERRIDE_MAX_VALUE_LENGTH = 8192

/** header value 不允许包含控制字符（与后端 httpguts.ValidHeaderFieldValue 对齐） */
// eslint-disable-next-line no-control-regex
const HEADER_VALUE_INVALID_PATTERN = /[\x00-\x08\x0a-\x1f\x7f]/

/** 长度限制按 UTF-8 字节计（与后端 Go len() 对齐，避免多字节值前端放行后端 400） */
const HEADER_TEXT_ENCODER = new TextEncoder()
function utf8ByteLength(value: string): number {
  return HEADER_TEXT_ENCODER.encode(value).length
}

/**
 * 校验请求头覆写行，返回首个错误的 i18n key（无错误返回 null）。
 * 名称为空但值非空 → invalidName；名称非法 → invalidName；
 * 禁止覆写 → blockedName；大小写不敏感重名 → duplicateName；
 * 值含控制字符或超长 → invalidValue；条目过多 → tooManyEntries。
 */
export function validateHeaderOverrideRows(
  rows: HeaderOverrideRow[]
): 'invalidName' | 'blockedName' | 'duplicateName' | 'invalidValue' | 'tooManyEntries' | null {
  const seen = new Set<string>()
  for (const row of rows) {
    const name = row.name.trim()
    const value = row.value.trim()
    if (!name) {
      if (value) return 'invalidName'
      continue
    }
    if (!isValidHeaderOverrideName(name) || name.length > HEADER_OVERRIDE_MAX_NAME_LENGTH) {
      return 'invalidName'
    }
    const lower = name.toLowerCase()
    if (HEADER_OVERRIDE_BLOCKED_NAMES.has(lower)) return 'blockedName'
    if (seen.has(lower)) return 'duplicateName'
    if (
      HEADER_VALUE_INVALID_PATTERN.test(value) ||
      utf8ByteLength(value) > HEADER_OVERRIDE_MAX_VALUE_LENGTH
    ) {
      return 'invalidValue'
    }
    seen.add(lower)
  }
  if (seen.size > HEADER_OVERRIDE_MAX_ENTRIES) return 'tooManyEntries'
  return null
}

/** 行数组 → credentials 存储对象（名称小写化，丢弃空行） */
export function buildHeaderOverridesObject(rows: HeaderOverrideRow[]): Record<string, string> {
  const result: Record<string, string> = {}
  for (const row of rows) {
    const name = row.name.trim().toLowerCase()
    if (!name) continue
    result[name] = row.value.trim()
  }
  return result
}

/** credentials 存储对象 → 行数组（按名称排序保证稳定展示） */
export function splitHeaderOverridesObject(record: unknown): HeaderOverrideRow[] {
  if (!record || typeof record !== 'object' || Array.isArray(record)) return []
  return Object.entries(record as Record<string, unknown>)
    .filter(([, value]) => typeof value === 'string')
    .map(([name, value]) => ({ name, value: value as string }))
    .sort((a, b) => a.name.localeCompare(b.name))
}

/**
 * 解析粘贴的 JSON 文本为请求头覆写行。
 * 仅接受扁平 JSON 对象；值允许 string/number/boolean（统一转字符串），
 * 其余类型或非对象输入返回 null 表示格式非法。键为空白的条目直接丢弃。
 */
export function parseHeaderOverridesJson(text: string): HeaderOverrideRow[] | null {
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    return null
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return null
  const rows: HeaderOverrideRow[] = []
  for (const [rawName, rawValue] of Object.entries(parsed as Record<string, unknown>)) {
    const name = rawName.trim()
    if (!name) continue
    if (
      typeof rawValue !== 'string' &&
      typeof rawValue !== 'number' &&
      typeof rawValue !== 'boolean'
    ) {
      return null
    }
    rows.push({ name, value: String(rawValue).trim() })
  }
  return rows.sort((a, b) => a.name.localeCompare(b.name))
}

/** 请求头覆写行 → 便于迁移/备份的 JSON 文本（跳过名称为空的占位行） */
export function serializeHeaderOverrideRows(rows: HeaderOverrideRow[]): string {
  const record: Record<string, string> = {}
  for (const row of rows) {
    const name = row.name.trim()
    if (!name) continue
    record[name] = row.value.trim()
  }
  return JSON.stringify(record, null, 2)
}

// ========== Grok 自定义转发地址（base_url 仅改写转发端点，凭证生命周期不受影响） ==========

/** OAuth 账号建号/刷新默认写入的 CLI 网关 host——只有它视同"未定制"。 */
const GROK_DEFAULT_GATEWAY_HOST = 'cli-chat-proxy.grok.com'

/**
 * 判断 Grok 账号存储的 base_url 是否为主动指定的上游端点。
 * 运营方可在官方 API / 区域 API / 第三方转发地址之间手动切换（应对单端点
 * 不可用），这些值都必须回显（开关开启 + 显示地址）。仅默认 CLI 网关
 * （建号/刷新自动写入）、空值与无法解析的值视为"未定制"（与后端
 * GetGrokBaseURL 的回落语义对齐），用于 OAuth 账号编辑时决定开关初始状态。
 */
export function isCustomGrokBaseUrl(value: unknown): boolean {
  if (typeof value !== 'string') return false
  const trimmed = value.trim()
  if (!trimmed) return false
  let parsed: URL
  try {
    parsed = new URL(trimmed)
  } catch {
    return false
  }
  return parsed.hostname.toLowerCase() !== GROK_DEFAULT_GATEWAY_HOST
}

export interface GrokBaseUrlPreset {
  /** i18n 子键：admin.accounts.grokCustomBaseUrl.presets.<labelKey> */
  labelKey?: 'cli' | 'official'
  /** 字面标签（如区域标识 us-east-1），专有名词不参与 i18n */
  label?: string
  url: string
}

/**
 * Grok 快捷端点（仅供快速填充，输入框仍可自由填写任意转发地址）。
 * 官方端点偶发不可用时，运营方靠这组预设在端点间手动切换。
 */
export const GROK_BASE_URL_PRESETS: GrokBaseUrlPreset[] = [
  { labelKey: 'cli', url: 'https://cli-chat-proxy.grok.com/v1' },
  { labelKey: 'official', url: 'https://api.x.ai/v1' },
  { label: 'us-east-1', url: 'https://us-east-1.api.x.ai/v1' },
  { label: 'us-west-2', url: 'https://us-west-2.api.x.ai/v1' },
  { label: 'eu-west-1', url: 'https://eu-west-1.api.x.ai/v1' }
]

// ========== 国产供应商（Kimi / Zhipu / DeepSeek）base_url 预设 ==========
// 与后端 service/domain_constants.go 的默认 base url 保持一致。
// 账号类型（payg 按量付费 / coding 编程套餐）决定额度监控方式；
// API 协议（chat_completions / anthropic / responses）决定转发端点与格式，
// 两者正交。同协议请求零转换直通，跨协议组合才走转换链。

export type CnAccountMode = 'payg' | 'coding'
export type OpenCodeAccountMode = 'zen' | 'zen_free' | 'go'
export type KiloAccountMode = 'free' | 'apikey'
export const DEFAULT_KILO_BASE_URL = 'https://api.kilo.ai/api/gateway'
export type CnProviderPlatform = 'kimi' | 'zhipu' | 'deepseek' | 'minimax' | 'qianwen'

/** deepseek / kimi / minimax 支持原生 responses；adaptive 会按入站协议选择原生端点。 */
export type CnApiProtocol = 'adaptive' | 'chat_completions' | 'anthropic' | 'responses'
export type CnNativeApiProtocol = Exclude<CnApiProtocol, 'adaptive'>

export function isCNProviderPlatform(platform: string): platform is CnProviderPlatform {
  return platform === 'qianwen' || platform === 'kimi' || platform === 'zhipu' || platform === 'deepseek' || platform === 'minimax'
}

/** 平台提供原生 Responses；具体模型支持由上游决定。 */
export function cnSupportsNativeResponses(platform: string): boolean {
  return platform === 'zhipu' || platform === 'commandcode' || platform === 'qianwen' || platform === 'deepseek' || platform === 'kimi' || platform === 'minimax' || platform === 'opencode_go'
}

// ===== 后加平台（混元 / 火山方舟 / 千帆 / Mistral）的文本协议 =====
// 协议集合的唯一依据是后端 domain/platform_capabilities.go 的平台描述符：
// MessagesPOST / ResponsesPOST 为 EndpointNative 即已核证，EndpointRejected 即缺失。
// 这四个平台此前被前端按 "是不是 chat-only" 一刀切成 chat_completions，用户拿不到混元的
// 原生 Anthropic Messages / Responses，也拿不到火山方舟与千帆的原生 Responses。

/** 后端已声明原生文本协议、但不在既有国产多协议集合里的平台。 */
export type DeclaredProtocolPlatform = 'hunyuan' | 'volcengine' | 'qianfan' | 'mistral' | 'openrouter'

/** 后端描述符声明的原生文本协议。 */
interface DeclaredTextProtocols {
  /** MessagesPOST: 同一站点上的原生 Anthropic Messages 路径（TokenHub 用 x-api-key）。 */
  messages: boolean
  /** ResponsesPOST: 原生 Responses 路径。 */
  responses: boolean
}

/**
 * 镜像 backend/internal/domain/platform_capabilities.go 的声明：
 *   hunyuan     ResponsesPOST + MessagesPOST 均为 native（混元是多协议 API-key 条目）
 *   volcengine  ResponsesPOST native；国内数据面无 Anthropic Messages 端点
 *   qianfan     ResponsesPOST native；messages 是已核证的缺失（POST /v2/messages 404）
 *   mistral     上游只有 Chat Completions，不声明任何原生协议，因此不给协议选项
 */
export const DECLARED_PLATFORM_TEXT_PROTOCOLS: Record<DeclaredProtocolPlatform, DeclaredTextProtocols> = {
  hunyuan: { messages: true, responses: true },
  volcengine: { messages: false, responses: true },
  qianfan: { messages: false, responses: true },
  mistral: { messages: false, responses: false },
  openrouter: { messages: true, responses: true }
}

export function isDeclaredProtocolPlatform(platform: string): platform is DeclaredProtocolPlatform {
  return Object.prototype.hasOwnProperty.call(DECLARED_PLATFORM_TEXT_PROTOCOLS, platform)
}

/** 平台已声明的原生协议（不含 adaptive）；chat_completions 是所有条目的既有 trunk。 */
export function declaredPlatformNativeProtocols(platform: DeclaredProtocolPlatform, mode: CnAccountMode = 'payg'): CnNativeApiProtocol[] {
  const declared = DECLARED_PLATFORM_TEXT_PROTOCOLS[platform]
  const protocols: CnNativeApiProtocol[] = ['chat_completions']
  if (declared.messages || platform === 'volcengine' || (mode === 'coding' && platform === 'qianfan')) protocols.push('anthropic')
  if (declared.responses && !(mode === 'coding' && platform === 'qianfan')) protocols.push('responses')
  return protocols
}

/** 协议卡选项：只有一种原生协议时没有可自适应的第二协议，因此不给 adaptive。 */
export function declaredPlatformApiProtocols(platform: DeclaredProtocolPlatform, mode: CnAccountMode = 'payg'): CnApiProtocol[] {
  const protocols = declaredPlatformNativeProtocols(platform, mode)
  return protocols.length > 1 ? ['adaptive', ...protocols] : protocols
}

/** 平台是否有原生 Responses 端点：既有国产/OpenCode 集合 + 后加平台的后端声明。 */
export function hasDeclaredNativeResponses(platform: string): boolean {
  if (cnSupportsNativeResponses(platform)) return true
  return isDeclaredProtocolPlatform(platform) && DECLARED_PLATFORM_TEXT_PROTOCOLS[platform].responses
}

// ===== 账号类型卡（按量付费 / 编程套餐）的平台覆盖 =====
// 用独立谓词而不是加宽 isCNProviderPlatform：后者被余额/额度单元格、协议选项与序列化
// 等多处消费，把后加平台带进去会外溢到与账号类型无关的展示逻辑。

/**
 * 模式卡是否可见：既有国产多协议供应商+ 后加四家（混元 / 火山方舟 / 千帆 / Mistral）。
 * 接入模式与是否提供额度 API 相互独立。
 */
export function showsAccountModeCard(platform: string): boolean {
  return isCNProviderPlatform(platform) || isDeclaredProtocolPlatform(platform)
}

/**
 * coding（编程套餐）档是否可见：DeepSeek 与 Mistral 官方只有按量付费。
 * Mistral 官方原文 "API keys are billed based on consumption, independent of any monthly
 * subscription"，所以不给它们编造套餐端点，模式卡只剩按量付费一项。
 */
export function showsCodingPlanMode(platform: string): boolean {
  if (!showsAccountModeCard(platform)) return false
  return platform !== 'deepseek' && platform !== 'mistral' && platform !== 'openrouter'
}

export const OPENCODE_GO_BASE_URL = 'https://opencode.ai/zen/go/v1'
export const OPENCODE_GO_ANTHROPIC_BASE_URL = 'https://opencode.ai/zen/go'
export const OPENCODE_ZEN_BASE_URL = 'https://opencode.ai/zen/v1'
export const OPENCODE_ZEN_ANTHROPIC_BASE_URL = 'https://opencode.ai/zen'

export function isOpenCodeGoPlatform(platform: string): boolean {
  return platform === 'opencode_go'
}

export const DEFAULT_COMMANDCODE_PROTOCOL_RULES: OpenCodeGoProtocolRule[] = [
  { pattern: 'claude-*', protocol: 'anthropic' },
  { pattern: 'anthropic/claude-*', protocol: 'anthropic' }
]

export const OPENCODE_GO_PROTOCOL_RULES_KEY = 'protocol_rules'

export interface OpenCodeGoProtocolRule {
  pattern: string
  protocol: CnNativeApiProtocol
}

export const DEFAULT_OPENCODE_GO_PROTOCOL_RULES: OpenCodeGoProtocolRule[] = [
  { pattern: 'grok-*', protocol: 'responses' },
  { pattern: 'gpt-*', protocol: 'responses' },
  { pattern: 'muse-spark-*', protocol: 'responses' },
  { pattern: 'minimax-*', protocol: 'anthropic' },
  { pattern: 'qwen*', protocol: 'anthropic' }
]

export const DEFAULT_OPENCODE_ZEN_PROTOCOL_RULES: OpenCodeGoProtocolRule[] = [
  { pattern: 'grok-*', protocol: 'responses' },
  { pattern: 'gpt-*', protocol: 'responses' },
  { pattern: 'muse-spark-*', protocol: 'responses' },
  { pattern: 'claude-*', protocol: 'anthropic' },
  { pattern: 'qwen*', protocol: 'anthropic' }
]

export const DEFAULT_OPENCODE_ZEN_FREE_PROTOCOL_RULES: OpenCodeGoProtocolRule[] = [
  { pattern: 'grok-*', protocol: 'responses' },
  { pattern: 'gpt-*', protocol: 'responses' },
  { pattern: 'muse-spark-*', protocol: 'responses' },
  { pattern: 'union-alpha', protocol: 'anthropic' },
  { pattern: 'claude-*', protocol: 'anthropic' },
  { pattern: 'qwen*', protocol: 'anthropic' }
]

export function resolveOpenCodeAccountMode(value: unknown): OpenCodeAccountMode {
  if (value === 'zen_free') return 'zen_free'
  return value === 'zen' ? 'zen' : 'go'
}

export function resolveKiloAccountMode(value: unknown): KiloAccountMode {
  return value === 'apikey' ? 'apikey' : 'free'
}

export function buildKiloCredentials(input: {
  mode: KiloAccountMode
  apiKey?: string
  baseUrl?: string
  existing?: Record<string, unknown>
}): Record<string, unknown> {
  const credentials: Record<string, unknown> = {
    ...(input.existing ? { ...input.existing } : {}),
    account_mode: input.mode
  }
  const trimmedKey = input.apiKey?.trim() || ''
  if (input.mode === 'free') {
    delete credentials.api_key
  } else if (trimmedKey) {
    credentials.api_key = trimmedKey
  }
  const trimmedBaseUrl = input.baseUrl?.trim() || ''
  if (trimmedBaseUrl) {
    credentials.base_url = trimmedBaseUrl
  } else {
    credentials.base_url = DEFAULT_KILO_BASE_URL
  }
  return credentials
}

export function defaultOpenCodeProtocolRules(mode: OpenCodeAccountMode = 'go'): OpenCodeGoProtocolRule[] {
  if (mode === 'zen_free') return DEFAULT_OPENCODE_ZEN_FREE_PROTOCOL_RULES
  return mode === 'zen' ? DEFAULT_OPENCODE_ZEN_PROTOCOL_RULES : DEFAULT_OPENCODE_GO_PROTOCOL_RULES
}

export function cloneOpenCodeGoProtocolRules(
  rules: OpenCodeGoProtocolRule[] = DEFAULT_OPENCODE_GO_PROTOCOL_RULES
): OpenCodeGoProtocolRule[] {
  return rules.map(rule => ({ pattern: rule.pattern, protocol: rule.protocol }))
}

function isNativeOpenCodeGoProtocol(value: unknown): value is CnNativeApiProtocol {
  return value === 'chat_completions' || value === 'anthropic' || value === 'responses'
}

export function parseOpenCodeGoProtocolRules(raw: unknown): OpenCodeGoProtocolRule[] | null {
  if (raw == null) return null
  if (!Array.isArray(raw)) return cloneOpenCodeGoProtocolRules()
  const rules: OpenCodeGoProtocolRule[] = []
  for (const item of raw) {
    if (!item || typeof item !== 'object') continue
    const pattern = typeof (item as { pattern?: unknown }).pattern === 'string'
      ? (item as { pattern: string }).pattern.trim()
      : ''
    const protocol = (item as { protocol?: unknown }).protocol
    if (!pattern || !isNativeOpenCodeGoProtocol(protocol)) continue
    rules.push({ pattern, protocol })
  }
  return rules
}

export function applyOpenCodeGoProtocolRules(
  credentials: Record<string, unknown>,
  rules: OpenCodeGoProtocolRule[],
  mode: 'create' | 'edit'
): void {
  const serialized = rules
    .map(rule => ({
      pattern: rule.pattern.trim().toLowerCase(),
      protocol: rule.protocol
    }))
    .filter(rule => rule.pattern.length > 0 && isNativeOpenCodeGoProtocol(rule.protocol))
  if (serialized.length > 0 || mode === 'edit') {
    credentials[OPENCODE_GO_PROTOCOL_RULES_KEY] = serialized
  }
}

export function isMultiProtocolApiKeyPlatform(platform: string): platform is CnProviderPlatform | 'opencode_go' | 'commandcode' {
  return platform === 'commandcode' || platform === 'qianwen' || platform === 'kimi' || platform === 'zhipu' || platform === 'deepseek' || platform === 'minimax' || platform === 'opencode_go'
}

export interface CnBaseUrlPreset {
  mode: CnAccountMode
  protocol: CnApiProtocol
  /** 专有名词，不参与 i18n（如 TokenHub Main、Ark）；给出 labelKey 时展示层按语言取词条 */
  label: string
  /** 需要本地化的档位名（如千帆套餐的企业版）；展示层优先于 label */
  labelKey?: string
  url: string
}

/** 有官方站点预设的平台：国产多协议供应商 + 后加四家（混元 / 火山方舟 / 千帆 / Mistral）。 */
export type CnBaseUrlPresetPlatform = CnProviderPlatform | DeclaredProtocolPlatform

/** 该平台是否有官方站点预设（预设组件的渲染门槛）。 */
export function hasPlatformBaseUrlPresets(platform: string): platform is CnBaseUrlPresetPlatform {
  return isCNProviderPlatform(platform) || isDeclaredProtocolPlatform(platform)
}

/**
 * 混元 TokenHub 站点白名单：逐条镜像 backend/internal/service/hunyuan_adapter.go 的
 * hunyuanActionBaseURL 白名单（4 个 TokenHub 站点 + 1 个旧版兼容站点），站点归属同时见
 * backend/internal/service/model_capability_matcher.go 的 hunyuan 分支。前端不自行拼地址：
 * 白名单以外的地址后端直接报错。混元的 Chat Completions / Anthropic Messages / Responses
 * 共用同一站点根（协议路径由后端拼接），所以每个站点按协议各给一条预设。
 */
const HUNYUAN_TOKENHUB_SITES: Array<{ label: string; url: string }> = [
  { label: 'TokenHub Main', url: 'https://tokenhub.tencentmaas.com/v1' },
  { label: 'TokenHub Main CN', url: 'https://tokenhub.tencentmaas.cn/v1' },
  { label: 'TokenHub Intl', url: 'https://tokenhub-intl.tencentmaas.com/v1' },
  { label: 'TokenHub Intl CN', url: 'https://tokenhub-intl.tencentmaas.cn/v1' },
  { label: 'Hunyuan Legacy', url: 'https://api.hunyuan.cloud.tencent.com/v1' }
]

const HUNYUAN_BASE_URL_PRESETS: CnBaseUrlPreset[] = HUNYUAN_TOKENHUB_SITES.flatMap((site): CnBaseUrlPreset[] =>
  declaredPlatformNativeProtocols('hunyuan').map(protocol => ({
    mode: 'payg',
    protocol,
    label: site.label,
    url: site.url
  }))
)

/**
 * 后加三家的编程套餐（coding）端点。套餐与按量付费的 Base URL 与 Key 互不通用
 * （腾讯 TokenHub 原文"互不通用，请勿混用"；火山方舟原文"请勿使用
 * https://ark.cn-beijing.volces.com/api/v3 ：该 Base URL 不会消耗您的 Coding Plan 额度，
 * 而是会产生额外费用"），所以地址逐字取自厂商一手文档，不自行拼接：
 *   腾讯 TokenHub Coding Plan   https://cloud.tencent.com/document/product/1823/130092
 *   火山方舟 Coding Plan 个人版 https://docs.volcengine.com/docs/ark/coding-plan-personal-get-started
 *   千帆 Token Plan 个人版       https://cloud.baidu.com/doc/qianfan/s/Dmrabu8b6
 * 与按量付费站点同一套约定：厂商公布的 OpenAI 兼容根服务 OpenAI 协议族
 * （chat_completions / responses 同址），Anthropic 兼容根单独一条。
 * Mistral 官方只有按量付费，因此不在此表内。
 */
const LATE_PLATFORM_CODING_BASE_URLS: Partial<Record<DeclaredProtocolPlatform, Partial<Record<CnNativeApiProtocol, string>>>> = {
  hunyuan: {
    chat_completions: 'https://api.lkeap.cloud.tencent.com/coding/v3',
    anthropic: 'https://api.lkeap.cloud.tencent.com/coding/anthropic'
  },
  volcengine: {
    chat_completions: 'https://ark.cn-beijing.volces.com/api/coding/v3',
    anthropic: 'https://ark.cn-beijing.volces.com/api/coding'
  },
  qianfan: {
    chat_completions: 'https://qianfan.baidubce.com/v2/tokenplan/personal',
    anthropic: 'https://qianfan.baidubce.com/anthropic/tokenplan/personal'
  }
}

/**
 * 千帆 Token Plan 企业版站点：与个人版地址不同，只在预设列表里作为可选项给出（默认仍个人版），
 * 不做第三级选择器。地址取自企业版文档 https://cloud.baidu.com/doc/qianfan/s/ymq8wwch2。
 */
const QIANFAN_TOKEN_PLAN_TEAM_BASE_URLS: Partial<Record<CnNativeApiProtocol, string>> = {
  chat_completions: 'https://qianfan.baidubce.com/v2/tokenplan/team',
  anthropic: 'https://qianfan.baidubce.com/anthropic/tokenplan/team'
}

/**
 * 套餐站点在指定协议下的地址：OpenAI 协议族（chat_completions / responses）共用厂商公布的
 * OpenAI 兼容根，adaptive 的单输入同样落在这个根上；Anthropic 兼容单独一条。
 * 没有该形态套餐地址时返回空串：宁可不给默认值，也不拿按量付费地址去配套餐 Key。
 */
function latePlatformCodingBaseUrl(
  platform: string,
  mode: CnAccountMode | OpenCodeAccountMode,
  protocol: CnApiProtocol
): string {
  if (mode !== 'coding' || !isDeclaredProtocolPlatform(platform)) return ''
  const paths = LATE_PLATFORM_CODING_BASE_URLS[platform]
  if (!paths) return ''
  return protocol === 'anthropic' ? (paths.anthropic ?? '') : (paths.chat_completions ?? '')
}

/** 按量付费的单一官方站点展开到该平台声明的每个文本协议（站点根取自唯一来源）。 */
function paygSitePresets(platform: DeclaredProtocolPlatform, label: string): CnBaseUrlPreset[] {
  return declaredPlatformNativeProtocols(platform).map(protocol => ({
    mode: 'payg',
    protocol,
    label,
    url: CHAT_PLATFORM_BASE_URLS[platform]
  }))
}

/** 套餐站点预设：OpenAI 兼容根服务 chat_completions / responses，Anthropic 兼容根单独一条。 */
function codingSitePresets(platform: DeclaredProtocolPlatform, label: string): CnBaseUrlPreset[] {
  const paths = LATE_PLATFORM_CODING_BASE_URLS[platform]
  if (!paths?.chat_completions) return []
  const presets: CnBaseUrlPreset[] = [
    { mode: 'coding', protocol: 'chat_completions', label, url: paths.chat_completions }
  ]
  if (platform === 'volcengine') presets.push({ mode: 'coding', protocol: 'responses', label, url: paths.chat_completions })
  if (paths.anthropic) {
    presets.push({ mode: 'coding', protocol: 'anthropic', label: `${label} Anthropic`, url: paths.anthropic })
  }
  return presets
}

/** 千帆套餐的企业版预设：地址是专有名词，档位名走 i18n。 */
function qianfanTeamPresets(): CnBaseUrlPreset[] {
  const presets: CnBaseUrlPreset[] = []
  const chatUrl = QIANFAN_TOKEN_PLAN_TEAM_BASE_URLS.chat_completions
  const anthropicUrl = QIANFAN_TOKEN_PLAN_TEAM_BASE_URLS.anthropic
  if (chatUrl) {
    presets.push({
      mode: 'coding',
      protocol: 'chat_completions',
      label: 'Qianfan TokenPlan Enterprise',
      labelKey: 'admin.accounts.cnProviders.baseUrlPresets.qianfanTokenPlanEnterprise',
      url: chatUrl
    })
  }
  if (anthropicUrl) {
    presets.push({
      mode: 'coding',
      protocol: 'anthropic',
      label: 'Qianfan TokenPlan Enterprise Anthropic',
      labelKey: 'admin.accounts.cnProviders.baseUrlPresets.qianfanTokenPlanEnterpriseAnthropic',
      url: anthropicUrl
    })
  }
  return presets
}

/** 各供应商按账号类型 × API 协议分档的快捷端点（点击快速填充，输入框仍可自由填写）。 */
export const CN_BASE_URL_PRESETS: Record<CnBaseUrlPresetPlatform, CnBaseUrlPreset[]> = {
  // Coding Plan publishes Chat and Messages endpoints; native Responses is undocumented.
  qianwen: [
    { mode: 'coding', protocol: 'chat_completions', label: 'Coding Plan Chat', url: 'https://coding.dashscope.aliyuncs.com/v1' },
    { mode: 'coding', protocol: 'anthropic', label: 'Coding Plan Messages', url: 'https://coding.dashscope.aliyuncs.com/apps/anthropic' },
    { mode: 'payg', protocol: 'chat_completions', label: 'DashScope Chat', url: 'https://dashscope.aliyuncs.com/compatible-mode/v1' },
    { mode: 'payg', protocol: 'responses', label: 'DashScope Responses', url: 'https://dashscope.aliyuncs.com/compatible-mode/v1' },
    { mode: 'payg', protocol: 'anthropic', label: 'DashScope Messages', url: 'https://dashscope.aliyuncs.com/apps/anthropic' }
  ],
  kimi: [
    { mode: 'payg', protocol: 'chat_completions', label: 'Moonshot', url: 'https://api.moonshot.cn/v1' },
    { mode: 'payg', protocol: 'anthropic', label: 'Moonshot Anthropic', url: 'https://api.moonshot.cn/anthropic' },
    { mode: 'payg', protocol: 'responses', label: 'Moonshot Responses', url: 'https://api.moonshot.cn/v1' },
    { mode: 'coding', protocol: 'chat_completions', label: 'Kimi For Coding', url: 'https://api.kimi.com/coding/v1' },
    { mode: 'coding', protocol: 'anthropic', label: 'Kimi Coding Anthropic', url: 'https://api.kimi.com/coding' },
    { mode: 'coding', protocol: 'responses', label: 'Kimi Coding Responses', url: 'https://api.kimi.com/coding/v1' }
  ],
  zhipu: [
    { mode: 'payg', protocol: 'responses', label: 'GLM Responses', url: 'https://open.bigmodel.cn/api/v1' },
    { mode: 'coding', protocol: 'responses', label: 'GLM Coding Responses', url: 'https://open.bigmodel.cn/api/v1' },
    { mode: 'payg', protocol: 'chat_completions', label: 'GLM PaaS', url: 'https://open.bigmodel.cn/api/paas/v4' },
    { mode: 'payg', protocol: 'anthropic', label: 'GLM Anthropic', url: 'https://open.bigmodel.cn/api/anthropic' },
    { mode: 'coding', protocol: 'chat_completions', label: 'GLM Coding', url: 'https://open.bigmodel.cn/api/coding/paas/v4' },
    { mode: 'coding', protocol: 'anthropic', label: 'GLM Coding Anthropic', url: 'https://open.bigmodel.cn/api/anthropic' }
  ],
  deepseek: [
    { mode: 'payg', protocol: 'chat_completions', label: 'DeepSeek', url: 'https://api.deepseek.com' },
    { mode: 'payg', protocol: 'anthropic', label: 'DeepSeek Anthropic', url: 'https://api.deepseek.com/anthropic' },
    { mode: 'payg', protocol: 'responses', label: 'DeepSeek Responses', url: 'https://api.deepseek.com' }
  ],
  hunyuan: [
    ...HUNYUAN_BASE_URL_PRESETS,
    ...codingSitePresets('hunyuan', 'TokenHub Coding Plan')
  ],
  volcengine: [
    ...paygSitePresets('volcengine', 'Ark'),
    ...codingSitePresets('volcengine', 'Ark Coding Plan')
  ],
  qianfan: [
    ...paygSitePresets('qianfan', 'Qianfan'),
    ...codingSitePresets('qianfan', 'Qianfan TokenPlan'),
    ...qianfanTeamPresets()
  ],
  mistral: paygSitePresets('mistral', 'Mistral'),
  openrouter: paygSitePresets('openrouter', 'OpenRouter'),
  minimax: [
    { mode: 'payg', protocol: 'chat_completions', label: 'MiniMax CN', url: 'https://api.minimaxi.com/v1' },
    { mode: 'payg', protocol: 'anthropic', label: 'MiniMax CN Anthropic', url: 'https://api.minimaxi.com/anthropic' },
    { mode: 'payg', protocol: 'responses', label: 'MiniMax CN Responses', url: 'https://api.minimaxi.com/v1' },
    { mode: 'payg', protocol: 'chat_completions', label: 'MiniMax Intl', url: 'https://api.minimax.io/v1' },
    { mode: 'payg', protocol: 'anthropic', label: 'MiniMax Intl Anthropic', url: 'https://api.minimax.io/anthropic' },
    { mode: 'payg', protocol: 'responses', label: 'MiniMax Intl Responses', url: 'https://api.minimax.io/v1' },
    { mode: 'coding', protocol: 'chat_completions', label: 'MiniMax Coding CN', url: 'https://api.minimaxi.com/v1' },
    { mode: 'coding', protocol: 'anthropic', label: 'MiniMax Coding CN Anthropic', url: 'https://api.minimaxi.com/anthropic' },
    { mode: 'coding', protocol: 'responses', label: 'MiniMax Coding CN Responses', url: 'https://api.minimaxi.com/v1' },
    { mode: 'coding', protocol: 'chat_completions', label: 'MiniMax Coding Intl', url: 'https://api.minimax.io/v1' },
    { mode: 'coding', protocol: 'anthropic', label: 'MiniMax Coding Intl Anthropic', url: 'https://api.minimax.io/anthropic' },
    { mode: 'coding', protocol: 'responses', label: 'MiniMax Coding Intl Responses', url: 'https://api.minimax.io/v1' }
  ]
}

// Retain legacy selectable endpoints, append exact regional/plan configurations.
for (const platform of Object.keys(NATIVE_PROFILE_PRESETS) as CnBaseUrlPresetPlatform[]) {
  const existing = CN_BASE_URL_PRESETS[platform]
  for (const preset of NATIVE_PROFILE_PRESETS[platform] ?? []) {
    if (!existing.some(p => p.mode === preset.mode && p.protocol === preset.protocol && p.url === preset.url && (!preset.label.startsWith('Token Enterprise') || p.label === preset.label))) existing.push(preset)
  }
}

/** 返回指定供应商 + 账号类型 + API 协议的默认 base url。 */
export function defaultCNBaseUrl(
  platform: string,
  mode: CnAccountMode | OpenCodeAccountMode,
  protocol: CnApiProtocol = 'chat_completions'
): string {
  if (protocol === 'anthropic') {
    if (platform === 'volcengine' && mode === 'payg') return 'https://ark.cn-beijing.volces.com/api/compatible'
    switch (platform) {
      case 'commandcode':
        return 'https://api.commandcode.ai/provider/v1'
      case 'openrouter':
        return CHAT_PLATFORM_BASE_URLS.openrouter
      case 'qianwen':
        return mode === 'coding' ? 'https://coding.dashscope.aliyuncs.com/apps/anthropic' : 'https://dashscope.aliyuncs.com/apps/anthropic'
      case 'kimi':
        return mode === 'coding' ? 'https://api.kimi.com/coding' : 'https://api.moonshot.cn/anthropic'
      case 'zhipu':
        return 'https://open.bigmodel.cn/api/anthropic'
      case 'deepseek':
        return 'https://api.deepseek.com/anthropic'
      case 'minimax':
        return 'https://api.minimaxi.com/anthropic'
      case 'opencode_go':
        return mode === 'zen' || mode === 'zen_free' ? OPENCODE_ZEN_ANTHROPIC_BASE_URL : OPENCODE_GO_ANTHROPIC_BASE_URL
      case 'hunyuan':
        // TokenHub 的 Anthropic Messages 路径与 OpenAI 路径共用同一站点根
        // （backend/internal/service/openai_gateway_messages_anthropic_native.go 按版本段
        // 拼 {base}/v1/messages，不会出现 /v1/v1）。套餐档换成厂商公布的 Anthropic 套餐根。
        return latePlatformCodingBaseUrl(platform, mode, protocol) || CHAT_PLATFORM_BASE_URLS.hunyuan
      case 'volcengine':
      case 'qianfan':
        // 按量付费的数据面没有 Messages 端点（后端声明 messagesPost 缺失），
        // 只有套餐档公布了 Anthropic 兼容地址；非套餐档返回空串，不拿按量站点冒充。
        return latePlatformCodingBaseUrl(platform, mode, protocol)
      default:
        return ''
    }
  }
  // responses：Kimi / DeepSeek / MiniMax 的 base 与 chat_completions 相同（端点路径差异由后端处理）；
  // 后加平台的已声明原生协议同样共用同一站点根。
  switch (platform) {
    case 'commandcode':
      return 'https://api.commandcode.ai/provider/v1'
    case 'qianwen':
      return mode === 'coding' ? 'https://coding.dashscope.aliyuncs.com/v1' : 'https://dashscope.aliyuncs.com/compatible-mode/v1'
    case 'kimi':
      return mode === 'coding' ? 'https://api.kimi.com/coding/v1' : 'https://api.moonshot.cn/v1'
    case 'zhipu':
      if (protocol === 'responses') return 'https://open.bigmodel.cn/api/v1'
      return mode === 'coding'
        ? 'https://open.bigmodel.cn/api/coding/paas/v4'
        : 'https://open.bigmodel.cn/api/paas/v4'
    case 'deepseek':
      return 'https://api.deepseek.com'
    case 'minimax':
      return 'https://api.minimaxi.com/v1'
    case 'opencode_go':
      return mode === 'zen' || mode === 'zen_free' ? OPENCODE_ZEN_BASE_URL : OPENCODE_GO_BASE_URL
    case 'kilo':
      return DEFAULT_KILO_BASE_URL
    // 后加平台：按量付费只有一条 OpenAI 形态的站点（TokenHub / Ark /api/v3 / 千帆 /v2），
    // Chat 与 Responses 同址；套餐（coding）档换成厂商的套餐专属站点。
    case 'hunyuan':
    case 'volcengine':
    case 'qianfan':
    case 'mistral':
    case 'openrouter':
      return latePlatformCodingBaseUrl(platform, mode, protocol) || CHAT_PLATFORM_BASE_URLS[platform]
    default:
      return ''
  }
}

/**
 * 该平台在指定账号类型下的官方预设地址（用于判断输入框里的地址是否仍是官方站点，
 * 而不是用户手填的中转地址）。
 */
export function platformPresetUrls(platform: string, mode: CnAccountMode): string[] {
  if (!hasPlatformBaseUrlPresets(platform)) return []
  return (CN_BASE_URL_PRESETS[platform] ?? []).filter(preset => preset.mode === mode).map(preset => preset.url)
}

/** 返回自适应模式下需要配置的原生协议及其默认端点。 */
export function defaultCNAdaptiveBaseUrls(
  platform: CnProviderPlatform | 'opencode_go' | 'commandcode' | DeclaredProtocolPlatform,
  mode: CnAccountMode | OpenCodeAccountMode
): Record<CnNativeApiProtocol, string> {
  return {
    chat_completions: defaultCNBaseUrl(platform, mode, 'chat_completions'),
    anthropic: defaultCNBaseUrl(platform, mode, 'anthropic'),
    responses: hasDeclaredNativeResponses(platform) && !(mode === 'coding' && (platform === 'qianwen' || platform === 'qianfan' || platform === 'hunyuan')) ? defaultCNBaseUrl(platform, mode, 'responses') : ''
  }
}

// ===== 国产供应商用量单元格可见性（单一事实源） =====
// CNProviderQuotaCell / CNProviderBalanceCell 与 AccountUsageCell 的占位符判定
// 共用，避免多处复制条件后一处改另一处漏改。

/** Separate control-plane query fields from the inference API key. */
export interface VolcengineQuotaDraft {
  accessKeyId: string
  secretAccessKey: string
  edition: 'personal' | 'team'
  seatId: string
  clearAccessKeyId: boolean
  clearSecretAccessKey: boolean
}

/** Secrets are never restored into inputs, including legacy unredacted DTOs. */
export function volcengineQuotaDraft(credentials: Record<string, unknown> = {}): VolcengineQuotaDraft {
  return {
    accessKeyId: '', secretAccessKey: '',
    edition: credentials.volcengine_plan_edition === 'team' ? 'team' : 'personal',
    seatId: typeof credentials.volcengine_seat_id === 'string' ? credentials.volcengine_seat_id : '',
    clearAccessKeyId: false, clearSecretAccessKey: false
  }
}

export function applyVolcengineQuotaCredentials(credentials: Record<string, unknown>, draft: VolcengineQuotaDraft): void {
  for (const [field, value, clear] of [
    ['volcengine_access_key_id', draft.accessKeyId, draft.clearAccessKeyId],
    ['volcengine_secret_access_key', draft.secretAccessKey, draft.clearSecretAccessKey]
  ] as const) {
    delete credentials[field]
    if (clear) credentials[field] = null
    else if (value.trim()) credentials[field] = value.trim()
  }
  credentials.volcengine_plan_edition = draft.edition
  if (draft.edition === 'team' && draft.seatId.trim()) credentials.volcengine_seat_id = draft.seatId.trim()
  else delete credentials.volcengine_seat_id
}

/** Match the saved Ark product without sending control-plane requests for relays. */
export function volcengineQuotaProfile(credentials: Record<string, unknown> = {}, customBaseUrlEnabled?: unknown): 'coding' | 'agent' | 'custom' | 'payg' {
  if (credentials.account_mode !== 'coding') return 'payg'
  if (customBaseUrlEnabled != null && customBaseUrlEnabled !== false) return 'custom'
  const protocol = credentials.api_protocol || 'chat_completions'
  if (!['adaptive', 'chat_completions', 'responses', 'anthropic'].includes(protocol as string)) return 'custom'
  const bases = credentials.api_base_urls
  if (bases != null && (typeof bases !== 'object' || Array.isArray(bases))) return 'custom'
  const entries: [string, unknown][] = [['base_url', credentials.base_url], ...Object.entries(bases || {})]
  let product: 'coding' | 'agent' | undefined
  for (const [key, value] of entries) {
    if (key !== 'base_url' && !['chat_completions', 'responses', 'anthropic'].includes(key)) return 'custom'
    if (value == null || value === '') continue
    if (typeof value !== 'string') return 'custom'
    const match = /^https:\/\/ark\.cn-beijing\.volces\.com\/api\/(coding|plan)(\/v3)?\/*$/.exec(value.trim())
    if (!match) return 'custom'
    const selected = key === 'base_url' ? protocol : key
    if (selected !== 'adaptive' && (selected === 'anthropic') === Boolean(match[2])) return 'custom'
    const current = match[1] === 'coding' ? 'coding' : 'agent'
    if (product && product !== current) return 'custom'
    product = current
  }
  return product || 'coding'
}

export function cnQuotaCellVisible(platform: string, accountMode: string, credentials?: Record<string, unknown>, customBaseUrlEnabled?: unknown): boolean {
  if (platform === 'byteplus') return byteplusQuotaProfile(credentials, customBaseUrlEnabled)
  if (platform === 'hunyuan') return tencentQuotaProfile(credentials || { account_mode: accountMode }, customBaseUrlEnabled)
  if (platform === 'volcengine') {
    const profile = volcengineQuotaProfile(credentials || { account_mode: accountMode }, customBaseUrlEnabled)
    return profile === 'coding' || (profile === 'agent' && credentials?.volcengine_plan_edition === 'team')
  }
  if (platform === 'commandcode') return true
  if (platform === 'opencode_go') return accountMode !== 'zen' && accountMode !== 'zen_free'
  return (platform === 'kimi' || platform === 'zhipu' || platform === 'minimax') && accountMode === 'coding'
}

export function cnBalanceCellVisible(platform: string, accountMode: string, credentials: Record<string, unknown> = {}, accountType = 'apikey', customBaseUrlEnabled?: unknown): boolean {
  if (platform !== 'minimax') return (platform === 'kimi' || platform === 'deepseek') && accountMode !== 'coding'
  if (accountMode !== 'payg' || accountType !== 'apikey' || (customBaseUrlEnabled != null && customBaseUrlEnabled !== false)) return false
  const overrides = credentials.api_base_urls
  if (overrides != null && (typeof overrides !== 'object' || Array.isArray(overrides))) return false
  let origin = ''
  for (const [protocol, value] of [['base_url', credentials.base_url], ...Object.entries(overrides || {})]) {
    if (value == null || value === '') continue
    if (!['base_url', 'chat_completions', 'anthropic', 'responses'].includes(protocol as string) || typeof value !== 'string') return false
    const match = /^https:\/\/(api\.minimax\.(?:cn|io))(?:\/v1|\/anthropic)?\/*$/.exec(value.trim())
    if (!match || (origin && origin !== match[1])) return false
    origin = match[1]
  }
  // The admin DTO redacts keys; key-class validation belongs to the backend.
  return origin !== ''
}

/**
 * 将请求头覆写写入 credentials。
 * create 模式：关闭时不写入任何字段；edit 模式：关闭时删除字段（全量替换语义）。
 */
export function applyHeaderOverride(
  credentials: Record<string, unknown>,
  enabled: boolean,
  rows: HeaderOverrideRow[],
  mode: 'create' | 'edit'
): void {
  if (enabled) {
    credentials[HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY] = true
    credentials[HEADER_OVERRIDES_CREDENTIAL_KEY] = buildHeaderOverridesObject(rows)
  } else if (mode === 'edit') {
    delete credentials[HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY]
    delete credentials[HEADER_OVERRIDES_CREDENTIAL_KEY]
  }
}

// ===== OpenAI plan_type (ChatGPT 订阅档位) 手动覆盖 =====

export interface PlanTypeOption {
  value: string
  label: string
  // 兼容 common/Select.vue 的 SelectOption(含索引签名)
  [key: string]: unknown
}

/**
 * plan_type 值的友好显示标签（ChatGPT 档位命名）。
 * 与 PlatformTypeBadge 共用 openAIPlanTypeLabel，避免两处映射漂移；
 * canonical 值 chatgptpro 显示为 Pro 20x，team 显示为 Business Standard。未知值原样返回。
 */
export function planTypeDisplayLabel(value: string): string {
  return openAIPlanTypeLabel(value) || value
}

/**
 * 从凭据里读取 plan_type，仅接受字符串（脏数据 42/true 等一律视为空，
 * 避免被当作合法自定义项保留）。
 */
export function readPlanType(credentials: Record<string, unknown> | undefined | null): string {
  const v = credentials?.plan_type
  return typeof v === 'string' ? v : ''
}

/**
 * 构建 plan_type 下拉选项：清空 + Plus/Pro 20x/Pro 5x/Business Premium/Free 预设。
 * 若当前值是某预设的别名（如 chatgptpro↔Pro 20x），用当前的 canonical 值占据该
 * 标签位（保留 canonical，显示友好标签，避免重复项）；若是完全预设外的值
 * （如 team 或异常值），追加为一项，避免编辑时下拉丢失原值。
 */
export function buildPlanTypeOptions(current: string, clearLabel: string): PlanTypeOption[] {
  const cur = (current || '').trim()
  const curLabel = cur ? planTypeDisplayLabel(cur) : ''
  const presets: PlanTypeOption[] = [
    { value: 'plus', label: 'Plus' },
    { value: 'pro', label: 'Pro 20x' },
    { value: 'prolite', label: 'Pro 5x' },
    { value: 'self_serve_business_prolite', label: 'Business Premium' },
    { value: 'free', label: 'Free' }
  ]
  const opts: PlanTypeOption[] = [{ value: '', label: clearLabel }]
  for (const p of presets) {
    if (cur && p.value !== cur.toLowerCase() && p.label === curLabel) {
      // 当前值是该预设的别名：用 canonical 当前值占位，标签仍显示友好名
      opts.push({ value: cur, label: p.label })
    } else {
      opts.push(p)
    }
  }
  if (cur && !opts.some(o => o.value.toLowerCase() === cur.toLowerCase())) {
    opts.push({ value: cur, label: planTypeDisplayLabel(cur) })
  }
  return opts
}

/**
 * 把手动选择的 plan_type 写入凭据：非空则设置，空则删除该键（清空/自动识别）。
 * 直接修改传入对象并返回。
 */
export function applyPlanType(
  credentials: Record<string, unknown>,
  planType: string
): Record<string, unknown> {
  const pt = (planType || '').trim()
  if (pt) {
    credentials.plan_type = pt
  } else {
    delete credentials.plan_type
  }
  return credentials
}


export function supportsManagementBalanceQuery(platform: string): boolean {
  return ['aihubmix', 'dmxapi', 'crazyrouter', 'crazyrouter_intl', 'therouter'].includes(platform)
}
export function supportsUsageQuery(platform: string): boolean {
  return supportsManagementBalanceQuery(platform) || ['azure_openai', 'github_copilot'].includes(platform)
}
export function usageQueryNeedsUserId(platform: string): boolean {
  return ['dmxapi', 'crazyrouter', 'crazyrouter_intl'].includes(platform)
}
export interface UsageQueryDraft { token: string; userId: string; clearToken: boolean; subscriptionId?: string; region?: string; username?: string }
export function usageQueryDraft(credentials: Record<string, unknown> = {}): UsageQueryDraft {
  // Passwords are never populated, even from legacy plaintext responses.
  return { token: '', userId: typeof credentials.usage_query_user_id === 'string' ? credentials.usage_query_user_id : '', clearToken: false, subscriptionId: String(credentials.usage_query_subscription_id || ''), region: String(credentials.usage_query_region || ''), username: String(credentials.usage_query_username || '') }
}
export function applyUsageQueryCredentials(credentials: Record<string, unknown>, platform: string, draft: UsageQueryDraft): void {
  if (!supportsUsageQuery(platform)) return
  delete credentials.usage_query_token
  delete credentials.usage_query_user_id
  for (const field of ['usage_query_subscription_id', 'usage_query_region', 'usage_query_username']) delete credentials[field]
  if (platform === 'azure_openai') {
    credentials.usage_query_subscription_id = draft.subscriptionId || ''
    credentials.usage_query_region = draft.region || ''
  }
  if (platform === 'github_copilot') credentials.usage_query_username = draft.username || ''
  if (draft.clearToken) credentials.usage_query_token = null
  else if (draft.token !== '') credentials.usage_query_token = draft.token
  if (usageQueryNeedsUserId(platform)) credentials.usage_query_user_id = draft.userId
}


/** Exact domestic enterprise profile; never borrow another region's contract. */
export function tencentQuotaProfile(credentials: Record<string, unknown> = {}, customBaseUrlEnabled?: unknown): boolean {
  if (credentials.account_mode !== 'coding' || (customBaseUrlEnabled != null && customBaseUrlEnabled !== false)) return false
  const protocol = credentials.api_protocol || 'chat_completions'
  if (!['adaptive', 'chat_completions', 'anthropic'].includes(protocol as string)) return false
  const bases = credentials.api_base_urls
  if (bases != null && (typeof bases !== 'object' || Array.isArray(bases))) return false
  const entries = [['base_url', credentials.base_url], ...Object.entries(bases || {})]
  let matched = false
  for (const [key, value] of entries) {
    if (value == null || value === '') continue
    const selected = key === 'base_url' ? protocol : key
    if (!['adaptive', 'chat_completions', 'anthropic'].includes(selected as string) || typeof value !== 'string') return false
    const root = value.trim().replace(/\/+$/, '')
    if (!['https://tokenhub.tencentmaas.com/plan/v3', 'https://tokenhub.tencentmaas.com/plan/anthropic'].includes(root)) return false
    if (selected !== 'adaptive' && (selected === 'anthropic') !== root.endsWith('/anthropic')) return false
    matched = true
  }
  return matched
}
export interface TencentQuotaDraft { secretId: string; secretKey: string; clearSecretId: boolean; clearSecretKey: boolean; region: string; teamId: string }
export function tencentQuotaDraft(credentials: Record<string, unknown> = {}): TencentQuotaDraft {
  return { secretId: '', secretKey: '', clearSecretId: false, clearSecretKey: false,
    region: typeof credentials.tencent_query_region === 'string' ? credentials.tencent_query_region : '',
    teamId: typeof credentials.tencent_query_team_id === 'string' ? credentials.tencent_query_team_id : '' }
}
export function applyTencentQuotaCredentials(credentials: Record<string, unknown>, draft: TencentQuotaDraft): void {
  for (const [field, value, clear] of [
    ['tencent_query_secret_id', draft.secretId, draft.clearSecretId],
    ['tencent_query_secret_key', draft.secretKey, draft.clearSecretKey]
  ] as const) {
    delete credentials[field]
    if (clear) credentials[field] = null
    else if (value.trim()) credentials[field] = value.trim()
  }
  credentials.tencent_query_region = draft.region.trim()
  credentials.tencent_query_team_id = draft.teamId.trim()
}


export function byteplusQuotaProfile(credentials: Record<string, unknown> = {}, customBaseUrlEnabled?: unknown): boolean {
  if (credentials.billing_plan !== 'team' || (customBaseUrlEnabled != null && customBaseUrlEnabled !== false)) return false
  const provider = compatibleProvider('byteplus')
  const mode = provider && compatibleMode(provider, credentials.account_mode)
  if (!mode || mode.mode !== 'coding') return false
  const protocol = credentials.api_protocol || mode.default_protocol
  if (protocol !== 'adaptive' && !mode.endpoints.some(endpoint => endpoint.protocol === protocol)) return false
  const bases = credentials.api_base_urls
  if (bases != null && (typeof bases !== 'object' || Array.isArray(bases))) return false
  for (const [key, value] of [['base_url', credentials.base_url], ...Object.entries(bases || {})]) {
    if (key !== 'base_url' && !mode.endpoints.some(endpoint => endpoint.protocol === key)) return false
    if (value == null || value === '') continue
    if (typeof value !== 'string') return false
    const selected = key === 'base_url' ? protocol : key
    if (!mode.endpoints.some(endpoint => (selected === 'adaptive' || endpoint.protocol === selected) && endpoint.base_url === value.trim().replace(/\/+$/, ''))) return false
  }
  return true
}

/** Reuse the existing cloud-key form; BytePlus edition comes only from billing_plan. */
export function byteplusQuotaDraft(credentials: Record<string, unknown> = {}): VolcengineQuotaDraft {
  return { accessKeyId: '', secretAccessKey: '', edition: 'team',
    seatId: typeof credentials.byteplus_seat_id === 'string' ? credentials.byteplus_seat_id : '',
    clearAccessKeyId: false, clearSecretAccessKey: false }
}
export function applyByteplusQuotaCredentials(credentials: Record<string, unknown>, draft: VolcengineQuotaDraft): void {
  for (const [field, value, clear] of [
    ['byteplus_access_key_id', draft.accessKeyId, draft.clearAccessKeyId],
    ['byteplus_secret_access_key', draft.secretAccessKey, draft.clearSecretAccessKey]
  ] as const) {
    delete credentials[field]
    if (clear) credentials[field] = null
    else if (value.trim()) credentials[field] = value.trim()
  }
  credentials.byteplus_seat_id = draft.seatId.trim()
}
