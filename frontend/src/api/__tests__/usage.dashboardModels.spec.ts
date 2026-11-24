import { describe, expect, it, vi } from 'vitest'
import { apiClient } from '../client'
import { getDashboardModels } from '../usage'

vi.mock('../client', () => ({ apiClient: { get: vi.fn() } }))

describe('dashboard model stats', () => {
  it('skips the request when the range is omitted', async () => {
    expect((await getDashboardModels()).models).toEqual([])
    expect(apiClient.get).not.toHaveBeenCalled()
  })

  it.each([['', '2026-11-24'], ['2026-11-18', ' ']])('waits for both dates before fetching', async (start_date, end_date) => {
    expect(await getDashboardModels({ start_date, end_date })).toEqual({ models: [], start_date, end_date })
    expect(apiClient.get).not.toHaveBeenCalled()
  })
})
