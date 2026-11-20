import { apiClient } from '@/api/client'
import type { components } from '@/api/generated/openapi'

type Schema = components['schemas']
export type PolicyHead = Schema['RoutingPolicyHead']
export type PolicyDraft = Schema['RoutingPolicyInput']
export type PolicyScope = Schema['RoutingPolicyScope']
export type PolicyRule = Schema['RoutingPolicyListRule']
export type PolicySimulation = Schema['RoutingPolicySimulation']
export type SimulationRequest = Schema['RoutingPolicySimulationInput']
export type PolicyVersion = Schema['RoutingPolicyVersion']
export type PolicyPublication = Schema['RoutingPolicyPublication']
export type RoutingTrace = Schema['RoutingDecisionTrace']
export type RoutingWorkspace = Schema['IdentityWorkspace']

type Page<T> = { items: T[]; next_cursor: string | null; as_of: string }

const identities = '/api/v2/workspaces'

function policyPath(workspace: string, id?: string) {
  const collection = `${identities}/${encodeURIComponent(workspace)}/routing/policies`
  return id ? `${collection}/${encodeURIComponent(id)}` : collection
}

function readOptions(signal: AbortSignal, params: Record<string, string | undefined> = {}) {
  // These handlers reject unknown query parameters. Suppress the legacy interceptor's timezone.
  return { signal, params: { ...params, timezone: undefined } }
}

function writeOptions(signal: AbortSignal, etag?: string, idempotent = false) {
  const headers: Record<string, string> = {}
  if (etag !== undefined) headers['If-Match'] = `"${etag}"`
  if (idempotent) {
    const random = crypto.getRandomValues(new Uint8Array(16))
    headers['Idempotency-Key'] = Array.from(random, byte => byte.toString(16).padStart(2, '0')).join('')
  }
  return { signal, headers }
}

export const routingAPI = {
  async workspaces(signal: AbortSignal, cursor?: string) {
    return (await apiClient.get<Schema['IdentityWorkspacePage']>(
      '/api/v2/me/scopes', readOptions(signal, { cursor }),
    )).data
  },
  async projects(workspace: string, signal: AbortSignal, cursor?: string) {
    return (await apiClient.get<Schema['IdentityProjectPage']>(
      `${identities}/${encodeURIComponent(workspace)}/projects`, readOptions(signal, { cursor }),
    )).data
  },
  async keys(workspace: string, project: string, signal: AbortSignal, cursor?: string) {
    const path = `${identities}/${encodeURIComponent(workspace)}/projects/${encodeURIComponent(project)}/keys`
    const { data } = await apiClient.get<Schema['IdentityKeyPage']>(path, readOptions(signal, { cursor }))
    return { ...data, items: data.items.map(({ id, name, prefix }) => ({ id, name, prefix })) }
  },
  async list(workspace: string, scope: PolicyScope, signal: AbortSignal) {
    const options = readOptions(signal, { scope_type: scope.type, scope_id: scope.id, project_id: scope.project_id })
    return (await apiClient.get<Page<PolicyHead>>(policyPath(workspace), options)).data
  },
  async save(workspace: string, draft: PolicyDraft, head: PolicyHead | null, signal: AbortSignal) {
    const response = head
      ? await apiClient.put<PolicyHead>(`${policyPath(workspace, head.id)}/draft`, draft, writeOptions(signal, head.etag))
      : await apiClient.post<PolicyHead>(policyPath(workspace), draft, writeOptions(signal, undefined, true))
    return response.data
  },
  async versions(workspace: string, id: string, signal: AbortSignal, cursor?: string) {
    return (await apiClient.get<Page<PolicyVersion>>(
      `${policyPath(workspace, id)}/versions`, readOptions(signal, { cursor }),
    )).data
  },
  async simulate(workspace: string, id: string, body: SimulationRequest, signal: AbortSignal) {
    return (await apiClient.post<PolicySimulation>(`${policyPath(workspace, id)}/simulate`, body, { signal })).data
  },
  async publish(workspace: string, head: PolicyHead, simulation: PolicySimulation, note: string, signal: AbortSignal) {
    const body: Schema['RoutingPolicyPublish'] = {
      draft_version: head.draft_version,
      base_version: head.base_version,
      simulation_id: simulation.simulation_id,
      change_note: note,
    }
    return (await apiClient.post<PolicyPublication>(
      `${policyPath(workspace, head.id)}/publish`, body, writeOptions(signal, head.etag, true),
    )).data
  },
  async rollback(workspace: string, head: PolicyHead, restoreVersion: string, note: string, signal: AbortSignal) {
    const body: Schema['RoutingPolicyRollback'] = {
      from_version: head.active_version,
      restore_version: restoreVersion,
      change_note: note,
    }
    return (await apiClient.post<PolicyPublication>(
      `${policyPath(workspace, head.id)}/rollback`, body, writeOptions(signal, head.etag, true),
    )).data
  },
}
