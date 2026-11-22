import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { identityApi } from '../scope'
import ProjectGrantEditor from '../components/ProjectGrantEditor.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ locale: { value: 'en' } }) }))
const grants = (version = '1') => ({ member_id: '7', version, effective_at: '2026-11-22T00:00:00Z', project_ids: ['9', '10'], actions: ['text.chat', 'usage.read'], project_grants: [{ project_id: '9', actions: ['text.chat'] }, { project_id: '10', actions: ['usage.read'] }], delegable_actions: ['text.chat', 'usage.read'] })
const props = { workspaceId: '2', memberId: '7', projects: [{ id: '9', name: 'Calls' }, { id: '10', name: 'Reports' }] }
afterEach(() => vi.restoreAllMocks())

describe('project-specific grant editor', () => {
  it('requires an explicit project and saves only its paired actions with CAS', async () => {
    vi.spyOn(identityApi, 'getMemberGrants').mockResolvedValue(grants())
    const save = vi.spyOn(identityApi, 'updateProjectGrants').mockResolvedValue({ member_id: '7', version: '2', effective_at: '2026-11-22T00:00:00Z' })
    const wrapper = mount(ProjectGrantEditor, { props })
    await flushPromises()
    expect(wrapper.findAll('input[type="checkbox"]')).toHaveLength(0)
    await wrapper.get('select').setValue('9')
    const fields = wrapper.findAll('input[type="checkbox"]')
    expect((fields[0].element as HTMLInputElement).checked).toBe(true)
    expect((fields[1].element as HTMLInputElement).checked).toBe(false)
    await fields[0].setValue(false)
    await wrapper.findAll('button')[0].trigger('click')
    await flushPromises()
    expect(save).toHaveBeenCalledWith('2', '7', '9', [], '1')
    wrapper.unmount()
  })
  it('reloads on a CAS conflict and asks for review instead of replaying the write', async () => {
    const read = vi.spyOn(identityApi, 'getMemberGrants').mockResolvedValueOnce(grants()).mockResolvedValueOnce(grants('3'))
    const save = vi.spyOn(identityApi, 'updateProjectGrants').mockRejectedValue({ status: 412 })
    const wrapper = mount(ProjectGrantEditor, { props })
    await flushPromises()
    await wrapper.get('select').setValue('10')
    await wrapper.findAll('button')[0].trigger('click')
    await flushPromises()
    expect(save).toHaveBeenCalledTimes(1)
    expect(read).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('Review before saving again')
    wrapper.unmount()
  })
})
