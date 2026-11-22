import type { UiStepItem, UiStepStatus } from '@/components/ui'
import { defaultWorkBuddyDomain, type WorkBuddySite } from '@/api/admin/workbuddy'
import {
  resolveKiloAccountMode,
  resolveOpenCodeAccountMode,
  type KiloAccountMode,
  type OpenCodeAccountMode,
  type WorkBuddyOAuthDraft
} from '@/components/account/credentialsBuilder'
import {
  buildHeaderOverridesMap,
  getPrimaryVerificationDiagnostic,
  hasUnavailableVerificationEvidence,
  inferWorkBuddySiteFromEndpoint,
  isAllowedCredentialType,
  isOAuthWorkflowSupported,
  isVerificationFresh,
  validateConnectionEndpoint,
  validateHeaderOverrideRows,
  validateNetworkPolicyId
} from './rules'
import type {
  AsyncErrorContext,
  ConnectionCredentialInput,
  ConnectionItem,
  ConnectionPatch,
  ConnectionPreview,
  ConnectionResource,
  ConnectionSchema,
  ConnectionVerification,
  CredentialOperationMode,
  HeaderOverrideDraftRow
} from './types'

export type WizardStepId = 'provider' | 'credentials' | 'verify' | 'preview' | 'activate'

export const WIZARD_STEP_ORDER: readonly WizardStepId[] = [
  'provider',
  'credentials',
  'verify',
  'preview',
  'activate'
]

export interface PersistedConnectionSnapshot {
  name: string
  endpoint: string
  networkPolicyId: string
  accountMode: string
  headerOverrides: Record<string, string>
  modelIds: string[]
  poolIds: string[]
  concurrency: number
  workbuddyMetadata?: Record<string, string>
}

export interface ConnectionWizardState {
  mode: 'create' | 'edit'
  currentStep: WizardStepId
  connectionId: string | null
  version: string
  credentialVersion: string
  credentialState: 'unset' | 'unverified' | 'verified' | 'revoked'
  credentialLastFour: string | null
  connectionState: 'draft' | 'active' | 'disabled'
  provider: string
  schemaVersion: string
  authType: string
  name: string
  endpoint: string
  networkPolicyId: string
  headerOverrides: HeaderOverrideDraftRow[]
  credentialMode: CredentialOperationMode
  credentialSecret: string
  credentialRefreshToken: string
  workbuddySite: WorkBuddySite
  workbuddyOAuth: WorkBuddyOAuthDraft
  kiloMode: KiloAccountMode
  openCodeMode: OpenCodeAccountMode
  verification: ConnectionVerification | null
  verificationStale: boolean
  modelIds: string[]
  poolIds: string[]
  concurrency: number
  preview: ConnectionPreview | null
  previewStale: boolean
  confirmedVersion: string | null
  asyncError: AsyncErrorContext | null
  persistedSnapshot?: PersistedConnectionSnapshot | null
}

function normalizeSnapshotEndpoint(endpoint: string): string {
  return endpoint.trim().replace(/\/+$/, '')
}

function normalizeSnapshotPolicyId(policyId?: string | null): string {
  const trimmed = (policyId ?? '').trim()
  return trimmed.toLowerCase() === 'default' ? '' : trimmed
}

function normalizeWorkBuddySiteValue(raw?: string | null): WorkBuddySite | null {
  const value = String(raw ?? '').trim().toLowerCase()
  if (!value) return null
  if (
    value === 'global' ||
    value === 'intl' ||
    value === 'international' ||
    value === 'workbuddy-ai' ||
    value === 'workbuddy_ai'
  ) {
    return 'global'
  }
  if (value === 'cn' || value === 'china' || value === 'workbuddy') {
    return 'cn'
  }
  return null
}

function normalizeMetadataTimestamp(value?: string | number | null): string {
  if (value === null || value === undefined) return ''
  return String(value).trim()
}

