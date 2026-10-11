import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import AccountsView from '../AccountsView.vue'

const {
  listAccounts,
  listWithEtag,
  getBatchUsage,
  getBatchTodayStats,
  getAllProxies,
  getAllGroups,
  getUpstreamBillingProbeSettings
} = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  listWithEtag: vi.fn(),
  getBatchUsage: vi.fn(),
  getBatchTodayStats: vi.fn(),
  getAllProxies: vi.fn(),
  getAllGroups: vi.fn(),
  getUpstreamBillingProbeSettings: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      list: listAccounts,
      listWithEtag,
      getBatchUsage,
      getBatchTodayStats,
      getUpstreamBillingProbeSettings,
      delete: vi.fn(),
      batchClearError: vi.fn(),
      batchRefresh: vi.fn(),
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
    showError: vi.fn(),
    showSuccess: vi.fn(),
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

// Render the usage cells through the real view state.
const DataTableStub = {
  props: ['columns', 'data'],
  template: `
    <div data-test="data-table">
      <template v-for="column in columns" :key="column.key">
        <div v-if="column.key === 'usage'" data-test="usage-header">
          <slot :name="'header-' + column.key" :column="column" />
        </div>
        <div v-if="column.key === 'upstream_billing_rate'" data-test="upstream-billing-header">
          <slot :name="'header-' + column.key" :column="column" />
        </div>
      </template>
      <div v-for="row in data" :key="row.id" data-test="account-rate">
        <slot name="cell-usage" :row="row" />
      </div>
    </div>
  `
}

// Expose the content passed to HelpTooltip without dealing with its <Teleport>.
const HelpTooltipStub = {
  props: ['content', 'widthClass'],
  template: '<span data-test="usage-windows-hint">{{ content }}</span>'
}

function mountView() {
  return mount(AccountsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: {
          template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
        },
        DataTable: DataTableStub,
        HelpTooltip: HelpTooltipStub,
        Pagination: true,
        ConfirmDialog: true,
        AccountTableActions: { template: '<div><slot name="beforeCreate" /><slot name="after" /></div>' },
        AccountTableFilters: {
          props: ['groups'],
          template: '<div data-test="account-filters" :data-group-count="groups.length"></div>'
        },
        AccountBulkActionsBar: true,
        AccountActionMenu: true,
        ImportDataModal: true,
        ReAuthAccountModal: true,
        AccountTestModal: true,
        AccountStatsModal: true,
        ScheduledTestsPanel: true,
        SyncFromCrsModal: true,
        TempUnschedStatusModal: true,
        ErrorPassthroughRulesModal: true,
        TLSFingerprintProfilesModal: true,
        CreateAccountModal: true,
        EditAccountModal: true,
        BulkEditAccountModal: true,
        PlatformTypeBadge: true,
        AccountCapacityCell: true,
        AccountStatusIndicator: true,
        AccountTodayStatsCell: true,
        AccountGroupsCell: true,
        AccountUsageCell: {
          props: ['account', 'batchedUsage', 'batchedUsageError', 'batchedUsageLoading', 'requestBatchedUsage'],
          mounted() { this.requestBatchedUsage?.(this.account) },
          template: '<div :data-usage-id="account.id">{{ batchedUsage?.source }}|{{ batchedUsageError }}|{{ batchedUsageLoading }}<button @click="requestBatchedUsage(account, { force: true })" /></div>'
        },
        Icon: true
      }
    }
  })
}


describe('admin AccountsView chunked usage', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    localStorage.clear()
    vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() }))
    getBatchUsage.mockReset()
    getUpstreamBillingProbeSettings.mockResolvedValue({ enabled: true, interval_minutes: 30 })
    listAccounts.mockResolvedValue({
      items: Array.from({ length: 13 }, (_, index) => ({
        id: index + 1, name: `Account ${index + 1}`, platform: 'anthropic', type: 'oauth',
        status: 'active', credentials: {}, extra: {}, group_ids: []
      })), total: 13, page: 1, page_size: 20, pages: 1
    })
    listWithEtag.mockResolvedValue({ notModified: true, etag: null, data: null })
    getBatchTodayStats.mockResolvedValue({ stats: {} })
    getAllProxies.mockResolvedValue([])
    getAllGroups.mockResolvedValue([])
  })
  afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

  it('renders completed chunks while later chunks wait and continues after a failed chunk', async () => {
    let finishFirst!: (value: unknown) => void
    let failSecond!: (reason: Error) => void
    getBatchUsage
      .mockImplementationOnce(() => new Promise(resolve => { finishFirst = resolve }))
      .mockImplementationOnce(() => new Promise((_, reject) => { failSecond = reject }))
      .mockResolvedValueOnce({ usage: { '13': { source: 'last' } }, errors: {} })
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const wrapper = mountView()
    try {
      await flushPromises()
      await vi.advanceTimersByTimeAsync(100)
      expect(getBatchUsage.mock.calls).toEqual([[[1, 2, 3, 4, 5, 6], false]])
      finishFirst({ usage: { '1': { source: 'first' } }, errors: {} })
      await flushPromises()
      expect(getBatchUsage.mock.calls).toEqual([[[1, 2, 3, 4, 5, 6], false], [[7, 8, 9, 10, 11, 12], false]])
      expect(wrapper.get('[data-usage-id="1"]').text()).toBe('first||false')
      expect(wrapper.get('[data-usage-id="7"]').text()).toBe('||true')
      failSecond(new Error('chunk failed'))
      await flushPromises()
      expect(getBatchUsage.mock.calls[2]).toEqual([[13], false])
      expect(wrapper.get('[data-usage-id="1"]').text()).toBe('first||false')
      expect(wrapper.get('[data-usage-id="7"]').text()).toBe('|Failed|false')
      expect(wrapper.get('[data-usage-id="13"]').text()).toBe('last||false')
    } finally { wrapper.unmount() }
  })

  it('does not let a late chunk overwrite a newer forced refresh', async () => {
    let finishOld!: (value: unknown) => void
    getBatchUsage.mockImplementationOnce(() => new Promise(resolve => { finishOld = resolve }))
      .mockResolvedValue({ usage: { '1': { source: 'new' } }, errors: {} })
    const wrapper = mountView()
    try {
      await flushPromises()
      await vi.advanceTimersByTimeAsync(100)
      await wrapper.get('[data-usage-id="1"] button').trigger('click')
      await vi.advanceTimersByTimeAsync(100)
      expect(getBatchUsage.mock.calls[1]).toEqual([[1], true])
      expect(wrapper.get('[data-usage-id="1"]').text()).toBe('new||false')
      finishOld({ usage: { '1': { source: 'old' } }, errors: {} })
      await flushPromises()
      expect(wrapper.get('[data-usage-id="1"]').text()).toBe('new||false')
    } finally { wrapper.unmount() }
  })

})
