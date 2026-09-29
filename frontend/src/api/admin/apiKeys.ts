import { validateTpmLimit } from '@/utils/tpmLimit'
/**
 * Inspect and manage a user's API keys as an administrator.
 */

import { apiClient } from '../client'
import type { ApiKey } from '@/types/user'

export interface UpdateApiKeyGroupResult {
  api_key: ApiKey
  auto_granted_group_access: boolean
  granted_group_id?: number
  granted_group_name?: string
}

export interface AdminApiKeyCredentialIssue {
  api_key: ApiKey
  secret: string
  grace_until?: string | null
}

export interface AdminRotateApiKeyOptions {
  grace_ttl_hours?: number
  revoke_previous?: boolean
}

/**
 * Update an API key's group binding
 * @param id - API Key ID
 * @param groupId - Group ID (0 to unbind, positive to bind, null/undefined to skip)
 * @returns Updated API key with auto-grant info
 */
export async function updateApiKeyGroup(id: number, groupId: number | null): Promise<UpdateApiKeyGroupResult> {
  const { data } = await apiClient.put<UpdateApiKeyGroupResult>(`/admin/api-keys/${id}`, {
    group_id: groupId === null ? 0 : groupId
  })
  return data
}

/**
 * Enable or disable any user's API key.
 * @param id - API Key ID
 * @param status - active or disabled
 */
export async function setApiKeyStatus(id: number, status: 'active' | 'disabled'): Promise<ApiKey> {
  const { data } = await apiClient.put<ApiKey>(`/admin/api-keys/${id}/status`, { status })
  return data
}

/**
 * Delete any user's API key.
 * @param id - API Key ID
 */
export async function deleteApiKey(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(`/admin/api-keys/${id}`)
  return data
}

/**
 * Rotate any user's API key secret, keeping its identity and history.
 * @param id - API Key ID
 * @param options - Optional grace window and immediate-revoke flag
 */
export async function rotateApiKey(id: number, options?: AdminRotateApiKeyOptions): Promise<AdminApiKeyCredentialIssue> {
  const payload: AdminRotateApiKeyOptions = {}
  if (options?.grace_ttl_hours !== undefined) {
    payload.grace_ttl_hours = options.grace_ttl_hours
  }
  if (options?.revoke_previous !== undefined) {
    payload.revoke_previous = options.revoke_previous
  }
  const { data } = await apiClient.post<AdminApiKeyCredentialIssue>(`/admin/api-keys/${id}/credentials/rotate`, payload)
  return data
}

export async function setTpmLimit(id: number, limit: number): Promise<ApiKey> {
  validateTpmLimit(limit)
  const { data } = await apiClient.put<ApiKey>(`/admin/api-keys/${id}/tpm`, { tpm_limit: limit })
  return data
}

export const apiKeysAPI = {
  setTpmLimit,
  updateApiKeyGroup,
  setApiKeyStatus,
  deleteApiKey,
  rotateApiKey
}

export default apiKeysAPI
