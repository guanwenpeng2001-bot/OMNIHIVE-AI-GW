import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ChannelPricingInsights from '@/components/admin/channel/ChannelPricingInsights.vue'
import ChannelsView from '@/views/admin/ChannelsView.vue'
import { createDefaultTimePricingForm, type PricingFormEntry } from '@/components/admin/channel/types'
import type { ChannelPricingPreview } from '@/api/admin/channels'
import en from '@/i18n/locales/en/admin/channels'

const { accountList, catalogRead, preview, channelList, updateChannel, groupList } = vi.hoisted(() => ({
  accountList: vi.fn(), catalogRead: vi.fn(), preview: vi.fn(), channelList: vi.fn(), updateChannel: vi.fn(), groupList: vi.fn()
}))
vi.mock('@/api/admin/accounts', () => ({ list: accountList, getModelCatalog: catalogRead }))
vi.mock('@/api/admin/channels', async () => ({ ...await vi.importActual('@/api/admin/channels'), previewPricing: preview }))
vi.mock('@/api/admin', () => ({ adminAPI: {
  channels: { list: channelList, update: updateChannel }, groups: { getAll: groupList },
  settings: { getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false }) }, accounts: { list: accountList }
} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual('vue-i18n'),
  useI18n: () => ({ t: (key: string) => {
    if (!key.startsWith('admin.channels.insights.')) return key
    return key.replace('admin.channels.', '').split('.').reduce<unknown>((value, part) => (value as Record<string, unknown>)?.[part], en.channels) || key
  } })
}))

function entry(): PricingFormEntry {
  return { models: ['model'], billing_mode: 'token', input_price: '1.20', output_price: '2.00', cache_write_price: null, cache_read_price: null, image_input_price: null, image_output_price: null, per_request_price: null, intervals: [], time_pricing: createDefaultTimePricingForm() }
}
function catalog(profile = 'openai') {
  return { staleness: 'fresh', display_metadata: { model: {
    id: 'model', provider_profile: profile, task_type: 'text_generation', metadata_status: 'incomplete', context_window: 128000,
    context_limit_kind: 'input', input_modalities: [], reasoning: false, supports_function_calling: true,
    reasoning_control: { kind: 'none' }, field_evidence: { context_window: { source: 'reviewed', confidence: 'verified', source_version: 'manifest-hash', observed_at: '2026-09-19T00:00:00Z' } }
  } } }
}
function comparison(): ChannelPricingPreview {
  const difference = { id: 'stable-difference', basis: 'channel_to_reference' as const, model: 'model', field: 'input_cost_per_token', status: 'comparable' as const, absolute_percent: 20, warning: true,
    baseline: { value: 0.000001, currency: 'USD', unit: 'token', scope: 'openai/model', conditions: '{"mode":"chat"}' }, candidate: { value: 0.0000012, currency: 'USD', unit: 'token', scope: 'openai/model', conditions: '{"mode":"chat"}' } }
  return { differences: [difference, difference] }
}
const wrappers: Array<ReturnType<typeof mount>> = []
async function openPanel() {
  const wrapper = mount(ChannelPricingInsights, { props: { entry: entry(), platform: 'openai', groupIds: [7] } })
  wrappers.push(wrapper)
  ;(wrapper.find('details').element as HTMLDetailsElement).open = true
  await new Promise(resolve => setTimeout(resolve, 0))
  await flushPromises()
  return wrapper
}
beforeEach(() => {
  vi.clearAllMocks()
  accountList.mockResolvedValue({ items: [{ id: 11, name: 'Account eleven' }, { id: 12, name: 'Account twelve' }], total: 2 })
  catalogRead.mockResolvedValue(catalog())
  preview.mockResolvedValue(comparison())
  updateChannel.mockResolvedValue({})
  groupList.mockResolvedValue([{ id: 7, name: 'Group seven', platform: 'openai' }])
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()) })

