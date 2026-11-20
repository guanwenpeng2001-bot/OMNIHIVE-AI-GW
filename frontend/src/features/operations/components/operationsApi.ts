import { apiClient } from '@/api/client'

export type ScopedAuditResource =
  | 'request_export'
  | 'artifact'
  | 'billing_command'
  | 'retention'
  | 'audit_event'
  | 'project'

export type ScopedAuditAction =
  | 'requests.export'
  | 'tasks.artifacts.revoke'
  | 'billing.adjust'
  | 'billing.reconcile'
  | 'audit.retention.set'
  | 'audit.retention.purge'
  | 'audit.export'
  | 'scope.read'
  | 'scope.write'

export interface ScopedAuditEvent {
  audit_event_id: string
  workspace_id: string
  project_id: string
  actor_id: string | null
  resource: ScopedAuditResource
  resource_id: string | null
  action: ScopedAuditAction
  effect: 'committed' | 'observed'
  at: string
  affected?: string
}

export interface ScopedAuditPage {
  items: ScopedAuditEvent[]
  as_of: string
  next_cursor: string | null
}

export interface ScopedAuditRetentionPolicy {
  event_days: number
  trace_days: number
  content_hours: number
}

export interface TelemetryDestinationInput {
  endpoint: string
}

export interface TelemetryDestination {
  id: string
  state: 'registered' | 'revoked'
  version: string
}

export interface TelemetryExportCreateInput {
  destination_id: string
  data_classes: Array<'timings'>
  sample_rate: string
  retention_days: number
}

export interface TelemetryExportUpdateInput {
  destination_id?: string
  data_classes?: Array<'timings'>
  sample_rate?: string
  retention_days?: number
  enabled?: boolean
}

export interface TelemetryExportConfig {
  id: string
  state: 'validating' | 'active' | 'failed' | 'disabled'
  version: string
  destination_id: string
}

export interface TelemetryExportDetail extends TelemetryExportConfig {
  data_classes: Array<'timings'>
  sample_rate: string
  retention_days: number
  last_success_at: string | null
  last_error_reason: string | null
  queue_depth: number
}

export type CatalogAccountMode =
  | 'api_key'
  | 'oauth'
  | 'setup-token'
  | 'service_account'
  | 'bedrock'
  | 'payg'
  | 'coding'

export type CatalogMaturity =
  | 'candidate'
  | 'experimental'
  | 'stable'
  | 'deprecated'
  | 'closed'

export type CatalogDeprecationReason =
  | 'provider_retirement'
  | 'adapter_retirement'
  | 'superseded'

export interface CatalogVerificationMatrix {
  entry_id: string
  adapter_version: string
  action: string
  account_mode: CatalogAccountMode
  protocol: string
}

export interface CatalogVerificationMatrixInput {
  entry_id: string
  adapter_version: string
  action: string
  account_mode: CatalogAccountMode
  protocol?: string
}

export interface CatalogVerificationEvidence {
  evidence_id: string
  source: 'endpoint_probe' | 'connection_verification' | string
  state: 'success' | 'failed' | 'unknown' | string
  freshness: 'fresh' | 'stale' | 'unknown' | string
  sample_count: string | number
  window_start: string
  observed_at: string
  verified_at: string | null
  expires_at: string
  fixture_id: string | null
}

export interface CatalogVerificationObservation extends CatalogVerificationEvidence {
  matrix: CatalogVerificationMatrix
  reason: string
}

export interface CatalogDeprecationNotice {
  reason: CatalogDeprecationReason
  announced_at: string
  sunset_at: string
}

export interface CatalogCompatibilityPublication {
  target_id: string
  entry_id: string
  action: string
  protocol: string
  account_mode: CatalogAccountMode
  adapter_version: string
  maturity: CatalogMaturity
  publication_version: string
  published_at: string
  rollback_version: string | null
  evidence: CatalogVerificationEvidence | null
  deprecation: CatalogDeprecationNotice | null
}

export interface CatalogVerificationTargetInput {
  provider: string
  model: string
  connection_id: string
  adapter_version: string
  action: string
  account_mode: CatalogAccountMode
  protocol: string
  interval_seconds?: number
}

export interface CatalogVerificationTarget {
  id: string
  entry_id: string
  provider: string
  model: string
  connection_id: string
  owner_id: string
  adapter_version: string
  action: string
  account_mode: CatalogAccountMode
  protocol: string
  interval_seconds: number
  schedule_version: string
  next_due_at: string | null
  created_at: string
}

export interface CatalogVerificationTargetDetail extends CatalogVerificationTarget {
  latest_attempt: CatalogVerificationObservation | null
  last_success: CatalogVerificationObservation | null
  publication: CatalogCompatibilityPublication | null
}

export interface CatalogVerificationOperation {
  operation_id: string
  state: 'queued' | 'running' | 'succeeded' | 'failed' | 'unknown'
  status_url: string
  resource_id: string
  result: CatalogVerificationObservation | null
  error: null
}

export type CatalogPublishPayload =
  | { maturity: 'experimental'; evidence_id?: string }
  | { maturity: 'stable'; evidence_id: string }
  | { maturity: 'deprecated'; deprecation_reason: CatalogDeprecationReason; sunset_at: string }
  | { maturity: 'closed' }
  | { rollback_version: string }

export type StatusSeverity = 'degraded' | 'outage' | 'maintenance'

export interface StatusComponentItem {
  id: string
  name: string
}

export interface StatusSummary {
  components?: StatusComponentItem[]
}

export interface ScopedWorkspaceSummary {
  workspace_id: string
  name: string
  projects: Array<{ id: string; name: string }>
  projects_next_cursor?: string | null
}

export interface ScopedWorkspacePage {
  items: ScopedWorkspaceSummary[]
  next_cursor: string | null
  as_of?: string
}

export interface ScopedProjectPage {
  items: Array<{ id: string; name: string }>
  next_cursor: string | null
}

export function generateCommandKey(): string {
  if (typeof globalThis.crypto?.randomUUID === 'function') {
    return globalThis.crypto.randomUUID()
  }
  return `${Date.now()}-${Math.random().toString(36).slice(2, 10)}`
}

const GATEWAY_API_PREFIX = '/api/v2'

function resolveGatewayPath(path: string): string {
  if (path.startsWith('/api/')) return path
  const normalized = path.startsWith('/') ? path : `/${path}`
  return `${GATEWAY_API_PREFIX}${normalized}`
}

export async function requestGet<T>(
  path: string,
  params?: Record<string, unknown>,
  headers?: Record<string, string>,
  signal?: AbortSignal
): Promise<T> {
  const { data } = await apiClient.get<T>(resolveGatewayPath(path), {
    params,
    headers,
    signal
  })
  return data
}

export async function requestPost<T>(
  path: string,
  body?: unknown,
  headers?: Record<string, string>
): Promise<T> {
  const { data } = await apiClient.post<T>(resolveGatewayPath(path), body, {
    headers
  })
  return data
}

export async function requestPut<T>(
  path: string,
  body?: unknown,
  headers?: Record<string, string>
): Promise<T> {
  const { data } = await apiClient.put<T>(resolveGatewayPath(path), body, {
    headers
  })
  return data
}

export async function requestPatch<T>(
  path: string,
  body?: unknown,
  headers?: Record<string, string>
): Promise<T> {
  const { data } = await apiClient.patch<T>(resolveGatewayPath(path), body, {
    headers
  })
  return data
}

export async function requestDelete<T = void>(
  path: string,
  headers?: Record<string, string>
): Promise<T> {
  const { data } = await apiClient.delete<T>(resolveGatewayPath(path), {
    headers
  })
  return data
}
