import { getCurrentScope, onScopeDispose } from 'vue'

export function isOAuthCancellation(error: unknown): boolean {
  if (!error || typeof error !== 'object') return false
  const { code, name } = error as { code?: string; name?: string }
  return name === 'AbortError' || code === 'ERR_CANCELED' || code === 'AUTH_SESSION_CHANGED'
}

/** Ignore a completed callback request once its owning page has gone away. */
export function useCallbackLifetime() {
  let active = true
  if (getCurrentScope()) onScopeDispose(() => { active = false })
  const isActive = () => active
  async function waitForCallback<Result>(request: Promise<Result>): Promise<Result> {
    let result: Result
    try {
      result = await request
    } catch (error) {
      if (isOAuthCancellation(error)) active = false
      throw error
    }
    if (!active) throw new Error('OAuth callback disposed')
    return result
  }
  return { isActive, waitForCallback }
}
