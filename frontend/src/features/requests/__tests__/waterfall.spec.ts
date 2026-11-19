import { baseCompile } from '@intlify/message-compiler'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en/wp6'
import type { Schema } from '../api'
import RequestWaterfall from '../components/RequestWaterfall.vue'
import { waterfall } from '../waterfall'

function attempt(number: number, patch: Partial<Schema['RequestsAttempt']> = {}): Schema['RequestsAttempt'] {
  return {
    id: `attempt-${number}`, number, candidate_id: null, connection_id: '72',
    connection_name: 'Operator-only connection', started_at: '2026-11-19T08:10:00.100Z',
    duration_ms: 500, upstream_ttft_ms: 0, status: 200, acceptance: 'accepted', outcome: 'succeeded',
    reason: null, semantic_output: null, abandoned_uncertain: false, receipt_state: 'settled', conversion: null,
    ...patch,
  }
}

const detail = {
  timeline: [
    { kind: 'admitted', at: '2026-11-19T08:10:00Z', source: 'gateway_admission' },
    { kind: 'first_output', at: '2026-11-19T08:10:00.100Z', source: 'upstream_ttft_estimate' },
    { kind: 'client_completed', at: '2026-11-19T08:10:00.600Z', source: 'gateway_finalization' },
    { kind: 'settled', at: '2026-11-19T08:10:01Z', source: 'settlement_receipt' },
  ],
  attempts: [attempt(2)],
  abandoned_attempts: [attempt(1, {
    duration_ms: null, receipt_state: 'unconfirmed', acceptance: 'unknown', outcome: 'unknown', abandoned_uncertain: true,
  })],
} satisfies Pick<Schema['RequestsDetail'], 'timeline' | 'attempts' | 'abandoned_attempts'>

describe('request waterfall', () => {
  it('uses one shared observed scale and keeps the abandoned attempt independent', () => {
    const result = waterfall(detail)
    expect(result.phases.map(event => [event.kind, event.position])).toEqual([
      ['admitted', 0], ['first_output', 10], ['client_completed', 60], ['settled', 100],
    ])
    expect(result.attempts[0]).toMatchObject({ start: 10, width: 50, replacement: true })
    expect(result.abandoned[0]).toMatchObject({ width: null, replacement: false })
  })

  it('keeps genuine zero distinct from a missing milestone or duration', () => {
    const result = waterfall({
      ...detail, timeline: [detail.timeline[0]], attempts: [attempt(1, {
        started_at: '2026-11-19T08:10:00Z', receipt_state: 'unconfirmed', duration_ms: 0,
      })],
    })
    expect(result.phases.find(event => event.kind === 'first_output')?.position).toBeNull()
    expect(result.attempts[0].width).toBe(0)
  })

  it('shows replacement evidence and masks connection labels without the grant', async () => {
    const wrapper = mount(RequestWaterfall, {
      props: { detail: detail as Schema['RequestsDetail'], timezone: 'UTC', connectionAccess: false },
      global: { plugins: [createI18n({ legacy: false, locale: 'en',
        messageCompiler: message => new Function('return ' + baseCompile(message as string).code)(),
        messages: { en: { wp6: en } }, })] },
    })
    expect(wrapper.text()).toContain('Settled replacement')
    expect(wrapper.text()).toContain('Abandoned attempts')
    expect(wrapper.text()).toContain('Estimated')
    expect(wrapper.text()).not.toContain('Operator-only connection')
    await wrapper.setProps({ connectionAccess: true })
    expect(wrapper.text()).toContain('Operator-only connection')
  })
})
