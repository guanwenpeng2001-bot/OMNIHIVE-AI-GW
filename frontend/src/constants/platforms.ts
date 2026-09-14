
export interface PlatformOption<T extends string = string> {
  value: T
  label: string
  supportsOAuth?: boolean
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
  { value: 'cursor', label: 'Cursor', supportsOAuth: true }
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
    !['qianwen', 'commandcode', 'cursor'].includes(platform)
}

export type AccountPlatform = typeof CONCRETE_PLATFORM_OPTIONS[number]['value']
export type GroupPlatform = typeof GROUP_PLATFORM_OPTIONS[number]['value']
