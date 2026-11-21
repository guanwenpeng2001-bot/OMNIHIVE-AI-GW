import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AdminGroup } from '@/types'
import GroupsView from '@/views/admin/GroupsView.vue'

const {
  listGroups,
  createGroup,
  getAllGroups,
  getModelAllowlistCandidates,
  getUsageSummary,
  getCapacitySummary,
  getLiveCapability,
  listAccounts,
  showSuccess,
  showError
} = vi.hoisted(() => ({
  listGroups: vi.fn(),
  createGroup: vi.fn(),
  getAllGroups: vi.fn(),
  getModelAllowlistCandidates: vi.fn(),
  getUsageSummary: vi.fn(),
  getCapacitySummary: vi.fn(),
  getLiveCapability: vi.fn(),
  listAccounts: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

const authState = vi.hoisted(() => ({ isSimpleMode: false }))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    groups: {
      list: listGroups,
      create: createGroup,
      getAll: getAllGroups,
      getModelAllowlistCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      duplicate: vi.fn(),
      update: vi.fn(),
      delete: vi.fn(),
      updateSortOrder: vi.fn()
    },
    accounts: {
      list: listAccounts,
      getById: vi.fn()
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authState
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({
    isCurrentStep: vi.fn(() => false),
    nextStep: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } })
  }
})

const existingGroup: AdminGroup = {
  id: 42,
  name: 'Workspace OpenAI',
  description: null,
  platform: 'openai',
  rate_multiplier: 1,
  rpm_limit: 0,
  is_exclusive: false,
  status: 'active',
  subscription_type: 'standard',
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  allow_image_generation: false,
  allow_batch_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  batch_image_discount_multiplier: 0.5,
  batch_image_hold_multiplier: 0.6,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  video_rate_independent: false,
  video_rate_multiplier: 1,
  video_price_480p: null,
  video_price_720p: null,
  video_price_1080p: null,
  web_search_price_per_call: null,
  peak_rate_enabled: false,
  peak_start: '',
  peak_end: '',
  peak_rate_multiplier: 1,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  allow_messages_dispatch: false,
  default_mapped_model: '',
  messages_dispatch_model_config: undefined,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: '2026-07-16T00:00:00Z',
  updated_at: '2026-07-16T00:00:00Z',
  model_routing: null,
  model_routing_enabled: false,
  mcp_xml_inject: true,
  supported_model_scopes: [],
  account_count: 1,
  active_account_count: 1,
  rate_limited_account_count: 0,
  model_allowlist: undefined,
  sort_order: 10
}

const AppLayoutStub = defineComponent({
  template: '<main><slot /></main>'
})

const TablePageLayoutStub = defineComponent({
  template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>'
})

const DataTableStub = defineComponent({
  props: {
    data: { type: Array, default: () => [] },
    columns: { type: Array, default: () => [] },
    loading: { type: Boolean, default: false }
  },
  template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-actions" :row="row" /></div></div>'
})

