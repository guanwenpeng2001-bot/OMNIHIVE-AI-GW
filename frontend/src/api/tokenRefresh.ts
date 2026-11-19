import axios from 'axios'
import type { ApiResponse } from '@/types/common'
import { getAPIBaseURL } from './url'

export const StorageKeys = {
  AUTH_TOKEN: 'omnihive.auth.token.v1',
  AUTH_USER: 'omnihive.auth.user.v1',
  AUTH_REFRESH_TOKEN: 'omnihive.auth.refresh_token.v1',
  AUTH_EXPIRES_AT: 'omnihive.auth.expires_at.v1',
  AUTH_SESSION_REV: 'omnihive.auth.session_rev.v1',
  AUTH_PENDING_SESSION: 'omnihive.auth.pending_session.v1',
  AUTH_EXPIRED_NOTICE: 'omnihive.auth.expired_notice.v1',
  AUTH_REMEMBERED_EMAIL: 'omnihive.auth.remembered_email.v1',
  AUTH_LOGIN_AGREEMENT: 'omnihive.auth.login_agreement.v1',
  AUTH_REGISTER_DRAFT: 'omnihive.auth.register_draft.v1',
  AUTH_OAUTH_PENDING_PROVIDER: 'omnihive.auth.oauth_pending_provider.v1',
  AUTH_OAUTH_AFF_CODE: 'omnihive.auth.oauth_aff_code.v1',
  AUTH_AFFILIATE_REF: 'omnihive.auth.affiliate_ref.v1',
  THEME_MODE: 'omnihive.theme.mode.v1',
  UI_DENSITY: 'omnihive.ui.density.v1',
  UI_SIDEBAR_COLLAPSED: 'omnihive.ui.sidebar_collapsed.v1',
  SCOPE_SELECTION: 'omnihive.scope.selection.v1',
  WORKSPACE_ID: 'omnihive.workspace_id.v1',
  PROJECT_ID: 'omnihive.project_id.v1',
  ROUTER_CHUNK_RELOAD: 'omnihive.router.chunk_reload.v1',
  OPS_ENABLED_CACHE: 'omnihive.ops.enabled.v1',
  ONBOARDING_ADMIN_GUIDE: 'omnihive.onboarding.admin_guide.v1',
  ONBOARDING_USER_GUIDE: 'omnihive.onboarding.user_guide.v1',
  MIGRATION_LEDGER: 'omnihive.migration.ledger.v1'
} as const

export const LegacyStorageKeys = {
  AUTH_TOKEN: 'auth_token',
  AUTH_USER: 'auth_user',
  AUTH_REFRESH_TOKEN: 'refresh_token',
  AUTH_EXPIRES_AT: 'token_expires_at',
  AUTH_SESSION_REV: 'auth_session_revision',
  AUTH_PENDING_SESSION: 'pending_auth_session',
  AUTH_EXPIRED_NOTICE: 'auth_expired',
  AUTH_REMEMBERED_EMAIL: 'auth_remembered_email',
  AUTH_LOGIN_AGREEMENT: 'sub2api_login_agreement_consent',
  AUTH_REGISTER_DRAFT: 'register_data',
  AUTH_OAUTH_PENDING_PROVIDER: 'email_oauth_pending_provider',
  AUTH_OAUTH_AFF_CODE: 'oauth_aff_code',
  AUTH_AFFILIATE_REF: 'affiliate_referral_code',
  THEME_MODES: ['omnihive.mode', 'theme'] as const,
  THEME_STYLE: 'omnihive.style',
  UI_DENSE: 'omnihive.dense',
  UI_SPEC: 'omnihive.spec',
  ROUTER_CHUNK_RELOAD: 'chunk_reload_attempted',
  OPS_ENABLED_CACHE: 'ops_monitoring_enabled_cached'
} as const

export type VersionedStorageMode = 'migrate' | 'bridge'

