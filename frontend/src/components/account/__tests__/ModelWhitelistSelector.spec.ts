import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, ref, computed } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

let realLocale: 'en' | 'zh' | null = null

const {
  copyToClipboard,
  showError,
  showSuccess,
  showInfo,
  showWarning,
  syncUpstreamModels,
  syncUpstreamModelsPreview,
  getAvailableModels,
  getModelCatalog,
  getPlatformModelPresets
} = vi.hoisted(() => ({
  copyToClipboard: vi.fn().mockResolvedValue(true),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  showInfo: vi.fn(),
  showWarning: vi.fn(),
  syncUpstreamModels: vi.fn(),
  syncUpstreamModelsPreview: vi.fn(),
  getAvailableModels: vi.fn().mockResolvedValue([]),
  getPlatformModelPresets: vi.fn().mockResolvedValue([
    { platform: 'openai', coverage_status: 'complete', models: [{ id: 'gpt-5.6-sol' }, { id: 'muse-spark-1.3' }] },
    { platform: 'cursor', coverage_status: 'partial', models: [{ id: 'default' }, { id: 'composer-2.5' }] },
  ]),
  getModelCatalog: vi.fn().mockResolvedValue({ synced: false, complete: false, discovered_count: 0, eligible_count: 0, excluded_count: 0, observed_at: null, last_error: null, snapshot_revision: null, warnings: [] })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => realLocale
        ? key.split('.').reduce((value: any, part) => value?.[part], realLocale === 'en' ? en : zh) ?? key
        : (key === 'common.copy' ? '复制' : key)
    })
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
    showInfo,
    showWarning
  })
}))

vi.mock('@/api/admin/accounts', () => ({
  accountsAPI: {
    syncUpstreamModels,
    syncUpstreamModelsPreview,
  getAvailableModels,
  getModelCatalog
  }
}))

vi.mock('@/api/admin/platformModelPresets', () => ({ getPlatformModelPresets }))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard
  })
}))

import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'

