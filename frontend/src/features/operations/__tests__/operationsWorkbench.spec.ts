import { flushPromises, mount } from '@vue/test-utils'
import { reactive, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ScopedAuditView from '../ScopedAuditView.vue'
import OperationalSignals from '../OperationalSignals.vue'
import CatalogVerificationWorkbench from '../components/CatalogVerificationWorkbench.vue'
import { computeCatalogEntryId } from '../components/operationsHelpers'

const {
  mockGet,
  mockPost,
  mockPut,
  mockPatch,
  mockDelete,
  mockLocaleHolder,
  mockScopeState
} = vi.hoisted(() => ({
  mockGet: vi.fn(),
  mockPost: vi.fn(),
  mockPut: vi.fn(),
  mockPatch: vi.fn(),
  mockDelete: vi.fn(),
  mockLocaleHolder: {
    locale: { value: 'zh-CN' } as { value: string } | undefined
  },
  mockScopeState: {
    workspaceId: '10',
    projectId: '20'
  }
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get: mockGet,
    post: mockPost,
    put: mockPut,
    patch: mockPatch,
    delete: mockDelete
  },
  isSilentRequestError: () => false,
  buildGatewayUrl: (path: string) => path,
  buildApiUrl: (path: string) => path
}))

vi.mock('@/api/admin/ops', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/admin/ops')>()
  const getConcurrencyStats = vi.fn().mockResolvedValue({ enabled: false, platform: {} })
  const getRealtimeTrafficSummary = vi.fn().mockResolvedValue({ enabled: false, summary: null })
  return {
    ...actual,
    default: {
      ...(actual.default ?? {}),
      getConcurrencyStats,
      getRealtimeTrafficSummary
    },
    getConcurrencyStats,
    getRealtimeTrafficSummary
  }
})

vi.mock('@/stores/auth', () => ({
  useAuthStore: () =>
    reactive({
      isAuthenticated: true,
      sessionRevision: 1,
      user: { id: 1, role: 'admin' }
    })
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () =>
    reactive({
      activeScope: mockScopeState,
      scopeRevision: 1,
      scopesLoaded: true,
      scopes: [
        {
          workspace_id: '10',
          name: 'Core Workspace',
          projects: [{ id: '20', name: 'Gateway Project' }]
        }
      ],
      selectWorkspaceScope: (workspaceId: string, projectId: string) => {
        mockScopeState.workspaceId = workspaceId
        mockScopeState.projectId = projectId
      }
    })
}))

vi.mock('@/features/console/scope', () => {
  const scopes = ref([
    {
      workspace_id: '10',
      name: 'Core Workspace',
      projects: [{ id: '20', name: 'Gateway Project' }]
    }
  ])
  const activeWorkspaceId = ref('10')
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

vi.mock('vue-router', () => ({
  useRoute: () => ({
    path: '/audit',
    query: {}
  }),
  useRouter: () => ({
    push: vi.fn(),
    replace: vi.fn()
  })
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
      get locale() {
        return mockLocaleHolder.locale
      }
    })
  }
})

const globalMountOptions = {
  global: {
    stubs: {
      'router-link': true,
      AppLayout: {
        template: '<div class="app-layout-stub"><slot /></div>'
      }
    }
  }
}