export interface VersionedStorageOptions {
  key: string
  legacyKeys?: readonly string[]
  storage?: 'local' | 'session'
  mode?: VersionedStorageMode
  migrateValue?: (legacyValue: string, legacyKey: string) => string | null
}

export interface VersionedStorageAdapter {
  readonly key: string
  readonly legacyKeys: readonly string[]
  get(): string | null
  set(value: string): void
  remove(): void
  getJSON<T>(fallback: T): T
  setJSON(value: unknown): void
}

const bridgeSnapshots = new Map<string, { v1: string | null; legacy: string | null }>()

function resolveBrowserStorage(kind: 'local' | 'session'): Storage | null {
  try {
    if (kind === 'session') {
      return typeof sessionStorage !== 'undefined' ? sessionStorage : null
    }
    return typeof localStorage !== 'undefined' ? localStorage : null
  } catch {
    return null
  }
}

function safeStorageGet(store: Storage | null, key: string): string | null {
  if (!store) return null
  try {
    return store.getItem(key)
  } catch {
    return null
  }
}

function safeStorageSet(store: Storage | null, key: string, value: string): void {
  if (!store) return
  try {
    store.setItem(key, value)
  } catch {
    // Ignore quota or disabled-storage errors
  }
}

function safeStorageRemove(store: Storage | null, key: string): void {
  if (!store) return
  try {
    store.removeItem(key)
  } catch {
    // Ignore storage removal errors
  }
}

function recordKeyMigration(store: Storage | null, targetKey: string): void {
  if (!store) return
  try {
    const raw = store.getItem(StorageKeys.MIGRATION_LEDGER)
    const parsed = raw ? (JSON.parse(raw) as Record<string, number>) : {}
    if (parsed && typeof parsed === 'object' && !parsed[targetKey]) {
      parsed[targetKey] = 1
      store.setItem(StorageKeys.MIGRATION_LEDGER, JSON.stringify(parsed))
    }
  } catch {
    // Non-critical diagnostic ledger
  }
}

function hasRecordedMigration(store: Storage | null, targetKey: string): boolean {
  if (!store) return false
  try {
    const raw = store.getItem(StorageKeys.MIGRATION_LEDGER)
    if (!raw) return false
    const parsed = JSON.parse(raw) as Record<string, number>
    return Boolean(parsed && typeof parsed === 'object' && parsed[targetKey])
  } catch {
    return false
  }
}

export function resetStorageAdapterSnapshotsForTests(): void {
  bridgeSnapshots.clear()
}

