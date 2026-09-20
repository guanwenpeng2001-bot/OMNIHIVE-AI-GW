import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { subscribeQPS } from '@/api/admin/ops'

const { post } = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('@/api/client', () => ({
  apiClient: { post },
  buildGatewayUrl: (path: string) => `http://localhost${path}`
}))

class TestSocket {
  static OPEN = 1
  static CONNECTING = 0
  static instances: TestSocket[] = []
  readyState = 0
  onopen: (() => void) | null = null
  onclose: ((event: { code: number }) => void) | null = null
  onmessage: unknown = null
  onerror: unknown = null
  constructor(public url: string, public protocols: string[]) { TestSocket.instances.push(this) }
  close() { this.readyState = 3; this.onclose?.({ code: 1000 }) }
}

describe('admin websocket tickets', () => {
  let stop: (() => void) | undefined
  beforeEach(() => {
    vi.useFakeTimers()
    vi.stubGlobal('WebSocket', TestSocket)
    TestSocket.instances = []
    post.mockReset()
    localStorage.setItem('auth_token', 'main-session-secret')
  })
  afterEach(() => { stop?.(); stop = undefined; vi.unstubAllGlobals(); vi.useRealTimers(); localStorage.clear() })

  it('obtains a fresh ticket for each connection and keeps the main token out of the handshake', async () => {
    post.mockResolvedValueOnce({ data: { ticket: 'ticket-first' } }).mockResolvedValueOnce({ data: { ticket: 'ticket-second' } })
    stop = subscribeQPS(vi.fn(), { staleTimeoutMs: 0, reconnectBaseDelayMs: 1 })
    await Promise.resolve()
    const first = TestSocket.instances[0]
    expect(first.protocols).toEqual(['sub2api-admin', 'ticket.ticket-first'])
    expect(first.url).not.toContain('main-session-secret')
    first.onopen?.()
    first.close()
    await vi.advanceTimersByTimeAsync(5000)
    expect(post).toHaveBeenCalledTimes(2)
    expect(TestSocket.instances[1].protocols).toEqual(['sub2api-admin', 'ticket.ticket-second'])
  })

  it('does not open a socket after cancellation while ticket issuance is pending', async () => {
    let resolveTicket!: (value: { data: { ticket: string } }) => void
    post.mockReturnValue(new Promise((resolve) => { resolveTicket = resolve }))
    stop = subscribeQPS(vi.fn())
    stop()
    resolveTicket({ data: { ticket: 'late-ticket' } })
    await Promise.resolve()
    expect(TestSocket.instances).toHaveLength(0)
  })
})
