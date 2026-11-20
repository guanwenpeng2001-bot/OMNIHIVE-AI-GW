import type { UiStepItem, UiStepStatus } from '@/components/ui'
import {
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
  verification: ConnectionVerification | null
  verificationStale: boolean
  modelIds: string[]
  poolIds: string[]
  concurrency: number
  preview: ConnectionPreview | null
  previewStale: boolean
  confirmedVersion: string | null
  asyncError: AsyncErrorContext | null
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
    verification: null,
    verificationStale: false,
    modelIds: [],
    poolIds: [],
    concurrency: 4,
    preview: null,
    previewStale: false,
    confirmedVersion: null,
    asyncError: null
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
    networkPolicyId: resource.network_policy_id,
    headerOverrides,
    credentialMode: nextCredentialState === 'unset' ? 'set' : 'keep',
    credentialSecret: '',
    credentialRefreshToken: '',
    verification: resource.last_verification,
    verificationStale: resource.last_verification ? !isVerificationFresh(resource.last_verification) : false,
    modelIds: [...resource.model_ids],
    poolIds: [...resource.pool_ids],
    concurrency: resource.limits?.concurrency ?? 4,
    preview: null,
    previewStale: false,
    confirmedVersion: resource.state === 'active' ? resource.version : null,
    asyncError: null
  }
}

export function invalidateVerification(state: ConnectionWizardState): ConnectionWizardState {
  return {
    ...state,
    verificationStale: state.verification !== null,
    preview: null,
    previewStale: state.preview !== null
  }
}

export function invalidatePreview(state: ConnectionWizardState): ConnectionWizardState {
  return {
    ...state,
    previewStale: state.preview !== null
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

export function hasPendingCredentialMutation(state: ConnectionWizardState): boolean {
  if (state.credentialMode === 'clear') {
    return true
  }
  if (state.credentialMode === 'set') {
    return state.credentialSecret.trim().length > 0
  }
  return false
}

export function buildCredentialInputPayload(state: ConnectionWizardState): ConnectionCredentialInput {
  if (state.credentialMode === 'clear') {
    return { operation: 'clear' }
  }
  const trimmedSecret = state.credentialSecret.trim()
  if (state.credentialMode === 'keep' || trimmedSecret === '') {
    return { operation: 'keep' }
  }
  const trimmedRefresh = state.credentialRefreshToken.trim()
  const normalizedAuth = state.authType.trim().toLowerCase()
  return {
    operation: 'set',
    type: state.authType,
    secret: trimmedSecret,
    ...(trimmedRefresh && normalizedAuth === 'oauth' ? { refresh_token: trimmedRefresh } : {})
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
      (state.credentialMode === 'set' && !state.credentialSecret.trim() && state.credentialState === 'unset')
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
    if (state.credentialMode === 'set' && !state.credentialSecret.trim()) {
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
      return {
        valid: false,
        reason: isZh
          ? '存在未通过的验证检查项，请修复后重新验证。'
          : 'Resolve failed verification checks before previewing.'
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
      if (stepId === 'verify' && (state.verificationStale || !isVerificationFresh(state.verification, nowMs))) {
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
