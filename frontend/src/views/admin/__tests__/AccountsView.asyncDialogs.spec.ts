import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import AccountsView from '../AccountsView.vue'

const {
  listAccounts,
  listWithEtag,
  getUpstreamBillingRatesWithEtag,
  getBatchTodayStats,
  getUpstreamBillingProbeSettings,
  getAllProxies,
  getAllGroups,
  probeUpstreamBilling,
  probeUpstreamBillingBatch,
  showError,
  showSuccess
} = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  listWithEtag: vi.fn(),
  getUpstreamBillingRatesWithEtag: vi.fn(),
  getBatchTodayStats: vi.fn(),
  getUpstreamBillingProbeSettings: vi.fn(),
  getAllProxies: vi.fn(),
  getAllGroups: vi.fn(),
  probeUpstreamBilling: vi.fn(),
  probeUpstreamBillingBatch: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      list: listAccounts,
      listWithEtag,
      getUpstreamBillingRatesWithEtag,
      getBatchTodayStats,
      getUpstreamBillingProbeSettings,
      delete: vi.fn(),
      batchClearError: vi.fn(),
      batchRefresh: vi.fn(),
      probeUpstreamBilling,
      probeUpstreamBillingBatch,
      toggleSchedulable: vi.fn()
    },
    proxies: {
      getAll: getAllProxies
    },
    groups: {
      getAll: getAllGroups
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
    showInfo: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    token: 'test-token'
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})


const dialogLoads = vi.hoisted(() => ({ create: vi.fn(), resolve: null as null | ((component: unknown) => void) }))
vi.mock('@/components/account/CreateAccountModal.vue', () => {
  dialogLoads.create()
  return new Promise(resolve => { dialogLoads.resolve = resolve })
})

describe('AccountsView lazy dialogs and refresh accessibility', () => {
  beforeEach(() => {
    localStorage.clear()
    listAccounts.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 0 })
    getAllProxies.mockResolvedValue([])
    getAllGroups.mockResolvedValue([])
    getBatchTodayStats.mockResolvedValue({})
    getUpstreamBillingProbeSettings.mockResolvedValue({})
  })
  it('loads only on first open, retains a draft while open, and exposes refresh state', async () => {
    const wrapper = mount(AccountsView, { attachTo: document.body, global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      TablePageLayout: { template: '<div><slot name="filters" /></div>' },
      AccountTableFilters: true,
      AccountTableActions: { template: '<div><button data-create @click="$emit(\'create\')">Create</button><slot name="after" /></div>' },
      AccountActionMenu: true, ConfirmDialog: true, TotpStepUpDialog: true, Icon: true
    } } })
    await flushPromises()
    expect(dialogLoads.create).not.toHaveBeenCalled()
    const refresh = wrapper.get('[aria-controls="account-auto-refresh-options"]')
    expect(refresh.attributes('aria-expanded')).toBe('false')
    await refresh.trigger('click')
    expect(refresh.attributes('aria-expanded')).toBe('true')
    const options = wrapper.get('#account-auto-refresh-options')
    const enabled = options.get('button')
    expect(enabled.attributes('aria-pressed')).toBe('false')
    await enabled.trigger('click')
    expect(enabled.attributes('aria-pressed')).toBe('true')
    await options.trigger('keydown', { key: 'Escape' })
    expect(refresh.attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(refresh.element)
    await wrapper.get('[data-create]').trigger('click')
    await vi.waitFor(() => expect(dialogLoads.create).toHaveBeenCalledTimes(1))
    dialogLoads.resolve!({ __esModule: true, default: { props: ['show'], data: () => ({ draft: '', initialized: false }), watch: { show(this: { initialized: boolean }, value: boolean) { this.initialized = value } }, template: '<div><input v-if="initialized" v-model="draft" /><button data-close @click="$emit(\'close\')">Close</button></div>' } })
    await flushPromises()
    await wrapper.get('input').setValue('unsaved draft')
    await wrapper.get('[data-create]').trigger('click')
    expect((wrapper.get('input').element as HTMLInputElement).value).toBe('unsaved draft')
    await wrapper.get('[data-close]').trigger('click')
    expect(wrapper.find('input').exists()).toBe(false)
    await wrapper.get('[data-create]').trigger('click')
    await flushPromises()
    expect(dialogLoads.create).toHaveBeenCalledTimes(1)
    expect((wrapper.get('input').element as HTMLInputElement).value).toBe('')
    wrapper.unmount()
  })
})
