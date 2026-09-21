/**
 * Admin credential secret API endpoints.
 * Encrypted credential references: metadata, explicit inline→reference
 * conversion, versioned rotation and terminal revocation. No plaintext read
 * endpoint exists by design.
 */

import { apiClient } from '../client'

export interface CredentialSecretMetadata {
  reference: string
  workspace_id: number
  account_id: number
  version: number
  updated_at: string
  revoked_at?: string
}

export async function getCredentialSecret(id: number): Promise<CredentialSecretMetadata> {
  const { data } = await apiClient.get<CredentialSecretMetadata>(`/admin/accounts/${id}/credential-secret`)
  return data
}

export async function convertCredentialSecret(id: number, accountRevision: string): Promise<CredentialSecretMetadata> {
  const { data } = await apiClient.post<CredentialSecretMetadata>(`/admin/accounts/${id}/credential-secret/convert`, {
    confirmed: true,
    account_revision: accountRevision,
  })
  return data
}

export async function rotateCredentialSecret(
  id: number,
  expectedVersion: number,
  credentials: Record<string, unknown>,
): Promise<CredentialSecretMetadata> {
  const { data } = await apiClient.post<CredentialSecretMetadata>(`/admin/accounts/${id}/credential-secret/rotate`, {
    expected_version: expectedVersion,
    credentials,
  })
  return data
}

export async function revokeCredentialSecret(id: number, expectedVersion: number): Promise<CredentialSecretMetadata> {
  const { data } = await apiClient.post<CredentialSecretMetadata>(`/admin/accounts/${id}/credential-secret/revoke`, {
    expected_version: expectedVersion,
  })
  return data
}

export default {
  getCredentialSecret,
  convertCredentialSecret,
  rotateCredentialSecret,
  revokeCredentialSecret,
}
