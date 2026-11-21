import { apiClient } from '../client'
import type { WorkBuddySite } from '@/types/admin'
import { oauthProxyPayload, type OAuthProxySelection } from '@/utils/accountProxy'

export type { WorkBuddySite } from '@/types/admin'

export interface WorkBuddySiteConfig {
  site: WorkBuddySite
  baseUrl: string
  domain: string
  hostLabel: string
}

export const WORKBUDDY_SITES: Record<WorkBuddySite, WorkBuddySiteConfig> = {
  cn: {
    site: 'cn',
    baseUrl: 'https://copilot.tencent.com',
    domain: 'www.workbuddy.cn',
    hostLabel: 'copilot.tencent.com'
  },
  global: {
    site: 'global',
    baseUrl: 'https://www.workbuddy.ai',
    domain: 'www.workbuddy.ai',
    hostLabel: 'www.workbuddy.ai'
  }
}

export function normalizeWorkBuddySite(value: unknown): WorkBuddySite | null {
  if (typeof value !== 'string') return null
  const normalized = value.trim().toLowerCase()
  if (normalized === 'cn' || normalized === 'china' || normalized === 'workbuddy') {
    return 'cn'
  }
  if (
    normalized === 'global' ||
    normalized === 'intl' ||
    normalized === 'international' ||
    normalized === 'workbuddy-ai' ||
    normalized === 'workbuddy_ai'
  ) {
    return 'global'
  }
  return null
}

export function defaultWorkBuddyBaseUrl(site: WorkBuddySite): string {
  return WORKBUDDY_SITES[site].baseUrl
}

export function defaultWorkBuddyDomain(site: WorkBuddySite): string {
  return WORKBUDDY_SITES[site].domain
}

export interface WorkBuddyOAuthStartRequest {
  site: WorkBuddySite
  proxy_id?: number
  proxy_group_id?: number
}

export interface WorkBuddyOAuthStartResponse {
  session_id: string
  auth_url: string
  state: string
  site: WorkBuddySite
  expires_in: number
}

export interface WorkBuddyOAuthPollRequest {
  session_id: string
}

export interface WorkBuddyOAuthCredentials {
  site: WorkBuddySite
  access_token: string
  refresh_token: string
  expires_at: string
  refresh_expires_at?: string
  uid: string
  domain: string
  enterprise_id?: string
}

export interface WorkBuddyOAuthPollResponse {
  status: 'pending' | 'completed' | 'failed'
  site: WorkBuddySite
  account_name?: string
  credentials?: WorkBuddyOAuthCredentials
  error?: string
}

export async function startOAuth(
  payload: WorkBuddyOAuthStartRequest,
  signal?: AbortSignal
): Promise<WorkBuddyOAuthStartResponse> {
  const { data } = await apiClient.post<WorkBuddyOAuthStartResponse>(
    '/admin/workbuddy/oauth/start',
    payload,
    signal ? { signal } : undefined
  )
  return data
}

export function buildStartOAuthPayload(
  site: WorkBuddySite,
  proxySelection?: OAuthProxySelection
): WorkBuddyOAuthStartRequest {
  return {
    site,
    ...oauthProxyPayload(proxySelection)
  }
}

export async function pollOAuth(
  payload: WorkBuddyOAuthPollRequest,
  signal?: AbortSignal
): Promise<WorkBuddyOAuthPollResponse> {
  const { data } = await apiClient.post<WorkBuddyOAuthPollResponse>(
    '/admin/workbuddy/oauth/poll',
    payload,
    signal ? { signal } : undefined
  )
  return data
}

export const workbuddyAPI = {
  startOAuth,
  pollOAuth
}

export default workbuddyAPI
