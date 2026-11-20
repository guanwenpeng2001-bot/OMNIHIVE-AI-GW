import type { components } from '@/api/generated/openapi'
import { apiClient, buildGatewayUrl } from '@/api/client'
import { getLocale } from '@/i18n'
import { PlaygroundSseParser, type ParsedSseRecord } from './sseParser'
import type {
  ExactQuote,
  ExactQuoteInput,
  PlaygroundReplay,
  PlaygroundResult,
  PlaygroundSubmit,
  PlaygroundSummary
} from './types'

function v2ProjectUrl(workspaceId: string, projectId: string, suffix: string): string {
  return buildGatewayUrl(`/api/v2/workspaces/${workspaceId}/projects/${projectId}${suffix}`)
}

function readAccessToken(): string | null {
  if (typeof window === 'undefined') return null
  try {
    const v1Token = window.localStorage.getItem('omnihive.auth.token.v1')
    if (v1Token) {
      try {
        const parsed = JSON.parse(v1Token)
        if (typeof parsed === 'string' && parsed) return parsed
        if (parsed && typeof parsed.value === 'string' && parsed.value) return parsed.value
      } catch {
        return v1Token
      }
    }
    return window.localStorage.getItem('auth_token')
  } catch {
    return null
  }
}

export type PlaygroundExecutionResponse =
  | { kind: 'summary'; summary: PlaygroundSummary }
  | { kind: 'result'; result: PlaygroundResult }
  | { kind: 'stream_completed' }

export interface StreamCallbacks {
  onFrame: (frame: ParsedSseRecord) => void
}

async function executeStreamingPost(
  url: string,
  body: PlaygroundSubmit | PlaygroundReplay,
  options: {
    idempotencyKey: string
    stream: boolean
    signal?: AbortSignal
    onFrame: (frame: ParsedSseRecord) => void
    fetchImpl?: typeof fetch
  }
): Promise<PlaygroundExecutionResponse> {
  const fetcher = options.fetchImpl ?? globalThis.fetch
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    Accept: options.stream ? 'text/event-stream' : 'application/json',
    'Idempotency-Key': options.idempotencyKey
  }
  try {
    headers['Accept-Language'] = getLocale()
  } catch {
    headers['Accept-Language'] = 'en'
  }
  const token = readAccessToken()
  if (token) {
    headers.Authorization = `Bearer ${token}`
  }

  const response = await fetcher(url, {
    method: 'POST',
    credentials: 'include',
    headers,
    body: JSON.stringify(body),
    signal: options.signal
  })

  const contentType = (response.headers.get('content-type') || '').toLowerCase()

  if (!response.ok) {
    let errorData: Record<string, unknown> = {}
    try {
      errorData = (await response.json()) as Record<string, unknown>
    } catch {
      const text = await response.text().catch(() => '')
      errorData = { message: text || `HTTP ${response.status}` }
    }
    throw {
      status: response.status,
      code: errorData.code ?? response.status,
      reason: typeof errorData.reason === 'string' ? errorData.reason : undefined,
      message:
        (typeof errorData.message === 'string' && errorData.message) ||
        `Playground request failed (${response.status})`,
      metadata:
        errorData.metadata && typeof errorData.metadata === 'object'
          ? (errorData.metadata as Record<string, unknown>)
          : undefined,
      requestId: response.headers.get('x-request-id') || undefined
    }
  }

  if (contentType.includes('application/json')) {
    const envelope = (await response.json()) as {
      code?: number
      message?: string
      data?: PlaygroundSummary | PlaygroundResult
    }
    const payload = (envelope && 'data' in envelope ? envelope.data : envelope) as
      | PlaygroundSummary
      | PlaygroundResult
    if (payload && 'outcome' in payload) {
      return { kind: 'result', result: payload }
    }
    return { kind: 'summary', summary: payload as PlaygroundSummary }
  }

  if (!response.body) {
    throw new Error('Streaming response missing readable body')
  }

  const reader = response.body.getReader()
  const decoder = new TextDecoder('utf-8')
  const parser = new PlaygroundSseParser()

  try {
    while (true) {
      const { value, done } = await reader.read()
      if (done) break
      const chunk = decoder.decode(value, { stream: true })
      const frames = parser.feed(chunk)
      for (const frame of frames) {
        options.onFrame(frame)
      }
    }
    const tailChunk = decoder.decode()
    if (tailChunk) {
      for (const frame of parser.feed(tailChunk)) {
        options.onFrame(frame)
      }
    }
    for (const frame of parser.flush()) {
      options.onFrame(frame)
    }
  } finally {
    reader.releaseLock()
  }

  return { kind: 'stream_completed' }
}

