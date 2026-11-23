import { reactive } from 'vue'
import {
  identityApi,
  type IdentityAccess,
  type IdentityWorkspaceAccess
} from '@/api/identity'
import { isSilentRequestError } from '@/api/client'
import { AUTH_SESSION_EVENT, authStorage } from '@/api/tokenRefresh'
import { normalizeApiError } from '@/utils/apiError'

export type PermissionProbeState = 'loading' | 'allowed' | 'denied' | 'unavailable'
export type AccessCacheStatus = 'idle' | 'loading' | 'ready' | 'denied' | 'unavailable'
export type AccessUnavailableKind =
  | 'unauthenticated'
  | 'not_found'
  | 'unavailable'
  | 'malformed'
  | 'expired'

export interface ScopeAccessError {
  status: number
  kind: AccessUnavailableKind | 'forbidden'
  reason?: string
  message: string
  requestId?: string
}

export interface AccessProbeEntry {
  cacheKey: string
  kind: 'workspace' | 'project'
  workspaceId: string
  projectId: string | null
  actorId: string
  status: AccessCacheStatus
  actions: string[]
  issuableKeyActions: string[] | null
  membershipRole: string | null
  membershipRoleSource: string | null
  workspaceAdmin: boolean
  grantableActions: string[]
  scopeVersion: string | null
  expiresAtMs: number | null
  error: ScopeAccessError | null
  updatedAtMs: number
}

interface InFlightProbe {
  generation: number
  controller: AbortController
  promise: Promise<AccessProbeEntry>
}

const accessCache = reactive<Record<string, AccessProbeEntry>>({})
const inFlightProbes = new Map<string, InFlightProbe>()
let globalProbeGeneration = 0
let listenersAttached = false

function resolveSessionRevision(): string {
  return authStorage.sessionRevision.get() || '0'
}

export function resolveProbeActorId(): string {
  const raw = authStorage.user.get()
  if (!raw) return ''
  try {
    const parsed = JSON.parse(raw) as { id?: number | string } | null
    if (parsed && parsed.id !== undefined && parsed.id !== null) {
      return String(parsed.id).trim()
    }
  } catch {
    // ignore malformed stored user
  }
  return ''
}

export function buildAccessCacheKey(
  workspaceId: string,
  projectId: string | null,
  actorId = resolveProbeActorId(),
  sessionRevision = resolveSessionRevision()
): string {
  return `${sessionRevision}:${actorId || 'anon'}:${workspaceId}:${projectId || '__workspace__'}`
}

function attachSessionListener(): void {
  if (listenersAttached || typeof window === 'undefined') return
  listenersAttached = true
  window.addEventListener(AUTH_SESSION_EVENT, () => {
    resetScopeAccessCache()
  })
}

export function resetScopeAccessCache(): void {
  globalProbeGeneration += 1
  for (const pending of inFlightProbes.values()) {
    pending.controller.abort()
  }
  inFlightProbes.clear()
  for (const key of Object.keys(accessCache)) {
    delete accessCache[key]
  }
}

export function invalidateScopeAccess(workspaceId?: string, projectId?: string | null): void {
  globalProbeGeneration += 1
  if (!workspaceId) {
    resetScopeAccessCache()
    return
  }
  const suffix = projectId ? ':' + workspaceId + ':' + projectId : ':' + workspaceId + ':'
  for (const [key, pending] of inFlightProbes.entries()) {
    if (key.includes(suffix)) {
      pending.controller.abort()
      inFlightProbes.delete(key)
    }
  }
  for (const key of Object.keys(accessCache)) {
    if (key.includes(suffix)) {
      delete accessCache[key]
    }
  }
}

function extractRequestId(error: unknown, metadata?: Record<string, unknown>): string | undefined {
  const raw = (error ?? {}) as {
    requestId?: string
    metadata?: Record<string, unknown>
    response?: {
      headers?: Record<string, string>
      data?: { metadata?: Record<string, unknown> }
    }
  }
  const metaId =
    (typeof raw.metadata?.request_id === 'string' && raw.metadata.request_id) ||
    (typeof raw.response?.data?.metadata?.request_id === 'string' &&
      raw.response.data.metadata.request_id) ||
    (typeof metadata?.request_id === 'string' && metadata.request_id) ||
    undefined
  return raw.requestId || metaId || raw.response?.headers?.['x-request-id']
}

