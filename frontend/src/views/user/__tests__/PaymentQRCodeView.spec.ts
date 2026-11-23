import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { authStorage } from '@/api/tokenRefresh'
import { PAYMENT_RECOVERY_STORAGE_KEY } from '@/components/payment/paymentFlow'
import PaymentQRCodeView from '../PaymentQRCodeView.vue'

const mocks = vi.hoisted(() => ({
  getOrder: vi.fn(),
  cancelOrder: vi.fn(),
  push: vi.fn(),
  showError: vi.fn(),
  toCanvas: vi.fn().mockResolvedValue(undefined),
  routeQuery: {} as Record<string, string>
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ query: mocks.routeQuery }),
  useRouter: () => ({ push: mocks.push })
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (k: string) => k })
}))

vi.mock('@/api/payment', () => ({
  paymentAPI: {
    getOrder: mocks.getOrder,
    cancelOrder: mocks.cancelOrder
  }
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({ showError: mocks.showError })
}))

vi.mock('@/stores/payment', () => ({
  usePaymentStore: () => ({})
}))

vi.mock('qrcode', () => ({
  default: { toCanvas: mocks.toCanvas }
}))

enableAutoUnmount(afterEach)

function setValidAuthSession(userId = 7) {
  const payload = btoa(JSON.stringify({ user_id: userId }))
  authStorage.token.set(`header.${payload}.sig`)
  authStorage.user.set(JSON.stringify({ id: userId }))
}

describe('PaymentQRCodeView URL sanitization and order verification', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    mocks.routeQuery = {}
  })

  it('ignores untrusted javascript:/data: pay_url and qr query params and never renders unsafe href', async () => {
    setValidAuthSession(7)
    mocks.routeQuery = {
      order_id: '42',
      pay_url: 'javascript:alert(1)',
      qr: 'data:text/html,<script>alert(1)</script>',
      payment_type: 'alipay'
    }
    mocks.getOrder.mockResolvedValue({
      data: {
        id: 42,
        status: 'PENDING',
        payment_type: 'alipay',
        expires_at: new Date(Date.now() + 600_000).toISOString()
      }
    })

    const wrapper = mount(PaymentQRCodeView, {
      global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } }
    })
    await flushPromises()

    expect(mocks.getOrder).toHaveBeenCalledWith(42)
    expect(mocks.toCanvas).not.toHaveBeenCalled()
    expect(wrapper.find('a').exists()).toBe(false)
  })

  it('renders sanitized payment URL only from verified owned recovery snapshot', async () => {
    setValidAuthSession(7)
    mocks.routeQuery = {
      order_id: '42',
      pay_url: 'javascript:alert(1)'
    }
    localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, JSON.stringify({
      ownerUserId: 7,
      orderId: 42,
      amount: 10,
      payAmount: 10,
      qrCode: '',
      payUrl: 'https://pay.example.com/checkout/42',
      expiresAt: new Date(Date.now() + 600_000).toISOString(),
      paymentType: 'alipay',
      paymentMode: 'qr',
      resumeToken: 'rt_42',
      clientSecret: '',
      createdAt: Date.now()
    }))
    mocks.getOrder.mockResolvedValue({
      data: { id: 42, status: 'PENDING', payment_type: 'alipay', expires_at: new Date(Date.now() + 600_000).toISOString() }
    })

    const wrapper = mount(PaymentQRCodeView, {
      global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } }
    })
    await flushPromises()

    const link = wrapper.get('a')
    expect(link.attributes('href')).toBe('https://pay.example.com/checkout/42')
  })
})
