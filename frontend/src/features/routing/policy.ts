import type { PolicyDraft, PolicyHead, PolicyRule, PolicyScope, PolicySimulation, RoutingTrace, SimulationRequest } from './api'

type Constraints = NonNullable<PolicyDraft['constraints']>
type Gate = 'inherit' | 'allow' | 'deny'

export const templates = ['stable', 'lowest_cost', 'low_latency', 'balanced'] as const
export type Template = typeof templates[number]

export interface RuleForm {
  mode: PolicyRule['mode']
  items: string
}

export interface PolicyForm {
  name: string
  models: RuleForm
  regions: RuleForm
  retention: NonNullable<Constraints['retention']>
  training: NonNullable<Constraints['training']>
  rate: string
  capabilities: string
  requireQuota: boolean
  pools: string
  deniedProviders: string
  expansion: Gate
  template: Template | ''
  providers: string
  overrideProviders: boolean
  endpoint: Gate
  fallbackModels: string
  restrictFallbackModels: boolean
  funding: Gate
}

function gateForm(value: boolean | undefined): Gate {
  return value === undefined ? 'inherit' : value ? 'allow' : 'deny'
}

function gateValue(value: Gate): boolean | undefined {
  return value === 'inherit' ? undefined : value === 'allow'
}

function ruleForm(rule?: PolicyRule): RuleForm {
  return { mode: rule?.mode ?? 'inherit', items: rule?.values?.join(', ') ?? '' }
}

export function policyForm(draft?: PolicyDraft): PolicyForm {
  const rules = draft?.constraints
  return {
    name: draft?.name ?? '',
    models: ruleForm(rules?.models),
    regions: ruleForm(rules?.regions),
    retention: rules?.retention || 'inherit',
    training: rules?.training || 'inherit',
    rate: rules?.max_input_rate?.amount ?? '',
    capabilities: rules?.required_capabilities?.join(', ') ?? '',
    requireQuota: rules?.require_known_quota ?? false,
    pools: draft?.candidates?.pool_ids?.join(', ') ?? '',
    deniedProviders: draft?.candidates?.denied_provider_ids?.join(', ') ?? '',
    expansion: gateForm(draft?.candidates?.allow_expansion),
    template: draft?.preferences?.template || '',
    providers: draft?.preferences?.ordered_provider_ids?.join(', ') ?? '',
    overrideProviders: draft?.preferences?.ordered_provider_ids !== undefined,
    endpoint: gateForm(draft?.fallback?.endpoint),
    fallbackModels: draft?.fallback?.model_ids?.join(', ') ?? '',
    restrictFallbackModels: draft?.fallback?.model_ids !== undefined,
    funding: gateForm(draft?.fallback?.byok_to_platform),
  }
}

export function listValues(value: string): string[] {
  return [...new Set(value.split(/[,\n]/).map(item => item.trim()).filter(Boolean))]
}

function serializeRule(rule: RuleForm): PolicyRule {
  return rule.mode === 'allow_list' ? { mode: rule.mode, values: listValues(rule.items) } : { mode: rule.mode }
}

export function serializePolicy(form: PolicyForm, scope: PolicyScope): PolicyDraft {
  return {
    name: form.name.trim(),
    scope: { ...scope },
    constraints: {
      models: serializeRule(form.models),
      regions: serializeRule(form.regions),
      retention: form.retention,
      training: form.training,
      max_input_rate: form.rate.trim()
        ? { currency: 'USD', amount: form.rate.trim(), unit: 'input_token', per: '1000000' }
        : undefined,
      required_capabilities: listValues(form.capabilities),
      require_known_quota: form.requireQuota,
    },
    candidates: {
      pool_ids: listValues(form.pools),
      denied_provider_ids: listValues(form.deniedProviders),
      allow_expansion: gateValue(form.expansion),
    },
    preferences: { template: form.template || undefined, ordered_provider_ids: form.overrideProviders ? listValues(form.providers) : undefined },
    fallback: {
      endpoint: gateValue(form.endpoint),
      model_ids: form.restrictFallbackModels ? listValues(form.fallbackModels) : undefined,
      byok_to_platform: gateValue(form.funding),
    },
  }
}

export const isUnsigned = (value: string) => /^(0|[1-9]\d*)$/.test(value)
export const isIdentity = (value: string) => /^[1-9]\d*$/.test(value) && BigInt(value) <= 9223372036854775807n
const isRate = (value: string) => /^(0|[1-9]\d*)(\.\d+)?$/.test(value)