export function buildWorkBuddyMetadataSnapshot(
  state: Pick<ConnectionWizardState, 'provider' | 'authType'> &
    Partial<Pick<ConnectionWizardState, 'workbuddySite' | 'workbuddyOAuth'>>
): Record<string, string> {
  const provider = state.provider.trim().toLowerCase()
  if (provider !== 'workbuddy') return {}
  const authType = state.authType.trim().toLowerCase()
  const site: WorkBuddySite = state.workbuddySite ?? 'cn'
  if (authType === 'api_key' || authType === 'apikey') {
    return { site }
  }
  if (authType === 'oauth') {
    const snapshot: Record<string, string> = { site }
    const oauth = state.workbuddyOAuth
    const uid = (oauth?.uid ?? '').trim()
    if (uid) snapshot.uid = uid
    const domain = (oauth?.domain ?? '').trim() || defaultWorkBuddyDomain(site)
    if (domain) snapshot.domain = domain
    const enterpriseId = (oauth?.enterprise_id ?? '').trim()
    if (enterpriseId) snapshot.enterprise_id = enterpriseId
    const expiresAt = (oauth?.expires_at ?? '').trim()
    if (expiresAt) snapshot.expires_at = expiresAt
    const refreshExpiresAt = (oauth?.refresh_expires_at ?? '').trim()
    if (refreshExpiresAt) snapshot.refresh_expires_at = refreshExpiresAt
    return snapshot
  }
  return {}
}

export function isPublicAccountMode(provider: string, accountMode: string): boolean {
  const normalizedProvider = provider.trim().toLowerCase()
  const normalizedMode = accountMode.trim().toLowerCase()
  if (normalizedProvider === 'kilo') return normalizedMode === 'free'
  if (normalizedProvider === 'opencode_go') return normalizedMode === 'zen_free'
  return false
}

export function isCrossingPublicPaidModeBoundary(
  state: Pick<ConnectionWizardState, 'provider' | 'authType' | 'kiloMode' | 'openCodeMode' | 'persistedSnapshot'>
): boolean {
  const persistedMode = state.persistedSnapshot?.accountMode?.trim() ?? ''
  const nextMode = resolveWizardAccountMode(state) ?? ''
  if (!persistedMode || !nextMode || persistedMode === nextMode) {
    return false
  }
  return isPublicAccountMode(state.provider, persistedMode) !== isPublicAccountMode(state.provider, nextMode)
}

export function isSwitchingFromPublicToPaidMode(
  state: Pick<ConnectionWizardState, 'provider' | 'authType' | 'kiloMode' | 'openCodeMode' | 'persistedSnapshot'>
): boolean {
  const persistedMode = state.persistedSnapshot?.accountMode?.trim() ?? ''
  const nextMode = resolveWizardAccountMode(state) ?? ''
  if (!persistedMode || !nextMode || persistedMode === nextMode) {
    return false
  }
  return isPublicAccountMode(state.provider, persistedMode) && !isPublicAccountMode(state.provider, nextMode)
}

export function requiresCredentialClearBeforeModePatch(
  state: Pick<
    ConnectionWizardState,
    'connectionId' | 'provider' | 'authType' | 'kiloMode' | 'openCodeMode' | 'credentialState' | 'persistedSnapshot'
  >
): boolean {
  if (!state.connectionId) return false
  if (!isCrossingPublicPaidModeBoundary(state)) return false
  return state.credentialState !== 'unset' && state.credentialState !== 'revoked'
}

export function resolveWizardAccountMode(
  state: Pick<ConnectionWizardState, 'provider' | 'authType' | 'kiloMode' | 'openCodeMode'>
): string | undefined {
  const provider = state.provider.trim().toLowerCase()
  const authType = state.authType.trim().toLowerCase()
  if (authType !== 'api_key' && authType !== 'apikey') {
    return undefined
  }
  if (provider === 'kilo') return state.kiloMode
  if (provider === 'opencode_go') return state.openCodeMode
  return undefined
}

export function createPersistedSnapshotFromState(
  state: Pick<
    ConnectionWizardState,
    'provider' | 'authType' | 'kiloMode' | 'openCodeMode' | 'name' | 'endpoint' | 'networkPolicyId' | 'headerOverrides' | 'modelIds' | 'poolIds' | 'concurrency'
  > &
    Partial<Pick<ConnectionWizardState, 'workbuddySite' | 'workbuddyOAuth'>>
): PersistedConnectionSnapshot {
  const workbuddyMetadata = buildWorkBuddyMetadataSnapshot(state)
  return {
    name: state.name.trim(),
    endpoint: normalizeSnapshotEndpoint(state.endpoint),
    networkPolicyId: normalizeSnapshotPolicyId(state.networkPolicyId),
    accountMode: resolveWizardAccountMode(state) ?? '',
    headerOverrides: buildHeaderOverridesMap(state.headerOverrides),
    modelIds: [...state.modelIds],
    poolIds: [...state.poolIds],
    concurrency: state.concurrency,
    ...(Object.keys(workbuddyMetadata).length > 0 ? { workbuddyMetadata } : {})
  }
}

