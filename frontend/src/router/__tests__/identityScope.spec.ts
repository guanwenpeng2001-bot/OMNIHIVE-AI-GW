import { describe, expect, it, vi } from 'vitest'
import { defineComponent, h, nextTick, reactive, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { identityScoped } from '../identityScope'
const { state } = vi.hoisted(() => ({ state: { current: null as any } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => state.current }))

describe('route identity boundary', () => {
  it('resets page filters/data for a new user and never renders admin data after role loss', async () => {
    state.current = reactive({ user: { id: 1, role: 'admin' }, isAuthenticated: true, isAdmin: true, token: 'private' })
    const Page = defineComponent({ setup() { const filter = ref(''); return () => h('input', { value: filter.value, onInput: (e: Event) => { filter.value = (e.target as HTMLInputElement).value } }) } })
    const wrapper = mount(identityScoped(Page, true))
    await wrapper.get('input').setValue('user-one-filter')
    state.current.user.id = 2
    await nextTick()
    expect((wrapper.get('input').element as HTMLInputElement).value).toBe('')
    expect(wrapper.html()).not.toContain('private')
    state.current.isAdmin = false
    state.current.user.role = 'user'
    await nextTick()
    expect(wrapper.find('input').exists()).toBe(false)
    wrapper.unmount()
  })
  it('does not reset personal filters on ordinary token refresh, and clears on logout', async () => {
    state.current = reactive({ user: { id: 1, role: 'user' }, isAuthenticated: true, isAdmin: false, token: 'first' })
    let mounts = 0
    const wrapper = mount(identityScoped(defineComponent({ setup() { mounts++; return () => h('p', 'My data') } }), false))
    state.current.token = 'rotated'
    await nextTick()
    expect(mounts).toBe(1)
    state.current.isAuthenticated = false
    await nextTick()
    expect(wrapper.text()).toBe('')
    wrapper.unmount()
  })

  it('remounts for a new session belonging to the same user', async () => {
    state.current = reactive({ user: { id: 1, role: 'user' }, isAuthenticated: true, sessionRevision: 'first' })
    let mounts = 0
    const wrapper = mount(identityScoped(defineComponent({ setup() { mounts++; return () => h('p', 'My data') } }), false))
    state.current.sessionRevision = 'second'
    await nextTick()
    expect(mounts).toBe(2)
    wrapper.unmount()
  })

  it('preserves an unsaved form when the same user profile is refreshed', async () => {
    state.current = reactive({ user: { id: 1, role: 'admin', balance: 10 }, isAuthenticated: true, isAdmin: true })
    let mounts = 0
    const Page = defineComponent({ setup() {
      mounts++
      const draft = ref('')
      return () => h('input', { value: draft.value, onInput: (e: Event) => { draft.value = (e.target as HTMLInputElement).value } })
    } })
    const wrapper = mount(identityScoped(Page, true))
    await wrapper.get('input').setValue('unsaved-account-edit')
    state.current.user = { id: 1, role: 'admin', balance: 20 }
    await nextTick()
    expect(mounts).toBe(1)
    expect((wrapper.get('input').element as HTMLInputElement).value).toBe('unsaved-account-edit')
    wrapper.unmount()
  })
})
