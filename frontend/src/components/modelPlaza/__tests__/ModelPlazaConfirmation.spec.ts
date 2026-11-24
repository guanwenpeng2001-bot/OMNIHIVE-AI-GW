import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ModelPlazaConfirmation from '../ModelPlazaConfirmation.vue'
import { confirmPlazaModels, getPlazaCandidates } from '@/api/admin/channels'
const http = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: http }))
vi.mock('@/api/admin/channels', () => ({ getPlazaCandidates: vi.fn(), confirmPlazaModels: vi.fn() }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { id: 7 } }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const view = { group_id: 2, revision: 'saved', candidates: [{ platform: 'openai', name: 'new-model' }], selected: [{ platform: 'openai', name: 'legacy' }] }
beforeEach(() => {
 setActivePinia(createPinia())
 vi.clearAllMocks()
 vi.mocked(getPlazaCandidates).mockResolvedValue(structuredClone(view))
 vi.mocked(confirmPlazaModels).mockResolvedValue()
})
describe('ModelPlazaConfirmation', () => {
 it('does not preselect discoveries and saves only an explicit choice', async () => {
  const wrapper = mount(ModelPlazaConfirmation, { props: { channelId: 1, groupId: 2 } })
  await flushPromises()
  const choices = wrapper.findAll('input')
  expect((choices[0].element as HTMLInputElement).checked).toBe(false)
  expect((choices[1].element as HTMLInputElement).checked).toBe(true)
  expect(wrapper.text()).toContain('modelPlaza.confirmation.legacy')
  await choices[0].setValue(true)
  await wrapper.findAll('button')[1].trigger('click')
  await flushPromises()
  expect(confirmPlazaModels).toHaveBeenCalledWith(1,2,'saved', [view.candidates[0],view.selected[0]])
 })
 it('publishes the selected group through the revision-checked channel API and reloads it', async () => {
  const api = await vi.importActual<typeof import('@/api/admin/channels')>('@/api/admin/channels')
  vi.mocked(getPlazaCandidates).mockImplementationOnce(api.getPlazaCandidates).mockImplementationOnce(api.getPlazaCandidates)
  vi.mocked(confirmPlazaModels).mockImplementationOnce(api.confirmPlazaModels)
  http.get.mockResolvedValueOnce({ data: view }).mockResolvedValueOnce({
    data: { ...view, revision: 'next', selected: [view.candidates[0]] }
  })
  http.put.mockResolvedValue({ data: { saved: true } })
  const wrapper = mount(ModelPlazaConfirmation, { props: { channelId: 1, groupId: 2 } })
  await flushPromises()
  const choices = wrapper.findAll('input')
  await choices[0].setValue(true)
  await choices[1].setValue(false)
  await wrapper.findAll('button')[1].trigger('click')
  await flushPromises()
  expect(http.put).toHaveBeenCalledWith('/admin/channels/1',
    { group_id: 2, revision: 'saved', models: [view.candidates[0]] }, { params: { plaza_confirmation: true } })
  expect(http.get).toHaveBeenCalledTimes(2)
  expect(http.get).toHaveBeenLastCalledWith('/admin/channels/1', { params: { plaza_group_id: 2 } })
  expect(wrapper.findAll('input')).toHaveLength(1)
  expect(wrapper.get('input').element.checked).toBe(true)
 })
 it.each([
  { status: 409, code: 'PLAZA_CONFIRMATION_CONFLICT', message: 'Candidates changed; reload and confirm again' },
  { status: 409, message: 'Conflict' },
  { code: 'PLAZA_CONFIRMATION_CONFLICT', message: 'Conflict' }
 ])('C04 retains draft on normalized client conflict %j and requires explicit reload', async (failure) => {
  vi.mocked(confirmPlazaModels).mockRejectedValue(failure)
  const wrapper = mount(ModelPlazaConfirmation, { props: { channelId: 1, groupId: 2 } })
  await flushPromises()
  await wrapper.find('input').setValue(true)
  await wrapper.findAll('button')[1].trigger('click')
  await flushPromises()
  expect(wrapper.find('[role="alert"]').text()).toContain('conflict')
  expect((wrapper.find('input').element as HTMLInputElement).checked).toBe(true)
  expect(wrapper.findAll('button')[1].attributes('disabled')).toBeDefined()
  await wrapper.findAll('button')[1].trigger('click')
  await flushPromises()
  expect(confirmPlazaModels).toHaveBeenCalledTimes(1)
  expect(getPlazaCandidates).toHaveBeenCalledTimes(1)
  vi.mocked(getPlazaCandidates).mockRejectedValueOnce({ status: 503, message: 'offline' })
  await wrapper.findAll('button')[0].trigger('click')
  await flushPromises()
  expect(wrapper.findAll('button')[1].attributes('disabled')).toBeDefined()
  vi.mocked(getPlazaCandidates).mockResolvedValue({ ...structuredClone(view), revision: 'reloaded' })
  vi.mocked(confirmPlazaModels).mockResolvedValue()
  await wrapper.findAll('button')[0].trigger('click')
  await flushPromises()
  expect((wrapper.find('input').element as HTMLInputElement).checked).toBe(false)
  expect(wrapper.findAll('button')[1].attributes('disabled')).toBeUndefined()
  await wrapper.find('input').setValue(true)
  await wrapper.findAll('button')[1].trigger('click')
  await flushPromises()
  expect(confirmPlazaModels).toHaveBeenNthCalledWith(2, 1, 2, 'reloaded', [view.candidates[0], view.selected[0]])
 })
 it('ignores a late response after changing group', async () => {
  let complete!: (value: typeof view) => void
  vi.mocked(getPlazaCandidates).mockReturnValueOnce(new Promise(resolve => { complete = resolve }))
  const wrapper = mount(ModelPlazaConfirmation, { props: { channelId: 1, groupId: 2 } })
  vi.mocked(getPlazaCandidates).mockResolvedValue({ ...view, candidates: [], selected: [], group_id: 3 })
  await wrapper.setProps({ groupId: 3 })
  await flushPromises()
  complete(view)
  await flushPromises()
  expect(wrapper.findAll('input')).toHaveLength(0)
 })
})
