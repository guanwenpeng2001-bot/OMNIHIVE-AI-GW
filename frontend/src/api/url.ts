import { workspaceRequestURL } from './workspaceScope'

const DEFAULT_API_BASE_URL = '/api/v1'
const API_BASE_URL = normalizeAPIBaseURL(import.meta.env.VITE_API_BASE_URL)

function normalizePath(path: string): string {
  return path.startsWith('/') ? path : `/${path}`
}

function normalizeAPIBaseURL(value: unknown): string {
  const raw = String(value || DEFAULT_API_BASE_URL).trim() || DEFAULT_API_BASE_URL
  const withoutTrailingSlash = raw.replace(/\/+$/, '')
  if (/^[a-z][a-z\d+.-]*:\/\//i.test(withoutTrailingSlash) || withoutTrailingSlash.startsWith('//')) {
    return withoutTrailingSlash
  }
  return normalizePath(withoutTrailingSlash)
}

export function getAPIBaseURL(): string {
  return API_BASE_URL
}

export function buildApiUrl(path: string): string {
  const base = getAPIBaseURL().replace(/\/+$/, '')
  let suffix = normalizePath(path)
  if (suffix === DEFAULT_API_BASE_URL) {
    suffix = ''
  } else if (suffix.startsWith(`${DEFAULT_API_BASE_URL}/`)) {
    suffix = suffix.slice(DEFAULT_API_BASE_URL.length)
  }
  const scopedSuffix = typeof window === 'undefined' ? suffix : workspaceRequestURL(suffix, window.location.pathname)
  return `${base}${scopedSuffix}`
}

export function buildGatewayUrl(path: string): string {
  const suffix = normalizePath(path)
  try {
    const base = typeof window === 'undefined'
      ? new URL(getAPIBaseURL())
      : new URL(getAPIBaseURL(), window.location.origin)
    // A reverse proxy may mount both management and gateway routes below a prefix.
    // Only derive that prefix from the known management API suffix.
    const prefix = base.pathname.endsWith(DEFAULT_API_BASE_URL)
      ? base.pathname.slice(0, -DEFAULT_API_BASE_URL.length)
      : ''
    return `${base.origin}${prefix}${suffix}`
  } catch {
    return suffix
  }
}
