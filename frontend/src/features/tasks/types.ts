export type ImageTaskState =
  | 'queued'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'canceled'
  | 'unknown'

export interface ImageTaskMoney {
  currency: string
  amount: string
}

export interface ImageTaskSummary {
  id: string
  kind: 'image' | string
  state: ImageTaskState
  stage: string
  model_id: string
  created_at: string
  updated_at: string
  progress: number | null
  settlement_state: string
  allowed_actions: string[]
}

export interface ImageTaskItemsRollup {
  total: string
  succeeded: string
  failed: string
  pending: string
  canceled: string
}

export interface ImageTaskArtifact {
  id: string
  content_type: string
  size_bytes: string | null
  expires_at: string | null
  state: 'available' | 'expired' | 'revoked' | string
}

export interface ImageTaskDetail extends ImageTaskSummary {
  project_id: string
  source: string
  request_id: string | null
  request_url: string | null
  settlement_id: string | null
  settlement_url: string | null
  parent_task_id: string | null
  provider_state: string
  artifact_state: string
  cancel_state: string
  progress_source: string
  items: ImageTaskItemsRollup
  artifacts: ImageTaskArtifact[]
  estimate: ImageTaskMoney | null
  actual_charge: ImageTaskMoney | null
  action_reasons: Record<string, string>
  version: string
}

export interface ImageTaskItem {
  id: string
  state: string
  acceptance: string
  reason: string | null
  retryable: boolean
  artifact_ids: string[]
}

export interface ImageTaskPage {
  items: ImageTaskSummary[]
  next_cursor: string | null
  as_of: string
}

export interface ImageTaskItemPage {
  items: ImageTaskItem[]
  next_cursor: string | null
  as_of: string
}

export interface ImageTaskCreateInput {
  kind: 'image'
  model_id: string
  api_key_id: string
  input: {
    prompt: string
    size?: string
    count?: number
  }
  funding_acknowledged: true
}

export interface ImageTaskRetryInput {
  item_ids?: string[]
  funding_acknowledged: true
}

export interface ImageTaskRetryResult {
  task_id: string
  parent_task_id: string
  new_request_id: string
  state: string
}

export interface ImageTaskCancelResult {
  task_id: string
  cancel_state: string
  state: string
  version: string
}

export interface ImageTaskTicket {
  url: string
  ticket: string
  expires_at: string
  content_type: string
  size_bytes: string | null
}
