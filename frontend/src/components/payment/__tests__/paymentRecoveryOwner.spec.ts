import { beforeEach, describe, expect, it } from 'vitest'
import {
  decidePaymentLaunch,
  PAYMENT_RECOVERY_STORAGE_KEY,
  readOwnedPaymentRecoverySnapshot,
  writePaymentRecoverySnapshot,
} from '../paymentFlow'

describe('persisted payment recovery ownership', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('auth_token', 'token-a')
    localStorage.setItem('auth_user', JSON.stringify({ id: 1 }))
  })

  function persist() {
    const { recovery } = decidePaymentLaunch({
      order_id: 42, amount: 10, pay_amount: 10, expires_at: '2099-01-01T00:00:00Z',
      client_secret: 'secret-a', resume_token: 'resume-a',
    }, { visibleMethod: 'stripe', orderType: 'balance', isMobile: false })
    writePaymentRecoverySnapshot(localStorage, recovery)
    return localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)
  }

  it('preserves same-account returns and token rotation', () => {
    const raw = persist()
    localStorage.setItem('auth_token', 'rotated-token-a')
    expect(readOwnedPaymentRecoverySnapshot(raw)).toMatchObject({ ownerUserId: 1, clientSecret: 'secret-a' })
  })

  it.each(['logout', 'switch', 'malformed'])('rejects and removes credentials after %s', (change) => {
    const raw = persist()
    if (change === 'logout') localStorage.removeItem('auth_token')
    if (change === 'switch') localStorage.setItem('auth_user', JSON.stringify({ id: 2 }))
    if (change === 'malformed') localStorage.setItem('auth_user', '{')
    expect(readOwnedPaymentRecoverySnapshot(raw, { resumeToken: 'resume-a' })).toBeNull()
    expect(localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
  })

  it('discards unowned legacy credentials', () => {
    const snapshot = JSON.parse(persist()!)
    delete snapshot.ownerUserId
    const raw = JSON.stringify(snapshot)
    localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, raw)
    expect(readOwnedPaymentRecoverySnapshot(raw)).toBeNull()
    expect(localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
  })
})
