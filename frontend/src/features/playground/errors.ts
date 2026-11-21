import { classifyConsoleError, type ConsoleAsyncNotice } from '@/features/console/asyncState'

export interface PlaygroundErrorOptions {
  requiredGrantFallback?: string
  isZh?: boolean
  isNonOwnedKey?: boolean
}

function extractHttpStatus(error: unknown): number {
  const raw = (error ?? {}) as {
    status?: number
    response?: { status?: number }
  }
  return raw.status ?? raw.response?.status ?? 0
}

export function normalizePlaygroundError(
  error: unknown,
  options?: PlaygroundErrorOptions
): ConsoleAsyncNotice {
  const base = classifyConsoleError(error, {
    requiredGrantFallback: options?.requiredGrantFallback,
    isZh: options?.isZh
  })
  const isZh = Boolean(options?.isZh)
  const status = extractHttpStatus(error)

  if (status === 404 && options?.isNonOwnedKey) {
    return {
      ...base,
      kind: 'error',
      title: isZh
        ? '无法使用他人 API 密钥运行 (404)'
        : 'Cannot Run with Another User’s API Key (404)',
      message: isZh
        ? '所选 API 密钥不属于当前用户。代他人运行需要当前作用域具备 keys.act_as 授权并显式指定付费人，否则服务端将返回 404。请切换为您自己的密钥。'
        : 'The selected API key does not belong to your account. Acting on another user’s key requires keys.act_as permission and an explicit payer; otherwise the gateway returns 404. Switch to one of your own keys.',
      code: base.reason || 404,
      requiredGrant: 'keys.act_as',
      retryable: false
    }
  }

  return base
}
