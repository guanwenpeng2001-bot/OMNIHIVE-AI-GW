import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getPublicModel, isSnapshotExpiredError, type PublicModelSummary } from '../api'
import { referencePriceHint, resolveAvailabilityState, salePriceLabel } from '../format'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get } }))

const rate = { currency: 'USD', amount: '3', per: '1000000', unit: 'input_token' }

describe('public facts and sale/reference isolation', () => {
  beforeEach(() => get.mockReset())

  it('keeps missing availability unknown even with sale rates and an eligible route', () => {
    expect(resolveAvailabilityState({ id: 'a', model: 'same', price_state: 'priced', input_rate: rate, route_state: 'eligible' })).toBe('unknown')
    expect(resolveAvailabilityState({ id: 'a', model: 'same', availability_state: 'requires_scope', route_state: 'eligible' })).toBe('requires_scope')
  })

  it('shows a reference as a secondary source without creating a sale price', () => {
    const model: PublicModelSummary = { id: 'a', model: 'same', price_state: 'unpriced', billing_state: 'missing', reference_price: { source: 'pi-ai', input_rate: rate } }
    expect(salePriceLabel(model, 'input')).toBe('No sale price')
    expect(referencePriceHint(model, 'input')).toBe('Ref $3.00 · pi-ai · comparison only')
  })

  it.each(['scope_required', 'multiple_offers', 'partial_rate', 'conflict', 'conditional'] as const)('never displays a bare or free sale for %s', state => {
    const model: PublicModelSummary = { id: 'a', model: 'same', billing_state: state, price_state: 'free', input_rate: rate }
    expect(salePriceLabel(model, 'input')).not.toMatch(/\$|Free/)
  })

  it('refuses an ambiguous bare name when another provider occurs on a later page', async () => {
    get.mockRejectedValueOnce({ status: 404 })
      .mockResolvedValueOnce({ data: { items: [{ id: 'a', model: 'same', provider: 'provider-a' }], next_cursor: 'page-2' } })
      .mockResolvedValueOnce({ data: { items: [{ id: 'b', model: 'same', provider: 'provider-b' }], next_cursor: null } })
    await expect(getPublicModel('same')).rejects.toMatchObject({ status: 409, reason: 'MODEL_IDENTITY_AMBIGUOUS' })
    expect(get).toHaveBeenCalledTimes(3)
    expect(isSnapshotExpiredError({ status: 409, reason: 'MODEL_IDENTITY_AMBIGUOUS' })).toBe(false)
  })

  it('resolves a provider-qualified legacy link to exactly that stable model ID', async () => {
    get.mockRejectedValueOnce({ status: 404 })
      .mockResolvedValueOnce({ data: { items: [{ id: 'a', model: 'same', provider: 'provider-a' }, { id: 'b', model: 'same', provider: 'provider-b' }], next_cursor: null } })
      .mockResolvedValueOnce({ data: { model: { id: 'b', model: 'same', provider: 'provider-b' } } })
    expect((await getPublicModel('provider-b/same')).model.id).toBe('b')
    expect(get.mock.calls[2][0]).toBe('/api/v2/public/models/b')
  })
})
