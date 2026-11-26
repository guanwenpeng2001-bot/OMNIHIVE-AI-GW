import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'
import AccountModelConfigurationSection from '../AccountModelConfigurationSection.vue'
import { splitModelMappingObject } from '@/composables/useModelWhitelist'
const { update, read, catalog, preview, models } = vi.hoisted(() => ({ update: vi.fn(), read: vi.fn(), catalog: vi.fn(), preview: vi.fn(), models: vi.fn().mockResolvedValue([]) }))
vi.mock('@/api/admin/accounts', () => ({ accountsAPI: { updateModelAllowlist: update, getById: read, getModelCatalog: catalog,
  getAvailableModels: models, syncUpstreamModels: preview } }))
vi.mock('@/api/admin/platformModelPresets', () => ({ getPlatformModelPresets: vi.fn().mockResolvedValue([{ platform: 'openai', models: [{ id: 'Platform' }] }]) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showInfo: vi.fn(), showError: vi.fn() }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values?: Record<string, unknown>) => key + (values ? JSON.stringify(values) : '') }) }))
const saved = (mapping: Record<string, string>, enabled = false, models: string[] = []) => ({ id: 12, credentials: { model_mapping: mapping }, extra: { upstream_model_allowlist: { enabled, models } } })
const mountSelector = (props = {}) => mount(ModelWhitelistSelector, { props: { accountId: 12, platform: 'openai', modelValue: ['Exact.Case/ID'], ...props }, global: { stubs: { ModelIcon: true, Teleport: true } } })
beforeEach(() => {
  vi.clearAllMocks()
  catalog.mockReset().mockResolvedValue({ synced: false, warnings: [], state: { rules_revision: '9007199254740993' } })
  update.mockReset().mockResolvedValue({ rules_revision: '9007199254740994', not_discovered: [] })
  read.mockReset().mockResolvedValue(saved({ 'Exact.Case/ID': 'Exact.Case/ID' }))
  preview.mockReset()
})
describe('atomic account model selection', () => {
  it('shows 41 alias request keys and targets and roundtrips without selecting candidates', async () => {
    const mapping = Object.fromEntries(Array.from({ length: 41 }, (_, index) => [`Request/${index}`, `Target/${index}`]))
    read.mockResolvedValue(saved(mapping))
    catalog.mockResolvedValue({ synced: true, warnings: [], state: { rules_revision: '3' }, snapshot: { discovered_models: ['NativeCandidate'] } })
    const selection = splitModelMappingObject(mapping)
    const wrapper = mountSelector({ modelValue: selection.allowedModels, modelMappings: selection.modelMappings })
    await flushPromises()
    const chips = wrapper.get('[data-testid="selected-models"]')
    expect(chips.findAll('button[aria-label^="common.remove"]')).toHaveLength(41)
    for (const [request, target] of Object.entries(mapping)) {
      expect(chips.text()).toContain(request)
      expect(chips.text()).toContain(target)
    }
    expect(wrapper.get('[data-testid="configured-model-summary"]').text()).toContain('"direct":0,"mappings":41,"wildcards":0')
    await wrapper.get('button[aria-expanded]').trigger('click')
    const options = wrapper.findAll('[data-testid="select-model"]')
    expect(options.filter(option => option.attributes('aria-pressed') === 'true')).toHaveLength(41)
    for (const candidate of ['Platform', 'NativeCandidate']) {
      expect(options.find(option => option.text().includes(candidate))!.attributes('aria-pressed')).toBe('false')
    }
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.get('[data-testid="save-model-selection"]').trigger('click')
    await flushPromises()
    expect(update).toHaveBeenCalledWith(12, expect.objectContaining({ model_mapping: mapping }))
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[]])
    expect(wrapper.emitted('update:modelMappings')?.at(-1)).toEqual([selection.modelMappings])
    wrapper.unmount()
  })

  it('still displays and saves 88 ordinary identity pairs without mapping rows', async () => {
    const identities = Array.from({ length: 88 }, (_, index) => `Command/${index}`)
    const mapping = Object.fromEntries(identities.map(model => [model, model]))
    read.mockResolvedValue(saved(mapping))
    const wrapper = mountSelector({ platform: 'commandcode', modelValue: identities, modelMappings: [] })
    await flushPromises()
    expect(wrapper.get('[data-testid="selected-models"]').findAll('button')).toHaveLength(88)
    expect(wrapper.get('[data-testid="configured-model-summary"]').text()).toContain('"direct":88,"mappings":0,"wildcards":0')
    await wrapper.get('[data-testid="save-model-selection"]').trigger('click')
    await flushPromises()
    expect(update).toHaveBeenCalledWith(12, expect.objectContaining({ mode: 'restricted', models: identities, model_mapping: {} }))
    wrapper.unmount()
  })

  it('removes only the alias request key and adds a custom identity beside a wildcard', async () => {
    const Host = defineComponent({ components: { ModelWhitelistSelector },
      setup: () => ({ models: ref(['Exact']), mappings: ref([{ from: 'Alias', to: 'Target' }, { from: 'family-*', to: 'FamilyTarget' }]) }),
      template: '<ModelWhitelistSelector v-model="models" v-model:model-mappings="mappings" :account-id="12" platform="openai" />' })
    const host = mount(Host, { global: { stubs: { ModelIcon: true, Teleport: true } } })
    await flushPromises()
    expect(host.get('[data-testid="selected-models"]').findAll('button')).toHaveLength(3)
    await host.get('button[aria-label="common.remove Alias"]').trigger('click')
    expect(host.vm.models).toEqual(['Exact'])
    expect(host.vm.mappings).toEqual([{ from: 'family-*', to: 'FamilyTarget' }])
    await host.get('input[aria-label="admin.accounts.customModelName"]').setValue('New/Exact')
    await host.get('input[aria-label="admin.accounts.customModelName"]').trigger('keydown', { key: 'Enter' })
    await host.get('[data-testid="save-model-selection"]').trigger('click')
    await flushPromises()
    expect(update).toHaveBeenCalledWith(12, expect.objectContaining({ mode: 'restricted', models: ['Exact', 'New/Exact'], model_mapping: { 'family-*': 'FamilyTarget' } }))
    host.unmount()
  })

  it.each([
    { modelValue: ['Alias'], modelMappings: [{ from: 'Alias', to: 'Target' }] },
    { modelValue: [], modelMappings: [{ from: 'Alias', to: 'Target' }, { from: 'Alias', to: 'OtherTarget' }] }
  ])('blocks conflicting targets for a duplicate request key', async selection => {
    const wrapper = mountSelector(selection)
    await flushPromises()
    await wrapper.get('[data-testid="save-model-selection"]').trigger('click')
    expect(wrapper.get('[role="alert"]').text()).toContain('conflictingTarget')
    expect(update).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it.each(['custom', 'filtered', 'fill', 'preview'])('keeps alias targets when adding through %s', async action => {
    const modelMappings = [{ from: ' Platform ', to: 'PrivateTarget' }, { from: 'family-*', to: 'FamilyTarget' }]
    preview.mockResolvedValue({ candidate_models: ['Platform', 'family-*', 'NewExact'], discovered_models: ['Platform', 'NewExact'], discovery_complete: true })
    const wrapper = mountSelector({ modelValue: ['Exact'], modelMappings })
    await flushPromises()
    if (action === 'custom') {
      await wrapper.get('input[aria-label="admin.accounts.customModelName"]').setValue(' Platform ')
      await wrapper.get('input[aria-label="admin.accounts.customModelName"]').trigger('keydown', { key: 'Enter' })
      expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    } else if (action === 'filtered') {
      await wrapper.get('button[aria-expanded]').trigger('click')
      await wrapper.get('[data-testid="select-filtered-models"]').trigger('click')
      expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['Exact']])
    } else if (action === 'fill') {
      await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.fillRelatedModels')!.trigger('click')
      expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['Exact']])
    } else {
      await wrapper.get('[data-testid="preview-upstream-models"]').trigger('click')
      await flushPromises()
      expect(wrapper.get('[data-testid="add-preview-models"]').text()).toContain('"count":1')
      await wrapper.get('[data-testid="add-preview-models"]').trigger('click')
      expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['Exact', 'NewExact']])
    }
    expect(wrapper.emitted('update:modelMappings')).toBeUndefined()
    expect(wrapper.get('[data-testid="selected-models"]').text()).toContain('PrivateTarget')
    expect(wrapper.get('[data-testid="selected-models"]').text()).toContain('family-*')
    wrapper.unmount()
  })

  it('toggles aliases and wildcard request keys off without converting or expanding them', async () => {
    const wrapper = mountSelector({ modelValue: ['Exact'], modelMappings: [{ from: 'Alias', to: 'Target' }, { from: 'family-*', to: 'FamilyTarget' }] })
    await flushPromises()
    await wrapper.get('button[aria-expanded]').trigger('click')
    for (const request of ['Alias', 'family-*']) {
      const option = wrapper.findAll('[data-testid="select-model"]').find(button => button.text().includes(request))!
      expect(option.attributes('aria-pressed')).toBe('true')
      await option.trigger('click')
      expect(wrapper.emitted('update:modelMappings')?.at(-1)).toEqual([wrapper.props('modelMappings')!.filter(mapping => mapping.from !== request)])
    }
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    wrapper.unmount()
  })

  it('confirms the final alias chip removal and preserves it on cancel', async () => {
    const wrapper = mountSelector({ modelValue: [], modelMappings: [{ from: 'Alias', to: 'Target' }] })
    await flushPromises()
    await wrapper.get('button[aria-label="common.remove Alias"]').trigger('click')
    expect(wrapper.findAll('[data-testid="confirm-deny-all-dialog"]')).toHaveLength(1)
    await wrapper.get('[data-testid="confirm-deny-all-cancel"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('update:modelMappings')).toBeUndefined()
    expect(update).not.toHaveBeenCalled()
    read.mockResolvedValue(saved({}, true))
    await wrapper.get('button[aria-label="common.remove Alias"]').trigger('click')
    await wrapper.get('[data-testid="confirm-deny-all-submit"]').trigger('click')
    await flushPromises()
    expect(update.mock.calls).toEqual([[12, { mode: 'deny_all', enabled: true, models: [], reason: 'admin.accounts.modelSync.defaultDenyAllReason', confirm_deny_all: true, expected_rules_revision: '9007199254740993' }]])
    expect(update.mock.calls[0]?.[1]).not.toHaveProperty('model_mapping')
    wrapper.unmount()
  })

  it('updates alias targets on the same chip and deduplicates identity rows', async () => {
    const wrapper = mountSelector({ modelValue: ['Exact'], modelMappings: [{ from: 'Exact', to: 'Exact' }, { from: 'Alias', to: 'OldTarget' }] })
    await flushPromises()
    expect(wrapper.get('[data-testid="selected-models"]').findAll('button')).toHaveLength(2)
    await wrapper.setProps({ modelMappings: [{ from: 'Exact', to: 'Exact' }, { from: 'Alias', to: 'NewTarget' }] })
    expect(wrapper.get('[data-testid="selected-models"]').text()).toContain('NewTarget')
    expect(wrapper.get('[data-testid="selected-models"]').text()).not.toContain('OldTarget')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    wrapper.unmount()
  })

  it('distinguishes configured aliases and wildcard rules from direct IDs and discovery counts', async () => {
    catalog.mockResolvedValue({ synced: true, warnings: [], state: { rules_revision: '3' }, discovered_count: 33, eligible_count: 46 })
    const modelMappings = Array.from({ length: 62 }, (_, index) => ({ from: index === 0 ? 'family-*' : `Alias/${index}`, to: 'Shared/Target' }))
    const wrapper = mountSelector({ modelValue: [], modelMappings, modelAllowlist: { enabled: false, models: [] } })
    await flushPromises()
    expect(wrapper.get('[data-testid="configured-model-summary"]').text()).toContain('"direct":0,"mappings":62,"wildcards":1')
    expect(wrapper.get('[data-testid="catalog-counts"]').text()).toContain('33')
    expect(wrapper.get('[data-testid="catalog-counts"]').text()).toContain('46')
    await wrapper.setProps({ modelMappings: [...modelMappings, { from: 'AnotherAlias', to: 'Shared/Target' }] })
    expect(wrapper.get('[data-testid="catalog-counts"]').text()).toContain('46')
    expect(wrapper.text()).not.toContain('modelCatalog.stale')
    expect(wrapper.text()).not.toContain('modelSync.emptySelection')
    expect(wrapper.emitted('update:modelMappings')).toBeUndefined()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(update).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('saves an undiscovered exact ID and alias together, without a reason or discovery gate', async () => {
    const mapping = { 'Exact.Case/ID': 'Exact.Case/ID', Alias: 'Private/Target' }
    read.mockResolvedValue(saved(mapping))
    update.mockResolvedValue({ rules_revision: '9007199254740994', not_discovered: ['Private/Target'] })
    const wrapper = mountSelector({ modelMappings: [{ from: 'Alias', to: 'Private/Target' }] })
    await flushPromises()
    await wrapper.get('[data-testid="save-model-selection"]').trigger('click')
    await flushPromises()
    expect(update.mock.calls).toEqual([[12, { mode: 'restricted', enabled: true, models: ['Exact.Case/ID'], reason: '', expected_rules_revision: '9007199254740993', model_mapping: { Alias: 'Private/Target' } }]])
    expect(wrapper.get('[data-testid="selection-warning"]').text()).toContain('Private/Target')
    expect(wrapper.emitted('selection-saved')).toHaveLength(1)
    wrapper.unmount()
  })
  it('reports unsaved, saving, saved and failed model writes without losing a newer draft', async () => {
    const wrapper = mountSelector()
    await flushPromises()
    expect(wrapper.get('[data-testid="model-save-status"]').text()).toContain('unchangedSelection')
    await wrapper.setProps({ modelValue: ['Changed'] })
    expect(wrapper.get('[data-testid="model-save-status"]').text()).toContain('unsavedSelection')
    let finish!: (result: unknown) => void
    update.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    read.mockResolvedValue(saved({ Changed: 'Changed' }))
    await wrapper.get('[data-testid="save-model-selection"]').trigger('click')
    expect(wrapper.get('[data-testid="model-save-status"]').text()).toContain('savingSelection')
    finish({ rules_revision: '4' })
    await flushPromises()
    expect(wrapper.get('[data-testid="model-save-status"]').text()).toContain('savedSelection')
    await wrapper.setProps({ modelValue: ['NewerDraft'] })
    update.mockRejectedValueOnce({ status: 409 })
    await wrapper.get('[data-testid="save-model-selection"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="model-save-status"]').text()).toContain('selectionSaveFailed')
    expect(wrapper.props('modelValue')).toEqual(['NewerDraft'])
    wrapper.unmount()
  })
  it('replaces a saved scope when an exact model is removed', async () => {
    catalog.mockResolvedValue({
      mode: 'restricted',
      models: ['Keep', 'Remove'],
      state: { rules_revision: '9007199254740993' }
    })
    read.mockResolvedValue(saved({}, true, ['Keep']))
    const wrapper = mountSelector({
      modelValue: ['Keep', 'Remove'],
      modelAllowlist: { enabled: true, models: ['Keep', 'Remove'] }
    })
    await flushPromises()
    await wrapper.get('button[aria-label="common.remove Remove"]').trigger('click')
    await wrapper.setProps({ modelValue: wrapper.emitted('update:modelValue')?.at(-1)?.[0] as string[] })
    await wrapper.get('[data-testid="save-model-selection"]').trigger('click')
    await flushPromises()
    expect(update).toHaveBeenCalledWith(12, expect.objectContaining({ mode: 'restricted', models: ['Keep'] }))
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['Keep']])
    wrapper.unmount()
  })
  it('keeps the current selection when the deny-all write conflicts', async () => {
    update.mockRejectedValueOnce({ status: 409 })
    const wrapper = mountSelector({ mode: 'restricted', modelMappings: [{ from: 'Alias', to: 'Target' }] })
    await flushPromises()
    await wrapper.get('[data-testid="clear-model-selection"]').trigger('click')
    await wrapper.get('[data-testid="confirm-deny-all-submit"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="deny-all"]').exists()).toBe(false)
    expect(wrapper.emitted('update:mode')).toBeUndefined()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.emitted('update:modelMappings')).toBeUndefined()
    wrapper.unmount()
  })
  it('confirms clearing, omits model_mapping on deny-all write, and echoes preserved aliases', async () => {
    read.mockResolvedValue(saved({ Alias: 'Target' }, true))
    const Host = defineComponent({ components: { AccountModelConfigurationSection }, setup: () => ({ models: ref(['Exact']), mappings: ref([{ from: 'Alias', to: 'Target' }]) }),
      template: '<AccountModelConfigurationSection v-model="models" v-model:mappings="mappings" :account-id="12" platform="openai" />' })
    const host = mount(Host, { global: { stubs: { ModelIcon: true, Teleport: true } } })
    await flushPromises()
    await host.get('[data-testid="clear-model-selection"]').trigger('click')
    expect(update).not.toHaveBeenCalled()
    expect(host.findAll('[data-testid="confirm-deny-all-dialog"]')).toHaveLength(1)
    await host.get('[data-testid="confirm-deny-all-submit"]').trigger('click')
    await flushPromises()
    expect(update.mock.calls).toEqual([[12, { mode: 'deny_all', enabled: true, models: [], reason: 'admin.accounts.modelSync.defaultDenyAllReason', confirm_deny_all: true, expected_rules_revision: '9007199254740993' }]])
    expect(update.mock.calls[0]?.[1]).not.toHaveProperty('model_mapping')
    expect(host.vm.models).toEqual([])
    expect(host.vm.mappings).toEqual([{ from: 'Alias', to: 'Target' }])
    expect(host.find('[data-testid="deny-all"]').exists()).toBe(true)
    host.unmount()
  })
  it('confirms removal of the last exact model and does not clear on cancel', async () => {
    const wrapper = mountSelector()
    await flushPromises()
    await wrapper.get('button[aria-label="common.remove Exact.Case/ID"]').trigger('click')
    expect(wrapper.findAll('[data-testid="confirm-deny-all-dialog"]')).toHaveLength(1)
    await wrapper.get('[data-testid="confirm-deny-all-cancel"]').trigger('click')
    expect(update).not.toHaveBeenCalled()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    wrapper.unmount()
  })
  it('reselects from deny-all through the atomic mapping endpoint', async () => {
    const wrapper = mountSelector({ modelAllowlist: { enabled: true, models: [] } })
    await flushPromises()
    expect(wrapper.get('[data-testid="save-model-selection"]').text()).toContain('reselect')
    await wrapper.get('[data-testid="save-model-selection"]').trigger('click')
    await flushPromises()
    expect(update).toHaveBeenCalledWith(12, expect.objectContaining({ mode: 'restricted', enabled: true, models: ['Exact.Case/ID'], model_mapping: {} }))
    expect(wrapper.find('[data-testid="deny-all"]').exists()).toBe(false)
    wrapper.unmount()
  })
  it('preserves nonempty filters on selection save', async () => {
    const wrapper = mountSelector({ modelAllowlist: { enabled: true, models: ['Exact.Case/ID'] } })
    await flushPromises()
    await wrapper.get('[data-testid="save-model-selection"]').trigger('click')
    expect(update).toHaveBeenCalledWith(12, expect.objectContaining({ enabled: true, models: ['Exact.Case/ID'] }))
    await flushPromises()
    wrapper.unmount()
  })
  it('switches across inherit, restricted and deny_all modes in a single unified editor without catalog-allowlist drawer', async () => {
    const Host = defineComponent({
      components: { AccountModelConfigurationSection },
      setup: () => ({
        models: ref(['Exact.Case/ID']),
        mappings: ref([{ from: 'Alias', to: 'Exact.Case/ID' }]),
        mode: ref<'inherit' | 'restricted' | 'deny_all'>('restricted'),
        reason: ref('')
      }),
      template:
        '<AccountModelConfigurationSection v-model="models" v-model:mappings="mappings" v-model:mode="mode" v-model:reason="reason" :account-id="12" platform="openai" />'
    })
    const host = mount(Host, { global: { stubs: { ModelIcon: true } } })
    await flushPromises()
    expect(host.find('[data-testid="catalog-allowlist"]').exists()).toBe(false)
    expect(host.get('[data-testid="mode-restricted"]').attributes('aria-pressed')).toBe('true')

    // Switch to inherit mode -> clears draft and stashes previous restricted selection
    await host.get('[data-testid="mode-inherit"]').trigger('click')
    expect(host.vm.mode).toBe('inherit')
    expect(host.vm.models).toEqual([])
    expect(host.vm.mappings).toEqual([])
    expect(host.find('[data-testid="mode-inherit-hint"]').exists()).toBe(true)

    // Switch back to restricted before saving -> restores stashed selection
    await host.get('[data-testid="mode-restricted"]').trigger('click')
    expect(host.vm.mode).toBe('restricted')
    expect(host.vm.models).toEqual(['Exact.Case/ID'])
    expect(host.vm.mappings).toEqual([{ from: 'Alias', to: 'Exact.Case/ID' }])

    // Switch to inherit and save
    read.mockResolvedValueOnce(saved({}, false, []))
    update.mockResolvedValueOnce({ mode: 'inherit', models: [], model_mapping: {}, rules_revision: '9007199254740994' })
    await host.get('[data-testid="mode-inherit"]').trigger('click')
    await host.get('[data-testid="save-model-selection"]').trigger('click')
    await flushPromises()
    expect(update).toHaveBeenLastCalledWith(12, {
      mode: 'inherit',
      enabled: false,
      models: [],
      reason: '',
      expected_rules_revision: '9007199254740993',
      model_mapping: {}
    })
    expect(host.vm.mode).toBe('inherit')
    host.unmount()
  })
  it('requires a non-empty reason in the single deny-all modal', async () => {
    read.mockResolvedValue(saved({ Alias: 'Exact.Case/ID' }, true, []))
    update.mockResolvedValue({
      mode: 'deny_all',
      models: [],
      model_mapping: { Alias: 'Exact.Case/ID' },
      rules_revision: '9007199254740994',
      scope: { enabled: true, models: [], reason: '上游维护停用' }
    })
    const wrapper = mountSelector({ modelMappings: [{ from: 'Alias', to: 'Exact.Case/ID' }] })
    await flushPromises()

    await wrapper.get('[data-testid="mode-deny-all"]').trigger('click')
    expect(wrapper.findAll('[data-testid="confirm-deny-all-dialog"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-testid="confirm-deny-all-submit"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-testid="deny-all-reason-input"]')).toHaveLength(1)

    // Empty reason disables confirm button and displays validation hint inside the same modal
    expect(wrapper.get('[data-testid="confirm-deny-all-submit"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="deny-all-reason-error"]').text()).toContain('denyAllReasonRequired')

    await wrapper.get('[data-testid="deny-all-reason-input"]').setValue('   ')
    expect(wrapper.get('[data-testid="confirm-deny-all-submit"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="deny-all-reason-error"]').text()).toContain('denyAllReasonRequired')
    await wrapper.get('[data-testid="confirm-deny-all-submit"]').trigger('click')
    await flushPromises()
    expect(update).not.toHaveBeenCalled()

    // Providing a valid reason enables confirm and sends atomic deny_all payload without model_mapping
    await wrapper.get('[data-testid="deny-all-reason-input"]').setValue(' 上游维护停用 ')
    expect(wrapper.get('[data-testid="confirm-deny-all-submit"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('[data-testid="deny-all-reason-error"]').exists()).toBe(false)
    await wrapper.get('[data-testid="confirm-deny-all-submit"]').trigger('click')
    await flushPromises()
    expect(update).toHaveBeenCalledWith(12, {
      mode: 'deny_all',
      enabled: true,
      models: [],
      reason: '上游维护停用',
      confirm_deny_all: true,
      expected_rules_revision: '9007199254740993'
    })
    expect(update.mock.calls[0]?.[1]).not.toHaveProperty('model_mapping')
    expect(wrapper.find('[data-testid="deny-all"]').exists()).toBe(true)
    expect(wrapper.emitted('update:mode')?.at(-1)).toEqual(['deny_all'])
    expect(wrapper.emitted('update:reason')?.at(-1)).toEqual(['上游维护停用'])
    expect(wrapper.emitted('update:modelMappings')?.at(-1)).toEqual([[{ from: 'Alias', to: 'Exact.Case/ID' }]])
    wrapper.unmount()
  })
  it('displays auto_added and not_discovered notices and backfills auto_added targets into modelValue', async () => {
    read.mockResolvedValue(
      saved(
        { 'Exact.Case/ID': 'Exact.Case/ID', 'Public/Alias': 'Auto/Target' },
        true,
        ['Exact.Case/ID', 'Auto/Target']
      )
    )
    update.mockResolvedValue({
      mode: 'restricted',
      models: ['Exact.Case/ID', 'Auto/Target'],
      model_mapping: { 'Exact.Case/ID': 'Exact.Case/ID', 'Public/Alias': 'Auto/Target' },
      auto_added: ['Auto/Target'],
      not_discovered: ['Auto/Target'],
      rules_revision: '9007199254740994'
    })
    const wrapper = mountSelector({
      modelValue: ['Exact.Case/ID'],
      modelMappings: [{ from: 'Public/Alias', to: 'Auto/Target' }]
    })
    await flushPromises()
    await wrapper.get('[data-testid="save-model-selection"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="auto-added-notice"]').text()).toContain('Auto/Target')
    expect(wrapper.get('[data-testid="selection-warning"]').text()).toContain('Auto/Target')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['Exact.Case/ID', 'Auto/Target']])
    wrapper.unmount()
  })
  it('never turns a scope-only save without mappings into deny_all (OpenRouter/OpenCode free account regression)', async () => {
    // Server returns empty model_mapping {} and enabled scope with ['openrouter/free']
    read.mockResolvedValue(saved({}, true, ['openrouter/free']))
    update.mockResolvedValue({
      mode: 'restricted',
      models: ['openrouter/free'],
      model_mapping: {},
      auto_added: [],
      not_discovered: [],
      rules_revision: '9007199254740994',
      scope: { enabled: true, models: ['openrouter/free'] }
    })
    const Host = defineComponent({
      components: { AccountModelConfigurationSection },
      setup: () => ({
        models: ref(['openrouter/free']),
        mappings: ref([]),
        mode: ref<'inherit' | 'restricted' | 'deny_all'>('restricted')
      }),
      template:
        '<AccountModelConfigurationSection v-model="models" v-model:mappings="mappings" v-model:mode="mode" :account-id="12" platform="openai" />'
    })
    const host = mount(Host, { global: { stubs: { ModelIcon: true } } })
    await flushPromises()
    await host.get('[data-testid="save-model-selection"]').trigger('click')
    await flushPromises()

    expect(update).toHaveBeenCalledWith(
      12,
      expect.objectContaining({
        mode: 'restricted',
        enabled: true,
        models: ['openrouter/free'],
        expected_rules_revision: '9007199254740993'
      })
    )
    expect(host.vm.mode).toBe('restricted')
    expect(host.vm.models).toEqual(['openrouter/free'])
    expect(host.find('[data-testid="deny-all"]').exists()).toBe(false)
    host.unmount()
  })
  it.each([400, 409])('shows HTTP %s and requires an explicit retry with the refreshed revision', async status => {
    update.mockRejectedValueOnce({ status })
    const wrapper = mountSelector()
    await flushPromises()
    catalog.mockResolvedValue({ synced: false, warnings: [], state: { rules_revision: '9007199254740998' } })
    await wrapper.get('[data-testid="save-model-selection"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain(status === 400 ? 'invalidSelection' : 'conflict')
    expect(update).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.get('[data-testid="save-model-selection"]').trigger('click')
    await flushPromises()
    expect(update).toHaveBeenLastCalledWith(12, expect.objectContaining({ expected_rules_revision: '9007199254740998' }))
    wrapper.unmount()
  })
  it('keeps selected IDs across changing discovery and distinguishes candidate sources', async () => {
    preview.mockResolvedValueOnce({ candidate_models: ['Returned'], discovered_models: ['Returned'], discovery_complete: true })
      .mockResolvedValueOnce({ candidate_models: ['New'], discovered_models: ['New'], discovery_complete: true })
    const wrapper = mountSelector()
    await flushPromises()
    await wrapper.get('[data-testid="preview-upstream-models"]').trigger('click')
    await flushPromises()
    await wrapper.get('button[aria-expanded]').trigger('click')
    expect(wrapper.get('[data-testid="model-options"]').text()).toMatch(/platformCandidates/)
    expect(wrapper.get('[data-testid="model-options"]').text()).toMatch(/nativeDiscovered/)
    expect(wrapper.get('[data-testid="selected-models"]').text()).toContain('manualSelection')
    await wrapper.get('[data-testid="preview-upstream-models"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.get('[data-testid="selected-models"]').text()).toContain('Exact.Case/ID')
    expect(update).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('ignores a mutation response after the account changes', async () => {
    let finish!: (result: unknown) => void
    update.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const wrapper = mountSelector()
    await flushPromises()
    await wrapper.get('[data-testid="save-model-selection"]').trigger('click')
    await wrapper.setProps({ accountId: 13 })
    finish({ rules_revision: '9' })
    await flushPromises()
    expect(read).not.toHaveBeenCalled()
    expect(wrapper.emitted('selection-saved')).toBeUndefined()
    wrapper.unmount()
  })
  it('does not let the previous account save lock or unlock the current account save', async () => {
    let finishPrevious!: (result: unknown) => void
    let finishCurrent!: (result: unknown) => void
    update.mockImplementationOnce(() => new Promise(resolve => { finishPrevious = resolve }))
      .mockImplementationOnce(() => new Promise(resolve => { finishCurrent = resolve }))
    const wrapper = mountSelector()
    await flushPromises()
    await wrapper.get('[data-testid="save-model-selection"]').trigger('click')
    await wrapper.setProps({ accountId: 13 })
    await flushPromises()
    expect(wrapper.get('[data-testid="save-model-selection"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="save-model-selection"]').trigger('click')
    finishPrevious({ rules_revision: '4' })
    await flushPromises()
    expect(wrapper.get('[data-testid="save-model-selection"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="model-save-status"]').text()).toContain('savingSelection')
    finishCurrent({ rules_revision: '5' })
    await flushPromises()
    expect(read).toHaveBeenCalledTimes(1)
    expect(read).toHaveBeenCalledWith(13)
    wrapper.unmount()
  })
})


it('still saves manually selected models when the optional model-list request fails', async () => {
  models.mockRejectedValueOnce(new Error('model list unavailable'))
  catalog.mockResolvedValue({ synced: false, warnings: [], state: { rules_revision: '3' } })
  const wrapper = mountSelector()
  await flushPromises()
  expect(wrapper.get('[data-testid="save-model-selection"]').attributes('disabled')).toBeUndefined()
  await wrapper.get('[data-testid="save-model-selection"]').trigger('click')
  await flushPromises()
  expect(update).toHaveBeenCalledWith(12, expect.objectContaining({ mode: 'restricted', models: ['Exact.Case/ID'], expected_rules_revision: '3', model_mapping: {} }))
  wrapper.unmount()
})

it('includes discovered-only models as candidates without selecting them on load', async () => {
  catalog.mockResolvedValue({ synced: true, warnings: [], state: { rules_revision: '3' }, snapshot: { discovered_models: ['New/Discovered'] } })
  const wrapper = mountSelector()
  await flushPromises()
  await wrapper.get('button[aria-expanded]').trigger('click')
  expect(wrapper.get('[data-testid="model-options"]').text()).toContain('New/Discovered')
  expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  wrapper.unmount()
})

it('confirms deleting the final alias through the same atomic clear action', async () => {
  read.mockResolvedValue(saved({}, true))
  const Host = defineComponent({ components: { AccountModelConfigurationSection }, setup: () => ({ models: ref<string[]>([]), mappings: ref([{ from: 'Alias', to: 'Target' }]) }),
    template: '<AccountModelConfigurationSection v-model="models" v-model:mappings="mappings" :account-id="12" platform="openai" />' })
  const host = mount(Host, { global: { stubs: { ModelIcon: true, Teleport: true } } })
  await flushPromises()
  await host.get('[data-testid="model-mappings"]').findAll('button').find(button => button.text() === 'common.delete')!.trigger('click')
  expect(update).not.toHaveBeenCalled()
  expect(host.findAll('[data-testid="confirm-deny-all-dialog"]')).toHaveLength(1)
  await host.get('[data-testid="confirm-deny-all-submit"]').trigger('click')
  await flushPromises()
  expect(update).toHaveBeenCalledWith(12, expect.objectContaining({ mode: 'deny_all', enabled: true, models: [] }))
  expect(update.mock.calls[0]?.[1]).not.toHaveProperty('model_mapping')
  expect(host.vm.mappings).toEqual([])
  host.unmount()
})


it('does not carry locally refreshed deny-all state to another account without a filter', async () => {
  read.mockResolvedValue(saved({}, true))
  const wrapper = mountSelector()
  await flushPromises()
  await wrapper.get('[data-testid="clear-model-selection"]').trigger('click')
  await wrapper.get('[data-testid="confirm-deny-all-submit"]').trigger('click')
  await flushPromises()
  expect(wrapper.find('[data-testid="deny-all"]').exists()).toBe(true)
  await wrapper.setProps({ accountId: 13 })
  await flushPromises()
  expect(wrapper.find('[data-testid="deny-all"]').exists()).toBe(false)
  wrapper.unmount()
})

it('does not replace a newer selection with a late save response', async () => {
  let finish!: (result: unknown) => void
  update.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  const wrapper = mountSelector()
  await flushPromises()
  await wrapper.get('[data-testid="save-model-selection"]').trigger('click')
  await wrapper.setProps({ modelValue: ['Newer/Selection'] })
  finish({ rules_revision: '4' })
  await flushPromises()
  expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  expect(wrapper.emitted('selection-saved')).toBeUndefined()
  expect(wrapper.get('[data-testid="selected-models"]').text()).toContain('Newer/Selection')
  wrapper.unmount()
})

it('opens the mapping editor and focuses the rejected row on section save', async () => {
  const host = mount(AccountModelConfigurationSection, {
    attachTo: document.body,
    props: { accountId: 12, platform: 'openai', modelValue: [], mappings: [{ from: 'bad*source', to: 'Target' }] },
    global: { stubs: { ModelIcon: true } }
  })
  await flushPromises()
  const mappings = host.get<HTMLDetailsElement>('[data-testid="model-mappings"]')
  expect(mappings.element.open).toBe(false)
  await host.get('[data-testid="save-model-selection"]').trigger('click')
  await flushPromises()
  expect(mappings.element.open).toBe(true)
  expect(document.activeElement).toBe(mappings.get('input').element)
  expect(update).not.toHaveBeenCalled()
  host.unmount()
})

it('syncs dirty status against persistedSelection and registers saveSelection actions without preserve flag', async () => {
  let registered: { saveSelection: () => Promise<boolean>; canSaveSelection: () => boolean } | null = null
  read.mockResolvedValue(saved({ 'Exact.Case/ID': 'Exact.Case/ID', 'Added/ID': 'Added/ID' }))
  const wrapper = mountSelector({
    persistedSelection: { models: ['Exact.Case/ID'], mappings: [] },
    registerActions: (actions: typeof registered) => { registered = actions }
  })
  await flushPromises()
  expect(wrapper.get('[data-testid="model-save-status"]').text()).toBe('admin.accounts.modelSync.unchangedSelection')
  expect(registered?.canSaveSelection()).toBe(true)

  await wrapper.setProps({ modelValue: ['Exact.Case/ID', 'Added/ID'] })
  await flushPromises()
  expect(wrapper.get('[data-testid="model-save-status"]').text()).toBe('admin.accounts.modelSync.unsavedSelection')

  const result = await registered!.saveSelection()
  await flushPromises()
  expect(result).toBe(true)
  expect(update).toHaveBeenCalledWith(12, expect.objectContaining({
    mode: 'restricted',
    models: ['Exact.Case/ID', 'Added/ID'],
    expected_rules_revision: '9007199254740993',
    model_mapping: {}
  }))
  expect(update.mock.calls[0]?.[1]).not.toHaveProperty('preserve_model_selection')

  await wrapper.setProps({
    modelValue: ['Concurrent/ID'],
    persistedSelection: { models: ['Concurrent/ID'], mappings: [] }
  })
  await flushPromises()
  expect(wrapper.get('[data-testid="model-save-status"]').text()).toBe('admin.accounts.modelSync.savedSelection')

  wrapper.unmount()
  expect(registered).toBeNull()
})

it('returns false from registered saveSelection on CAS conflict while keeping draft intact', async () => {
  let registered: { saveSelection: () => Promise<boolean>; canSaveSelection: () => boolean } | null = null
  update.mockRejectedValueOnce({ response: { status: 409, data: { reason: 'CONFLICT' } } })
  const wrapper = mountSelector({
    modelValue: ['Exact.Case/ID', 'Draft/ID'],
    persistedSelection: { models: ['Exact.Case/ID'], mappings: [] },
    registerActions: (actions: typeof registered) => { registered = actions }
  })
  await flushPromises()

  const result = await registered!.saveSelection()
  await flushPromises()
  expect(result).toBe(false)
  expect(wrapper.get('[data-testid="model-save-status"]').text()).toBe('admin.accounts.modelSync.selectionSaveFailed')
  expect(wrapper.get('[data-testid="selected-models"]').text()).toContain('Draft/ID')
  wrapper.unmount()
})