export function createVersionedStorage(options: VersionedStorageOptions): VersionedStorageAdapter {
  const {
    key,
    legacyKeys = [],
    storage = 'local',
    mode = 'migrate',
    migrateValue
  } = options
  const snapshotKey = `${storage}:${key}`
  const primaryLegacy = legacyKeys[0]

  function get(): string | null {
    const store = resolveBrowserStorage(storage)
    if (!store) return null

    if (mode === 'bridge') {
      const v1Val = safeStorageGet(store, key)
      const legacyRaw = primaryLegacy ? safeStorageGet(store, primaryLegacy) : null
      const legacyVal =
        legacyRaw !== null && primaryLegacy && migrateValue
          ? migrateValue(legacyRaw, primaryLegacy)
          : legacyRaw
      const prev = bridgeSnapshots.get(snapshotKey)

      if (v1Val === null && legacyVal === null) {
        for (const lk of legacyKeys.slice(1)) {
          const extraRaw = safeStorageGet(store, lk)
          if (extraRaw !== null) {
            const extraMigrated = migrateValue ? migrateValue(extraRaw, lk) : extraRaw
            safeStorageRemove(store, lk)
            if (extraMigrated !== null) {
              safeStorageSet(store, key, extraMigrated)
              if (primaryLegacy) safeStorageSet(store, primaryLegacy, extraMigrated)
              recordKeyMigration(store, key)
              bridgeSnapshots.set(snapshotKey, {
                v1: extraMigrated,
                legacy: primaryLegacy ? extraMigrated : null
              })
              return extraMigrated
            }
          }
        }
        bridgeSnapshots.set(snapshotKey, { v1: null, legacy: null })
        return null
      }

      let resolved: string | null
      const unmigratedLegacy =
        v1Val === null && legacyVal !== null && !hasRecordedMigration(store, key)
      if (!unmigratedLegacy && prev !== undefined) {
        const v1Changed = v1Val !== prev.v1
        const legacyChanged = legacyVal !== prev.legacy
        if (v1Changed && !legacyChanged) {
          resolved = v1Val
        } else if (legacyChanged && !v1Changed) {
          resolved = legacyVal
        } else {
          resolved = v1Val ?? legacyVal
        }
      } else {
        resolved = v1Val ?? legacyVal
      }

      for (const lk of legacyKeys.slice(1)) {
        if (safeStorageGet(store, lk) !== null) safeStorageRemove(store, lk)
      }

      if (resolved === null) {
        if (v1Val !== null) safeStorageRemove(store, key)
        for (const lk of legacyKeys) {
          if (safeStorageGet(store, lk) !== null) safeStorageRemove(store, lk)
        }
        bridgeSnapshots.set(snapshotKey, { v1: null, legacy: null })
        return null
      }

      if (v1Val !== resolved) {
        safeStorageSet(store, key, resolved)
        if (v1Val === null) {
          recordKeyMigration(store, key)
        }
      }
      if (primaryLegacy && legacyRaw !== resolved) {
        safeStorageSet(store, primaryLegacy, resolved)
      }
      bridgeSnapshots.set(snapshotKey, {
        v1: resolved,
        legacy: primaryLegacy ? resolved : null
      })
      return resolved
    }

    const current = safeStorageGet(store, key)
    if (current !== null) {
      for (const lk of legacyKeys) {
        if (safeStorageGet(store, lk) !== null) {
          safeStorageRemove(store, lk)
        }
      }
      return current
    }

    for (const lk of legacyKeys) {
      const rawLegacy = safeStorageGet(store, lk)
      if (rawLegacy !== null) {
        const nextVal = migrateValue ? migrateValue(rawLegacy, lk) : rawLegacy
        for (const oldKey of legacyKeys) {
          safeStorageRemove(store, oldKey)
        }
        if (nextVal !== null) {
          safeStorageSet(store, key, nextVal)
          recordKeyMigration(store, key)
        }
        return nextVal
      }
    }

    return null
  }

  function set(value: string): void {
    const store = resolveBrowserStorage(storage)
    if (!store) return
    safeStorageSet(store, key, value)
    if (mode === 'bridge' && primaryLegacy) {
      safeStorageSet(store, primaryLegacy, value)
      for (const lk of legacyKeys.slice(1)) {
        safeStorageRemove(store, lk)
      }
      bridgeSnapshots.set(snapshotKey, { v1: value, legacy: value })
      return
    }
    for (const lk of legacyKeys) {
      safeStorageRemove(store, lk)
    }
  }

  function remove(): void {
    const store = resolveBrowserStorage(storage)
    if (!store) return
    safeStorageRemove(store, key)
    for (const lk of legacyKeys) {
      safeStorageRemove(store, lk)
    }
    if (mode === 'bridge') {
      bridgeSnapshots.set(snapshotKey, { v1: null, legacy: null })
    }
  }

  function getJSON<T>(fallback: T): T {
    const raw = get()
    if (!raw) return fallback
    try {
      return JSON.parse(raw) as T
    } catch {
      return fallback
    }
  }

  function setJSON(value: unknown): void {
    try {
      set(JSON.stringify(value))
    } catch {
      // Ignore serialization errors
    }
  }

  return {
    key,
    legacyKeys,
    get,
    set,
    remove,
    getJSON,
    setJSON
  }
}

