import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import ModelCatalogManagementView from '../ModelCatalogManagementView.vue'

const mocks = vi.hoisted(() => ({ list: vi.fn(), detail: vi.fn(), refresh: vi.fn() }))
const workspace = ref<string | null>(null)
const project = ref<string | null>(null)
vi.mock('vue-i18n', () => ({ useI18n: () => ({ locale: ref('en') }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isAdmin: false }) }))
vi.mock('@/features/console/scope', () => ({ useConsoleScope: () => ({
  scopes: ref([{ workspace_id: '1', name: 'Workspace' }]),
  availableProjects: ref([{ id: '10', name: 'One' }, { id: '20', name: 'Two' }]),
  activeWorkspaceId: workspace, activeProjectId: project, scopeError: ref(null),
  refreshScopes: mocks.refresh, selectWorkspaceProject: vi.fn(), selectPersonalScope: vi.fn(),
}) }))
vi.mock('@/features/console/catalogApi', () => ({ catalogApi: { listScopedModels: mocks.list, getModelDiagnostics: mocks.detail } }))

function render() { return mount(ModelCatalogManagementView, { global: { stubs: { AppLayout: { template: '<main><slot /></main>' }, RouterLink: { template: '<a><slot /></a>' } } } }) }
describe('Scoped model operations', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    workspace.value = null
    project.value = null
    mocks.refresh.mockResolvedValue(undefined)
    mocks.list.mockResolvedValue([{ id: 'm_one', provider: 'one', displayName: 'Shared' }])
  })
  it('requires an explicit project and preserves personal scope', async () => {
    const wrapper = render()
    await flushPromises()
    expect(mocks.list).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('personal space does not switch to Default')
    expect(project.value).toBeNull()
    wrapper.unmount()
  })
  it('requests all scoped offers and shows an authorization error without a public fallback', async () => {
    workspace.value = '1'; project.value = '10'
    mocks.detail.mockRejectedValue({ response: { status: 403 } })
    const wrapper = render()
    await flushPromises()
    expect(mocks.list).toHaveBeenCalledWith('1', '10', { accessible: null }, expect.any(AbortSignal))
    await wrapper.findAll('select')[2].setValue('m_one')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('models.read and connections.read')
    expect(wrapper.find('table').exists()).toBe(false)
    expect(mocks.detail).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
  it('drops account evidence returned after a project switch', async () => {
    workspace.value = '1'; project.value = '10'
    let complete!: (value: unknown) => void
    mocks.detail.mockImplementation(() => new Promise(resolve => { complete = resolve }))
    const wrapper = render()
    await flushPromises()
    await wrapper.findAll('select')[2].setValue('m_one')
    await flushPromises()
    const signal = mocks.detail.mock.calls[0][3] as AbortSignal
    project.value = '20'
    await flushPromises()
    complete({ model: { id: 'm_one', provider: 'one', display_name: 'OLD PRIVATE DETAILS' }, offers: [], field_evidence: [], catalog_version: '1', evaluated_at: '2026-11-23T00:00:00Z' })
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(wrapper.text()).not.toContain('OLD PRIVATE DETAILS')
    expect(mocks.list).toHaveBeenLastCalledWith('1', '20', { accessible: null }, expect.any(AbortSignal))
    wrapper.unmount()
  })
})
