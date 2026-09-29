import { postIdempotentCreate } from './idempotency'
/**
 * API Keys management endpoints
 * Handles CRUD operations for user API keys
 */

import { apiClient } from './client'
import type { ApiKey, ApiKeyPeriodBudgetRequest, CreateApiKeyRequest, UpdateApiKeyRequest } from '@/types/user'
import type { PaginatedResponse } from '@/types/common'

/**
 * List all API keys for current user
 * @param page - Page number (default: 1)
 * @param pageSize - Items per page (default: 10)
 * @param filters - Optional filter parameters
 * @param options - Optional request options
 * @returns Paginated list of API keys
 */
export async function list(
  page: number = 1,
  pageSize: number = 10,
  filters?: {
    search?: string
    status?: string
    group_id?: number | string
    sort_by?: string
    sort_order?: 'asc' | 'desc'
  },
  options?: {
    signal?: AbortSignal
  }
): Promise<PaginatedResponse<ApiKey>> {
  const { data } = await apiClient.get<PaginatedResponse<ApiKey>>('/keys', {
    params: { page, page_size: pageSize, ...filters },
    signal: options?.signal
  })
  return data
}

/**
 * Get API key by ID
 * @param id - API key ID
 * @returns API key details
 */
export async function getById(id: number): Promise<ApiKey> {
  const { data } = await apiClient.get<ApiKey>(`/keys/${id}`)
  return data
}

/**
 * Create new API key
 * @param name - Key name
 * @param groupId - Optional group ID
 * @param customKey - Optional custom key value
 * @param ipWhitelist - Optional IP whitelist
 * @param ipBlacklist - Optional IP blacklist
 * @param quota - Optional quota limit in USD (0 = unlimited)
 * @param expiresInDays - Optional days until expiry (undefined = never expires)
 * @param rateLimitData - Optional rate limit fields
 * @param budgetData - Optional period budget (USD) fields, separate from rate limits
 * @returns The created key plus its secret, which is shown only once
 */
export async function create(
  name: string,
  groupId?: number | null,
  customKey?: string,
  ipWhitelist?: string[],
  ipBlacklist?: string[],
  quota?: number,
  expiresInDays?: number,
  rateLimitData?: { rate_limit_5h?: number; rate_limit_1d?: number; rate_limit_7d?: number },
  budgetData?: ApiKeyPeriodBudgetRequest
): Promise<ApiKeyCredentialIssue> {
  const payload: CreateApiKeyRequest = { name }
  if (groupId !== undefined) {
    payload.group_id = groupId
  }
  if (customKey) {
    payload.custom_key = customKey
  }
  if (ipWhitelist && ipWhitelist.length > 0) {
    payload.ip_whitelist = ipWhitelist
  }
  if (ipBlacklist && ipBlacklist.length > 0) {
    payload.ip_blacklist = ipBlacklist
  }
  if (quota !== undefined && quota > 0) {
    payload.quota = quota
  }
  if (expiresInDays !== undefined && expiresInDays > 0) {
    payload.expires_in_days = expiresInDays
  }
  if (rateLimitData?.rate_limit_5h && rateLimitData.rate_limit_5h > 0) {
    payload.rate_limit_5h = rateLimitData.rate_limit_5h
  }
  if (rateLimitData?.rate_limit_1d && rateLimitData.rate_limit_1d > 0) {
    payload.rate_limit_1d = rateLimitData.rate_limit_1d
  }
  if (rateLimitData?.rate_limit_7d && rateLimitData.rate_limit_7d > 0) {
    payload.rate_limit_7d = rateLimitData.rate_limit_7d
  }
  if (budgetData) {
    if (budgetData.legacy_budget_anchor) {
      payload.legacy_budget_anchor = budgetData.legacy_budget_anchor
    }
    if (budgetData.budget_period_mode) {
      payload.budget_period_mode = budgetData.budget_period_mode
    }
    if (budgetData.budget_limit !== undefined) {
      payload.budget_limit = budgetData.budget_limit
    }
    if (budgetData.budget_duration_seconds !== undefined) {
      payload.budget_duration_seconds = budgetData.budget_duration_seconds
    }
    if (budgetData.budget_anchor) {
      payload.budget_anchor = budgetData.budget_anchor
    }
    if (budgetData.budget_calendar_unit) {
      payload.budget_calendar_unit = budgetData.budget_calendar_unit
    }
    if (budgetData.budget_timezone) {
      payload.budget_timezone = budgetData.budget_timezone
    }
  }

  return postIdempotentCreate<ApiKeyCredentialIssue>('/keys', payload)
}

/**
 * Issue returned once when a key is created or rotated: the plaintext secret is
 * never readable again from any endpoint.
 */
export interface ApiKeyCredentialIssue {
  api_key: ApiKey
  secret: string
  grace_until?: string | null
}

export interface RotateApiKeyOptions {
  grace_ttl_hours?: number
  revoke_previous?: boolean
}

/**
 * Rotate a key's secret in place, keeping its identity and history.
 * @param id - API key ID
 * @param options - Optional grace window and immediate-revoke flag
 * @returns The key plus the new secret, shown once
 */
export async function rotate(id: number, options?: RotateApiKeyOptions): Promise<ApiKeyCredentialIssue> {
  const payload: RotateApiKeyOptions = {}
  if (options?.grace_ttl_hours !== undefined) {
    payload.grace_ttl_hours = options.grace_ttl_hours
  }
  if (options?.revoke_previous !== undefined) {
    payload.revoke_previous = options.revoke_previous
  }
  const { data } = await apiClient.post<ApiKeyCredentialIssue>(`/keys/${id}/credentials/rotate`, payload)
  return data
}

/**
 * Revoke a key's previous (grace) credentials immediately.
 * @param id - API key ID
 * @returns Count of revoked credentials
 */
export async function revokeCredentials(id: number): Promise<{ revoked_credentials: number }> {
  const { data } = await apiClient.post<{ revoked_credentials: number }>(`/keys/${id}/credentials/revoke`, {})
  return data
}

/**
 * Update API key
 * @param id - API key ID
 * @param updates - Fields to update
 * @returns Updated API key
 */
export async function update(id: number, updates: UpdateApiKeyRequest): Promise<ApiKey> {
  const { data } = await apiClient.put<ApiKey>(`/keys/${id}`, updates)
  return data
}

/**
 * Delete API key
 * @param id - API key ID
 * @returns Success confirmation
 */
export async function deleteKey(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(`/keys/${id}`)
  return data
}

/**
 * Toggle API key status (active/inactive)
 * @param id - API key ID
 * @param status - New status
 * @returns Updated API key
 */
export async function toggleStatus(id: number, status: 'active' | 'inactive'): Promise<ApiKey> {
  return update(id, { status })
}

export const keysAPI = {
  list,
  getById,
  create,
  update,
  delete: deleteKey,
  toggleStatus,
  rotate,
  revokeCredentials
}

export default keysAPI
