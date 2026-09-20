import { buildApiUrl } from '../client'
import { ADMIN_UI_REQUEST_HEADER } from '../adminUIRequest'

export interface AccountTestRequest {
  model_id: string
  prompt: string
  mode?: 'default' | 'compact' | 'text' | 'image' | 'video' | 'search' | 'tts' | 'stt' | 'realtime'
  image_data_url?: string
  audio_data_url?: string
}

export type AccountTestEvent =
  | { type: 'test_start'; model?: string }
  | { type: 'content' | 'status'; text?: string }
  | { type: 'image'; image_url?: string; mime_type?: string }
  | { type: 'audio'; audio_url?: string; mime_type?: string }
  | { type: 'video'; video_url?: string; mime_type?: string }
  | { type: 'test_complete'; success?: boolean; error?: string }
  | { type: 'error'; error?: string }

function isAccountTestEvent(value: unknown): value is AccountTestEvent {
  if (!value || typeof value !== 'object' || !('type' in value)) return false
  if (!['test_start', 'content', 'status', 'image', 'audio', 'video', 'test_complete', 'error'].includes(String(value.type))) return false
  for (const field of ['model', 'text', 'image_url', 'audio_url', 'video_url', 'mime_type', 'error']) {
    if (field in value && typeof Reflect.get(value, field) !== 'string') return false
  }
  return !('success' in value) || typeof value.success === 'boolean'
}

/** POST SSE endpoint; the caller owns cancellation and presentation of terminal events. */
export async function streamAccountTest(
  accountId: number,
  request: AccountTestRequest,
  onEvent: (event: AccountTestEvent) => void,
  signal: AbortSignal
): Promise<void> {
  const response = await fetch(buildApiUrl(`/admin/accounts/${accountId}/test`), {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      'Content-Type': 'application/json',
      [ADMIN_UI_REQUEST_HEADER]: '1'
    },
    body: JSON.stringify(request),
    signal
  })
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`)
  const reader = response.body?.getReader()
  if (!reader) throw new Error('No response body')
  const decoder = new TextDecoder()
  let buffer = ''
  const acceptLine = (line: string) => {
    if (!line.startsWith('data:')) return
    const payload = line.slice(5).trim()
    if (!payload) return
    let event: unknown
    try {
      event = JSON.parse(payload)
    } catch {
      return
    }
    if (isAccountTestEvent(event)) onEvent(event)
  }
  try {
    while (true) {
      signal.throwIfAborted()
      const { done, value } = await reader.read()
      signal.throwIfAborted()
      buffer += decoder.decode(value, { stream: !done })
      const lines = buffer.split('\n')
      buffer = lines.pop() ?? ''
      for (const line of lines) acceptLine(line)
      if (done) {
        acceptLine(buffer)
        break
      }
    }
  } finally {
    reader.releaseLock()
  }
}
