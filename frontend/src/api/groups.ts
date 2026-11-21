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

export interface ProjectKeyGroupItem {
  id: string
  name: string
  platform: string
}

export interface ProjectKeyGroupPage {
  items: ProjectKeyGroupItem[]
  next_cursor: string | null
  as_of?: string
}

function isSnapshotExpiredError(error: unknown): boolean {
  if (!error || typeof error !== 'object') return false
  const candidate = error as { status?: number; code?: string; reason?: string; response?: { status?: number } }
  const status = candidate.status ?? candidate.response?.status
  return status === 410 || candidate.code === 'SNAPSHOT_EXPIRED' || candidate.reason === 'SNAPSHOT_EXPIRED'
}

export async function getProjectKeyGroups(
  workspaceId: string | number,
  projectId: string | number,
  options?: { signal?: AbortSignal; limit?: number }
): Promise<ProjectKeyGroupItem[]> {
  const wsId = String(workspaceId).trim()
  const projId = String(projectId).trim()
  const url = `/api/v2/workspaces/${encodeURIComponent(wsId)}/projects/${encodeURIComponent(projId)}/key-groups`
  const limit = options?.limit ?? 100

  let retriedExpired = false
  while (true) {
    const collected: ProjectKeyGroupItem[] = []
    let cursor: string | undefined
    try {
      while (true) {
        const params: { limit: number; cursor?: string } = cursor ? { limit, cursor } : { limit }
        const { data } = await apiClient.get<ProjectKeyGroupPage>(url, {
          ...(options?.signal ? { signal: options.signal } : {}),
          params
        })
        const pageItems = Array.isArray(data?.items) ? data.items : []
        for (const item of pageItems) {
          collected.push({
            id: String(item.id),
            name: item.name,
            platform: item.platform
          })
        }
        const nextCursor = data?.next_cursor ?? null
        if (!nextCursor) {
          return collected
        }
        cursor = nextCursor
      }
    } catch (error) {
      if (!retriedExpired && cursor && isSnapshotExpiredError(error)) {
        retriedExpired = true
        continue
      }
      throw error
    }
  }
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
  if (workspaceId && projectId) {
    const items = await getProjectKeyGroups(workspaceId, projectId, { signal: options?.signal })
    return items.map(
      (item) =>
        ({
          id: item.id as unknown as number,
          name: item.name,
          description: null,
          platform: item.platform as Group['platform'],
          rate_multiplier: 1,
          is_exclusive: false,
          status: 'active',
          subscription_type: 'standard',
          daily_limit_usd: null,
          weekly_limit_usd: null,
          monthly_limit_usd: null,
          user_count: 0,
          created_at: '',
          updated_at: ''
        }) as unknown as Group
    )
  }
  const url = workspaceId
    ? `/workspaces/${encodeURIComponent(workspaceId)}/groups/available`
    : '/groups/available'
  const config =
    options?.signal
      ? { signal: options.signal }
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
  getProjectKeyGroups,
  getUserGroupRates
}

export default userGroupsAPI
