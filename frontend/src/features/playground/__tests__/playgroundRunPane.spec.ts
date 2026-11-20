import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import PlaygroundRunPane from '../components/PlaygroundRunPane.vue'
import type { PlaygroundSessionSnapshot, PlaygroundSideConfig } from '../types'

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
    sseFrames: [],
    inputTokens: 10,
    outputTokens: 20,
    ttftMs: 120,
    elapsedMs: 420,
    priceVersion: 'pv_1',
    settlementState: 'pending',
    errorNotice: null,
    startedAtMs: 1000,
    firstTokenAtMs: 1120,
    endedAtMs: 1420,
    rawRequestPayload: null,
    ...overrides
  }
}

describe('PlaygroundRunPane async settlement confirmation state', () => {
  it('shows friendly pending-ledger confirmation notice and emits query-status on refresh', async () => {
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
