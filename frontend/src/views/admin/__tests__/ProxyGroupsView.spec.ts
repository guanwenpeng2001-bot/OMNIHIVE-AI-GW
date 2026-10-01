import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ProxyGroupsView from '../ProxyGroupsView.vue'
import { proxyGroupsAPI } from '@/api/admin/proxyGroups'
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/proxyGroups', () => ({ proxyGroupsAPI: { list: vi.fn(), remove: vi.fn() } }))
const global = { stubs: {
  AppLayout: { template: '<main><slot /></main>' }, TablePageLayout: { template: '<div><slot name="filters"/><slot name="table"/><slot name="pagination"/></div>' },
  Pagination: true, ConfirmDialog: true, ProxyGroupEditModal: true, ProxyGroupMembersModal: true, ProxyGroupBindingsDrawer: true
} }
describe('IP group list', () => {
  beforeEach(() => { vi.clearAllMocks(); vi.mocked(proxyGroupsAPI.list).mockResolvedValue({ items: [], total: 0 } as never) })
  it('offers an actionable empty state and sends search/status/service filters', async () => {
    const wrapper = mount(ProxyGroupsView, { global })
    await flushPromises()
    expect(wrapper.text()).toContain('admin.proxyGroups.empty')
    await wrapper.get('input[type="search"]').setValue('west')
    await wrapper.get('select').setValue('inactive')
    await wrapper.get('input[list]').setValue('grok')
    await flushPromises()
    expect(proxyGroupsAPI.list).toHaveBeenLastCalledWith(1, 20, { search: 'west', status: 'inactive', target_service: 'grok' })
    wrapper.unmount()
  })
  it('shows schedulable/total without treating tags as health and prevents deleting bound groups', async () => {
    vi.mocked(proxyGroupsAPI.list).mockResolvedValue({ items: [{ id: 7, name: 'US group', target_services: ['grok'], status: 'active', fallback_mode: 'none', member_count: 5, schedulable_member_count: 2, bound_account_count: 3 }], total: 1 } as never)
    const wrapper = mount(ProxyGroupsView, { global })
    await flushPromises()
    expect(wrapper.text()).toContain('2 / 5')
    expect(wrapper.text()).toContain('admin.proxyGroups.tagsHint')
    expect(wrapper.text()).toContain('admin.proxyGroups.countsHint')
    expect(wrapper.findAll('button').find(b => b.text() === 'common.delete')!.attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
  it('does not overwrite a newer filter result with a stale response', async () => {
    let resolve!: (value: never) => void
    vi.mocked(proxyGroupsAPI.list).mockImplementationOnce(() => new Promise(done => { resolve = done }))
    const wrapper = mount(ProxyGroupsView, { global })
    await wrapper.get('input[type="search"]').setValue('new')
    await flushPromises()
    resolve({ items: [{ id: 1, name: 'Stale group' }], total: 1 } as never)
    await flushPromises()
    expect(wrapper.text()).not.toContain('Stale group')
    wrapper.unmount()
  })
})
