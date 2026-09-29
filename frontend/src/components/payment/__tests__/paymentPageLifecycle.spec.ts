import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, ref, type EffectScope } from 'vue'
import { advanceAuthSession, AUTH_SESSION_KEY } from '@/api/tokenRefresh'
import { usePaymentPageLifecycle } from '../paymentFlow'

let scope: EffectScope
beforeEach(() => {
  localStorage.clear()
  localStorage.setItem('auth_token', 'token-a')
  localStorage.setItem('auth_user', JSON.stringify({ id: 1 }))
  scope = effectScope()
})
afterEach(() => scope.stop())

describe('payment page lifetime', () => {
  it('aborts immediately when the same account starts another session', () => {
    const invalidate = vi.fn()
    const lifecycle = scope.run(() => usePaymentPageLifecycle(() => '/payment', invalidate))!
    advanceAuthSession()
    expect(lifecycle.signal.aborted).toBe(true)
    expect(lifecycle.isCurrent()).toBe(false)
    scope.stop()
    expect(invalidate).toHaveBeenCalledTimes(1)
  })

  it('keeps token renewal valid but rejects a cross-tab session replacement', () => {
    const lifecycle = scope.run(() => usePaymentPageLifecycle(() => '/payment'))!
    localStorage.setItem('auth_token', 'rotated-a')
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_token' }))
    expect(lifecycle.isCurrent()).toBe(true)
    localStorage.setItem(AUTH_SESSION_KEY, 'new-session')
    window.dispatchEvent(new StorageEvent('storage', { key: AUTH_SESSION_KEY }))
    expect(lifecycle.signal.aborted).toBe(true)
  })

  it('invalidates reused route components synchronously and never revives them', () => {
    const page = ref('order-1')
    const lifecycle = scope.run(() => usePaymentPageLifecycle(() => page.value))!
    page.value = 'order-2'
    expect(lifecycle.signal.aborted).toBe(true)
    page.value = 'order-1'
    expect(lifecycle.isCurrent()).toBe(false)
  })

  it('rejects a silent identity change and aborts when the component is disposed', () => {
    const lifecycle = scope.run(() => usePaymentPageLifecycle(() => '/payment'))!
    localStorage.setItem('auth_user', JSON.stringify({ id: 2 }))
    expect(lifecycle.isCurrent()).toBe(false)
    const fresh = scope.run(() => usePaymentPageLifecycle(() => '/payment'))!
    expect(fresh.isCurrent()).toBe(true)
    scope.stop()
    expect(fresh.signal.aborted).toBe(true)
  })
})