export const authStorage = {
  token: createVersionedStorage({
    key: StorageKeys.AUTH_TOKEN,
    legacyKeys: [LegacyStorageKeys.AUTH_TOKEN],
    mode: 'bridge'
  }),
  user: createVersionedStorage({
    key: StorageKeys.AUTH_USER,
    legacyKeys: [LegacyStorageKeys.AUTH_USER],
    mode: 'bridge'
  }),
  refreshToken: createVersionedStorage({
    key: StorageKeys.AUTH_REFRESH_TOKEN,
    legacyKeys: [LegacyStorageKeys.AUTH_REFRESH_TOKEN],
    mode: 'bridge'
  }),
  expiresAt: createVersionedStorage({
    key: StorageKeys.AUTH_EXPIRES_AT,
    legacyKeys: [LegacyStorageKeys.AUTH_EXPIRES_AT],
    mode: 'bridge'
  }),
  sessionRevision: createVersionedStorage({
    key: StorageKeys.AUTH_SESSION_REV,
    legacyKeys: [LegacyStorageKeys.AUTH_SESSION_REV],
    mode: 'bridge'
  }),
  pendingSession: createVersionedStorage({
    key: StorageKeys.AUTH_PENDING_SESSION,
    legacyKeys: [LegacyStorageKeys.AUTH_PENDING_SESSION],
    mode: 'bridge'
  }),
  expiredNotice: createVersionedStorage({
    key: StorageKeys.AUTH_EXPIRED_NOTICE,
    legacyKeys: [LegacyStorageKeys.AUTH_EXPIRED_NOTICE],
    storage: 'session',
    mode: 'bridge'
  }),
  rememberedEmail: createVersionedStorage({
    key: StorageKeys.AUTH_REMEMBERED_EMAIL,
    legacyKeys: [LegacyStorageKeys.AUTH_REMEMBERED_EMAIL],
    mode: 'migrate'
  }),
  loginAgreement: createVersionedStorage({
    key: StorageKeys.AUTH_LOGIN_AGREEMENT,
    legacyKeys: [LegacyStorageKeys.AUTH_LOGIN_AGREEMENT],
    mode: 'migrate'
  }),
  registerDraft: createVersionedStorage({
    key: StorageKeys.AUTH_REGISTER_DRAFT,
    legacyKeys: [LegacyStorageKeys.AUTH_REGISTER_DRAFT],
    storage: 'session',
    mode: 'migrate'
  }),
  oauthPendingProvider: createVersionedStorage({
    key: StorageKeys.AUTH_OAUTH_PENDING_PROVIDER,
    legacyKeys: [LegacyStorageKeys.AUTH_OAUTH_PENDING_PROVIDER],
    storage: 'session',
    mode: 'migrate'
  }),
  oauthAffCode: createVersionedStorage({
    key: StorageKeys.AUTH_OAUTH_AFF_CODE,
    legacyKeys: [LegacyStorageKeys.AUTH_OAUTH_AFF_CODE],
    storage: 'session',
    mode: 'migrate'
  }),
  affiliateRef: createVersionedStorage({
    key: StorageKeys.AUTH_AFFILIATE_REF,
    legacyKeys: [LegacyStorageKeys.AUTH_AFFILIATE_REF],
    mode: 'migrate'
  })
} as const

export const opsEnabledStorage = createVersionedStorage({
  key: StorageKeys.OPS_ENABLED_CACHE,
  legacyKeys: [LegacyStorageKeys.OPS_ENABLED_CACHE],
  mode: 'bridge'
})

export const themeModeStorage = createVersionedStorage({
  key: StorageKeys.THEME_MODE,
  legacyKeys: ['theme', 'omnihive.mode'],
  mode: 'bridge',
  migrateValue: (value) => {
    const normalized = value.trim().toLowerCase()
    if (normalized === 'dark' || normalized === 'light' || normalized === 'system') {
      return normalized
    }
    return null
  }
})

