import { apiClient } from '@/api/client'
import { projectBase, queryParams, controlGet, workspaceBase, type ProjectScope, type Schema } from '@/features/requests/api'

export type CostWindow = Omit<Schema['CostExportInput'], 'format'>
export type Operation = Omit<Schema['IdentityOperation'], 'result'> & {
  result: { download_url?: string | null; expires_at?: string } | null
}

export function newCommandKey(): string {
  return crypto.randomUUID?.() ??
    Array.from(crypto.getRandomValues(new Uint8Array(16)), byte => byte.toString(16).padStart(2, '0')).join('')
}

function billingBase(scope: ProjectScope): string {
  return `${projectBase(scope)}/billing`
}

function caseBase(scope: ProjectScope): string {
  return `${workspaceBase(scope.workspaceId)}/billing/reconciliation`
}

function safeOperationId(id: string): boolean {
  return /^[a-f\d]{8}-[a-f\d]{4}-[a-f\d]{4}-[a-f\d]{4}-[a-f\d]{12}$/.test(id)
}

export function scopedDownload(scope: ProjectScope, operation: Operation): string | null {
  if (!safeOperationId(operation.operation_id)) return null
  const expected = `${billingBase(scope)}/exports/${operation.operation_id}/download`
  return operation.result?.download_url === expected ? expected : null
}

export const billingApi = {
  activity: (scope: ProjectScope, params: URLSearchParams, signal: AbortSignal) =>
    controlGet<Schema['CostActivity']>(`${billingBase(scope)}/activity`, params, signal),
  workspaceActivity: (workspaceId: string, params: URLSearchParams, signal: AbortSignal) =>
    controlGet<Schema['CostActivity']>(`${workspaceBase(workspaceId)}/billing/activity`, params, signal),
  charges: (scope: ProjectScope, params: URLSearchParams, signal: AbortSignal) =>
    controlGet<Schema['CostChargePage']>(`${billingBase(scope)}/charges`, params, signal),
  ledger: (scope: ProjectScope, params: URLSearchParams, signal: AbortSignal) =>
    controlGet<Schema['CostLedgerPage']>(`${billingBase(scope)}/ledger`, params, signal),
  quotas: (scope: ProjectScope, key: string, model: string, signal: AbortSignal) =>
    controlGet<Schema['CostQuota']>(`${billingBase(scope)}/quotas`, queryParams({ api_key_id: key, model_id: model }), signal),
  async updateBudget(
    scope: ProjectScope,
    budgetId: string,
    input: Schema['CostBudgetInput'],
    version: string,
    key: string,
    signal: AbortSignal,
  ): Promise<Schema['CostRule']> {
    const cleanVersion = String(version || '0').replace(/^"+|"+$/g, '')
    return (await apiClient.put<Schema['CostRule']>(
      `${billingBase(scope)}/budgets/${encodeURIComponent(budgetId)}`,
      input,
      {
        headers: { 'Idempotency-Key': key, 'If-Match': `"${cleanVersion}"` },
        signal,
      },
    )).data
  },
  cases: (scope: ProjectScope, state: string, currency: string, cursor: string | null, signal: AbortSignal) =>
    controlGet<Schema['CostCasePage']>(caseBase(scope), queryParams({ project_id: scope.projectId, state, currency, cursor }), signal),
  async createExport(scope: ProjectScope, window: CostWindow, key: string, signal: AbortSignal): Promise<Operation> {
    return (await apiClient.post<Operation>(`${billingBase(scope)}/exports`, { ...window, format: 'csv' }, {
      headers: { 'Idempotency-Key': key }, signal,
    })).data
  },
  operation(scope: ProjectScope, id: string, signal: AbortSignal): Promise<Operation> {
    if (!safeOperationId(id)) return Promise.reject({ status: 404, reason: 'NOT_FOUND' })
    return controlGet<Operation>(`${workspaceBase(scope.workspaceId)}/operations/${id}`, undefined, signal)
  },
  async download(scope: ProjectScope, operation: Operation, signal: AbortSignal): Promise<Blob> {
    const path = scopedDownload(scope, operation)
    if (!path) throw { status: 404, reason: 'NOT_FOUND' }
    return (await apiClient.get<Blob>(path, {
      responseType: 'blob', params: new URLSearchParams(), paramsSerializer: { serialize: () => '' }, signal,
    })).data
  },
  async postAdjustment(scope: ProjectScope, input: Schema['CostAdjustmentInput'], key: string, signal: AbortSignal): Promise<Schema['CostAdjustment']> {
    return (await apiClient.post<Schema['CostAdjustment']>(`${billingBase(scope)}/adjustments`, input, {
      headers: { 'Idempotency-Key': key }, signal,
    })).data
  },
  listPaymentEvidence: (scope: ProjectScope, adjustmentId: string, signal: AbortSignal) =>
    controlGet<{ items: Schema['PaymentAdjustmentEvidence'][] }>(
      `${billingBase(scope)}/adjustments/${encodeURIComponent(adjustmentId)}/payment-evidence`,
      undefined,
      signal,
    ),
  async linkPaymentEvidence(
    scope: ProjectScope,
    adjustmentId: string,
    eventId: string,
    signal: AbortSignal,
  ): Promise<void> {
    await apiClient.put(
      `${billingBase(scope)}/adjustments/${encodeURIComponent(adjustmentId)}/payment-evidence/${encodeURIComponent(eventId)}`,
      undefined,
      { signal },
    )
  },
  async resolveCase(scope: ProjectScope, record: Schema['CostCase'], input: Schema['CostResolutionInput'], key: string, signal: AbortSignal): Promise<Operation> {
    if (record.project_id !== scope.projectId) throw { status: 404, reason: 'NOT_FOUND' }
    return (await apiClient.post<Operation>(`${caseBase(scope)}/${encodeURIComponent(record.id)}/resolve`, input, {
      headers: { 'Idempotency-Key': key, 'If-Match': `"${record.version}"` }, signal,
    })).data
  },
}
