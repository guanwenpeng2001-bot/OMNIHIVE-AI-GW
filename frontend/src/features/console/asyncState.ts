import { normalizeApiError } from '@/utils/apiError'

export type ConsoleAsyncKind =
  | 'skeleton'
  | 'refreshing'
  | 'empty'
  | 'zero-filter'
  | 'error'
  | 'permission'
  | 'stale'
  | 'conflict'
  | 'confirmed'
  | 'unknown'

export interface ConsoleAsyncNotice {
  kind: ConsoleAsyncKind
  title: string
  message?: string
  code?: string | number
  reason?: string
  requestId?: string
  requiredGrant?: string
  lastSyncedAt?: string
  retryable?: boolean
}

export function classifyConsoleError(
  error: unknown,
  options?: {
    hasCachedData?: boolean
    lastSyncedAt?: string
    requiredGrantFallback?: string
    isZh?: boolean
  }
): ConsoleAsyncNotice {
  const normalized = normalizeApiError(error)
  const raw = (error ?? {}) as {
    status?: number
    code?: string | number
    reason?: string
    requestId?: string
    metadata?: Record<string, unknown>
    response?: {
      status?: number
      headers?: Record<string, string>
      data?: {
        code?: number | string
        reason?: string
        message?: string
        metadata?: Record<string, unknown>
      }
    }
  }

  const status = raw.status ?? raw.response?.status ?? normalized.status ?? 0
  const reason = raw.reason || raw.response?.data?.reason || normalized.reason
  const metadata: Record<string, unknown> = {
    ...normalized.metadata,
    ...(raw.response?.data?.metadata ?? {}),
    ...(raw.metadata ?? {})
  }
  const requestId =
    raw.requestId ||
    (typeof metadata.request_id === 'string' ? metadata.request_id : undefined) ||
    raw.response?.headers?.['x-request-id']
  const requiredGrant =
    (typeof metadata.required_grant === 'string' ? metadata.required_grant : undefined) ||
    (typeof metadata.action === 'string' ? metadata.action : undefined) ||
    options?.requiredGrantFallback
  const isZh = Boolean(options?.isZh)

  if (status === 403 || reason === 'FORBIDDEN' || reason === 'INSUFFICIENT_GRANT') {
    return {
      kind: 'permission',
      title: isZh ? '当前工作空间或项目缺少访问授权 (403)' : 'Insufficient Scope Permission (403)',
      message: normalized.message,
      code: reason || 403,
      reason,
      requestId,
      requiredGrant,
      retryable: false
    }
  }

  if (status === 409 || reason?.includes('CONFLICT') || reason?.includes('MISMATCH')) {
    return {
      kind: 'conflict',
      title: isZh ? '资源版本或幂等载荷冲突 (409)' : 'Version or Idempotency Conflict (409)',
      message:
        normalized.message ||
        (isZh
          ? '远端状态已更新，请重新拉取最新版本后再试。'
          : 'Remote state changed. Reload the latest version before retrying.'),
      code: reason || 409,
      reason,
      requestId,
      retryable: true
    }
  }

  if (
    status === 504 ||
    reason === 'OUTCOME_UNKNOWN' ||
    reason === 'DISPATCH_UNCERTAIN' ||
    raw.code === 'ECONNABORTED'
  ) {
    return {
      kind: 'unknown',
      title: isZh
        ? '执行结果暂未确认，请勿盲目重复提交'
        : 'Uncertain Execution Outcome — Do Not Blindly Re-Submit',
      message:
        normalized.message ||
        (isZh
          ? '请求可能已被上游接收，请先查询原状态或幂等记录，避免重复扣费。'
          : 'Upstream acceptance is uncertain. Query run/task status before creating a new billable call.'),
      code: reason || status || 'UNKNOWN',
      reason,
      requestId,
      retryable: true
    }
  }

  if (options?.hasCachedData) {
    return {
      kind: 'stale',
      title: isZh ? '刷新失败，当前展示最近一次同步快照' : 'Refresh Failed — Showing Last Synced Snapshot',
      message: normalized.message,
      code: reason || status || undefined,
      reason,
      requestId,
      lastSyncedAt: options.lastSyncedAt,
      retryable: true
    }
  }

  return {
    kind: 'error',
    title: isZh ? '请求加载失败' : 'Request Failed',
    message: normalized.message,
    code: reason || status || undefined,
    reason,
    requestId,
    retryable: true
  }
}
