import { describe, expect, it } from 'vitest'
import { toCatalogModelVM } from '../viewModel'
import type { CatalogModelSummary } from '../contract'

function baseSummary(overrides: Partial<CatalogModelSummary> = {}): CatalogModelSummary {
  return {
    id: 'm_test_1',
    provider: 'anthropic',
    model: 'claude-sonnet-4-5',
    display_name: 'Claude Sonnet 4.5',
    catalog_version: 'cv_1',
    protocols: ['anthropic_messages'],
    actions: ['text.chat'],
    modalities: ['text'],
    regions: ['us'],
    context_tokens: '200000',
    maturity: 'stable',
    catalog_state: 'listed',
    access_state: 'unknown',
    route_state: 'unknown',
    price_state: 'unpriced',
    input_rate: null,
    output_rate: null,
    evidence_state: 'fresh',
    observed_at: '2026-11-22T00:00:00Z',
    ...overrides
  }
}

describe('toCatalogModelVM 3-axis projection', () => {
  it('never guesses availability from price_state, route_state, or rates when availability fields are missing', () => {
    const vm = toCatalogModelVM(
      baseSummary({
        route_state: 'eligible',
        price_state: 'priced',
        input_rate: { currency: 'USD', amount: '3', per: '1000000', unit: 'input_token' }
      }),
      null,
      { isZh: true }
    )

    expect(vm.availability.state).toBe('unknown')
    expect(vm.availability.badge).toBeNull()
    expect(vm.availability.isCallable).toBe(false)
  })

  it('maps origin kinds and never exposes raw pi-ai strings in public UI labels', () => {
    const vm = toCatalogModelVM(
      baseSummary({
        source: {
          kinds: ['builtin_reviewed', 'pi_ai_reference', 'account_declaration'],
          candidate: true,
          discovered: true
        },
        availability: {
          state: 'live',
          publication: 'published',
          access: 'allowed'
        },
        billing: {
          state: 'unpriced',
          reason: 'no_sale_tariff',
          reference_price: {
            source: 'pi-ai',
            state: 'priced',
            additional_rates: null,
            time_bands: null,
            output_rate: null,
            input_rate: { currency: 'USD', amount: '3', per: '1000000', unit: 'input_token' }
          }
        }
      }),
      null,
      { isZh: true }
    )

    const joinedOriginLabels = vm.origin.badges.map((b) => b.label).join(' | ')
    expect(joinedOriginLabels).toContain('内置审阅')
    expect(joinedOriginLabels).toContain('社区目录（pi）')
    expect(joinedOriginLabels).toContain('上游实时发现')
    expect(joinedOriginLabels).not.toContain('pi-ai')

    expect(vm.billing.reference?.sourceLabel).toBe('社区目录（pi）')
    expect(vm.billing.reference?.disclaimer).toBe('非网关结算价')
    expect(vm.billing.isEstimatable).toBe(false)
  })

  it('projects advertised public requires_scope offers as live (可调用/Available) without any 需项目授权 badge, while preserving scoped badge', () => {
    const summary = baseSummary({
      availability: {
        state: 'requires_scope',
        publication: 'published',
        access: 'requires_scope'
      }
    })

    const publicZh = toCatalogModelVM(summary, null, { isZh: true, perspective: 'public' })
    expect(publicZh.availability.state).toBe('live')
    expect(publicZh.availability.isCallable).toBe(true)
    expect(publicZh.availability.badge?.label).toBe('可调用')

    const publicEn = toCatalogModelVM(summary, null, { isZh: false, perspective: 'public' })
    expect(publicEn.availability.badge?.label).toBe('Available')

    const scopedVm = toCatalogModelVM(summary, null, { isZh: true, perspective: 'scoped' })
    expect(scopedVm.availability.state).toBe('requires_scope')
    expect(scopedVm.availability.badge?.label).toBe('需项目授权')
    expect(scopedVm.availability.isCallable).toBe(false)
  })

  it('distinguishes catalog_only sub-reasons, listed_unrouted, unavailable, varies, and error states cleanly', () => {
    const platformOnly = toCatalogModelVM(
      baseSummary({
        source: { kinds: ['platform_preset'], candidate: true, discovered: false },
        availability: {
          state: 'requires_scope',
          publication: 'unpublished',
          access: 'requires_scope',
          reason: 'candidate_only'
        }
      }),
      null,
      { isZh: true }
    )
    expect(platformOnly.availability.state).toBe('catalog_only')
    expect(platformOnly.availability.badge?.label).toBe('仅目录收录')
    expect(platformOnly.availability.summarySentence).toContain('本平台尚未接入该模型路由')

    const discoveredOnly = toCatalogModelVM(
      baseSummary({
        availability: {
          state: 'unavailable',
          publication: 'unpublished',
          access: 'denied',
          reason: 'discovered_without_eligible_binding'
        }
      }),
      null,
      { isZh: true }
    )
    expect(discoveredOnly.availability.state).toBe('catalog_only')
    expect(discoveredOnly.availability.summarySentence).toContain('尚未加入有效模型')

    const listedUnrouted = toCatalogModelVM(
      baseSummary({
        availability: {
          state: 'unavailable',
          publication: 'published',
          access: 'denied',
          reason: 'published_without_eligible_binding'
        }
      }),
      null,
      { isZh: true }
    )
    expect(listedUnrouted.availability.state).toBe('listed_unrouted')
    expect(listedUnrouted.availability.badge?.label).toBe('已上架·暂不可用')

    const variesVm = toCatalogModelVM(
      baseSummary({
        billing: {
          state: 'conflict',
          reason: 'offers_disagree'
        }
      }),
      null,
      { isZh: true }
    )
    expect(variesVm.billing.isGroupVarying).toBe(true)
    expect(variesVm.billing.modeBadge?.label).toBe('按分组定价')
    expect(variesVm.billing.inputDisplay).toBe('按分组定价')
    expect(variesVm.billing.isEstimatable).toBe(false)

    const errorVm = toCatalogModelVM(
      baseSummary({
        billing: { state: 'error', reason: 'price_read_failed' }
      }),
      null,
      { isZh: true }
    )
    expect(errorVm.billing.isError).toBe(true)
    expect(errorVm.billing.isFree).toBe(false)
    expect(errorVm.billing.modeBadge?.label).toBe('价格加载失败')

    const be4Partial = toCatalogModelVM(
      baseSummary({
        billing: { state: 'partial', reason: 'partial_price' }
      }),
      {
        model: baseSummary(),
        aliases: [],
        model_facts: [],
        common_facts: {
          context_kind: 'input',
          embedding_dimensions: null,
          vision: null,
          reasoning: null,
          prompt_caching: null,
          pdf: null,
          context_tokens: '128000',
          max_output_tokens: '16384',
          modalities: ['text'],
          tools: true
        },
        capabilities: [],
        endpoints: [],
        price_version: null,
        price_source: '',
        additional_rates: [],
        time_bands: [],
        metrics: {
          window_start: '',
          window_end: '',
          source: 'unavailable',
          requests: null,
          ttft_samples: null,
          speed_samples: null,
          success_rate: null,
          ttft_p50_ms: null,
          output_tokens_per_second: null,
          state: 'insufficient_samples'
        },
        benchmarks: [],
        documentation_url: null,
        redactions: []
      },
      { isZh: true }
    )
    expect(be4Partial.billing.reasonLabel).toBe('部分分组或动作缺少定价')
    expect(be4Partial.contextDisplay).toBe('128K')
  })
})
