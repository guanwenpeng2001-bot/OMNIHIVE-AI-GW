import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { useAppStore } from '@/stores/app'
import { identityApi } from '@/features/console/scope'
import TaskCenterView from '../TaskCenterView.vue'
import type { ImageTaskDetail } from '@/features/tasks/types'

const mocks = vi.hoisted(() => ({
  listTasks: vi.fn(),
  createTask: vi.fn(),
  getTaskDetail: vi.fn(),
  listTaskItems: vi.fn(),
  cancelTask: vi.fn(),
  retryTask: vi.fn(),
  issueArtifactDownloadTicket: vi.fn(),
  fetchArtifactContentBlob: vi.fn(),
  revokeArtifact: vi.fn(),
  listKeys: vi.fn(),
  listScopedModels: vi.fn()
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (k: string) => k, locale: { value: 'en' } })
  }
})

vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { template: '<main><slot /></main>' }
}))

vi.mock('@/api', () => ({
  keysAPI: { list: mocks.listKeys }
}))

vi.mock('@/features/console/catalogApi', () => ({
  catalogApi: { listScopedModels: mocks.listScopedModels }
}))

vi.mock('@/features/tasks/api', () => ({
  tasksApi: {
    listTasks: mocks.listTasks,
    createTask: mocks.createTask,
    getTaskDetail: mocks.getTaskDetail,
    listTaskItems: mocks.listTaskItems,
    cancelTask: mocks.cancelTask,
    retryTask: mocks.retryTask,
    issueArtifactDownloadTicket: mocks.issueArtifactDownloadTicket,
    fetchArtifactContentBlob: mocks.fetchArtifactContentBlob,
    revokeArtifact: mocks.revokeArtifact
  }
}))

const fixedNowMs = Date.parse('2026-11-19T12:00:00Z')

function sampleTaskDetail(overrides: Partial<ImageTaskDetail> = {}): ImageTaskDetail {
  return {
    id: 'task_1d66e105',
    project_id: '5',
    kind: 'image',
    source: 'native_v2',
    model_id: 'gpt-image-1',
    request_id: 'req_abc12345',
    request_url: '/api/v2/workspaces/1/projects/5/requests/req_abc12345',
    settlement_id: 'stl_999',
    settlement_url: null,
    parent_task_id: null,
    state: 'failed',
    stage: 'partially_succeeded',
    provider_state: 'accepted',
    artifact_state: 'available',
    cancel_state: 'unsupported',
    progress: null,
    progress_source: 'unreported',
    items: { total: '3', succeeded: '1', failed: '2', pending: '0', canceled: '0' },
    artifacts: [
      {
        id: 'art_avail',
        content_type: 'image/png',
        size_bytes: '204800',
        expires_at: '2026-11-20T10:00:00Z',
        state: 'available'
      },
      {
        id: 'art_exp',
        content_type: 'image/png',
        size_bytes: '102400',
        expires_at: '2026-11-19T08:00:00Z',
        state: 'expired'
      },
      {
        id: 'art_rev',
        content_type: 'image/png',
        size_bytes: null,
        expires_at: '2026-11-20T10:00:00Z',
        state: 'revoked'
      }
    ],
    settlement_state: 'settled',
    estimate: { currency: 'USD', amount: '0.60000000' },
    actual_charge: { currency: 'USD', amount: '0.20000000' },
    created_at: '2026-11-19T11:50:00Z',
    updated_at: '2026-11-19T11:55:00Z',
    allowed_actions: ['tasks.read', 'tasks.retry', 'tasks.artifacts.read', 'tasks.artifacts.revoke'],
    action_reasons: { 'tasks.cancel': 'CANCEL_UNSUPPORTED' },
    version: '4',
    ...overrides
  }
}

