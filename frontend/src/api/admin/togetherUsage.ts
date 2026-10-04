import { apiClient } from '../client'
import type { Account } from '@/types'
import { paygBalanceAllowed } from '@/constants/compatibleProviders'

export interface TogetherBillingLine {
  product_name: string
  quantity: string
  unit_price: string
  cost: string
  pricing_dimensions: Record<string, unknown>
  attributes: Record<string, unknown>
}
export interface TogetherBillingUsage {
  object: 'list'
  scope: 'organization'
  organization_id: string
  billing_period: string
  granularity: 'day'
  currency: 'USD'
  earliest_window_start: string | null
  latest_window_end: string | null
  data: { date: string; start_time: string; end_time: string; line_items: TogetherBillingLine[] }[]
  next_cursor: string | null
  pages_fetched: number
  partial: boolean
}
export interface TogetherBillingResult {
  provider: 'together'
  kind: 'billing_usage'
  success: boolean
  persisted: false
  fetched_at: number
  unsupported?: boolean
  reason?: string
  status_code?: number
  usage?: TogetherBillingUsage
}
export async function queryTogetherUsage(id: number, month: string): Promise<TogetherBillingResult> {
  const { data } = await apiClient.get<TogetherBillingResult>(`/admin/cn-providers/accounts/${id}/usage`, {
    params: { month, granularity: 'day', timezone: undefined }
  })
  return data
}

/** Mount only official preset payg accounts; stored credentials remain authoritative. */
export function togetherBillingVisible(account: Account): boolean {
  const credentials = account.credentials ?? {}
  if (account.platform !== 'together' || account.type !== 'apikey' || !paygBalanceAllowed(account.platform, credentials)) return false
  if (account.credentials_status?.has_api_key === false) return false
  if (account.extra?.custom_base_url_enabled != null && account.extra.custom_base_url_enabled !== false) return false
  for (const field of ['account_mode', 'api_protocol', 'base_url', 'api_key']) {
    if (field in credentials && typeof credentials[field] !== 'string') return false
  }
  if (!['', 'payg'].includes(String(credentials.account_mode ?? '').trim())) return false
  if (!['', 'adaptive', 'chat_completions'].includes(String(credentials.api_protocol ?? '').trim())) return false
  const approved = (value: unknown) => {
    if (typeof value !== 'string') return false
    if (!value.trim()) return true
    const parts = /^https:\/\/([^/]+)(\/v1\/*)$/.exec(value.trim())
    return parts?.[1].toLowerCase() === 'api.together.ai'
  }
  if (credentials.base_url != null && !approved(credentials.base_url)) return false
  if ('api_base_urls' in credentials) {
    const bases = credentials.api_base_urls
    if (!bases || typeof bases !== 'object' || Array.isArray(bases)) return false
    if (Object.entries(bases).some(([protocol, value]) => protocol !== 'chat_completions' || !approved(value))) return false
  }
  return true
}

export function validTogetherBillingResult(result: TogetherBillingResult, month: string): boolean {
  const usage = result?.usage
  const decimal = (value: unknown) => typeof value === 'string' && /^-?\d+(\.\d+)?$/.test(value)
  const record = (value: unknown) => !!value && typeof value === 'object' && !Array.isArray(value)
  return result?.provider === 'together' && result.kind === 'billing_usage' && result.persisted === false &&
    usage?.scope === 'organization' && usage.object === 'list' && usage.currency === 'USD' && usage.billing_period === month && usage.granularity === 'day' &&
    typeof usage.organization_id === 'string' && !!usage.organization_id && typeof usage.partial === 'boolean' &&
    Number.isInteger(usage.pages_fetched) && usage.pages_fetched > 0 &&
    (usage.next_cursor === null || typeof usage.next_cursor === 'string' && !!usage.next_cursor) && usage.partial === (usage.next_cursor !== null) &&
    Array.isArray(usage.data) && usage.data.every(window => typeof window?.date === 'string' && window.date.startsWith(`${month}-`) &&
      Number.isFinite(Date.parse(window.start_time)) && Number.isFinite(Date.parse(window.end_time)) && Array.isArray(window.line_items) &&
      window.line_items.every(line => typeof line?.product_name === 'string' && !!line.product_name && decimal(line.quantity) && decimal(line.unit_price) && decimal(line.cost) && record(line.pricing_dimensions) && record(line.attributes)))
}
