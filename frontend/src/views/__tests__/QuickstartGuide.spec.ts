import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { reactive, ref } from 'vue'
import QuickstartGuide from '@/components/intro/QuickstartGuide.vue'
const locale = ref<'en' | 'zh'>('en')
const mocks = vi.hoisted(() => ({
  auth: {} as { user: { id: number; role: string }; token: string; sessionRevision: string },
  app: { apiBaseUrl: '' }, list: vi.fn(), settings: vi.fn(), visible: vi.fn()
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks.app }))
vi.mock('@/api/keys', () => ({ keysAPI: { list: mocks.list } }))
vi.mock('@/api/modelAccess', () => ({ keyVisibleModels: mocks.visible }))
vi.mock('@/api/client', () => ({ apiClient: { get: (path: string, options: unknown) =>
  path === '/settings/public' ? mocks.settings(options) : Promise.reject(new Error('unexpected path'))
} }))
vi.mock('vue-i18n', async (original) => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ locale, t: (key: string) => key.split('.').reduce((value: any, part) => value[part], locale.value === 'en' ? en : zh) }) }))
let wrapper: ReturnType<typeof mount>
const mountGuide = () => mount(QuickstartGuide, { global: { stubs: { CodeBlock: { props: ['code'], template: '<pre>{{ code }}</pre>' } } } })
beforeEach(() => {
  locale.value = 'en'
  mocks.auth = reactive({ user: { id: 1, role: 'user' }, token: 'jwt-only', sessionRevision: 'session' })
  mocks.app = reactive({ apiBaseUrl: 'https://gateway.example/deploy/v1/' })
  mocks.list.mockResolvedValue({ items: [{ id: 1, name: 'My laptop', group_id: 2, group: { name: 'Text' }, status: 'active', key: '****masked' }], total: 1 })
  mocks.settings.mockResolvedValue({ data: { api_base_url: 'https://gateway.example/deploy/v1/' } })
  mocks.visible.mockResolvedValue({ models: ['model-a'], fullyEnumerable: true })
  localStorage.clear()
  wrapper = mountGuide()
})
afterEach(() => { wrapper.unmount(); vi.clearAllMocks(); vi.unstubAllGlobals() })
async function loadCatalogue() {
  await wrapper.get('button').trigger('click')
  await flushPromises()
  await wrapper.get('select').setValue(1)
  await wrapper.get('input[type="checkbox"]').setValue(true)
  await wrapper.findAll('button').find(button => button.text() === "Load this key's visible models")!.trigger('click')
  await flushPromises()
}
async function selectProtocol(protocol = 'responses') {
  await wrapper.findAll('select').at(-1)!.setValue(protocol)
}
describe('authorized read-only onboarding', () => {
  it.each(['en', 'zh'] as const)('renders localized metadata errors in %s', async language => {
    locale.value = language
    mocks.list.mockRejectedValueOnce(new Error('private internal detail'))
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe((language === 'en' ? en : zh).quickstartGuide.keysFailed)
  })

  it('never reads a model on mount and never sends a credential to the gateway', async () => {
    expect(mocks.list).not.toHaveBeenCalled()
    expect(mocks.visible).not.toHaveBeenCalled()
    await loadCatalogue()
    // The panel session answers the visible set; the browser never holds the secret.
    expect(mocks.visible).toHaveBeenCalledTimes(1)
    expect(mocks.visible).toHaveBeenCalledWith(1, expect.any(AbortSignal))
    expect(wrapper.text()).not.toContain('****masked')
    expect(wrapper.text()).not.toContain('private-secret')
    expect(wrapper.text()).toContain('/deploy')
    expect(localStorage.length).toBe(0)
  })

  it('generates examples from the configured address and carries no key material', async () => {
    await loadCatalogue()
    await selectProtocol()
    expect(wrapper.findAll('pre')).toHaveLength(3)
    expect(wrapper.text()).toContain('pending receipt')
    expect(wrapper.text()).toContain('/deploy/v1/responses')
    expect(wrapper.text()).not.toContain('sk-')
    await wrapper.get('input[type="search"]').setValue('not-a-model')
    expect(wrapper.text()).toContain('No filter matches')
    expect(wrapper.findAll('pre')).toHaveLength(0)
  })

  it('prompts explicitly when the deployment has no public API address', async () => {
    mocks.settings.mockResolvedValueOnce({ data: { api_base_url: '' } })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain((en as any).quickstartGuide.missingApiBaseUrl)
    // No guessed console origin, no examples.
    expect(wrapper.find('input[type="checkbox"]').exists()).toBe(false)
    expect(wrapper.findAll('pre')).toHaveLength(0)
  })

  it('discards a late catalogue after switching identity, including all filters', async () => {
    let resolve!: (value: unknown) => void
    mocks.visible.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    await loadCatalogue()
    mocks.auth.user.id = 9
    resolve({ models: ['old-private-model'], fullyEnumerable: true })
    await flushPromises()
    expect(wrapper.text()).not.toContain('old-private-model')
    expect(wrapper.find('select').exists()).toBe(false)
    expect(wrapper.find('input').exists()).toBe(false)
  })

  it('distinguishes failure and empty without showing raw diagnostics', async () => {
    mocks.visible.mockRejectedValueOnce(new Error('raw prompt private-secret'))
    await loadCatalogue()
    expect(wrapper.get('[role="alert"]').text()).toContain('Loading models failed')
    expect(wrapper.text()).not.toContain('raw prompt')
    mocks.visible.mockResolvedValueOnce({ models: [], fullyEnumerable: true })
    await wrapper.findAll('button').find(button => button.text() === "Load this key's visible models")!.trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('No visible models')
    expect(wrapper.findAll('pre')).toHaveLength(0)
  })

  it('requires explicit protocol selection when the panel declares no endpoints', async () => {
    await loadCatalogue()
    expect(wrapper.text()).toContain('does not declare endpoint capabilities')
    expect(wrapper.findAll('pre')).toHaveLength(0)
    await selectProtocol('embeddings')
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

  it('requires confirmation of the displayed target before reading the visible models', async () => {
    await wrapper.get('button').trigger('click')
    await flushPromises()
    await wrapper.get('select').setValue(1)
    expect(wrapper.text()).toContain('https://gateway.example/deploy')
    const button = wrapper.findAll('button').find(button => button.text() === "Load this key's visible models")!
    expect(button.attributes('disabled')).toBeDefined()
    await button.trigger('click')
    expect(mocks.visible).not.toHaveBeenCalled()
  })

  it('keeps examples and an in-flight request through ordinary token renewal', async () => {
    let resolve!: (value: unknown) => void
    mocks.visible.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    await loadCatalogue()
    const signal = mocks.visible.mock.calls[0][1] as AbortSignal
    mocks.auth.token = 'renewed-token'
    expect(signal.aborted).toBe(false)
    resolve({ models: ['current-model'], fullyEnumerable: true })
    await flushPromises()
    await selectProtocol()
    expect(wrapper.findAll('pre')).toHaveLength(3)
    mocks.auth.token = 'renewed-again'
    await flushPromises()
    expect(wrapper.findAll('pre')).toHaveLength(3)
  })

  it.each(['key', 'gateway', 'session', 'role', 'confirmation', 'unmount'])('aborts and rejects late model data after %s changes', async change => {
    let resolve!: (value: unknown) => void
    mocks.visible.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    await loadCatalogue()
    const signal = mocks.visible.mock.calls[0][1] as AbortSignal
    if (change === 'key') await wrapper.get('select').setValue(null)
    if (change === 'gateway') mocks.app.apiBaseUrl = 'https://other.example/new'
    if (change === 'session') mocks.auth.sessionRevision = 'different-session'
    if (change === 'role') mocks.auth.user.role = 'admin'
    if (change === 'confirmation') await wrapper.get('input[type="checkbox"]').setValue(false)
    if (change === 'unmount') wrapper.unmount()
    expect(signal.aborted).toBe(true)
    resolve({ models: ['stale-private-model'], fullyEnumerable: true })
    await flushPromises()
    expect(wrapper.text()).not.toContain('stale-private-model')
    expect(wrapper.findAll('pre')).toHaveLength(0)
    if (change === 'gateway') expect(wrapper.find('input[type="checkbox"]').exists()).toBe(false)
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
    expect(mocks.visible).not.toHaveBeenCalled()
  })

  it('reports permission denial without echoing the response body', async () => {
    const denial = Object.assign(new Error('denied'), { response: { status: 403, data: { error: 'private-secret' } } })
    mocks.visible.mockRejectedValueOnce(denial)
    await loadCatalogue()
    expect(wrapper.get('[role="alert"]').text()).toContain('access')
    expect(wrapper.text()).not.toContain('private-secret')
    expect(wrapper.findAll('pre')).toHaveLength(0)
  })

  it('does not offer examples when the visible set is empty', async () => {
    mocks.visible.mockResolvedValueOnce({ models: [], fullyEnumerable: true })
    await loadCatalogue()
    expect(wrapper.text()).toContain('No visible models')
    expect(wrapper.findAll('pre')).toHaveLength(0)
  })
})
