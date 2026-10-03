import { compatibleMode, compatibleProvider } from '@/constants/compatibleProviders'
import type { Account } from '@/types'

// Only public, inference-key balance contracts implemented by the backend.
const supported = new Set(['novita', 'ppio', 'siliconflow_cn', 'siliconflow_global', 'shengsuanyun', 'soleapi'])

export function compatibleBalanceVisible(account: Account): boolean {
  if (account.type !== 'apikey' || !supported.has(account.platform)) return false
  const provider = compatibleProvider(account.platform)
  if (!provider) return false
  const credentials = account.credentials ?? {}
  const mode = compatibleMode(provider, credentials.account_mode)
  if (!mode || mode.mode !== 'payg') return false
  const protocol = credentials.api_protocol
  if (protocol && protocol !== 'adaptive' && !mode.endpoints.some(endpoint => endpoint.protocol === protocol)) return false
  if (account.extra?.custom_base_url_enabled === true) return false
  const approved = (value: unknown): boolean => {
    if (value == null || value === '') return true
    if (typeof value !== 'string') return false
    if (!value.trim()) return true
    try {
      const base = new URL(value.trim())
      return base.protocol === 'https:' && !base.username && !base.password && !base.search && !base.hash &&
        mode.endpoints.some(endpoint => {
          if (!endpoint.base_url) return false
          const preset = new URL(endpoint.base_url)
          return base.host === preset.host && base.pathname.replace(/\/+$/, '') === preset.pathname.replace(/\/+$/, '')
        })
    } catch { return false }
  }
  if (!approved(credentials.base_url)) return false
  const bases = credentials.api_base_urls
  if (bases != null && (typeof bases !== 'object' || Array.isArray(bases))) return false
  return bases == null || Object.values(bases).every(approved)
}
