import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import WorkspacesView from '../WorkspacesView.vue'

const mocks = vi.hoisted(() => ({
  list: vi.fn(), members: vi.fn(), create: vi.fn(), setMember: vi.fn(), removeMember: vi.fn(),
  showError: vi.fn(), showSuccess: vi.fn(), auth: { isAdmin: false },
}))

vi.mock('@/api/workspaces', () => ({ workspacesAPI: mocks }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))

const workspace = { id: 2, name: 'Team', role: 'admin', is_default: false }
const mountPage = () => mount(WorkspacesView, { global: { stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } } } })

beforeEach(() => {
  vi.resetAllMocks()
  mocks.auth.isAdmin = false
  mocks.list.mockResolvedValue([workspace])
  mocks.members.mockResolvedValue([{ user_id: 7, role: 'member' }])
  mocks.setMember.mockResolvedValue(undefined)
  mocks.removeMember.mockResolvedValue(undefined)
})

describe('workspace membership UI', () => {
  it('keeps ordinary members out of delegation controls', async () => {
    mocks.list.mockResolvedValue([{ ...workspace, role: 'member' }])
    const page = mountPage()
    await flushPromises()
    expect(mocks.members).not.toHaveBeenCalled()
    expect(page.find('form').exists()).toBe(false)
    expect(page.find('a[href="/workspaces/2/keys"]').exists()).toBe(true)
    expect(page.find('a[href="/workspaces/2/accounts"]').exists()).toBe(false)
    page.unmount()
  })

  it('sends member additions and removals to the selected domain', async () => {
    const page = mountPage()
    await flushPromises()
    await page.find('input[type="number"]').setValue('42')
    await page.find('form select').setValue('admin')
    await page.find('form').trigger('submit')
    await flushPromises()
    expect(mocks.setMember).toHaveBeenCalledWith(2, 42, 'admin')
    const remove = page.findAll('button').find(button => button.text() === 'workspaces.remove')!
    await remove.trigger('click')
    await flushPromises()
    expect(mocks.removeMember).toHaveBeenCalledWith(2, 7)
    expect(mocks.showError).not.toHaveBeenCalled()
    page.unmount()
  })

  it('discards an older member response after switching domains', async () => {
    let resolveOld!: (value: { user_id: number; role: string }[]) => void
    mocks.list.mockResolvedValue([workspace, { ...workspace, id: 3, name: 'Another' }, { ...workspace, id: 4, name: 'Latest' }])
    mocks.members.mockImplementation((workspaceID: number) => workspaceID === 3
      ? new Promise(resolve => { resolveOld = resolve })
      : Promise.resolve([{ user_id: workspaceID * 11, role: 'member' }]))
    const page = mountPage()
    await flushPromises()
    await page.find('select').setValue('3')
    await flushPromises()
    await page.find('select').setValue('4')
    await flushPromises()
    resolveOld([{ user_id: 33, role: 'member' }])
    await flushPromises()
    expect(page.find('li').text()).toContain('44')
    expect(page.find('li').text()).not.toContain('33')
    page.unmount()
  })
})
