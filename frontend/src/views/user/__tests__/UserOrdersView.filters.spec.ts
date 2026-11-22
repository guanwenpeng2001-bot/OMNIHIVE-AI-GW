import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import UserOrdersView from '../UserOrdersView.vue'
import Select from '@/components/common/Select.vue'
import Pagination from '@/components/common/Pagination.vue'

const api = vi.hoisted(() => ({
  getMyOrders: vi.fn(),
  getRefundEligibleProviders: vi.fn(),
  showError: vi.fn(),
  cachedPublicSettings: { payment_enabled: true } as { payment_enabled?: boolean }
}))
vi.mock('@/api/payment', () => ({ paymentAPI: api }))
vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError: api.showError,
    cachedPublicSettings: api.cachedPublicSettings
  })
}))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)

beforeEach(() => {
  vi.clearAllMocks()
  api.cachedPublicSettings = { payment_enabled: true }
  api.getMyOrders.mockResolvedValue({ data: { items: [], total: 100 } })
  api.getRefundEligibleProviders.mockResolvedValue({ data: { provider_instance_ids: [] } })
})

async function openOrders() {
  const wrapper = mount(UserOrdersView, {
    global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      BillingNav: true, OrderTable: true, BaseDialog: true, Icon: true, Pagination: true, teleport: true
    } }
  })
  await flushPromises()
  wrapper.getComponent(Pagination).vm.$emit('update:page', 4)
  await flushPromises()
  return wrapper
}

describe('order status filtering', () => {
  it('loads the first page with the selected status', async () => {
    const wrapper = await openOrders()
    const select = wrapper.getComponent(Select)
    await select.get('button').trigger('click')
    await select.findAll('[role="option"]').find(option => option.text() === 'payment.status.pending')!.trigger('click')
    await flushPromises()
    expect(api.getMyOrders).toHaveBeenLastCalledWith({ page: 1, page_size: 20, status: 'PENDING' })
    expect(wrapper.getComponent(Pagination).props('page')).toBe(1)
    expect(api.getMyOrders).toHaveBeenCalledTimes(3)
  })

  it('keeps the current page on manual refresh', async () => {
    const wrapper = await openOrders()
    await wrapper.get('[title="common.refresh"]').trigger('click')
    await flushPromises()
    expect(api.getMyOrders).toHaveBeenLastCalledWith({ page: 4, page_size: 20, status: undefined })
  })

  it('renders order invoice and receipt human-support instructions [E1-4]', async () => {
    const wrapper = await openOrders()
    const invoiceCard = wrapper.find('[data-testid="order-invoice-support"]')
    expect(invoiceCard.exists()).toBe(true)
    expect(invoiceCard.html()).toContain('/docs#support')
  })

  it('skips order queries, suppresses error toasts, and hides /purchase CTA when payment_enabled is false', async () => {
    api.cachedPublicSettings = { payment_enabled: false }
    const wrapper = mount(UserOrdersView, {
      global: { stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        BillingNav: true, OrderTable: true, BaseDialog: true, Icon: true, Pagination: true, teleport: true
      } }
    })
    await flushPromises()

    expect(api.getMyOrders).not.toHaveBeenCalled()
    expect(api.getRefundEligibleProviders).not.toHaveBeenCalled()
    expect(api.showError).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="payment-disabled-notice"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('payment.result.backToRecharge')
  })
})
