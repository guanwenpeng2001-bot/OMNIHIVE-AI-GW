import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import type { AdminUser, Group } from '@/types'
import UserAllowedGroupsModal from '../UserAllowedGroupsModal.vue'

const { list, update, showSuccess } = vi.hoisted(() => ({
  list: vi.fn(), update: vi.fn(), showSuccess: vi.fn()
}))
const authState = reactive({ user: { id: 1, role: 'admin' }, isAuthenticated: true })
vi.mock('@/stores/auth', () => ({ useAuthStore: () => authState }))
vi.mock('@/api/admin', () => ({ adminAPI: { groups: { list }, users: { update } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess }) }))
vi.mock('vue-i18n', async (original) => ({
  ...(await original<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
const groups = [
  { id: 11, name: 'Exclusive', platform: 'openai', is_exclusive: true, rate_multiplier: 1, subscription_type: 'standard', status: 'active' },
  { id: 12, name: 'Public', platform: 'openai', is_exclusive: false, rate_multiplier: 1, subscription_type: 'standard', status: 'active' }
] as Group[]
const userA = { id: 101, email: 'a@example.test', allowed_groups: [11, 12], group_rates: { 11: 0.5 }, restrict_public_groups: true } as unknown as AdminUser
const userB = { id: 202, email: 'b@example.test', allowed_groups: [], group_rates: {}, restrict_public_groups: false } as unknown as AdminUser
const wrappers: ReturnType<typeof mountModal>[] = []
function mountModal(user: AdminUser | null = userA) {
  return mount(UserAllowedGroupsModal, {
    props: { show: true, user },
    global: { stubs: {
      BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
      PlatformIcon: true
    } }
  })
}
function open(user: AdminUser | null = userA) {
  const wrapper = mountModal(user)
  wrappers.push(wrapper)
  return wrapper
}
const saveButton = (wrapper: ReturnType<typeof mountModal>) => wrapper.get('button.btn-primary')
// Invoke the handler directly as well as checking the button: disabled UI alone is insufficient.
const save = (wrapper: ReturnType<typeof mountModal>) =>
  (wrapper.vm as unknown as { handleSave: () => Promise<void> }).handleSave()

beforeEach(() => {
  vi.clearAllMocks()
  list.mockReset().mockResolvedValue({ items: groups })
  update.mockReset().mockResolvedValue({})
  vi.spyOn(console, 'error').mockImplementation(() => {})
})
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  vi.restoreAllMocks()
})

describe('UserAllowedGroupsModal draft ownership', () => {
  it('blocks saving until the initial load succeeds and preserves rate deletion semantics', async () => {
    const pending = deferred<{ items: Group[] }>()
    list.mockReturnValueOnce(pending.promise)
    const wrapper = open()
    expect(saveButton(wrapper).attributes('disabled')).toBeDefined()
    expect((wrapper.vm as unknown as { canSave: boolean }).canSave).toBe(false)
    await save(wrapper)
    expect(update).not.toHaveBeenCalled()
    pending.resolve({ items: groups })
    await flushPromises()
    expect(saveButton(wrapper).attributes('disabled')).toBeUndefined()
    expect((wrapper.vm as unknown as { canSave: boolean }).canSave).toBe(true)
    await wrapper.get('input[type="number"]').setValue('')
    await saveButton(wrapper).trigger('click')
    await flushPromises()
    expect(update).toHaveBeenCalledWith(101, {
      allowed_groups: [11, 12], restrict_public_groups: true, group_rates: { 11: null }
    })
    expect(wrapper.emitted('success')).toHaveLength(1)
  })

  it('clears A on close and cannot save its draft while B loads or after B fails', async () => {
    const wrapper = open()
    await flushPromises()
    await wrapper.get('input[type="number"]').setValue('0.75')
    await wrapper.get('button.btn-secondary').trigger('click')
    await wrapper.setProps({ show: false })
    expect(wrapper.findAll('input')).toHaveLength(0)
    await save(wrapper)
    await wrapper.setProps({ show: false, user: null })
    const pending = deferred<{ items: Group[] }>()
    list.mockReturnValueOnce(pending.promise)
    await wrapper.setProps({ show: true, user: userB })
    expect(saveButton(wrapper).attributes('disabled')).toBeDefined()
    expect((wrapper.vm as unknown as { canSave: boolean }).canSave).toBe(false)
    await save(wrapper)
    pending.reject(new Error('offline'))
    await flushPromises()
    expect((wrapper.vm as unknown as { loadFailed: boolean }).loadFailed).toBe(true)
    expect(saveButton(wrapper).attributes('disabled')).toBeDefined()
    expect((wrapper.vm as unknown as { canSave: boolean }).canSave).toBe(false)
    await save(wrapper)
    expect(update).not.toHaveBeenCalled()
  })

  it.each(['resolve', 'reject'] as const)('ignores old load %s without unlocking the new load', async (settlement) => {
    const old = deferred<{ items: Group[] }>()
    const current = deferred<{ items: Group[] }>()
    list.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const wrapper = open()
    await wrapper.setProps({ user: userB })
    if (settlement === 'resolve') old.resolve({ items: groups })
    else old.reject(new Error('stale'))
    await flushPromises()
    expect(saveButton(wrapper).attributes('disabled')).toBeDefined()
    expect((wrapper.vm as unknown as { canSave: boolean }).canSave).toBe(false)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    await save(wrapper)
    expect(update).not.toHaveBeenCalled()
    current.resolve({ items: groups })
    await flushPromises()
    await save(wrapper)
    expect(update).toHaveBeenCalledWith(202, {
      allowed_groups: [], restrict_public_groups: false, group_rates: undefined
    })
  })

  it('does not overwrite a newer loaded draft with an older response', async () => {
    const old = deferred<{ items: Group[] }>()
    list.mockReturnValueOnce(old.promise)
    const wrapper = open()
    await wrapper.setProps({ user: userB })
    await flushPromises()
    old.resolve({ items: [] })
    await flushPromises()
    expect(wrapper.findAll('input[type="number"]')).toHaveLength(2)
    await save(wrapper)
    expect(update).toHaveBeenCalledWith(202, {
      allowed_groups: [], restrict_public_groups: false, group_rates: undefined
    })
  })

  it('invalidates pending loads even when reopening the same user', async () => {
    const old = deferred<{ items: Group[] }>()
    list.mockReturnValueOnce(old.promise)
    const wrapper = open()
    await wrapper.setProps({ show: false })
    old.resolve({ items: groups })
    await flushPromises()
    await save(wrapper)
    expect(update).not.toHaveBeenCalled()
    list.mockResolvedValueOnce({ items: [] })
    await wrapper.setProps({ show: true })
    await flushPromises()
    await save(wrapper)
    expect(update).toHaveBeenCalledWith(101, {
      allowed_groups: [], restrict_public_groups: true, group_rates: undefined
    })
  })

  it('blocks duplicate saves and ignores save completion after switching users', async () => {
    const pending = deferred<object>()
    update.mockReturnValueOnce(pending.promise)
    const wrapper = open()
    await flushPromises()
    const saving = save(wrapper)
    await save(wrapper)
    expect(update).toHaveBeenCalledTimes(1)
    await wrapper.setProps({ user: userB })
    await flushPromises()
    pending.resolve({})
    await saving
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.emitted('success')).toBeUndefined()
    expect(showSuccess).not.toHaveBeenCalled()
    expect(saveButton(wrapper).attributes('disabled')).toBeUndefined()
    expect((wrapper.vm as unknown as { canSave: boolean }).canSave).toBe(true)
  })

  it('invalidates an unsaved draft when the acting administrator changes', async () => {
    const wrapper = open()
    await flushPromises()
    authState.user = { id: 2, role: 'admin' }
    await save(wrapper)
    expect(update).not.toHaveBeenCalled()
    expect(saveButton(wrapper).attributes('disabled')).toBeDefined()
    expect((wrapper.vm as unknown as { canSave: boolean }).canSave).toBe(false)
  })

  it('does not load or save without a target user', async () => {
    const wrapper = open(null)
    await save(wrapper)
    expect(list).not.toHaveBeenCalled()
    expect(update).not.toHaveBeenCalled()
    expect(saveButton(wrapper).attributes('disabled')).toBeDefined()
    expect((wrapper.vm as unknown as { canSave: boolean }).canSave).toBe(false)
  })
})