function normalizeActionArray(value: unknown): string[] | null {
  if (!Array.isArray(value)) return null
  const normalized: string[] = []
  for (const item of value) {
    if (typeof item !== 'string' || item.trim() === '') return null
    normalized.push(item.trim())
  }
  return normalized
}

function parseValidFutureExpiry(
  expiresAt: unknown,
  nowMs: number
): { valid: boolean; expired: boolean; ms: number | null } {
  if (typeof expiresAt !== 'string' || expiresAt.trim() === '') {
    return { valid: false, expired: false, ms: null }
  }
  const ms = Date.parse(expiresAt)
  if (!Number.isFinite(ms)) {
    return { valid: false, expired: false, ms: null }
  }
  return { valid: true, expired: ms <= nowMs, ms }
}

function buildEntryBase(
  cacheKey: string,
  kind: 'workspace' | 'project',
  workspaceId: string,
  projectId: string | null,
  actorId: string,
  status: AccessCacheStatus,
  nowMs: number
): AccessProbeEntry {
  return {
    cacheKey,
    kind,
    workspaceId,
    projectId,
    actorId,
    status,
    actions: [],
    issuableKeyActions: null,
    membershipRole: null,
    membershipRoleSource: null,
    workspaceAdmin: false,
    grantableActions: [],
    scopeVersion: null,
    expiresAtMs: null,
    error: null,
    updatedAtMs: nowMs
  }
}

function buildFailureEntry(
  cacheKey: string,
  kind: 'workspace' | 'project',
  workspaceId: string,
  projectId: string | null,
  actorId: string,
  status: AccessCacheStatus,
  error: ScopeAccessError,
  nowMs: number
): AccessProbeEntry {
  return {
    ...buildEntryBase(cacheKey, kind, workspaceId, projectId, actorId, status, nowMs),
    error
  }
}

function mapProbeError(
  err: unknown,
  cacheKey: string,
  kind: 'workspace' | 'project',
  workspaceId: string,
  projectId: string | null,
  actorId: string,
  nowMs: number
): AccessProbeEntry {
  const normalized = normalizeApiError(err)
  const raw = (err ?? {}) as {
    status?: number
    code?: string | number
    reason?: string
    response?: { status?: number; data?: { reason?: string } }
  }
  const httpStatus = raw.status ?? raw.response?.status ?? normalized.status ?? 0
  const reason = raw.reason || raw.response?.data?.reason || normalized.reason
  const requestId = extractRequestId(err, normalized.metadata)

  if (httpStatus === 403 || reason === 'FORBIDDEN' || reason === 'INSUFFICIENT_GRANT') {
    return buildFailureEntry(
      cacheKey,
      kind,
      workspaceId,
      projectId,
      actorId,
      'denied',
      {
        status: 403,
        kind: 'forbidden',
        reason: reason || 'INSUFFICIENT_GRANT',
        message: normalized.message || 'Permission denied',
        requestId
      },
      nowMs
    )
  }

  if (httpStatus === 401 || reason === 'UNAUTHORIZED') {
    return buildFailureEntry(
      cacheKey,
      kind,
      workspaceId,
      projectId,
      actorId,
      'unavailable',
      {
        status: 401,
        kind: 'unauthenticated',
        reason: reason || 'UNAUTHORIZED',
        message: normalized.message || 'Authentication required',
        requestId
      },
      nowMs
    )
  }

  if (httpStatus === 404 || reason === 'NOT_FOUND') {
    return buildFailureEntry(
      cacheKey,
      kind,
      workspaceId,
      projectId,
      actorId,
      'unavailable',
      {
        status: 404,
        kind: 'not_found',
        reason: reason || 'NOT_FOUND',
        message: normalized.message || 'Scope or resource not found',
        requestId
      },
      nowMs
    )
  }

  return buildFailureEntry(
    cacheKey,
    kind,
    workspaceId,
    projectId,
    actorId,
    'unavailable',
    {
      status: httpStatus,
      kind: 'unavailable',
      reason: reason || (raw.code === 'ECONNABORTED' ? 'TIMEOUT' : 'DEPENDENCY_UNAVAILABLE'),
      message: normalized.message || 'Permission service unavailable',
      requestId
    },
    nowMs
  )
}

