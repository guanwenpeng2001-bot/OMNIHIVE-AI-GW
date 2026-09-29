import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView, type Router } from 'vue-router'
import { paymentRouteComponent } from '@/components/payment/paymentRoute'
import { PAYMENT_RECOVERY_STORAGE_KEY } from '@/components/payment/paymentFlow'
import PaymentResultView from '@/views/user/PaymentResultView.vue'
import StripePaymentView from '@/views/user/StripePaymentView.vue'
import AirwallexPaymentView from '@/views/user/AirwallexPaymentView.vue'

const mocks = vi.hoisted(() => ({
  pollOrderStatus: vi.fn(), getOrder: vi.fn(), refreshUser: vi.fn(),
  resolveOrderPublicByResumeToken: vi.fn(),
  fetchConfig: vi.fn(), loadStripe: vi.fn(), confirmPayment: vi.fn(),
  confirmWechatPayPayment: vi.fn(), elementMount: vi.fn(), elementDestroy: vi.fn(),
  elementOn: vi.fn(), airwallexInit: vi.fn(), redirectToCheckout: vi.fn(),
}))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }),
}))
vi.mock('@/stores/payment', () => ({ usePaymentStore: () => ({
  pollOrderStatus: mocks.pollOrderStatus, fetchConfig: mocks.fetchConfig,
  config: { stripe_publishable_key: 'pk_test' },
}) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ refreshUser: mocks.refreshUser }) }))
vi.mock('@/api/payment', () => ({ paymentAPI: {
  getOrder: mocks.getOrder, resolveOrderPublicByResumeToken: mocks.resolveOrderPublicByResumeToken,
} }))
vi.mock('@stripe/stripe-js/pure', () => ({ loadStripe: mocks.loadStripe }))
vi.mock('@airwallex/components-sdk', () => ({ init: mocks.airwallexInit }))

function deferred<Value>() {
  let resolve!: (value: Value) => void
  const promise = new Promise<Value>(done => { resolve = done })
  return { promise, resolve }
}
function order(id: number, status = 'PAID') {
  return { id, status, amount: id * 10, pay_amount: id * 10, fee_rate: 0,
    payment_type: 'stripe', order_type: 'balance', out_trade_no: `order-${id}` }
}

let wrapper: VueWrapper
async function openPage(url: string) {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/payment/result', component: paymentRouteComponent(PaymentResultView, 'result') },
    { path: '/payment/stripe', component: paymentRouteComponent(StripePaymentView, 'stripe') },
    { path: '/payment/airwallex', component: paymentRouteComponent(AirwallexPaymentView, 'airwallex') },
  ] })
  await router.push(url)
  await router.isReady()
  wrapper = mount(RouterView, { global: { plugins: [router], stubs: {
    AppLayout: { template: '<div><slot /></div>' }, Icon: true, OrderStatusBadge: true,
  } } })
  await flushPromises()
  return router
}
async function back(router: Router) {
  const navigated = new Promise<void>(resolve => {
    const remove = router.afterEach(() => { remove(); resolve() })
  })
  router.back()
  await navigated
  await flushPromises()
}
function storeAirwallexOrder(id: number) {
  localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, JSON.stringify({
    ownerUserId: 9, orderId: id, amount: 10, payAmount: 10, qrCode: '',
    expiresAt: '2099-01-01T00:00:00Z', paymentType: 'airwallex', payUrl: '',
    outTradeNo: '', clientSecret: `secret-${id}`, intentId: `intent-${id}`,
    currency: 'CNY', countryCode: 'CN', paymentEnv: 'demo', orderType: 'balance',
    paymentMode: '', resumeToken: '', createdAt: Date.now(),
  }))
}

beforeEach(() => {
  vi.resetAllMocks()
  localStorage.clear()
  localStorage.setItem('auth_token', 'token')
  localStorage.setItem('auth_user', JSON.stringify({ id: 9 }))
  mocks.refreshUser.mockResolvedValue({})
  mocks.fetchConfig.mockResolvedValue(undefined)
  mocks.getOrder.mockImplementation(async (id: number) => ({ data: order(id) }))
  mocks.loadStripe.mockResolvedValue({
    elements: () => ({ create: () => ({ mount: mocks.elementMount, on: mocks.elementOn, destroy: mocks.elementDestroy }) }),
    confirmPayment: mocks.confirmPayment, confirmWechatPayPayment: mocks.confirmWechatPayPayment,
  })
  mocks.airwallexInit.mockResolvedValue({ payments: { redirectToCheckout: mocks.redirectToCheckout } })
})
afterEach(() => {
  wrapper?.unmount()
  vi.useRealTimers()
})