function stringArraysEqual(a: readonly string[], b: readonly string[]): boolean {
  if (a.length !== b.length) return false
  for (let i = 0; i < a.length; i += 1) {
    if (a[i] !== b[i]) return false
  }
  return true
}

function stringMapsEqual(a: Record<string, string>, b: Record<string, string>): boolean {
  const keysA = Object.keys(a)
  const keysB = Object.keys(b)
  if (keysA.length !== keysB.length) return false
  for (const key of keysA) {
    if (!Object.prototype.hasOwnProperty.call(b, key) || a[key] !== b[key]) {
      return false
    }
  }
  return true
}

export function buildMinimalConnectionPatch(
  state: ConnectionWizardState,
  options: { includeAssignments?: boolean; supportsHeaderOverrides?: boolean } = {}
): ConnectionPatch | null {
  const includeAssignments = options.includeAssignments ?? true
  const supportsHeaderOverrides = options.supportsHeaderOverrides ?? false
  const baseline = state.persistedSnapshot ?? null
  const nextName = state.name.trim()
  const nextEndpoint = normalizeSnapshotEndpoint(state.endpoint)
  const nextPolicyId = normalizeSnapshotPolicyId(state.networkPolicyId)
  const nextAccountMode = resolveWizardAccountMode(state) ?? ''
  const nextHeaders = buildHeaderOverridesMap(state.headerOverrides)

  if (!baseline) {
    const fallbackPatch: ConnectionPatch = {
      name: nextName,
      endpoint: nextEndpoint,
      network_policy_id: nextPolicyId,
      ...(nextAccountMode ? { account_mode: nextAccountMode } : {}),
      ...(supportsHeaderOverrides ? { header_overrides: nextHeaders } : {})
    }
    if (includeAssignments) {
      fallbackPatch.model_ids = [...state.modelIds]
      fallbackPatch.pool_ids = [...state.poolIds]
      fallbackPatch.limits = { concurrency: state.concurrency }
    }
    return fallbackPatch
  }

  const patch: ConnectionPatch = {}
  let hasChanges = false

  if (nextName !== baseline.name) {
    patch.name = nextName
    hasChanges = true
  }
  if (nextEndpoint !== baseline.endpoint) {
    patch.endpoint = nextEndpoint
    hasChanges = true
  }
  if (nextPolicyId !== baseline.networkPolicyId) {
    patch.network_policy_id = nextPolicyId
    hasChanges = true
  }
  if (nextAccountMode && nextAccountMode !== baseline.accountMode) {
    patch.account_mode = nextAccountMode
    hasChanges = true
  }
  if (supportsHeaderOverrides && !stringMapsEqual(nextHeaders, baseline.headerOverrides)) {
    patch.header_overrides = nextHeaders
    hasChanges = true
  }

  if (includeAssignments) {
    if (!stringArraysEqual(state.modelIds, baseline.modelIds)) {
      patch.model_ids = [...state.modelIds]
      hasChanges = true
    }
    if (!stringArraysEqual(state.poolIds, baseline.poolIds)) {
      patch.pool_ids = [...state.poolIds]
      hasChanges = true
    }
    if (state.concurrency !== baseline.concurrency) {
      patch.limits = { concurrency: state.concurrency }
      hasChanges = true
    }
  }

  return hasChanges ? patch : null
}

