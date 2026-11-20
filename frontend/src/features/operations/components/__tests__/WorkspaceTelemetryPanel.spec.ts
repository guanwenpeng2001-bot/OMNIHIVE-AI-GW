import { flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import WorkspaceTelemetryPanel from '../WorkspaceTelemetryPanel.vue'

const { mockGet, mockPost, mockPatch, mockDelete, mockActiveWorkspaceId, mockScopes } = vi.hoisted(
  () => ({
    mockGet: vi.fn(),
    mockPost: vi.fn(),
    mockPatch: vi.fn(),
    mockDelete: vi.fn(),
    mockActiveWorkspaceId: { value: '10' },
    mockScopes: [
      { workspace_id: '10', name: 'Alpha Workspace', projects: [{ id: '101', name: 'P1' }] },
      { workspace_id: '20', name: 'Beta Workspace', projects: [{ id: '201', name: 'P2' }] }
    ]
  })
)

vi.mock('@/api/client', () => ({
  apiClient: {
    get: mockGet,
    post: mockPost,
    patch: mockPatch,
    delete: mockDelete
  }
}))

vi.mock('@/features/console/scope', () => {
  const scopes = ref(mockScopes)
  const activeWorkspaceId = ref(mockActiveWorkspaceId.value)
  return {
    useConsoleScope: () => ({
      scopes,
      activeWorkspaceId,
      refreshScopes: vi.fn().mockResolvedValue(undefined),
      selectWorkspaceProject: (wsId: string) => {
        activeWorkspaceId.value = wsId
      }
    })
  }
})

describe('WorkspaceTelemetryPanel tenant isolation & honest health badge (S8 #2 & FE-FAKE-02)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
  })

  it('never reads or persists tenant destinations/exports in localStorage and clears state on workspace switch', async () => {
    const poisonedId = '99999999-9999-4999-8999-999999999999'
    localStorage.setItem(
      'omnihive.telemetry.registry.v1:10',
      JSON.stringify({
        destinations: [{ id: poisonedId, state: 'registered', version: '1' }],
        exports: [{ id: poisonedId, state: 'active', version: '1', destination_id: poisonedId }]
      })
    )
    const setItemSpy = vi.spyOn(Storage.prototype, 'setItem')

    const ws10DestId = '11111111-2222-4333-8444-555555555555'
    mockGet.mockImplementation(async (url: string) => {
      if (url === '/api/v2/workspaces/10/observability/destinations') {
        return { data: { items: [{ id: ws10DestId, state: 'registered', version: '1' }] } }
      }
      if (url === '/api/v2/workspaces/10/observability/exports') {
        return { data: { items: [] } }
      }
      if (url === '/api/v2/workspaces/20/observability/destinations') {
        return { data: { items: [] } }
      }
      if (url === '/api/v2/workspaces/20/observability/exports') {
        return { data: { items: [] } }
      }
      return { data: {} }
    })

    mockPost.mockResolvedValueOnce({
      data: {
        id: '22222222-3333-4444-8555-666666666666',
        state: 'registered',
        version: '1'
      }
    })

    const wrapper = mount(WorkspaceTelemetryPanel, {
      props: { isZh: false }
    })
    await flushPromises()

    // Must ignore pre-existing localStorage cache and display only server-loaded workspace 10 collector
    expect(wrapper.text()).not.toContain(poisonedId)
    expect(wrapper.text()).toContain(ws10DestId)

    // Registering a collector must NOT write tenant telemetry data into localStorage
    await wrapper
      .get('#telemetry-collector-endpoint')
      .setValue('https://collector.alpha.example.com/ingest')
    await wrapper.get('[data-testid="telemetry-register-destination"]').trigger('submit')
    await flushPromises()

    expect(setItemSpy).not.toHaveBeenCalled()

    // Switching workspace to 20 immediately clears workspace 10 collectors & exports
    const wsSelect = wrapper.get('select')
    await wsSelect.setValue('20')
    await flushPromises()

    expect(wrapper.text()).not.toContain(ws10DestId)
    expect(wrapper.text()).not.toContain('22222222-3333-4444-8555-666666666666')
    expect(mockGet).toHaveBeenCalledWith(
      '/api/v2/workspaces/20/observability/destinations',
      expect.anything()
    )
    setItemSpy.mockRestore()
  })

  it('renders unknown/unavailable telemetry health with neutral badge and never fakes green healthy', async () => {
    const exportId = 'aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee'
    const destId = '11111111-2222-4333-8444-555555555555'

    mockGet.mockImplementation(async (url: string) => {
      if (url.endsWith('/destinations') || url.endsWith('/exports')) {
        return { data: { items: [] } }
      }
      return { data: {} }
    })

    const wrapper = mount(WorkspaceTelemetryPanel, {
      props: { isZh: false }
    })
    await flushPromises()

    // Initial unmeasured state -> neutral unknown badge, never green healthy
    const initialBadge = wrapper.get('[data-testid="telemetry-health-badge"]')
    expect(initialBadge.attributes('data-health-state')).toBe('unknown')
    expect(initialBadge.text()).toBe('Unknown')
    expect(initialBadge.classes().join(' ')).not.toContain('ok')

    // Active export pipeline with NO observed delivery yet (last_success_at: null) -> remains unknown (neutral)
    mockGet.mockImplementationOnce(async () => ({
      data: {
        id: exportId,
        state: 'active',
        version: '1',
        destination_id: destId,
        data_classes: ['timings'],
        sample_rate: '0.1',
        retention_days: 7,
        last_success_at: null,
        last_error_reason: null,
        queue_depth: 0
      }
    }))

    await wrapper.get('#telemetry-export-lookup-id').setValue(exportId)
    await wrapper.get('[data-testid="telemetry-query-export"]').trigger('submit')
    await flushPromises()

    expect(wrapper.get('[data-testid="telemetry-health-badge"]').attributes('data-health-state')).toBe(
      'unknown'
    )
    expect(wrapper.get('[data-testid="telemetry-health-badge"]').text()).toBe('Unknown')

    // Only after a real observed delivery timestamp (last_success_at) does it transition to healthy
    mockGet.mockImplementationOnce(async () => ({
      data: {
        id: exportId,
        state: 'active',
        version: '2',
        destination_id: destId,
        data_classes: ['timings'],
        sample_rate: '0.1',
        retention_days: 7,
        last_success_at: '2026-11-20T12:00:00Z',
        last_error_reason: null,
        queue_depth: 0
      }
    }))

    await wrapper.get('[data-testid="telemetry-query-export"]').trigger('submit')
    await flushPromises()

    expect(wrapper.get('[data-testid="telemetry-health-badge"]').attributes('data-health-state')).toBe(
      'healthy'
    )
    expect(wrapper.get('[data-testid="telemetry-health-badge"]').text()).toBe('Healthy')
  })
})