function validateAccessResponse(
  resp: IdentityAccess | IdentityWorkspaceAccess | null | undefined,
  cacheKey: string,
  kind: 'workspace' | 'project',
  workspaceId: string,
  projectId: string | null,
  actorId: string,
  nowMs: number
): AccessProbeEntry {
  const fail = (
    failKind: AccessUnavailableKind,
    reason: string,
    message: string
  ): AccessProbeEntry =>
    buildFailureEntry(cacheKey, kind, workspaceId, projectId, actorId, 'unavailable', {
      status: 0,
      kind: failKind,
      reason,
      message
    }, nowMs)

  if (!resp || typeof resp !== 'object') {
    return fail('malformed', 'MALFORMED_ACCESS_RESPONSE', 'Malformed access response')
  }
  const actions = normalizeActionArray(resp.actions)
  if (actions === null) {
    return fail('malformed', 'MALFORMED_ACTIONS', 'Malformed access actions')
  }

  const projResp = resp as IdentityAccess
  let issuableKeyActions: string[] | null = null
  if (kind === 'project' && projResp.issuable_key_actions !== undefined) {
    issuableKeyActions = normalizeActionArray(projResp.issuable_key_actions)
    if (issuableKeyActions === null) {
      return fail('malformed', 'MALFORMED_ISSUABLE_ACTIONS', 'Malformed issuable key actions')
    }
  }

  const wsMismatch = resp.workspace_id !== undefined && String(resp.workspace_id) !== workspaceId
  const projMismatch =
    kind === 'project'
      ? resp.project_id !== undefined && String(resp.project_id) !== projectId
      : resp.project_id !== undefined && resp.project_id !== null && resp.project_id !== ''
  const actorMismatch = Boolean(actorId && resp.actor_id !== undefined && String(resp.actor_id) !== actorId)

  if (wsMismatch || projMismatch || actorMismatch) {
    return fail('malformed', 'ACCESS_SCOPE_MISMATCH', 'Access response does not match requested scope or actor')
  }

  const expiry = parseValidFutureExpiry(resp.expires_at, nowMs)
  if (!expiry.valid) {
    return fail('malformed', 'MALFORMED_ACCESS_EXPIRY', 'Malformed access expiry')
  }
  if (expiry.expired) {
    return fail('expired', 'ACCESS_EXPIRED', 'Access decision has expired')
  }

  const wsResp = resp as IdentityWorkspaceAccess
  const grantableActions =
    kind === 'workspace' && wsResp.grantable_actions !== undefined
      ? (normalizeActionArray(wsResp.grantable_actions) ?? [])
      : []

  return {
    ...buildEntryBase(cacheKey, kind, workspaceId, projectId, String(resp.actor_id ?? actorId), 'ready', nowMs),
    actions,
    issuableKeyActions,
    membershipRole: kind === 'workspace' && typeof wsResp.membership_role === 'string' ? wsResp.membership_role : null,
    membershipRoleSource:
      kind === 'workspace'
        ? (typeof wsResp.role_source === 'string'
            ? wsResp.role_source
            : typeof wsResp.membership_role_source === 'string'
              ? wsResp.membership_role_source
              : null)
        : null,
    workspaceAdmin: kind === 'workspace' ? Boolean(wsResp.effective_admin ?? wsResp.workspace_admin) : false,
    grantableActions,
    scopeVersion: resp.scope_version ? String(resp.scope_version) : null,
    expiresAtMs: expiry.ms
  }
}

export const KNOWN_DELEGABLE_KEY_ACTIONS = [
  'text.chat',
  'text.embeddings',
  'image.generate',
  'usage.read',
  'tasks.read',
  'models.read'
] as const

