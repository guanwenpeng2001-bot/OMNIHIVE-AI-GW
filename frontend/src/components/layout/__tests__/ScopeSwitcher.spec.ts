import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, h, ref } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { identityApi, type IdentityWorkspacePage } from '@/api/identity'
import enConsoleWorkbench from '@/i18n/locales/en/consoleWorkbench'
import zhConsoleWorkbench from '@/i18n/locales/zh/consoleWorkbench'
import { useAuthStore } from '@/stores/auth'
import { requestScopeSwitcher, useConsoleScopeStore } from '@/stores/consoleScope'
import { resetScopeAccessCache } from '@/features/console/scopeAccess'
import ScopeSwitcher from '../ScopeSwitcher.vue'

const currentLocale = ref<'en' | 'zh'>('zh')

function resolveDictMessage(dict: Record<string, unknown>, key: string): string {
  let cur: unknown = dict
  for (const part of key.split('.')) {
    if (!cur || typeof cur !== 'object') return key
    cur = (cur as Record<string, unknown>)[part]
  }
  return typeof cur === 'string' ? cur : key
}

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      locale: currentLocale,
      t: (key: string) =>
        resolveDictMessage(
          (currentLocale.value === 'zh' ? zhConsoleWorkbench : enConsoleWorkbench) as unknown as Record<string, unknown>,
          key
        )
    })
  }
})

const EmptyRouteView = defineComponent({
  render: () => h('div')
})

function makeRouter(initialPath = '/dashboard') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/dashboard', component: EmptyRouteView },
      { path: '/keys', component: EmptyRouteView },
      { path: '/workspaces', component: EmptyRouteView },
      { path: '/workspaces/:workspaceId/keys', component: EmptyRouteView }
    ]
  })
  return { router, initialPath }
}
describe('ScopeSwitcher', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    setActivePinia(createPinia())
    resetScopeAccessCache()
    vi.restoreAllMocks()
    currentLocale.value = 'zh'

    const authStore = useAuthStore()
    authStore.token = 'tok-1'
    authStore.user = {
      id: 7,
      email: 'member@example.com',
      username: 'member',
      role: 'user',
      balance: 10,
      concurrency: 1,
      status: 'active',
      allowed_groups: [],
      created_at: '',
      updated_at: ''
    }
  })

  it('starts in Personal Space with PER badge, lists WS/PRJ/Shared Default/Archived options, and exposes request-access link', async () => {
    vi.spyOn(identityApi, 'listScopes').mockResolvedValueOnce({
      items: [
        {
          workspace_id: '1',
          name: 'Default Workspace',
          is_default: true,
          projects: [
            { id: '1', name: 'Default Project', is_default: true },
            { id: '2', name: 'Old Default', is_default: false }
          ],
          projects_next_cursor: null
        },
        {
          workspace_id: '12',
          name: 'Acme AI Team',
          is_default: false,
          projects: [
            { id: '24', name: 'Default', is_default: true },
            { id: '25', name: 'Archived Proj', state: 'archived', is_default: false }
          ],
          projects_next_cursor: null
        }
      ],
      next_cursor: null
    } as IdentityWorkspacePage)

    const { router, initialPath } = makeRouter('/keys')
    await router.push(initialPath)
    await router.isReady()

    const wrapper = mount(ScopeSwitcher, {
      global: {
        plugins: [router]
      }
    })
    await flushPromises()

    const trigger = wrapper.get('[data-testid="scope-switcher-trigger"]')
    expect(trigger.text()).toContain('PER')
    expect(trigger.text()).toContain('个人空间')

    requestScopeSwitcher()
    await flushPromises()
    expect(trigger.attributes('aria-expanded')).toBe('true')
    await trigger.trigger('keydown.esc')
    expect(useConsoleScopeStore().activeScope.mode).toBe('personal')
    await trigger.trigger('click')

    // Shared Default badge appears ONLY when BOTH ws.is_default && proj.is_default (1:1), not 12:24 named 'Default'
    const sharedProjOption = wrapper.get('[data-testid="scope-option-proj:1:1"]')
    expect(sharedProjOption.find('[data-testid="scope-option-default-shared"]').exists()).toBe(true)
    const acmeProjOption = wrapper.get('[data-testid="scope-option-proj:12:24"]')
    expect(acmeProjOption.find('[data-testid="scope-option-default-shared"]').exists()).toBe(false)

    // Archived project option is disabled
    const archivedOption = wrapper.get('[data-testid="scope-option-proj:12:25"]')
    expect(archivedOption.attributes('aria-disabled')).toBe('true')

    // Footer carries direct request project access action without importing FE-2 internals
    const requestLink = wrapper.get('[data-testid="scope-switcher-request-access"]')
    expect(requestLink.attributes('href')).toContain('/workspaces?requestAccess=1')

    // Selecting workspace overview keeps projectId: null and shows WS badge
    await wrapper.get('[data-testid="scope-option-ws:12"]').trigger('click')
    await flushPromises()
    const scopeStore = useConsoleScopeStore()
    expect(scopeStore.activeScope).toEqual({ mode: 'workspace', workspaceId: '12', projectId: null })
    expect(wrapper.get('[data-testid="scope-switcher-trigger"]').text()).toContain('WS')

    // Selecting project updates activeScope and shows PRJ badge
    await wrapper.get('[data-testid="scope-switcher-trigger"]').trigger('click')
    await wrapper.get('[data-testid="scope-option-proj:12:24"]').trigger('click')
    await flushPromises()
    expect(scopeStore.activeScope).toEqual({ mode: 'workspace', workspaceId: '12', projectId: '24' })
    expect(wrapper.get('[data-testid="scope-switcher-trigger"]').text()).toContain('PRJ')
  })

  it('strips workspace URL prefix and scope query params when switching back to Personal Space', async () => {
    currentLocale.value = 'en'
    vi.spyOn(identityApi, 'listScopes').mockResolvedValueOnce({
      items: [
        {
          workspace_id: '12',
          name: 'Acme AI Team',
          projects: [{ id: '24', name: 'Core Product' }],
          projects_next_cursor: null
        }
      ],
      next_cursor: null
    } as IdentityWorkspacePage)

    const { router } = makeRouter('/workspaces/12/keys')
    await router.push('/workspaces/12/keys?project_id=24')
    await router.isReady()

    const wrapper = mount(ScopeSwitcher, {
      global: {
        plugins: [router]
      }
    })
    await flushPromises()

    const scopeStore = useConsoleScopeStore()
    expect(scopeStore.activeScope).toEqual({ mode: 'workspace', workspaceId: '12', projectId: '24' })

    await wrapper.get('[data-testid="scope-switcher-trigger"]').trigger('click')
    await wrapper.get('[data-testid="scope-option-personal"]').trigger('click')
    await flushPromises()

    expect(scopeStore.activeScope).toEqual({ mode: 'personal', workspaceId: null, projectId: null })
    expect(router.currentRoute.value.fullPath).toBe('/keys')
  })
})