export const uiDensityStorage = createVersionedStorage({
  key: StorageKeys.UI_DENSITY,
  legacyKeys: [LegacyStorageKeys.UI_DENSE],
  mode: 'migrate',
  migrateValue: (value) => {
    const normalized = value.trim().toLowerCase()
    if (normalized === '1' || normalized === 'true' || normalized === 'compact') {
      return 'compact'
    }
    if (normalized === '0' || normalized === 'false' || normalized === 'comfort') {
      return 'comfort'
    }
    return null
  }
})

export const sidebarCollapsedStorage = createVersionedStorage({
  key: StorageKeys.UI_SIDEBAR_COLLAPSED,
  mode: 'migrate'
})

export const scopeSelectionStorage = createVersionedStorage({
  key: StorageKeys.SCOPE_SELECTION,
  mode: 'migrate'
})

export const routerChunkReloadStorage = createVersionedStorage({
  key: StorageKeys.ROUTER_CHUNK_RELOAD,
  legacyKeys: [LegacyStorageKeys.ROUTER_CHUNK_RELOAD],
  storage: 'session',
  mode: 'migrate'
})

export const ALL_AUTH_STORAGE_WATCH_KEYS: readonly string[] = [
  StorageKeys.AUTH_TOKEN,
  StorageKeys.AUTH_USER,
  StorageKeys.AUTH_REFRESH_TOKEN,
  StorageKeys.AUTH_EXPIRES_AT,
  StorageKeys.AUTH_SESSION_REV,
  StorageKeys.AUTH_PENDING_SESSION,
  LegacyStorageKeys.AUTH_TOKEN,
  LegacyStorageKeys.AUTH_USER,
  LegacyStorageKeys.AUTH_REFRESH_TOKEN,
  LegacyStorageKeys.AUTH_EXPIRES_AT,
  LegacyStorageKeys.AUTH_SESSION_REV,
  LegacyStorageKeys.AUTH_PENDING_SESSION
]

export function clearPersistedAuthCredentials(): void {
  authStorage.token.remove()
  authStorage.refreshToken.remove()
  authStorage.user.remove()
  authStorage.expiresAt.remove()
}

export const TOKEN_REFRESH_LOCK_NAME = 'omnihive.auth.token_refresh.v1'
export const LEGACY_TOKEN_REFRESH_LOCK_NAME = 'sub2api-auth-token-refresh'
const TOKEN_REFRESH_TIMEOUT_MS = 30_000
const PEER_REFRESH_WAIT_MS = 1_000
const PEER_REFRESH_GRACE_MS = 1_000
const PEER_REFRESH_POLL_MS = 25

export interface RefreshTokenResponse {
  access_token: string
  refresh_token: string
  expires_in: number
  token_type: string
}

export interface RefreshAuthTokensOptions {
  /** Access token attached to the request that received a 401 response. */
  failedAccessToken?: string | null
  session?: AuthSessionSnapshot
}

export const AUTH_SESSION_KEY = StorageKeys.AUTH_SESSION_REV
export const LEGACY_AUTH_SESSION_KEY = LegacyStorageKeys.AUTH_SESSION_REV
export const AUTH_SESSION_EVENT = 'auth-session-changed'

export interface AuthSessionSnapshot {
  revision: string | null
  accessToken: string | null
  userID: number | null
}

export function captureAuthSession(): AuthSessionSnapshot {
  return {
    revision: authStorage.sessionRevision.get(),
    accessToken: authStorage.token.get(),
    userID: getStoredUserID()
  }
}

export function isAuthSessionCurrent(snapshot: AuthSessionSnapshot): boolean {
  const current = captureAuthSession()
  return current.revision === snapshot.revision && current.userID === snapshot.userID &&
    !!current.accessToken === !!snapshot.accessToken &&
    (snapshot.revision !== null || snapshot.userID !== null || current.accessToken === snapshot.accessToken)
}

export function authSessionChangedError(): Error & { status: number; code: string } {
  return Object.assign(new Error('Session changed during token refresh'), {
    status: 401, code: 'AUTH_SESSION_CHANGED'
  })
}

