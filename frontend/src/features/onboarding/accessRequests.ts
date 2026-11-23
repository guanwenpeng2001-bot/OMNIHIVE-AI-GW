import {
  controlGet,
  controlPost,
  createIdempotencyKey
} from '@/features/shared/controlClient'
import { buildIfMatchHeaders } from '@/features/members/cas'

export type AccessRequestState = 'pending' | 'approved' | 'declined'

export interface ProjectAccessRequestItem {
  id: string
  workspace_id: string
  requester_id?: string
  requester_user_id?: string
  requester_member_id?: string
  requester_email?: string
  requester_username?: string
  requested_project_id?: string | null
  requested_project_name?: string | null
  approved_project_id?: string | null
  approved_project_name?: string | null
  granted_actions?: string[]
  add_actions?: string[]
  note: string
  state: AccessRequestState
  version: string
  created_at?: string
  decided_at?: string | null
  decided_by?: string | null
}

export interface ProjectAccessRequestPage {
  items: ProjectAccessRequestItem[]
  next_cursor: string | null
  as_of?: string
}

export interface CreateProjectAccessRequestInput {
  note: string
  requested_project_id?: string | null
}

export interface ApproveProjectAccessRequestInput {
  decision: 'approve'
  project_id: string
  add_actions: string[]
  member_version: string
}

export interface DeclineProjectAccessRequestInput {
  decision: 'decline'
}

export type DecideProjectAccessRequestInput =
  | ApproveProjectAccessRequestInput
  | DeclineProjectAccessRequestInput

const VALID_STATES = new Set<AccessRequestState>(['pending', 'approved', 'declined'])

export function validateAccessRequestNote(note: string | null | undefined): {
  valid: boolean
  trimmed: string
  length: number
} {
  const trimmed = String(note ?? '').trim()
  const length = trimmed.length
  return {
    valid: length >= 1 && length <= 500,
    trimmed,
    length
  }
}

export function isValidAccessRequestItem(raw: unknown): raw is ProjectAccessRequestItem {
  if (!raw || typeof raw !== 'object') return false
  const candidate = raw as Record<string, unknown>
  const id = typeof candidate.id === 'string' ? candidate.id.trim() : ''
  const workspaceId =
    typeof candidate.workspace_id === 'string' ? candidate.workspace_id.trim() : ''
  const state =
    typeof candidate.state === 'string'
      ? (candidate.state.trim() as AccessRequestState)
      : ('' as AccessRequestState)
  const version =
    typeof candidate.version === 'string' || typeof candidate.version === 'number'
      ? String(candidate.version).trim()
      : ''
  if (!id || !workspaceId || !VALID_STATES.has(state) || !version) {
    return false
  }
  return true
}

export function normalizeAccessRequestItem(raw: unknown): ProjectAccessRequestItem | null {
  if (!isValidAccessRequestItem(raw)) return null
  const item = raw as unknown as Record<string, unknown>
  return {
    id: String(item.id).trim(),
    workspace_id: String(item.workspace_id).trim(),
    requester_id:
      item.requester_id !== undefined && item.requester_id !== null
        ? String(item.requester_id)
        : undefined,
    requester_user_id:
      item.requester_user_id !== undefined && item.requester_user_id !== null
        ? String(item.requester_user_id)
        : item.requester_id !== undefined && item.requester_id !== null
          ? String(item.requester_id)
          : undefined,
    requester_member_id:
      item.requester_member_id !== undefined && item.requester_member_id !== null
        ? String(item.requester_member_id)
        : undefined,
    requester_email:
      typeof item.requester_email === 'string' ? item.requester_email : undefined,
    requester_username:
      typeof item.requester_username === 'string' ? item.requester_username : undefined,
    requested_project_id:
      typeof item.requested_project_id === 'string' && item.requested_project_id.trim()
        ? item.requested_project_id.trim()
        : null,
    requested_project_name:
      typeof item.requested_project_name === 'string' ? item.requested_project_name : null,
    approved_project_id:
      typeof item.approved_project_id === 'string' && item.approved_project_id.trim()
        ? item.approved_project_id.trim()
        : null,
    approved_project_name:
      typeof item.approved_project_name === 'string' ? item.approved_project_name : null,
    granted_actions: Array.isArray(item.granted_actions)
      ? item.granted_actions.map((a) => String(a).trim()).filter(Boolean)
      : Array.isArray(item.add_actions)
        ? item.add_actions.map((a) => String(a).trim()).filter(Boolean)
        : undefined,
    note: typeof item.note === 'string' ? item.note : '',
    state: item.state as AccessRequestState,
    version: String(item.version).trim(),
    created_at: typeof item.created_at === 'string' ? item.created_at : undefined,
    decided_at: typeof item.decided_at === 'string' ? item.decided_at : null,
    decided_by:
      item.decided_by !== undefined && item.decided_by !== null
        ? String(item.decided_by)
        : null
  }
}

