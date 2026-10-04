import { compatibleMode, compatibleProvider, paygBalanceAllowed } from '@/constants/compatibleProviders'
import type { Account } from '@/types'
import type { CompatibleUsageReason } from '@/api/admin/cnProviders'

const walletHosts: Record<string, string> = {
  novita: 'api.novita.ai', ppio: 'api.ppio.com', siliconflow_cn: 'api.siliconflow.cn',
  siliconflow_global: 'api.siliconflow.com', shengsuanyun: 'router.shengsuanyun.com',
  soleapi: 'soleapi.com', atlascloud: 'api.atlascloud.ai'
}
export type CompatibleUsageCapability = { kind: 'wallet_balance' | 'consumed_usage' | 'unsupported'; reason?: CompatibleUsageReason }

/** Mirrors the server's fixed-origin eligibility; never derives query URLs from credentials. */
export function compatibleUsageCapability(account: Account): CompatibleUsageCapability {
  const unsupported = (reason: CompatibleUsageReason): CompatibleUsageCapability => ({ kind: 'unsupported', reason })
  const provider = compatibleProvider(account.platform)
  if (!provider) return unsupported('no_verified_query')
  if (account.type !== 'apikey') return unsupported('invalid_credentials')
  const credentials = account.credentials ?? {}
  if (!paygBalanceAllowed(account.platform, credentials)) return unsupported('mode_unsupported')
  for (const field of ['account_mode', 'api_protocol', 'base_url', 'api_key']) {
    if (field in credentials && typeof credentials[field] !== 'string') return unsupported('invalid_credentials')
  }
  const mode = compatibleMode(provider, (credentials.account_mode as string | undefined)?.trim())
  if (!mode || mode.mode !== 'payg') return unsupported('mode_unsupported')
  const host = account.platform === 'qiniu' ? 'api.qnaigc.com' : walletHosts[account.platform]
  if (!host) return unsupported('no_verified_query')
  const protocol = (credentials.api_protocol as string | undefined)?.trim()
  if (protocol && protocol !== 'adaptive' && !mode.endpoints.some(endpoint => endpoint.protocol === protocol)) return unsupported('invalid_credentials')
  const relay = account.extra?.custom_base_url_enabled
  if (relay != null && relay !== false) return unsupported('custom_origin')
  const approved = (value: unknown): boolean => {
    if (typeof value !== 'string') return false
    if (!value.trim()) return true
    try {
      const raw = value.trim()
      // URL normalizes dot segments, backslashes and default ports; the server
      // compares the supplied host/path, so these must not become preset URLs.
      const parts = /^https:\/\/([^/?#]+)([^?#]*)$/.exec(raw)
      if (!parts) return false
      const base = new URL(raw)
      return base.protocol === 'https:' && !base.username && !base.password && !base.search && !base.hash &&
        mode.endpoints.some(endpoint => {
          if (!endpoint.base_url) return false
          const preset = new URL(endpoint.base_url)
          return parts[1].toLowerCase() === preset.host && parts[2].replace(/\/+$/, '') === preset.pathname.replace(/\/+$/, '')
        })
    } catch { return false }
  }
  if (mode.endpoints.some(endpoint => { try { const preset = new URL(endpoint.base_url || ''); return preset.protocol !== 'https:' || preset.host !== host } catch { return true } })) return unsupported('custom_origin')
  if (credentials.base_url != null && !approved(credentials.base_url)) return unsupported('custom_origin')
  const bases = credentials.api_base_urls
  if ('api_base_urls' in credentials) {
    if (!bases || typeof bases !== 'object' || Array.isArray(bases)) return unsupported('custom_origin')
    for (const [protocol, value] of Object.entries(bases)) {
      const endpoint = mode.endpoints.find(entry => entry.protocol === protocol)
      if (!endpoint || !approved(value)) return unsupported('custom_origin')
      if (typeof value === 'string' && value.trim() && new URL(value.trim()).pathname.replace(/\/+$/, '') !== new URL(endpoint.base_url || '').pathname.replace(/\/+$/, '')) return unsupported('custom_origin')
    }
  }
  // List DTOs redact keys; the server resolves stored secrets by account ID.
  if (account.credentials_status?.has_api_key === false) return unsupported('invalid_credentials')
  return { kind: account.platform === 'qiniu' ? 'consumed_usage' : 'wallet_balance' }
}

export function compatibleBalanceVisible(account: Account): boolean {
  return compatibleUsageCapability(account).kind === 'wallet_balance'
}

export function compatibleFailureReason(result: { reason?: CompatibleUsageReason; status_code?: number; unsupported?: boolean }): CompatibleUsageReason {
  if (result.reason) return result.reason
  if (result.status_code === 401) return 'invalid_credentials'
  if (result.status_code === 403) return 'permission_denied'
  if (result.status_code === 429) return 'rate_limited'
  return result.unsupported ? 'no_verified_query' : 'upstream_error'
}