export function createInitialWizardState(mode: 'create' | 'edit' = 'create'): ConnectionWizardState {
  return {
    mode,
    currentStep: 'provider',
    connectionId: null,
    version: '1',
    credentialVersion: '1',
    credentialState: 'unset',
    credentialLastFour: null,
    connectionState: 'draft',
    provider: '',
    schemaVersion: '1',
    authType: 'api_key',
    name: '',
    endpoint: '',
    networkPolicyId: '',
    headerOverrides: [],
    credentialMode: mode === 'edit' ? 'keep' : 'set',
    credentialSecret: '',
    credentialRefreshToken: '',
    workbuddySite: 'cn',
    workbuddyOAuth: {
      access_token: '',
      refresh_token: '',
      uid: '',
      domain: defaultWorkBuddyDomain('cn'),
      expires_at: ''
    },
    kiloMode: 'free',
    openCodeMode: 'zen',
    verification: null,
    verificationStale: false,
    modelIds: [],
    poolIds: [],
    concurrency: 4,
    preview: null,
    previewStale: false,
    confirmedVersion: null,
    asyncError: null,
    persistedSnapshot: null
  }
}

export function hydrateWizardFromResource(
  state: ConnectionWizardState,
  resource: ConnectionResource
): ConnectionWizardState {
  const credentialRedacted = Array.isArray(resource.redactions) && resource.redactions.includes('credential')
  const nextCredentialState = resource.credential?.state ?? (credentialRedacted ? 'unverified' : 'unset')
  const preserveCreateDraftMode =
    state.mode === 'create' && resource.state === 'draft' && nextCredentialState === 'unset'
  const headerOverrides: HeaderOverrideDraftRow[] = resource.header_overrides
    ? Object.entries(resource.header_overrides).map(([key, value]) => ({
        key,
        value: String(value ?? '')
      }))
    : state.headerOverrides
  const normalizedPolicyId = normalizeSnapshotPolicyId(resource.network_policy_id)
  const concurrency = resource.limits?.concurrency ?? 4
  const rawMeta = resource.metadata
  const hydratedSite: WorkBuddySite =
    resource.provider === 'workbuddy'
      ? (normalizeWorkBuddySiteValue(rawMeta?.site) ?? inferWorkBuddySiteFromEndpoint(resource.endpoint || ''))
      : 'cn'
  const hydratedWorkbuddyOAuth: WorkBuddyOAuthDraft =
    resource.provider === 'workbuddy'
      ? {
          access_token: '',
          refresh_token: '',
          uid: typeof rawMeta?.uid === 'string' ? rawMeta.uid.trim() : '',
          domain:
            (typeof rawMeta?.domain === 'string' ? rawMeta.domain.trim() : '') ||
            defaultWorkBuddyDomain(hydratedSite),
          ...(typeof rawMeta?.enterprise_id === 'string' && rawMeta.enterprise_id.trim()
            ? { enterprise_id: rawMeta.enterprise_id.trim() }
            : {}),
          expires_at: normalizeMetadataTimestamp(rawMeta?.expires_at),
          ...(normalizeMetadataTimestamp(rawMeta?.refresh_expires_at)
            ? { refresh_expires_at: normalizeMetadataTimestamp(rawMeta?.refresh_expires_at) }
            : {})
        }
      : {
          access_token: '',
          refresh_token: '',
          uid: '',
          domain: defaultWorkBuddyDomain('cn'),
          expires_at: ''
        }
  const hydratedWorkbuddyMetadata = buildWorkBuddyMetadataSnapshot({
    provider: resource.provider,
    authType: resource.auth_type,
    workbuddySite: hydratedSite,
    workbuddyOAuth: hydratedWorkbuddyOAuth
  })
  const rawAccountMode = typeof resource.account_mode === 'string' ? resource.account_mode.trim() : ''
  const hydratedKiloMode: KiloAccountMode =
    resource.provider === 'kilo' && rawAccountMode ? resolveKiloAccountMode(rawAccountMode) : (state.kiloMode ?? 'free')
  const hydratedOpenCodeMode: OpenCodeAccountMode =
    resource.provider === 'opencode_go' && rawAccountMode
      ? resolveOpenCodeAccountMode(rawAccountMode)
      : (state.openCodeMode ?? 'zen')
  return {
    ...state,
    mode: preserveCreateDraftMode ? 'create' : 'edit',
    connectionId: resource.id,
    version: resource.version,
    credentialVersion: resource.credential?.version ?? '1',
    credentialState: nextCredentialState,
    credentialLastFour: resource.credential?.last_four ?? null,
    connectionState: resource.state,
    provider: resource.provider,
    schemaVersion: resource.schema_version,
    authType: resource.auth_type,
    name: resource.name,
    endpoint: resource.endpoint,
    networkPolicyId: normalizedPolicyId,
    headerOverrides,
    credentialMode: nextCredentialState === 'unset' ? 'set' : 'keep',
    credentialSecret: '',
    credentialRefreshToken: '',
    workbuddySite: hydratedSite,
    workbuddyOAuth: hydratedWorkbuddyOAuth,
    kiloMode: hydratedKiloMode,
    openCodeMode: hydratedOpenCodeMode,
    verification: resource.last_verification ?? null,
    verificationStale: resource.last_verification ? !isVerificationFresh(resource.last_verification) : false,
    modelIds: [...resource.model_ids],
    poolIds: [...resource.pool_ids],
    concurrency,
    preview: null,
    previewStale: false,
    confirmedVersion: resource.state === 'active' ? resource.version : null,
    asyncError: null,
    persistedSnapshot: {
      name: resource.name.trim(),
      endpoint: normalizeSnapshotEndpoint(resource.endpoint),
      networkPolicyId: normalizedPolicyId,
      accountMode: rawAccountMode,
      headerOverrides: resource.header_overrides
        ? { ...resource.header_overrides }
        : buildHeaderOverridesMap(headerOverrides),
      modelIds: [...resource.model_ids],
      poolIds: [...resource.pool_ids],
      concurrency,
      ...(Object.keys(hydratedWorkbuddyMetadata).length > 0
        ? { workbuddyMetadata: hydratedWorkbuddyMetadata }
        : {})
    }
  }
}