describe('ChannelsView capability pricing contract', () => {
  it('loads only on demand and preserves unknown, explicit false, empty lists and field evidence', async () => {
    const wrapper = await openPanel()
    expect(catalogRead).not.toHaveBeenCalled()
    expect(accountList).toHaveBeenCalledWith(1, 100, { group: '7', platform: 'openai', lite: 'true' })
    await wrapper.get('[data-testid="insight-account"]').setValue('11')
    await flushPromises()
    const table = wrapper.get('[data-testid="capability-pricing-table"]').text()
    expect(table).toContain('128000 tokens (input)')
    expect(table).toContain('Known empty')
    expect(table).toContain('Not supported')
    expect(table).toContain('capabilityCatalog.unknown')
    expect(table).toContain('manifest-hash')
    expect(table).toContain('2026-09-19T00:00:00Z')
    expect(table).not.toContain('Embedding dimensions')
  })

  it('displays embedding dimensions and media limits by task without inventing text limits', async () => {
    const embedding = catalog()
    Object.assign(embedding.display_metadata.model, { task_type: 'embedding', embedding_dimensions: 1536 })
    catalogRead.mockResolvedValue(embedding)
    const wrapper = await openPanel()
    await wrapper.get('[data-testid="insight-account"]').setValue('11'); await flushPromises()
    expect(wrapper.text()).toContain('1536')
    const media = catalog()
    Object.assign(media.display_metadata.model, { task_type: 'video_generation', task_limits: { max_duration_seconds: 8 } })
    catalogRead.mockResolvedValue(media)
    await wrapper.get('[data-testid="insight-account"]').setValue('12'); await flushPromises()
    expect(wrapper.text()).toContain('max_duration_seconds')
    expect(wrapper.text()).not.toContain('Embedding dimensions')
  })

  it('C02 labels the channel denominator and reference comparison without mutating the draft', async () => {
    const wrapper = await openPanel()
    await wrapper.get('[data-testid="insight-account"]').setValue('11'); await flushPromises()
    const original = JSON.stringify(wrapper.props('entry'))
    await wrapper.get('[data-testid="compare-pricing"]').trigger('click'); await flushPromises()
    expect(preview).toHaveBeenCalledWith(expect.objectContaining({ profile: 'openai', pricing: expect.objectContaining({ input_price: 0.0000012, output_price: 0.000002 }) }))
    expect(wrapper.findAll('[role="alert"]')).toHaveLength(1)
    expect(wrapper.text()).toContain('Channel draft → reference')
    expect(wrapper.text()).toContain('Channel draft / denominator: 0.000001 USD/token')
    expect(wrapper.text()).toContain('Reference price: 0.0000012 USD/token')
    expect(wrapper.text()).toContain('0.000001 USD/token')
    expect(wrapper.text()).toContain('20.00%')
    expect(wrapper.text()).toContain('openai/model')
    expect(JSON.stringify(wrapper.props('entry'))).toBe(original)
    expect(wrapper.emitted('update')).toBeUndefined()
  })

  it('discards late account and quote results on identity, draft and close changes', async () => {
    let resolveOld!: (value: ReturnType<typeof catalog>) => void
    catalogRead.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve }))
    const wrapper = await openPanel()
    await wrapper.get('[data-testid="insight-account"]').setValue('11')
    catalogRead.mockResolvedValue(catalog('openai-codex'))
    await wrapper.get('[data-testid="insight-account"]').setValue('12'); await flushPromises()
    resolveOld(catalog('openai')); await flushPromises()
    expect(wrapper.text()).toContain('openai-codex')
    let resolvePreview!: (value: ChannelPricingPreview) => void
    preview.mockReturnValue(new Promise(resolve => { resolvePreview = resolve }))
    await wrapper.get('[data-testid="compare-pricing"]').trigger('click')
    await wrapper.setProps({ entry: { ...entry(), input_price: '5.00' } })
    resolvePreview(comparison()); await flushPromises()
    expect(wrapper.find('[data-testid="pricing-preview-result"]').exists()).toBe(false)
    ;(wrapper.find('details').element as HTMLDetailsElement).open = false
    await new Promise(resolve => setTimeout(resolve, 0))
    await flushPromises()
    expect(wrapper.find('[data-testid="pricing-insights"]').exists()).toBe(false)
  })

  it('preserves original channel save amounts and excludes capabilities and comparison DTOs', async () => {
    const card = { platform: 'openai', models: ['model'], billing_mode: 'token', input_price: 1.2e-6, output_price: 2e-6, cache_write_price: null, cache_write_1h_price: null, cache_read_price: null, image_input_price: null, image_output_price: null, per_request_price: null, intervals: [], time_pricing: null, fast_multiplier: null, flex_multiplier: null, max_reasoning_effort_multiplier: null }
    channelList.mockResolvedValue({ items: [{ id: 8, name: 'Channel eight', status: 'active', group_ids: [7], model_pricing: [card], model_mapping: {}, account_stats_pricing_rules: [] }], total: 1 })
    const wrapper = mount(ChannelsView, { global: { stubs: {
      AppLayout: { template: '<main><slot /></main>' },
      TablePageLayout: { template: '<section><slot name="table" /></section>' },
      DataTable: defineComponent({ props: ['data'], template: '<div v-for="row in data" :key="row.id"><slot name="cell-actions" :row="row" /></div>' }),
      BaseDialog: defineComponent({ props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' }),
      ConfirmDialog: true, Select: true, Toggle: true, Icon: true, PlatformIcon: true, ModelPlazaConfirmation: true,
      PricingEntryCard: { template: '<section><slot name="insights" /></section>' }
    } } })
    wrappers.push(wrapper)
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click'); await flushPromises()
    expect(wrapper.findComponent(ChannelPricingInsights).exists()).toBe(true)
    const panel = wrapper.findComponent(ChannelPricingInsights)
    ;(panel.find('details').element as HTMLDetailsElement).open = true
    await new Promise(resolve => setTimeout(resolve, 0)); await flushPromises()
    await panel.get('[data-testid="insight-account"]').setValue('11'); await flushPromises()
    await panel.get('[data-testid="compare-pricing"]').trigger('click'); await flushPromises()
    expect(panel.text()).toContain('20.00%')
    await wrapper.get('#channel-form').trigger('submit'); await flushPromises()
    expect(updateChannel).toHaveBeenCalledTimes(1)
    const payload = updateChannel.mock.calls[0][1]
    expect(payload.model_pricing).toEqual([card])
    expect(JSON.stringify(payload)).not.toMatch(/provider_profile|field_evidence|differences|capabilities|candidate_hash/)
  })

  it('CC-21 preserves native conditional prices and multi-unit variants through save and reopen', async () => {
    const card = { platform: 'openai', models: ['model'], billing_mode: 'token', input_price: 1.2e-6, output_price: 2e-6, cache_write_price: null, cache_write_1h_price: null, cache_read_price: null, image_input_price: null, image_output_price: null, per_request_price: null, intervals: [], time_pricing: null, fast_multiplier: null, flex_multiplier: null, max_reasoning_effort_multiplier: null }
    const facts = {
      native_pricing_source: 'dashscope',
      thinking_pricing: { input_price: 0.001, output_price: 0.002, cache_read_price: null, explicit_cache_pricing: { input_price: 0.001, output_price: 0.002, cache_read_price: 0, cache_write_price: 0.004 } },
      explicit_cache_pricing: { input_price: 0.001, output_price: 0.002, cache_read_price: 0, cache_write_price: 0.004 },
      batch_chat_pricing: { input_price: 0.0001, output_price: 0.0002, cache_read_price: null },
      price_variants: [{ id: 'model', canonical: 'canonical', aliases: ['alias'], expires_at: '2030-01-01T00:00:00Z', prices: [{ unit: 'character', dimension: 'output', currency: 'USD', sales: 0, cost: 0.01 }, { unit: 'second', dimension: 'output', currency: 'USD', sales: 0.2, cost: 0.1 }] }],
    }
    Object.assign(card, facts)
    channelList.mockResolvedValue({ items: [{ id: 8, name: 'Channel eight', status: 'active', group_ids: [7], model_pricing: [card], model_mapping: {}, account_stats_pricing_rules: [] }], total: 1 })
    const wrapper = mount(ChannelsView, { global: { stubs: {
      AppLayout: { template: '<main><slot /></main>' },
      TablePageLayout: { template: '<section><slot name="table" /></section>' },
      DataTable: defineComponent({ props: ['data'], template: '<div v-for="row in data" :key="row.id"><slot name="cell-actions" :row="row" /></div>' }),
      BaseDialog: defineComponent({ props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' }),
      ConfirmDialog: true, Select: true, Toggle: true, Icon: true, PlatformIcon: true, ModelPlazaConfirmation: true,
      PricingEntryCard: { template: '<section><slot name="insights" /></section>' }
    } } })
    wrappers.push(wrapper)
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click'); await flushPromises()
    expect(wrapper.findComponent(ChannelPricingInsights).exists()).toBe(true)
    const panel = wrapper.findComponent(ChannelPricingInsights)
    ;(panel.find('details').element as HTMLDetailsElement).open = true
    await new Promise(resolve => setTimeout(resolve, 0)); await flushPromises()
    await panel.get('[data-testid="insight-account"]').setValue('11'); await flushPromises()
    await panel.get('[data-testid="compare-pricing"]').trigger('click'); await flushPromises()
    expect(panel.text()).toContain('20.00%')
    await wrapper.get('#channel-form').trigger('submit'); await flushPromises()
    expect(updateChannel).toHaveBeenCalledTimes(1)
    const payload = updateChannel.mock.calls[0][1]
    expect(payload.model_pricing).toEqual([card])
    channelList.mockResolvedValue({ items: [{ ...payload, id: 8, status: 'active' }], total: 1 })
    // Reopen the actual saved API response and save a second time.
    await wrapper.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click'); await flushPromises()
    await wrapper.get('#channel-form').trigger('submit'); await flushPromises()
    expect(updateChannel.mock.calls[1][1].model_pricing).toEqual([card])
    expect(JSON.stringify(payload)).not.toMatch(/provider_profile|field_evidence|differences|capabilities|candidate_hash/)
  })

  it('previews a candidate through the read-only API with an explicit saved group and sample', async () => {
    const wrapper = await openPanel()
    await wrapper.get('[data-testid="insight-account"]').setValue('11'); await flushPromises()
    const reference = { ...comparison().differences[0], id: 'source-change', basis: 'reference_to_reference' as const }
    preview.mockResolvedValue({ differences: [], source: { candidate_hash: 'candidate-digest', before_count: 1, after_count: 2, added: ['added'], removed: [], differences: [reference] },
      impacts: [{ group_id: 7, model: 'model', comparable: true, before: { ActualCost: 0.02 }, after: { ActualCost: 0.04 } }] })
    await wrapper.get('[data-testid="candidate-catalog"]').setValue('{"model":{"input_cost_per_token":0.000002}}')
    await wrapper.get('[data-testid="preview-source"]').trigger('click'); await flushPromises()
    expect(preview).toHaveBeenCalledWith(expect.objectContaining({ candidate: { model: { input_cost_per_token: 0.000002 } }, group_id: 7, input_tokens: 1000, output_tokens: 1000 }))
    expect(wrapper.text()).toContain('Current reference → candidate reference')
    expect(wrapper.text()).toContain('Saved configuration sample charge (USD): 0.02 → 0.04')
    expect(wrapper.text()).toContain('unsaved draft prices are excluded')
    expect(updateChannel).not.toHaveBeenCalled()
    preview.mockClear()
    await wrapper.get('[data-testid="candidate-catalog"]').setValue('{invalid')
    await wrapper.get('[data-testid="preview-source"]').trigger('click'); await flushPromises()
    expect(preview).not.toHaveBeenCalled()
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="pricing-preview-result"]').exists()).toBe(false)
  })

  it('does not manufacture warnings or percentages for missing, incompatible or exact-ten-percent comparisons', async () => {
    const wrapper = await openPanel()
    preview.mockResolvedValue({ differences: [
      { ...comparison().differences[0], id: 'boundary', warning: false, absolute_percent: 10 },
      { ...comparison().differences[0], id: 'unknown', status: 'missing', warning: false, absolute_percent: null, baseline: { value: null, currency: '', unit: '', scope: '', conditions: '' } },
      { ...comparison().differences[0], id: 'media', status: 'incomparable', warning: false, absolute_percent: null, baseline: { value: 1, currency: 'CNY', unit: 'second', scope: 'region/plan', conditions: 'video' } }
    ] })
    await wrapper.get('[data-testid="compare-pricing"]').trigger('click'); await flushPromises()
    expect(wrapper.findAll('[role="alert"]')).toHaveLength(0)
    expect(wrapper.text()).toContain('10.00%')
    expect(wrapper.text()).toContain('Price missing / inherited')
    expect(wrapper.text()).toContain('Not comparable')
    expect(wrapper.text()).toContain('CNY/second')
    expect(wrapper.text()).not.toContain('20.00%')
  })

  it('recovers from read failure and reloads association changes without stale account evidence', async () => {
    accountList.mockRejectedValueOnce(new Error('read failed'))
    const wrapper = await openPanel()
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    await wrapper.findAll('button').find(button => button.text() === 'Retry')!.trigger('click'); await flushPromises()
    await wrapper.get('[data-testid="insight-account"]').setValue('11'); await flushPromises()
    expect(wrapper.text()).toContain('128000')
    accountList.mockResolvedValue({ items: [], total: 0 })
    await wrapper.setProps({ groupIds: [9], platform: 'anthropic' }); await flushPromises()
    expect(wrapper.text()).not.toContain('128000')
    expect(wrapper.findAll('option').some(option => option.text().includes('Account eleven'))).toBe(false)
    expect(accountList).toHaveBeenLastCalledWith(1, 100, { group: '9', platform: 'anthropic', lite: 'true' })
  })
})
