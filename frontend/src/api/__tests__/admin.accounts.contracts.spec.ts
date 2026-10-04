import { beforeEach, describe, expect, expectTypeOf, it, vi } from 'vitest'
import type { Account, AccountListItem, AccountStatus, CreateAccountRequest } from '@/types/admin'
import type { AvailableAccountModel } from '@/types/model'
import type { PaginatedResponse } from '@/types/common'

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('../client', () => ({ apiClient: { get, post } }))

import { batchCreate, create, getAvailableModels, getById, listWithEtag, setSchedulable } from '../admin/accounts'
import type { BatchCreateAccountsResponse } from '../admin/accounts'

describe('admin account DTO contracts', () => {
  beforeEach(() => vi.clearAllMocks())

  it.each(['active', 'inactive', 'error', undefined] as const)('forwards optional create status %s unchanged for single and batch requests', async (status) => {
    expectTypeOf<CreateAccountRequest['status']>().toEqualTypeOf<AccountStatus | undefined>()
    expectTypeOf<CreateAccountRequest>().not.toHaveProperty('schedulable')
    expectTypeOf<CreateAccountRequest>().not.toHaveProperty('skip_default_group_bind')
    const request: CreateAccountRequest = {
      name: 'status-contract', platform: 'openai', type: 'apikey', credentials: { api_key: 'test' },
      ...(status === undefined ? {} : { status })
    }
    post.mockResolvedValue({ data: { id: 42, status: status ?? 'active' } })
    await create(request)
    expect(post).toHaveBeenLastCalledWith('/admin/accounts', request, expect.any(Object))
    await batchCreate([request])
    expect(post).toHaveBeenLastCalledWith('/admin/accounts/batch', { accounts: [request] }, expect.any(Object))
    if (status === undefined) expect(request).not.toHaveProperty('status')
  })

  it('preserves the batch IDs and named failures without inventing account objects', async () => {
    const response: BatchCreateAccountsResponse = {
      success: 1, failed: 1,
      results: [{ name: 'created', success: true, id: 42 }, { name: 'failed', success: false, error: 'invalid credentials' }]
    }
    post.mockResolvedValue({ data: response })
    const requests: CreateAccountRequest[] = [{ name: 'created', platform: 'openai', type: 'apikey', credentials: { api_key: 'test-key' }, concurrency: 1, priority: 1 }]
    const result = await batchCreate(requests)
    expect(result).toEqual(response)
    expect(post).toHaveBeenCalledWith('/admin/accounts/batch', { accounts: requests }, expect.objectContaining({ headers: { 'Idempotency-Key': expect.any(String) } }))
    const first = result.results[0]
    if (first.success) expectTypeOf(first.id).toEqualTypeOf<number>()
    else expectTypeOf(first.error).toEqualTypeOf<string>()
  })

  it('models list and detail separately and treats a conditional 304 as no payload', async () => {
    expectTypeOf<AccountListItem>().not.toHaveProperty('groups')
    expectTypeOf<Account>().toHaveProperty('groups')
    get.mockResolvedValueOnce({ status: 304, data: '', headers: { etag: 'revision' } })
    expect(await listWithEtag(1, 20, { lite: '1', status: 'rate_limited' }, { etag: 'revision' })).toEqual({ notModified: true, etag: 'revision', data: null })
    expectTypeOf<Awaited<ReturnType<typeof getById>>>().toEqualTypeOf<Account>()
  })

  it('allows provider models without Claude-only creation dates and returns schedulable account state', async () => {
    const models: AvailableAccountModel[] = [{ id: 'model', display_name: 'model', catalog_source: 'applied' }]
    get.mockResolvedValue({ data: models })
    expect(await getAvailableModels(42)).toEqual(models)
    post.mockResolvedValue({ data: { id: 42, schedulable: false, status: 'active' } })
    expect(await setSchedulable(42, false)).toEqual({ id: 42, schedulable: false, status: 'active' })
    expect(post).toHaveBeenCalledWith('/admin/accounts/42/schedulable', { schedulable: false })
    expectTypeOf<Awaited<ReturnType<typeof setSchedulable>>>().toEqualTypeOf<Account>()
    expectTypeOf<PaginatedResponse<AccountListItem>>().toHaveProperty('pages')
  })
})
