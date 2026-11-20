import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import PlaygroundRunPane from '../components/PlaygroundRunPane.vue'
import type { PlaygroundSessionSnapshot, PlaygroundSideConfig } from '../types'

let currentLocale: 'en' | 'zh' = 'en'

function resolveTranslation(key: string, params?: Record<string, unknown>): string {
  let value: unknown = currentLocale === 'zh' ? zh : en
  for (const part of key.split('.')) {
    if (value && typeof value === 'object' && part in (value as Record<string, unknown>)) {
      value = (value as Record<string, unknown>)[part]
    } else {
      return key
    }
  }
  if (typeof value !== 'string') return key
  if (!params) return value
  return value.replace(/\{(\w+)\}/g, (_, token: string) =>
    params[token] !== undefined ? String(params[token]) : `{${token}}`
  )
}

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: resolveTranslation,
    locale: { value: currentLocale },
  }),
}))

const sideConfig: PlaygroundSideConfig = {
  label: 'Side A',
  apiKeyId: '12',
  modelId: 'gpt-5.6-sol',
  protocol: 'openai_chat',
  temperature: 0.5,
  maxOutputTokens: 1024,
  reasoningEffort: '',
  stream: true,
  includeRawEvents: false,
  retainTextPreview: true
}

function makeSession(overrides: Partial<PlaygroundSessionSnapshot> = {}): PlaygroundSessionSnapshot {
  return {
    status: 'succeeded',
    runId: 'run-123',
    requestId: 'req-456',
    idempotencyKey: 'idem-789',
    lastSeq: 3,
    outputText: 'Hello world',
    reasoningText: '',
    toolCalls: [],
    syncOutput: null,
    diagnostics: [],
    rawEvents: [],
    sseFrames: [],
    inputTokens: '10',
    outputTokens: '20',
    ttftMs: 120,
    elapsedMs: 420,
    priceVersion: 'pv_1',
    payerUserId: '1',
    settlementState: 'pending',
    statusUrl: null,
    detailUrl: null,
    errorNotice: null,
    submittedPayload: null,
    ...overrides
  }
}

describe('PlaygroundRunPane async settlement confirmation state', () => {
  it('shows friendly pending-ledger confirmation notice and emits query-status on refresh', async () => {
    currentLocale = 'zh'
    const wrapper = mount(PlaygroundRunPane, {
      props: {
        sideKey: 'A',
        config: sideConfig,
        session: makeSession({ status: 'succeeded', settlementState: 'pending' }),
        isZh: true
      }
    })

    const banner = wrapper.get('[data-testid="settlement-pending-A"]')
    expect(banner.text()).toContain('推理已完成 · 账本异步确认中')

    await wrapper.get('[data-testid="refresh-settlement-btn-A"]').trigger('click')
    expect(wrapper.emitted('query-status')).toHaveLength(1)
  })

  it('renders English label and hides pending banner once settlement completes', async () => {
    currentLocale = 'en'
    const wrapper = mount(PlaygroundRunPane, {
      props: {
        sideKey: 'A',
        config: sideConfig,
        session: makeSession({ status: 'succeeded', settlementState: 'pending' }),
        isZh: false
      }
    })

    expect(wrapper.get('[data-testid="settlement-pending-A"]').text()).toContain(
      'Completed · Ledger confirming asynchronously'
    )

    await wrapper.setProps({
      session: makeSession({ status: 'succeeded', settlementState: 'settled' })
    })
    expect(wrapper.find('[data-testid="settlement-pending-A"]').exists()).toBe(false)
  })
})

describe('PlaygroundRunPane collapsible Raw Request JSON & SSE Frames inspector (FE-F01-01)', () => {
  it('expands diagnostics inspector to display formatted submittedPayload, sseFrames, and rawEvents', async () => {
    currentLocale = 'en'
    const wrapper = mount(PlaygroundRunPane, {
      props: {
        sideKey: 'A',
        config: sideConfig,
        session: makeSession({
          submittedPayload: {
            api_key_id: '12',
            model_id: 'gpt-5.6-sol',
            protocol: 'openai_chat',
            action: 'text.chat',
            input: { messages: [{ role: 'user', content: 'ping' }] },
            options: { stream: true, include_raw_events: true },
            funding_acknowledged: true,
          },
          sseFrames: [
            {
              id: '1',
              event: 'started',
              seq: 1,
              receivedAtMs: 25,
              summary: 'run_id=run-123',
              rawData: JSON.stringify({ seq: 1, run_id: 'run-123', request_id: 'req-456' }),
            },
            {
              id: '2',
              event: 'delta',
              seq: 2,
              receivedAtMs: 120,
              summary: 'text: Hello world',
              rawData: JSON.stringify({ seq: 2, kind: 'text', text: 'Hello world' }),
            },
          ],
          rawEvents: [{ provider: 'openai', chunk: { id: 'chatcmpl-1' } }],
        }),
        isZh: false
      }
    })

    // Collapsed by default
    expect(wrapper.find('[data-testid="playground-diagnostics-inspector-A"]').exists()).toBe(false)

    // Expand inspector
    await wrapper.get('[data-testid="toggle-diagnostics-A"]').trigger('click')
    expect(wrapper.find('[data-testid="playground-diagnostics-inspector-A"]').exists()).toBe(true)

    // Raw Request JSON tab active by default
    const payloadPre = wrapper.get('[data-testid="diagnostics-submitted-payload-A"]')
    expect(payloadPre.text()).toContain('"model_id": "gpt-5.6-sol"')
    expect(payloadPre.text()).toContain('"content": "ping"')

    // Switch to Raw SSE Frames tab
    await wrapper.get('[data-testid="diagnostics-tab-sse-A"]').trigger('click')
    const framesList = wrapper.get('[data-testid="diagnostics-sse-frames-A"]')
    expect(framesList.text()).toContain('started')
    expect(framesList.text()).toContain('delta')
    expect(framesList.text()).toContain('"run_id": "run-123"')

    const rawEventsList = wrapper.get('[data-testid="diagnostics-raw-events-A"]')
    expect(rawEventsList.text()).toContain('"chatcmpl-1"')
  })
})
