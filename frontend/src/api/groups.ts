/**
 * User Groups API endpoints (non-admin)
 * Handles group-related operations for regular users
 */

import { apiClient } from './client'
import type { Group } from '@/types/user'

export interface AvailableGroupsOptions {
  signal?: AbortSignal
  workspaceId?: string | number | null
  projectId?: string | number | null
}

/**
 * Get available groups that the current user can bind to API keys
 * This returns groups based on user's permissions:
 * - Standard groups: public (non-exclusive) or explicitly allowed
 * - Subscription groups: user has active subscription
 * @returns List of available groups
 */
export async function getAvailable(options?: AvailableGroupsOptions): Promise<Group[]> {
  const workspaceId = options?.workspaceId != null ? String(options.workspaceId).trim() : ''
  const projectId = options?.projectId != null ? String(options.projectId).trim() : ''
  const url = workspaceId
    ? `/workspaces/${encodeURIComponent(workspaceId)}/groups/available`
    : '/groups/available'
  const params = workspaceId && projectId ? { project_id: projectId } : undefined
  const config =
    options?.signal || params
      ? {
          ...(options?.signal ? { signal: options.signal } : {}),
          ...(params ? { params } : {})
        }
      : undefined
  const { data } = await apiClient.get<Group[]>(url, config)
  return data
}

/**
 * Get current user's custom group rate multipliers
 * @returns Map of group_id to custom rate_multiplier
 */
export async function getUserGroupRates(options?: AvailableGroupsOptions): Promise<Record<number, number>> {
  const workspaceId = options?.workspaceId != null ? String(options.workspaceId).trim() : ''
  const url = workspaceId
    ? `/workspaces/${encodeURIComponent(workspaceId)}/groups/rates`
    : '/groups/rates'
  const config = options?.signal ? { signal: options.signal } : undefined
  const { data } = await apiClient.get<Record<number, number> | null>(url, config)
  return data || {}
}

export const userGroupsAPI = {
  getAvailable,
  getUserGroupRates
}

export default userGroupsAPI
