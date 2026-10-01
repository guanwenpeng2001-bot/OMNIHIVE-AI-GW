import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ProxyGroupEditModal from '../ProxyGroupEditModal.vue'
import ProxyGroupMembersModal from '../ProxyGroupMembersModal.vue'
import ProxyGroupBindingsDrawer from '../ProxyGroupBindingsDrawer.vue'
import { proxyGroupsAPI } from '@/api/admin/proxyGroups'
import { list } from '@/api/admin/proxies'
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/proxyGroups', () => ({ proxyGroupsAPI: { get: vi.fn(), create: vi.fn(), update: vi.fn(), setMembers: vi.fn(), bindings: vi.fn(), resetBindings: vi.fn() } }))
vi.mock('@/api/admin/proxies', () => ({ default: {}, list: vi.fn(), getAll: vi.fn().mockResolvedValue([]) }))
const global = { stubs: { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }, ProxySelector: true, ConfirmDialog: { props: ['show', 'message'], template: '<div v-if="show" data-confirm><p>{{ message }}</p><button @click="$emit(\'confirm\')">confirm</button></div>' } } }
const group = { id: 5, name: 'Group', target_services: ['grok'], status: 'active', fallback_mode: 'none', binding_ttl_seconds: 86400 }
const nodes = [{ id: 1, name: 'Unknown', host: 'local-1', status: 'active', expires_at: null }, { id: 2, name: 'Paused', host: 'local-2', status: 'inactive', expires_at: null }, { id: 3, name: 'Expired', host: 'local-3', status: 'active', expires_at: '2001-01-01' }]
describe('group management dialogs', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(proxyGroupsAPI.get).mockResolvedValue({ ...group, members: [{ proxy: nodes[1] }, { proxy: nodes[2] }] } as never)
    vi.mocked(list).mockResolvedValue({ items: nodes, total: 3 } as never)
  })
  it('keeps edits local until save and preserves the original on cancel', async () => {
    const wrapper = mount(ProxyGroupEditModal, { props: { group: group as never }, global })
    await wrapper.get('input').setValue('Unsaved name')
    await wrapper.findAll('button').find(b => b.text() === 'common.cancel')!.trigger('click')
    expect(group.name).toBe('Group')
    expect(proxyGroupsAPI.update).not.toHaveBeenCalled()
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })
  it('defaults to fail-closed fallback and serializes explicit direct fallback', async () => {
    const wrapper = mount(ProxyGroupEditModal, { global })
    const fallback = wrapper.findAll('select')[1]
    expect(fallback.element.value).toBe('none')
    await wrapper.get('input').setValue('New group')
    await fallback.setValue('direct')
    expect(wrapper.text()).toContain('admin.proxyGroups.directHint')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(proxyGroupsAPI.create).toHaveBeenCalledWith(expect.objectContaining({ name: 'New group', fallback_mode: 'direct', fallback_proxy_id: 0, binding_ttl_seconds: 86400 }))
    wrapper.unmount()
  })
  it('allows unknown health and keeps inactive/expired selected members visible while filtering', async () => {
    const wrapper = mount(ProxyGroupMembersModal, { props: { groupId: 5 }, global })
    await flushPromises()
    expect(wrapper.text()).toContain('Paused')
    expect(wrapper.text()).toContain('Expired')
    expect(wrapper.text()).toContain('admin.proxyGroups.unknown')
    const checkbox = wrapper.get<HTMLInputElement>('input[type="checkbox"][value="1"]')
    expect(checkbox.attributes('disabled')).toBeUndefined()
    await checkbox.setValue(true)
    await wrapper.get('input:not([type="checkbox"])').setValue('Unknown')
    expect(wrapper.findAll('input[type="checkbox"]')).toHaveLength(1)
    expect(wrapper.text()).toContain('Paused')
    await wrapper.findAll('button').find(b => b.text() === 'common.save')!.trigger('click')
    expect(proxyGroupsAPI.setMembers).toHaveBeenCalledWith(5, [2, 3, 1])
    wrapper.unmount()
  })
  it('shows only warmed node latency in observations, including zero', async () => {
    vi.mocked(list).mockResolvedValue({ items: [
      { ...nodes[0], latency_ms: 432 },
      { ...nodes[1], latency_ms: 0, latency_method: 'warmed_http' },
      { ...nodes[2], latency_ms: 234, latency_method: 'unavailable' }
    ], total: 3 } as never)
    const wrapper = mount(ProxyGroupMembersModal, { props: { groupId: 5 }, global })
    await flushPromises()
    expect(wrapper.text()).not.toContain('432 ms')
    expect(wrapper.text()).not.toContain('234 ms')
    expect(wrapper.text()).toContain('0 ms')
    wrapper.unmount()
  })
  it('blocks member replacement if loading failed rather than clearing the group', async () => {
    vi.mocked(list).mockRejectedValueOnce(new Error('offline'))
    const wrapper = mount(ProxyGroupMembersModal, { props: { groupId: 5 }, global })
    await flushPromises()
    const save = wrapper.findAll('button').find(b => b.text() === 'common.save')!
    expect(save.attributes('disabled')).toBeDefined()
    await save.trigger('click')
    expect(proxyGroupsAPI.setMembers).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('requires confirmation before resetting bindings and reloads observations', async () => {
    vi.mocked(proxyGroupsAPI.bindings).mockResolvedValue({ items: [{ account_id: 1, account_name: 'Account', proxy_id: 3, eligible: false }], total: 1 })
    vi.mocked(proxyGroupsAPI.resetBindings).mockResolvedValue({ cleared: 1 })
    const wrapper = mount(ProxyGroupBindingsDrawer, { props: { groupId: 5 }, global })
    await flushPromises()
    await wrapper.findAll('button').find(b => b.text() === 'admin.proxyGroups.reset')!.trigger('click')
    expect(proxyGroupsAPI.resetBindings).not.toHaveBeenCalled()
    expect(wrapper.get('[data-confirm]').text()).toContain('admin.proxyGroups.resetConfirm')
    await wrapper.get('[data-confirm] button').trigger('click')
    await flushPromises()
    expect(proxyGroupsAPI.resetBindings).toHaveBeenCalledWith(5)
    expect(proxyGroupsAPI.bindings).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })
})