export const playgroundApi = {
  submitRun(
    workspaceId: string,
    projectId: string,
    payload: PlaygroundSubmit,
    options: {
      idempotencyKey: string
      signal?: AbortSignal
      onFrame: (frame: ParsedSseRecord) => void
      fetchImpl?: typeof fetch
    }
  ): Promise<PlaygroundExecutionResponse> {
    return executeStreamingPost(
      v2ProjectUrl(workspaceId, projectId, '/playground/runs'),
      payload,
      {
        idempotencyKey: options.idempotencyKey,
        stream: payload.options?.stream !== false,
        signal: options.signal,
        onFrame: options.onFrame,
        fetchImpl: options.fetchImpl
      }
    )
  },

  replayRequest(
    workspaceId: string,
    projectId: string,
    requestId: string,
    payload: PlaygroundReplay,
    options: {
      idempotencyKey: string
      signal?: AbortSignal
      onFrame: (frame: ParsedSseRecord) => void
      fetchImpl?: typeof fetch
    }
  ): Promise<PlaygroundExecutionResponse> {
    return executeStreamingPost(
      v2ProjectUrl(workspaceId, projectId, `/requests/${encodeURIComponent(requestId)}/replay`),
      payload,
      {
        idempotencyKey: options.idempotencyKey,
        stream: payload.options?.stream !== false,
        signal: options.signal,
        onFrame: options.onFrame,
        fetchImpl: options.fetchImpl
      }
    )
  },

  async getRunByIdempotencyKey(
    workspaceId: string,
    projectId: string,
    idempotencyKey: string,
    signal?: AbortSignal
  ): Promise<PlaygroundSummary> {
    const { data } = await apiClient.get<PlaygroundSummary>(
      v2ProjectUrl(workspaceId, projectId, '/playground/runs/by-idempotency-key'),
      {
        baseURL: '',
        headers: { 'Idempotency-Key': idempotencyKey },
        signal
      }
    )
    return data
  },

  async getRunDetail(
    workspaceId: string,
    projectId: string,
    runId: string,
    signal?: AbortSignal
  ): Promise<PlaygroundSummary> {
    const { data } = await apiClient.get<PlaygroundSummary>(
      v2ProjectUrl(workspaceId, projectId, `/playground/runs/${encodeURIComponent(runId)}`),
      { baseURL: '', signal }
    )
    return data
  },

  async cancelRun(
    workspaceId: string,
    projectId: string,
    runId: string
  ): Promise<PlaygroundSummary> {
    const { data } = await apiClient.post<PlaygroundSummary>(
      v2ProjectUrl(workspaceId, projectId, `/playground/runs/${encodeURIComponent(runId)}/cancel`),
      {},
      { baseURL: '' }
    )
    return data
  },

  async quoteProjectModel(
    workspaceId: string,
    projectId: string,
    payload: ExactQuoteInput,
    signal?: AbortSignal
  ): Promise<ExactQuote> {
    const { data } = await apiClient.post<ExactQuote>(
      v2ProjectUrl(workspaceId, projectId, '/quotes'),
      payload,
      { baseURL: '', signal }
    )
    return data
  },

  async getEffectiveQuota(
    workspaceId: string,
    projectId: string,
    params?: { api_key_id?: string; model_id?: string },
    signal?: AbortSignal
  ): Promise<components['schemas']['CostQuota']> {
    const { data } = await apiClient.get<components['schemas']['CostQuota']>(
      v2ProjectUrl(workspaceId, projectId, '/billing/quotas'),
      { baseURL: '', params, signal }
    )
    return data
  }
}
