import { computed, onScopeDispose, ref } from 'vue'
import {
  buildStartOAuthPayload,
  pollOAuth,
  startOAuth,
  type WorkBuddyOAuthCredentials,
  type WorkBuddyOAuthPollResponse,
  type WorkBuddySite
} from '@/api/admin/workbuddy'
import { isSilentRequestError } from '@/api/client'
import type { OAuthProxySelection } from '@/utils/accountProxy'
import { extractApiErrorMessage } from '@/utils/apiError'

export type WorkBuddyOAuthStatus =
  | 'idle'
  | 'starting'
  | 'waiting'
  | 'completed'
  | 'failed'
  | 'timeout'
  | 'cancelled'

export interface WorkBuddyStartLoginOptions {
  pollIntervalMs?: number
  onCompleted?: (response: WorkBuddyOAuthPollResponse) => void
}

const DEFAULT_POLL_INTERVAL_MS = 2500
const DEFAULT_EXPIRES_IN_SECONDS = 300

/**
 * Manages WorkBuddy browser OAuth session creation, automatic polling, timeout,
 * manual cancellation, and scope-dispose cleanup.
 */
export function useWorkBuddyOAuth() {
  const status = ref<WorkBuddyOAuthStatus>('idle')
  const authUrl = ref('')
  const sessionId = ref('')
  const site = ref<WorkBuddySite>('cn')
  const accountName = ref('')
  const credentials = ref<WorkBuddyOAuthCredentials | null>(null)
  const error = ref('')
  const expiresIn = ref(0)

  const loading = computed(() => status.value === 'starting' || status.value === 'waiting')

  let generation = 0
  let disposed = false
  let deadlineMs = 0
  let timer: ReturnType<typeof setTimeout> | undefined
  let controller: AbortController | undefined

  const clearTimer = () => {
    if (timer !== undefined) {
      clearTimeout(timer)
      timer = undefined
    }
  }

  const abortInFlight = () => {
    if (controller) {
      controller.abort()
      controller = undefined
    }
  }

  const cancel = () => {
    generation += 1
    clearTimer()
    abortInFlight()
    if (status.value === 'starting' || status.value === 'waiting') {
      status.value = 'cancelled'
    }
  }

  const reset = () => {
    generation += 1
    clearTimer()
    abortInFlight()
    status.value = 'idle'
    authUrl.value = ''
    sessionId.value = ''
    accountName.value = ''
    credentials.value = null
    error.value = ''
    expiresIn.value = 0
    deadlineMs = 0
  }

  const schedulePoll = (
    currentGen: number,
    pollIntervalMs: number,
    onCompleted?: (response: WorkBuddyOAuthPollResponse) => void
  ) => {
    clearTimer()
    timer = setTimeout(() => {
      void pollOnce(currentGen, pollIntervalMs, onCompleted)
    }, pollIntervalMs)
  }

  const pollOnce = async (
    currentGen: number,
    pollIntervalMs: number,
    onCompleted?: (response: WorkBuddyOAuthPollResponse) => void
  ) => {
    if (disposed || currentGen !== generation || status.value !== 'waiting') {
      return
    }
    if (Date.now() >= deadlineMs) {
      clearTimer()
      abortInFlight()
      status.value = 'timeout'
      return
    }
    try {
      const response = await pollOAuth({ session_id: sessionId.value }, controller?.signal)
      if (disposed || currentGen !== generation || status.value !== 'waiting') {
        return
      }
      if (response.status === 'pending') {
        if (Date.now() >= deadlineMs) {
          clearTimer()
          status.value = 'timeout'
          return
        }
        schedulePoll(currentGen, pollIntervalMs, onCompleted)
        return
      }
      if (response.status === 'completed') {
        clearTimer()
        status.value = 'completed'
        site.value = response.site || site.value
        accountName.value = response.account_name || ''
        credentials.value = response.credentials || null
        onCompleted?.(response)
        return
      }
      clearTimer()
      status.value = 'failed'
      error.value = response.error || ''
    } catch (cause) {
      if (disposed || currentGen !== generation || isSilentRequestError(cause)) {
        return
      }
      clearTimer()
      status.value = 'failed'
      error.value = extractApiErrorMessage(cause, '')
    }
  }

  const startLogin = async (
    targetSite: WorkBuddySite,
    proxySelection?: OAuthProxySelection,
    options: WorkBuddyStartLoginOptions = {}
  ): Promise<boolean> => {
    generation += 1
    const currentGen = generation
    clearTimer()
    abortInFlight()
    controller = new AbortController()

    status.value = 'starting'
    site.value = targetSite
    authUrl.value = ''
    sessionId.value = ''
    accountName.value = ''
    credentials.value = null
    error.value = ''
    expiresIn.value = 0

    const pollIntervalMs = options.pollIntervalMs ?? DEFAULT_POLL_INTERVAL_MS

    try {
      const response = await startOAuth(
        buildStartOAuthPayload(targetSite, proxySelection),
        controller.signal
      )
      if (disposed || currentGen !== generation) {
        return false
      }
      authUrl.value = response.auth_url
      sessionId.value = response.session_id
      site.value = response.site || targetSite
      const ttlSeconds = Math.max(10, Number(response.expires_in) || DEFAULT_EXPIRES_IN_SECONDS)
      expiresIn.value = ttlSeconds
      deadlineMs = Date.now() + ttlSeconds * 1000
      status.value = 'waiting'
      schedulePoll(currentGen, pollIntervalMs, options.onCompleted)
      return true
    } catch (cause) {
      if (disposed || currentGen !== generation || isSilentRequestError(cause)) {
        return false
      }
      status.value = 'failed'
      error.value = extractApiErrorMessage(cause, '')
      return false
    }
  }

  onScopeDispose(() => {
    disposed = true
    clearTimer()
    abortInFlight()
  })

  return {
    status,
    authUrl,
    sessionId,
    site,
    accountName,
    credentials,
    error,
    expiresIn,
    loading,
    startLogin,
    cancel,
    reset
  }
}