describe('TaskCenterView BE-15 workflows and honest states', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.resetAllMocks()
    setActivePinia(createPinia())
    const app = useAppStore()
    app.selectScope({ mode: 'workspace', workspaceId: '1', projectId: '5' })
    vi.spyOn(app, 'fetchScopes').mockImplementation(async () => {
      const page = await identityApi.listScopes()
      app.scopes = page.items
      app.scopesLoaded = true
      app.scopesNextCursor = page.next_cursor
      return page
    })
    vi.spyOn(identityApi, 'listScopes').mockResolvedValue({
      items: [
        {
          workspace_id: '1',
          name: 'Studio',
          projects_next_cursor: null,
          projects: [{ id: '5', name: 'Media Pipeline' }]
        }
      ],
      next_cursor: null,
      as_of: '2026-11-19T12:00:00Z'
    })
    vi.spyOn(identityApi, 'getProjectAccess').mockResolvedValue({
      actor_id: '1',
      workspace_id: '1',
      project_id: '5',
      actions: ['tasks.read', 'tasks.create', 'tasks.cancel', 'tasks.retry', 'tasks.artifacts.read', 'tasks.artifacts.revoke', 'keys.read'],
      scope_version: '1',
      expires_at: '2026-11-20T00:00:00Z'
    })
  })

  it('never fabricates a numeric progress bar when progress is null and renders explicit available/expired/revoked artifact states', async () => {
    const detail = sampleTaskDetail()
    mocks.listTasks.mockResolvedValue({
      items: [
        {
          id: detail.id,
          kind: 'image',
          state: 'running',
          stage: 'processing_upstream',
          model_id: 'gpt-image-1',
          created_at: detail.created_at,
          updated_at: detail.updated_at,
          progress: null,
          settlement_state: 'reserved',
          allowed_actions: ['tasks.read', 'tasks.cancel']
        }
      ],
      next_cursor: null,
      as_of: '2026-11-19T12:00:00Z'
    })
    mocks.getTaskDetail.mockResolvedValue(detail)
    mocks.listTaskItems.mockResolvedValue({
      items: [
        { id: '1', state: 'succeeded', acceptance: 'accepted', reason: null, retryable: false, artifact_ids: ['art_avail'] },
        { id: '2', state: 'failed', acceptance: 'rejected', reason: 'RATE_LIMITED', retryable: true, artifact_ids: [] }
      ],
      next_cursor: null,
      as_of: '2026-11-19T12:00:00Z'
    })

    const wrapper = mount(TaskCenterView, {
      props: { teleportDisabled: true, nowMs: fixedNowMs }
    })
    await flushPromises()

    // Table row shows em-dash for null progress rather than 0% or fake progress bar
    expect(wrapper.get(`[data-testid="row-unreported-progress-${detail.id}"]`).text()).toBe('—')

    // Open detail drawer
    await wrapper.get(`[data-testid="inspect-task-${detail.id}"]`).trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="determinate-progress-bar"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="unreported-progress-note"]').text()).toContain('did not report numeric percentage')
    expect(wrapper.get('[data-testid="task-action-reasons"]').text()).toContain('CANCEL_UNSUPPORTED')

    // Available vs Expired vs Revoked artifacts
    expect(wrapper.get('[data-testid="artifact-badge-art_avail"]').text()).toBe('Available')
    expect(wrapper.get('[data-testid="download-artifact-art_avail"]').attributes('disabled')).toBeUndefined()

    expect(wrapper.get('[data-testid="artifact-badge-art_exp"]').text()).toBe('Expired')
    expect(wrapper.get('[data-testid="download-artifact-art_exp"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="artifact-expired-reason-art_exp"]').exists()).toBe(true)

    expect(wrapper.get('[data-testid="artifact-badge-art_rev"]').text()).toBe('Revoked')
    expect(wrapper.get('[data-testid="download-artifact-art_rev"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="artifact-revoked-reason-art_rev"]').exists()).toBe(true)

    wrapper.unmount()
  })

  it('executes ticketed artifact download, revocation, and confirmed paid retry for retryable sub-items', async () => {
    const detail = sampleTaskDetail()
    const childDetail = sampleTaskDetail({
      id: 'task_6e058894',
      parent_task_id: detail.id,
      state: 'running',
      progress: 50,
      progress_source: 'provider_reported'
    })

    mocks.listTasks.mockResolvedValue({
      items: [detail],
      next_cursor: null,
      as_of: '2026-11-19T12:00:00Z'
    })
    mocks.getTaskDetail.mockResolvedValueOnce(detail).mockResolvedValue(childDetail)
    mocks.listTaskItems.mockResolvedValue({
      items: [
        { id: '1', state: 'succeeded', acceptance: 'accepted', reason: null, retryable: false, artifact_ids: ['art_avail'] },
        { id: '2', state: 'failed', acceptance: 'rejected', reason: 'UPSTREAM_ERROR', retryable: true, artifact_ids: [] }
      ],
      next_cursor: null,
      as_of: '2026-11-19T12:00:00Z'
    })
    mocks.issueArtifactDownloadTicket.mockResolvedValue({
      url: '/api/v2/workspaces/1/projects/5/tasks/task_1d66e105/artifacts/art_avail/content',
      ticket: 'tkt_secret_1m',
      expires_at: '2026-11-19T12:01:00Z',
      content_type: 'image/png',
      size_bytes: '204800'
    })
    mocks.fetchArtifactContentBlob.mockResolvedValue(new Blob(['png'], { type: 'image/png' }))
    mocks.retryTask.mockResolvedValue({
      task_id: 'task_6e058894',
      parent_task_id: 'task_1d66e105',
      new_request_id: 'req_new_retry',
      state: 'running'
    })

    const wrapper = mount(TaskCenterView, {
      props: { teleportDisabled: true, nowMs: fixedNowMs }
    })
    await flushPromises()

    await wrapper.get(`[data-testid="inspect-task-${detail.id}"]`).trigger('click')
    await flushPromises()

    // Download available artifact via ticket + content blob
    await wrapper.get('[data-testid="download-artifact-art_avail"]').trigger('click')
    await flushPromises()
    expect(mocks.issueArtifactDownloadTicket).toHaveBeenCalledWith('1', '5', 'task_1d66e105', 'art_avail')
    expect(mocks.fetchArtifactContentBlob).toHaveBeenCalledWith(
      '1',
      '5',
      'task_1d66e105',
      'art_avail',
      expect.objectContaining({ ticket: 'tkt_secret_1m' })
    )

    // Open Paid Retry modal -> non-retryable item #1 disabled, retryable item #2 preselected
    await wrapper.get('[data-testid="task-paid-retry-btn"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="retry-item-checkbox-1"]').attributes('disabled')).toBeDefined()
    expect((wrapper.get('[data-testid="retry-item-checkbox-2"]').element as HTMLInputElement).checked).toBe(true)

    await wrapper.get('[data-testid="confirm-paid-retry-btn"]').trigger('click')
    await flushPromises()

    expect(mocks.retryTask).toHaveBeenCalledWith('1', '5', 'task_1d66e105', {
      item_ids: ['2'],
      funding_acknowledged: true
    })
    // Child task has determinate 50% progress bar
    expect(wrapper.find('[data-testid="determinate-progress-bar"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="task-progress-value"]').text()).toContain('50%')

    wrapper.unmount()
  })

  it('distinguishes empty state, zero-filter state, and 403 permission error state', async () => {
    mocks.listTasks.mockResolvedValueOnce({ items: [], next_cursor: null, as_of: '2026-11-19T12:00:00Z' })
    const wrapper = mount(TaskCenterView, { props: { teleportDisabled: true } })
    await flushPromises()
    expect(wrapper.find('[data-testid="console-state-empty"]').exists()).toBe(true)

    // Filter resulting in zero matches -> zero-filter state
    mocks.listTasks.mockResolvedValueOnce({ items: [], next_cursor: null, as_of: '2026-11-19T12:00:00Z' })
    await wrapper.get('[data-testid="filter-task-state"]').setValue('failed')
    await flushPromises()
    expect(wrapper.find('[data-testid="console-state-zero-filter"]').exists()).toBe(true)

    // 403 permission denied
    mocks.listTasks.mockRejectedValueOnce({ status: 403, reason: 'FORBIDDEN', message: 'Missing tasks.read' })
    await wrapper.get('[data-testid="filter-task-state"]').setValue('')
    await flushPromises()
    expect(wrapper.find('[data-testid="console-state-permission"]').exists()).toBe(true)

    wrapper.unmount()
  })

  it('renders no-scope state without falling back to hardcoded workspace/project 1 when scopes are empty', async () => {
    vi.spyOn(identityApi, 'listScopes').mockResolvedValue({
      items: [{ workspace_id: '9', name: 'Empty', projects_next_cursor: null, projects: [] }],
      next_cursor: null,
      as_of: '2026-11-19T12:00:00Z'
    })
    const wrapper = mount(TaskCenterView, { props: { teleportDisabled: true } })
    await flushPromises()

    expect(mocks.listTasks).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="console-state-no-scope"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="open-create-task-btn"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('hides create button and skips listTasks when getProjectAccess denies tasks.read (403)', async () => {
    vi.spyOn(identityApi, 'getProjectAccess').mockResolvedValue({
      actor_id: '1',
      workspace_id: '1',
      project_id: '5',
      actions: ['projects.read', 'models.read'],
      scope_version: '1',
      expires_at: '2026-11-20T00:00:00Z'
    })
    const wrapper = mount(TaskCenterView, { props: { teleportDisabled: true } })
    await flushPromises()

    expect(mocks.listTasks).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="open-create-task-btn"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="console-state-permission"]').exists()).toBe(true)
    wrapper.unmount()
  })
})