export function invalidateVerification(state: ConnectionWizardState): ConnectionWizardState {
  return {
    ...state,
    verificationStale: state.verification !== null,
    preview: null,
    previewStale: state.preview !== null,
    confirmedVersion: null
  }
}

export function invalidatePreview(state: ConnectionWizardState): ConnectionWizardState {
  return {
    ...state,
    previewStale: state.preview !== null,
    confirmedVersion: null
  }
}

export function getUtf8ByteLength(value: string): number {
  return new TextEncoder().encode(value).length
}

function containsControlCharacter(value: string): boolean {
  for (let idx = 0; idx < value.length; idx += 1) {
    const code = value.charCodeAt(idx)
    if (code <= 0x1f || (code >= 0x7f && code <= 0x9f)) {
      return true
    }
  }
  return false
}

export function validateConnectionName(name: string, isZh = false): string | null {
  const trimmedName = name.trim()
  if (!trimmedName) {
    return isZh ? '请填写连接名称。' : 'Connection name is required.'
  }
  if (containsControlCharacter(trimmedName)) {
    return isZh ? '连接名称不能包含控制字符。' : 'Connection name must not contain control characters.'
  }
  if (getUtf8ByteLength(trimmedName) > 100) {
    return isZh
      ? '连接名称长度不能超过 100 字节（约 33 个汉字或 100 个英文字符）。'
      : 'Connection name must be 100 UTF-8 bytes or fewer.'
  }
  return null
}

export function isWizardKeylessPublicLane(state: Pick<ConnectionWizardState, 'provider' | 'authType' | 'kiloMode' | 'openCodeMode'>): boolean {
  const provider = state.provider.trim().toLowerCase()
  const authType = state.authType.trim().toLowerCase()
  if (authType !== 'api_key' && authType !== 'apikey') return false
  if (provider === 'kilo') return state.kiloMode === 'free'
  if (provider === 'opencode_go') return state.openCodeMode === 'zen_free'
  return false
}

export function hasWorkBuddyMetadataChanged(state: ConnectionWizardState): boolean {
  if (state.provider.trim().toLowerCase() !== 'workbuddy') return false
  const baseline = state.persistedSnapshot?.workbuddyMetadata
  if (!baseline) return false
  const current = buildWorkBuddyMetadataSnapshot(state)
  return !stringMapsEqual(current, baseline)
}

function canMutateExistingWorkBuddyMetadata(state: ConnectionWizardState): boolean {
  return (
    state.provider.trim().toLowerCase() === 'workbuddy' &&
    state.credentialState !== 'unset' &&
    state.credentialState !== 'revoked' &&
    hasWorkBuddyMetadataChanged(state)
  )
}

