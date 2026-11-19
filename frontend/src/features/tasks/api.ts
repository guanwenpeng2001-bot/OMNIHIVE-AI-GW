import { apiClient, buildGatewayUrl } from '@/api/client'
import { generateIdempotencyKey } from '@/features/console/scope'
import type {
  ImageTaskCancelResult,
  ImageTaskCreateInput,
  ImageTaskDetail,
  ImageTaskItemPage,
  ImageTaskPage,
  ImageTaskRetryInput,
  ImageTaskRetryResult,
  ImageTaskTicket
} from './types'

function v2TasksBase(workspaceId: string, projectId: string): string {
  return buildGatewayUrl(`/api/v2/workspaces/${workspaceId}/projects/${projectId}/tasks`)
}

export interface TaskListFilterParams {
  cursor?: string
  limit?: number
  kind?: string[]
  state?: string[]
  model_id?: string[]
  from?: string
  to?: string
}

function buildTaskQueryPairs(params?: TaskListFilterParams): URLSearchParams {
  const search = new URLSearchParams()
  if (!params) return search
  if (params.cursor) search.set('cursor', params.cursor)
  if (params.limit) search.set('limit', String(params.limit))
  if (params.from) search.set('from', params.from)
  if (params.to) search.set('to', params.to)
  for (const k of params.kind ?? []) {
    if (k) search.append('kind', k)
  }
  for (const s of params.state ?? []) {
    if (s) search.append('state', s)
  }
  for (const m of params.model_id ?? []) {
    if (m) search.append('model_id', m)
  }
  return search
}

export const tasksV2Api = {
  async listTasks(
    workspaceId: string,
    projectId: string,
    params?: TaskListFilterParams,
    signal?: AbortSignal
  ): Promise<ImageTaskPage> {
    const { data } = await apiClient.get<ImageTaskPage>(
      v2TasksBase(workspaceId, projectId),
      {
        baseURL: '',
        params: buildTaskQueryPairs(params),
        signal
      }
    )
    return data
  },

  async createTask(
    workspaceId: string,
    projectId: string,
    payload: ImageTaskCreateInput,
    idempotencyKey = generateIdempotencyKey()
  ): Promise<ImageTaskDetail> {
    const { data } = await apiClient.post<ImageTaskDetail>(
      v2TasksBase(workspaceId, projectId),
      payload,
      {
        baseURL: '',
        headers: { 'Idempotency-Key': idempotencyKey }
      }
    )
    return data
  },

  async getTaskDetail(
    workspaceId: string,
    projectId: string,
    taskId: string,
    signal?: AbortSignal
  ): Promise<ImageTaskDetail> {
    const { data } = await apiClient.get<ImageTaskDetail>(
      `${v2TasksBase(workspaceId, projectId)}/${encodeURIComponent(taskId)}`,
      { baseURL: '', signal }
    )
    return data
  },

  async listTaskItems(
    workspaceId: string,
    projectId: string,
    taskId: string,
    params?: { cursor?: string; limit?: number },
    signal?: AbortSignal
  ): Promise<ImageTaskItemPage> {
    const { data } = await apiClient.get<ImageTaskItemPage>(
      `${v2TasksBase(workspaceId, projectId)}/${encodeURIComponent(taskId)}/items`,
      { baseURL: '', params, signal }
    )
    return data
  },

  async cancelTask(
    workspaceId: string,
    projectId: string,
    taskId: string,
    idempotencyKey = generateIdempotencyKey()
  ): Promise<ImageTaskCancelResult> {
    const { data } = await apiClient.post<ImageTaskCancelResult>(
      `${v2TasksBase(workspaceId, projectId)}/${encodeURIComponent(taskId)}/cancel`,
      {},
      {
        baseURL: '',
        headers: { 'Idempotency-Key': idempotencyKey }
      }
    )
    return data
  },

  async retryTask(
    workspaceId: string,
    projectId: string,
    taskId: string,
    payload: ImageTaskRetryInput,
    idempotencyKey = generateIdempotencyKey()
  ): Promise<ImageTaskRetryResult> {
    const { data } = await apiClient.post<ImageTaskRetryResult>(
      `${v2TasksBase(workspaceId, projectId)}/${encodeURIComponent(taskId)}/retry`,
      payload,
      {
        baseURL: '',
        headers: { 'Idempotency-Key': idempotencyKey }
      }
    )
    return data
  },

  async issueArtifactDownloadTicket(
    workspaceId: string,
    projectId: string,
    taskId: string,
    artifactId: string
  ): Promise<ImageTaskTicket> {
    const { data } = await apiClient.post<ImageTaskTicket>(
      `${v2TasksBase(workspaceId, projectId)}/${encodeURIComponent(taskId)}/artifacts/${encodeURIComponent(artifactId)}/download`,
      {},
      { baseURL: '' }
    )
    return data
  },

  async fetchArtifactContentBlob(
    workspaceId: string,
    projectId: string,
    taskId: string,
    artifactId: string,
    ticket: ImageTaskTicket
  ): Promise<Blob> {
    const contentUrl = ticket.url
      ? ticket.url.startsWith('http')
        ? ticket.url
        : buildGatewayUrl(ticket.url)
      : `${v2TasksBase(workspaceId, projectId)}/${encodeURIComponent(taskId)}/artifacts/${encodeURIComponent(artifactId)}/content`

    const { data } = await apiClient.get<Blob>(contentUrl, {
      baseURL: '',
      responseType: 'blob',
      headers: {
        'X-Artifact-Ticket': ticket.ticket
      }
    })
    return data
  },

  async revokeArtifact(
    workspaceId: string,
    projectId: string,
    taskId: string,
    artifactId: string
  ): Promise<void> {
    await apiClient.delete(
      `${v2TasksBase(workspaceId, projectId)}/${encodeURIComponent(taskId)}/artifacts/${encodeURIComponent(artifactId)}`,
      { baseURL: '' }
    )
  }
}
