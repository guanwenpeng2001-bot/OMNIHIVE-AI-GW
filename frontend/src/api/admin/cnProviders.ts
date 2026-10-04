/**
 * Admin CN providers (Kimi / Zhipu / DeepSeek) API endpoints.
 * Coding-plan rolling-window quota probe + payg balance probe.
 */

import { apiClient } from '../client'

/** 滚动用量窗口档（5 小时 / 每周），对齐后端 service.CNQuotaTier。 */
export interface CNQuotaTier {
  window: '5h' | 'weekly' | 'monthly'
  used_percent: number
  reset_at?: string
}

/** Coding Plan 额度探测结果（kimi / zhipu），对齐后端 CNProviderQuotaProbeResult。 */
export interface CNProviderQuotaProbeResult {
  unsupported?: boolean
  credits?: Record<string, number>
  provider: string
  source?: string
  success: boolean
  credential_valid: boolean
  tiers?: CNQuotaTier[]
  plan_level?: string
  status_code?: number
  fetched_at: number
  persisted: boolean
  error?: string
}

/** 单币种余额明细（deepseek 双币种账号含 CNY + USD 两条）。 */
export interface CNProviderBalanceEntry {
  currency: string
  balance: number
}

/** Native and compatible payg balance results; failed/unsupported queries omit unknown amounts. */
export interface CNProviderBalanceResult {
  kind?: 'wallet_balance' | 'project_budget' | 'unsupported'
  reason?: CompatibleUsageReason
  project_budget?: CompatibleProjectBudget
  scope?: 'account' | 'account_in_project'
  provider: string
  success: boolean
  unsupported?: boolean
  /** 主币种余额（balances 首条，兼容单币种展示）。 */
  balance?: number
  currency?: string
  /** 多币种明细；缺省时按主币种展示。 */
  balances?: CNProviderBalanceEntry[]
  available?: boolean
  status_code?: number
  fetched_at: number
  persisted: boolean
  error?: string
}

/** 查询 Coding Plan 滚动窗口用量（5h + weekly）。 */
export async function queryQuota(id: number): Promise<CNProviderQuotaProbeResult> {
  const { data } = await apiClient.get<CNProviderQuotaProbeResult>(
    `/admin/cn-providers/accounts/${id}/quota`
  )
  return data
}

/** 查询 payg 账号余额。 */
export async function queryBalance(id: number): Promise<CNProviderBalanceResult> {
  const { data } = await apiClient.get<CNProviderBalanceResult>(
    `/admin/cn-providers/accounts/${id}/balance`
  )
  return data
}

export type CompatibleUsageReason = 'no_verified_query' | 'mode_unsupported' | 'custom_origin' | 'invalid_credentials' | 'proxy_unavailable' | 'permission_denied' | 'upstream_error' | 'invalid_response' | 'rate_limited'

export interface CompatibleProjectBudget {
  scope: 'account_in_project'
  project_id: number
  ram_user_id: number
  budget_kind: 'daily' | 'monthly' | 'unlimited'
  remaining_percentage: number | null
  period_kind: 'daily' | 'monthly'
  period_start: string
  period_end: string
  consumed: number
  limit: number | null
  unlimited: boolean
  currency: 'CNY'
}

export interface CompatibleUsageWindow {
  granularity: 'day'
  start: string
  end: string
  timezone: string
}

export interface CompatibleConsumedUsageResult {
  provider: string
  kind: 'consumed_usage' | 'unsupported'
  success: boolean
  unsupported?: boolean
  reason?: CompatibleUsageReason
  status_code?: number
  fetched_at: number
  persisted: boolean
  error?: string
  usage?: CompatibleUsageWindow & {
    models: { id: string; name: string; items: { name: string; unit: string; total: number }[] }[]
  }
}

export async function queryUsage(id: number, params: CompatibleUsageWindow): Promise<CompatibleConsumedUsageResult> {
  const { data } = await apiClient.get<CompatibleConsumedUsageResult>(`/admin/cn-providers/accounts/${id}/usage`, { params })
  return data
}

export default {
  queryUsage,
  queryQuota,
  queryBalance
}
