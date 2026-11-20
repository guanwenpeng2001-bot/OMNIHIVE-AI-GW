import { onScopeDispose, ref, watch } from 'vue'
import { normalizeApiError, type NormalizedApiError } from '@/utils/apiError'
import { billingApi, newCommandKey, scopedDownload, type CostWindow, type Operation } from './api'
import type { ProjectScope } from '@/features/requests/api'

export type CsvState = 'idle' | 'working' | 'ready' | 'pending' | 'downloaded' | 'expired' | 'conflict' | 'unknown' | 'failed'

export function exportResult(scope: ProjectScope, operation: Operation): CsvState {
  if (operation.state === 'queued' || operation.state === 'running') return 'pending'
  if (operation.state === 'unknown') return 'unknown'
  if (operation.state === 'failed') return 'failed'
  if (operation.result?.download_url === null ||
      (operation.result?.expires_at && Date.parse(operation.result.expires_at) <= Date.now())) return 'expired'
  return scopedDownload(scope, operation) ? 'ready' : 'failed'
}

export function useCsvExport(key: () => string, scope: () => ProjectScope | null, range: () => CostWindow | null) {
  const state = ref<CsvState>('idle')
  const operation = ref<Operation | null>(null)
  const operationID = ref<string | null>(null)
  const error = ref<NormalizedApiError | null>(null)
  let commandKey: string | null = null
  let pending: AbortController | null = null

  function begin() {
    pending?.abort()
    const request = new AbortController()
    pending = request
    state.value = 'working'
    error.value = null
    return request
  }
  function current(request: AbortController) {
    return pending === request && !request.signal.aborted && Boolean(key())
  }
  function fail(cause: unknown, request: AbortController, mutation: boolean) {
    if (!current(request)) return
    const issue = normalizeApiError(cause)
    error.value = issue
    if (typeof issue.metadata.operation_id === 'string') operationID.value = issue.metadata.operation_id
    if (issue.status === 410) state.value = 'expired'
    else if (issue.status === 409) state.value = 'conflict'
    else if (mutation && (issue.status === 0 || issue.status >= 500 || issue.metadata.retry === 'query_status')) state.value = 'unknown'
    else state.value = 'failed'
  }
  function receive(target: ProjectScope, result: Operation, request: AbortController) {
    if (!current(request)) return
    operation.value = result
    operationID.value = result.operation_id
    state.value = exportResult(target, result)
  }

  async function create(fresh = false) {
    const target = scope()
    const window = range()
    if (!key() || !target || !window || state.value === 'working') return
    if (fresh || commandKey === null) commandKey = newCommandKey()
    const request = begin()
    try {
      receive(target, await billingApi.createExport(target, window, commandKey, request.signal), request)
    } catch (cause) {
      fail(cause, request, true)
    }
  }

  async function check() {
    const target = scope()
    const id = operationID.value
    if (!target || !id || !key() || state.value === 'working') return
    const request = begin()
    try {
      receive(target, await billingApi.operation(target, id, request.signal), request)
    } catch (cause) {
      fail(cause, request, false)
    }
  }

  async function download() {
    const target = scope()
    const result = operation.value
    if (!key() || !target || !result || state.value === 'working') return
    const request = begin()
    try {
      const file = await billingApi.download(target, result, request.signal)
      const { saveAs } = await import('file-saver')
      if (!current(request)) return
      saveAs(file, 'billing-charges.csv')
      state.value = 'downloaded'
    } catch (cause) {
      fail(cause, request, false)
    }
  }

  function reset() {
    pending?.abort()
    pending = null
    commandKey = null
    operationID.value = null
    operation.value = null
    state.value = 'idle'
    error.value = null
  }
  watch(key, reset, { flush: 'sync' })
  onScopeDispose(reset)
  return { state, operation, operationID, error, create, check, download }
}
