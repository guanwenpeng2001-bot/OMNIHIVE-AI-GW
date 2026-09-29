import type { GroupPlatform } from '@/types'

export type CcSwitchClientType = 'claude' | 'gemini'

export interface CcSwitchImportConfig {
  app: string
  endpoint: string
  model?: string
}

export interface CcSwitchImportDeeplinkInput {
  baseUrl: string
  platform?: GroupPlatform | null
  clientType: CcSwitchClientType
  providerName: string
  apiKey: string
  usageScript: string
  /**
   * Public models the Key can actually call (its visible set). The deep link only
   * ever carries a model from this list; without it no model is written at all, so
   * the client never starts on a model the Key cannot use.
   */
  visibleModels?: string[] | null
}

// Normalize the stored provider URL before querying its usage endpoint.
export const CC_SWITCH_USAGE_SCRIPT = `({
    request: {
      url: "{{baseUrl}}".replace(/\\/+$/, "").replace(/\\/v1$/, "") + "/v1/usage",
      method: "GET",
      headers: { "Authorization": "Bearer {{apiKey}}" }
    },
    extractor: function(response) {
      const remaining = response?.remaining ?? response?.quota?.remaining ?? response?.balance;
      const unit = response?.unit ?? response?.quota?.unit ?? "USD";
      return {
        isValid: response?.is_active ?? response?.isValid ?? true,
        remaining,
        unit
      };
    }
  })`

function withV1Endpoint(baseUrl: string): string {
  const normalizedBaseUrl = baseUrl.replace(/\/+$/, '')
  return normalizedBaseUrl.endsWith('/v1') ? normalizedBaseUrl : `${normalizedBaseUrl}/v1`
}

/** Media/utility ids must never become a client's default chat model. */
const NON_CHAT_MODEL_PREFIXES = [
  'text-embedding-',
  'tts-',
  'whisper-',
  'dall-e-',
  'gpt-image-',
  'text-moderation-',
  'omni-moderation-',
  'grok-imagine',
  'rerank',
  'stable-diffusion',
  'seedream',
  'seedance',
  'wan-'
]

function isChatModel(model: string): boolean {
  const normalized = model.toLowerCase()
  return !NON_CHAT_MODEL_PREFIXES.some((prefix) => normalized.startsWith(prefix))
}

/** Codex CLI / Codex app speak the Responses API, so only the OpenAI family qualifies. */
function isCodexFamilyModel(model: string): boolean {
  const normalized = model.toLowerCase()
  return /^gpt-/.test(normalized) || /^codex-/.test(normalized) || /^chatgpt-/.test(normalized) || /^o[1-5](-|$)/.test(normalized)
}

/** Grok Build CLI expects an xAI model id. */
function isGrokFamilyModel(model: string): boolean {
  return /^grok(-|$)/i.test(model.trim())
}

/**
 * Picks the import model from the Key's visible set. Returns undefined when the
 * visible set is empty or contains no model the selected client can drive, so the
 * deep link omits `model` instead of pinning an invented constant.
 */
export function preferredCcSwitchModel(
  visibleModels: string[] | null | undefined,
  family: 'codex' | 'grok'
): string | undefined {
  const models = (visibleModels ?? []).map((model) => model.trim()).filter((model) => model && isChatModel(model))
  const matches = family === 'codex' ? models.filter(isCodexFamilyModel) : models.filter(isGrokFamilyModel)
  return matches[0]
}

export function resolveCcSwitchImportConfig(
  platform: GroupPlatform | undefined | null,
  clientType: CcSwitchClientType,
  baseUrl: string,
  visibleModels?: string[] | null
): CcSwitchImportConfig {
  switch (platform || 'anthropic') {
    case 'antigravity':
      return {
        app: clientType === 'gemini' ? 'gemini' : 'claude',
        endpoint: `${baseUrl.replace(/\/+$/, '')}/antigravity`
      }
    case 'openai':
      return {
        app: 'codex',
        endpoint: baseUrl.replace(/\/+$/, ''),
        model: preferredCcSwitchModel(visibleModels, 'codex')
      }
    case 'gemini':
      return {
        app: 'gemini',
        endpoint: baseUrl
      }
    case 'grok':
      return {
        app: 'grokbuild',
        endpoint: withV1Endpoint(baseUrl),
        model: preferredCcSwitchModel(visibleModels, 'grok')
      }
    default:
      return {
        app: 'claude',
        endpoint: baseUrl
      }
  }
}

export function buildCcSwitchImportDeeplink(input: CcSwitchImportDeeplinkInput): string {
  const config = resolveCcSwitchImportConfig(input.platform, input.clientType, input.baseUrl, input.visibleModels)
  const entries: [string, string][] = [
    ['resource', 'provider'],
    ['app', config.app],
    ['name', input.providerName],
    ['homepage', input.baseUrl],
    ['endpoint', config.endpoint],
    ['apiKey', input.apiKey],
    ['configFormat', 'json'],
    ['usageEnabled', 'true'],
    ['usageScript', btoa(input.usageScript)],
    ['usageAutoInterval', '30']
  ]

  if (config.model) {
    entries.splice(2, 0, ['model', config.model])
  }

  return `ccswitch://v1/import?${new URLSearchParams(entries).toString()}`
}