export function getIssuableKeyActions(
  entry: AccessProbeEntry | null | undefined,
  nowMs = Date.now()
): { status: PermissionProbeState; actions: string[] } {
  if (!entry || entry.status === 'idle' || entry.status === 'loading') {
    return { status: 'loading', actions: [] }
  }
  if (entry.status === 'denied') {
    return { status: 'denied', actions: [] }
  }
  if (entry.status === 'unavailable' || !isAccessEntryFresh(entry, nowMs)) {
    return { status: 'unavailable', actions: [] }
  }
  if (entry.issuableKeyActions !== null) {
    const normalized = entry.issuableKeyActions.filter((act) => act.trim() !== '')
    return {
      status: normalized.length > 0 ? 'allowed' : 'denied',
      actions: normalized
    }
  }
  if (entry.actions.includes('*')) {
    return { status: 'allowed', actions: [...KNOWN_DELEGABLE_KEY_ACTIONS] }
  }
  const intersection = KNOWN_DELEGABLE_KEY_ACTIONS.filter((act) => entry.actions.includes(act))
  return {
    status: intersection.length > 0 ? 'allowed' : 'denied',
    actions: intersection
  }
}

export function isAccessEntryFresh(entry: AccessProbeEntry | null | undefined, nowMs = Date.now()): boolean {
  if (!entry || entry.status !== 'ready') return false
  if (entry.expiresAtMs === null) return false
  return entry.expiresAtMs > nowMs
}

export function getAccessActionState(
  entry: AccessProbeEntry | null | undefined,
  action: string,
  nowMs = Date.now()
): PermissionProbeState {
  if (!entry || entry.status === 'idle' || entry.status === 'loading') {
    return 'loading'
  }
  if (entry.status === 'denied') {
    return 'denied'
  }
  if (entry.status === 'unavailable') {
    return 'unavailable'
  }
  if (!isAccessEntryFresh(entry, nowMs)) {
    return 'unavailable'
  }
  if (entry.actions.includes(action) || entry.actions.includes('*')) {
    return 'allowed'
  }
  return 'denied'
}

export function getCachedProjectAccess(
  workspaceId: string,
  projectId: string,
  actorId?: string
): AccessProbeEntry | null {
  return accessCache[buildAccessCacheKey(workspaceId, projectId, actorId)] ?? null
}

export function getCachedWorkspaceAccess(
  workspaceId: string,
  actorId?: string
): AccessProbeEntry | null {
  return accessCache[buildAccessCacheKey(workspaceId, null, actorId)] ?? null
}

