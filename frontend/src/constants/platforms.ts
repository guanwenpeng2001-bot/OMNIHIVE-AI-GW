import { COMPATIBLE_PROVIDERS, compatibleProvider } from './compatibleProviders'


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
export const NATIVE_PLATFORM_OPTIONS = [
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
  { value: 'qianwen', label: 'Qianwen AI' },
  { value: 'commandcode', label: 'Command Code' },
  { value: 'qianfan', label: 'Qianfan' },
  { value: 'hunyuan', label: 'Hunyuan' },
  { value: 'mistral', label: 'Mistral' },
  { value: 'openrouter', label: 'OpenRouter' },
  { value: 'bedrock', label: 'Amazon Bedrock' },
  { value: 'volcengine', label: 'Volcengine Ark' },
  { value: 'cursor', label: 'Cursor', supportsOAuth: true }
] as const satisfies readonly PlatformOption[]

export const CONCRETE_PLATFORM_OPTIONS: readonly PlatformOption[] = [
  ...NATIVE_PLATFORM_OPTIONS,
  ...COMPATIBLE_PROVIDERS.map(provider => ({ value: provider.id, label: provider.label }))
]

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
  return !compatibleProvider(platform) && NATIVE_PLATFORM_OPTIONS.some(option => option.value === platform) &&
    !['qianwen', 'commandcode', 'cursor', 'qianfan', 'hunyuan', 'mistral', 'openrouter', 'bedrock', 'volcengine'].includes(platform)
}

export type AccountPlatform = typeof CONCRETE_PLATFORM_OPTIONS[number]['value']
export type GroupPlatform = typeof GROUP_PLATFORM_OPTIONS[number]['value']

/** Every concrete descriptor supports catalog previews; the Go contract test checks this projection. */
export function platformSupportsModelCatalog(platform: string): boolean {
  const provider = compatibleProvider(platform.toLowerCase())
  if (provider) return provider.modes.some(mode => mode.discovery === 'openai_models')
  return NATIVE_PLATFORM_OPTIONS.some(option => option.value === platform.toLowerCase())
}

/** Platform ids a display-name lookup must cover: hosting aliases (bedrock,
 *  vertex), concrete products and the composite route target. */
const PLATFORM_LABEL_OPTIONS: readonly PlatformOption[] = [
  ...QUOTA_PLATFORM_OPTIONS,
  { value: 'composite', label: 'Composite' }
]

/**
 * Authoritative platform display names.
 *
 * Every surface — admin or user, filter, table or badge — resolves a platform
 * name through this function. The names themselves live in the single i18n
 * table `admin.accounts.platforms.*`; the catalog label is only the English
 * fallback for callers that have no translator (badges inside pure utilities).
 * A catalog label must therefore stay locale-neutral: localized names come from
 * the i18n table, never from the static label.
 */
export function getPlatformLabel(platform: string, translate?: (key: string) => string): string {
  const option = PLATFORM_LABEL_OPTIONS.find(option => option.value === platform)
  if (!option) return platform || 'API'
  if (translate) {
    const key = `admin.accounts.platforms.${platform}`
    const label = translate(key)
    if (label !== key) return label
  }
  return option.label
}

/** Protocol families the platform selector groups by. Bedrock serves the
 *  Anthropic Messages protocol and Vertex/Antigravity the Google one; every
 *  other product entry is reached through the OpenAI-compatible trunk, except
 *  Cursor, which speaks its own relay protocol — that is why the catch-all
 *  family is labelled "OpenAI compatible / Other" instead of claiming every
 *  member is OpenAI. */
export const PLATFORM_FAMILY_MEMBERS = {
  anthropic: ['anthropic', 'bedrock'],
  google: ['gemini', 'antigravity', 'vertex']
} as const

export type PlatformFamily = keyof typeof PLATFORM_FAMILY_MEMBERS | 'openaiCompatible'

export function platformFamily(platform: string): PlatformFamily {
  const declared = (Object.keys(PLATFORM_FAMILY_MEMBERS) as Array<keyof typeof PLATFORM_FAMILY_MEMBERS>)
    .find(family => (PLATFORM_FAMILY_MEMBERS[family] as readonly string[]).includes(platform))
  return declared ?? 'openaiCompatible'
}

/** Explicit defaults for providers with a verified chat-only runtime contract. */
export const CHAT_PLATFORM_BASE_URLS: Record<string, string> = {
  mistral: 'https://api.mistral.ai/v1',
  openrouter: 'https://openrouter.ai/api/v1',
  hunyuan: 'https://tokenhub.tencentmaas.com/v1',
  qianfan: 'https://qianfan.baidubce.com/v2',
  volcengine: 'https://ark.cn-beijing.volces.com/api/v3'
}
export function isChatOnlyPlatform(platform: string): boolean {
  return Object.prototype.hasOwnProperty.call(CHAT_PLATFORM_BASE_URLS, platform)
}
