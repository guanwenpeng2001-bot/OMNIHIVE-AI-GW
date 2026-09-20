/**
 * Admin Channels API endpoints
 * Handles channel management for administrators
 */

import type { PaginatedResponse } from '@/types/common'
import { apiClient } from '../client'
import type { BillingMode, ChannelStatus, BillingModelSource } from '@/constants/channel'

export interface PricingQuote {
  value: number | null
  currency: string
  unit: string
  scope: string
  conditions: string
}

export interface PricingDifference {
  id: string
  basis: 'reference_to_reference' | 'channel_to_reference'
  model: string
  field: string
  baseline: PricingQuote
  candidate: PricingQuote
  status: 'comparable' | 'missing' | 'incomparable' | 'new_charge'
  absolute_percent: number | null
  warning: boolean
}

export interface ChannelPricingPreview {
  differences: PricingDifference[]
  source?: { candidate_hash: string; before_count: number; after_count: number; added: string[]; removed: string[]; differences: PricingDifference[] }
  impacts?: Array<{ model: string; group_id: number; comparable: boolean; charge_changed: boolean; before: { ActualCost: number } | null; after: { ActualCost: number } | null; before_error?: string; after_error?: string }>
}

export async function previewPricing(request: {
  model: string; profile: string; task_type: string; pricing: ChannelModelPricing
  candidate?: unknown; group_id?: number; input_tokens?: number; output_tokens?: number
}): Promise<ChannelPricingPreview> {
  const { data } = await apiClient.post<ChannelPricingPreview>('/admin/channels/pricing/preview', request)
  return data
}

export type { BillingMode } from '@/constants/channel'

export interface PricingInterval {
  id?: number
  min_tokens: number
  max_tokens: number | null
  tier_label: string
  input_price: number | null
  output_price: number | null
  cache_write_price: number | null
  cache_write_1h_price?: number | null
  cache_read_price: number | null
  input_multiplier: number | null
  output_multiplier: number | null
  cache_write_multiplier: number | null
  cache_read_multiplier: number | null
  per_request_price: number | null
  sort_order: number
}

export interface ChannelTimePricingPeriod {
  start_time: string
  end_time: string
  multiplier: number
}

export interface ChannelTimePricing {
  timezone: string
  weekdays_only?: boolean
  periods: ChannelTimePricingPeriod[]
}

export interface ConditionalModelPricing {
  input_price: number | null
  output_price: number | null
  cache_read_price: number | null
  cache_write_price?: number | null
  intervals?: PricingInterval[]
  explicit_cache_pricing?: ConditionalModelPricing | null
  batch_chat_pricing?: ConditionalModelPricing | null
}

export interface ChannelModelPricing {
  native_pricing_source?: string
  thinking_pricing?: ConditionalModelPricing | null
  explicit_cache_pricing?: ConditionalModelPricing | null
  batch_chat_pricing?: ConditionalModelPricing | null
  id?: number
  platform: string
  models: string[]
  billing_mode: BillingMode
  input_price: number | null
  output_price: number | null
  cache_write_price: number | null
  cache_write_1h_price?: number | null
  cache_read_price: number | null
  fast_multiplier?: number | null
  flex_multiplier?: number | null
  max_reasoning_effort_multiplier?: number | null
  image_input_price: number | null
  image_output_price: number | null
  per_request_price: number | null
  intervals: PricingInterval[]
  time_pricing: ChannelTimePricing | null
}

export interface AccountStatsPricingRule {
  id?: number
  name: string
  group_ids: number[]
  account_ids: number[]
  pricing: ChannelModelPricing[]
}

export interface Channel {
  id: number
  name: string
  description: string
  status: ChannelStatus
  billing_model_source: BillingModelSource
  restrict_models: boolean
  features_config?: Record<string, unknown>
  group_ids: number[]
  model_pricing: ChannelModelPricing[]
  model_mapping: Record<string, Record<string, string>> // platform → {src→dst}
  apply_pricing_to_account_stats: boolean
  account_stats_pricing_rules: AccountStatsPricingRule[]
  created_at: string
  updated_at: string
}

