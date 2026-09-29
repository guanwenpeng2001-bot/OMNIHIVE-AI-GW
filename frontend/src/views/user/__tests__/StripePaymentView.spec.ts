import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'

const routeState = vi.hoisted(() => ({
  query: {} as Record<string, unknown>,
}))
const routerPush = vi.hoisted(() => vi.fn())
const getOrder = vi.hoisted(() => vi.fn())
const paymentStore = vi.hoisted(() => ({
  config: { stripe_publishable_key: 'pk_test' } as { stripe_publishable_key?: string },
  fetchConfig: vi.fn(),
  pollOrderStatus: vi.fn(),
}))
const loadStripe = vi.hoisted(() => vi.fn())
const stripeElements = vi.hoisted(() => ({
  create: vi.fn(),
}))
const stripePaymentElement = vi.hoisted(() => ({
  destroy: vi.fn(),
  mount: vi.fn(),
  on: vi.fn(),
}))
const stripeInstance = vi.hoisted(() => ({
  elements: vi.fn(),
  confirmPayment: vi.fn(),
  confirmAlipayPayment: vi.fn(),
  confirmWechatPayPayment: vi.fn(),
}))

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return {
    ...actual,
    useRoute: () => routeState,
    useRouter: () => ({ push: routerPush }),
  }
})

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
      locale: { value: 'zh-CN' },
    }),
  }
})

vi.mock('@/stores/payment', () => ({
  usePaymentStore: () => paymentStore,
}))

vi.mock('@/api/payment', () => ({
  paymentAPI: {
    getOrder,
  },
}))

vi.mock('@stripe/stripe-js/pure', () => ({
  loadStripe,
}))

import StripePaymentView from '../StripePaymentView.vue'
import { formatPaymentAmount } from '@/components/payment/currency'
import type { PaymentOrder } from '@/types/payment'

function orderFactory(overrides: Partial<PaymentOrder> = {}): PaymentOrder {
  return {
    id: 42,
    user_id: 7,
    amount: 100,
    pay_amount: 103,
    currency: 'CNY',
    fee_rate: 0.03,
    payment_type: 'stripe',
    out_trade_no: 'sub2_stripe_42',
    status: 'PENDING',
    order_type: 'balance',
    created_at: '2026-04-20T12:00:00Z',
    expires_at: '2026-04-20T12:30:00Z',
    refund_amount: 0,
    ...overrides,
  }
}

function mountView() {
  return shallowMount(StripePaymentView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        Icon: true,
      },
    },
  })
}

