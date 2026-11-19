import { describe, expect, it } from 'vitest'
import type { PolicyDraft, PolicyHead, PolicySimulation, RoutingTrace, SimulationRequest } from '../api'
import {
  canPublish, composeRestrictions, countRankingCandidates, mergePolicyForm, policyForm,
  serializePolicy, traceSummary, validatePolicy, validScenario,
} from '../policy'

const scope = { type: 'project', id: '2' } as const

function policy(fields: Partial<PolicyDraft> = {}): PolicyDraft {
  return { name: 'Europe', scope, ...fields }
}

function trace(version = 'draft'): RoutingTrace {
  return {
    schema_version: 1, mode: 'simulation', snapshot_id: 's1', as_of: '2026-11-19T10:00:00Z',
    policy_version: version, catalog_version: 'c1', price_version: 'p1', seed: '0',
    origins: [], stages: [], selected: { model_id: 'm1', candidate_id: 'c1', funding_source: 'wallet' },
    candidates: [
      { id: 'c2', state: 'eligible', rank: 2, reasons: [], factors: [] },
      { id: 'c1', state: 'eligible', rank: 1, reasons: [], factors: [] },
      { id: 'c3', state: 'excluded', rank: null, reasons: [{ code: 'PRICE_UNKNOWN', stage: 'price' }], factors: [] },
    ],
    uncertainties: ['CAPACITY_CAN_CHANGE'],
    redactions: ['connection_identity'],
  }
}

describe('routing policy composition', () => {
  it('intersects lists, keeps prohibitions and chooses the exact lower rate', () => {
    const parent = policy({
      constraints: {
        models: { mode: 'allow_list', values: ['a', 'b'] },
        regions: { mode: 'allow_list', values: ['DE', 'FR'] },
        max_input_rate: { currency: 'USD', amount: '0.000000000000000001', per: '1000000', unit: 'input_token' },
        retention: 'strict_zero', training: 'prohibited',
      },
      fallback: { byok_to_platform: false },
    })
    const child = policy({
      constraints: {
        models: { mode: 'allow_list', values: ['b', 'c'] },
        regions: { mode: 'deny_all' },
        max_input_rate: { currency: 'USD', amount: '0.000000000000000002', per: '1000000', unit: 'input_token' },
      },
      fallback: { byok_to_platform: true },
    })
    expect(composeRestrictions([parent, child])).toEqual({
      models: ['b'], regions: [], rate: '0.000000000000000001',
      retention: true, training: true, fundingDenied: true,
    })
  })

  it('preserves deny-all, explicit empty overrides and an exact zero ceiling', () => {
    const head = policy({
      constraints: {
        models: { mode: 'deny_all' },
        max_input_rate: { currency: 'USD', amount: '0', per: '1000000', unit: 'input_token' },
      },
      preferences: { ordered_provider_ids: [] },
      fallback: { model_ids: [], byok_to_platform: false },
    })
    const draft = serializePolicy(policyForm(head), scope)
    expect(draft.constraints?.models).toEqual({ mode: 'deny_all' })
    expect(draft.constraints?.max_input_rate?.amount).toBe('0')
    expect(draft.preferences?.ordered_provider_ids).toEqual([])
    expect(draft.fallback?.model_ids).toEqual([])
    expect(draft.fallback?.byok_to_platform).toBe(false)
    expect(validatePolicy({ ...policyForm(head), regions: { mode: 'allow_list', items: ' , ' } })).toBe('regionListRequired')
  })

  it('reapplies edited fields only after a conflict', () => {
    const base = policyForm(policy())
    const local = { ...base, name: 'Local' }
    const latest = { ...base, name: 'Server', rate: '1.5' }
    expect(mergePolicyForm(base, local, latest)).toMatchObject({ name: 'Local', rate: '1.5' })
  })
})

describe('routing simulation evidence', () => {
  const request: SimulationRequest = {
    version: 'draft', project_id: '2', model_id: 'm1', protocol: 'openai_chat',
    action: 'text.chat', input_token_bound: '0', output_token_bound: '128', seed: '0',
  }

  it('accepts seed zero and large exact values without JS number conversion', () => {
    expect(validScenario(request)).toBe(true)
    expect(validScenario({ ...request, seed: '18446744073709551615' })).toBe(true)
    expect(validScenario({ ...request, seed: '18446744073709551616' })).toBe(false)
    expect(validScenario({ ...request, seed: '01' })).toBe(false)
    expect(validScenario({ ...request, api_key_id: 'sk_plaintext' })).toBe(false)
  })

  it('keeps rank, exclusions and unknown affinity distinct', () => {
    const summary = traceSummary(trace())
    expect(summary.ranked.map(candidate => candidate.id)).toEqual(['c1', 'c2'])
    expect(summary.excluded[0].reasons[0].code).toBe('PRICE_UNKNOWN')
    expect(summary.affinity).toBeNull()
    const data = trace()
    data.candidates[1].factors.push({
      name: 'affinity_hit', value: 'true', unit: '', source: 'snapshot', freshness: 'fresh',
      window_seconds: null, samples: null, observed_at: null,
    })
    expect(traceSummary(data).affinity?.value).toBe('true')
  })

  it('uses measured candidate count only for compatible hard constraints', () => {
    const baseline = policyForm(policy())
    expect(countRankingCandidates(baseline, { ...baseline, template: 'low_latency' }, scope, trace())).toBe(2)
    expect(countRankingCandidates(baseline, { ...baseline, rate: '1' }, scope, trace())).toBeNull()
    expect(countRankingCandidates(baseline, baseline, scope, null)).toBeNull()
  })

  it('permits publication only for the current saved draft and unexpired proof', () => {
    const now = Date.parse('2026-11-19T10:00:00Z')
    const head: PolicyHead = { id: 'p', active_version: 'active', draft_version: 'draft', base_version: 'active', etag: '3', definition: policy() }
    const proof: PolicySimulation = {
      trace: trace(), simulation_id: 'proof', baseline: null, diff: null,
      impact: { projects: '0', keys: '0' }, expires_at: '2026-11-19T10:05:00Z',
    }
    expect(canPublish(head, proof, false, now)).toBe(true)
    expect(canPublish(head, proof, true, now)).toBe(false)
    expect(canPublish({ ...head, base_version: 'previous' }, proof, false, now)).toBe(false)
    expect(canPublish({ ...head, draft_version: 'different' }, proof, false, now)).toBe(false)
    expect(canPublish(head, proof, false, now + 300_000)).toBe(false)
  })
})
