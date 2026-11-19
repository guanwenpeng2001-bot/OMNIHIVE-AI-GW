import type { ConsoleAsyncNotice } from '@/features/console/asyncState'
import type { CatalogModelRate } from '@/features/console/catalogApi'

export type PlaygroundProtocol =
  | 'openai_chat'
  | 'openai_responses'
  | 'anthropic_messages'
  | 'gemini_generate'

export interface PlaygroundOptions {
  stream?: boolean
  include_raw_events?: boolean
  retain_text_preview?: boolean
}

export interface PlaygroundSubmit {
  api_key_id: string
  payer_user_id?: string
  model_id: string
  protocol: PlaygroundProtocol
  action: 'text.chat'
  input: Record<string, unknown>
  options?: PlaygroundOptions
  funding_acknowledged: true
}

export interface PlaygroundReplay {
  api_key_id: string
  payer_user_id?: string
  input: Record<string, unknown>
  options?: PlaygroundOptions
  funding_acknowledged: true
}

export type PlaygroundRunState =
  | 'running'
  | 'cancel_requested'
  | 'succeeded'
  | 'failed'
  | 'canceled'
  | 'unknown'

export type PlaygroundOutcome = 'succeeded' | 'failed' | 'canceled' | 'unknown'

export interface PlaygroundSummary {
  run_id: string
  request_id: string
  protocol: PlaygroundProtocol
  state: PlaygroundRunState
  last_seq: number
  content_retained: boolean
  text_preview?: string
  status_url: string
  detail_url: string
  payer_user_id: string
  settlement_state: string
}

export interface PlaygroundResult {
  run_id: string
  request_id: string
  protocol: PlaygroundProtocol
  outcome: PlaygroundOutcome
  output: Record<string, unknown> | null
  reason?: string
  retry?: 'never' | 'query_status'
  settlement_state: string
  status_url: string
  detail_url: string
}

export interface PlaygroundQuantity {
  unit: 'input_token' | 'output_token'
  quantity: string
}

export type PlaygroundSseEventName =
  | 'started'
  | 'delta'
  | 'usage'
  | 'raw_event'
  | 'diagnostic'
  | 'finished'
  | 'failed'

export interface PlaygroundEvent {
  seq: number
  run_id: string
  request_id: string
  protocol?: PlaygroundProtocol
  kind?: 'text' | 'reasoning' | 'tool_arguments'
  index?: number
  text?: string
  tool_call_id?: string
  name?: string
  fragment?: string
  quantities?: PlaygroundQuantity[]
  evidence?: 'provider_reported'
  frame?: unknown
  code?: string
  field?: string
  severity?: string
  price_version?: string
  payer_user_id?: string
  outcome?: PlaygroundOutcome
  reason?: string
  retry?: 'never' | 'query_status'
  status_url?: string
  detail_url?: string
  settlement_state?: string
}

export interface PlaygroundMessageDraft {
  id: string
  role: 'user' | 'assistant' | 'tool'
  content: string
  toolCallId?: string
}

export interface PlaygroundToolCallView {
  key: string
  index: number
  toolCallId: string
  name: string
  rawArguments: string
  parsedArguments: Record<string, unknown> | null
}

export interface PlaygroundDiagnosticItem {
  seq: number
  code?: string
  field?: string
  severity?: string
  reason?: string
}

export interface PlaygroundSseFrameLog {
  id: string
  event: PlaygroundSseEventName | string
  seq: number | null
  receivedAtMs: number
  summary: string
  rawData: string
}

export interface PlaygroundSideConfig {
  label: string
  apiKeyId: string
  modelId: string
  protocol: PlaygroundProtocol
  temperature: number
  maxOutputTokens: number
  reasoningEffort: '' | 'low' | 'medium' | 'high'
  stream: boolean
  includeRawEvents: boolean
  retainTextPreview: boolean
}

export interface PlaygroundCostEstimate {
  sideLabel: string
  modelId: string
  protocol: PlaygroundProtocol
  apiKeyId: string
  estimatedInputTokens: number
  maxOutputTokens: number
  currency: string
  inputRate: CatalogModelRate | null
  outputRate: CatalogModelRate | null
  estimatedInputCost: string | null
  maxOutputCost: string | null
  maxTotalCost: string | null
  rateKnown: boolean
  payloadBytes: number
  exceedsBodyLimit: boolean
}

export type PlaygroundMachineStatus =
  | 'idle'
  | 'awaiting_consent'
  | 'submitting'
  | 'streaming'
  | 'cancel_requested'
  | 'succeeded'
  | 'failed'
  | 'canceled'
  | 'unknown'
  | 'disconnected_recoverable'

export interface PlaygroundSessionSnapshot {
  status: PlaygroundMachineStatus
  idempotencyKey: string | null
  runId: string | null
  requestId: string | null
  protocol: PlaygroundProtocol | null
  priceVersion: string | null
  payerUserId: string | null
  lastSeq: number
  outputText: string
  reasoningText: string
  syncOutput: Record<string, unknown> | null
  toolCalls: PlaygroundToolCallView[]
  inputTokens: string | null
  outputTokens: string | null
  diagnostics: PlaygroundDiagnosticItem[]
  rawEvents: unknown[]
  sseFrames: PlaygroundSseFrameLog[]
  settlementState: string | null
  statusUrl: string | null
  detailUrl: string | null
  outcomeReason: string | null
  retryAdvice: 'never' | 'query_status' | null
  ttftMs: number | null
  elapsedMs: number | null
  errorNotice: ConsoleAsyncNotice | null
  submittedPayload: PlaygroundSubmit | PlaygroundReplay | null
}