export function advanceAuthSession(): void {
  // getRandomValues also works on HTTP installations where randomUUID is unavailable.
  const revision = Array.from(crypto.getRandomValues(new Uint32Array(4)), n => n.toString(16)).join('-')
  authStorage.sessionRevision.set(revision)
  window.dispatchEvent(new Event(AUTH_SESSION_EVENT))
}

interface AuthSnapshot extends AuthSessionSnapshot {
  accessToken: string | null
  refreshToken: string
  expiresAt: number
  userID: number | null
}

let inFlightRefresh: Promise<RefreshTokenResponse> | null = null
let inFlightSession: AuthSessionSnapshot | null = null

function getStoredUserID(): number | null {
  const rawUser = authStorage.user.get()
  if (!rawUser) {
    return null
  }

  try {
    const id = Number((JSON.parse(rawUser) as { id?: unknown }).id)
    return Number.isFinite(id) && id > 0 ? id : null
  } catch {
    return null
  }
}

function readAuthSnapshot(): AuthSnapshot {
  const refreshToken = authStorage.refreshToken.get()
  if (!refreshToken) {
    throw new Error('No refresh token available')
  }

  return {
    ...captureAuthSession(),
    accessToken: authStorage.token.get(),
    refreshToken,
    expiresAt: Number(authStorage.expiresAt.get()),
    userID: getStoredUserID()
  }
}

function readStoredTokenPair(snapshot: AuthSnapshot): RefreshTokenResponse | null {
  const accessToken = authStorage.token.get()
  const refreshToken = authStorage.refreshToken.get()
  const expiresAt = Number(authStorage.expiresAt.get())

  if (
    !accessToken ||
    !refreshToken ||
    !Number.isFinite(expiresAt) ||
    expiresAt <= Date.now() ||
    !isAuthSessionCurrent(snapshot) ||
    getStoredUserID() !== snapshot.userID
  ) {
    return null
  }

  return {
    access_token: accessToken,
    refresh_token: refreshToken,
    expires_in: Math.max(1, Math.ceil((expiresAt - Date.now()) / 1000)),
    token_type: 'Bearer'
  }
}

function readPeerRefreshResult(
  snapshot: AuthSnapshot,
  failedAccessToken?: string | null
): RefreshTokenResponse | null {
  const storedPair = readStoredTokenPair(snapshot)
  if (!storedPair) {
    return null
  }

  if (storedPair.refresh_token !== snapshot.refreshToken) {
    return storedPair
  }

  if (
    failedAccessToken &&
    snapshot.accessToken !== failedAccessToken &&
    storedPair.access_token === snapshot.accessToken
  ) {
    return storedPair
  }

  return null
}

async function waitForPeerRefresh(
  snapshot: AuthSnapshot,
  failedAccessToken?: string | null,
  deadline = Date.now() + PEER_REFRESH_WAIT_MS
): Promise<RefreshTokenResponse | null> {
  while (Date.now() < deadline) {
    const peerResult = readPeerRefreshResult(snapshot, failedAccessToken)
    if (peerResult) {
      return peerResult
    }
    await new Promise((resolve) => window.setTimeout(resolve, PEER_REFRESH_POLL_MS))
  }

  return readPeerRefreshResult(snapshot, failedAccessToken)
}

function persistTokenPair(tokens: RefreshTokenResponse): void {
  authStorage.token.set(tokens.access_token)
  authStorage.expiresAt.set(String(Date.now() + tokens.expires_in * 1000))
  // The rotating refresh token is written last so other tabs can treat its change as a commit marker.
  authStorage.refreshToken.set(tokens.refresh_token)
}

