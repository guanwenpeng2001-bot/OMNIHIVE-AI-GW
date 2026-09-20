import type { CreateAccountRequest } from '@/types'

export interface AccountCredentialDraft {
  credential_draft: string
  revision: string
  platform: string
  type: string
  expires_at: string
}

export type AccountWorkflowRequest = CreateAccountRequest & {
  existing_account_id?: number
  credential_draft?: string
  credential_draft_revision?: string
}

export function readAccountCredentialDraft(value: unknown): AccountCredentialDraft | undefined {
  if (!value || typeof value !== 'object') return undefined
  const draft = value as Record<string, unknown>
  if (typeof draft.credential_draft !== 'string' || !draft.credential_draft || typeof draft.revision !== 'string') return undefined
  if (typeof draft.platform !== 'string' || typeof draft.type !== 'string' || typeof draft.expires_at !== 'string') return undefined
  return draft as unknown as AccountCredentialDraft
}

export function credentialsFromAuthorization(value: unknown, legacyCredentials: () => Record<string, unknown>): Record<string, unknown> {
  const draft = readAccountCredentialDraft(value)
  return draft ? { ...draft } : legacyCredentials()
}
