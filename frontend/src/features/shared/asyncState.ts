import { normalizeApiError, type NormalizedApiError } from '@/utils/apiError'

export type AsyncViewState =
  | 'skeleton'
  | 'refreshing'
  | 'ready'
  | 'empty'
  | 'zero-filter'
  | 'partial-error'
  | 'forbidden'
  | 'stale'
  | 'conflict'
  | 'confirmed'
  | 'unknown'

export interface AsyncErrorInfo {
  state: 'forbidden' | 'conflict' | 'unknown' | 'error'
  status: number
  reason?: string
  message: string
  requestId?: string
}

export function classifyAsyncError(error: unknown, fallbackMessage = 'Request failed'): AsyncErrorInfo {
  const normalized: NormalizedApiError = normalizeApiError(error)
  const metadata = normalized.metadata || {}
  const requestId =
    (typeof metadata.request_id === 'string' && metadata.request_id) ||
    (typeof metadata.requestId === 'string' && metadata.requestId) ||
    undefined

  if (normalized.status === 403 || normalized.reason === 'FORBIDDEN') {
    return {
      state: 'forbidden',
      status: 403,
      reason: normalized.reason,
      message: normalized.message || 'You do not have permission to perform this action.',
      requestId
    }
  }

  if (normalized.status === 409 || normalized.status === 412 || normalized.reason === 'CONFLICT') {
    return {
      state: 'conflict',
      status: normalized.status || 409,
      reason: normalized.reason,
      message: normalized.message || 'Resource version conflict. Reload the latest state and retry.',
      requestId
    }
  }

  if (
    normalized.status === 0 ||
    normalized.status === 502 ||
    normalized.status === 503 ||
    normalized.status === 504 ||
    normalized.reason === 'TIMEOUT' ||
    normalized.reason === 'UNKNOWN_OUTCOME'
  ) {
    return {
      state: 'unknown',
      status: normalized.status,
      reason: normalized.reason,
      message: normalized.message || 'Outcome is unknown due to a network or gateway interruption. Verify state before retrying.',
      requestId
    }
  }

  return {
    state: 'error',
    status: normalized.status,
    reason: normalized.reason,
    message: normalized.message || fallbackMessage,
    requestId
  }
}

const SECRET_PATTERNS: RegExp[] = [
  /(sk-[a-zA-Z0-9_-]{8,})/g,
  /(Bearer\s+[a-zA-Z0-9._-]{8,})/gi,
  /(ghp_[a-zA-Z0-9]{10,})/g,
  /(AIza[0-9A-Za-z_-]{16,})/g
]

export function redactSensitiveText(input: string | null | undefined): string {
  if (!input) return '—'
  let output = input
  for (const pattern of SECRET_PATTERNS) {
    output = output.replace(pattern, '[REDACTED]')
  }
  return output
}

export function formatNullableMono(value: string | number | null | undefined): string {
  if (value === null || value === undefined || value === '') return '—'
  return String(value)
}
