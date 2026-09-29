import { getCurrentScope, onScopeDispose, ref } from 'vue'
import { useAppStore } from '@/stores/app'
import { isOAuthCancellation } from './useCallbackLifetime'

/** Shared ownership of an authorization attempt; reset/disposal invalidates late results. */
export function useOAuthRequestState() {
  const appStore = useAppStore()
  const authUrl = ref('')
  const authCode = ref('')
  const sessionId = ref('')
  const sessionKey = ref('')
  const state = ref('')
  const loading = ref(false)
  const error = ref('')
  let generation = 0
  let disposed = false

  function resetState() {
    generation++
    authUrl.value = authCode.value = sessionId.value = sessionKey.value = state.value = ''
    loading.value = false
    error.value = ''
  }

  if (getCurrentScope()) onScopeDispose(() => { disposed = true; resetState() })

  function beginRequest() {
    const revision = ++generation
    const isCurrent = () => !disposed && revision === generation
    loading.value = true
    error.value = ''
    return {
      isCurrent,
      async wait<Result>(pending: Promise<Result>): Promise<Result> {
        let result: Result
        try {
          result = await pending
        } catch (failure) {
          if (isCurrent() && isOAuthCancellation(failure)) {
            loading.value = false
            generation++
          }
          throw failure
        }
        if (!isCurrent()) throw new Error('OAuth attempt superseded')
        return result
      },
      fail(message: string, notify = true) {
        if (!isCurrent()) return
        error.value = message
        if (notify) appStore.showError(message)
      },
      finish() { if (isCurrent()) loading.value = false },
    }
  }

  return { authUrl, authCode, sessionId, sessionKey, state, loading, error, resetState, beginRequest }
}
