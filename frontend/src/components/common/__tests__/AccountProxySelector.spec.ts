import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import AccountProxySelector from '../AccountProxySelector.vue'
import ProxyGroupSelector from '../ProxyGroupSelector.vue'
import ProxySelector from '../ProxySelector.vue'
import { proxyGroupsAPI } from '@/api/admin/proxyGroups'
import { accountProxyPayload } from '@/utils/accountProxy'
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string, args?: object) => key + (args ? JSON.stringify(args) : '') }) }))
vi.mock('@/api/admin/proxyGroups', () => ({ proxyGroupsAPI: { list: vi.fn(), get: vi.fn() } }))
const active = { id: 8, name: 'US group', status: 'active', member_count: 2, schedulable_member_count: 1 }
function mountSelection(proxyId: number | null = null, groupId: number | null = null) {
  return mount(defineComponent({
    components: { AccountProxySelector },
    setup() { return { node: ref(proxyId), group: ref(groupId), valid: ref(true), accountProxyPayload } },
    template: '<AccountProxySelector v-model:proxy-id="node" v-model:proxy-group-id="group" :proxies="[]" @valid="valid = $event" /><output>{{ JSON.stringify(accountProxyPayload(node, group)) }}</output><span data-valid>{{ valid }}</span>'
  }), { global: { stubs: { ProxySelector: { props: ['modelValue'], template: '<button @click="$emit(\'update:modelValue\', 12)">Pick node</button>' } } } })
}
describe('account egress selection', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(proxyGroupsAPI.list).mockResolvedValue({ items: [active], total: 1 } as never)
  })
  it('keeps default egress and does not load or automatically choose a group', () => {
    const wrapper = mountSelection()
    expect(wrapper.get('select').element.value).toBe('direct')
    expect(wrapper.get('output').text()).toBe('{"proxy_id":0,"proxy_group_id":0}')
    expect(proxyGroupsAPI.list).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('requires an explicit choice and sends mutually exclusive IDs when switching modes', async () => {
    const wrapper = mountSelection(12)
    await wrapper.get('select').setValue('group')
    await flushPromises()
    expect(wrapper.get('[data-valid]').text()).toBe('false')
    await wrapper.getComponent(ProxyGroupSelector).get('select').setValue('8')
    expect(wrapper.get('output').text()).toBe('{"proxy_id":0,"proxy_group_id":8}')
    expect(wrapper.get('[data-valid]').text()).toBe('true')
    await wrapper.get('select').setValue('single')
    expect(wrapper.get('[data-valid]').text()).toBe('false')
    await wrapper.get('button').trigger('click')
    expect(wrapper.get('output').text()).toBe('{"proxy_id":12,"proxy_group_id":0}')
    await wrapper.get('select').setValue('direct')
    expect(wrapper.get('output').text()).toBe('{"proxy_id":0,"proxy_group_id":0}')
    wrapper.unmount()
  })
  it('retains an inactive selected group and never falls back to direct on load failure', async () => {
    vi.mocked(proxyGroupsAPI.get).mockResolvedValue({ ...active, id: 99, name: 'Paused group', status: 'inactive' } as never)
    const wrapper = mountSelection(null, 99)
    await flushPromises()
    expect(wrapper.getComponent(ProxyGroupSelector).text()).toContain('Paused group')
    expect(wrapper.get('output').text()).toBe('{"proxy_id":0,"proxy_group_id":99}')
    wrapper.unmount()
    vi.mocked(proxyGroupsAPI.list).mockRejectedValue(new Error('offline'))
    const failed = mountSelection(null, 99)
    await flushPromises()
    expect(failed.getComponent(ProxyGroupSelector).get('select').element.value).toBe('99')
    expect(failed.get('[role="alert"]').text()).toContain('groupLoadFailed')
    expect(failed.get('output').text()).toBe('{"proxy_id":0,"proxy_group_id":99}')
    failed.unmount()
  })
  it('keeps the legacy node selector no-proxy action usable', async () => {
    const wrapper = mountSelection(12)
    wrapper.getComponent(ProxySelector).vm.$emit('update:modelValue', null)
    await flushPromises()
    expect(wrapper.get('select').element.value).toBe('direct')
    expect(wrapper.get('[data-valid]').text()).toBe('true')
    expect(wrapper.get('output').text()).toBe('{"proxy_id":0,"proxy_group_id":0}')
    wrapper.unmount()
  })
})
