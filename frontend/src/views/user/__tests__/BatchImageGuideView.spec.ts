import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'

import BatchImageGuideView from '../BatchImageGuideView.vue'
import type { ApiKey } from '@/types'
import type { BatchImageJob } from '@/api/batchImage'

const POLL_INTERVAL_MS = 8000

const batchImageApi = vi.hoisted(() => ({
  submitBatchImageJob: vi.fn(),
  getBatchImageJob: vi.fn(),
  listBatchImageJobs: vi.fn(),
  listBatchImageItems: vi.fn(),
  listBatchImageModels: vi.fn(),
  cancelBatchImageJob: vi.fn(),
  deleteBatchImageJobRecord: vi.fn(),
  downloadBatchImageZip: vi.fn(),
  getBatchImageItemContent: vi.fn(),
  saveBlob: vi.fn(),
}))

const keysApi = vi.hoisted(() => ({ list: vi.fn() }))

const appStoreMock = vi.hoisted(() => ({
  showError: vi.fn(),
  showSuccess: vi.fn(),
  showInfo: vi.fn(),
  fetchPublicSettings: vi.fn(),
}))

// The view resolves every user-facing string through batchImageText(), so the
// spec keeps its own message table and asserts against the same table.
const i18nMock = vi.hoisted(() => {
  const messages: Record<string, string> = {
    'common.cancel': 'Cancel',
    'common.add': 'Add',
    'common.delete': 'Delete',
    'common.view': 'View',
    'common.more': 'More',
    'common.copy': 'Copy',
    'common.status': 'Status',
    'common.actions': 'Actions',
    'batchImage.actions.createJob': 'Create job',
    'batchImage.actions.submitJob': 'Submit job',
    'batchImage.actions.cancelJob': 'Cancel job',
    'batchImage.actions.usageGuide': 'Usage guide',
    'batchImage.actions.downloadZip': 'Download zip',
    'batchImage.actions.download': 'Download',
    'batchImage.actions.retryFailedItems': 'Retry failed',
    'batchImage.actions.deleteRecords': 'Delete records',
    'batchImage.messages.submitted': 'Job submitted',
    'batchImage.messages.submitFailed': 'Submit failed',
    'batchImage.messages.cancelConfirm': 'Cancel this job?',
    'batchImage.messages.cancelled': 'Job cancelled',
    'batchImage.messages.cancelFailed': 'Cancel failed',
    'batchImage.messages.refreshFailed': 'Refresh failed',
    'batchImage.messages.loadJobsFailed': 'Load jobs failed',
    'batchImage.messages.loadKeysFailed': 'Load keys failed',
    'batchImage.messages.loadModelsFailed': 'Load models failed',
    'batchImage.messages.selectApiKey': 'Select an API key',
    'batchImage.messages.promptRequired': 'A prompt is required',
    'batchImage.messages.selectModel': 'Select a model',
    'batchImage.messages.noModelsForKey': 'No models for this key',
    'batchImage.messages.tooManyOutputImages': 'Too many output images',
    'batchImage.messages.tooManyReferenceImages': 'Too many reference images',
    'batchImage.messages.adminReference': '(admin reference)',
    'batchImage.messages.errorReference': 'Reference',
    'batchImage.messages.errorCodeRef': 'code {code}',
    'batchImage.messages.requestIdRef': 'request {id}',
    'batchImage.messages.httpStatusRef': 'http {status}',
    'batchImage.create.title': 'Create batch job',
    'batchImage.create.taskName': 'Task name',
  }
  const translate = (key: string) => messages[key] ?? key
  return { messages, translate }
})

vi.mock('@/api', () => ({ keysAPI: { list: keysApi.list } }))

vi.mock('@/api/batchImage', () => ({
  cancelBatchImageJob: batchImageApi.cancelBatchImageJob,
  deleteBatchImageJobRecord: batchImageApi.deleteBatchImageJobRecord,
  downloadBatchImageZip: batchImageApi.downloadBatchImageZip,
  getBatchImageItemContent: batchImageApi.getBatchImageItemContent,
  getBatchImageJob: batchImageApi.getBatchImageJob,
  listBatchImageJobs: batchImageApi.listBatchImageJobs,
  listBatchImageItems: batchImageApi.listBatchImageItems,
  listBatchImageModels: batchImageApi.listBatchImageModels,
  saveBlob: batchImageApi.saveBlob,
  submitBatchImageJob: batchImageApi.submitBatchImageJob,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: appStoreMock.showError,
    showSuccess: appStoreMock.showSuccess,
    showInfo: appStoreMock.showInfo,
    fetchPublicSettings: appStoreMock.fetchPublicSettings,
    apiBaseUrl: '',
  }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copied: { value: false }, copyToClipboard: vi.fn() }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: i18nMock.translate, locale: { value: 'en' } }),
  }
})

