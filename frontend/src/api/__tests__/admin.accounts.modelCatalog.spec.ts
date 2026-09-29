import { beforeEach, describe, expect, it, vi } from 'vitest'
const { post, get, put } = vi.hoisted(() => ({ post: vi.fn(), get: vi.fn(), put: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { post, get, put } }))
import type { AccountModelCatalog } from '@/types'
import { getModelCatalog, updateModelAllowlist, syncUpstreamModels, syncUpstreamModelsPreview } from '@/api/admin/accounts'
import notSynced from './fixtures/model-catalog-not_synced.json'
import empty from './fixtures/model-catalog-empty.json'
import nonempty from './fixtures/model-catalog-nonempty.json'
import identityInvalid from './fixtures/model-catalog-identity_invalid.json'
import failedHistory from './fixtures/model-catalog-failed_history.json'

const review = { account_id: 42, digest: 'review', identity_revision: '9007199254740994', rules_revision: '9007199254740993', snapshot_revision: '7', credential_account_id: 42, credential_identity_revision: '9007199254740994', release: { schema_version: 2, enrichment_version: 'capability-v2', source_generation: 1, source_versions: {} } } satisfies NonNullable<AccountModelCatalog['review']>

describe('account model catalog API', () => {
  beforeEach(() => { post.mockReset(); get.mockReset() })
  it('sends explicit preview and apply parameters and preserves decimal revision strings beyond JS integer precision', async () => {
    post.mockResolvedValue({ data: { models: [] } })
    await syncUpstreamModels(42, false)
    await syncUpstreamModels(42, true, nonempty.baseline.rules_revision, undefined, review)
    expect(post.mock.calls).toEqual([
      ['/admin/accounts/42/models/sync-upstream', undefined, { params: { apply: false } }],
      ['/admin/accounts/42/models/sync-upstream', { review }, { params: { apply: true, expected_rules_revision: '9007199254740993' } }]
    ])
  })
  it('rejects apply without a review and sends allowlist CAS revisions without coercion', async () => {
    await expect(syncUpstreamModels(42, true, '1')).rejects.toThrow('review')
    expect(post).not.toHaveBeenCalled()
    const request = { enabled: true, models: ['Native'], reason: 'scope confirmed', expected_rules_revision: '9007199254740993' }
    put.mockResolvedValueOnce({ data: { rules_revision: '9007199254740994' } })
    expect(await updateModelAllowlist(42, request)).toEqual({ rules_revision: '9007199254740994' })
    expect(put).toHaveBeenCalledWith('/admin/accounts/42/models/allowlist', request)
  })
  it('propagates conflict without retrying and reads catalog summaries separately', async () => {
    post.mockRejectedValueOnce({ status: 409 })
    await expect(syncUpstreamModels(42, true, '2', undefined, review)).rejects.toEqual({ status: 409 })
    expect(post).toHaveBeenCalledOnce()
    get.mockResolvedValueOnce({ data: notSynced })
    expect(await getModelCatalog(42)).toEqual(notSynced)
    expect(get).toHaveBeenCalledWith('/admin/accounts/42/models/catalog')
  })
  it.each([notSynced, empty, nonempty, identityInvalid, failedHistory])('preserves the real handler catalog DTO %#', async (fixture) => {
    get.mockResolvedValueOnce({ data: fixture })
    expect(await getModelCatalog(42)).toEqual(fixture)
  })
  it('uses the temporary preview endpoint without a saved account or apply flag', async () => {
    const connection = { platform: 'openai', type: 'apikey', api_key: 'fixture-key' }
    post.mockResolvedValueOnce({ data: { models: [], applied: false } })
    expect(await syncUpstreamModelsPreview(connection)).toEqual({ models: [], applied: false })
    expect(post).toHaveBeenCalledWith('/admin/accounts/models/sync-upstream-preview', connection)
  })
})


it('preserves all five preview fields and propagates catalog 404 without capability probing', async () => {
  const preview = { discovered_models: ['native'], candidate_models: ['native'], eligible_models: [], discovery_complete: false, capability_status: 'incomplete' }
  post.mockResolvedValueOnce({ data: preview })
  expect(await syncUpstreamModelsPreview({ platform: 'openai', type: 'apikey' })).toEqual(preview)
  get.mockReset().mockRejectedValueOnce({ status: 404 })
  await expect(getModelCatalog(42)).rejects.toEqual({ status: 404 })
  expect(get.mock.calls).toEqual([['/admin/accounts/42/models/catalog']])
})