export function validatePolicy(form: PolicyForm): string | null {
  if (!form.name.trim()) return 'nameRequired'
  if (form.models.mode === 'allow_list' && !listValues(form.models.items).length) return 'modelListRequired'
  if (form.regions.mode === 'allow_list' && !listValues(form.regions.items).length) return 'regionListRequired'
  if (form.rate.trim() && !isRate(form.rate.trim())) return 'invalidRate'
  return null
}

function intersect(parent: string[] | null, next?: PolicyRule): string[] | null {
  if (!next || next.mode === 'inherit') return parent
  if (next.mode === 'deny_all') return []
  const allowed = next.values ?? []
  return parent === null ? allowed : parent.filter(value => allowed.includes(value))
}

function compareDecimal(left: string, right: string): number {
  const [a, af = ''] = left.split('.')
  const [b, bf = ''] = right.split('.')
  const fraction = Math.max(af.length, bf.length)
  const first = BigInt(a + af.padEnd(fraction, '0'))
  const second = BigInt(b + bf.padEnd(fraction, '0'))
  return first === second ? 0 : first < second ? -1 : 1
}

export function composeRestrictions(policies: PolicyDraft[]) {
  let models: string[] | null = null
  let regions: string[] | null = null
  let rate: string | null = null
  let retention = false
  let training = false
  let fundingDenied = false
  for (const { constraints, fallback } of policies) {
    models = intersect(models, constraints?.models)
    regions = intersect(regions, constraints?.regions)
    const limit = constraints?.max_input_rate?.amount
    if (limit && isRate(limit) && (rate === null || compareDecimal(limit, rate) < 0)) rate = limit
    retention ||= constraints?.retention === 'strict_zero'
    training ||= constraints?.training === 'prohibited'
    fundingDenied ||= fallback?.byok_to_platform === false
  }
  return { models, regions, rate, retention, training, fundingDenied }
}

export function canPublish(head: PolicyHead | null, simulation: PolicySimulation | null, dirty: boolean, now: number): boolean {
  return Boolean(head?.draft_version && head.base_version === head.active_version && !dirty &&
    simulation?.trace.policy_version === head.draft_version && Date.parse(simulation.expires_at) > now)
}

export function countRankingCandidates(
  saved: PolicyForm,
  edited: PolicyForm,
  scope: PolicyScope,
  trace: RoutingTrace | null,
): number | null {
  if (!trace) return null
  const normalize = (form: PolicyForm) => {
    const policy = serializePolicy({ ...form, name: '', template: '' }, scope)
    return JSON.stringify(policy)
  }
  if (normalize(saved) !== normalize(edited)) return null
  return trace.candidates.filter(candidate => candidate.state === 'eligible').length
}

export function validScenario(scenario: SimulationRequest): boolean {
  const unsigned = (value: string, max: bigint) => isUnsigned(value) && BigInt(value) <= max
  return isIdentity(scenario.project_id) && (!scenario.api_key_id || isIdentity(scenario.api_key_id)) &&
    Boolean(scenario.model_id.trim() && scenario.protocol.trim() && scenario.action.trim()) &&
    unsigned(scenario.seed, 18446744073709551615n) &&
    [scenario.input_token_bound, scenario.output_token_bound].every(value => unsigned(value, 9223372036854775807n))
}

export function traceSummary(trace: RoutingTrace) {
  return {
    ranked: trace.candidates.filter(candidate => candidate.state === 'eligible')
      .sort((a, b) => (a.rank ?? Number.MAX_SAFE_INTEGER) - (b.rank ?? Number.MAX_SAFE_INTEGER)),
    excluded: trace.candidates.filter(candidate => candidate.state === 'excluded'),
    affinity: trace.candidates
      .find(candidate => candidate.id === trace.selected?.candidate_id)
      ?.factors.find(factor => factor.name === 'affinity_hit' || factor.name === 'affinity') ?? null,
  }
}

export function mergePolicyForm(base: PolicyForm, local: PolicyForm, server: PolicyForm): PolicyForm {
  const edited = Object.fromEntries(Object.entries(local).filter(([key, value]) => {
    return JSON.stringify(value) !== JSON.stringify(base[key as keyof PolicyForm])
  }))
  return { ...server, ...edited }
}
