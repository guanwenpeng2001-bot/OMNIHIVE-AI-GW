import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const {
  copyToClipboard,
  showError,
  showSuccess,
  showInfo,
  showWarning,
  syncUpstreamModels,
  syncUpstreamModelsPreview,
  getAvailableModels
} = vi.hoisted(() => ({
  copyToClipboard: vi.fn().mockResolvedValue(true),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  showInfo: vi.fn(),
  showWarning: vi.fn(),
  syncUpstreamModels: vi.fn(),
  syncUpstreamModelsPreview: vi.fn(),
  getAvailableModels: vi.fn().mockResolvedValue([])
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => (key === 'common.copy' ? '复制' : key)
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
  getAvailableModels
  }
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard
  })
}))

import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'

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
  it('keeps Cursor-exclusive models when platforms are mixed', async () => {
    const wrapper = mountSelector({ platform: undefined, platforms: ['cursor', 'openai'] })
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
    await wrapper.get('button[aria-expanded]').trigger('click')
    expect(findModelRow(wrapper, 'composer-2.5').exists()).toBe(true)
    expect(findModelRow(wrapper, 'cursor-future-model').exists()).toBe(true)
    const fill = wrapper.findAll('button').find(button => button.text() === 'admin.accounts.fillRelatedModels')!
    await fill.trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toContain('composer-2.5')
    wrapper.unmount()
  })

  beforeEach(() => {
    copyToClipboard.mockClear()
    showError.mockReset()
    showSuccess.mockReset()
    showInfo.mockReset()
    showWarning.mockReset()
    syncUpstreamModels.mockReset()
    syncUpstreamModelsPreview.mockReset()
  })

  it('copies a model ID without selecting the model', async () => {
    const wrapper = mountSelector()
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
    await wrapper.get('button[aria-expanded]').trigger('click')

    const row = findModelRow(wrapper, 'gpt-5.6-sol')
    await row.get('[data-testid="select-model"]').trigger('click')

    expect(wrapper.emitted('update:modelValue')).toEqual([[['gpt-5.6-sol']]])
    expect(copyToClipboard).not.toHaveBeenCalled()
  })

  it('warns when model IDs sync but capability metadata is incomplete', async () => {
    syncUpstreamModels.mockResolvedValue({
      models: ['x-preview-f-free'],
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
    expect(wrapper.emitted('update:modelValue')).toEqual([[['x-preview-f-free']]])
    expect(showWarning).toHaveBeenCalledWith('admin.accounts.syncUpstreamModelsMetadataIncomplete')
    expect(showSuccess).not.toHaveBeenCalled()
  })

  it('shows success and neutral information when some capabilities were saved', async () => {
    syncUpstreamModels.mockResolvedValue({
      models: ['gpt-6-astra', 'gpt-image-2'],
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

    expect(wrapper.emitted('update:modelValue')).toEqual([[['gpt-6-astra', 'gpt-image-2']]])
    expect(showSuccess).toHaveBeenCalledWith('admin.accounts.syncUpstreamModelsSuccess')
    expect(showInfo).toHaveBeenCalledWith('admin.accounts.syncUpstreamModelsMetadataPartial')
    expect(showWarning).not.toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()
  })

  it('reports a successful preview so account creation can persist metadata', async () => {
    syncUpstreamModelsPreview.mockResolvedValue({
      models: ['x-preview-f-free'],
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
    expect(wrapper.emitted('update:modelValue')).toEqual([[['x-preview-f-free']]])
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
  finish({ models: ['previous-account-model'] })
  await flushPromises()
  expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  wrapper.unmount()
})


describe('model catalog context fencing', () => {
  it('keeps preset, synchronized and exact selected IDs together', async () => {
    getAvailableModels.mockResolvedValueOnce([{ id: 'remote-id' }])
    const wrapper = mountSelector({ accountId: 100, modelValue: ['Legacy.Case/ID'] })
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
    resolve({ models: ['stale-preview'] })
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.emitted('upstream-synced')).toBeUndefined()
    syncUpstreamModelsPreview.mockResolvedValueOnce({ models: ['fresh-preview'] })
    await sync().trigger('click')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual(['fresh-preview'])
    wrapper.unmount()
  })
  it('distinguishes failure, empty and stale data and offers retry', async () => {
    getAvailableModels.mockRejectedValueOnce(new Error('offline'))
    const wrapper = mountSelector({ accountId: 101 })
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toContain('failed')
    getAvailableModels.mockResolvedValueOnce([])
    await wrapper.findAll('button').find(b => b.text() === 'common.tryAgain')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toContain('empty')
    syncUpstreamModels.mockResolvedValueOnce({ models: ['cached-model'] })
    await wrapper.findAll('button').find(b => b.text() === 'admin.accounts.syncUpstreamModels')!.trigger('click')
    await flushPromises()
    syncUpstreamModels.mockRejectedValueOnce(new Error('offline'))
    await wrapper.findAll('button').find(b => b.text() === 'admin.accounts.syncUpstreamModels')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toContain('stale')
    wrapper.unmount()
  })
})
