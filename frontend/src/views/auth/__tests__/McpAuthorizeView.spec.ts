import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { reactive } from 'vue'
import McpAuthorizeView from '../McpAuthorizeView.vue'

const routeState = reactive({
  fullPath: '/oauth/mcp/authorize',
  query: {} as Record<string, string>,
})

vi.mock('vue-router', () => ({
  useRoute: () => routeState,
  useRouter: () => ({ push: vi.fn() }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    user: { id: 1, role: 'user' },
    isAdmin: false,
  }),
}))

vi.mock('@/components/layout/AuthLayout.vue', () => ({
  default: { template: '<div><slot /></div>' },
}))

describe('McpAuthorizeView missing client context guard (FL-04)', () => {
  beforeEach(() => {
    routeState.fullPath = '/oauth/mcp/authorize'
    routeState.query = {}
  })

  it('shows an explicit error and never renders demo client details when query context is missing', () => {
    const wrapper = mount(McpAuthorizeView)

    const alert = wrapper.get('[role="alert"]')
    expect(alert.text()).toContain('缺少必要参数')
    expect(wrapper.text()).not.toContain('Claude Desktop MCP')
    expect(wrapper.text()).not.toContain('claude-desktop-mcp')
    expect(wrapper.text()).not.toContain('127.0.0.1:43110')
    expect(wrapper.find('form').exists()).toBe(false)
  })
})
