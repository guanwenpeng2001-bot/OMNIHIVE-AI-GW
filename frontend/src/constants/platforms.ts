
export interface PlatformOption<T extends string = string> {
  value: T
  label: string
  supportsOAuth?: boolean
}

/** Hosting is an account type; legacy wire values remain readable during rollout. */
export const HOSTED_ACCOUNT_OPTIONS = [
  { platform: 'anthropic', type: 'bedrock', label: 'Amazon Bedrock' },
  { platform: 'gemini', type: 'service_account', label: 'Vertex AI' },
  { platform: 'anthropic', type: 'service_account', label: 'Vertex AI' }
] as const

export function canonicalPlatform(platform: string): string {
  if (platform === 'bedrock') return 'anthropic'
  if (platform === 'vertex') return 'gemini'
  return platform
}

export function hostedAccountType(platform: string): string {
  if (platform === 'bedrock') return 'bedrock'
  if (platform === 'vertex') return 'service_account'
  return ''
}

/** Stable budget key used by quota forms; hosting subtypes must not collapse. */
export function hostedQuotaKey(platform: string, accountType?: string): string {
  if (platform === 'anthropic' && accountType === 'bedrock') return 'bedrock'
  if (platform === 'gemini' && accountType === 'service_account') return 'vertex'
  return platform
}

/**
 * Concrete upstream platforms supported by accounts and request routing.
 * Keep platform selectors derived from this catalog so newly added providers
 * do not silently disappear from list filters.
 */
export const CONCRETE_PLATFORM_OPTIONS = [
  { value: 'anthropic', label: 'Anthropic', supportsOAuth: true },
  { value: 'openai', label: 'OpenAI', supportsOAuth: true },
  { value: 'gemini', label: 'Gemini', supportsOAuth: true },
  { value: 'antigravity', label: 'Antigravity', supportsOAuth: true },
  { value: 'grok', label: 'Grok', supportsOAuth: true },
  { value: 'kimi', label: 'Kimi' },
  { value: 'zhipu', label: 'Zhipu GLM' },
  { value: 'deepseek', label: 'DeepSeek' },
  { value: 'minimax', label: 'MiniMax' },
  { value: 'opencode_go', label: 'OpenCode' },
  { value: 'qianwen', label: '千问AI' },
  { value: 'commandcode', label: 'Command Code' },
  { value: 'qianfan', label: 'Qianfan' },
  { value: 'hunyuan', label: 'Hunyuan' },
  { value: 'mistral', label: 'Mistral' },
  { value: 'bedrock', label: 'Amazon Bedrock' },
  { value: 'volcengine', label: 'Volcengine Ark' },
  { value: 'cursor', label: 'Cursor', supportsOAuth: true }
] as const satisfies readonly PlatformOption[]

/** Budget identities remain stable even after the product identity migrates. */
export const QUOTA_PLATFORM_OPTIONS = [
  ...CONCRETE_PLATFORM_OPTIONS,
  { value: 'vertex', label: 'Vertex AI' }
] as const satisfies readonly PlatformOption[]

/** Product filters exclude legacy hosting aliases while account editors and
 * quota budgets retain the full compatibility catalog during staged rollout. */
export const PRODUCT_PLATFORM_OPTIONS = CONCRETE_PLATFORM_OPTIONS.filter(option => !hostedAccountType(option.value))

export const PRODUCT_GROUP_PLATFORM_OPTIONS = [
  ...PRODUCT_PLATFORM_OPTIONS,
  { value: 'composite', label: 'Composite', supportsOAuth: true }
] as const satisfies readonly PlatformOption[]

/** Platforms that can own a group. */
export const GROUP_PLATFORM_OPTIONS = [
  ...CONCRETE_PLATFORM_OPTIONS,
  { value: 'composite', label: 'Composite', supportsOAuth: true }
] as const satisfies readonly PlatformOption[]

/** OAuth-only group filters follow account capabilities, including composite routes. */
export function platformSupportsOAuth(platform: string): boolean {
  return GROUP_PLATFORM_OPTIONS.some(option => option.value === platform && 'supportsOAuth' in option && option.supportsOAuth)
}

/** No verified pricing provider mapping exists for these platforms. */
export function platformSupportsPricingSync(platform: string): boolean {
  return CONCRETE_PLATFORM_OPTIONS.some(option => option.value === platform) &&
    !['qianwen', 'commandcode', 'cursor', 'qianfan', 'hunyuan', 'mistral', 'bedrock', 'volcengine'].includes(platform)
}

export type AccountPlatform = typeof CONCRETE_PLATFORM_OPTIONS[number]['value']
export type GroupPlatform = typeof GROUP_PLATFORM_OPTIONS[number]['value']

/** Every concrete descriptor supports catalog previews; the Go contract test checks this projection. */
export function platformSupportsModelCatalog(platform: string): boolean {
  return CONCRETE_PLATFORM_OPTIONS.some(option => option.value === platform.toLowerCase())
}

/** Shared display labels; callers with an active locale pass their translator. */
export function getPlatformLabel(platform: string, translate?: (key: string) => string): string {
  const option = GROUP_PLATFORM_OPTIONS.find(option => option.value === platform)
  if (!option) return platform || 'API'
  if (translate) {
    const key = `admin.accounts.platforms.${platform}`
    const label = translate(key)
    if (label !== key) return label
  }
  return option.label
}

/** Explicit defaults for providers with a verified chat-only runtime contract. */
export const CHAT_PLATFORM_BASE_URLS: Record<string, string> = {
  mistral: 'https://api.mistral.ai/v1',
  hunyuan: 'https://api.hunyuan.cloud.tencent.com/v1',
  qianfan: 'https://qianfan.baidubce.com/v2',
  volcengine: 'https://ark.cn-beijing.volces.com/api/v3'
}
export function isChatOnlyPlatform(platform: string): boolean {
  return Object.prototype.hasOwnProperty.call(CHAT_PLATFORM_BASE_URLS, platform)
}
