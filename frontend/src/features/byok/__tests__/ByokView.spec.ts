import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ByokView from '@/views/user/ByokView.vue'
import { formatIfMatchHeader } from '@/features/byok/api'
import { identityApi } from '@/api/identity'
import { useConsoleScopeStore } from '@/stores/consoleScope'
import { resetScopeAccessCache } from '@/features/console/scopeAccess'

const {
  listWorkspaceCredentials,
  listWorkspaceClasses,
  verifyWorkspaceCredential,
  getWorkspaceCredential,
  patchWorkspaceCredential,
  listProjectCredentials,
  listProjectClasses
} = vi.hoisted(() => ({
  listWorkspaceCredentials: vi.fn(),
  listWorkspaceClasses: vi.fn(),
  verifyWorkspaceCredential: vi.fn(),
  getWorkspaceCredential: vi.fn(),
  patchWorkspaceCredential: vi.fn(),
  listProjectCredentials: vi.fn(),
  listProjectClasses: vi.fn()
}))

vi.mock('@/features/byok/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/features/byok/api')>()
  return {
    ...actual,
    byokApi: {
      ...actual.byokApi,
      listWorkspaceCredentials,
      listWorkspaceClasses,
      verifyWorkspaceCredential,
      getWorkspaceCredential,
      patchWorkspaceCredential,
      listProjectCredentials,
      listProjectClasses
    }
  }
})

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    isAuthenticated: true,
    user: { id: 1 }
  })
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() })
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (k: string) => k,
      locale: ref('zh-CN')
    })
  }
})

const sampleCredential = {
  id: '12',
  owner_workspace_id: '1',
  auth_type: 'api_key' as const,
  provider: 'openai',
  version: '3',
  credential_version: '2',
  state: 'unverified' as const,
  fingerprint: 'sha256:98ab34cd',
  verified_model_ids: [],
  verified_until: null,
  scope: {
    project_ids: ['10'],
    service_account_ids: [],
    member_ids: ['1'],
    key_ids: ['100'],
    model_ids: ['gpt-5.6-sol']
  },
  priority: 10,
  allow_platform_fallback: false,
  include_external_in_budget: false
}

function mountByok() {
  return mount(ByokView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        ByokCredentialFormModal: true,
        ByokInspectorDrawer: true
      }
    }
  })
}