describe('ScopedAuditView & OperationalSignals Workbench', () => {
  it('loads affected components from the public status resource for incident creation', async () => {
    mockGet.mockImplementation(async (path: string) => ({
      data: path === '/api/v2/public/status' ? { components: [{ id: 'inference', name: 'Model Inference' }] } : { items: [] }
    }))
    const wrapper = mount(OperationalSignals)
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text().includes('发布状态事件'))!.trigger('click')
    await flushPromises()
    expect(mockGet).toHaveBeenCalledWith('/api/v2/public/status', expect.any(Object))
    expect(document.body.textContent + wrapper.text()).toContain('Model Inference')
    wrapper.unmount()
  })

  beforeEach(() => {
    vi.clearAllMocks()
    mockLocaleHolder.locale = { value: 'zh-CN' }
    mockScopeState.workspaceId = '10'
    mockScopeState.projectId = '20'

    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (url: string) => {
        if (String(url).endsWith('/health')) {
          return {
            ok: true,
            status: 200,
            headers: new Headers({ 'content-type': 'application/json' }),
            json: async () => ({ status: 'ok' })
          }
        }
        return {
          ok: true,
          status: 200,
          headers: new Headers({ 'content-type': 'application/json' }),
          json: async () => ({})
        }
      })
    )
  })

  describe('ScopedAuditView retention & bilingual support', () => {
    it('loads project audit retention policy and saves validated retention periods', async () => {
      mockGet.mockImplementation(async (url: string) => {
        if (url === '/api/v2/me/scopes') {
          return {
            data: {
              items: [
                {
                  workspace_id: '10',
                  name: 'Core Workspace',
                  projects: [{ id: '20', name: 'Gateway Project' }]
                }
              ],
              next_cursor: null
            }
          }
        }
        if (url === '/api/v2/workspaces/10/projects/20/access') {
          return {
            data: {
              workspace_id: '10',
              project_id: '20',
              actions: ['audit.read', 'audit.retention.write'],
              expires_at: new Date(Date.now() + 60_000).toISOString()
            }
          }
        }
        if (url === '/api/v2/workspaces/10/projects/20/audit/retention') {
          return {
            data: {
              event_days: 90,
              trace_days: 7,
              content_hours: 24
            }
          }
        }
        if (url === '/api/v2/workspaces/10/projects/20/audit/events') {
          return {
            data: {
              items: [
                {
                  audit_event_id: '501',
                  workspace_id: '10',
                  project_id: '20',
                  actor_id: '1',
                  resource: 'retention',
                  resource_id: '20',
                  action: 'audit.retention.set',
                  effect: 'committed',
                  at: '2026-11-20T03:00:00Z'
                }
              ],
              as_of: '2026-11-20T03:01:00Z',
              next_cursor: null
            }
          }
        }
        return { data: {} }
      })

      mockPut.mockResolvedValueOnce({
        data: {
          event_days: 180,
          trace_days: 14,
          content_hours: 12
        }
      })

      const wrapper = mount(ScopedAuditView, globalMountOptions)
      await flushPromises()

      expect(mockGet).toHaveBeenCalledWith(
        '/api/v2/workspaces/10/projects/20/audit/retention',
        expect.objectContaining({ params: undefined })
      )

      const eventDaysInput = wrapper.get('#retention-event-days')
      const traceDaysInput = wrapper.get('#retention-trace-days')
      const contentHoursInput = wrapper.get('#retention-content-hours')

      expect((eventDaysInput.element as HTMLInputElement).value).toBe('90')
      expect((traceDaysInput.element as HTMLInputElement).value).toBe('7')
      expect((contentHoursInput.element as HTMLInputElement).value).toBe('24')

      // Out-of-range content_hours (>24) should block API call
      await contentHoursInput.setValue('36')
      await wrapper.get('[data-testid="audit-retention-panel"] form').trigger('submit')
      await flushPromises()
      expect(mockPut).not.toHaveBeenCalled()
      expect(wrapper.get('[data-testid="audit-retention-validation-error"]').text()).toContain('24')

      // Valid update
      await eventDaysInput.setValue('180')
      await traceDaysInput.setValue('14')
      await contentHoursInput.setValue('12')
      await wrapper.get('[data-testid="audit-retention-panel"] form').trigger('submit')
      await flushPromises()

      expect(mockPut).toHaveBeenCalledWith(
        '/api/v2/workspaces/10/projects/20/audit/retention',
        {
          event_days: 180,
          trace_days: 14,
          content_hours: 12
        },
        expect.objectContaining({ headers: undefined })
      )
      expect(wrapper.find('[data-testid="audit-retention-confirmed"]').exists()).toBe(true)
    })

    it('degrades gracefully on 403 retention read and handles undefined i18n locale in English fallback', async () => {
      mockLocaleHolder.locale = undefined
      mockGet.mockImplementation(async (url: string) => {
        if (url === '/api/v2/workspaces/10/projects/20/audit/retention') {
          return Promise.reject({
            response: {
              status: 403,
              data: { code: 403, reason: 'FORBIDDEN', message: 'Forbidden' }
            }
          })
        }
        if (url === '/api/v2/workspaces/10/projects/20/audit/events') {
          return { data: { items: [], as_of: '2026-11-20T03:00:00Z', next_cursor: null } }
        }
        return { data: { items: [], next_cursor: null } }
      })

      const wrapper = mount(ScopedAuditView, globalMountOptions)
      await flushPromises()

      expect(wrapper.text()).toContain('Project Audit & Retention')
      expect(wrapper.get('[data-testid="audit-retention-readonly"]').text()).toContain('Read-only')
    })
  })

  describe('WorkspaceTelemetryPanel collector & export lifecycle', () => {
    it('registers HTTPS destination, creates export pipeline, queries queue depth, updates, and revokes', async () => {
      const destinationId = '11111111-2222-4333-8444-555555555555'
      const exportId = 'aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee'

      mockPost.mockImplementation(async (url: string) => {
        if (url === '/api/v2/workspaces/10/observability/destinations') {
          return {
            data: {
              id: destinationId,
              state: 'registered',
              version: '1'
            }
          }
        }
        if (url === '/api/v2/workspaces/10/observability/exports') {
          return {
            data: {
              id: exportId,
              state: 'validating',
              version: '1',
              destination_id: destinationId
            }
          }
        }
        return { data: {} }
      })

      mockGet.mockImplementation(async (url: string) => {
        if (url === `/api/v2/workspaces/10/observability/exports/${exportId}`) {
          return {
            data: {
              id: exportId,
              state: 'active',
              version: '2',
              destination_id: destinationId,
              data_classes: ['timings'],
              sample_rate: '0.25',
              retention_days: 14,
              last_success_at: '2026-11-20T04:00:00Z',
              last_error_reason: null,
              queue_depth: 42
            }
          }
        }
        return { data: {} }
      })

      mockPatch.mockResolvedValueOnce({
        data: {
          id: exportId,
          state: 'disabled',
          version: '3',
          destination_id: destinationId
        }
      })

      mockDelete.mockResolvedValueOnce({
        data: {
          id: destinationId,
          state: 'revoked',
          version: '2'
        }
      })

      const wrapper = mount(OperationalSignals)
      await flushPromises()

      // Reject insecure HTTP collector first
      await wrapper.get('#telemetry-collector-endpoint').setValue('http://insecure.example.com/ingest')
      await wrapper.get('[data-testid="telemetry-register-destination"]').trigger('submit')
      await flushPromises()
      expect(mockPost).not.toHaveBeenCalledWith(
        '/api/v2/workspaces/10/observability/destinations',
        expect.anything(),
        expect.anything()
      )

      // 1. Register valid HTTPS collector
      await wrapper.get('#telemetry-collector-endpoint').setValue('https://collector.example.com/telemetry')
      await wrapper.get('[data-testid="telemetry-register-destination"]').trigger('submit')
      await flushPromises()

      expect(mockPost).toHaveBeenCalledWith(
        '/api/v2/workspaces/10/observability/destinations',
        { endpoint: 'https://collector.example.com/telemetry' },
        expect.objectContaining({
          headers: expect.objectContaining({
            'Idempotency-Key': expect.any(String)
          })
        })
      )

      // 2. Create export pipeline (destination auto-filled)
      await wrapper.get('#telemetry-export-sample-rate').setValue('0.25')
      await wrapper.get('#telemetry-export-retention-days').setValue('14')
      await wrapper.get('[data-testid="telemetry-create-export"]').trigger('submit')
      await flushPromises()

      expect(mockPost).toHaveBeenCalledWith(
        '/api/v2/workspaces/10/observability/exports',
        {
          destination_id: destinationId,
          data_classes: ['timings'],
          sample_rate: '0.25',
          retention_days: 14
        },
        expect.objectContaining({
          headers: expect.objectContaining({
            'Idempotency-Key': expect.any(String)
          })
        })
      )

      // 3. Query export detail & queue depth
      await wrapper.get('[data-testid="telemetry-query-export"]').trigger('submit')
      await flushPromises()

      expect(mockGet).toHaveBeenCalledWith(
        `/api/v2/workspaces/10/observability/exports/${exportId}`,
        expect.anything()
      )
      expect(wrapper.get('[data-testid="telemetry-export-detail"]').text()).toContain('42')

      // 4. Patch export pipeline
      await wrapper.get('#telemetry-patch-sample-rate').setValue('0.05')
      await wrapper.get('#telemetry-patch-enabled').trigger('click')
      await wrapper.get('[data-testid="telemetry-patch-export"]').trigger('submit')
      await flushPromises()

      expect(mockPatch).toHaveBeenCalledWith(
        `/api/v2/workspaces/10/observability/exports/${exportId}`,
        {
          sample_rate: '0.05',
          retention_days: 14,
          enabled: false
        },
        expect.objectContaining({
          headers: expect.objectContaining({
            'Idempotency-Key': expect.any(String),
            'If-Match': '"2"'
          })
        })
      )

      // 5. Revoke collector destination
      await wrapper.get('[data-testid="telemetry-revoke-destination"]').trigger('click')
      await flushPromises()

      expect(mockDelete).toHaveBeenCalledWith(
        `/api/v2/workspaces/10/observability/destinations/${destinationId}`,
        expect.objectContaining({
          headers: expect.objectContaining({
            'Idempotency-Key': expect.any(String),
            'If-Match': '"1"'
          })
        })
      )
    })

    it('shows ops.export.configure permission notice when collector registration returns 403', async () => {
      mockPost.mockRejectedValueOnce({
        response: {
          status: 403,
          data: { code: 403, reason: 'FORBIDDEN', message: 'Forbidden' }
        }
      })

      const wrapper = mount(OperationalSignals)
      await flushPromises()

      await wrapper.get('#telemetry-collector-endpoint').setValue('https://collector.example.com/telemetry')
      await wrapper.get('[data-testid="telemetry-register-destination"]').trigger('submit')
      await flushPromises()

      expect(wrapper.get('[data-testid="telemetry-permission-denied"]').text()).toContain('ops.export.configure')
    })
  })

  describe('CatalogVerificationWorkbench target, schedule, publication & probe enqueue', () => {
    it('queries target detail, updates schedule with If-Match, publishes compatibility, and enqueues probe', async () => {
      const targetId = 'vt_12345678-1234-4234-8234-1234567890ab'
      const entryId = computeCatalogEntryId('openai', 'gpt-4o-mini')
      const evidenceId = 've_abcdef12-3456-4789-8abc-def123456789'

      mockPost.mockImplementation(async (url: string) => {
        if (url === '/api/v2/admin/catalog/verification-targets') {
          return {
            data: {
              id: targetId,
              entry_id: entryId,
              provider: 'openai',
              model: 'gpt-4o-mini',
              connection_id: '101',
              owner_id: '1',
              adapter_version: '2026.1',
              action: 'text.chat',
              account_mode: 'api_key',
              protocol: 'openai_chat',
              interval_seconds: 0,
              schedule_version: '1',
              next_due_at: null,
              created_at: '2026-11-20T02:00:00Z'
            }
          }
        }
        if (url === `/api/v2/admin/catalog/verification-targets/${targetId}/publications`) {
          return {
            data: {
              target_id: targetId,
              entry_id: entryId,
              action: 'text.chat',
              protocol: 'openai_chat',
              account_mode: 'api_key',
              adapter_version: '2026.1',
              maturity: 'experimental',
              publication_version: '1',
              published_at: '2026-11-20T04:10:00Z',
              rollback_version: null,
              evidence: null,
              deprecation: null
            }
          }
        }
        if (url === '/api/v2/admin/catalog/verifications') {
          return {
            data: {
              operation_id: 'op_probe_9001',
              state: 'queued',
              status_url: '/api/v2/admin/operations/op_probe_9001',
              resource_id: entryId,
              result: null,
              error: null
            }
          }
        }
        return { data: {} }
      })

      mockGet.mockImplementation(async (url: string) => {
        if (url === `/api/v2/admin/catalog/verification-targets/${targetId}`) {
          return {
            data: {
              id: targetId,
              entry_id: entryId,
              provider: 'openai',
              model: 'gpt-4o-mini',
              connection_id: '101',
              owner_id: '1',
              adapter_version: '2026.1',
              action: 'text.chat',
              account_mode: 'api_key',
              protocol: 'openai_chat',
              interval_seconds: 0,
              schedule_version: '1',
              next_due_at: null,
              created_at: '2026-11-20T02:00:00Z',
              latest_attempt: {
                evidence_id: evidenceId,
                source: 'endpoint_probe',
                state: 'success',
                freshness: 'fresh',
                sample_count: '1',
                window_start: '2026-11-20T04:00:00Z',
                observed_at: '2026-11-20T04:00:05Z',
                verified_at: '2026-11-20T04:00:05Z',
                expires_at: '2026-11-21T04:00:05Z',
                fixture_id: 'fx_openai_chat',
                matrix: {
                  entry_id: entryId,
                  adapter_version: '2026.1',
                  action: 'text.chat',
                  account_mode: 'api_key',
                  protocol: 'openai_chat'
                },
                reason: ''
              },
              last_success: {
                evidence_id: evidenceId,
                source: 'endpoint_probe',
                state: 'success',
                freshness: 'fresh',
                sample_count: '1',
                window_start: '2026-11-20T04:00:00Z',
                observed_at: '2026-11-20T04:00:05Z',
                verified_at: '2026-11-20T04:00:05Z',
                expires_at: '2026-11-21T04:00:05Z',
                fixture_id: 'fx_openai_chat',
                matrix: {
                  entry_id: entryId,
                  adapter_version: '2026.1',
                  action: 'text.chat',
                  account_mode: 'api_key',
                  protocol: 'openai_chat'
                },
                reason: ''
              },
              publication: null
            }
          }
        }
        return { data: {} }
      })

      mockPut.mockResolvedValueOnce({
        data: {
          id: targetId,
          entry_id: entryId,
          provider: 'openai',
          model: 'gpt-4o-mini',
          connection_id: '101',
          owner_id: '1',
          adapter_version: '2026.1',
          action: 'text.chat',
          account_mode: 'api_key',
          protocol: 'openai_chat',
          interval_seconds: 3600,
          schedule_version: '2',
          next_due_at: '2026-11-20T05:00:00Z',
          created_at: '2026-11-20T02:00:00Z'
        }
      })

      const opsWrapper = mount(OperationalSignals)
      await flushPromises()
      expect(
        opsWrapper.get('[data-testid="ops-relocated-catalog-workbench-link"]').attributes('href')
      ).toBe('/admin/models')

      const wrapper = mount(CatalogVerificationWorkbench, {
        props: { isZh: true }
      })
      await flushPromises()

      // 1. Register verification target
      expect(wrapper.get('[data-testid="catalog-channel-setup"]').attributes('href')).toBe('/admin/channels/pricing')
      await wrapper.get('#catalog-rollout-workspace').setValue('2')
      expect(wrapper.get('[data-testid="catalog-channel-setup"]').attributes('href')).toBe('/workspaces/2/channels/pricing')
      await wrapper.get('#catalog-target-model').setValue('gpt-4o-mini')
      await wrapper.get('#catalog-target-connection-id').setValue('101')
      await wrapper.get('[data-testid="catalog-register-target"]').trigger('submit')
      await flushPromises()

      expect(mockPost).toHaveBeenCalledWith(
        '/api/v2/admin/catalog/verification-targets',
        {
          provider: 'openai',
          model: 'gpt-4o-mini',
          connection_id: '101',
          adapter_version: 'ad_openai_chat_responses_v1',
          action: 'text.chat',
          account_mode: 'api_key',
          protocol: 'openai_chat',
          interval_seconds: 0
        },
        expect.objectContaining({
          headers: expect.objectContaining({
            'Idempotency-Key': expect.any(String)
          })
        })
      )

      // 2. Query verification target detail
      await wrapper.get('[data-testid="catalog-query-target"]').trigger('submit')
      await flushPromises()

      expect(mockGet).toHaveBeenCalledWith(
        `/api/v2/admin/catalog/verification-targets/${targetId}`,
        expect.anything()
      )
      expect(wrapper.get('[data-testid="catalog-target-detail"]').text()).toContain(evidenceId)

      // 3. Update recurring verification schedule
      await wrapper.get('#catalog-schedule-interval').setValue('3600')
      await wrapper.get('[data-testid="catalog-save-schedule"]').trigger('submit')
      await flushPromises()

      expect(mockPut).toHaveBeenCalledWith(
        `/api/v2/admin/catalog/verification-targets/${targetId}/schedule`,
        { interval_seconds: 3600 },
        expect.objectContaining({
          headers: {
            'If-Match': '"1"'
          }
        })
      )

      // 4. Publish compatibility status (experimental with evidence_id)
      await wrapper.get('[data-testid="catalog-publish-compatibility"]').trigger('submit')
      await flushPromises()

      expect(mockPost).toHaveBeenCalledWith(
        `/api/v2/admin/catalog/verification-targets/${targetId}/publications`,
        {
          maturity: 'experimental',
          evidence_id: evidenceId
        },
        expect.objectContaining({
          headers: expect.objectContaining({
            'Idempotency-Key': expect.any(String),
            'If-Match': '"0"'
          })
        })
      )

      // 5. Enqueue immediate catalog probe
      await wrapper.get('[data-testid="catalog-enqueue-probe"]').trigger('submit')
      await flushPromises()

      expect(mockPost).toHaveBeenCalledWith(
        '/api/v2/admin/catalog/verifications',
        {
          entry_id: entryId,
          adapter_version: '2026.1',
          action: 'text.chat',
          account_mode: 'api_key',
          protocol: 'openai_chat'
        },
        expect.objectContaining({
          headers: expect.objectContaining({
            'Idempotency-Key': expect.any(String)
          })
        })
      )
      expect(wrapper.get('[data-testid="catalog-probe-operation"]').text()).toContain('op_probe_9001')
    })

    it('supports deprecated sunset notice and candidate rollback publication payloads', async () => {
      const targetId = 'vt_99999999-1234-4234-8234-1234567890ab'
      const entryId = computeCatalogEntryId('anthropic', 'claude-sonnet-4')

      mockPost.mockImplementation(async () => ({
        data: {
          target_id: targetId,
          entry_id: entryId,
          action: 'text.chat',
          protocol: 'anthropic_messages',
          account_mode: 'api_key',
          adapter_version: '2026.1',
          maturity: 'deprecated',
          publication_version: '3',
          published_at: '2026-11-20T05:00:00Z',
          rollback_version: null,
          evidence: null,
          deprecation: {
            reason: 'superseded',
            announced_at: '2026-11-20T05:00:00Z',
            sunset_at: '2027-06-01T00:00:00Z'
          }
        }
      }))

      const wrapper = mount(CatalogVerificationWorkbench, {
        props: { isZh: true }
      })
      await flushPromises()

      await wrapper.get('#catalog-target-lookup-id').setValue(targetId)
      await wrapper.get('#catalog-publish-version').setValue('2')

      const selects = wrapper.findAll('[data-testid="catalog-verification-workbench"] select')
      const maturitySelect = selects[selects.length - 1]
      await maturitySelect.setValue('deprecated')
      await flushPromises()

      const datetimeInput = wrapper.get('[data-testid="catalog-verification-workbench"] input[type="datetime-local"]')
      await datetimeInput.setValue('2027-06-01T08:00')
      await wrapper.get('[data-testid="catalog-publish-compatibility"]').trigger('submit')
      await flushPromises()

      expect(mockPost).toHaveBeenCalledWith(
        `/api/v2/admin/catalog/verification-targets/${targetId}/publications`,
        expect.objectContaining({
          maturity: 'deprecated',
          deprecation_reason: 'superseded',
          sunset_at: expect.any(String)
        }),
        expect.objectContaining({
          headers: expect.objectContaining({
            'Idempotency-Key': expect.any(String),
            'If-Match': '"2"'
          })
        })
      )

      // Switch to candidate rollback
      await maturitySelect.setValue('candidate')
      await flushPromises()
      await wrapper.get('#catalog-publish-rollback-version').setValue('0')
      await wrapper.get('[data-testid="catalog-publish-compatibility"]').trigger('submit')
      await flushPromises()

      expect(mockPost).toHaveBeenLastCalledWith(
        `/api/v2/admin/catalog/verification-targets/${targetId}/publications`,
        { rollback_version: '0' },
        expect.objectContaining({
          headers: expect.objectContaining({
            'Idempotency-Key': expect.any(String),
            'If-Match': '"3"'
          })
        })
      )
    })
  })
})