describe('saved catalog evidence states', () => {
  it.each([
    ['unsynced', { synced: false }],
    ['empty', { synced: true, eligible_count: 0 }],
    ['ready', { synced: true, eligible_count: 1, effective_snapshot: { eligible_models: [{ id: 'native' }] } }],
    ['invalidIdentity', { synced: true, warnings: ['identity_changed'], snapshot: { eligible_models: [{ id: 'historical' }] } }],
    ['stale', { synced: true, last_error: 'upstream_unavailable', snapshot: { eligible_models: [{ id: 'historical' }] } }]
  ])('renders %s independently of array length', async (state, summary) => {
    getModelCatalog.mockResolvedValueOnce({ warnings: [], ...summary })
    const wrapper = mountSelector({ accountId: 24, catalogOnly: true })
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toContain(`modelCatalog.${state}`)
    expect(wrapper.find('[data-testid="apply-model-snapshot"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('requires second confirmation and cancels it when configuration changes', async () => {
    syncUpstreamModels.mockReset().mockResolvedValueOnce({ candidate_models: ['target'], models: ['target'], discovery_complete: true, complete: true, review: { account_id: 24, digest: 'review' } })
    const wrapper = mountSelector({ accountId: 24, catalogOnly: true })
    await flushPromises()
    await wrapper.get('[data-testid="preview-upstream-models"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="apply-model-snapshot"]').trigger('click')
    expect(syncUpstreamModels).toHaveBeenCalledTimes(1)
    expect(wrapper.getComponent(ConfirmDialog).props('show')).toBe(true)
    await wrapper.setProps({ configurationDirty: true })
    expect(wrapper.getComponent(ConfirmDialog).props('show')).toBe(false)
    wrapper.getComponent(ConfirmDialog).vm.$emit('confirm')
    expect(syncUpstreamModels).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
})

function mountSelector(props: Record<string, unknown> = {}) {
  return mount(ModelWhitelistSelector, {
    props: {
      modelValue: [],
      platform: 'openai',
      ...props,
    },
    global: {
      stubs: {
        ModelIcon: true
      }
    }
  })
}

function findModelRow(wrapper: ReturnType<typeof mountSelector>, modelId: string) {
  const row = wrapper
    .findAll('[data-testid="model-option"]')
    .find(candidate => candidate.text().includes(modelId))

  if (!row) {
    throw new Error(`Model row not found: ${modelId}`)
  }

  return row
}

describe('ModelWhitelistSelector', () => {
  it('groups account models while preserving selection, copy, search and unknown IDs', async () => {
    getAvailableModels.mockResolvedValueOnce([
      { id: 'private-chat', category: 'chat' },
      { id: 'private-image', category: 'image' },
      { id: 'private-vector', task_type: 'embedding' }
    ])
    const wrapper = mountSelector({ accountId: 9, modelValue: ['custom-unknown'] })
    await flushPromises()
    await wrapper.get('button[aria-expanded]').trigger('click')
    expect(wrapper.get('[data-model-category="chat"]').text()).toContain('private-chat')
    expect(wrapper.get('[data-model-category="embedding"]').text()).toContain('private-vector')
    expect(wrapper.get('[data-model-category="other"]').text()).toContain('custom-unknown')
    await findModelRow(wrapper, 'private-image').get('[data-testid="select-model"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['custom-unknown', 'private-image']])
    await findModelRow(wrapper, 'private-image').get('[data-testid="copy-model-id"]').trigger('click')
    expect(copyToClipboard).toHaveBeenCalledWith('private-image')
    await wrapper.get('input[aria-label="admin.accounts.searchModels"]').setValue('private-vector')
    expect(wrapper.findAll('[data-testid="model-option"]')).toHaveLength(1)
    expect(wrapper.find('[data-model-category="chat"]').exists()).toBe(false)
    expect(wrapper.emitted('update:modelValue')).toHaveLength(1)
    wrapper.unmount()
  })
  it('keeps Cursor-exclusive models when platforms are mixed', async () => {
    const wrapper = mountSelector({ platform: undefined, platforms: ['cursor', 'openai'] })
    await flushPromises()
    await wrapper.get('button[aria-expanded]').trigger('click')
    expect(findModelRow(wrapper, 'default').exists()).toBe(true)
    expect(findModelRow(wrapper, 'muse-spark-1.3').exists()).toBe(true)
    await findModelRow(wrapper, 'default').get('[data-testid="select-model"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toContain('default')
    wrapper.unmount()
  })

  it('offers the Cursor fallback and live picker IDs and fills the live catalog', async () => {
    getAvailableModels.mockResolvedValueOnce([{ id: 'cursor-future-model', display_name: 'Future' }])
    const wrapper = mountSelector({ platform: 'cursor', accountId: 42 })
    await flushPromises()
    await flushPromises()
    await wrapper.get('button[aria-expanded]').trigger('click')
    expect(findModelRow(wrapper, 'composer-2.5').exists()).toBe(true)
    expect(findModelRow(wrapper, 'cursor-future-model').exists()).toBe(true)
    const fill = wrapper.findAll('button').find(button => button.text() === 'admin.accounts.fillRelatedModels')!
    await fill.trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toContain('composer-2.5')
    wrapper.unmount()
  })

  beforeEach(() => {
    realLocale = null
    copyToClipboard.mockClear()
    showError.mockReset()
    showSuccess.mockReset()
    showInfo.mockReset()
    showWarning.mockReset()
    syncUpstreamModels.mockReset()
    syncUpstreamModelsPreview.mockReset()
  })

  it.each(['en', 'zh'] as const)('localizes capability request errors in %s', async locale => {
    realLocale = locale
    syncUpstreamModels.mockRejectedValueOnce({ message: 'model_capability_defaults: identity_fingerprint mismatch' })
    const wrapper = mountSelector({ accountId: 46 })
    await flushPromises()
    const messages = locale === 'en' ? en : zh
    await wrapper.findAll('button').find(item => item.text() === messages.admin.accounts.syncUpstreamModels)!.trigger('click')
    await flushPromises()
    expect(showError).toHaveBeenCalledWith(messages.capabilityMessages.invalid)
    expect(wrapper.text()).toContain(messages.capabilityMessages.invalid)
    expect(wrapper.text()).not.toContain('identity_fingerprint mismatch')
    wrapper.unmount()
    realLocale = null
  })

  it.each(['en', 'zh'] as const)('keeps capability gaps out of Toasts in %s', async locale => {
    realLocale = locale
    syncUpstreamModels.mockResolvedValueOnce({ candidate_models: ['private-model'], models: ['private-model'], warnings: ['upstream_model_metadata_incomplete'],
      metadata: { 'private-model': { missing_fields: ['max_input_tokens'], estimated_fields: ['reasoning'], conflicts: ['output_modalities'] } } })
    const wrapper = mountSelector({ accountId: 46 })
    await flushPromises()
    const messages = locale === 'en' ? en : zh
    await wrapper.findAll('button').find(item => item.text() === messages.admin.accounts.syncUpstreamModels)!.trigger('click')
    await flushPromises()
    expect(showWarning).not.toHaveBeenCalled()
    expect(showWarning).not.toHaveBeenCalled()
    expect(showWarning).not.toHaveBeenCalled()
    wrapper.unmount()
    realLocale = null
  })

  it('copies a model ID without selecting the model', async () => {
    const wrapper = mountSelector()
    await flushPromises()
    await wrapper.get('button[aria-expanded]').trigger('click')

    const row = findModelRow(wrapper, 'gpt-5.6-sol')

    const copyButton = row.get('[data-testid="copy-model-id"]')
    expect(copyButton.attributes('aria-label')).toBe('复制 gpt-5.6-sol')

    await copyButton.trigger('click')
    await flushPromises()

    expect(copyToClipboard).toHaveBeenCalledWith('gpt-5.6-sol')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('keeps the existing model selection behavior', async () => {
    const wrapper = mountSelector()
    await flushPromises()
    await wrapper.get('button[aria-expanded]').trigger('click')

    const row = findModelRow(wrapper, 'gpt-5.6-sol')
    await row.get('[data-testid="select-model"]').trigger('click')

    expect(wrapper.emitted('update:modelValue')).toEqual([[['gpt-5.6-sol']]])
    expect(copyToClipboard).not.toHaveBeenCalled()
  })

  it('keeps discovered IDs without metadata warning Toasts', async () => {
    syncUpstreamModels.mockResolvedValue({
      candidate_models: ['x-preview-f-free'], models: ['x-preview-f-free'],
      warnings: [
        {
          code: 'upstream_model_metadata_incomplete',
          message: 'Model IDs were synced, but capability metadata could not be updated.'
        }
      ]
    })
    const wrapper = mount(ModelWhitelistSelector, {
      props: {
        modelValue: [],
        platform: 'openai',
        accountId: 46
      },
      global: {
        stubs: {
          ModelIcon: true
        }
      }
    })

    const syncButton = wrapper
      .findAll('button')
      .find(button => button.text() === 'admin.accounts.syncUpstreamModels')
    expect(syncButton).toBeDefined()
    await syncButton!.trigger('click')
    await flushPromises()

    expect(syncUpstreamModels).toHaveBeenCalledWith(46, false)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(showWarning).not.toHaveBeenCalled()
    expect(showSuccess).not.toHaveBeenCalled()
  })

  it('keeps preview capability diagnostics inline', async () => {
    syncUpstreamModels.mockResolvedValue({
      candidate_models: ['gpt-6-astra', 'gpt-image-2'], models: ['gpt-6-astra', 'gpt-image-2'],
      warnings: [
        {
          code: 'upstream_model_metadata_partial',
          message: 'Some model capabilities were saved; remaining models are still incomplete.'
        }
      ]
    })
    const wrapper = mount(ModelWhitelistSelector, {
      props: {
        modelValue: [],
        platform: 'openai',
        accountId: 46
      },
      global: {
        stubs: {
          ModelIcon: true
        }
      }
    })

    const syncButton = wrapper
      .findAll('button')
      .find(button => button.text() === 'admin.accounts.syncUpstreamModels')
    expect(syncButton).toBeDefined()
    await syncButton!.trigger('click')
    await flushPromises()

    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(showInfo).not.toHaveBeenCalled()
    expect(showInfo).not.toHaveBeenCalled()
    expect(showWarning).not.toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()
  })

  it('reports a successful preview so account creation can persist metadata', async () => {
    syncUpstreamModelsPreview.mockResolvedValue({
      candidate_models: ['x-preview-f-free'], models: ['x-preview-f-free'],
      metadata: {
        'x-preview-f-free': {
          id: 'x-preview-f-free',
          reasoning: true,
          supported_reasoning_levels: ['low', 'high', 'max'],
        },
      },
    })
    const wrapper = mountSelector({
      syncCredentials: {
        platform: 'openai',
        type: 'apikey',
        base_url: 'https://opencode.ai/zen/v1',
        api_key: 'test-key',
      },
    })
    const syncButton = wrapper
      .findAll('button')
      .find(button => button.text() === 'admin.accounts.syncUpstreamModels')

    expect(syncButton).toBeDefined()
    await syncButton?.trigger('click')
    await flushPromises()

    expect(syncUpstreamModelsPreview).toHaveBeenCalledOnce()
    expect(wrapper.emitted('upstream-synced')).toEqual([[]])
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('shows the upstream sync button for OpenCode Go create-account credentials', () => {
    const wrapper = mountSelector({
      platform: 'opencode_go',
      syncCredentials: {
        platform: 'opencode_go',
        type: 'apikey',
        base_url: 'https://opencode.ai/zen/go/v1',
        api_key: 'sk-test',
      },
    })
    const syncButton = wrapper
      .findAll('button')
      .find(button => button.text() === 'admin.accounts.syncUpstreamModels')

    expect(syncButton).toBeDefined()
    expect(syncButton?.exists()).toBe(true)
  })

  it.each(['qianwen', 'commandcode'])(
    'shows the upstream sync button for %s accounts',
    (platform) => {
      const wrapper = mountSelector({ platform, accountId: 19 })
      const syncButton = wrapper
        .findAll('button')
        .find(button => button.text() === 'admin.accounts.syncUpstreamModels')

      expect(syncButton?.exists()).toBe(true)
      wrapper.unmount()
    }
  )
  it.each([
    ['credentials', 'syncUpstreamModelsCredentials'],
    ['configuration', 'syncUpstreamModelsConfiguration'],
    ['unsupported', 'syncUpstreamModelsUnsupported'],
    ['upstream', 'syncUpstreamModelsUnavailable']
  ])('shows the localized %s preview failure', async (reason, key) => {
    syncUpstreamModelsPreview.mockRejectedValue({ status: 502, reason: `upstream_model_sync_${reason}`, message: 'private details' })
    const wrapper = mountSelector({ syncCredentials: { platform: 'qianwen', type: 'apikey', api_key: 'test' } })
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.syncUpstreamModels')?.trigger('click')
    await flushPromises()
    expect(showError).toHaveBeenCalledWith(`admin.accounts.${key}`)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

})


it('ignores discovery from the previous account after a context switch', async () => {
  let finish!: (value: { models: string[] }) => void
  syncUpstreamModels.mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
  const wrapper = mount(ModelWhitelistSelector, { props: { modelValue: [], platform: 'qianwen', accountId: 1 }, global: { stubs: { ModelIcon: true } } })
  await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.syncUpstreamModels')!.trigger('click')
  await wrapper.setProps({ accountId: 2 })
  finish({ candidate_models: ['previous-account-model'], models: ['previous-account-model'] })
  await flushPromises()
  expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  wrapper.unmount()
})


describe('model catalog context fencing', () => {
  it('keeps preset, synchronized and exact selected IDs together', async () => {
    getAvailableModels.mockResolvedValueOnce([{ id: 'remote-id' }])
    const wrapper = mountSelector({ accountId: 100, modelValue: ['Legacy.Case/ID'] })
    await flushPromises()
    await flushPromises()
    await wrapper.get('button[aria-expanded]').trigger('click')
    for (const id of ['gpt-5.6', 'remote-id', 'Legacy.Case/ID']) expect(findModelRow(wrapper, id).exists()).toBe(true)
    expect(wrapper.find('button button').exists()).toBe(false)
    expect(wrapper.get('button[aria-expanded]').attributes('aria-controls')).toBeTruthy()
    expect(wrapper.find('button[aria-label$="Legacy.Case/ID"]').exists()).toBe(true)
    wrapper.unmount()
  })
  it('invalidates preview on credential change and close/reopen, including A-B-A', async () => {
    let resolve!: (value: { models: string[] }) => void
    syncUpstreamModelsPreview.mockImplementationOnce(() => new Promise(r => { resolve = r }))
    const credentials = { platform: 'openai', credentials: { api_key: 'fake-a' } }
    const wrapper = mountSelector({ syncCredentials: credentials })
    const sync = () => wrapper.findAll('button').find(b => b.text() === 'admin.accounts.syncUpstreamModels')!
    await sync().trigger('click')
    await wrapper.setProps({ syncCredentials: { ...credentials, credentials: { api_key: 'fake-b' } } })
    await wrapper.setProps({ syncCredentials: credentials, active: false })
    await wrapper.setProps({ active: true })
    resolve({ candidate_models: ['stale-preview'], models: ['stale-preview'] })
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.emitted('upstream-synced')).toBeUndefined()
    syncUpstreamModelsPreview.mockResolvedValueOnce({ candidate_models: ['fresh-preview'], models: ['fresh-preview'] })
    await sync().trigger('click')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await flushPromises()
    await wrapper.get('button[aria-expanded]').trigger('click')
    expect(findModelRow(wrapper, 'fresh-preview').exists()).toBe(true)
    wrapper.unmount()
  })
  it('distinguishes failure, empty and stale data and offers retry', async () => {
    getAvailableModels.mockRejectedValueOnce(new Error('offline'))
    const wrapper = mountSelector({ accountId: 101 })
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toContain('failed')
    getAvailableModels.mockResolvedValueOnce([])
    getModelCatalog.mockResolvedValueOnce({ synced: true, warnings: [], eligible_count: 0 })
    await wrapper.findAll('button').find(b => b.text() === 'common.tryAgain')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toContain('empty')
    syncUpstreamModels.mockResolvedValueOnce({ candidate_models: ['cached-model'], models: ['cached-model'] })
    await wrapper.findAll('button').find(b => b.text() === 'admin.accounts.syncUpstreamModels')!.trigger('click')
    await flushPromises()
    syncUpstreamModels.mockRejectedValueOnce(new Error('offline'))
    await wrapper.findAll('button').find(b => b.text() === 'admin.accounts.syncUpstreamModels')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toContain('stale')
    wrapper.unmount()
  })
})


describe('snapshot application contract', () => {
  const preview = {
    candidate_models: ['native-new'], models: ['native-new'], applied: false, discovery_complete: true, complete: true, rules_revision: 'rules-opaque', review: { account_id: 46, digest: 'reviewed' },
    discovered_models: ['native-new', 'pending'],
    diff: { added: ['native-new'], removed: ['native-old'], retained: [] },
    excluded_models: [{ model: 'pending', reason: 'entitlement_unconfirmed' }],
    invalid_aliases: ['old-alias']
  }
  it('previews without selecting models and applies using the revision without saving aliases', async () => {
    syncUpstreamModels.mockReset().mockResolvedValueOnce(preview).mockResolvedValueOnce({ ...preview, applied: true, candidate_models: [], models: [] })
    const wrapper = mountSelector({ accountId: 46, modelValue: ['kept-alias'] })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.syncUpstreamModels')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('native-old')
    expect(wrapper.text()).toContain('entitlement_unconfirmed')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.get('[data-testid="apply-model-snapshot"]').trigger('click')
    wrapper.getComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(syncUpstreamModels.mock.calls).toEqual([[46, false], [46, true, 'rules-opaque', undefined, preview.review]])
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.text()).toContain('admin.accounts.modelSync.syncedEmpty')
    wrapper.unmount()
  })
  it('requires a fresh preview after a conflict and never retries without revision conditions', async () => {
    syncUpstreamModels.mockReset().mockResolvedValueOnce(preview).mockRejectedValueOnce({ status: 409 })
    const wrapper = mountSelector({ accountId: 46 })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.syncUpstreamModels')!.trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="apply-model-snapshot"]').trigger('click')
    wrapper.getComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(syncUpstreamModels).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[role="alert"]').text()).toContain('modelSync.conflict')
    expect(wrapper.get('[data-testid="apply-model-snapshot"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
  it('ignores an apply response after closing and switching accounts', async () => {
    let finishApply!: (result: typeof preview) => void
    syncUpstreamModels.mockReset().mockResolvedValueOnce(preview).mockImplementationOnce(() => new Promise(resolve => { finishApply = resolve }))
    const wrapper = mountSelector({ accountId: 46 })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.syncUpstreamModels')!.trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="apply-model-snapshot"]').trigger('click')
    wrapper.getComponent(ConfirmDialog).vm.$emit('confirm')
    await wrapper.setProps({ active: false })
    await wrapper.setProps({ active: true, accountId: 47 })
    finishApply({ ...preview, applied: true })
    await flushPromises()
    expect(wrapper.text()).not.toContain('native-new')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    wrapper.unmount()
  })
  it('cannot apply an incomplete preview or an old server response without a revision', async () => {
    syncUpstreamModels.mockReset().mockResolvedValueOnce({ candidate_models: ['candidate'], models: ['candidate'] })
    const wrapper = mountSelector({ accountId: 46 })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.syncUpstreamModels')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="apply-model-snapshot"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
})


it('uses server platform candidates, preserving unknown when their request fails', async () => {
  getPlatformModelPresets.mockResolvedValueOnce([{ platform: 'openai', coverage_status: 'partial', models: [{ id: 'server-only-model' }] }])
  const wrapper = mountSelector()
  await flushPromises()
  await wrapper.get('button[aria-expanded]').trigger('click')
  expect(findModelRow(wrapper, 'server-only-model').exists()).toBe(true)
  expect(wrapper.text()).not.toContain('gpt-5.6-sol')
  expect(wrapper.findComponent({ name: 'ModelCatalogDetails' }).props('candidateCount')).toBe(1)
  wrapper.unmount()
  getPlatformModelPresets.mockRejectedValueOnce(new Error('offline'))
  const failed = mountSelector({ modelValue: ['saved-custom-model'] })
  await flushPromises()
  expect(failed.findComponent({ name: 'ModelCatalogDetails' }).props('candidateCount')).toBeNull()
  expect(failed.text()).toContain('saved-custom-model')
  failed.unmount()
})


describe('explicit preview adoption', () => {
  const completePreview = {
    candidate_models: [' new-id ', 'new-id', 'Second.Case/ID'], models: [' new-id ', 'new-id', 'Second.Case/ID'], discovery_complete: true, complete: true, applied: false,
    rules_revision: 'saved-rules', metadata: { 'metadata-only': {} },
    discovered_models: ['new-id', 'excluded-id'],
    excluded_models: [{ model: 'excluded-id', reason: 'entitlement_unconfirmed' }],
    diff: { added: ['new-id'], removed: ['obsolete'], retained: [] }
  }
  it('appends only complete preview IDs in order, preserves candidates through real v-model updates and is idempotent', async () => {
    syncUpstreamModelsPreview.mockResolvedValueOnce(completePreview)
    const Host = defineComponent({
      components: { ModelWhitelistSelector },
      setup() {
        const models = ref(['Legacy.Case/ID'])
        const credentials = computed(() => ({ platform: 'openai', api_key: 'key', model_mapping: Object.fromEntries(models.value.map(model => [model, model])) }))
        return { models, credentials }
      },
      template: '<ModelWhitelistSelector v-model="models" platform="openai" :sync-credentials="credentials" />'
    })
    const host = mount(Host, { global: { stubs: { ModelIcon: true } } })
    const selector = host.getComponent(ModelWhitelistSelector)
    await selector.get('[data-testid="preview-upstream-models"]').trigger('click')
    await flushPromises()
    expect(selector.emitted('update:modelValue')).toBeUndefined()
    expect(selector.find('[data-testid="apply-model-snapshot"]').exists()).toBe(false)
    await selector.get('input[aria-label="admin.accounts.customModelName"]').setValue('ignored-search')
    await selector.get('[data-testid="add-preview-models"]').trigger('click')
    expect(host.vm.models).toEqual(['Legacy.Case/ID', 'new-id', 'Second.Case/ID'])
    expect(selector.text()).toContain('admin.accounts.modelSync.previewCandidates')
    expect(selector.text()).not.toContain('obsolete')
    await selector.get('[data-testid="add-preview-models"]').trigger('click')
    expect(selector.emitted('update:modelValue')).toHaveLength(1)
    await selector.get('button[aria-expanded]').trigger('click')
    await selector.findAll('[data-testid="select-model"]').find(button => button.text() === 'new-id')!.trigger('click')
    expect(host.vm.models).toEqual(['Legacy.Case/ID', 'Second.Case/ID'])
    expect(selector.findAll('[data-testid="select-model"]').some(button => button.text() === 'new-id')).toBe(true)
    host.unmount()
  })
  it('keeps a disabled preview action and visible associated reason without credentials', async () => {
    const wrapper = mountSelector()
    const preview = wrapper.get('[data-testid="preview-upstream-models"]')
    expect(preview.attributes('disabled')).toBeDefined()
    expect(wrapper.get(`[id="${preview.attributes('aria-describedby')}"]`).text()).toContain('credentialsRequired')
    wrapper.unmount()
  })
  it.each([false, undefined])('allows additive adoption when completeness is %s', async complete => {
    syncUpstreamModelsPreview.mockResolvedValueOnce({ ...completePreview, complete })
    const wrapper = mountSelector({ syncCredentials: { platform: 'openai', api_key: 'key' } })
    await wrapper.get('[data-testid="preview-upstream-models"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="add-preview-models"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })
  it('rejects alias collisions explicitly without replacing aliases or selection', async () => {
    syncUpstreamModelsPreview.mockResolvedValueOnce(completePreview)
    const wrapper = mountSelector({ syncCredentials: { platform: 'openai', api_key: 'key' }, modelMappings: [{ from: 'new-id', to: 'alias-target' }] })
    await wrapper.get('[data-testid="preview-upstream-models"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="add-preview-models"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.get('[role="alert"]').text()).toContain('admin.accounts.modelValidation.conflictingTarget')
    wrapper.unmount()
  })
  it('keeps catalog-only views without a selection owner read-only', async () => {
    syncUpstreamModels.mockResolvedValueOnce(completePreview)
    const wrapper = mountSelector({ accountId: 42, catalogOnly: true })
    await flushPromises()
    await wrapper.get('[data-testid="preview-upstream-models"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="add-preview-models"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="select-model"]').exists()).toBe(false)
    wrapper.unmount()
  })
  it.each([false, true])('allows additive adoption in catalog-only views (complete=%s)', async discoveryComplete => {
    syncUpstreamModels.mockResolvedValueOnce({ ...completePreview, discovery_complete: discoveryComplete, review: { account_id: 42, digest: 'review' } })
    const wrapper = mountSelector({ accountId: 42, catalogOnly: true, catalogSelectionEnabled: true, modelValue: ['existing'] })
    await flushPromises()
    await wrapper.get('[data-testid="preview-upstream-models"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="add-preview-models"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="add-preview-models"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['existing', 'new-id', 'Second.Case/ID']])
    expect(wrapper.get('[data-testid="apply-model-snapshot"]').attributes('disabled') !== undefined).toBe(!discoveryComplete)
    expect(wrapper.find('[data-testid="select-model"]').exists()).toBe(false)
    expect(wrapper.find('input').exists()).toBe(false)
    wrapper.unmount()
  })
  it('invalidates requests on identity changes without letting old finally stop the next request', async () => {
    let resolveOld!: (value: typeof completePreview) => void
    let resolveNew!: (value: typeof completePreview) => void
    syncUpstreamModelsPreview.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
      .mockImplementationOnce(() => new Promise(resolve => { resolveNew = resolve }))
    const wrapper = mountSelector({ syncCredentials: { platform: 'openai', api_key: 'key' } })
    await wrapper.get('[data-testid="preview-upstream-models"]').trigger('click')
    await wrapper.setProps({ candidateIdentity: { host: 'https://other.example', type: 'apikey' } })
    expect(wrapper.get('[data-testid="preview-upstream-models"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="preview-upstream-models"]').trigger('click')
    resolveOld(completePreview)
    await flushPromises()
    expect(wrapper.get('[data-testid="preview-upstream-models"]').attributes('disabled')).toBeDefined()
    resolveNew(completePreview)
    await flushPromises()
    expect(wrapper.get('[data-testid="preview-upstream-models"]').attributes('disabled')).toBeUndefined()
    await wrapper.setProps({ connectionContext: { proxy: 2 } })
    expect(wrapper.find('[data-testid="add-preview-models"]').exists()).toBe(false)
    wrapper.unmount()
  })
  it('keeps snapshot apply disabled for unsaved changes and invalidates saved revisions', async () => {
    syncUpstreamModels.mockResolvedValueOnce(completePreview)
    const wrapper = mountSelector({ accountId: 42 })
    await flushPromises()
    await wrapper.get('[data-testid="preview-upstream-models"]').trigger('click')
    await flushPromises()
    await wrapper.setProps({ configurationDirty: true, modelValue: ['new-id'] })
    expect(wrapper.get('[data-testid="apply-model-snapshot"]').attributes('disabled')).toBeDefined()
    await wrapper.setProps({ configurationDirty: false, savedRevision: 1 })
    expect(wrapper.find('[data-testid="apply-model-snapshot"]').exists()).toBe(false)
    wrapper.unmount()
  })
})


it('does not bulk-adopt empty or failed results and leaves existing selections intact', async () => {
  syncUpstreamModelsPreview.mockResolvedValueOnce({ candidate_models: [], models: [], discovery_complete: true, complete: true, applied: false })
  const wrapper = mountSelector({ modelValue: ['kept'], syncCredentials: { platform: 'openai', api_key: 'key' } })
  await wrapper.get('[data-testid="preview-upstream-models"]').trigger('click')
  await flushPromises()
  expect(wrapper.get('[data-testid="add-preview-models"]').attributes('disabled')).toBeDefined()
  syncUpstreamModelsPreview.mockRejectedValueOnce(new Error('offline'))
  await wrapper.get('[data-testid="preview-upstream-models"]').trigger('click')
  await flushPromises()
  expect(wrapper.get('[data-testid="add-preview-models"]').attributes('disabled')).toBeDefined()
  expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  expect(wrapper.props('modelValue')).toEqual(['kept'])
  wrapper.unmount()
})


it('keeps actions before bounded selections and inline options with one selected count', async () => {
  const models = Array.from({ length: 100 }, (_, index) => `long-provider/model-version-${index}`)
  const wrapper = mountSelector({ modelValue: models })
  await flushPromises()
  const selected = wrapper.get('[data-testid="selected-models"]')
  expect(selected.classes()).toContain('max-h-[min(12rem,30vh)]')
  expect(selected.classes()).toContain('overflow-y-auto')
  const removeButton = selected.get('button')
  expect(removeButton.classes()).toEqual(expect.arrayContaining(['h-7', 'w-7']))
  const preview = wrapper.get('[data-testid="preview-upstream-models"]')
  expect(preview.element.compareDocumentPosition(selected.element) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  expect(wrapper.text().split('admin.accounts.selectedModels')).toHaveLength(2)
  await wrapper.get('button[aria-expanded]').trigger('click')
  const options = wrapper.get('[data-testid="model-options"]')
  expect(options.classes()).toContain('max-h-[min(18rem,40vh)]')
  expect(options.element.parentElement?.classList.contains('absolute')).toBe(false)
  expect(findModelRow(wrapper, models[99]).text()).toContain(models[99])
  await options.trigger('keydown', { key: 'Escape' })
  expect(wrapper.get('button[aria-expanded]').attributes('aria-expanded')).toBe('false')
  await wrapper.setProps({ modelValue: [] })
  expect(wrapper.find('[data-testid="selected-models"]').exists()).toBe(false)
  expect(wrapper.get('button[aria-expanded]').classes()).not.toContain('border-t')
  wrapper.unmount()
})


it.each(['trigger', 'search'])('consumes the first Escape from the expanded %s inside a real dialog', async location => {
  const host = mount(defineComponent({
    components: { BaseDialog, ModelWhitelistSelector },
    setup() { return { show: ref(true), models: ref([]) } },
    template: '<BaseDialog :show="show" title="Account" @close="show = false"><ModelWhitelistSelector v-model="models" platform="openai" /></BaseDialog>'
  }), { attachTo: document.body, global: { stubs: { ModelIcon: true } } })
  await flushPromises()
  const selector = host.getComponent(ModelWhitelistSelector)
  const trigger = selector.get<HTMLButtonElement>('button[aria-expanded]')
  trigger.element.focus()
  await trigger.trigger('click')
  const target = location === 'trigger' ? trigger : selector.get<HTMLInputElement>('input[aria-label="admin.accounts.searchModels"]')
  target.element.focus()
  await target.trigger('keydown', { key: 'Escape' })
  expect(host.vm.show).toBe(true)
  expect(trigger.attributes('aria-expanded')).toBe('false')
  expect(document.activeElement).toBe(trigger.element)
  await trigger.trigger('keydown', { key: 'Escape' })
  expect(host.vm.show).toBe(false)
  host.unmount()
})


it.each([534, 1000])('filters %s models locally without selecting them or fetching each row', async count => {
  syncUpstreamModels.mockClear()
  const models = Array.from({ length: count }, (_, index) => `large-${String(index).padStart(4, '0')}`)
  getAvailableModels.mockResolvedValueOnce(models.map(id => ({ id })))
  const wrapper = mountSelector({ accountId: 89, modelValue: ['draft-kept'] })
  await flushPromises()
  const calls = getAvailableModels.mock.calls.length
  await wrapper.get('button[aria-expanded]').trigger('click')
  expect(wrapper.findAll('[data-testid="model-option"]')).toHaveLength(100)
  await wrapper.get('[data-testid="model-options-more"]').trigger('click')
  expect(wrapper.findAll('[data-testid="model-option"]')).toHaveLength(200)
  await wrapper.get('input[aria-label="admin.accounts.searchModels"]').setValue(models[count - 1])
  expect(wrapper.findAll('[data-testid="model-option"]')).toHaveLength(1)
  expect(getAvailableModels).toHaveBeenCalledTimes(calls)
  expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  await wrapper.get('[data-testid="select-model"]').trigger('click')
  expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([['draft-kept', models[count - 1]]])
  expect(syncUpstreamModels).not.toHaveBeenCalled()
  wrapper.unmount()
})


describe('preview evidence and action matrix', () => {
  it.each([
    [true, 'incomplete', ['native'], false],
    [false, 'incomplete', ['native'], true],
    [true, 'complete', [], false]
  ] as const)('separates discovery %s and capability %s from additive selection', async (discovery, capability, candidates, applyDisabled) => {
    syncUpstreamModels.mockReset().mockResolvedValueOnce({ models: [], discovered_models: [...candidates], candidate_models: [...candidates], eligible_models: [], discovery_complete: discovery, complete: discovery, capability_status: capability, review: discovery ? { account_id: 42, digest: 'review' } : undefined })
    const wrapper = mountSelector({ accountId: 42 })
    await flushPromises()
    await wrapper.get('[data-testid="preview-upstream-models"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="add-preview-models"]').attributes('disabled') !== undefined).toBe(candidates.length === 0)
    expect(wrapper.get('[data-testid="apply-model-snapshot"]').attributes('disabled') !== undefined).toBe(applyDisabled)
    await wrapper.setProps({ modelValue: ['local-selection'], modelMappings: [{ from: 'alias', to: 'native' }] })
    expect(wrapper.get('[data-testid="apply-model-snapshot"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="add-preview-models"]').attributes('disabled') !== undefined).toBe(candidates.length === 0)
    wrapper.unmount()
  })

  it('keeps an in-flight discovery when selection changes but revokes its rule review', async () => {
    let resolvePreview!: (result: unknown) => void
    syncUpstreamModels.mockReset().mockImplementationOnce(() => new Promise(resolve => { resolvePreview = resolve }))
    const wrapper = mountSelector({ accountId: 42 })
    await flushPromises()
    await wrapper.get('[data-testid="preview-upstream-models"]').trigger('click')
    await wrapper.setProps({ modelValue: ['chosen-during-discovery'] })
    expect(wrapper.get('[data-testid="preview-upstream-models"]').attributes('disabled')).toBeDefined()
    resolvePreview({ candidate_models: ['native'], discovered_models: ['native'], models: [], discovery_complete: true, capability_status: 'incomplete', review: { account_id: 42, digest: 'review' } })
    await flushPromises()
    expect(wrapper.get('[data-testid="add-preview-models"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-testid="apply-model-snapshot"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="add-preview-models"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['chosen-during-discovery', 'native']])
    wrapper.unmount()
  })

  it('leaves hosted candidates empty while allowing manual entry', async () => {
    const wrapper = mountSelector({ platform: 'anthropic', candidateIdentity: { type: 'bedrock', host: 'https://bedrock.example', region: 'eu-west-1' } })
    await flushPromises()
    await wrapper.get('button[aria-expanded]').trigger('click')
    expect(wrapper.findAll('[data-testid="model-option"]')).toHaveLength(0)
    await wrapper.get('input[aria-label="admin.accounts.customModelName"]').setValue('arn:aws:bedrock:eu-west-1:custom')
    await wrapper.get('input[aria-label="admin.accounts.customModelName"]').trigger('keydown.enter')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['arn:aws:bedrock:eu-west-1:custom']])
    wrapper.unmount()
  })
})
