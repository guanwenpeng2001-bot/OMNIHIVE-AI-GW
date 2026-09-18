import { beforeEach, describe, expect, it, vi } from 'vitest'
import { modelAvailabilityAPI } from '../admin/modelAvailability'

const client = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), delete: vi.fn() }))
vi.mock('../client', () => ({ apiClient: client }))
const state = { identity_revision: '9007199254740993', rules_revision: '2', snapshot_revision: '3', declarations: [] }
const request = { identity_revision: state.identity_revision, rules_revision: '2', snapshot_revision: '3', model: 'target', endpoint: 'responses', reason: 'verified', confirmed: true }
beforeEach(() => { for (const method of Object.values(client)) method.mockReset().mockResolvedValue({ data: state }) })
describe('availability declaration API transport', () => {
  it('reads the saved account without invoking discovery or a mutation', async () => {
    expect(await modelAvailabilityAPI.get(12)).toEqual(state)
    expect(client.get).toHaveBeenCalledWith('/admin/accounts/12/model-availability')
    expect(client.put).not.toHaveBeenCalled()
    expect(client.delete).not.toHaveBeenCalled()
  })
  it('sends only the explicit request with opaque revisions', async () => {
    expect(await modelAvailabilityAPI.declare(12, request)).toEqual(state)
    expect(client.put).toHaveBeenCalledWith('/admin/accounts/12/model-availability', request)
  })
  it('sends revocation CAS and basis in the DELETE JSON body', async () => {
    expect(await modelAvailabilityAPI.revoke(12, request)).toEqual(state)
    expect(client.delete).toHaveBeenCalledWith('/admin/accounts/12/model-availability', { data: request })
  })
})