async function runAccessProbe(
  kind: 'workspace' | 'project',
  workspaceId: string,
  projectId: string | null,
  options?: { force?: boolean; actorId?: string; nowMs?: () => number }
): Promise<AccessProbeEntry> {
  attachSessionListener()
  const now = options?.nowMs ?? (() => Date.now())
  const actorId = options?.actorId ?? resolveProbeActorId()
  const cacheKey = buildAccessCacheKey(workspaceId, projectId, actorId)

  if (!workspaceId || (kind === 'project' && !projectId)) {
    return buildFailureEntry(
      cacheKey,
      kind,
      workspaceId,
      projectId,
      actorId,
      'unavailable',
      { status: 404, kind: 'not_found', reason: 'SCOPE_UNAVAILABLE', message: 'Scope missing' },
      now()
    )
  }

  const fetcher =
    kind === 'project'
      ? typeof identityApi.getProjectAccess === 'function'
        ? (signal: AbortSignal) => identityApi.getProjectAccess(workspaceId, projectId!, signal)
        : null
      : typeof identityApi.getWorkspaceAccess === 'function'
        ? (signal: AbortSignal) => identityApi.getWorkspaceAccess(workspaceId, signal)
        : null

  if (!fetcher) {
    const absent = buildFailureEntry(
      cacheKey,
      kind,
      workspaceId,
      projectId,
      actorId,
      'unavailable',
      { status: 0, kind: 'unavailable', reason: 'CLIENT_METHOD_MISSING', message: 'Access probe method unavailable' },
      now()
    )
    accessCache[cacheKey] = absent
    return absent
  }

  const existing = accessCache[cacheKey]
  if (!options?.force && isAccessEntryFresh(existing, now())) {
    return existing
  }

  const existingInFlight = inFlightProbes.get(cacheKey)
  if (!options?.force && existingInFlight) {
    return existingInFlight.promise
  }
  if (options?.force && existingInFlight) {
    existingInFlight.controller.abort()
    inFlightProbes.delete(cacheKey)
  }

  const generation = ++globalProbeGeneration
  const controller = new AbortController()
  accessCache[cacheKey] = buildEntryBase(cacheKey, kind, workspaceId, projectId, actorId, 'loading', now())

  let rawPromise: Promise<IdentityWorkspaceAccess | IdentityAccess>
  try {
    rawPromise = Promise.resolve(fetcher(controller.signal))
  } catch (syncError) {
    rawPromise = Promise.reject(syncError)
  }

  const promise = rawPromise
    .then((resp) => {
      if (controller.signal.aborted || inFlightProbes.get(cacheKey)?.generation !== generation) {
        return accessCache[cacheKey] ?? buildFailureEntry(
          cacheKey,
          kind,
          workspaceId,
          projectId,
          actorId,
          'unavailable',
          { status: 0, kind: 'unavailable', reason: 'STALE_PROBE', message: 'Superseded access probe' },
          now()
        )
      }
      const entry = validateAccessResponse(resp, cacheKey, kind, workspaceId, projectId, actorId, now())
      accessCache[cacheKey] = entry
      return entry
    })
    .catch((err) => {
      if (isSilentRequestError(err) || inFlightProbes.get(cacheKey)?.generation !== generation) {
        return accessCache[cacheKey] ?? buildFailureEntry(
          cacheKey,
          kind,
          workspaceId,
          projectId,
          actorId,
          'unavailable',
          { status: 0, kind: 'unavailable', reason: 'PROBE_ABORTED', message: 'Access probe canceled' },
          now()
        )
      }
      const entry = mapProbeError(err, cacheKey, kind, workspaceId, projectId, actorId, now())
      accessCache[cacheKey] = entry
      return entry
    })
    .finally(() => {
      if (inFlightProbes.get(cacheKey)?.generation === generation) {
        inFlightProbes.delete(cacheKey)
      }
    })

  inFlightProbes.set(cacheKey, { generation, controller, promise })
  return promise
}

export function ensureProjectAccess(
  workspaceId: string,
  projectId: string,
  options?: { force?: boolean; actorId?: string; nowMs?: () => number }
): Promise<AccessProbeEntry> {
  return runAccessProbe('project', workspaceId, projectId, options)
}

export function ensureWorkspaceAccess(
  workspaceId: string,
  options?: { force?: boolean; actorId?: string; nowMs?: () => number }
): Promise<AccessProbeEntry> {
  return runAccessProbe('workspace', workspaceId, null, options)
}

async function recheckScopedActionBeforeMutation(
  kind: 'workspace' | 'project',
  workspaceId: string,
  projectId: string | null,
  action: string,
  options?: { actorId?: string; nowMs?: () => number }
): Promise<{ state: PermissionProbeState; entry: AccessProbeEntry }> {
  const nowMs = options?.nowMs ?? (() => Date.now())
  const entry = await runAccessProbe(kind, workspaceId, projectId, {
    force: false,
    actorId: options?.actorId,
    nowMs
  })
  let state = getAccessActionState(entry, action, nowMs())
  if (state === 'unavailable' && entry.error?.kind === 'expired') {
    const refreshed = await runAccessProbe(kind, workspaceId, projectId, {
      force: true,
      actorId: options?.actorId,
      nowMs
    })
    state = getAccessActionState(refreshed, action, nowMs())
    return { state, entry: refreshed }
  }
  return { state, entry }
}

export function recheckProjectActionBeforeMutation(
  workspaceId: string,
  projectId: string,
  action: string,
  options?: { actorId?: string; nowMs?: () => number }
): Promise<{ state: PermissionProbeState; entry: AccessProbeEntry }> {
  return recheckScopedActionBeforeMutation('project', workspaceId, projectId, action, options)
}

export function recheckWorkspaceActionBeforeMutation(
  workspaceId: string,
  action: string,
  options?: { actorId?: string; nowMs?: () => number }
): Promise<{ state: PermissionProbeState; entry: AccessProbeEntry }> {
  return recheckScopedActionBeforeMutation('workspace', workspaceId, null, action, options)
}
