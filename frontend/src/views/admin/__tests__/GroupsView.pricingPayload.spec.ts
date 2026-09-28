import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { sourceGroup, simpleCard, complexCard } from './groupsPricingFixture'
import GroupsView from '@/views/admin/GroupsView.vue'
import { apiClient } from '@/api/client'
import { update } from '@/api/admin/groups'
import type { ChannelModelPricing } from '@/api/admin/channels'
import type { AxiosAdapter } from 'axios'

const {
  listGroups,
  duplicateGroup,
  getModelAllowlistCandidates,
  getUsageSummary,
  getCapacitySummary,
  getLiveCapability,
  showSuccess,
  showError
} = vi.hoisted(() => ({
  listGroups: vi.fn(),
  duplicateGroup: vi.fn(),
  getModelAllowlistCandidates: vi.fn(),
  getUsageSummary: vi.fn(),
  getCapacitySummary: vi.fn(),
  getLiveCapability: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

const authState = vi.hoisted(() => ({ isSimpleMode: false }))

vi.mock('@/api/admin', async () => ({
  adminAPI: {
    groups: {
      list: listGroups,
      duplicate: duplicateGroup,
      getModelAllowlistCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      getAll: vi.fn(),
      create: vi.fn(),
      update: (await import('@/api/admin/groups')).update,
      delete: vi.fn(),
      updateSortOrder: vi.fn()
    },
    accounts: {
      list: vi.fn(),
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
    useI18n: () => ({ t: (key: string) => key })
  }
})

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

function mountView() {
  return mount(GroupsView, {
    attachTo: document.body,
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: true,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        EmptyState: true,
        Select: true,
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

let payloads: Record<string, unknown>[]
let previousAdapter: typeof apiClient.defaults.adapter

async function openEditor(cards: ChannelModelPricing[] | undefined = [complexCard, simpleCard]) {
  listGroups.mockResolvedValue({ items: [{ ...sourceGroup, platform: 'composite', model_pricing: cards }], total: 1, page: 1, page_size: 20, pages: 1 })
  const wrapper = mountView()
  await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
  await flushPromises()
  return wrapper
}

async function save(wrapper: ReturnType<typeof mountView>) {
  // Exercise the real save button and native form submission.
  const button = wrapper.get('button[form="edit-group-form"]')
  ;(button.element as HTMLButtonElement).click()
  await flushPromises()
}

describe('GroupsView pricing HTTP payload', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    payloads = []
    previousAdapter = apiClient.defaults.adapter
    apiClient.defaults.adapter = (async config => {
      expect(config.method).toBe('put')
      expect(config.url).toBe('/admin/groups/42')
      expect(typeof config.data).toBe('string')
      payloads.push(JSON.parse(config.data))
      return { config, status: 200, statusText: 'OK', headers: {}, data: { code: 0, data: sourceGroup } }
    }) satisfies AxiosAdapter
    getModelAllowlistCandidates.mockResolvedValue([])
    getUsageSummary.mockResolvedValue([])
    getCapacitySummary.mockResolvedValue([])
    getLiveCapability.mockResolvedValue({ supported: false })
  })
  afterEach(() => { apiClient.defaults.adapter = previousAdapter; vi.restoreAllMocks(); document.body.innerHTML = '' })

  it('saves a shared TPM limit through the rendered edit form', async () => {
    const wrapper = await openEditor()
    await wrapper.get('#edit-group-form [data-testid="tpm-limit-field"] input').setValue('37')
    await save(wrapper)
    expect(payloads).toHaveLength(1)
    expect(payloads[0].tpm_limit).toBe(37)
    expect(payloads[0].rpm_limit).toBe(sourceGroup.rpm_limit ?? 0)
    wrapper.unmount()
  })

  it('preserves providers, case, zero prices, precision, nested conditions, intervals and multipliers when renaming', async () => {
    const wrapper = await openEditor()
    await wrapper.get('#edit-group-form input[type="text"]').setValue('Renamed')
    await save(wrapper)
    expect(payloads).toHaveLength(1)
    expect(payloads[0].name).toBe('Renamed')
    expect(payloads[0].model_pricing).toEqual([complexCard, simpleCard])
    wrapper.unmount()
  })

  it('applies only an explicitly edited simple price and preserves every hidden multiplier', async () => {
    const wrapper = await openEditor()
    const cards = wrapper.findAll('[data-testid="group-pricing-card"]')
    await cards[1].get('input[type="number"]').setValue('0')
    await save(wrapper)
    expect(payloads[0].model_pricing).toEqual([complexCard, { ...simpleCard, input_price: 0 }])
    wrapper.unmount()
  })

  it('deletes only the selected provider card even when model names match', async () => {
    const wrapper = await openEditor()
    await wrapper.get('[data-testid="remove-pricing-card"]').trigger('click')
    await save(wrapper)
    expect(payloads[0].model_pricing).toEqual([simpleCard])
    wrapper.unmount()
  })

  it('sends an explicit empty array when the final card is deleted', async () => {
    const wrapper = await openEditor([complexCard])
    await wrapper.get('[data-testid="remove-pricing-card"]').trigger('click')
    await save(wrapper)
    expect(payloads[0]).toHaveProperty('model_pricing', [])
    wrapper.unmount()
  })

  it('keeps a missing price list empty rather than inventing a price', async () => {
    const wrapper = await openEditor([])
    await save(wrapper)
    expect(payloads[0]).toHaveProperty('model_pricing', [])
    wrapper.unmount()
  })

  it('keeps omission distinct from explicit removal in the HTTP API', async () => {
    await update(42, { name: 'Renamed' })
    expect(payloads[0]).not.toHaveProperty('model_pricing')
    await update(42, { model_pricing: [] })
    expect(payloads[1]).toHaveProperty('model_pricing', [])
  })

  it('preserves omitted optional fields on an unchanged legacy card', async () => {
    const legacyCard = { ...simpleCard }
    delete legacyCard.cache_write_1h_price
    delete legacyCard.flex_multiplier
    const wrapper = await openEditor([legacyCard])
    await save(wrapper)
    expect(payloads[0].model_pricing).toEqual([legacyCard])
    wrapper.unmount()
  })

  it('requires a concrete provider for a new Composite card and saves the selected provider', async () => {
    const wrapper = await openEditor([])
    const transport = vi.fn(apiClient.defaults.adapter as AxiosAdapter)
    apiClient.defaults.adapter = transport
    await wrapper.findAll('button').find(button => button.text() === 'admin.groups.modelPricing.add')!.trigger('click')
    const card = wrapper.get('[data-testid="group-pricing-card"]')
    await card.get('input[type="text"]').setValue('Shared-Model')
    await card.get('input[type="text"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(transport).not.toHaveBeenCalled()
    await card.get('input[type="number"]').setValue('0')
    await save(wrapper)
    expect(payloads).toHaveLength(0)
    await card.get('select').setValue('deepseek')
    await save(wrapper)
    expect(payloads[0].model_pricing).toEqual([expect.objectContaining({ platform: 'deepseek', models: ['Shared-Model'], input_price: 0 })])
    wrapper.unmount()
  })

  it('blocks an empty model list instead of silently deleting its price card', async () => {
    const wrapper = await openEditor([simpleCard])
    const model = wrapper.get('[data-testid="group-pricing-card"] input[type="text"]')
    await model.trigger('keydown', { key: 'Backspace' })
    await save(wrapper)
    expect(payloads).toHaveLength(0)
    expect(showError).toHaveBeenCalled()
    wrapper.unmount()
  })
})