export function hasPendingCredentialMutation(state: ConnectionWizardState): boolean {
  if (state.credentialMode === 'clear') {
    return true
  }
  if (state.credentialMode === 'set') {
    return (
      state.credentialSecret.trim().length > 0 ||
      isWizardKeylessPublicLane(state) ||
      canMutateExistingWorkBuddyMetadata(state)
    )
  }
  return canMutateExistingWorkBuddyMetadata(state)
}

function buildWorkBuddyCredentialMetadata(
  state: ConnectionWizardState
): Record<string, string | number | undefined> | undefined {
  const provider = state.provider.trim().toLowerCase()
  if (provider !== 'workbuddy') return undefined
  const authType = state.authType.trim().toLowerCase()
  if (authType === 'api_key' || authType === 'apikey') {
    return { site: state.workbuddySite }
  }
  if (authType === 'oauth') {
    const metadata: Record<string, string | number | undefined> = {
      site: state.workbuddySite
    }
    const uid = state.workbuddyOAuth.uid.trim()
    if (uid) metadata.uid = uid
    const domain = state.workbuddyOAuth.domain.trim() || defaultWorkBuddyDomain(state.workbuddySite)
    if (domain) metadata.domain = domain
    const enterpriseId = state.workbuddyOAuth.enterprise_id?.trim()
    if (enterpriseId) metadata.enterprise_id = enterpriseId
    const expiresAt = state.workbuddyOAuth.expires_at.trim()
    if (expiresAt) metadata.expires_at = expiresAt
    const refreshExpiresAt = state.workbuddyOAuth.refresh_expires_at?.trim()
    if (refreshExpiresAt) metadata.refresh_expires_at = refreshExpiresAt
    return metadata
  }
  return undefined
}

export function buildCredentialInputPayload(state: ConnectionWizardState): ConnectionCredentialInput {
  if (state.credentialMode === 'clear') {
    return { operation: 'clear' }
  }
  if (state.credentialMode === 'keep') {
    if (canMutateExistingWorkBuddyMetadata(state)) {
      const metadata = buildWorkBuddyCredentialMetadata(state)
      return metadata ? { operation: 'keep', metadata } : { operation: 'keep' }
    }
    return { operation: 'keep' }
  }
  if (isWizardKeylessPublicLane(state)) {
    const provider = state.provider.trim().toLowerCase()
    return {
      operation: 'set',
      type: state.authType,
      secret: provider === 'kilo' ? '' : 'public'
    }
  }
  const trimmedSecret = state.credentialSecret.trim()
  if (trimmedSecret === '') {
    if (canMutateExistingWorkBuddyMetadata(state)) {
      const metadata = buildWorkBuddyCredentialMetadata(state)
      return metadata ? { operation: 'keep', metadata } : { operation: 'keep' }
    }
    return { operation: 'keep' }
  }
  const trimmedRefresh = state.credentialRefreshToken.trim()
  const normalizedAuth = state.authType.trim().toLowerCase()
  const metadata = buildWorkBuddyCredentialMetadata(state)
  return {
    operation: 'set',
    type: state.authType,
    secret: trimmedSecret,
    ...(trimmedRefresh && normalizedAuth === 'oauth' ? { refresh_token: trimmedRefresh } : {}),
    ...(metadata ? { metadata } : {})
  }
}

