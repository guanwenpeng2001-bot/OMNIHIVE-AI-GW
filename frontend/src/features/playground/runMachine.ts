import { reactive } from 'vue'
import { classifyConsoleError } from '@/features/console/asyncState'
import { generateIdempotencyKey } from '@/features/console/scope'
import { playgroundApi } from './api'
import { normalizePlaygroundError } from './errors'
import type { ParsedSseRecord } from './sseParser'
import type {
  PlaygroundEvent,
  PlaygroundReplay,
  PlaygroundResult,
  PlaygroundSessionSnapshot,
  PlaygroundSubmit,
  PlaygroundSummary,
  PlaygroundToolCallView
} from './types'

export interface PlaygroundApiAdapter {
  submitRun: typeof playgroundApi.submitRun
  replayRequest: typeof playgroundApi.replayRequest
  getRunByIdempotencyKey: typeof playgroundApi.getRunByIdempotencyKey
  getRunDetail: typeof playgroundApi.getRunDetail
  cancelRun: typeof playgroundApi.cancelRun
}

function createInitialSnapshot(): PlaygroundSessionSnapshot {
  return {
    status: 'idle',
    idempotencyKey: null,
    runId: null,
    requestId: null,
    protocol: null,
    priceVersion: null,
    payerUserId: null,
    lastSeq: 0,
    outputText: '',
    reasoningText: '',
    syncOutput: null,
    toolCalls: [],
    inputTokens: null,
    outputTokens: null,
    diagnostics: [],
    rawEvents: [],
    sseFrames: [],
    settlementState: null,
    statusUrl: null,
    detailUrl: null,
    outcomeReason: null,
    retryAdvice: null,
    ttftMs: null,
    elapsedMs: null,
    errorNotice: null,
    submittedPayload: null
  }
}

function tryParseJsonObject(raw: string): Record<string, unknown> | null {
  const trimmed = raw.trim()
  if (!trimmed.startsWith('{') || !trimmed.endsWith('}')) return null
  try {
    const parsed = JSON.parse(trimmed)
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed)
      ? (parsed as Record<string, unknown>)
      : null
  } catch {
    return null
  }
}

