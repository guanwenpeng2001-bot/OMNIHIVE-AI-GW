import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { reactive, ref } from 'vue'
import QuickstartGuide from '@/components/intro/QuickstartGuide.vue'
const mocks = vi.hoisted(() => ({
  auth: {} as { user: { id: number; role: string }; token: string; sessionRevision: string },
  app: { apiBaseUrl: '' }, list: vi.fn(), getById: vi.fn(), settings: vi.fn(), fetch: vi.fn()
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks.app }))
vi.mock('@/api/keys', () => ({ keysAPI: { list: mocks.list, getById: mocks.getById } }))
vi.mock('@/api/client', () => ({ apiClient: { get: (path: string, options: unknown) =>
  path === '/settings/public' ? mocks.settings(options) : mocks.getById(path, options)
} }))
vi.mock('vue-i18n', async (original) => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ locale: ref('en') }) }))
let wrapper: ReturnType<typeof mount>
const mountGuide = () => mount(QuickstartGuide, { global: { stubs: { CodeBlock: { props: ['code'], template: '<pre>{{ code }}</pre>' } } } })
beforeEach(() => {
  mocks.auth = reactive({ user: { id: 1, role: 'user' }, token: 'jwt-only', sessionRevision: 'session' })
  mocks.app = reactive({ apiBaseUrl: 'https://gateway.example/deploy/v1/' })
  mocks.list.mockResolvedValue({ items: [{ id: 1, name: 'My laptop', group_id: 2, group: { name: 'Text' }, status: 'active', key: 'private-secret' }], total: 1 })
  mocks.getById.mockResolvedValue({ data: { id: 1, group_id: 2, status: 'active', key: 'private-secret' } })
  mocks.settings.mockResolvedValue({ data: { api_base_url: 'https://gateway.example/deploy/v1/' } })
  mocks.fetch.mockResolvedValue({ ok: true, json: async () => ({ data: [{ id: 'model-a', endpoints: ['responses'] }] }) })
  vi.stubGlobal('fetch', mocks.fetch)
  localStorage.clear()
  wrapper = mountGuide()
})
afterEach(() => { wrapper.unmount(); vi.clearAllMocks(); vi.unstubAllGlobals() })
async function loadCatalogue() {
  await wrapper.get('button').trigger('click')
  await flushPromises()
  await wrapper.get('select').setValue(1)
  await wrapper.get('input[type="checkbox"]').setValue(true)
  await wrapper.findAll('button').find(button => button.text() === 'Load models for this key')!.trigger('click')
  await flushPromises()
}
describe('authorized read-only onboarding', () => {
  it('never calls a model on mount; generated examples and storage contain no actual key', async () => {
    expect(mocks.list).not.toHaveBeenCalled()
    expect(mocks.fetch).not.toHaveBeenCalled()
    await loadCatalogue()
    expect(mocks.fetch).toHaveBeenCalledTimes(1)
    expect(mocks.fetch).toHaveBeenCalledWith('https://gateway.example/deploy/v1/models', expect.objectContaining({
      headers: { Authorization: 'Bearer private-secret' }, redirect: 'error', cache: 'no-store',
      credentials: 'omit', referrerPolicy: 'no-referrer', signal: expect.any(AbortSignal)
    }))
    expect(wrapper.findAll('pre')).toHaveLength(3)
    expect(wrapper.text()).toContain('pending receipt')
    expect(wrapper.text()).not.toContain('private-secret')
    expect(wrapper.text()).toContain('/deploy/v1/responses')
    expect(localStorage.length).toBe(0)
    await wrapper.get('input[type="search"]').setValue('not-a-model')
    expect(wrapper.text()).toContain('No filter matches')
    expect(wrapper.findAll('pre')).toHaveLength(0)
  })
  it('discards a late catalogue after switching identity, including all filters', async () => {
    let resolve!: (value: unknown) => void
    mocks.fetch.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    await loadCatalogue()
    mocks.auth.user.id = 9
    resolve({ ok: true, json: async () => ({ data: [{ id: 'old-private-model' }] }) })
    await flushPromises()
    expect(wrapper.text()).not.toContain('old-private-model')
    expect(wrapper.find('select').exists()).toBe(false)
    expect(wrapper.find('input').exists()).toBe(false)
  })
  it('distinguishes failure and empty without showing raw diagnostics', async () => {
    mocks.fetch.mockRejectedValueOnce(new Error('raw prompt private-secret'))
    await loadCatalogue()
    expect(wrapper.get('[role="alert"]').text()).toContain('Catalogue failed')
    expect(wrapper.text()).not.toContain('raw prompt')
    mocks.fetch.mockResolvedValueOnce({ ok: true, json: async () => ({ data: [] }) })
    await wrapper.findAll('button').find(button => button.text() === 'Load models for this key')!.trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('No visible models')
    expect(wrapper.findAll('pre')).toHaveLength(0)
  })
  it('requires explicit protocol selection when metadata does not declare endpoints', async () => {
    mocks.fetch.mockResolvedValueOnce({ ok: true, json: async () => ({ data: [{ id: 'unknown-model' }] }) })
    await loadCatalogue()
    expect(wrapper.text()).toContain('does not declare endpoint capabilities')
    expect(wrapper.findAll('pre')).toHaveLength(0)
    await wrapper.findAll('select').at(-1)!.setValue('embeddings')
    expect(wrapper.findAll('pre')).toHaveLength(3)
    expect(wrapper.text()).toContain('/deploy/v1/embeddings')
  })
  it('keeps pagination reachable when all keys on a page lack a group', async () => {
    mocks.list.mockResolvedValueOnce({ items: [{ id: 1, name: 'Ungrouped', group_id: null, status: 'active' }], total: 101 })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('No active grouped keys on this page')
    expect(wrapper.findAll('button').some(button => button.text() === 'Next')).toBe(true)
  })

  it('requires confirmation of the displayed target before reading key details', async () => {
    await wrapper.get('button').trigger('click')
    await flushPromises()
    await wrapper.get('select').setValue(1)
    expect(wrapper.text()).toContain('https://gateway.example/deploy')
    const button = wrapper.findAll('button').find(button => button.text() === 'Load models for this key')!
    expect(button.attributes('disabled')).toBeDefined()
    await button.trigger('click')
    expect(mocks.getById).not.toHaveBeenCalled()
    expect(mocks.fetch).not.toHaveBeenCalled()
  })

  it('keeps examples and an in-flight request through ordinary token renewal', async () => {
    let resolve!: (value: unknown) => void
    mocks.fetch.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    await loadCatalogue()
    const signal = mocks.fetch.mock.calls[0][1].signal as AbortSignal
    mocks.auth.token = 'renewed-token'
    expect(signal.aborted).toBe(false)
    resolve({ ok: true, json: async () => ({ data: [{ id: 'current-model', endpoints: ['responses'] }] }) })
    await flushPromises()
    expect(wrapper.findAll('pre')).toHaveLength(3)
    mocks.auth.token = 'renewed-again'
    await flushPromises()
    expect(wrapper.findAll('pre')).toHaveLength(3)
  })

  it.each(['key', 'gateway', 'session', 'role', 'confirmation', 'unmount'])('aborts and rejects late model data after %s changes', async change => {
    let resolve!: (value: unknown) => void
    mocks.fetch.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    await loadCatalogue()
    const signal = mocks.fetch.mock.calls[0][1].signal as AbortSignal
    if (change === 'key') await wrapper.get('select').setValue(null)
    if (change === 'gateway') mocks.app.apiBaseUrl = 'https://other.example/new'
    if (change === 'session') mocks.auth.sessionRevision = 'different-session'
    if (change === 'role') mocks.auth.user.role = 'admin'
    if (change === 'confirmation') await wrapper.get('input[type="checkbox"]').setValue(false)
    if (change === 'unmount') wrapper.unmount()
    expect(signal.aborted).toBe(true)
    resolve({ ok: true, json: async () => ({ data: [{ id: 'stale-private-model', endpoints: ['responses'] }] }) })
    await flushPromises()
    expect(wrapper.text()).not.toContain('stale-private-model')
    expect(wrapper.findAll('pre')).toHaveLength(0)
    if (change === 'gateway') expect(wrapper.find('input[type="checkbox"]').exists()).toBe(false)
  })

  it('cancels a pending key detail and never sends its late credential', async () => {
    let resolve!: (value: unknown) => void
    mocks.getById.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    await loadCatalogue()
    const signal = mocks.getById.mock.calls[0][1].signal as AbortSignal
    mocks.auth.user.id = 2
    expect(signal.aborted).toBe(true)
    resolve({ data: { id: 1, group_id: 2, status: 'active', key: 'old-secret' } })
    await flushPromises()
    expect(mocks.fetch).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('old-secret')
  })

  it('cancels list and settings requests on unmount', async () => {
    let resolve!: (value: unknown) => void
    mocks.list.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    await wrapper.get('button').trigger('click')
    const listSignal = mocks.list.mock.calls[0][3].signal as AbortSignal
    const settingsSignal = mocks.settings.mock.calls[0][0].signal as AbortSignal
    wrapper.unmount()
    expect(listSignal.aborted).toBe(true)
    expect(settingsSignal.aborted).toBe(true)
    resolve({ items: [], total: 0 })
    await flushPromises()
    expect(mocks.fetch).not.toHaveBeenCalled()
  })

  it.each([401, 403])('reports permission denial for HTTP %s without echoing the response', async status => {
    const json = vi.fn().mockResolvedValue({ error: 'private-secret' })
    mocks.fetch.mockResolvedValueOnce({ ok: false, status, json })
    await loadCatalogue()
    expect(wrapper.get('[role="alert"]').text()).toContain('access denied')
    expect(json).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('private-secret')
    expect(wrapper.findAll('pre')).toHaveLength(0)
  })

  it('does not offer examples for explicitly unsupported endpoint metadata', async () => {
    mocks.fetch.mockResolvedValueOnce({ ok: true, json: async () => ({ data: [{ id: 'unsupported', endpoints: [] }] }) })
    await loadCatalogue()
    expect(wrapper.text()).toContain('No supported example endpoints')
    expect(wrapper.findAll('pre')).toHaveLength(0)
  })
})
