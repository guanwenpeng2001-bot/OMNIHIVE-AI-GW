import { apiClient } from '@/api/client'
import { getAPIBaseURL } from '@/api/url'

const DEFAULT_V1_SUFFIX = '/api/v1'
const DEFAULT_V2_SUFFIX = '/api/v2'

export function getAPIV2BaseURL(): string {
  const base = getAPIBaseURL().replace(/\/+$/, '')
  if (base.endsWith(DEFAULT_V1_SUFFIX)) {
    return `${base.slice(0, -DEFAULT_V1_SUFFIX.length)}${DEFAULT_V2_SUFFIX}`
  }
  return DEFAULT_V2_SUFFIX
}

export function createIdempotencyKey(): string {
  if (typeof globalThis.crypto?.randomUUID === 'function') {
    return globalThis.crypto.randomUUID()
  }
  return `${Date.now()}-${Math.random().toString(36).slice(2, 10)}`
}

export async function v2Get<T>(
  path: string,
  params?: Record<string, unknown>,
  headers?: Record<string, string>
): Promise<T> {
  const { data } = await apiClient.get<T>(path, {
    baseURL: getAPIV2BaseURL(),
    params,
    headers
  })
  return data
}

export async function v2Post<T>(
  path: string,
  body?: unknown,
  headers?: Record<string, string>
): Promise<T> {
  const { data } = await apiClient.post<T>(path, body, {
    baseURL: getAPIV2BaseURL(),
    headers
  })
  return data
}

export async function v2Put<T>(
  path: string,
  body?: unknown,
  headers?: Record<string, string>
): Promise<T> {
  const { data } = await apiClient.put<T>(path, body, {
    baseURL: getAPIV2BaseURL(),
    headers
  })
  return data
}

export async function v2Patch<T>(
  path: string,
  body?: unknown,
  headers?: Record<string, string>
): Promise<T> {
  const { data } = await apiClient.patch<T>(path, body, {
    baseURL: getAPIV2BaseURL(),
    headers
  })
  return data
}

export async function v2Delete<T = void>(
  path: string,
  headers?: Record<string, string>
): Promise<T> {
  const { data } = await apiClient.delete<T>(path, {
    baseURL: getAPIV2BaseURL(),
    headers
  })
  return data
}