export function createPlaygroundRunController(
  options?: {
    api?: PlaygroundApiAdapter
    now?: () => number
    isZh?: () => boolean
    isKeyOwned?: (apiKeyId: string) => boolean
  }
) {
  const api = options?.api ?? playgroundApi
  const now = options?.now ?? (() => Date.now())
  const isZh = options?.isZh ?? (() => false)
  const isKeyOwned = options?.isKeyOwned ?? (() => true)

  const state = reactive<PlaygroundSessionSnapshot>(createInitialSnapshot())
  const toolCallIndexMap = new Map<string, PlaygroundToolCallView>()

  let activeAbortController: AbortController | null = null
  let activeGeneration = 0
  let startedTimestampMs = 0
  let terminalEventSeen = false
  let stopRequestedByUser = false
  let lastWorkspaceId = ''
  let lastProjectId = ''

  function reset(): void {
    abortActiveLocalStream()
    activeGeneration++
    toolCallIndexMap.clear()
    Object.assign(state, createInitialSnapshot())
    terminalEventSeen = false
    stopRequestedByUser = false
  }

  function abortActiveLocalStream(): void {
    if (activeAbortController) {
      activeAbortController.abort()
      activeAbortController = null
    }
  }

  function markConsentPending(payload: PlaygroundSubmit | PlaygroundReplay): void {
    abortActiveLocalStream()
    activeGeneration++
    toolCallIndexMap.clear()
    Object.assign(state, createInitialSnapshot(), {
      status: 'awaiting_consent',
      submittedPayload: payload
    })
  }

  function cancelConsent(): void {
    if (state.status === 'awaiting_consent') {
      state.status = 'idle'
      state.submittedPayload = null
    }
  }

  function applySummary(summary: PlaygroundSummary): void {
    state.runId = summary.run_id
    state.requestId = summary.request_id
    state.protocol = summary.protocol
    state.payerUserId = summary.payer_user_id
    state.lastSeq = Math.max(state.lastSeq, summary.last_seq ?? 0)
    state.settlementState = summary.settlement_state
    state.statusUrl = summary.status_url
    state.detailUrl = summary.detail_url
    if (summary.content_retained && summary.text_preview && !state.outputText) {
      state.outputText = summary.text_preview
    }

    if (summary.state === 'running') {
      state.status = 'streaming'
    } else if (summary.state === 'cancel_requested') {
      state.status = 'cancel_requested'
    } else if (summary.state === 'succeeded') {
      state.status = 'succeeded'
    } else if (summary.state === 'failed') {
      state.status = 'failed'
    } else if (summary.state === 'canceled') {
      state.status = 'canceled'
    } else {
      state.status = 'unknown'
      state.retryAdvice = 'query_status'
    }
  }

  function applyResult(result: PlaygroundResult): void {
    terminalEventSeen = true
    state.runId = result.run_id
    state.requestId = result.request_id
    state.protocol = result.protocol
    state.syncOutput = result.output
    state.outcomeReason = result.reason ?? null
    state.retryAdvice = result.retry ?? null
    state.settlementState = result.settlement_state
    state.statusUrl = result.status_url
    state.detailUrl = result.detail_url
    state.elapsedMs = Math.max(0, now() - startedTimestampMs)

    if (result.output && !state.outputText) {
      const choices = (result.output as { choices?: Array<{ message?: { content?: string } }> }).choices
      const directText = choices?.[0]?.message?.content
      if (typeof directText === 'string') {
        state.outputText = directText
      }
    }

    state.status = result.outcome
  }

  function handleSseFrame(frame: ParsedSseRecord, generation: number): void {
    if (generation !== activeGeneration) return
    const frameTime = now()
    let parsed: PlaygroundEvent
    try {
      parsed = JSON.parse(frame.data) as PlaygroundEvent
    } catch {
      state.sseFrames.push({
        id: frame.id || String(state.sseFrames.length + 1),
        event: frame.event,
        seq: null,
        receivedAtMs: frameTime - startedTimestampMs,
        summary: 'Unparseable frame',
        rawData: frame.data
      })
      return
    }

    if (typeof parsed.seq === 'number') {
      if (parsed.seq <= state.lastSeq) {
        return
      }
      state.lastSeq = parsed.seq
    }

    if (!state.runId && parsed.run_id) state.runId = parsed.run_id
    if (!state.requestId && parsed.request_id) state.requestId = parsed.request_id
    if (!state.protocol && parsed.protocol) state.protocol = parsed.protocol

    state.sseFrames.push({
      id: frame.id || String(parsed.seq),
      event: frame.event,
      seq: parsed.seq ?? null,
      receivedAtMs: Math.max(0, frameTime - startedTimestampMs),
      summary: summarizeEvent(frame.event, parsed),
      rawData: frame.data
    })

    if (frame.event === 'started') {
      if (state.status !== 'cancel_requested') {
        state.status = 'streaming'
      }
      if (parsed.price_version) state.priceVersion = parsed.price_version
      if (parsed.payer_user_id) state.payerUserId = parsed.payer_user_id
      return
    }

    if (frame.event === 'delta') {
      if (state.ttftMs === null) {
        state.ttftMs = Math.max(0, frameTime - startedTimestampMs)
      }
      if (parsed.kind === 'reasoning') {
        state.reasoningText += parsed.text ?? parsed.fragment ?? ''
      } else if (parsed.kind === 'tool_arguments') {
        const idx = typeof parsed.index === 'number' ? parsed.index : 0
        const mapKey = String(idx)
        let existing = toolCallIndexMap.get(mapKey)
        if (!existing) {
          existing = {
            key: `${idx}-${parsed.tool_call_id || state.toolCalls.length}`,
            index: idx,
            toolCallId: parsed.tool_call_id || '',
            name: parsed.name || '',
            rawArguments: '',
            parsedArguments: null
          }
          toolCallIndexMap.set(mapKey, existing)
          state.toolCalls.push(existing)
        }
        if (parsed.tool_call_id) existing.toolCallId = parsed.tool_call_id
        if (parsed.name) existing.name = parsed.name
        existing.rawArguments += parsed.fragment ?? parsed.text ?? ''
        existing.parsedArguments = tryParseJsonObject(existing.rawArguments)
      } else {
        state.outputText += parsed.text ?? parsed.fragment ?? ''
      }
      return
    }

    if (frame.event === 'usage') {
      if (Array.isArray(parsed.quantities)) {
        for (const q of parsed.quantities) {
          if (q.unit === 'input_token') {
            state.inputTokens = String(q.quantity)
          } else if (q.unit === 'output_token') {
            state.outputTokens = String(q.quantity)
          }
        }
      }
      return
    }

    if (frame.event === 'raw_event') {
      state.rawEvents.push(parsed.frame)
      return
    }

    if (frame.event === 'diagnostic') {
      state.diagnostics.push({
        seq: parsed.seq,
        code: parsed.code,
        field: parsed.field,
        severity: parsed.severity,
        reason: parsed.reason
      })
      return
    }

    if (frame.event === 'finished' || frame.event === 'failed') {
      terminalEventSeen = true
      state.elapsedMs = Math.max(0, frameTime - startedTimestampMs)
      state.outcomeReason = parsed.reason ?? null
      state.retryAdvice = parsed.retry ?? null
      if (parsed.settlement_state) state.settlementState = parsed.settlement_state
      if (parsed.status_url) state.statusUrl = parsed.status_url
      if (parsed.detail_url) state.detailUrl = parsed.detail_url
      const outcome = parsed.outcome ?? (frame.event === 'failed' ? 'failed' : 'succeeded')
      state.status = outcome
    }
  }

  function summarizeEvent(eventName: string, ev: PlaygroundEvent): string {
    if (eventName === 'started') return `run=${ev.run_id.slice(0, 8)} price=${ev.price_version || '—'}`
    if (eventName === 'delta') {
      if (ev.kind === 'tool_arguments') {
        return `tool[${ev.index ?? 0}] ${ev.name || ev.tool_call_id || ''}`
      }
      return `${ev.kind || 'text'} (${(ev.text ?? ev.fragment ?? '').length} chars)`
    }
    if (eventName === 'usage') {
      return (ev.quantities || []).map((q) => `${q.unit}=${q.quantity}`).join(', ') || 'usage'
    }
    if (eventName === 'finished' || frameIsFailed(eventName)) {
      return `${ev.outcome || eventName}${ev.reason ? ` · ${ev.reason}` : ''}`
    }
    return eventName
  }

  function frameIsFailed(eventName: string): boolean {
    return eventName === 'failed'
  }

  async function executeApprovedRun(
    workspaceId: string,
    projectId: string,
    payload: PlaygroundSubmit,
    explicitIdempotencyKey?: string
  ): Promise<void> {
    if (!payload.funding_acknowledged) {
      throw new Error('Funding acknowledgement is required before submitting a Playground run')
    }
    abortActiveLocalStream()
    const generation = ++activeGeneration
    const controller = new AbortController()
    activeAbortController = controller
    lastWorkspaceId = workspaceId
    lastProjectId = projectId
    terminalEventSeen = false
    stopRequestedByUser = false
    toolCallIndexMap.clear()
    startedTimestampMs = now()

    const idemKey = explicitIdempotencyKey || generateIdempotencyKey()
    Object.assign(state, createInitialSnapshot(), {
      status: 'submitting',
      idempotencyKey: idemKey,
      protocol: payload.protocol,
      submittedPayload: payload
    })

    try {
      const result = await api.submitRun(workspaceId, projectId, payload, {
        idempotencyKey: idemKey,
        signal: controller.signal,
        onFrame: (frame) => handleSseFrame(frame, generation)
      })
      if (generation !== activeGeneration) return
      if (result.kind === 'summary') {
        applySummary(result.summary)
      } else if (result.kind === 'result') {
        applyResult(result.result)
      } else if (!terminalEventSeen) {
        state.elapsedMs = Math.max(0, now() - startedTimestampMs)
        state.status = stopRequestedByUser ? 'canceled' : 'disconnected_recoverable'
      }
    } catch (err) {
      if (generation !== activeGeneration) return
      state.elapsedMs = Math.max(0, now() - startedTimestampMs)
      if (stopRequestedByUser) {
        if (state.status !== 'canceled' && state.status !== 'succeeded' && state.status !== 'unknown') {
          state.status = state.runId ? 'cancel_requested' : 'disconnected_recoverable'
        }
        return
      }
      const isAbortOrNetwork =
        (err instanceof DOMException && err.name === 'AbortError') ||
        (err instanceof TypeError) ||
        ((err as { name?: string })?.name === 'AbortError') ||
        !(err as { status?: number })?.status

      if (isAbortOrNetwork && state.idempotencyKey) {
        state.status = 'disconnected_recoverable'
        state.errorNotice = {
          kind: 'unknown',
          title: isZh()
            ? '连接中断 — 可使用原恢复标识安全查询状态'
            : 'Stream Disconnected — Recover Safely Without Re-Billing',
          message: isZh()
            ? '切勿盲目重新提交计费请求；点击「恢复原运行状态」查询已受理的执行与扣费结算。'
            : 'Do not re-submit a new billable request. Recover the existing run status using the preserved recovery handle.',
          retryable: true
        }
        return
      }

      const classified = normalizePlaygroundError(err, {
        requiredGrantFallback: 'playground.run',
        isZh: isZh(),
        isNonOwnedKey: !isKeyOwned(payload.api_key_id)
      })
      state.errorNotice = classified
      state.status = classified.kind === 'unknown' ? 'unknown' : 'failed'
    } finally {
      if (activeAbortController === controller) {
        activeAbortController = null
      }
    }
  }

  async function executeApprovedReplay(
    workspaceId: string,
    projectId: string,
    sourceRequestId: string,
    payload: PlaygroundReplay,
    explicitIdempotencyKey?: string
  ): Promise<void> {
    if (!payload.funding_acknowledged) {
      throw new Error('Funding acknowledgement is required before replaying a request')
    }
    abortActiveLocalStream()
    const generation = ++activeGeneration
    const controller = new AbortController()
    activeAbortController = controller
    lastWorkspaceId = workspaceId
    lastProjectId = projectId
    terminalEventSeen = false
    stopRequestedByUser = false
    toolCallIndexMap.clear()
    startedTimestampMs = now()

    const idemKey = explicitIdempotencyKey || generateIdempotencyKey()
    Object.assign(state, createInitialSnapshot(), {
      status: 'submitting',
      idempotencyKey: idemKey,
      submittedPayload: payload
    })

    try {
      const result = await api.replayRequest(workspaceId, projectId, sourceRequestId, payload, {
        idempotencyKey: idemKey,
        signal: controller.signal,
        onFrame: (frame) => handleSseFrame(frame, generation)
      })
      if (generation !== activeGeneration) return
      if (result.kind === 'summary') {
        applySummary(result.summary)
      } else if (result.kind === 'result') {
        applyResult(result.result)
      } else if (!terminalEventSeen) {
        state.elapsedMs = Math.max(0, now() - startedTimestampMs)
        state.status = stopRequestedByUser ? 'canceled' : 'disconnected_recoverable'
      }
    } catch (err) {
      if (generation !== activeGeneration) return
      state.elapsedMs = Math.max(0, now() - startedTimestampMs)
      const classified = normalizePlaygroundError(err, {
        requiredGrantFallback: 'requests.replay',
        isZh: isZh(),
        isNonOwnedKey: !isKeyOwned(payload.api_key_id)
      })
      state.errorNotice = classified
      state.status = classified.kind === 'unknown' ? 'unknown' : 'failed'
    } finally {
      if (activeAbortController === controller) {
        activeAbortController = null
      }
    }
  }

  async function stop(workspaceId = lastWorkspaceId, projectId = lastProjectId): Promise<void> {
    stopRequestedByUser = true
    if (!state.runId) {
      abortActiveLocalStream()
      if (state.idempotencyKey) {
        state.status = 'disconnected_recoverable'
      } else {
        state.status = 'canceled'
      }
      return
    }

    state.status = 'cancel_requested'
    try {
      const summary = await api.cancelRun(workspaceId, projectId, state.runId)
      abortActiveLocalStream()
      applySummary(summary)
      if (summary.state === 'cancel_requested' || summary.state === 'running') {
        const refreshed = await api
          .getRunDetail(workspaceId, projectId, state.runId)
          .catch(() => summary)
        applySummary(refreshed)
      }
    } catch (err) {
      abortActiveLocalStream()
      state.errorNotice = classifyConsoleError(err, {
        requiredGrantFallback: 'playground.cancel',
        isZh: isZh()
      })
    }
  }

  async function recoverByIdempotencyKey(
    workspaceId = lastWorkspaceId,
    projectId = lastProjectId
  ): Promise<PlaygroundSummary | null> {
    if (!state.idempotencyKey) return null
    state.errorNotice = null
    try {
      const summary = await api.getRunByIdempotencyKey(
        workspaceId,
        projectId,
        state.idempotencyKey
      )
      applySummary(summary)
      return summary
    } catch (err) {
      state.errorNotice = classifyConsoleError(err, {
        requiredGrantFallback: 'playground.read',
        isZh: isZh()
      })
      return null
    }
  }

  async function queryRunStatus(
    workspaceId = lastWorkspaceId,
    projectId = lastProjectId
  ): Promise<PlaygroundSummary | null> {
    if (!state.runId) {
      return recoverByIdempotencyKey(workspaceId, projectId)
    }
    state.errorNotice = null
    try {
      const summary = await api.getRunDetail(workspaceId, projectId, state.runId)
      applySummary(summary)
      return summary
    } catch (err) {
      state.errorNotice = classifyConsoleError(err, {
        requiredGrantFallback: 'playground.read',
        isZh: isZh()
      })
      return null
    }
  }

  return {
    state,
    reset,
    markConsentPending,
    cancelConsent,
    executeApprovedRun,
    executeApprovedReplay,
    stop,
    recoverByIdempotencyKey,
    queryRunStatus
  }
}