export function normalizeAccessRequestPage(raw: unknown): ProjectAccessRequestPage {
  if (!raw || typeof raw !== 'object') {
    throw new Error('Malformed access request page response')
  }
  const record = raw as Record<string, unknown>
  if (!Array.isArray(record.items)) {
    throw new Error('Missing items array in access request page response')
  }
  const items: ProjectAccessRequestItem[] = []
  for (const entry of record.items) {
    const normalized = normalizeAccessRequestItem(entry)
    if (!normalized) {
      throw new Error('Malformed access request item in response')
    }
    items.push(normalized)
  }
  return {
    items,
    next_cursor:
      typeof record.next_cursor === 'string' && record.next_cursor.trim()
        ? record.next_cursor.trim()
        : null,
    as_of: typeof record.as_of === 'string' ? record.as_of : undefined
  }
}

export const accessRequestsAPI = {
  async create(
    workspaceId: string,
    input: CreateProjectAccessRequestInput,
    idempotencyKey = createIdempotencyKey()
  ): Promise<ProjectAccessRequestItem> {
    const ws = String(workspaceId ?? '').trim()
    if (!ws) {
      throw new Error('Workspace ID is required to submit an access request')
    }
    const noteCheck = validateAccessRequestNote(input.note)
    if (!noteCheck.valid) {
      throw new Error('Access request note must be between 1 and 500 characters')
    }
    const requestedProjectId =
      typeof input.requested_project_id === 'string' && input.requested_project_id.trim()
        ? input.requested_project_id.trim()
        : undefined
    const payload: Record<string, unknown> = {
      note: noteCheck.trimmed,
      ...(requestedProjectId ? { requested_project_id: requestedProjectId } : {})
    }
    const raw = await controlPost<unknown>(
      `/workspaces/${encodeURIComponent(ws)}/access-requests`,
      payload,
      { 'Idempotency-Key': idempotencyKey }
    )
    const normalized = normalizeAccessRequestItem(raw)
    if (!normalized) {
      throw new Error('Malformed access request creation response')
    }
    return normalized
  },

  async list(
    workspaceId: string,
    params?: { view?: 'mine' | 'review'; cursor?: string; limit?: number }
  ): Promise<ProjectAccessRequestPage> {
    const ws = String(workspaceId ?? '').trim()
    if (!ws) {
      throw new Error('Workspace ID is required to list access requests')
    }
    const query: Record<string, unknown> = {
      view: params?.view ?? 'mine',
      ...(params?.cursor ? { cursor: params.cursor } : {}),
      ...(params?.limit ? { limit: params.limit } : {})
    }
    const raw = await controlGet<unknown>(
      `/workspaces/${encodeURIComponent(ws)}/access-requests`,
      query
    )
    return normalizeAccessRequestPage(raw)
  },

  async decide(
    workspaceId: string,
    requestId: string,
    requestVersion: string,
    input: DecideProjectAccessRequestInput
  ): Promise<ProjectAccessRequestItem> {
    const ws = String(workspaceId ?? '').trim()
    const reqId = String(requestId ?? '').trim()
    if (!ws || !reqId) {
      throw new Error('Workspace ID and request ID are required for access request decision')
    }
    const headers = buildIfMatchHeaders(requestVersion)
    let body: Record<string, unknown>
    if (input.decision === 'decline') {
      body = { decision: 'decline' }
    } else {
      const projectId = String(input.project_id ?? '').trim()
      const memberVersion = String(input.member_version ?? '')
        .trim()
        .replace(/^"+|"+$/g, '')
        .trim()
      const addActions = Array.from(
        new Set((input.add_actions ?? []).map((a) => String(a ?? '').trim()).filter(Boolean))
      ).sort()
      if (!projectId) {
        throw new Error('Target project ID is required to approve an access request')
      }
      if (!memberVersion) {
        throw new Error('Member grant version is required to approve an access request')
      }
      if (addActions.length === 0) {
        throw new Error('At least one action must be selected to approve an access request')
      }
      body = {
        decision: 'approve',
        project_id: projectId,
        add_actions: addActions,
        member_version: memberVersion
      }
    }

    const raw = await controlPost<unknown>(
      `/workspaces/${encodeURIComponent(ws)}/access-requests/${encodeURIComponent(reqId)}/decision`,
      body,
      headers
    )
    const normalized = normalizeAccessRequestItem(raw)
    if (!normalized) {
      throw new Error('Malformed access request decision response')
    }
    return normalized
  }
}
