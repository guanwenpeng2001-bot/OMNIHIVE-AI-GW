import { apiClient } from '../client'
import type { Account } from '@/types'
import { compatibleMode, compatibleProvider } from '@/constants/compatibleProviders'

export const managementUsageReasons = ['missing_query_credentials', 'invalid_credentials', 'permission_denied', 'rate_limited', 'not_found', 'upstream_error', 'invalid_response', 'mode_unsupported', 'no_verified_query', 'custom_origin', 'url_rejected', 'proxy_unavailable', 'pagination_limit', 'report_failed'] as const
export type ManagementUsageReason = typeof managementUsageReasons[number]
export interface ManagementBillingMonth { year: number; month: number }
export interface AzureQuotaItem {
  name: { value: string; localizedValue: string | null }
  currentValue: number | null
  limit: number | null
  unit: string | null
}
export const githubBillingTextFields = ['product', 'sku', 'model', 'unitType'] as const
export const githubBillingNumberFields = ['pricePerUnit', 'grossQuantity', 'grossAmount', 'discountQuantity', 'discountAmount', 'netQuantity', 'netAmount'] as const
export type GitHubBillingItem = Record<typeof githubBillingTextFields[number], string | null> & Record<typeof githubBillingNumberFields[number], number | null>
export interface GitHubBillingReport {
  report: 'premium_request' | 'ai_credit'
  success: boolean
  reason?: ManagementUsageReason
  status_code?: number
  timePeriod?: { year: number; month: number | null }
  user?: string
  usageItems: GitHubBillingItem[] | null
}
export interface ManagementUsageResult {
  provider: string
  kind: 'subscription_regional_quota' | 'personal_billing_usage' | 'unsupported'
  scope: 'subscription_region' | 'personal_account' | ''
  success: boolean
  partial: boolean
  reason?: ManagementUsageReason
  status_code?: number
  fetched_at: number
  persisted: boolean
  quota?: { subscription_id: string; region: string; value: AzureQuotaItem[]; pages_fetched: number; has_more: boolean }
  billing?: ManagementBillingMonth & { username: string; reports: GitHubBillingReport[] }
}

/** The integration owner dispatches this existing admin route by stored platform. */
export async function queryManagementUsage(id: number, month?: ManagementBillingMonth): Promise<ManagementUsageResult> {
  const { data } = await apiClient.get<ManagementUsageResult>(`/admin/cn-providers/accounts/${id}/usage`, {
    // Prevent the global GET interceptor injecting an unsupported timezone.
    params: { ...month, timezone: undefined }
  })
  return data
}

export function managementFailureReason(value: { reason?: string; status_code?: number }): ManagementUsageReason {
  if (managementUsageReasons.includes(value.reason as ManagementUsageReason)) return value.reason as ManagementUsageReason
  if (value.status_code === 401) return 'invalid_credentials'
  if (value.status_code === 403) return 'permission_denied'
  if (value.status_code === 404) return 'not_found'
  if (value.status_code === 429) return 'rate_limited'
  return 'upstream_error'
}

const object = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value)
const optionalText = (value: unknown) => value == null || typeof value === 'string'
const optionalNumber = (value: unknown) => value == null || (typeof value === 'number' && Number.isFinite(value))

/** Runtime validation protects display from malformed or cross-scope API data. */
export function validManagementUsage(value: unknown, provider: string, requested?: ManagementBillingMonth): value is ManagementUsageResult {
  if (!object(value) || value.provider !== provider || typeof value.success !== 'boolean' || typeof value.partial !== 'boolean' || value.persisted !== false) return false
  if (provider === 'azure_openai') {
    if (value.kind !== 'subscription_regional_quota' || value.scope !== 'subscription_region') return false
    const quota = value.quota
    if (!quota) return !value.success && !value.partial
    return object(quota) && typeof quota.subscription_id === 'string' && typeof quota.region === 'string' &&
      typeof quota.pages_fetched === 'number' && Number.isInteger(quota.pages_fetched) && quota.pages_fetched > 0 && quota.pages_fetched <= 10 &&
      typeof quota.has_more === 'boolean' && !(value.success && (value.partial || quota.has_more)) && Array.isArray(quota.value) && quota.value.every(row =>
        object(row) && object(row.name) && typeof row.name.value === 'string' && optionalText(row.name.localizedValue) && optionalNumber(row.currentValue) && optionalNumber(row.limit) && optionalText(row.unit))
  }
  if (provider !== 'github_copilot' || value.kind !== 'personal_billing_usage' || value.scope !== 'personal_account') return false
  const billing = value.billing
  if (!billing) return !value.success && !value.partial
  if (!object(billing) || billing.year !== requested?.year || billing.month !== requested?.month || typeof billing.username !== 'string' || !Array.isArray(billing.reports) || billing.reports.length !== 2) return false
  let successes = 0
  for (const [index, report] of billing.reports.entries()) {
    if (!object(report) || report.report !== ['premium_request', 'ai_credit'][index] || typeof report.success !== 'boolean') return false
    if (!report.success) { if (report.usageItems != null) return false; continue }
    successes++
    if (!object(report.timePeriod) || report.timePeriod.year !== billing.year || (report.timePeriod.month != null && report.timePeriod.month !== billing.month) ||
      typeof report.user !== 'string' || report.user.toLowerCase() !== billing.username.toLowerCase() || !Array.isArray(report.usageItems) ||
      !report.usageItems.every(row => object(row) && githubBillingTextFields.every(field => optionalText(row[field])) && githubBillingNumberFields.every(field => optionalNumber(row[field])))) return false
  }
  return value.success === (successes === 2) && value.partial === (successes === 1)
}

export function managementUsageVisible(account: Account): boolean {
  if (!['azure_openai', 'github_copilot'].includes(account.platform) || account.type !== 'apikey') return false
  const provider = compatibleProvider(account.platform)
  return !!provider && compatibleMode(provider, account.credentials?.account_mode)?.mode === 'payg'
}