describe('StripePaymentView', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })
  beforeEach(() => {
    routeState.query = {
      order_id: '42',
      client_secret: 'pi_secret_42',
    }
    routerPush.mockReset()
    getOrder.mockReset()
    paymentStore.config = { stripe_publishable_key: 'pk_test' }
    paymentStore.fetchConfig.mockReset().mockResolvedValue(undefined)
    paymentStore.pollOrderStatus.mockReset()
    loadStripe.mockReset().mockResolvedValue(stripeInstance)
    stripeElements.create.mockReset().mockReturnValue(stripePaymentElement)
    stripePaymentElement.mount.mockReset()
    stripePaymentElement.on.mockReset().mockImplementation((event: string, callback: () => void) => {
      if (event === 'ready') callback()
    })
    stripeInstance.elements.mockReset().mockReturnValue(stripeElements)
    stripeInstance.confirmPayment.mockReset()
    stripeInstance.confirmAlipayPayment.mockReset()
    stripeInstance.confirmWechatPayPayment.mockReset()
    window.localStorage.clear()
  })

  it('本地恢复快照缺失时使用订单接口返回的 Stripe 币种展示金额', async () => {
    getOrder.mockResolvedValue({
      data: orderFactory({ currency: 'HKD', pay_amount: 103 }),
    })

    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    expect(getOrder).toHaveBeenCalledWith(42, expect.any(AbortSignal))
    expect(loadStripe).toHaveBeenCalledWith('pk_test')
    expect(wrapper.text()).toContain(formatPaymentAmount(103, 'HKD', 'zh-CN'))
    wrapper.unmount()
  })

  it.each([false, true])('ignores a pending poll after unmount (popup=%s)', async (popup) => {
    vi.useFakeTimers()
    vi.stubGlobal('opener', popup ? {} : null)
    const close = vi.spyOn(window, 'close').mockImplementation(() => {})
    routeState.query.method = 'wechat_pay'
    getOrder.mockResolvedValue({ data: orderFactory() })
    stripeInstance.confirmWechatPayPayment.mockResolvedValue({ paymentIntent: {
      next_action: { wechat_pay_display_qr_code: { image_data_url: 'qr' } },
    } })
    let resolve!: (order: PaymentOrder) => void
    paymentStore.pollOrderStatus.mockReturnValue(new Promise<PaymentOrder>(done => { resolve = done }))
    const wrapper = mountView()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(9000)
    expect(paymentStore.pollOrderStatus).toHaveBeenCalledTimes(1)
    wrapper.unmount()
    expect(paymentStore.pollOrderStatus.mock.calls[0][1].aborted).toBe(true)
    resolve(orderFactory({ status: 'PAID' }))
    await flushPromises()
    await vi.advanceTimersByTimeAsync(6000)
    expect(routerPush).not.toHaveBeenCalled()
    expect(close).not.toHaveBeenCalled()
  })

  it('does not start polling when Stripe confirmation resolves after unmount', async () => {
    vi.useFakeTimers()
    routeState.query.method = 'wechat_pay'
    getOrder.mockResolvedValue({ data: orderFactory() })
    let resolve!: (value: unknown) => void
    stripeInstance.confirmWechatPayPayment.mockReturnValue(new Promise(done => { resolve = done }))
    const wrapper = mountView()
    await flushPromises()
    wrapper.unmount()
    resolve({ paymentIntent: { next_action: { wechat_pay_display_qr_code: { image_data_url: 'qr' } } } })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(9000)
    expect(paymentStore.pollOrderStatus).not.toHaveBeenCalled()
    expect(routerPush).not.toHaveBeenCalled()
  })

  it('does not initialize Stripe when order loading finishes after unmount', async () => {
    let resolve!: (value: { data: PaymentOrder }) => void
    getOrder.mockReturnValue(new Promise(done => { resolve = done }))
    const wrapper = mountView()
    wrapper.unmount()
    resolve({ data: orderFactory() })
    await flushPromises()
    expect(paymentStore.fetchConfig).not.toHaveBeenCalled()
    expect(loadStripe).not.toHaveBeenCalled()
  })

  it('navigates once after a live successful poll', async () => {
    vi.useFakeTimers()
    routeState.query.method = 'wechat_pay'
    getOrder.mockResolvedValue({ data: orderFactory() })
    stripeInstance.confirmWechatPayPayment.mockResolvedValue({ paymentIntent: {
      next_action: { wechat_pay_display_qr_code: { image_data_url: 'qr' } },
    } })
    paymentStore.pollOrderStatus.mockResolvedValue(orderFactory({ status: 'COMPLETED' }))
    const wrapper = mountView()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(12000)
    expect(paymentStore.pollOrderStatus).toHaveBeenCalledTimes(1)
    expect(routerPush).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
  it.each(['identity', 'route'])('does not initialize Stripe for an old order after %s changes', async (change) => {
    localStorage.setItem('auth_token', 'token-a')
    localStorage.setItem('auth_user', JSON.stringify({ id: 1 }))
    let resolve!: (value: unknown) => void
    getOrder.mockReturnValue(new Promise(done => { resolve = done }))
    const wrapper = mountView()
    if (change === 'identity') localStorage.setItem('auth_user', JSON.stringify({ id: 2 }))
    if (change === 'route') routeState.query = { order_id: '43', client_secret: 'new-secret' }
    resolve({ data: orderFactory() })
    await flushPromises()
    expect(loadStripe).not.toHaveBeenCalled()
    expect(getOrder.mock.calls[0][1].aborted).toBe(true)
    wrapper.unmount()
  })

})