export interface CreateChannelRequest {
  name: string
  description?: string
  group_ids?: number[]
  model_pricing?: ChannelModelPricing[]
  model_mapping?: Record<string, Record<string, string>>
  billing_model_source?: BillingModelSource
  restrict_models?: boolean
  features_config?: Record<string, unknown>
  apply_pricing_to_account_stats?: boolean
  account_stats_pricing_rules?: AccountStatsPricingRule[]
}

export interface UpdateChannelRequest {
  name?: string
  description?: string
  status?: ChannelStatus
  group_ids?: number[]
  model_pricing?: ChannelModelPricing[]
  model_mapping?: Record<string, Record<string, string>>
  billing_model_source?: BillingModelSource
  restrict_models?: boolean
  features_config?: Record<string, unknown>
  apply_pricing_to_account_stats?: boolean
  account_stats_pricing_rules?: AccountStatsPricingRule[]
}

/**
 * List channels with pagination
 */
export async function list(
  page: number = 1,
  pageSize: number = 20,
  filters?: {
    status?: ChannelStatus
    search?: string
    sort_by?: string
    sort_order?: 'asc' | 'desc'
  },
  options?: { signal?: AbortSignal }
): Promise<PaginatedResponse<Channel>> {
  const { data } = await apiClient.get<PaginatedResponse<Channel>>('/admin/channels', {
    params: {
      page,
      page_size: pageSize,
      ...filters
    },
    signal: options?.signal
  })
  return data
}

/**
 * Get channel by ID
 */
export async function getById(id: number): Promise<Channel> {
  const { data } = await apiClient.get<Channel>(`/admin/channels/${id}`)
  return data
}

/**
 * Create a new channel
 */
export async function create(req: CreateChannelRequest): Promise<Channel> {
  const { data } = await apiClient.post<Channel>('/admin/channels', req)
  return data
}

/**
 * Update a channel
 */
export async function update(id: number, req: UpdateChannelRequest): Promise<Channel> {
  const { data } = await apiClient.put<Channel>(`/admin/channels/${id}`, req)
  return data
}

/**
 * Delete a channel
 */
export async function remove(id: number): Promise<void> {
  await apiClient.delete(`/admin/channels/${id}`)
}

export interface ModelDefaultPricing {
  found: boolean
  input_price?: number    // per-token price
  output_price?: number
  cache_write_price?: number
  cache_write_1h_price?: number | null
  cache_read_price?: number
  image_input_price?: number
  image_output_price?: number
  max_reasoning_effort_multiplier?: number | null
}

export async function getModelDefaultPricing(model: string): Promise<ModelDefaultPricing> {
  const { data } = await apiClient.get<ModelDefaultPricing>('/admin/channels/model-pricing', {
    params: { model }
  })
  return data
}

export interface SyncPricingModelsResult {
  models: string[]
}

/**
 * Fetch the latest model names from the LiteLLM pricing catalog for the given platform
 */
export async function syncPricingModels(platform: string): Promise<SyncPricingModelsResult> {
  const { data } = await apiClient.get<SyncPricingModelsResult>('/admin/channels/pricing/sync-models', {
    params: { platform }
  })
  return data
}

const channelsAPI = { list, getById, create, update, remove, getModelDefaultPricing, syncPricingModels }
export default channelsAPI
export interface PlazaCandidateIdentity { platform: string; name: string }
export interface PlazaSelectedModel extends PlazaCandidateIdentity { confirmed_by?: number; confirmed_at?: string }
export interface PlazaCandidateView { group_id: number; revision: string; candidates: PlazaCandidateIdentity[]; selected: PlazaSelectedModel[] }

export async function getPlazaCandidates(channelID: number, groupID: number): Promise<PlazaCandidateView> {
  const { data } = await apiClient.get<PlazaCandidateView>(`/admin/channels/${channelID}`, { params: { plaza_group_id: groupID } })
  return data
}

export async function confirmPlazaModels(channelID: number, groupID: number, revision: string, models: PlazaCandidateIdentity[]): Promise<void> {
  await apiClient.put(`/admin/channels/${channelID}`, { group_id: groupID, revision, models }, { params: { plaza_confirmation: true } })
}