describe('ByokView & If-Match contract', () => {
  function futureISO(offsetMs = 3600_000): string {
    return new Date(Date.now() + offsetMs).toISOString()
  }

  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    setActivePinia(createPinia())
    resetScopeAccessCache()
    vi.resetAllMocks()
    vi.spyOn(identityApi, 'listScopes').mockResolvedValue({
      items: [
        {
          workspace_id: '1',
          workspace_name: 'Core Team',
          workspace_state: 'active',
          projects: [{ id: '10', name: 'Production', state: 'active' }],
          projects_next_cursor: null
        }
      ],
      next_cursor: null,
      as_of: '2026-11-20T00:00:00Z'
    })
    vi.spyOn(identityApi, 'getWorkspaceAccess').mockResolvedValue({
      actor_id: '1',
      workspace_id: '1',
      actions: ['credentials.byok.manage'],
      effective_admin: true,
      role_source: 'inherited',
      scope_version: '1',
      expires_at: futureISO()
    })
    vi.spyOn(identityApi, 'listProjectKeys').mockResolvedValue({
      items: [
        {
          id: '100',
          project_id: '10',
          name: 'Prod Key',
          prefix: 'oh_live_100',
          owner_id: '1',
          group_id: '7',
          state: 'active',
          created_at: '2026-11-20T00:00:00Z',
          version: '1'
        }
      ],
      next_cursor: null
    })
    vi.spyOn(identityApi, 'getProjectAccess').mockResolvedValue({
      actor_id: '1',
      workspace_id: '1',
      project_id: '10',
      actions: ['credentials.byok.manage', 'keys.read'],
      scope_version: '1',
      expires_at: futureISO()
    })
    listWorkspaceClasses.mockResolvedValue({
      items: [{ provider: 'openai', auth_type: 'api_key', supported: true, enabled: true }]
    })
    listProjectClasses.mockResolvedValue({
      items: [{ provider: 'openai', auth_type: 'api_key', supported: true, enabled: true }]
    })
    listWorkspaceCredentials.mockResolvedValue({ items: [sampleCredential], next_cursor: null })
    listProjectCredentials.mockResolvedValue({ items: [], next_cursor: null })
    useConsoleScopeStore().selectWorkspaceScope('1', '10')
  })

  it('formats If-Match header with strict quoted version', () => {
    expect(formatIfMatchHeader('3')).toEqual({ 'If-Match': '"3"' })
    expect(formatIfMatchHeader('"7"')).toEqual({ 'If-Match': '"7"' })
  })

  it('renders workspace credentials, provider classes, and executes verify probe', async () => {
    verifyWorkspaceCredential.mockResolvedValue({
      operation_id: 'op-1',
      state: 'succeeded',
      status_url: '/api/v2/workspaces/1/operations/op-1',
      resource_id: '12',
      result: null,
      error: null
    })
    getWorkspaceCredential.mockResolvedValue({
      ...sampleCredential,
      version: '4',
      state: 'verified',
      verified_model_ids: ['gpt-5.6-sol']
    })

    const wrapper = mountByok()
    await flushPromises()

    expect(listWorkspaceCredentials).toHaveBeenCalledWith('1', expect.any(Object))
    expect(wrapper.find('[data-testid="byok-provider-classes"]').text()).toContain('openai')
    expect(wrapper.find('[data-testid="byok-credential-card"]').text()).toContain('sha256:98ab34cd')

    await wrapper.get('[data-testid="byok-verify-btn"]').trigger('click')
    await flushPromises()

    expect(verifyWorkspaceCredential).toHaveBeenCalledWith('1', '12', '3')
    expect(wrapper.find('[data-testid="byok-credential-card"]').text()).toContain('已验证可用')
  })

  it('displays 403 permission-denied banner with credentials.byok.manage grant', async () => {
    listWorkspaceCredentials.mockRejectedValue({
      status: 403,
      reason: 'INSUFFICIENT_GRANT',
      message: 'Missing BYOK grant'
    })

    const wrapper = mountByok()
    await flushPromises()

    const banner = wrapper.get('[data-testid="console-state-permission"]')
    expect(banner.text()).toContain('credentials.byok.manage')
  })

  it('surfaces 409 version conflict banner and reloads latest state on retry', async () => {
    patchWorkspaceCredential.mockRejectedValue({
      status: 409,
      reason: 'VERSION_CONFLICT',
      message: 'Stale revision'
    })

    const wrapper = mountByok()
    await flushPromises()

    const switches = wrapper.findAll('button[role="switch"]')
    await switches[0].trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="console-state-conflict"]').exists()).toBe(true)
    listWorkspaceCredentials.mockClear()
    await wrapper.get('[data-testid="console-state-retry"]').trigger('click')
    await flushPromises()

    expect(listWorkspaceCredentials).toHaveBeenCalledTimes(1)
  })

  it('disables enroll button and hides project service account panel when credentials.byok.manage is absent', async () => {
    vi.spyOn(identityApi, 'getProjectAccess').mockResolvedValue({
      actor_id: '1',
      workspace_id: '1',
      project_id: '10',
      actions: ['projects.read', 'models.read'],
      scope_version: '1',
      expires_at: futureISO()
    })

    const wrapper = mountByok()
    await flushPromises()

    const projectTab = wrapper.findAll('button[role="tab"]')[1]
    await projectTab.trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="byok-enroll-btn"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="scope-gate-empty-state"]').attributes('data-state')).toBe('denied')
    expect(wrapper.find('byok-service-accounts-panel-stub').exists()).toBe(false)
    expect(listProjectCredentials).not.toHaveBeenCalled()
  })

  it('fails closed on workspace-tab missing grant or 503 probe and shows ScopeGateEmptyState in Personal Space', async () => {
    resetScopeAccessCache()
    vi.spyOn(identityApi, 'getWorkspaceAccess').mockResolvedValueOnce({
      actor_id: '1',
      workspace_id: '1',
      actions: ['projects.read'],
      scope_version: '1',
      expires_at: futureISO()
    })
    const deniedWs = mountByok()
    await flushPromises()
    expect(deniedWs.get('[data-testid="scope-gate-empty-state"]').attributes('data-state')).toBe('denied')
    expect(deniedWs.get('[data-testid="byok-enroll-btn"]').attributes('disabled')).toBeDefined()
    expect(listWorkspaceCredentials).not.toHaveBeenCalled()
    deniedWs.unmount()

    resetScopeAccessCache()
    vi.spyOn(identityApi, 'getWorkspaceAccess').mockRejectedValueOnce(
      Object.assign(new Error('Service Unavailable'), { status: 503, metadata: { request_id: 'req-byok503' } })
    )
    const errWs = mountByok()
    await flushPromises()
    expect(errWs.get('[data-testid="scope-gate-empty-state"]').attributes('data-state')).toBe('unavailable')
    expect(listWorkspaceCredentials).not.toHaveBeenCalled()
    errWs.unmount()

    useConsoleScopeStore().selectPersonalScope()
    const personalWrapper = mountByok()
    await flushPromises()
    expect(personalWrapper.get('[data-testid="scope-gate-empty-state"]').attributes('data-state')).toBe('empty')
    personalWrapper.unmount()
  })
})
