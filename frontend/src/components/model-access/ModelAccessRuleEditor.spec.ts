import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ModelAccessRuleEditor from './ModelAccessRuleEditor.vue'
import { modelAccess } from '@/api/modelAccess'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/modelAccess', () => ({ modelAccess: { groups: vi.fn(), rule: vi.fn(), saveRule: vi.fn(), preview: vi.fn() } }))

describe('model access rule editing', () => {
  beforeEach(() => {
    vi.mocked(modelAccess.groups).mockResolvedValue([{ id: 5, name: 'Public', models: ['public'], revision: 1, references: 0 }])
    vi.mocked(modelAccess.rule).mockResolvedValue({ access_group_id: null, models: null, exclude: [], revision: 3 })
    vi.mocked(modelAccess.saveRule).mockImplementation(async (_path, rule) => ({ ...rule, revision: rule.revision + 1 }))
    vi.mocked(modelAccess.preview).mockResolvedValue({ models: [], fully_enumerable: true })
  })
  it('preserves inheritance and deliberately saves explicit empty as deny-all', async () => {
    const wrapper = mount(ModelAccessRuleEditor, { props: { path: '/keys/7' }, global: { stubs: { Select: true } } })
    await flushPromises()
    await wrapper.findAll('button')[1]!.trigger('click')
    await flushPromises()
    expect(modelAccess.saveRule).toHaveBeenLastCalledWith('/keys/7', expect.objectContaining({ models: null, revision: 3 }))
    await wrapper.get('input[type="checkbox"]').setValue(true)
    await wrapper.findAll('button')[1]!.trigger('click')
    await flushPromises()
    expect(modelAccess.saveRule).toHaveBeenLastCalledWith('/keys/7', expect.objectContaining({ models: [], revision: 4 }))
    expect(wrapper.text()).toContain('modelAccess.empty')
  })
  it('shows save conflicts and does not report success', async () => {
    vi.mocked(modelAccess.saveRule).mockRejectedValueOnce(new Error('Revision changed'))
    const wrapper = mount(ModelAccessRuleEditor, { props: { path: '/keys/7' }, global: { stubs: { Select: true } } })
    await flushPromises()
    await wrapper.findAll('button')[1]!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('Revision changed')
    expect(wrapper.text()).not.toContain('modelAccess.saved')
  })
})
