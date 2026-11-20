import { afterEach, describe, expect, it, vi } from 'vitest'
import { effectScope, ref } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { apiClient } from '@/api/client'
import { routingAPI, type PolicyHead, type PolicyScope, type SimulationRequest } from '../api'
import { useRoutingPolicy } from '../useRoutingPolicy'

const scope: PolicyScope = { type: 'project', id: '5' }
const head: PolicyHead = {
  id: 'policy-a', etag: '41', active_version: 'prev', draft_version: 'draft', base_version: 'prev',
  definition: { scope, name: 'Current policy' },
}

const signal = new AbortController().signal

function page(items: PolicyHead[]) {
  return { items, as_of: '2026-11-19T10:00:00Z', next_cursor: null }
}

afterEach(() => vi.restoreAllMocks())

describe('routing policy transport and CAS', () => {
  it('sends quoted revisions, fresh POST idempotency keys and timezone-free reads', async () => {
    const post = vi.spyOn(apiClient, 'post').mockResolvedValue({ data: head })
    const put = vi.spyOn(apiClient, 'put').mockResolvedValue({ data: head })
    const get = vi.spyOn(apiClient, 'get').mockResolvedValue({ data: page([head]) })
    await routingAPI.list('2', scope, signal)
    expect(get).toHaveBeenCalledWith('/api/v2/workspaces/2/routing/policies', {
      signal, params: { scope_type: 'project', scope_id: '5', project_id: undefined, timezone: undefined },
    })
    await routingAPI.save('2', head.definition, head, signal)
    expect(put).toHaveBeenCalledWith('/api/v2/workspaces/2/routing/policies/policy-a/draft', head.definition, {
      signal, headers: { 'If-Match': '"41"' },
    })
    await routingAPI.save('2', head.definition, null, signal)
    await routingAPI.rollback('2', head, 'old', 'Restore', signal)
    const first = post.mock.calls[0][2]?.headers
    const rollback = post.mock.calls[1]
    expect(first?.['Idempotency-Key']).toEqual(expect.any(String))
    expect(rollback[1]).toEqual({ from_version: 'prev', restore_version: 'old', change_note: 'Restore' })
    expect(rollback[2]?.headers?.['If-Match']).toBe('"41"')
    expect(rollback[2]?.headers?.['Idempotency-Key']).not.toBe(first?.['Idempotency-Key'])
  })

  it('submits a side-effect-free scenario with an explicit seed and frozen snapshot', async () => {
    const post = vi.spyOn(apiClient, 'post').mockResolvedValue({ data: {} })
    const request: SimulationRequest = {
      version: 'draft', compare_version: 'prev', project_id: '5', api_key_id: null, model_id: 'm1',
      protocol: 'openai_chat', action: 'text.chat', input_token_bound: '100', output_token_bound: '0',
      snapshot_id: 'snapshot-a', seed: '0',
    }
    await routingAPI.simulate('2', head.id, request, signal)
    expect(post).toHaveBeenCalledTimes(1)
    expect(post).toHaveBeenCalledWith('/api/v2/workspaces/2/routing/policies/policy-a/simulate', request, { signal })
  })

  it.each([409, 412])('reloads the conflicting revision after %s and rebases local edits without auto-saving', async status => {
    const latest = { ...head, etag: '42', definition: { ...head.definition, constraints: { retention: 'strict_zero' as const } } }
    vi.spyOn(routingAPI, 'list').mockResolvedValueOnce(page([head])).mockResolvedValueOnce(page([latest]))
    vi.spyOn(routingAPI, 'versions').mockResolvedValue({ items: [], as_of: '', next_cursor: null })
    const submit = vi.spyOn(routingAPI, 'save').mockRejectedValue({ status })
    const effects = effectScope()
    const store = effects.run(() => useRoutingPolicy(ref('2'), ref(scope)))!
    try {
      await flushPromises()
      store.form.value.name = 'Local edit'
      await store.save()
      expect(store.conflict.value?.status).toBe(status)
      expect(store.conflict.value?.current?.etag).toBe('42')
      expect(store.form.value.name).toBe('Local edit')
      expect(store.head.value?.etag).toBe('41')
      store.resolveConflict(true)
      expect(store.head.value?.etag).toBe('42')
      expect(store.form.value).toMatchObject({ name: 'Local edit', retention: 'strict_zero' })
      expect(submit).toHaveBeenCalledTimes(1)
    } finally {
      effects.stop()
    }
  })

  it('marks lost write acknowledgements unknown and blocks automatic resubmission', async () => {
    vi.spyOn(routingAPI, 'list').mockResolvedValue(page([head]))
    vi.spyOn(routingAPI, 'versions').mockResolvedValue({ items: [], as_of: '', next_cursor: null })
    const submit = vi.spyOn(routingAPI, 'save').mockRejectedValue(new Error('disconnected'))
    const effects = effectScope()
    const store = effects.run(() => useRoutingPolicy(ref('2'), ref(scope)))!
    try {
      await flushPromises()
      store.form.value.name = 'Local edit'
      await store.save()
      expect(store.unconfirmed.value).toBe('save')
      await store.save()
      expect(submit).toHaveBeenCalledTimes(1)
      await store.verifyOutcome()
      expect(store.conflict.value?.current?.etag).toBe('41')
    } finally {
      effects.stop()
    }
  })
})
