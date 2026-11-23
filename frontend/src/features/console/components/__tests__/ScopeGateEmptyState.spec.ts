import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import ScopeGateEmptyState from '../ScopeGateEmptyState.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string>) =>
        params ? `${key}:${JSON.stringify(params)}` : key,
      locale: { value: 'en' }
    })
  }
})

describe('ScopeGateEmptyState', () => {
  it('exposes all three recovery exits and emits select-project and switch-personal', async () => {
    const wrapper = mount(ScopeGateEmptyState, {
      props: {
        state: 'denied',
        surface: 'playground',
        workspaceId: '1',
        workspaceName: 'Default',
        projectId: '1',
        projectName: 'Default',
        requiredGrant: 'playground.run',
        projectOptions: [
          {
            workspaceId: '1',
            workspaceName: 'Default',
            projectId: '1',
            projectName: 'Default',
            isDefaultShared: true
          },
          {
            workspaceId: '12',
            workspaceName: 'Acme AI',
            projectId: '24',
            projectName: 'Core Product',
            isDefaultShared: false
          }
        ]
      }
    })

    expect(wrapper.get('[data-testid="scope-gate-empty-state"]').attributes('data-state')).toBe('denied')
    expect(wrapper.text()).toContain('playground.run')

    // Prefers non-default-shared team project by default
    const select = wrapper.get('[data-testid="scope-gate-project-select"]')
    expect((select.element as HTMLSelectElement).value).toBe('12:24')

    await wrapper.get('[data-testid="scope-gate-switch-project"]').trigger('click')
    expect(wrapper.emitted('select-project')).toEqual([[{ workspaceId: '12', projectId: '24' }]])

    await wrapper.get('[data-testid="scope-gate-switch-personal"]').trigger('click')
    expect(wrapper.emitted('switch-personal')).toHaveLength(1)

    await wrapper.get('[data-testid="scope-gate-request-access"]').trigger('click')
    expect(wrapper.emitted('request-access')).toHaveLength(1)
  })

  it('accepts custom request-access action via slot or component prop without importing FE-2 internals', async () => {
    const slotWrapper = mount(ScopeGateEmptyState, {
      props: {
        state: 'denied',
        surface: 'keys',
        workspaceId: '1',
        projectId: '1'
      },
      slots: {
        'request-access': '<button data-testid="custom-slot-request">Slot Request</button>'
      }
    })
    expect(slotWrapper.find('[data-testid="custom-slot-request"]').exists()).toBe(true)
    expect(slotWrapper.find('[data-testid="scope-gate-request-access"]').exists()).toBe(false)

    const CustomRequest = defineComponent({
      props: { workspaceId: String },
      setup(props) {
        return () => h('button', { 'data-testid': 'prop-component-request' }, `Req ${props.workspaceId}`)
      }
    })
    const propWrapper = mount(ScopeGateEmptyState, {
      props: {
        state: 'denied',
        surface: 'playground',
        workspaceId: '12',
        requestAccessComponent: CustomRequest,
        requestAccessProps: { workspaceId: '12' }
      }
    })
    expect(propWrapper.get('[data-testid="prop-component-request"]').text()).toBe('Req 12')
  })

  it('renders unavailable state with requestId and retry action', async () => {
    const wrapper = mount(ScopeGateEmptyState, {
      props: {
        state: 'unavailable',
        surface: 'keys',
        requestId: 'req-503-abc',
        httpStatus: 503,
        showRetry: true,
        showRequestAccess: false
      }
    })
    expect(wrapper.text()).toContain('req-503-abc')
    await wrapper.get('[data-testid="scope-gate-retry"]').trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
  })
})
