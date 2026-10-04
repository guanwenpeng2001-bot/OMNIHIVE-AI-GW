import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import CompatibleConsumedUsage from '../CompatibleConsumedUsage.vue'
import type { Account } from '@/types'

const { queryUsage } = vi.hoisted(() => ({ queryUsage: vi.fn() }))
enableAutoUnmount(afterEach)
vi.mock('@/api/admin', () => ({ adminAPI: { cnProviders: { queryUsage } } }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
const account = { id: 12, type: 'apikey', platform: 'qiniu', credentials: { account_mode: 'payg' }, credentials_status: { has_api_key: true } } as Account
function response(start = '2026-10-01T00:00:00+08:00', end = '2026-10-02T00:00:00+08:00') {
  return { success: true, kind: 'consumed_usage', usage: { start, end, granularity: 'day', timezone: 'Asia/Shanghai', models: [
    { id: 'model-id', name: 'Model name', items: [{ name: 'input', unit: 'kToken', total: 0 }, { name: 'images', unit: 'image', total: 2 }] }
  ] } }
}
async function dates(wrapper: ReturnType<typeof mount>, start: string, end: string) {
  await wrapper.get('[data-test="usage-start"]').setValue(start)
  await wrapper.get('[data-test="usage-end"]').setValue(end)
}
describe('CompatibleConsumedUsage', () => {
  beforeEach(() => { queryUsage.mockReset() })
  it('queries manually with explicit daily offsets and preserves models, units and real zero', async () => {
    queryUsage.mockResolvedValue(response())
    const wrapper = mount(CompatibleConsumedUsage, { props: { account } })
    expect(queryUsage).not.toHaveBeenCalled()
    expect(wrapper.findAll('label')).toHaveLength(2)
    await dates(wrapper, '2026-10-01', '2026-10-01')
    await wrapper.get('[data-test="usage-query"]').trigger('click')
    await flushPromises()
    expect(queryUsage).toHaveBeenCalledWith(12, { start: '2026-10-01T00:00:00+08:00', end: '2026-10-02T00:00:00+08:00', timezone: 'Asia/Shanghai', granularity: 'day' })
    expect(wrapper.text()).toContain('Model name (model-id)')
    expect(wrapper.text()).toContain('input: 0 kToken')
    expect(wrapper.text()).toContain('images: 2 image')
    expect(wrapper.text()).not.toContain('balance')
  })
  it.each([['2026-10-02', '2026-10-01'], ['2026-09-01', '2026-10-02'], ['', '2026-10-01']])('rejects invalid window %s/%s', async (start, end) => {
    const wrapper = mount(CompatibleConsumedUsage, { props: { account } })
    await dates(wrapper, start, end)
    expect(wrapper.get('[data-test="usage-query"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[role="alert"]').text()).toContain('invalidWindow')
    expect(queryUsage).not.toHaveBeenCalled()
  })
  it('allows exactly 31 days and disables all controls during fetching', async () => {
    let resolve!: (result: unknown) => void
    queryUsage.mockReturnValue(new Promise(done => { resolve = done }))
    const wrapper = mount(CompatibleConsumedUsage, { props: { account } })
    await dates(wrapper, '2026-10-01', '2026-10-31')
    expect(wrapper.get('[data-test="usage-query"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-test="usage-query"]').trigger('click')
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    expect(wrapper.attributes('aria-busy')).toBe('true')
    resolve(response())
    await flushPromises()
  })
  it('ignores a late response when the account switches to subscription', async () => {
    let resolve!: (result: unknown) => void
    queryUsage.mockReturnValue(new Promise(done => { resolve = done }))
    const wrapper = mount(CompatibleConsumedUsage, { props: { account } })
    await wrapper.get('[data-test="usage-query"]').trigger('click')
    await wrapper.setProps({ account: { ...account, credentials: { account_mode: 'subscription' } } as Account })
    resolve(response())
    await flushPromises()
    expect(wrapper.text()).toBe('')
    await wrapper.setProps({ account })
    expect(wrapper.text()).not.toContain('Model name')
  })
  it('retains the prior window with an explicit stale label on permission failure', async () => {
    queryUsage.mockResolvedValueOnce(response()).mockResolvedValueOnce({ success: false, status_code: 403 })
    const wrapper = mount(CompatibleConsumedUsage, { props: { account } })
    await dates(wrapper, '2026-10-01', '2026-10-01')
    await wrapper.get('[data-test="usage-query"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('compatibleUsage.stale')
    await dates(wrapper, '2026-10-02', '2026-10-02')
    expect(wrapper.text()).toContain('compatibleUsage.stale')
    await wrapper.get('[data-test="usage-query"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('permission_denied')
    expect(wrapper.text()).toContain('input: 0 kToken')
    expect(wrapper.text()).toContain('2026-10-01 00:00')
  })
  it('rejects null totals without synthesizing zero', async () => {
    const invalid = response()
    Object.assign(invalid.usage.models[0].items[0], { total: null })
    queryUsage.mockResolvedValue(invalid)
    const wrapper = mount(CompatibleConsumedUsage, { props: { account } })
    await wrapper.get('[data-test="usage-query"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('invalid_response')
    expect(wrapper.text()).not.toContain('input: 0')
  })
  it('never queries unsafe origins', () => {
    const wrapper = mount(CompatibleConsumedUsage, { props: { account: { ...account, credentials: { base_url: 'https://relay.example' } } as Account } })
    expect(wrapper.find('button').exists()).toBe(false)
    expect(queryUsage).not.toHaveBeenCalled()
  })
})
