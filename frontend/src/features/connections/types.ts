import type { components } from '@/api/generated/openapi'

export type ConnectionField = components['schemas']['ConnectionField']
export type ConnectionSchema = components['schemas']['ConnectionSchema']
export type ConnectionLimits = components['schemas']['ConnectionLimits']
export type ConnectionMetadata = components['schemas']['ConnectionMetadata']
export type ConnectionCheck = components['schemas']['ConnectionCheck']
export type ConnectionVerification = components['schemas']['ConnectionVerification']
export type ConnectionQuota = components['schemas']['ConnectionQuota']
export type ConnectionCreate = components['schemas']['ConnectionCreate'] & {
  header_overrides?: Record<string, string>
}
export type ConnectionPatch = components['schemas']['ConnectionPatch'] & {
  header_overrides?: Record<string, string>
}
export type ConnectionAssignments = components['schemas']['ConnectionAssignments']
export type ConnectionResource = components['schemas']['ConnectionResource'] & {
  header_overrides?: Record<string, string>
}
export type ConnectionRedacted = components['schemas']['ConnectionRedacted']
export type ConnectionCredentialInput = components['schemas']['ConnectionCredentialInput']
export type ConnectionCredentialResult = components['schemas']['ConnectionCredentialResult']
export type ConnectionVerify = components['schemas']['ConnectionVerify']
export type ConnectionItem = components['schemas']['ConnectionItem']
export type ConnectionPreview = components['schemas']['ConnectionPreview']
export type ConnectionActivate = components['schemas']['ConnectionActivate']
export type ConnectionRotation = components['schemas']['ConnectionRotation']
export type ConnectionBulkIn = components['schemas']['ConnectionBulkIn']
export type ConnectionBulkApply = components['schemas']['ConnectionBulkApply']
export type ConnectionPool = components['schemas']['ConnectionPool']
export type ConnectionNewPool = components['schemas']['ConnectionNewPool']
export type ConnectionEvent = components['schemas']['ConnectionEvent']
export type ConnectionSchemaPage = components['schemas']['ConnectionSchemaPage']
export type ConnectionResourcePage = components['schemas']['ConnectionResourcePage']
export type ConnectionEventPage = components['schemas']['ConnectionEventPage']
export type ConnectionPoolPage = components['schemas']['ConnectionPoolPage']
export type ConnectionWriteResult = components['schemas']['ConnectionWriteResult']
export type ConnectionOperation = components['schemas']['ConnectionOperation']
export type ConnectionAuthorization = components['schemas']['ConnectionAuthorization']

export type CredentialOperationMode = 'keep' | 'set' | 'clear'

export interface HeaderOverrideDraftRow {
  key: string
  value: string
}

export interface ValidationFieldIssue {
  path: string
  reason: string
  labelZh: string
  labelEn: string
  hintZh: string
  hintEn: string
}

export type AsyncPageState =
  | 'initial_load'
  | 'background_refresh'
  | 'empty'
  | 'zero_filter'
  | 'partial_failure'
  | 'permission_denied'
  | 'stale'
  | 'version_conflict'
  | 'confirmed'
  | 'unknown_outcome'

export interface AsyncErrorContext {
  state: AsyncPageState
  status: number
  reason?: string
  message: string
  requestId?: string
  requiredRole?: string
  serverVersion?: string
  statusUrl?: string
  fields?: ValidationFieldIssue[]
}

export interface TempUnschedRuleInput {
  error_code: number | null
  keywords: string
  duration_minutes: number | null
  description: string
}

export interface TempUnschedRulePayload {
  error_code: number
  keywords: string[]
  duration_minutes: number
  description: string
}

export interface CheckMixedChannelLike {
  message?: string
  details?: {
    group_name?: string
    current_platform?: string
    other_platform?: string
  }
}

export interface MixedChannelDetails {
  groupName: string
  currentPlatform: string
  otherPlatform: string
}
