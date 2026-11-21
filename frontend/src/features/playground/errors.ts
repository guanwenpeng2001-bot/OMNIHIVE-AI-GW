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

  if (base.reason === 'NO_ELIGIBLE_ENDPOINT' || (status === 422 && base.reason === 'NO_ELIGIBLE_ENDPOINT')) {
    return {
      ...base,
      kind: 'error',
      title: isZh
        ? '当前作用域内无可用的已验证端点 (422)'
        : 'No Verified Endpoint Available in Scope (422)',
      message: isZh
        ? '当前作用域内没有可用的已验证端点或已发布模型：请检查模型是否已发布到目录、连接是否已验证（BYOK 页面 /byok），或切换协议/模型后重试。'
        : 'No verified endpoint or published model is available in the selected scope. Check that the model is published to the catalog, verify your connection on the BYOK page (/byok), or switch protocol/model.',
      code: 'NO_ELIGIBLE_ENDPOINT',
      reason: 'NO_ELIGIBLE_ENDPOINT',
      retryable: false
    }
  }

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
