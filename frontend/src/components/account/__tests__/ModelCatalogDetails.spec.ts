import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ModelCatalogDetails from '../ModelCatalogDetails.vue'
import type { AccountModelCatalogDetails, AccountModelCatalog } from '@/types'
import notSynced from '@/api/__tests__/fixtures/model-catalog-not_synced.json'
import empty from '@/api/__tests__/fixtures/model-catalog-empty.json'
import nonempty from '@/api/__tests__/fixtures/model-catalog-nonempty.json'
import identityInvalid from '@/api/__tests__/fixtures/model-catalog-identity_invalid.json'
import failedHistory from '@/api/__tests__/fixtures/model-catalog-failed_history.json'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('model catalog user-facing information', () => {
  it.each([
    [notSynced, 'unsynced'], [empty, 'empty'], [nonempty, 'ready'],
    [identityInvalid, 'invalidIdentity'], [failedHistory, 'failed']
  ] as const)('shows saved state without technical maintenance %#', (summary, status) => {
    const wrapper = mount(ModelCatalogDetails, { props: { candidateCount: 10, summary: summary as AccountModelCatalogDetails } })
    expect(wrapper.get('[role="status"]').text()).toBe(`admin.accounts.modelCatalog.${status}`)
    expect(wrapper.get('[data-testid="catalog-counts"]').text()).toContain('10')
    expect(wrapper.find('[data-testid="catalog-diagnostics"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('9007199254740993')
  })
  it('does not turn discovery changes into removal instructions or capability gates', () => {
    const wrapper = mount(ModelCatalogDetails, { props: { candidateCount: null, result: {
      discovered_models: ['New'], candidate_models: ['New'], eligible_models: [], discovery_complete: false,
      capability_status: 'incomplete', diff: { removed: ['Selected'] }, metadata: { New: {
        metadata_status: 'incomplete', missing_fields: ['context_window'],
        field_evidence: { supports_vision: { source: 'upstream', content_hash: 'secret-hash', confidence: 'high', match_rule: 'exact' } }
      } }
    } as unknown as AccountModelCatalog } })
    expect(wrapper.text()).toContain('admin.accounts.modelSync.incomplete')
    expect(wrapper.text()).not.toContain('Selected')
    expect(wrapper.text()).not.toMatch(/secret-hash|context_window|confidence|coverageSummary/)
    expect(wrapper.find('[data-testid="capability-details"]').exists()).toBe(false)
  })
  it('shows stale state and suppresses effective counts until refreshed', () => {
    const wrapper = mount(ModelCatalogDetails, { props: { candidateCount: 10, summary: nonempty as AccountModelCatalogDetails, stale: true } })
    expect(wrapper.get('[role="status"]').text()).toContain('stale')
    expect(wrapper.get('[data-testid="catalog-counts"]').text()).toContain('—')
  })
  it('bounds and searches optional capability descriptions without field evidence', async () => {
    const metadata = Object.fromEntries(Array.from({ length: 1000 }, (_, index) => [`Exact/${index}`, { embedding_dimensions: 1536, content_hash: 'hidden' }]))
    const wrapper = mount(ModelCatalogDetails, { props: { candidateCount: 1000, summary: nonempty as AccountModelCatalogDetails, displayMetadata: metadata } })
    const details = wrapper.get<HTMLDetailsElement>('[data-testid="capability-details"]')
    expect(details.element.open).toBe(false)
    expect(details.findAll('div > p')).toHaveLength(50)
    await details.get('input').setValue('Exact/999')
    expect(details.findAll('div > p')).toHaveLength(1)
    expect(details.text()).toContain('Exact/999')
    expect(details.text()).not.toContain('hidden')
  })
})
