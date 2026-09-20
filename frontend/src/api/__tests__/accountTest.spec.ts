import { afterEach, describe, expect, it, vi } from 'vitest'
import { streamAccountTest, type AccountTestEvent } from '../admin/accountTest'

vi.mock('../client', () => ({ buildApiUrl: (path: string) => `/api/v1${path}` }))

afterEach(() => vi.unstubAllGlobals())

describe('account test SSE contract', () => {
  it('decodes split UTF-8, CRLF and a final event without a newline, ignoring invalid fields', async () => {
    const bytes = new TextEncoder().encode('data:{"type":"content","text":"你好"}\r\n\ndata: {"type":"image","image_url":42}\n\ndata: {"type":"test_complete","success":true}')
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        for (const byte of bytes) controller.enqueue(Uint8Array.of(byte))
        controller.close()
      }
    })
    const fetchMock = vi.fn().mockResolvedValue(new Response(stream))
    vi.stubGlobal('fetch', fetchMock)
    const events: AccountTestEvent[] = []
    const controller = new AbortController()
    await streamAccountTest(42, { model_id: 'model', prompt: '' }, event => events.push(event), controller.signal)
    expect(events).toEqual([{ type: 'content', text: '你好' }, { type: 'test_complete', success: true }])
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/admin/accounts/42/test', expect.objectContaining({
      method: 'POST', signal: controller.signal, body: JSON.stringify({ model_id: 'model', prompt: '' })
    }))
    expect(stream.locked).toBe(false)
  })

  it('forwards abort and does not deliver data returned after cancellation', async () => {
    const controller = new AbortController()
    const releaseLock = vi.fn()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      body: { getReader: () => ({
        read: async () => {
          controller.abort()
          return { done: false, value: new TextEncoder().encode('data: {"type":"test_complete","success":true}\n') }
        },
        releaseLock
      }) }
    }))
    const onEvent = vi.fn()
    await expect(streamAccountTest(42, { model_id: 'model', prompt: '' }, onEvent, controller.signal)).rejects.toMatchObject({ name: 'AbortError' })
    expect(onEvent).not.toHaveBeenCalled()
    expect(releaseLock).toHaveBeenCalledOnce()
  })

  it('rejects a non-success HTTP response before consuming the stream', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 403 })))
    await expect(streamAccountTest(42, { model_id: 'model', prompt: '' }, vi.fn(), new AbortController().signal)).rejects.toThrow('403')
  })
})
