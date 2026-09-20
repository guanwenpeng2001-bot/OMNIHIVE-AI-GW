import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
const testLocale = vi.hoisted(() => ({ value: 'zh' }))
vi.mock('vue-i18n', async () => ({ ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')), useI18n: () => ({ t: (key: string) => key.startsWith('capabilityCatalog.') || key.startsWith('admin.accounts.modelSync.evidenceSources.') ? key.split('.').reduce((value: any, part) => value[part], testLocale.value === 'zh' ? zh : en) : key, te: () => true }) }))
import { formatDateTime } from '@/utils/format'
import ModelCatalogDetails from '../ModelCatalogDetails.vue'
import notSynced from '@/api/__tests__/fixtures/model-catalog-not_synced.json'
import empty from '@/api/__tests__/fixtures/model-catalog-empty.json'
import nonempty from '@/api/__tests__/fixtures/model-catalog-nonempty.json'
import identityInvalid from '@/api/__tests__/fixtures/model-catalog-identity_invalid.json'
import failedHistory from '@/api/__tests__/fixtures/model-catalog-failed_history.json'
import type { AccountModelCatalogDetails } from '@/types'

beforeEach(() => { testLocale.value = 'zh' })

describe('model catalog evidence display', () => {
  it.each(['complete', 'estimated', 'incomplete'])('reads %s capability status and eligibility from the five-field DTO', status => {
    const wrapper = mount(ModelCatalogDetails, { props: { candidateCount: 2, result: {
      discovered_models: ['native'], candidate_models: ['native', 'candidate'], eligible_models: [{ id: 'native' }], discovery_complete: true, capability_status: status,
      complete: false, models: [],
    } } })
    expect(wrapper.text()).not.toContain('admin.accounts.modelSync.incomplete')
    expect(wrapper.get('[data-testid="capability-sets"]').text()).toContain('E 1')
    expect(wrapper.get('[data-testid="capability-state"]').text()).toBe(status === 'complete' ? zh.capabilityCatalog.complete : status === 'estimated' ? zh.capabilityCatalog.estimated : zh.capabilityCatalog.partial)
    wrapper.unmount()
  })

  it.each([
    ['zh', ['上游响应', '补充元数据', '公共模型目录', '启发式估算']],
    ['en', ['Upstream response', 'Supplemental metadata', 'Public model catalog', 'Heuristic estimate']],
  ] as const)('labels evidence sources in %s while preserving source details', async (locale, labels) => {
    testLocale.value = locale
    const sources = ['upstream', 'litellm:catalog', 'supplement:reviewed', 'account:override', 'cursor-builtin:models', 'models.dev', 'heuristic:context', 'custom-source']
    const expectedLabels = [labels[0], labels[1], labels[1], labels[1], labels[1], labels[2], labels[3], null]
    const wrapper = mount(ModelCatalogDetails, { props: { candidateCount: null, result: {
      models: ['model'], metadata: { model: { field_evidence: Object.fromEntries(sources.map((source, index) => [`field_${index}`, {
        source, confidence: 'high', source_version: 'catalog-revision', content_hash: 'catalog-hash',
        provider_profile: 'provider-profile', matched_model_id: 'matched-model', match_rule: 'exact',
        observed_at: '2026-09-16T01:00:00Z',
      }])) } },
    } } })
    const row = wrapper.get<HTMLDetailsElement>('[data-testid="capability-row"]')
    row.element.open = true
    await row.trigger('toggle')
    const fields = wrapper.get('[data-testid="capability-fields"]')
    const displayedSources = fields.findAll('dl > div').map(field => field.get('dd').text())
    const sourceHeading = locale === 'zh' ? zh.capabilityCatalog.source : en.capabilityCatalog.source
    sources.forEach((source, index) => {
      expect(displayedSources[index]).toBe(`${sourceHeading}: ${expectedLabels[index] ? `${expectedLabels[index]} · ` : ''}${source} · high`)
    })
    expect(fields.text()).not.toMatch(/T[1-4] ·/)
    expect(fields.text()).toContain('catalog-revision')
    expect(fields.text()).toContain('provider-profile · matched-model · exact')
    expect(fields.text()).toContain(formatDateTime('2026-09-16T01:00:00Z'))
    wrapper.unmount()
  })
  it('preserves endpoint-specific authorization evidence separately from specification metadata', () => {
    const wrapper = mount(ModelCatalogDetails, { props: { candidateCount: null, result: {
      models: ['native', 'declared', 'probed'],
      eligible_models: [
        { id: 'native', endpoints: ['chat_completions'], evidence_kind: 'account_discovery' },
        { id: 'declared', endpoints: ['responses'], evidence_kind: 'administrator_declaration' },
        { id: 'probed', endpoints: ['images'], evidence_kind: 'probe_success' }
      ],
      metadata: { declared: { evidence_kind: 'specification_only' } }
    } } })
    const evidence = wrapper.findAll('[data-model-category] li').map(row => row.text())
    expect(evidence.find(row => row.includes('native'))).toContain('chat_completions')
    expect(evidence.find(row => row.includes('declared'))).toContain('administrator_declaration')
    expect(evidence.find(row => row.includes('declared'))).toContain('responses')
    expect(evidence.find(row => row.includes('probed'))).toContain('probe_success')
    expect(evidence.find(row => row.includes('probed'))).toContain('images')
    wrapper.unmount()
  })
  it('renders category blocks even when some models have no metadata', () => {
    const wrapper = mount(ModelCatalogDetails, { props: { candidateCount: 3, result: {
      models: ['chat-model', 'image-model', 'unknown-model'],
      metadata: { 'chat-model': { task_type: 'text_generation' }, 'image-model': { output_modalities: ['image'] } }
    } } })
    expect(wrapper.findAll('[data-model-category]').map(block => block.attributes('data-model-category'))).toEqual(['chat', 'image', 'other'])
    expect(wrapper.get('[data-model-category="image"]').text()).toContain('image-model')
    expect(wrapper.get('[data-model-category="other"]').text()).toContain('unknown-model')
  })
  it('warns about stale evidence while preserving effective counts', () => {
    const wrapper = mount(ModelCatalogDetails, { props: { candidateCount: 40, summary: { ...nonempty, staleness: 'stale' } } })
    expect(wrapper.get('[role="alert"]').text()).toContain('modelCatalog.stale')
    expect(wrapper.findAll('dd')[2].text()).toBe('1')
  })
  it('keeps platform, native, effective and public quantities distinct without inventing public bindings', () => {
    const wrapper = mount(ModelCatalogDetails, { props: { candidateCount: 40, result: {
      models: ['native-target'], discovered_models: ['native-target', 'gpt-pending'], applied: false,
      diff: { added: ['native-target'], removed: ['old-target'], retained: [] },
      excluded_models: [{ model: 'gpt-pending', reason: 'entitlement_unconfirmed' }],
      invalid_aliases: ['old-public'],
      public_model_diff: { added: ['public-alias'], removed: [], retained: ['other-alias'], bindings: [{ public_model: 'public-alias', target_model: 'native-target' }] }
    } } })
    expect(wrapper.findAll('dd').map(element => element.text())).toEqual(['40', '2', '1', '—'])
    expect(wrapper.text()).toContain('modelSync.publicDiffUnavailable')
    expect(wrapper.text()).not.toContain('public-alias → native-target')
    expect(wrapper.text()).toContain('admin.accounts.modelSync.reasons.entitlement_unconfirmed')
    expect(wrapper.text()).toContain('old-public')
    expect(wrapper.text()).toContain('modelSync.previewOnly')
  })
  it('distinguishes unsynchronized counts from a successfully empty snapshot', async () => {
    const summary = notSynced
    const wrapper = mount(ModelCatalogDetails, { props: { candidateCount: 40, summary } })
    expect(wrapper.findAll('dd').map(element => element.text())).toEqual(['40', '—', '—', '—'])
    expect(wrapper.text()).toContain('modelSync.notSynced')
    await wrapper.setProps({ summary: empty })
    expect(wrapper.text()).toContain('modelSync.syncedEmpty')
    expect(wrapper.findAll('dd').map(element => element.text())).toEqual(['40', '0', '0', '—'])
  })
  it.each([
    [nonempty, 'synced', ['40', '2', '1', '—']],
    [identityInvalid, 'reasons.identity_changed', ['40', '2', '0', '—']],
    [failedHistory, 'failedPrevious', ['40', '2', '1', '—']]
  ] as const)('renders handler history, effective counts and evidence %#', (fixture, status, counts) => {
    const wrapper = mount(ModelCatalogDetails, { props: { candidateCount: 40, summary: fixture as AccountModelCatalogDetails } })
    expect(wrapper.findAll('dd').map(element => element.text())).toEqual(counts)
    expect(wrapper.text()).toContain(`modelSync.${status}`)
    expect(wrapper.text()).toContain('account_discovery')
    expect(wrapper.text()).toContain(formatDateTime('2026-09-16T01:00:00Z'))
    expect(wrapper.text()).toContain('modelSync.publicDiffUnavailable')
    if (fixture.last_error) expect(wrapper.text()).toContain(fixture.last_error)
    if (fixture === identityInvalid) expect(wrapper.text()).not.toContain('modelSync.syncedEmpty')
  })
  it('shows expired and invalid identity evidence from reason codes without inferring from names', () => {
    const wrapper = mount(ModelCatalogDetails, { props: { candidateCount: 40, result: { models: [], warnings: [{ code: 'identity_changed', message: '' }, { code: 'stale_evidence', message: '' }] } } })
    expect(wrapper.text()).toContain('modelSync.reasons.identity_changed')
    expect(wrapper.text()).toContain('modelSync.reasons.stale_evidence')
  })
})


it('retains preview evidence but hides invalidated differences and effective counts', () => {
  const wrapper = mount(ModelCatalogDetails, { props: { candidateCount: 2, stale: true, result: {
    models: ['candidate'], complete: true, applied: false,
    diff: { added: ['old-added'], removed: ['old-removed'], retained: [] },
    public_model_diff: { added: ['old-public'], removed: [], retained: [] }
  } } })
  expect(wrapper.text()).toContain('candidate')
  expect(wrapper.text()).not.toContain('old-added')
  expect(wrapper.text()).not.toContain('old-removed')
  expect(wrapper.text()).not.toContain('old-public')
  expect(wrapper.findAll('dd')[2].text()).toBe('—')
})


it('keeps creation diagnostics collapsed and risks visible without saved-account noise', () => {
  const wrapper = mount(ModelCatalogDetails, { props: { creationContext: true, candidateCount: 8, result: {
    models: ['preview-model'], complete: true, applied: false,
    diff: { added: [], removed: ['old-model'], retained: [] }
  } } })
  // 能力档案 details（逐字段缺口）排在诊断区之前且有意展开；断言定位诊断区本身。
  const diagnostics = wrapper.findAll('details').find(d => d.text().includes('modelSync.platformCandidates'))
  expect(diagnostics, 'diagnostics details must exist').toBeTruthy()
  expect(diagnostics!.attributes('open')).toBeUndefined()
  expect(wrapper.get('[role="alert"]').element.closest('details')).toBeNull()
  expect(wrapper.text()).toContain('modelSync.previewCount')
  expect(wrapper.text()).not.toContain('modelSync.savedConfigHint')
  expect(wrapper.text()).not.toContain('modelSync.publicDiffUnavailable')
})

it('does not render an unknown diagnostic card before discovery', () => {
  const wrapper = mount(ModelCatalogDetails, { props: { creationContext: true, candidateCount: 8 } })
  expect(wrapper.find('section').exists()).toBe(false)
})

it('retains discovered-only models in the saved coverage denominator before a new preview', () => {
  const wrapper = mount(ModelCatalogDetails, { props: {
    candidateCount: 0, mappingTargets: ['mapped-only'],
    summary: nonempty as AccountModelCatalogDetails
  } })
  const rows = wrapper.findAll('[data-testid="capability-row"]').map(row => row.text())
  expect(rows).toHaveLength(3)
  expect(rows.some(row => row.includes('native-target'))).toBe(true)
  expect(rows.some(row => row.includes('pending-target'))).toBe(true)
  expect(rows.some(row => row.includes('mapped-only'))).toBe(true)
  expect(rows.every(row => row.includes('状态未提供'))).toBe(true)
})


it.each([534, 1000])('bounds a %s model capability catalog and opens field evidence on demand', async count => {
  const discovered = Array.from({ length: count }, (_, i) => `model-${String(i).padStart(4, '0')}`)
  const result = {
    models: discovered.slice(0, 12), discovered_models: discovered, complete: true,
    metadata: Object.fromEntries(discovered.map((id, i) => [id, {
      metadata_status: i === count - 1 ? 'incomplete' : 'complete',
      conflicts: i === count - 1 ? ['context_window conflict'] : [],
      supports_function_calling: false,
      field_evidence: { supports_function_calling: { source: 'upstream' } }
    }]))
  } as any
  const wrapper = mount(ModelCatalogDetails, { props: { candidateCount: 5, mappingTargets: [discovered[0]!, 'mapping-only', '*'], result } })
  expect(wrapper.get('[data-testid="capability-sets"]').text()).toContain(`D ${count} · M 2 · U ${count + 1} · E 12`)
  expect(wrapper.findAll('[data-testid="capability-row"]')).toHaveLength(50)
  expect(wrapper.findAll('[data-testid="capability-fields"]')).toHaveLength(0)
  await wrapper.get('[data-testid="capability-search"]').setValue(discovered[count - 1])
  const row = wrapper.get<HTMLDetailsElement>('[data-testid="capability-row"]')
  expect(wrapper.findAll('[data-testid="capability-row"]')).toHaveLength(1)
  row.element.open = true
  await row.trigger('toggle')
  expect(wrapper.get('[data-testid="capability-fields"]').text()).toContain('supports_function_calling: false')
  expect(wrapper.get('[data-testid="capability-fields"]').text()).toContain('上游响应 · upstream')
  expect(wrapper.get('[data-testid="capability-fields"]').text()).toContain('context_window conflict')
  await wrapper.get('[data-testid="capability-search"]').setValue('')
  await wrapper.get('[data-testid="capability-filter"]').setValue('conflict')
  expect(wrapper.findAll('[data-testid="capability-row"]')).toHaveLength(1)
  await wrapper.get('[data-testid="capability-filter"]').setValue('all')
  await wrapper.get('[data-testid="capability-more"]').trigger('click')
  expect(wrapper.findAll('[data-testid="capability-row"]')).toHaveLength(100)
})

it.each([
  [[], [], '未发现模型'],
  [['discovered'], [], '无路由资格'],
  [['discovered'], ['discovered'], '部分模型能力'],
])('distinguishes discovery, eligibility and capability coverage %#', (discovered, eligible, state) => {
  const wrapper = mount(ModelCatalogDetails, { props: { candidateCount: 0, mappingTargets: [], result: { models: eligible, discovered_models: discovered, complete: true } } })
  expect(wrapper.get('[data-testid="capability-state"]').text()).toContain(state)
})

it('shows mapped-only read projection without changing discovery or eligibility', () => {
  const summary = { ...notSynced, display_metadata: { 'private-target': { id: 'private-target', metadata_status: 'incomplete', context_window: 8192 } } } as AccountModelCatalogDetails
  const before = JSON.stringify(summary)
  const wrapper = mount(ModelCatalogDetails, { props: { summary, candidateCount: 0, mappingTargets: ['private-target'] } })
  expect(wrapper.findAll('[data-testid="capability-row"]')).toHaveLength(1)
  expect(wrapper.text()).toContain('private-target')
  expect(JSON.stringify(summary)).toBe(before)
  expect(summary.synced).toBe(false)
  expect(summary.eligible_count).toBe(0)
})