export function canProceedFromStep(
  state: ConnectionWizardState,
  step: WizardStepId,
  schema?: ConnectionSchema | null,
  nowMs: number = Date.now(),
  isZh = false
): { valid: boolean; reason?: string } {
  if (step === 'provider') {
    if (!state.provider.trim()) {
      return { valid: false, reason: isZh ? '请选择上游提供商协议。' : 'Select a provider schema.' }
    }
    if (!state.authType.trim()) {
      return { valid: false, reason: isZh ? '请选择认证模式。' : 'Select an authentication mode.' }
    }
    if (!isAllowedCredentialType(state.authType)) {
      return {
        valid: false,
        reason: isZh
          ? '所选认证模式不受服务端凭据契约支持，请重新选择认证方式。'
          : 'Selected authentication mode is not supported by credential schema.'
      }
    }
    const nameErr = validateConnectionName(state.name, isZh)
    if (nameErr) return { valid: false, reason: nameErr }
    const endpointRequired = Boolean(schema?.fields?.some((field) => field.path === 'endpoint' && field.required))
    const endpointErr = validateConnectionEndpoint(state.endpoint, endpointRequired, isZh)
    if (endpointErr) return { valid: false, reason: endpointErr }
    const policyErr = validateNetworkPolicyId(state.networkPolicyId, isZh)
    if (policyErr) return { valid: false, reason: policyErr }
    const headerErr = validateHeaderOverrideRows(
      state.headerOverrides,
      state.provider,
      state.authType,
      isZh
    )
    if (headerErr) return { valid: false, reason: headerErr }
    return { valid: true }
  }

  if (step === 'credentials') {
    if (state.credentialMode === 'clear') {
      return {
        valid: false,
        reason: isZh
          ? '清空凭据后无法通过连通性验证，请先设置新的替换凭据。'
          : 'Cleared credentials cannot be verified until a replacement secret is set.'
      }
    }
    if (isSwitchingFromPublicToPaidMode(state)) {
      const secret = state.credentialSecret.trim()
      if (state.credentialMode !== 'set' || !secret || secret.toLowerCase() === 'public') {
        return {
          valid: false,
          reason: isZh
            ? '从免费模式切换至付费模式必须填写有效的付费 API Key（不能使用 public 占位密钥）。'
            : 'Switching from a free lane to a paid lane requires a valid API key (cannot use the "public" placeholder).'
        }
      }
    }
    const normalizedProvider = state.provider.trim().toLowerCase()
    if (
      state.credentialMode === 'set' &&
      !isWizardKeylessPublicLane(state) &&
      (normalizedProvider === 'kilo' || normalizedProvider === 'opencode_go') &&
      state.credentialSecret.trim().toLowerCase() === 'public'
    ) {
      return {
        valid: false,
        reason: isZh
          ? '付费模式不能使用 "public" 占位密钥，请输入真实的 API Key。'
          : 'Paid mode cannot use the "public" placeholder secret. Enter a real API key.'
      }
    }
    if (state.credentialMode === 'keep' && state.credentialState === 'revoked') {
      return {
        valid: false,
        reason: isZh
          ? '当前凭据已失效或吊销，请填写新的替换密钥或重新完成授权。'
          : 'Current credential has been revoked. Set a replacement secret or re-authorize.'
      }
    }
    const missingUnsetCredential =
      (state.credentialMode === 'keep' && state.credentialState === 'unset') ||
      (state.credentialMode === 'set' && !state.credentialSecret.trim() && !isWizardKeylessPublicLane(state) && state.credentialState === 'unset')
    if (missingUnsetCredential) {
      const oauthSupported = isOAuthWorkflowSupported(schema, state.authType)
      return {
        valid: false,
        reason: oauthSupported
          ? isZh
            ? '尚未配置有效凭据，请填写访问令牌密钥或在下方完成 OAuth 授权绑定后再继续。'
            : 'No credential is configured yet. Enter an access token secret or attach OAuth authorization before continuing.'
          : isZh
            ? '尚未配置有效凭据，请先填写 API Key 密钥后再进入连通性验证。'
            : 'No credential is configured yet. Enter a valid API key / secret before continuing.'
      }
    }
    if (
      state.credentialMode === 'set' &&
      !state.credentialSecret.trim() &&
      !isWizardKeylessPublicLane(state) &&
      !canMutateExistingWorkBuddyMetadata(state)
    ) {
      return {
        valid: false,
        reason: isZh
          ? '请填写新的替换密钥，或将凭据模式切换回“保留现有凭据”。'
          : 'Enter a replacement secret or switch credential mode to Keep.'
      }
    }
    return { valid: true }
  }

  if (step === 'verify') {
    if (!state.verification) {
      return {
        valid: false,
        reason: isZh ? '请先执行连通性与凭据探测验证。' : 'Run connection verification before previewing.'
      }
    }
    if (state.verificationStale || !isVerificationFresh(state.verification, nowMs)) {
      return {
        valid: false,
        reason: isZh
          ? '探测验证结果已过期，请重新运行验证。'
          : 'Verification is stale or expired. Re-run verification.'
      }
    }
    const hasFailedCheck = state.verification.checks.some((check) => check.state === 'failed')
    if (hasFailedCheck) {
      const diagnostic = getPrimaryVerificationDiagnostic(state.verification, isZh)
      return {
        valid: false,
        reason: diagnostic
          ? `${diagnostic.message} (${diagnostic.reason})`
          : isZh
            ? '存在未通过的验证检查项，请修复后重新验证。'
            : 'Resolve failed verification checks before previewing.'
      }
    }
    if (hasUnavailableVerificationEvidence(state.verification)) {
      const diagnostic = getPrimaryVerificationDiagnostic(state.verification, isZh)
      return {
        valid: false,
        reason:
          diagnostic && diagnostic.reason !== 'EVIDENCE_UNAVAILABLE'
            ? `${diagnostic.message} (${diagnostic.reason})`
            : isZh
              ? '验证探测未能获取有效能力证据（EVIDENCE_UNAVAILABLE），请重试验证探针或先保存为草稿并退出。'
              : 'Verification evidence is unavailable (EVIDENCE_UNAVAILABLE). Retry verification probe or save as draft and exit.'
      }
    }
    return { valid: true }
  }

  if (step === 'preview') {
    if (!state.preview || state.previewStale) {
      return {
        valid: false,
        reason: isZh
          ? '启用连接前请先生成最新的资源池与模型分配预览。'
          : 'Generate a fresh assignment preview before activating.'
      }
    }
    if (state.preview.blocked.length > 0 || state.preview.items.some((item) => item.state !== 'applicable')) {
      return {
        valid: false,
        reason: isZh
          ? '预览中存在阻断项，请先调整模型或资源池配置后重新生成预览。'
          : 'Resolve blocked preview items before activating.'
      }
    }
    const expiresMs = Date.parse(state.preview.expires_at)
    if (!Number.isFinite(expiresMs) || expiresMs <= nowMs) {
      return {
        valid: false,
        reason: isZh
          ? '分配预览已超过有效期，请重新生成预览后再启用。'
          : 'Preview expired. Re-run preview before activating.'
      }
    }
    return { valid: true }
  }

  return { valid: true }
}

