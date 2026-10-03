export type CompatibleProtocol = 'chat_completions' | 'anthropic' | 'responses'
export interface CompatibleEndpoint {
  protocol: CompatibleProtocol
  base_url?: string
  path?: string
  auth: 'bearer' | 'x_api_key' | 'api_key_header'
}
export interface CompatibleMode {
  mode: 'payg' | 'coding'
  default_protocol: CompatibleProtocol
  endpoints: CompatibleEndpoint[]
  bridges: string[]
  discovery: 'none' | 'openai_models'
  discovery_protocol?: CompatibleProtocol
  capability_profile: string
}
export interface CompatibleProvider {
  id: string
  label: string
  label_zh: string
  base_source: 'preset' | 'account'
  default_mode: 'payg' | 'coding'
  modes: CompatibleMode[]
  evidence_url: string
  evidence_observed: string
}

// The backend validates these same files at startup. No second provider list.
const tables = import.meta.glob<CompatibleProvider[]>(
  '../../../backend/internal/domain/data/compatible_providers/*.json',
  { eager: true, import: 'default' }
)
export const COMPATIBLE_PROVIDERS: readonly CompatibleProvider[] = Object.keys(tables).sort()
  .flatMap(path => tables[path]).sort((left, right) => left.id.localeCompare(right.id))
export function compatibleProvider(platform: string): CompatibleProvider | undefined {
  return COMPATIBLE_PROVIDERS.find(provider => provider.id === platform)
}
export function compatibleMode(provider: CompatibleProvider, mode?: unknown): CompatibleMode | undefined {
  return provider.modes.find(entry => entry.mode === (mode == null || mode === '' ? provider.default_mode : mode))
}
export function compatibleProtocolLabel(protocol: string): string {
  return ({ chat_completions: 'Chat Completions', anthropic: 'Messages', responses: 'Responses', adaptive: 'Adaptive' } as Record<string, string>)[protocol] ?? protocol
}
export function compatibleAuthLabel(auth: CompatibleEndpoint['auth']): string {
  return { bearer: 'Authorization: Bearer …', x_api_key: 'x-api-key: …', api_key_header: 'api-key: …' }[auth]
}

/** Preserve account overrides and unrelated credentials; presets remain server defaults.
 * In particular, never materialize official per-protocol bases beside a custom base. */
export function buildCompatibleCredentials(provider: CompatibleProvider, draft: Record<string, unknown>): Record<string, unknown> {
  const mode = compatibleMode(provider, draft.account_mode)
  if (!mode) throw new Error('Select an available account mode')
  const protocol = draft.api_protocol == null || draft.api_protocol === '' ? 'adaptive' : draft.api_protocol
  if (protocol !== 'adaptive' && !mode.endpoints.some(endpoint => endpoint.protocol === protocol)) {
    throw new Error('Select an available native protocol')
  }
  if (provider.base_source === 'account' && (typeof draft.base_url !== 'string' || !draft.base_url.trim())) {
    throw new Error('Enter the resource base URL')
  }
  return { ...draft, account_mode: mode.mode, api_protocol: protocol }
}

export function compatibleDiscoveryDisabled(provider: CompatibleProvider | undefined, credentials: Record<string, unknown>): boolean {
  return !!provider && compatibleMode(provider, credentials.account_mode)?.discovery !== 'openai_models'
}
