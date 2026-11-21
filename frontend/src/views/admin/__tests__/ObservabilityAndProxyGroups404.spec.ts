import { defineComponent } from 'vue'
import { ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import ProxyGroupsView from '@/views/admin/ProxyGroupsView.vue'
import WorkspaceTelemetryPanel from '@/features/operations/components/WorkspaceTelemetryPanel.vue'

const { listProxyGroups, requestGet } = vi.hoisted(() => ({
  listProxyGroups: vi.fn(),
  requestGet: vi.fn()
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } })
  }
})

vi.mock('@/api/admin/proxyGroups', () => ({
  proxyGroupsAPI: {
    list: listProxyGroups,
    remove: vi.fn()
  }
}))

vi.mock('@/api/client', () => ({
  requestGet,
  requestPost: vi.fn(),
  requestPatch: vi.fn(),
  requestDelete: vi.fn(),
  isSilentRequestError: vi.fn(() => false)
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn(), showInfo: vi.fn() })
}))

vi.mock('@/features/console/scope', () => ({
  useConsoleScope: () => ({
    scopes: ref([{ workspace_id: '1', name: 'Default', projects: [{ id: '1', name: 'Default' }] }]),
    activeWorkspaceId: ref('1'),
    refreshScopes: vi.fn().mockResolvedValue(undefined),
    selectWorkspaceProject: vi.fn()
  })
}))

const AppLayoutStub = defineComponent({ template: '<main><slot /></main>' })
const TablePageLayoutStub = defineComponent({
  template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>'
})

describe('404 error states for ProxyGroupsView and WorkspaceTelemetryPanel (E3-5)', () => {
  beforeEach(() => {
    listProxyGroups.mockReset()
    requestGet.mockReset()
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  it('renders an explicit ErrorState with retry inside ProxyGroupsView table area on 404', async () => {
    listProxyGroups.mockRejectedValueOnce({
      response: { status: 404, data: { message: 'Not Found' } },
      message: 'Request failed with status code 404'
    })

    const wrapper = mount(ProxyGroupsView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          TablePageLayout: TablePageLayoutStub,
          Pagination: true,
          ConfirmDialog: true,
          ProxyGroupEditModal: true,
          ProxyGroupMembersModal: true,
          ProxyGroupBindingsDrawer: true,
          EgressPoolsPanel: true
        }
      }
    })
    await flushPromises()

    const tableWrapper = wrapper.get('.table-wrapper')
    expect(tableWrapper.text()).toContain('admin.proxyGroups.loadFailed')
    expect(tableWrapper.find('button').exists()).toBe(true)
  })

  it('marks workspace telemetry unavailable on 404 from observability destinations/exports', async () => {
    requestGet.mockRejectedValue({
      response: { status: 404, data: { message: 'Not Found' } },
      status: 404,
      message: 'Not Found'
    })

    const wrapper = mount(WorkspaceTelemetryPanel, {
      props: { isZh: false }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Unavailable')
    expect(wrapper.text()).not.toContain('Workspace Default (#1)Unknown')
  })
})
