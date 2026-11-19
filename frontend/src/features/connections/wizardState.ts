import type { UiStepItem, UiStepStatus } from '@/components/ui'
import { isVerificationFresh, validateConnectionEndpoint } from './rules'
import type {
  AsyncErrorContext,
  ConnectionCredentialInput,
  ConnectionItem,
  ConnectionPreview,
  ConnectionResource,
  ConnectionSchema,
  ConnectionVerification,
  CredentialOperationMode
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
  return {
    ...state,
    mode: 'edit',
    connectionId: resource.id,
    version: resource.version,
    credentialVersion: resource.credential?.version ?? '1',
    credentialState: resource.credential?.state ?? 'unset',
    credentialLastFour: resource.credential?.last_four ?? null,
    connectionState: resource.state,
    provider: resource.provider,
    schemaVersion: resource.schema_version,
    authType: resource.auth_type,
    name: resource.name,
    endpoint: resource.endpoint,
    networkPolicyId: resource.network_policy_id,
    credentialMode: 'keep',
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

export function buildCredentialInputPayload(state: ConnectionWizardState): ConnectionCredentialInput {
  if (state.credentialMode === 'keep') {
    return { operation: 'keep' }
  }
  if (state.credentialMode === 'clear') {
    return { operation: 'clear' }
  }
  return {
    operation: 'set',
    type: state.authType,
    secret: state.credentialSecret.trim(),
    ...(state.credentialRefreshToken.trim() ? { refresh_token: state.credentialRefreshToken.trim() } : {})
  }
}

export function canProceedFromStep(
  state: ConnectionWizardState,
  step: WizardStepId,
  schema?: ConnectionSchema | null,
  nowMs: number = Date.now()
): { valid: boolean; reason?: string } {
  if (step === 'provider') {
    if (!state.provider.trim()) return { valid: false, reason: 'Select a provider schema.' }
    if (!state.authType.trim()) return { valid: false, reason: 'Select an authentication mode.' }
    if (!state.name.trim()) return { valid: false, reason: 'Connection name is required.' }
    const endpointRequired = Boolean(schema?.fields?.some((field) => field.path === 'endpoint' && field.required))
    const endpointErr = validateConnectionEndpoint(state.endpoint, endpointRequired)
    if (endpointErr) return { valid: false, reason: endpointErr }
    return { valid: true }
  }

  if (step === 'credentials') {
    if (state.credentialMode === 'set' && !state.credentialSecret.trim()) {
      return { valid: false, reason: 'Enter a secret or switch credential mode to Keep.' }
    }
    if (state.credentialMode === 'clear') {
      return { valid: false, reason: 'Cleared credentials cannot be verified until a replacement secret is set.' }
    }
    if (state.mode === 'create' && state.credentialMode === 'keep' && state.credentialState === 'unset') {
      return { valid: false, reason: 'Set a credential before continuing.' }
    }
    return { valid: true }
  }

  if (step === 'verify') {
    if (!state.verification) {
      return { valid: false, reason: 'Run connection verification before previewing.' }
    }
    if (state.verificationStale || !isVerificationFresh(state.verification, nowMs)) {
      return { valid: false, reason: 'Verification is stale or expired. Re-run verification.' }
    }
    const hasFailedCheck = state.verification.checks.some((check) => check.state === 'failed')
    if (hasFailedCheck) {
      return { valid: false, reason: 'Resolve failed verification checks before previewing.' }
    }
    return { valid: true }
  }

  if (step === 'preview') {
    if (!state.preview || state.previewStale) {
      return { valid: false, reason: 'Generate a fresh assignment preview before activating.' }
    }
    const expiresMs = Date.parse(state.preview.expires_at)
    if (!Number.isFinite(expiresMs) || expiresMs <= nowMs) {
      return { valid: false, reason: 'Preview expired. Re-run preview before activating.' }
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
