import { beforeEach, describe, expect, it, vi } from 'vitest'

const { mockPut } = vi.hoisted(() => ({
  mockPut: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get: vi.fn(),
    post: vi.fn(),
    put: mockPut,
    patch: vi.fn(),
    delete: vi.fn()
  }
}))

import { connectionsApi } from '../api'

describe('connections/api', () => {
  beforeEach(() => {
    mockPut.mockReset()
  })

  it('uses workspaceBase without duplicate /v1 prefix in attachAuthorization', async () => {
    mockPut.mockResolvedValueOnce({
      data: { credential: { version: 2, state: 'configured', rotated_at: null } }
    })

    await connectionsApi.attachAuthorization(
      'conn_1',
      '1',
      { type: 'oauth', code: 'auth-code' } as never,
      '42',
      'idem-123'
    )

    expect(mockPut).toHaveBeenCalledWith(
      '/v2/workspaces/42/connections/conn_1/authorization',
      { type: 'oauth', code: 'auth-code' },
      {
        headers: {
          'If-Match': '"1"',
          'Idempotency-Key': 'idem-123'
        }
      }
    )
  })
})