async function requestTokenPair(
  snapshot: AuthSnapshot,
  failedAccessToken?: string | null,
  mayHaveUncoordinatedPeer = false
): Promise<RefreshTokenResponse> {
  // If this request loses a one-time-token race, the winning peer can legitimately take as long
  // as our own HTTP timeout to publish its replacement token. Keep the recovery window tied to
  // that timeout instead of an arbitrary short delay.
  const peerRefreshDeadline = Date.now() + TOKEN_REFRESH_TIMEOUT_MS + PEER_REFRESH_GRACE_MS

  try {
    const response = await axios.post<ApiResponse<RefreshTokenResponse>>(
      `${getAPIBaseURL()}/auth/refresh`,
      { refresh_token: snapshot.refreshToken },
      { headers: { 'Content-Type': 'application/json' }, timeout: TOKEN_REFRESH_TIMEOUT_MS }
    )
    const payload = response.data
    if (payload.code !== 0 || !payload.data) {
      throw new Error(payload.message || 'Token refresh failed')
    }

    if (
      !isAuthSessionCurrent(snapshot) ||
      authStorage.refreshToken.get() !== snapshot.refreshToken ||
      getStoredUserID() !== snapshot.userID
    ) {
      const peerResult = readPeerRefreshResult(snapshot, failedAccessToken)
      if (peerResult) {
        return peerResult
      }
      throw authSessionChangedError()
    }

    persistTokenPair(payload.data)
    return payload.data
  } catch (error) {
    // A peer tab may have rotated the one-time refresh token while this request was in flight.
    // A 4xx response can arrive quickly while the winning peer's response is still in flight, so
    // wait through the shared request deadline before treating the session as expired. Transient
    // non-4xx failures retain the short reconciliation window.
    const responseStatus = (error as { response?: { status?: unknown } }).response?.status
    const isTokenRejection =
      typeof responseStatus === 'number' && responseStatus >= 400 && responseStatus < 500
    const peerResult = await waitForPeerRefresh(
      snapshot,
      failedAccessToken,
      isTokenRejection && mayHaveUncoordinatedPeer
        ? peerRefreshDeadline
        : Date.now() + PEER_REFRESH_WAIT_MS
    )
    if (peerResult) {
      return peerResult
    }
    throw error
  }
}

async function runRefresh(options: RefreshAuthTokensOptions): Promise<RefreshTokenResponse> {
  if (options.session && !isAuthSessionCurrent(options.session)) throw authSessionChangedError()
  const snapshot = readAuthSnapshot()
  const refresh = async (mayHaveUncoordinatedPeer = false): Promise<RefreshTokenResponse> => {
    if (!isAuthSessionCurrent(snapshot)) throw authSessionChangedError()
    const peerResult = readPeerRefreshResult(snapshot, options.failedAccessToken)
    if (peerResult) {
      return peerResult
    }
    return requestTokenPair(snapshot, options.failedAccessToken, mayHaveUncoordinatedPeer)
  }

  if (typeof navigator !== 'undefined' && navigator.locks) {
    return navigator.locks.request(TOKEN_REFRESH_LOCK_NAME, () =>
      navigator.locks.request(LEGACY_TOKEN_REFRESH_LOCK_NAME, () => refresh(false))
    )
  }

  return refresh(true)
}

/**
 * Refresh and persist the browser session.
 *
 * Calls in the same document share one promise. Web Locks serialize refreshes across tabs, while
 * the token snapshot check adopts a peer's newly rotated token instead of logging the user out.
 */
export function refreshAuthTokens(
  options: RefreshAuthTokensOptions = {}
): Promise<RefreshTokenResponse> {
  if (!options.session && options.failedAccessToken &&
    options.failedAccessToken !== authStorage.token.get()) {
    return Promise.reject(authSessionChangedError())
  }
  if (options.session && !isAuthSessionCurrent(options.session)) {
    return Promise.reject(authSessionChangedError())
  }
  if (inFlightRefresh && inFlightSession && isAuthSessionCurrent(inFlightSession)) {
    return inFlightRefresh
  }

  inFlightSession = captureAuthSession()
  const pending = runRefresh(options)
  inFlightRefresh = pending
  const clearPending = (): void => {
    if (inFlightRefresh === pending) {
      inFlightRefresh = null
    }
  }
  void pending.then(clearPending, clearPending)
  return pending
}