export function getWizardStepStatuses(
  state: ConnectionWizardState,
  labels: Record<WizardStepId, { title: string; description: string }>,
  nowMs: number = Date.now()
): UiStepItem[] {
  const currentIdx = WIZARD_STEP_ORDER.indexOf(state.currentStep)
  return WIZARD_STEP_ORDER.map((stepId, idx) => {
    let status: UiStepStatus = 'upcoming'
    if (stepId === state.currentStep) {
      status = state.asyncError ? 'error' : 'current'
    } else if (idx < currentIdx) {
      if (
        stepId === 'verify' &&
        (state.verificationStale ||
          !isVerificationFresh(state.verification, nowMs) ||
          hasUnavailableVerificationEvidence(state.verification) ||
          Boolean(state.verification?.checks.some((check) => check.state === 'failed')))
      ) {
        status = 'warning'
      } else if (stepId === 'preview' && state.previewStale) {
        status = 'warning'
      } else {
        status = 'complete'
      }
    } else if (stepId === 'activate' && state.confirmedVersion) {
      status = 'complete'
    }
    return {
      id: stepId,
      title: labels[stepId].title,
      description: labels[stepId].description,
      status
    }
  })
}

export interface BulkDiffSummary {
  applicableCount: number
  blockedCount: number
  conflictCount: number
  appliedCount: number
  selectableIds: string[]
}

export function computeBulkDiffSummary(items: readonly ConnectionItem[]): BulkDiffSummary {
  const selectableIds: string[] = []
  let applicableCount = 0
  let blockedCount = 0
  let conflictCount = 0
  let appliedCount = 0

  for (const item of items) {
    if (item.state === 'applicable') {
      applicableCount += 1
      selectableIds.push(item.id)
    } else if (item.state === 'blocked') {
      blockedCount += 1
    } else if (item.state === 'conflict') {
      conflictCount += 1
    } else if (item.state === 'applied') {
      appliedCount += 1
    }
  }

  return {
    applicableCount,
    blockedCount,
    conflictCount,
    appliedCount,
    selectableIds
  }
}

export function toggleBulkSelection(selectedIds: readonly string[], targetId: string): string[] {
  if (selectedIds.includes(targetId)) {
    return selectedIds.filter((id) => id !== targetId)
  }
  return [...selectedIds, targetId]
}