describe('payment route generations with real router navigation', () => {
  it('cancels the initial result request, shows order 2, and reloads order 1 on back', async () => {
    const pending = deferred<ReturnType<typeof order>>()
    mocks.pollOrderStatus.mockReturnValueOnce(pending.promise).mockImplementation(async (id: number) => order(id))
    const router = await openPage('/payment/result?order_id=1')
    const initialSignal = mocks.pollOrderStatus.mock.calls[0][1] as AbortSignal
    expect(wrapper.text()).not.toContain('#1')
    await router.push('/payment/result?order_id=2')
    await flushPromises()
    expect(initialSignal.aborted).toBe(true)
    expect(wrapper.text()).toContain('#2')
    expect(wrapper.text()).toContain('payment.result.success')
    pending.resolve(order(1, 'COMPLETED'))
    await flushPromises()
    expect(wrapper.text()).not.toContain('#1')
    expect(mocks.refreshUser).not.toHaveBeenCalled()
    await back(router)
    expect(mocks.pollOrderStatus.mock.calls.map(call => call[0])).toEqual([1, 2, 1])
    expect(wrapper.text()).toContain('#1')
    expect((mocks.pollOrderStatus.mock.calls[2][1] as AbortSignal).aborted).toBe(false)
  })

  it('keeps a pending request live when only display query changes', async () => {
    const pending = deferred<ReturnType<typeof order>>()
    mocks.pollOrderStatus.mockReturnValue(pending.promise)
    const router = await openPage('/payment/result?order_id=1')
    const instance = wrapper.findComponent(PaymentResultView).vm.$.uid
    await router.push('/payment/result?order_id=1&status=success&money=999&type=alipay')
    pending.resolve(order(1))
    await flushPromises()
    expect(wrapper.findComponent(PaymentResultView).vm.$.uid).toBe(instance)
    expect(mocks.pollOrderStatus).toHaveBeenCalledTimes(1)
    expect((mocks.pollOrderStatus.mock.calls[0][1] as AbortSignal).aborted).toBe(false)
    expect(wrapper.text()).toContain('#1')
  })

  it('does not commit or rearm an old result poll after switching orders', async () => {
    vi.useFakeTimers()
    const pending = deferred<ReturnType<typeof order>>()
    mocks.pollOrderStatus.mockResolvedValueOnce(order(1, 'PENDING')).mockReturnValueOnce(pending.promise)
      .mockResolvedValue(order(2))
    const router = await openPage('/payment/result?order_id=1')
    await vi.advanceTimersByTimeAsync(2000)
    await router.push('/payment/result?order_id=2')
    await flushPromises()
    expect((mocks.pollOrderStatus.mock.calls[1][1] as AbortSignal).aborted).toBe(true)
    localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, 'new-order-recovery')
    pending.resolve(order(1, 'COMPLETED'))
    await flushPromises()
    await vi.advanceTimersByTimeAsync(20000)
    expect(mocks.pollOrderStatus).toHaveBeenCalledTimes(3)
    expect(mocks.refreshUser).not.toHaveBeenCalled()
    expect(localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBe('new-order-recovery')
    expect(wrapper.text()).toContain('#2')
  })

  it('replaces token-only return context without using the token as the component key', async () => {
    const pending = deferred<{ data: ReturnType<typeof order> }>()
    mocks.resolveOrderPublicByResumeToken.mockReturnValueOnce(pending.promise)
      .mockResolvedValue({ data: order(2) })
    const router = await openPage('/payment/result?resume_token=private-first-token')
    const initialKey = wrapper.findComponent(PaymentResultView).vm.$.vnode.key
    await router.push('/payment/result?resume_token=private-next-token')
    await flushPromises()
    const nextKey = wrapper.findComponent(PaymentResultView).vm.$.vnode.key
    expect(typeof initialKey).toBe('number')
    expect(typeof nextKey).toBe('number')
    expect(nextKey).not.toBe(initialKey)
    expect((mocks.resolveOrderPublicByResumeToken.mock.calls[0][1] as AbortSignal).aborted).toBe(true)
    pending.resolve({ data: order(1, 'COMPLETED') })
    await flushPromises()
    expect(wrapper.text()).toContain('#2')
    expect(mocks.refreshUser).not.toHaveBeenCalled()
    expect(wrapper.html()).not.toContain('private-')
  })

  it('cancels pending Stripe order loading and initializes the next order and back', async () => {
    const pending = deferred<{ data: ReturnType<typeof order> }>()
    mocks.getOrder.mockReturnValueOnce(pending.promise)
    const router = await openPage('/payment/stripe?order_id=1&client_secret=secret-one')
    await router.push('/payment/stripe?order_id=2&client_secret=secret-two')
    await flushPromises()
    expect((mocks.getOrder.mock.calls[0][1] as AbortSignal).aborted).toBe(true)
    expect(wrapper.find('button.btn-stripe').exists()).toBe(true)
    expect(mocks.loadStripe).toHaveBeenCalledTimes(1)
    await back(router)
    pending.resolve({ data: order(1) })
    await flushPromises()
    expect(mocks.getOrder.mock.calls.map(call => call[0])).toEqual([1, 2, 1])
    expect(mocks.loadStripe).toHaveBeenCalledTimes(2)
    expect(wrapper.find('button.btn-stripe').exists()).toBe(true)
  })

  it('rebuilds Stripe for another order and back while rejecting stale ready and confirmation callbacks', async () => {
    vi.useFakeTimers()
    const pending = deferred<object>()
    mocks.confirmPayment.mockReturnValueOnce(pending.promise)
    const router = await openPage('/payment/stripe?order_id=1&client_secret=secret-one')
    const ready = mocks.elementOn.mock.calls[0][1] as () => void
    ready()
    await flushPromises()
    await wrapper.get('button.btn-stripe').trigger('click')
    expect(mocks.confirmPayment).toHaveBeenCalledTimes(1)
    await router.push('/payment/stripe?order_id=2&client_secret=secret-two')
    await flushPromises()
    expect(mocks.elementDestroy).toHaveBeenCalledTimes(1)
    expect((mocks.getOrder.mock.calls[0][1] as AbortSignal).aborted).toBe(true)
    ready()
    pending.resolve({})
    await flushPromises()
    await vi.advanceTimersByTimeAsync(4000)
    expect(router.currentRoute.value.path).toBe('/payment/stripe')
    expect(wrapper.get('button.btn-stripe').attributes('disabled')).toBeDefined()
    const currentInstance = wrapper.findComponent(StripePaymentView).vm.$.uid
    await router.push('/payment/stripe?order_id=2&client_secret=secret-two&status=success')
    await flushPromises()
    expect(wrapper.findComponent(StripePaymentView).vm.$.uid).toBe(currentInstance)
    expect(mocks.getOrder).toHaveBeenCalledTimes(2)
    await back(router)
    await back(router)
    expect(mocks.getOrder.mock.calls.map(call => call[0])).toEqual([1, 2, 1])
    expect(mocks.elementMount).toHaveBeenCalledTimes(3)
    expect(wrapper.html()).not.toContain('secret-one')
  })

  it('ignores a late Stripe poll and never navigates the next order', async () => {
    vi.useFakeTimers()
    const pending = deferred<ReturnType<typeof order>>()
    mocks.confirmWechatPayPayment.mockResolvedValue({ paymentIntent: {
      next_action: { wechat_pay_display_qr_code: { image_data_url: 'qr' } },
    } })
    mocks.pollOrderStatus.mockReturnValueOnce(pending.promise)
    const router = await openPage('/payment/stripe?order_id=1&client_secret=secret-one&method=wechat_pay')
    await vi.advanceTimersByTimeAsync(3000)
    await router.push('/payment/stripe?order_id=2&client_secret=secret-two')
    await flushPromises()
    pending.resolve(order(1, 'COMPLETED'))
    await flushPromises()
    await vi.advanceTimersByTimeAsync(10000)
    expect((mocks.pollOrderStatus.mock.calls[0][1] as AbortSignal).aborted).toBe(true)
    expect(mocks.pollOrderStatus).toHaveBeenCalledTimes(1)
    expect(router.currentRoute.value.query.order_id).toBe('2')
    expect(router.currentRoute.value.path).toBe('/payment/stripe')
  })

  it('restarts Airwallex initialization for another order and back without redirecting the old order', async () => {
    const pending = deferred<object>()
    mocks.airwallexInit.mockReturnValueOnce(pending.promise)
    storeAirwallexOrder(1)
    const router = await openPage('/payment/airwallex?order_id=1')
    storeAirwallexOrder(2)
    await router.push('/payment/airwallex?order_id=2')
    await flushPromises()
    expect(mocks.airwallexInit).toHaveBeenCalledTimes(2)
    expect(mocks.redirectToCheckout).toHaveBeenCalledWith(expect.objectContaining({ intent_id: 'intent-2' }))
    pending.resolve({ payments: { redirectToCheckout: mocks.redirectToCheckout } })
    await flushPromises()
    expect(mocks.redirectToCheckout).toHaveBeenCalledTimes(1)
    await router.push('/payment/airwallex?order_id=2&status=success')
    await flushPromises()
    expect(mocks.airwallexInit).toHaveBeenCalledTimes(2)
    await back(router)
    storeAirwallexOrder(1)
    await back(router)
    expect(mocks.airwallexInit).toHaveBeenCalledTimes(3)
    expect(mocks.redirectToCheckout).toHaveBeenLastCalledWith(expect.objectContaining({ intent_id: 'intent-1' }))
    expect(wrapper.html()).not.toContain('secret-1')
  })
})
