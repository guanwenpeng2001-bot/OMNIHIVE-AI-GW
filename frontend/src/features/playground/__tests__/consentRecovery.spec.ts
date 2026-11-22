import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { combinedExactBound, isExecutableEstimate } from '../costEstimate'
import { createPlaygroundRunController, type PlaygroundApiAdapter } from '../runMachine'
import { listRecoveries } from '../recovery'
import type { PlaygroundCostEstimate, PlaygroundSubmit } from '../types'
import PlaygroundCostConfirmModal from '../components/PlaygroundCostConfirmModal.vue'

const estimate = (patch: Partial<PlaygroundCostEstimate> = {}): PlaygroundCostEstimate => ({ sideLabel: 'A', modelId: 'model', apiKeyId: '15', protocol: 'openai_chat', estimatedInputTokens: 1, maxOutputTokens: 1, inputRate: null, outputRate: null, currency: 'USD', estimatedInputCost: '0.01', maxOutputCost: '0.02', maxTotalCost: '0.03', rateKnown: true, quoteState: 'authoritative', keyScoped: true, priceVersion: 'sale_1', payloadBytes: 64, exceedsBodyLimit: false, ...patch })

describe('executable funding consent', () => {
  it.each([{ quoteState: 'loading' }, { quoteState: 'unpriced' }, { quoteState: 'fallback_catalog' }, { keyScoped: false }, { priceVersion: null }, { budgetBlocked: true }, { maxTotalCost: null }])('blocks incomplete or unconfirmed quote %j', patch => {
    const item = estimate(patch as Partial<PlaygroundCostEstimate>)
    expect(isExecutableEstimate(item)).toBe(false)
    const wrapper = mount(PlaygroundCostConfirmModal, { props: { open: true, teleportDisabled: true, estimates: [item] } })
    expect(wrapper.get('[data-testid="confirm-playground-funding"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
  it('uses exact decimal addition and refuses mixed currencies or a missing side', () => {
    expect(combinedExactBound([estimate({ maxTotalCost: '9007199254740993.01' }), estimate({ maxTotalCost: '0.02' })])).toBe('9007199254740993.03')
    expect(combinedExactBound([estimate(), estimate({ currency: 'EUR' })])).toBeNull()
    expect(combinedExactBound([estimate(), estimate({ quoteState: 'unpriced' })])).toBeNull()
  })
})

describe('recovery journal across scope remounts', () => {
  beforeEach(() => sessionStorage.clear())
  it('keeps only principal/scope/idempotency metadata and recovers without dispatching again', async () => {
    const api: PlaygroundApiAdapter = { submitRun: vi.fn().mockRejectedValue(new TypeError('network failed')), replayRequest: vi.fn(), getRunByIdempotencyKey: vi.fn().mockResolvedValue({ run_id: 'run_1', request_id: 'req_1', protocol: 'openai_chat', state: 'succeeded', last_seq: 1, content_retained: false, status_url: '/status', detail_url: '/detail', payer_user_id: '1', settlement_state: 'settled' }), getRunDetail: vi.fn(), cancelRun: vi.fn() }
    const payload: PlaygroundSubmit = { api_key_id: '15', model_id: 'model', protocol: 'openai_chat', action: 'text.chat', input: { messages: [{ role: 'user', content: 'PRIVATE-PROMPT' }] }, funding_acknowledged: true }
    const first = createPlaygroundRunController({ api, principalId: () => '1' })
    await first.executeApprovedRun('2', '9', payload, 'stable-idempotency')
    first.reset()
    const handles = listRecoveries('1', '2', '9')
    expect(handles).toHaveLength(1)
    expect(JSON.stringify(sessionStorage)).not.toContain('PRIVATE-PROMPT')
    expect(listRecoveries('other', '2', '9')).toEqual([])
    expect(listRecoveries('1', '2', '10')).toEqual([])
    const wrongPrincipal = createPlaygroundRunController({ api, principalId: () => 'other' })
    expect(wrongPrincipal.restoreRecovery(handles[0])).toBe(false)
    const restored = createPlaygroundRunController({ api, principalId: () => '1' })
    expect(restored.restoreRecovery(handles[0])).toBe(true)
    await restored.recoverByIdempotencyKey()
    expect(api.getRunByIdempotencyKey).toHaveBeenCalledWith('2', '9', 'stable-idempotency')
    expect(api.submitRun).toHaveBeenCalledTimes(1)
    expect(listRecoveries('1', '2', '9')).toEqual([])
  })
})
