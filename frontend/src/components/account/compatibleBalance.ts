import { supportsUsageQuery, usageQueryNeedsUserId } from './credentialsBuilder'
import { compatibleMode, compatibleProvider, paygBalanceAllowed } from '@/constants/compatibleProviders'
import type { Account } from '@/types'
import type { CompatibleUsageReason } from '@/api/admin/cnProviders'

// Approved inference hosts; TeamoRouter billing uses the corresponding apex
// host selected exclusively by the backend, never a credential-supplied URL.
const walletHosts: Record<string, string> = {
  novita: 'api.novita.ai', ppio: 'api.ppio.com', siliconflow_cn: 'api.siliconflow.cn',
  siliconflow_global: 'api.siliconflow.com', shengsuanyun: 'router.shengsuanyun.com',
  soleapi: 'soleapi.com', atlascloud: 'api.atlascloud.ai',
  stepfun_api: 'api.stepfun.com', stepfun_api_intl: 'api.stepfun.ai',
  teamorouter: 'api.teamorouter.cn', teamorouter_com: 'api.teamorouter.com',
  sudocode_chat: 'api.sudocode.chat'
}
export type CompatibleUsageCapability = { kind: 'wallet_balance' | 'account_credits' | 'consumed_usage' | 'unsupported'; reason?: CompatibleUsageReason }

/** Mirrors the server's fixed-origin eligibility; never derives query URLs from credentials. */
export function compatibleUsageCapability(account: Account): CompatibleUsageCapability {
  const unsupported = (reason: CompatibleUsageReason): CompatibleUsageCapability => ({ kind: 'unsupported', reason })
  const provider = compatibleProvider(account.platform)
  if (!provider) return unsupported('no_verified_query')
  if (account.type !== 'apikey') return unsupported('invalid_credentials')
  const credentials = account.credentials ?? {}
  if (!paygBalanceAllowed(account.platform, credentials)) return unsupported('mode_unsupported')
  const management = supportsUsageQuery(account.platform)
  for (const field of ['account_mode', 'api_protocol', 'base_url', 'api_key']) {
    if (management && field === 'api_key') continue
    if (field in credentials && typeof credentials[field] !== 'string') return unsupported('invalid_credentials')
  }
  const mode = compatibleMode(provider, (credentials.account_mode as string | undefined)?.trim())
  if (!mode || mode.mode !== 'payg') return unsupported('mode_unsupported')
  const host = account.platform === 'qiniu' ? 'api.qnaigc.com' : walletHosts[account.platform]
  if (!host && !management) return unsupported('no_verified_query')
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
  if (!management && mode.endpoints.some(endpoint => { try { const preset = new URL(endpoint.base_url || ''); return preset.protocol !== 'https:' || preset.host !== host } catch { return true } })) return unsupported('custom_origin')
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
  if (management) {
    const kind = account.platform === 'therouter' ? 'account_credits' : 'wallet_balance'
    const userId = credentials.usage_query_user_id
    const token = credentials.usage_query_token
    const hasToken = account.credentials_status?.has_usage_query_token ?? (typeof token === 'string' && token.length > 0)
    if (!hasToken || (usageQueryNeedsUserId(account.platform) && (userId == null || userId === '' || (typeof userId === 'string' && !userId.trim())))) return { kind, reason: 'missing_query_credentials' }
    if ((token != null && (typeof token !== 'string' || !/^[!-~]+$/.test(token))) || (usageQueryNeedsUserId(account.platform) && (typeof userId !== 'string' || !/^[!-~]+$/.test(userId)))) return { kind, reason: 'invalid_credentials' }
    return { kind }
  }
  // List DTOs redact keys; the server resolves stored secrets by account ID.
  if (account.credentials_status?.has_api_key === false) return unsupported('invalid_credentials')
  return { kind: account.platform === 'qiniu' ? 'consumed_usage' : 'wallet_balance' }
}

export function compatibleBalanceVisible(account: Account): boolean {
  return ['wallet_balance', 'account_credits'].includes(compatibleUsageCapability(account).kind)
}

export function compatibleFailureReason(result: { reason?: CompatibleUsageReason; status_code?: number; unsupported?: boolean }): CompatibleUsageReason {
  if (result.reason) return result.reason
  if (result.status_code === 401) return 'invalid_credentials'
  if (result.status_code === 403) return 'permission_denied'
  if (result.status_code === 429) return 'rate_limited'
  return result.unsupported ? 'no_verified_query' : 'upstream_error'
}
