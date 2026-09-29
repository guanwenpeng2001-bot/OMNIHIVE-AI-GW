import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import AdminComplianceDialog from '../AdminComplianceDialog.vue'
import { useAuthStore } from '@/stores/auth'
import { useAdminComplianceStore } from '@/stores/adminCompliance'
import type { AdminComplianceStatus } from '@/api/admin/compliance'
import { advanceAuthSession } from '@/api/tokenRefresh'
import type { User } from '@/types'

const api = vi.hoisted(() => ({ getStatus: vi.fn(), accept: vi.fn() }))
const app = vi.hoisted(() => ({ showSuccess: vi.fn(), showError: vi.fn() }))
const router = vi.hoisted(() => ({ replace: vi.fn().mockResolvedValue(undefined) }))
const route = vi.hoisted(() => ({ path: '/admin/compliance', fullPath: '/admin/compliance', meta: { requiresAdmin: true }, query: { redirect: '/admin/settings' } }))
vi.mock('@/api/admin/compliance', () => ({ default: api }))
vi.mock('@/api', () => ({ authAPI: {} }))
vi.mock('@/i18n', () => ({ getLocale: () => 'en' }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => router }))
vi.mock('@/stores', async () => ({
  ...await import('@/stores/auth'),
  ...await import('@/stores/adminCompliance'),
  useAppStore: () => app,
}))

let wrapper: VueWrapper
beforeEach(() => {
  vi.clearAllMocks()
  setActivePinia(createPinia())
  const auth = useAuthStore()
  auth.user = { id: 1, role: 'admin' } as User
  auth.token = 'a'
  useAdminComplianceStore().requireAcknowledgement()
  wrapper = mount(AdminComplianceDialog, { attachTo: document.body, global: { stubs: { Icon: true } } })
})
afterEach(() => { wrapper.unmount(); document.body.innerHTML = '' })
const button = (key: string) => Array.from(document.querySelectorAll<HTMLButtonElement>('button')).find(item => item.textContent?.includes(key))!
const typePhrase = async () => {
  const input = document.querySelector<HTMLInputElement>('#admin-compliance-phrase')!
  input.value = useAdminComplianceStore().expectedPhrase
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
}

it('does not accept or dismiss through Escape, outside click, or a mismatched phrase', async () => {
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  document.querySelector<HTMLElement>('.modal-backdrop')?.click()
  const input = document.querySelector<HTMLInputElement>('#admin-compliance-phrase')!
  input.dispatchEvent(new KeyboardEvent('keyup', { key: 'Enter', bubbles: true }))
  await nextTick()
  expect(api.accept).not.toHaveBeenCalled()
  expect(useAdminComplianceStore().shouldShow).toBe(true)
  expect(button('adminCompliance.accept').disabled).toBe(true)
})

it('submits only once and resumes the destination after acceptance', async () => {
  let resolve!: (value: AdminComplianceStatus) => void
  api.accept.mockReturnValue(new Promise<AdminComplianceStatus>(done => { resolve = done }))
  await typePhrase()
  button('adminCompliance.accept').click()
  button('adminCompliance.accept').click()
  await nextTick()
  expect(api.accept).toHaveBeenCalledTimes(1)
  resolve({ required: false } as AdminComplianceStatus)
  await flushPromises()
  expect(app.showSuccess).toHaveBeenCalledTimes(1)
  expect(router.replace).toHaveBeenCalledWith('/admin/settings')
})

it.each(['resolve', 'reject'])('ignores an old acceptance UI result after switching users: %s', async outcome => {
  let resolve!: (value: AdminComplianceStatus) => void
  let reject!: (error: Error) => void
  api.accept.mockReturnValue(new Promise<AdminComplianceStatus>((yes, no) => { resolve = yes; reject = no }))
  await typePhrase()
  button('adminCompliance.accept').click()
  useAuthStore().user = { id: 2, role: 'admin' } as User
  useAdminComplianceStore().requireAcknowledgement()
  await nextTick()
  expect(document.querySelector<HTMLInputElement>('#admin-compliance-phrase')!.value).toBe('')
  if (outcome === 'resolve') resolve({ required: false } as AdminComplianceStatus)
  else reject(new Error('old session error'))
  await flushPromises()
  expect(useAdminComplianceStore().required).toBe(true)
  expect(app.showSuccess).not.toHaveBeenCalled()
  expect(app.showError).not.toHaveBeenCalled()
  expect(router.replace).not.toHaveBeenCalled()
})

it('offers a working logout action without requiring agreement', async () => {
  const logout = vi.spyOn(useAuthStore(), 'logout').mockResolvedValue()
  button('adminCompliance.logout').click()
  await flushPromises()
  expect(logout).toHaveBeenCalledTimes(1)
  expect(router.replace).toHaveBeenCalledWith('/login')
  expect(api.accept).not.toHaveBeenCalled()
})

it('keeps the gate visible after same-user session invalidation', async () => {
  const store = useAdminComplianceStore()
  store.status = { required: false } as AdminComplianceStatus
  store.initialized = true
  await nextTick()
  localStorage.setItem('auth_user', JSON.stringify(useAuthStore().user))
  localStorage.setItem('auth_token', 'same-user-new-session')
  advanceAuthSession()
  await nextTick()
  expect(button('adminCompliance.logout')).toBeDefined()
  expect(store.initialized).toBe(false)
})