const AppLayoutStub = {
  name: 'AppLayout',
  template: '<div><slot /></div>',
}

const TablePageLayoutStub = {
  name: 'TablePageLayout',
  template: `
    <div>
      <slot name="filters" />
      <slot name="actions" />
      <slot name="table" />
      <slot name="pagination" />
    </div>
  `,
}

const DataTableStub = {
  name: 'DataTable',
  props: ['columns', 'data', 'loading', 'expandableActions', 'rowKey'],
  template: `
    <div>
      <div v-for="row in data" :key="row.id" :data-test="'job-row-' + row.id">
        <slot name="cell-id" :value="row.id" :row="row" />
        <slot name="cell-status" :value="row.status" :row="row" />
      </div>
    </div>
  `,
}

const BaseDialogStub = {
  name: 'BaseDialog',
  props: ['show', 'title', 'width', 'zIndex'],
  emits: ['close'],
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
}

const IconStub = {
  name: 'Icon',
  props: ['name', 'size'],
  template: '<span />',
}

const geminiApiKey = (): ApiKey => ({
  id: 7,
  user_id: 1,
  key: 'sk-gemini-batch-key',
  name: 'Gemini batch key',
  group_id: 3,
  status: 'active',
  ip_whitelist: [],
  ip_blacklist: [],
  last_used_at: null,
  last_used_ip: null,
  quota: 0,
  quota_used: 0,
  expires_at: null,
  created_at: '2026-09-01T00:00:00Z',
  updated_at: '2026-09-01T00:00:00Z',
  group: {
    id: 3,
    name: 'Gemini',
    platform: 'gemini',
    allow_batch_image_generation: true,
  },
} as unknown as ApiKey)

const batchJob = (overrides: Partial<BatchImageJob> = {}): BatchImageJob => ({
  id: 'batch_running_001',
  object: 'batch',
  task_name: 'Lighthouse batch',
  parent_batch_id: null,
  status: 'running',
  model: 'gemini-3-pro-image',
  provider: 'gemini_api',
  item_count: 2,
  success_count: 0,
  fail_count: 0,
  estimated_cost: 0.4,
  hold_amount: 0.4,
  actual_cost: null,
  created_at: 1_758_000_000,
  submitted_at: 1_758_000_000,
  settled_at: null,
  ...overrides,
})

const findButtonByText = (wrapper: VueWrapper, text: string) => {
  const button = wrapper.findAll('button').find(item => item.text().includes(text))
  if (!button) {
    throw new Error(`Button not found: ${text}`)
  }
  return button
}

const settleView = async () => {
  await flushPromises()
  await nextTick()
  await flushPromises()
  await nextTick()
}

const mountGuide = async () => {
  const wrapper = mount(BatchImageGuideView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        BaseDialog: BaseDialogStub,
        Icon: IconStub,
        Select: true,
        SearchInput: true,
        Teleport: true,
        'i18n-t': true,
      },
    },
  })
  await settleView()
  return wrapper
}

/** Opens the create dialog and submits one prompt, leaving a running job selected. */
const submitRunningJob = async (wrapper: VueWrapper) => {
  await findButtonByText(wrapper, i18nMock.messages['batchImage.actions.createJob']).trigger('click')
  await nextTick()
  await wrapper.get('textarea').setValue('a watercolor painting of a lighthouse')
  await wrapper.get('form').trigger('submit')
  await settleView()
  expect(batchImageApi.submitBatchImageJob).toHaveBeenCalledTimes(1)
  // Drop the "job submitted" notification so later assertions only cover what
  // the cancellation handler reported.
  appStoreMock.showSuccess.mockClear()
  appStoreMock.showError.mockClear()
}

