import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import BulkModelPreview from '../BulkModelPreview.vue'

const { syncUpstreamModels } = vi.hoisted(() => ({ syncUpstreamModels: vi.fn() }))
vi.mock('@/api/admin/accounts', () => ({ accountsAPI: { syncUpstreamModels } }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
const mountPreview = (props = {}) => mount(BulkModelPreview, {
  props: { active: true, accountIds: [11, 22], filtered: false, modelValue: ['existing'], modelMappings: [], ...props },
  global: { stubs: { ModelCatalogDetails: true } }
})
beforeEach(() => { syncUpstreamModels.mockReset() })

describe('per-account batch model previews', () => {
  it('preserves previous evidence on failed retry without allowing an intersection', async () => {
    syncUpstreamModels.mockResolvedValue({ discovery_complete: true, complete: true, candidate_models: ['historical-model'], models: ['historical-model'] })
    const wrapper = mountPreview()
    await wrapper.get('[data-testid="preview-each-account"]').trigger('click')
    await flushPromises()
    syncUpstreamModels.mockRejectedValueOnce({ status: 502 })
    await wrapper.get('[data-account-id="22"] button').trigger('click')
    await flushPromises()
    const row = wrapper.get('[data-account-id="22"]')
    expect(row.text()).toContain('historical-model')
    expect(row.text()).toContain('previousPreview')
    expect(wrapper.get('[data-testid="add-common-models"]').attributes('disabled')).toBeDefined()
    expect(syncUpstreamModels.mock.calls).toEqual([[11, false], [22, false], [22, false]])
    wrapper.unmount()
  })

  it('clears history on close/reopen and ignores late failures from the old connection', async () => {
    let rejectPreview!: (error: unknown) => void
    syncUpstreamModels.mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectPreview = reject }))
    const wrapper = mountPreview()
    await wrapper.get('[data-testid="preview-each-account"]').trigger('click')
    await wrapper.setProps({ active: false })
    await wrapper.setProps({ active: true, connectionContext: 'new-connection' })
    rejectPreview({ status: 502 })
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-account-id]')).toHaveLength(2)
    expect(wrapper.get('[data-testid="add-common-models"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
  it('uses every real ID and adds the intersection after every result is valid', async () => {
    syncUpstreamModels.mockResolvedValueOnce({ discovery_complete: true, complete: true, rules_revision: 'first', candidate_models: ['shared', 'only-first'], models: ['shared', 'only-first'] })
      .mockResolvedValueOnce({ discovery_complete: true, complete: true, rules_revision: 'second', candidate_models: ['shared', 'only-second'], models: ['shared', 'only-second'] })
    const wrapper = mountPreview()
    await wrapper.get('[data-testid="preview-each-account"]').trigger('click')
    await flushPromises()
    expect(syncUpstreamModels.mock.calls).toEqual([[11, false], [22, false]])
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.get('[data-testid="add-common-models"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[['existing', 'shared']]])
    expect(wrapper.findAllComponents({ name: 'ModelCatalogDetails' }).map(details => details.attributes('result'))).toHaveLength(2)
    wrapper.unmount()
  })

  it('keeps failed accounts in the denominator and retries only that account', async () => {
    syncUpstreamModels.mockResolvedValueOnce({ discovery_complete: true, complete: true, candidate_models: ['shared'], models: ['shared'] }).mockRejectedValueOnce({ status: 502 })
    const wrapper = mountPreview()
    await wrapper.get('[data-testid="preview-each-account"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="add-common-models"]').attributes('disabled')).toBeDefined()
    syncUpstreamModels.mockResolvedValueOnce({ discovery_complete: true, complete: true, candidate_models: ['shared'], models: ['shared'] })
    await wrapper.get('[data-account-id="22"] button').trigger('click')
    await flushPromises()
    expect(syncUpstreamModels.mock.calls).toEqual([[11, false], [22, false], [22, false]])
    expect(wrapper.get('[data-testid="add-common-models"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it.each([undefined, false])('allows completeness %s for additive intersection', async complete => {
    syncUpstreamModels.mockResolvedValueOnce({ discovery_complete: true, complete: true, candidate_models: ['shared'], models: ['shared'] }).mockResolvedValueOnce({ complete, candidate_models: ['shared'], models: ['shared'] })
    const wrapper = mountPreview()
    await wrapper.get('[data-testid="preview-each-account"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="add-common-models"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('stops unstarted requests and permits a later individual preview', async () => {
    let resolvePreview!: (result: unknown) => void
    syncUpstreamModels.mockImplementationOnce(() => new Promise(resolve => { resolvePreview = resolve }))
    const wrapper = mountPreview()
    await wrapper.get('[data-testid="preview-each-account"]').trigger('click')
    await wrapper.get('[data-testid="stop-account-previews"]').trigger('click')
    resolvePreview({ discovery_complete: true, complete: true, candidate_models: ['shared'], models: ['shared'] })
    await flushPromises()
    expect(syncUpstreamModels).toHaveBeenCalledOnce()
    expect(wrapper.get('[data-testid="add-common-models"]').attributes('disabled')).toBeDefined()
    syncUpstreamModels.mockResolvedValueOnce({ discovery_complete: true, complete: true, candidate_models: [], models: [] })
    await wrapper.get('[data-account-id="22"] button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('noCommonModels')
    wrapper.unmount()
  })

  it('invalidates late results on target changes and disabling the field', async () => {
    let resolvePreview!: (result: unknown) => void
    syncUpstreamModels.mockImplementationOnce(() => new Promise(resolve => { resolvePreview = resolve }))
    const wrapper = mountPreview()
    await wrapper.get('[data-testid="preview-each-account"]').trigger('click')
    await wrapper.setProps({ accountIds: [33] })
    resolvePreview({ discovery_complete: true, complete: true, candidate_models: ['old'], models: ['old'] })
    await flushPromises()
    expect(wrapper.text()).not.toContain('old')
    expect(syncUpstreamModels).toHaveBeenCalledOnce()
    await wrapper.setProps({ active: false })
    expect(wrapper.findAll('[data-account-id]')).toHaveLength(0)
    wrapper.unmount()
  })

  it('requires selected IDs for filtered targets and never samples the current page', async () => {
    const wrapper = mountPreview({ filtered: true })
    await wrapper.get('[data-testid="preview-each-account"]').trigger('click')
    expect(syncUpstreamModels).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('bulkSelectIds')
    wrapper.unmount()
  })

  it('does not overwrite a conflicting alias when adding common models', async () => {
    syncUpstreamModels.mockResolvedValue({ discovery_complete: true, complete: true, candidate_models: ['shared'], models: ['shared'] })
    const wrapper = mountPreview({ modelMappings: [{ from: 'shared', to: 'alias-target' }] })
    await wrapper.get('[data-testid="preview-each-account"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="add-common-models"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.get('[role="alert"]').text()).toContain('conflictingTarget')
    wrapper.unmount()
  })
})
