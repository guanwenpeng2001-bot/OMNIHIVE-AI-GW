import type { UsageLog, UsagePricingSnapshot } from '@/types'

type Translate = (key: string) => string
export const billingEvidenceFields = ['pricingState', 'settlementState', 'completionState', 'source', 'version', 'conditions', 'quantity', 'unit', 'unitPrice', 'charged', 'requestId', 'jobId', 'sessionId', 'turnId', 'snapshot'] as const

// Units are taken from the frozen calculator contract, never inferred from cost.
export function pricingUnit(s: UsagePricingSnapshot): string {
  if (s.token_prices) return 'tokens'
  if (s.mode === 'audio') return s.size_tier === 'tts' ? 'million_characters' : s.size_tier === 'stt' ? 'hours' : 'unknown'
  if (s.mode === 'realtime') return 'characters'
  if (s.mode === 'asr' || s.mode === 'video') return 'seconds'
  if (s.mode === '3d') return 'models'
  if (s.mode === 'image') return 'images'
  if (s.mode === 'per_request' && ['calls', 'thousand_calls'].includes(s.size_tier || '')) return s.size_tier!
  return 'unknown'
}

export function billingExplanationValues(log: UsageLog, t: Translate): string[] {
  const e = log.billing_explanation
  const s = e?.snapshot
  const unknown = t('usage.evidence.unknown')
  const state = (v?: string | null) => v ? t(`usage.evidence.states.${v}`) : unknown
  const conditions = s ? JSON.stringify({
    tier: s.service_tier ?? null, size: s.size_tier ?? null,
    thinking: s.thinking_enabled ?? null, long_context: s.long_context_applied ?? false,
    rate: s.rate_multiplier, time: s.time_multiplier, reasoning: s.reasoning_multiplier,
    adjustment: s.adjustment_multiplier ?? null, fx: s.fx ?? null,
    explicit_cache: s.tokens?.QianwenExplicitCache ?? null, batch: s.tokens?.QianwenBatchChat ?? null,
  }) : unknown
  return [
    state(e?.pricing_state), state(e?.settlement_state), state(e?.completion_state),
    s?.source || e?.price_source || unknown, s?.price_card_version || e?.price_card_version || unknown, conditions,
    s ? (s.token_prices ? JSON.stringify(s.tokens) : String(s.units ?? 0)) : unknown,
    s ? t(`usage.evidence.units.${pricingUnit(s)}`) : unknown,
    s ? (s.unit_price != null ? String(s.unit_price) : s.token_prices ? JSON.stringify(s.token_prices) : unknown) : unknown,
    s?.charged_amount != null ? s.charged_amount.toFixed(8) : unknown,
    log.request_id || unknown, e?.job_id || unknown, e?.session_id || log.session_id || unknown,
    e?.turn_id || unknown, s ? JSON.stringify(s) : unknown,
  ]
}
