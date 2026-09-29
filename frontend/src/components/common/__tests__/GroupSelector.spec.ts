import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import GroupSelector from '../GroupSelector.vue'

const authState = { isSimpleMode: false }

vi.mock('@/stores', () => ({ useAuthStore: () => authState }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const groups = [
  { id: 1, name: 'Basic', platform: 'anthropic', status: 'active' },
  { id: 2, name: 'Composite', platform: 'composite', status: 'active' }
] as any

const mountSelector = (modelValue: number[] = []) => mount(GroupSelector, {
  props: { modelValue, groups },
  global: { stubs: { GroupBadge: { props: ['name'], template: '<span>{{ name }}</span>' }, Icon: true } }
})

describe('GroupSelector simple-mode binding policy', () => {
  beforeEach(() => { authState.isSimpleMode = false })

  it('warns about an empty account group selection without requiring it', async () => {
    const wrapper = mountSelector()
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    await wrapper.setProps({ warnWhenEmpty: true, platform: 'cursor' })
    expect(wrapper.text()).toContain('admin.accounts.noGroupWarning')
    await wrapper.setProps({ modelValue: [2] })
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
  })

  it('hides composite groups in simple mode and preserves basic groups', () => {
    authState.isSimpleMode = true
    const wrapper = mountSelector()
    expect(wrapper.text()).toContain('Basic')
    expect(wrapper.text()).not.toContain('Composite')
  })

  it('keeps composite groups available in advanced mode', () => {
    const wrapper = mountSelector()
    expect(wrapper.text()).toContain('Composite')
  })

  it('cleans hidden historical composite IDs while preserving visible selections', () => {
    authState.isSimpleMode = true
    const wrapper = mountSelector([1, 2])
    expect(wrapper.emitted('update:modelValue')).toEqual([[[1]]])
  })
})


it('labels the group, search, and native checkboxes with their business identity', async () => {
  authState.isSimpleMode = false
  const wrapper = mountSelector([1])
  await wrapper.setProps({ searchable: true })
  expect(wrapper.get('fieldset legend').text()).toContain('admin.users.groups')
  expect(wrapper.get('input[type="text"]').attributes('aria-label')).toBe('common.search: admin.users.groups')
  const checkbox = wrapper.get<HTMLInputElement>('input[aria-label="Basic (anthropic)"]')
  expect(checkbox.attributes('type')).toBe('checkbox')
  expect(checkbox.attributes('role')).toBeUndefined()
  expect(checkbox.element.checked).toBe(true)
  expect(checkbox.element.labels?.[0].textContent).toContain('Basic')
  await checkbox.setValue(false)
  expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[]])
  wrapper.unmount()
})