describe('BatchImageGuideView polling', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // Only the polling/cache intervals are faked, so promise flushing still works.
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    keysApi.list.mockResolvedValue({ items: [geminiApiKey()] })
    batchImageApi.listBatchImageModels.mockResolvedValue({
      object: 'list',
      data: [{ id: 'gemini-3-pro-image', object: 'model', provider: 'gemini_api' }],
    })
    batchImageApi.listBatchImageJobs.mockResolvedValue({ object: 'list', data: [], has_more: false })
    batchImageApi.listBatchImageItems.mockResolvedValue({ object: 'list', data: [], has_more: false })
    batchImageApi.submitBatchImageJob.mockResolvedValue(batchJob())
    batchImageApi.getBatchImageJob.mockResolvedValue(batchJob())
    batchImageApi.cancelBatchImageJob.mockResolvedValue(batchJob({ status: 'cancelled' }))
    vi.spyOn(window, 'confirm').mockReturnValue(true)
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  it('re-reads the running job on every polling interval tick', async () => {
    const wrapper = await mountGuide()
    await submitRunningJob(wrapper)

    expect(batchImageApi.submitBatchImageJob).toHaveBeenCalledTimes(1)
    expect(batchImageApi.getBatchImageJob).not.toHaveBeenCalled()

    vi.advanceTimersByTime(POLL_INTERVAL_MS - 1)
    await flushPromises()
    expect(batchImageApi.getBatchImageJob).not.toHaveBeenCalled()

    vi.advanceTimersByTime(1)
    await settleView()
    expect(batchImageApi.getBatchImageJob).toHaveBeenCalledTimes(1)
    expect(batchImageApi.getBatchImageJob).toHaveBeenLastCalledWith('sk-gemini-batch-key', 'batch_running_001')

    vi.advanceTimersByTime(POLL_INTERVAL_MS)
    await settleView()
    expect(batchImageApi.getBatchImageJob).toHaveBeenCalledTimes(2)

    wrapper.unmount()
  })

  it('stops polling once the polled job reaches a terminal status', async () => {
    const wrapper = await mountGuide()
    await submitRunningJob(wrapper)

    batchImageApi.getBatchImageJob.mockResolvedValueOnce(batchJob({ status: 'running' }))
    vi.advanceTimersByTime(POLL_INTERVAL_MS)
    await settleView()
    expect(batchImageApi.getBatchImageJob).toHaveBeenCalledTimes(1)

    batchImageApi.getBatchImageJob.mockResolvedValueOnce(batchJob({ status: 'completed', success_count: 2 }))
    const timersWhileRunning = vi.getTimerCount()
    vi.advanceTimersByTime(POLL_INTERVAL_MS)
    await settleView()
    expect(batchImageApi.getBatchImageJob).toHaveBeenCalledTimes(2)
    expect(vi.getTimerCount()).toBe(timersWhileRunning - 1)

    vi.advanceTimersByTime(POLL_INTERVAL_MS * 3)
    await settleView()
    expect(batchImageApi.getBatchImageJob).toHaveBeenCalledTimes(2)

    wrapper.unmount()
  })

  it('clears the polling interval when the component unmounts', async () => {
    const setIntervalSpy = vi.spyOn(globalThis, 'setInterval')
    const clearIntervalSpy = vi.spyOn(globalThis, 'clearInterval')

    const wrapper = await mountGuide()
    await submitRunningJob(wrapper)

    const pollIntervalEntry = setIntervalSpy.mock.calls
      .map((call, index) => ({ delay: call[1], timerID: setIntervalSpy.mock.results[index]?.value }))
      .find(entry => entry.delay === POLL_INTERVAL_MS)
    expect(pollIntervalEntry).toBeDefined()
    expect(vi.getTimerCount()).toBeGreaterThan(0)

    wrapper.unmount()

    // No interval survives the unmount, and the polling interval itself is the
    // one that got cleared.
    expect(clearIntervalSpy).toHaveBeenCalledWith(pollIntervalEntry?.timerID)
    expect(vi.getTimerCount()).toBe(0)

    batchImageApi.getBatchImageJob.mockClear()
    vi.advanceTimersByTime(POLL_INTERVAL_MS * 4)
    await settleView()
    expect(batchImageApi.getBatchImageJob).not.toHaveBeenCalled()
  })
})

