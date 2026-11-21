import { effectScope } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { adminAPI } from '@/api/admin'
import { pollOAuth, startOAuth } from '@/api/admin/workbuddy'
import { useWorkBuddyOAuth } from '@/composables/useWorkBuddyOAuth'

const { postMock } = vi.hoisted(() => ({
  postMock: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    post: postMock
  },
  isSilentRequestError: (error: unknown) =>
    Boolean(error && typeof error === 'object' && (error as { name?: string }).name === 'AbortError')
}))

describe('admin workbuddy OAuth API & composable', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    postMock.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('calls /admin/workbuddy/oauth/start and /admin/workbuddy/oauth/poll with expected payloads', async () => {
    postMock
      .mockResolvedValueOnce({
        data: {
          session_id: 'wb-sess-1',
          auth_url: 'https://copilot.tencent.com/oauth/authorize?state=abc',
          state: 'abc',
          site: 'cn',
          expires_in: 300
        }
      })
      .mockResolvedValueOnce({
        data: {
          status: 'completed',
          site: 'cn',
          account_name: 'alice-cn',
          credentials: {
            site: 'cn',
            access_token: 'wb-at',
            refresh_token: 'wb-rt',
            expires_at: '2026-12-01T00:00:00Z',
            uid: 'uid-1',
            domain: 'www.workbuddy.cn'
          }
        }
      })

    const started = await startOAuth({ site: 'cn', proxy_id: 7 })
    expect(postMock).toHaveBeenNthCalledWith(
      1,
      '/admin/workbuddy/oauth/start',
      { site: 'cn', proxy_id: 7 },
      undefined
    )
    expect(started.session_id).toBe('wb-sess-1')

    const polled = await adminAPI.workbuddy.pollOAuth({ session_id: 'wb-sess-1' })
    expect(postMock).toHaveBeenNthCalledWith(
      2,
      '/admin/workbuddy/oauth/poll',
      { session_id: 'wb-sess-1' },
      undefined
    )
    expect(polled.status).toBe('completed')
    expect(polled.credentials?.uid).toBe('uid-1')
    expect(adminAPI.workbuddy.startOAuth).toBe(startOAuth)
    expect(adminAPI.workbuddy.pollOAuth).toBe(pollOAuth)
  })

  it('polls until completed and invokes onCompleted callback', async () => {
    postMock
      .mockResolvedValueOnce({
        data: {
          session_id: 'wb-sess-global',
          auth_url: 'https://www.workbuddy.ai/oauth/authorize?state=xyz',
          state: 'xyz',
          site: 'global',
          expires_in: 120
        }
      })
      .mockResolvedValueOnce({
        data: {
          status: 'pending',
          site: 'global'
        }
      })
      .mockResolvedValueOnce({
        data: {
          status: 'completed',
          site: 'global',
          account_name: 'bob-global',
          credentials: {
            site: 'global',
            access_token: 'global-at',
            refresh_token: 'global-rt',
            expires_at: '2026-12-31T00:00:00Z',
            uid: 'uid-global',
            domain: 'www.workbuddy.ai'
          }
        }
      })

    const onCompleted = vi.fn()
    const scope = effectScope()
    const state = scope.run(() => useWorkBuddyOAuth())!

    const started = await state.startLogin('global', undefined, { pollIntervalMs: 1000, onCompleted })
    expect(started).toBe(true)
    expect(state.status.value).toBe('waiting')
    expect(state.authUrl.value).toBe('https://www.workbuddy.ai/oauth/authorize?state=xyz')

    await vi.advanceTimersByTimeAsync(1000)
    expect(state.status.value).toBe('waiting')

    await vi.advanceTimersByTimeAsync(1000)
    expect(state.status.value).toBe('completed')
    expect(state.accountName.value).toBe('bob-global')
    expect(state.credentials.value?.access_token).toBe('global-at')
    expect(onCompleted).toHaveBeenCalledOnce()
    scope.stop()
  })

  it('marks failed when poll returns failed status', async () => {
    postMock
      .mockResolvedValueOnce({
        data: {
          session_id: 'wb-fail',
          auth_url: 'https://copilot.tencent.com/auth',
          state: 's1',
          site: 'cn',
          expires_in: 60
        }
      })
      .mockResolvedValueOnce({
        data: {
          status: 'failed',
          site: 'cn',
          error: 'Authorization denied by user'
        }
      })

    const scope = effectScope()
    const state = scope.run(() => useWorkBuddyOAuth())!
    await state.startLogin('cn', null, { pollIntervalMs: 500 })
    await vi.advanceTimersByTimeAsync(500)

    expect(state.status.value).toBe('failed')
    expect(state.error.value).toBe('Authorization denied by user')
    scope.stop()
  })

  it('cleans up timers and aborts in-flight requests on cancel and scope dispose', async () => {
    postMock.mockResolvedValueOnce({
      data: {
        session_id: 'wb-cancel',
        auth_url: 'https://copilot.tencent.com/auth',
        state: 's2',
        site: 'cn',
        expires_in: 60
      }
    })

    const scope = effectScope()
    const state = scope.run(() => useWorkBuddyOAuth())!
    await state.startLogin('cn', null, { pollIntervalMs: 1000 })
    expect(state.status.value).toBe('waiting')

    state.cancel()
    expect(state.status.value).toBe('cancelled')
    await vi.advanceTimersByTimeAsync(5000)
    expect(postMock).toHaveBeenCalledTimes(1)

    // Also verify timeout & dispose cleanup
    postMock.mockResolvedValueOnce({
      data: {
        session_id: 'wb-timeout',
        auth_url: 'https://copilot.tencent.com/auth',
        state: 's3',
        site: 'cn',
        expires_in: 10
      }
    })
    await state.startLogin('cn', null, { pollIntervalMs: 2000 })
    scope.stop()
    await vi.advanceTimersByTimeAsync(15_000)
    expect(postMock).toHaveBeenCalledTimes(2)
  })
})
