/**
 * User billing timeline API: one axis for recharges, deductions, refunds,
 * rebates, redeemed cards and administrative balance adjustments.
 */

import { apiClient } from './client'

export type UserBillingTimelineKind =
  | 'recharge'
  | 'charge'
  | 'refund'
  | 'rebate'
  | 'redeem'
  | 'adjustment'

export interface UserBillingTimelineEntry {
  kind: UserBillingTimelineKind
  // Signed: credits are positive, debits negative.
  amount: number
  // False only for rebate accruals, which stay in the affiliate quota until transferred.
  affects_balance: boolean
  occurred_at: string
  reference: string
  detail?: string
  request_count?: number
}

export interface UserBillingTimelineTotals {
  credits: number
  debits: number
  net: number
  accrued_rebate: number
  charge_request_count: number
}

export interface UserBillingTimelinePage {
  entries: UserBillingTimelineEntry[]
  totals: UserBillingTimelineTotals
  total: number
  page: number
  page_size: number
  timezone: string
}

export interface UserBillingTimelineParams {
  page?: number
  page_size?: number
  start_date?: string
  end_date?: string
  timezone?: string
}

export async function getMyBillingTimeline(
  params: UserBillingTimelineParams = {}
): Promise<UserBillingTimelinePage> {
  const { data } = await apiClient.get<UserBillingTimelinePage>('/usage/billing-timeline', { params })
  return data
}

export const billingAPI = {
  getMyBillingTimeline,
}