describe('BatchImageGuideView cancellation', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    keysApi.list.mockResolvedValue({ items: [geminiApiKey()] })
    batchImageApi.listBatchImageModels.mockResolvedValue({
      object: 'list',
      data: [{ id: 'gemini-3-pro-image', object: 'model', provider: 'gemini_api' }],
    })
    batchImageApi.listBatchImageJobs.mockResolvedValue({ object: 'list', data: [], has_more: false })
    batchImageApi.listBatchImageItems.mockResolvedValue({ object: 'list', data: [], has_more: false })
    batchImageApi.submitBatchImageJob.mockResolvedValue(batchJob())
    batchImageApi.getBatchImageJob.mockResolvedValue(batchJob())
    batchImageApi.cancelBatchImageJob.mockResolvedValue(batchJob({ status: 'cancelled' }))
    vi.spyOn(window, 'confirm').mockReturnValue(true)
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  it('cancels the selected job and reports success', async () => {
    const wrapper = await mountGuide()
    await submitRunningJob(wrapper)

    const cancelButton = findButtonByText(wrapper, i18nMock.messages['batchImage.actions.cancelJob'])
    expect(cancelButton.attributes('disabled')).toBeUndefined()

    await cancelButton.trigger('click')
    await settleView()

    expect(window.confirm).toHaveBeenCalledWith(i18nMock.messages['batchImage.messages.cancelConfirm'])
    expect(batchImageApi.cancelBatchImageJob).toHaveBeenCalledTimes(1)
    expect(batchImageApi.cancelBatchImageJob).toHaveBeenCalledWith('sk-gemini-batch-key', 'batch_running_001')
    expect(appStoreMock.showSuccess).toHaveBeenCalledWith(i18nMock.messages['batchImage.messages.cancelled'])
    expect(appStoreMock.showError).not.toHaveBeenCalled()

    // The cancelled job can no longer be cancelled.
    expect(findButtonByText(wrapper, i18nMock.messages['batchImage.actions.cancelJob']).attributes('disabled')).toBeDefined()

    wrapper.unmount()
  })

  it('reports the failure and keeps the job cancellable when cancellation fails', async () => {
    const wrapper = await mountGuide()
    await submitRunningJob(wrapper)

    batchImageApi.cancelBatchImageJob.mockRejectedValueOnce({})
    await findButtonByText(wrapper, i18nMock.messages['batchImage.actions.cancelJob']).trigger('click')
    await settleView()

    expect(batchImageApi.cancelBatchImageJob).toHaveBeenCalledWith('sk-gemini-batch-key', 'batch_running_001')
    expect(appStoreMock.showError).toHaveBeenCalledWith(i18nMock.messages['batchImage.messages.cancelFailed'])
    expect(appStoreMock.showSuccess).not.toHaveBeenCalled()

    // cancelling resets, so the operator can retry the request.
    const cancelButton = findButtonByText(wrapper, i18nMock.messages['batchImage.actions.cancelJob'])
    expect(cancelButton.attributes('disabled')).toBeUndefined()
    await cancelButton.trigger('click')
    await settleView()
    expect(batchImageApi.cancelBatchImageJob).toHaveBeenCalledTimes(2)

    wrapper.unmount()
  })

  it('does not call the cancel API when the confirmation is dismissed', async () => {
    const wrapper = await mountGuide()
    await submitRunningJob(wrapper)

    vi.spyOn(window, 'confirm').mockReturnValue(false)
    await findButtonByText(wrapper, i18nMock.messages['batchImage.actions.cancelJob']).trigger('click')
    await settleView()

    expect(batchImageApi.cancelBatchImageJob).not.toHaveBeenCalled()
    expect(appStoreMock.showSuccess).not.toHaveBeenCalled()
    expect(appStoreMock.showError).not.toHaveBeenCalled()

    wrapper.unmount()
  })

  it('cancels the job picked from the job list', async () => {
    batchImageApi.listBatchImageJobs.mockResolvedValue({
      object: 'list',
      data: [batchJob({ id: 'batch_listed_002', task_name: 'Listed batch' })],
      has_more: false,
    })
    batchImageApi.getBatchImageJob.mockResolvedValue(batchJob({ id: 'batch_listed_002', task_name: 'Listed batch' }))
    batchImageApi.cancelBatchImageJob.mockResolvedValue(batchJob({ id: 'batch_listed_002', status: 'cancelled' }))

    const wrapper = await mountGuide()
    await wrapper.get('[data-test="job-row-batch_listed_002"] button').trigger('click')
    await settleView()

    await findButtonByText(wrapper, i18nMock.messages['batchImage.actions.cancelJob']).trigger('click')
    await settleView()

    expect(batchImageApi.cancelBatchImageJob).toHaveBeenCalledWith('sk-gemini-batch-key', 'batch_listed_002')
    expect(appStoreMock.showSuccess).toHaveBeenCalledWith(i18nMock.messages['batchImage.messages.cancelled'])

    wrapper.unmount()
  })
})
