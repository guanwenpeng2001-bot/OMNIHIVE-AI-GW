import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AtlasModelUsage from '../AtlasModelUsage.vue'
import type { Account } from '@/types'
import type { AtlasUsageResult, AtlasUsageWindow } from '@/api/admin/atlasUsage'

const { queryAtlasUsage } = vi.hoisted(() => ({ queryAtlasUsage: vi.fn() }))
enableAutoUnmount(afterEach)
vi.mock('@/api/admin/atlasUsage', async () => ({ ...await vi.importActual<typeof import('@/api/admin/atlasUsage')>('@/api/admin/atlasUsage'), queryAtlasUsage }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
const account = { id: 12, type: 'apikey', platform: 'atlascloud', credentials: { account_mode: 'payg' }, credentials_status: { has_api_key: true } } as Account
function response(window: AtlasUsageWindow = { start_date: '2026-10-01', end_date: '2026-10-02' }): AtlasUsageResult {
  return { provider: 'atlascloud', success: true, kind: 'consumed_usage', fetched_at: 1, persisted: false,
    usage: { ...window, scope: 'self', partial: false, has_more: false, next_page: null, pages_fetched: 1, data: [{
      object: 'usage.bucket', date: window.start_date, start_at: `${window.start_date}T00:00:00Z`, end_at: `${window.end_date}T00:00:00Z`, covered_until: `${window.end_date}T00:00:00Z`, partial: false,
      results: [{ object: 'usage.result', usage: { requests: 12, tokens: { input: 12000, output: 3000, total: 999, cache_creation: 1, cache_creation_1h: 2, cache_read: 3, input_image: 4, output_image: 5 }, images: null, video: null } }]
    }] } }
}
async function dates(wrapper: ReturnType<typeof mount>, start = '2026-10-01', end = '2026-10-02') {
  await wrapper.get('[data-test="atlas-start"]').setValue(start)
  await wrapper.get('[data-test="atlas-end"]').setValue(end)
}
async function query(wrapper: ReturnType<typeof mount>) { await wrapper.get('[data-test="atlas-query"]').trigger('click'); await flushPromises() }

describe('AtlasModelUsage', () => {
  beforeEach(() => { queryAtlasUsage.mockReset(); vi.useFakeTimers({ toFake: ['Date'] }); vi.setSystemTime(new Date('2026-10-01T23:30:00Z')) })
  afterEach(() => { vi.useRealTimers() })
  it('queries manually with UTC exclusive dates, independent token counters and unknown media', async () => {
    queryAtlasUsage.mockResolvedValue(response())
    const wrapper = mount(AtlasModelUsage, { props: { account } })
    expect(queryAtlasUsage).not.toHaveBeenCalled()
    await query(wrapper)
    expect(queryAtlasUsage).toHaveBeenCalledWith(12, { start_date: '2026-10-01', end_date: '2026-10-02' })
    expect(wrapper.text()).toContain('atlasUsage.scope')
    expect(wrapper.text()).toContain('tokens.total: 999 tokens')
    expect(wrapper.text()).toContain('tokens.cache_read: 3 tokens')
    expect(wrapper.text()).toContain('atlasUsage.images: admin.accounts.atlasUsage.unknown')
    expect(wrapper.text()).not.toContain('atlasUsage.images: 0')
    expect(wrapper.text()).not.toContain('stale')
  })
  it.each([[1, '2026-10-01'], [7, '2026-09-25'], [30, '2026-09-02']])('presets %i days in UTC', async (days, start) => {
    const wrapper = mount(AtlasModelUsage, { props: { account } })
    await wrapper.get(`[data-test="atlas-preset-${days}"]`).trigger('click')
    expect((wrapper.get('[data-test="atlas-start"]').element as HTMLInputElement).value).toBe(start)
    expect((wrapper.get('[data-test="atlas-end"]').element as HTMLInputElement).value).toBe('2026-10-02')
  })
  it.each([['2026-10-02', '2026-10-02'], ['2026-01-01', '2026-07-01'], ['', '2026-10-02']])('rejects invalid dates %s/%s', async (start, end) => {
    const wrapper = mount(AtlasModelUsage, { props: { account } })
    await dates(wrapper, start, end)
    expect(wrapper.get('[data-test="atlas-query"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[role="alert"]').text()).toContain('invalidWindow')
  })
  it('shows partial day, pagination, media units and nullable tokens', async () => {
    const data = response()
    Object.assign(data.usage!, { partial: true, has_more: true, next_page: 'opaque', pages_fetched: 11 })
    data.usage!.data[0].partial = true
    Object.assign(data.usage!.data[0].results[0].usage, { tokens: null, images: { count: 2 }, video: { seconds: 5 } })
    queryAtlasUsage.mockResolvedValue(data)
    const wrapper = mount(AtlasModelUsage, { props: { account } })
    await query(wrapper)
    expect(wrapper.text()).toContain('atlasUsage.partial')
    expect(wrapper.text()).toContain('atlasUsage.pages: 11')
    expect(wrapper.text()).toContain('atlasUsage.images: 2')
    expect(wrapper.text()).toContain('5 admin.accounts.atlasUsage.seconds')
    expect(wrapper.text()).toContain('tokens: admin.accounts.atlasUsage.unknown')
  })
  it('keeps old readings stale on errors and date changes', async () => {
    queryAtlasUsage.mockResolvedValueOnce(response()).mockResolvedValueOnce({ success: false, status_code: 403 })
    const wrapper = mount(AtlasModelUsage, { props: { account } })
    await query(wrapper)
    await dates(wrapper, '2026-10-02', '2026-10-03')
    expect(wrapper.text()).toContain('compatibleUsage.stale')
    await query(wrapper)
    expect(wrapper.get('[role="alert"]').text()).toContain('permission_denied')
    expect(wrapper.text()).toContain('tokens.total: 999 tokens')
    expect(wrapper.text()).toContain('2026-10-01 – 2026-10-02')
  })
  it('shows empty data separately from transport failure', async () => {
    const data = response(); data.usage!.data = []
    queryAtlasUsage.mockResolvedValueOnce(data).mockRejectedValueOnce(new Error('secret'))
    const wrapper = mount(AtlasModelUsage, { props: { account } })
    await query(wrapper)
    expect(wrapper.text()).toContain('compatibleUsage.noItems')
    await query(wrapper)
    expect(wrapper.get('[role="alert"]').text()).toContain('upstream_error')
    expect(wrapper.text()).not.toContain('secret')
  })
  it('does not treat missing counters as zero', async () => {
    const data = response(); Object.assign(data.usage!.data[0].results[0].usage.tokens!, { total: undefined })
    queryAtlasUsage.mockResolvedValue(data)
    const wrapper = mount(AtlasModelUsage, { props: { account } }); await query(wrapper)
    expect(wrapper.get('[role="alert"]').text()).toContain('invalid_response')
    expect(wrapper.text()).not.toContain('tokens.total: 0')
  })
  it('disables controls while pending and discards a response after account changes', async () => {
    let resolve!: (data: AtlasUsageResult) => void
    queryAtlasUsage.mockReturnValue(new Promise(done => { resolve = done }))
    const wrapper = mount(AtlasModelUsage, { props: { account } })
    await wrapper.get('[data-test="atlas-query"]').trigger('click')
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    await wrapper.setProps({ account: { ...account, id: 13 } as Account })
    resolve(response()); await flushPromises()
    expect(wrapper.text()).not.toContain('tokens.total: 999')
  })
  it.each([{ account_mode: 'coding' }, { base_url: 'https://relay.example' }])('hides ineligible accounts', credentials => {
    const wrapper = mount(AtlasModelUsage, { props: { account: { ...account, credentials } as Account } })
    expect(wrapper.find('button').exists()).toBe(false)
    expect(queryAtlasUsage).not.toHaveBeenCalled()
  })
  it('clears a prior identity reading on a saved-key account revision', async () => {
    queryAtlasUsage.mockResolvedValue(response())
    const wrapper = mount(AtlasModelUsage, { props: { account } })
    await query(wrapper)
    expect(wrapper.text()).toContain('tokens.total: 999')
    await wrapper.setProps({ account: { ...account, updated_at: '2099-01-01', extra: { total_spend: 24 } } })
    expect(wrapper.text()).toContain('tokens.total: 999')
    await wrapper.setProps({ account: { ...account, runtime_identity: 'rotated-query-identity' } })
    expect(wrapper.text()).not.toContain('tokens.total: 999')
  })
})
