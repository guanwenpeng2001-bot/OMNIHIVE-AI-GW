import { expect, it, vi } from 'vitest'
import { identityApi } from '../identity'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('../client', () => ({ apiClient: { get } }))

it('reads the selected workspace member page using the v2 cursor and cancellation signal', async () => {
  const page = { items: [], next_cursor: 'next' }
  get.mockResolvedValue({ data: page })
  const params = { cursor: 'opaque+/=', limit: 12 }
  const signal = new AbortController().signal
  expect(await identityApi.listMembers('42', params, signal)).toEqual(page)
  expect(get).toHaveBeenCalledWith(expect.stringMatching(/\/api\/v2\/workspaces\/42\/members$/), {
    baseURL: '', params, signal
  })
})