const BaseDialogStub = defineComponent({
  props: {
    show: { type: Boolean, default: false }
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

const SelectStub = defineComponent({
  props: {
    modelValue: { type: [String, Number, null], default: '' },
    options: { type: Array as () => Array<{ value: string; label: string }>, default: () => [] }
  },
  emits: ['update:modelValue', 'change'],
  methods: {
    onChange(event: Event) {
      const value = (event.target as HTMLSelectElement).value
      this.$emit('update:modelValue', value)
      this.$emit('change', value)
    }
  },
  template: `
    <select
      :value="modelValue"
      @change="onChange"
    >
      <option v-for="opt in options" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
    </select>
  `
})

function mountView() {
  return mount(GroupsView, {
    attachTo: document.body,
    global: {
      stubs: {
        'router-link': true,
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: true,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        EmptyState: true,
        Select: SelectStub,
        PlatformIcon: true,
        Icon: true,
        GroupCapacityBadge: true,
        GroupRateMultipliersModal: true,
        GroupRPMOverridesModal: true,
        VueDraggable: true
      }
    }
  })
}

describe('GroupsView create model allowlist candidates guard', () => {
  let consoleErrorSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    authState.isSimpleMode = false
    localStorage.clear()
    consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    for (const fn of [
      listGroups,
      createGroup,
      getAllGroups,
      getModelAllowlistCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      listAccounts,
      showSuccess,
      showError
    ]) {
      fn.mockReset()
    }

    listGroups.mockResolvedValue({
      items: [existingGroup],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    getAllGroups.mockResolvedValue([existingGroup])
    createGroup.mockResolvedValue({ ...existingGroup, id: 77, name: 'New Group' })
    getModelAllowlistCandidates.mockRejectedValue(new Error('WORKSPACE_NOT_FOUND'))
    getUsageSummary.mockResolvedValue([])
    getCapacitySummary.mockResolvedValue([])
    getLiveCapability.mockResolvedValue({ supported: false })
    listAccounts.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 0 })
  })

  afterEach(() => {
    vi.restoreAllMocks()
    document.body.innerHTML = ''
  })

  it('never requests group ID 0 candidates on mount, create modal open, or create platform switch, and creates cleanly without console.error', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(getModelAllowlistCandidates).not.toHaveBeenCalled()
    expect(consoleErrorSpy).not.toHaveBeenCalled()

    await wrapper.get('[data-tour="groups-create-btn"]').trigger('click')
    await flushPromises()

    expect(getModelAllowlistCandidates).not.toHaveBeenCalled()
    expect(consoleErrorSpy).not.toHaveBeenCalled()

    const platformSelect = wrapper.get('[data-tour="group-form-platform"]')
    await platformSelect.setValue('openai')
    await flushPromises()
    await platformSelect.setValue('gemini')
    await flushPromises()

    expect(getModelAllowlistCandidates).not.toHaveBeenCalled()
    expect(consoleErrorSpy).not.toHaveBeenCalled()

    // Custom allowlist entry and form creation work without candidate preloading
    const modelsSection = wrapper.get('#create-group-form [data-group-section="models"] [role="switch"]')
    await modelsSection.trigger('click')
    await flushPromises()

    expect(wrapper.get('#create-group-form').text()).toContain('admin.groups.modelAllowlist.empty')
    const customInput = wrapper.get('#create-group-form input[placeholder="admin.groups.modelAllowlist.customPlaceholder"]')
    await customInput.setValue('gemini-*')
    await wrapper.findAll('#create-group-form button').find((b) => b.text() === 'admin.groups.modelAllowlist.addCustom')!.trigger('click')
    await flushPromises()

    await wrapper.get('[data-tour="group-form-name"]').setValue('ws-gemini')
    const submitBtn = wrapper.get('button[form="create-group-form"]')
    ;(submitBtn.element as HTMLButtonElement).click()
    await flushPromises()

    expect(createGroup).toHaveBeenCalledWith(
      expect.objectContaining({
        name: 'ws-gemini',
        platform: 'gemini',
        model_allowlist: { enabled: true, models: ['gemini-*'] }
      })
    )
    expect(getModelAllowlistCandidates).not.toHaveBeenCalled()
    expect(consoleErrorSpy).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('still fetches allowlist candidates using the real positive group ID when editing an existing group', async () => {
    getModelAllowlistCandidates.mockResolvedValue(['gpt-5.4', 'gpt-5.4-mini'])
    const wrapper = mountView()
    await flushPromises()

    expect(getModelAllowlistCandidates).not.toHaveBeenCalled()

    await wrapper.findAll('button').find((b) => b.text() === 'common.edit')!.trigger('click')
    await flushPromises()

    expect(getModelAllowlistCandidates).toHaveBeenCalledTimes(1)
    expect(getModelAllowlistCandidates).toHaveBeenCalledWith(42, 'openai')
    wrapper.unmount()
  })
})
