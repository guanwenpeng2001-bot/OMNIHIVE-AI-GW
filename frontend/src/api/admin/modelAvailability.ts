import { apiClient } from '../client'

export interface ModelAvailabilityDeclaration {
  account_id: number
  model: string
  endpoint: string
  identity_revision: number
  rules_revision: number
  credential_account_id?: number
  credential_identity_revision?: number
  reason: string
  confirmed_by: number
  confirmed_at: string
  expires_at?: string
  revoked_at?: string
  evidence_kind: 'administrator_declaration' | 'probe_success'
  valid: boolean
}

export interface ModelAvailabilityState {
  identity_revision: string
  rules_revision: string
  snapshot_revision: string
  declarations: ModelAvailabilityDeclaration[]
}

export interface ChangeModelAvailability {
  identity_revision: string
  rules_revision: string
  snapshot_revision: string
  model: string
  endpoint: string
  reason: string
  confirmed: boolean
  expires_at?: string
}

export const modelAvailabilityAPI = {
  async get(accountId: number): Promise<ModelAvailabilityState> {
    const { data } = await apiClient.get<ModelAvailabilityState>(`/admin/accounts/${accountId}/model-availability`)
    return data
  },
  async declare(accountId: number, request: ChangeModelAvailability): Promise<ModelAvailabilityState> {
    const { data } = await apiClient.put<ModelAvailabilityState>(`/admin/accounts/${accountId}/model-availability`, request)
    return data
  },
  async revoke(accountId: number, request: ChangeModelAvailability): Promise<ModelAvailabilityState> {
    const { data } = await apiClient.delete<ModelAvailabilityState>(`/admin/accounts/${accountId}/model-availability`, { data: request })
    return data
  }
}
