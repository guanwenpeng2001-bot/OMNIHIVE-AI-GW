import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AuditLogView from '../AuditLogView.vue'
import { auditRequestText, sanitizeAuditPath } from '@/utils/auditPath'

const { list, get, writeText, showError } = vi.hoisted(() => ({
  list: vi.fn(), get: vi.fn(), writeText: vi.fn(), showError: vi.fn(),
}))
vi.mock('@/api/admin', () => ({ adminAPI: { audit: { list, get } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ te: () => false, t: (key: string) => key }) }))
vi.mock('@/i18n', () => ({ i18n: { global: { t: (key: string) => key } } }))

const row = { id: 1, action: 'PUT /accounts/:id', method: 'PATCH',
  path: `/api/v1/accounts/42/${'long-segment/'.repeat(30)}?page=2&access_token=secret&filter=a%2Fb&API-Key=private#token`,
  actor_email: '', status_code: 200, created_at: '2026-01-01T00:00:00Z' }
const expected = `PATCH /api/v1/accounts/42/${'long-segment/'.repeat(30)}?page=2&filter=a%2Fb`

function mountAudit() {
  return mount(AuditLogView, { global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' },
    TablePageLayout: { template: '<div><slot name="table" /></div>' },
    DataTable: { props: ['data'], template: '<div><template v-for="row in data" :key="row.id"><slot name="cell-action" :row="row" /><slot name="cell-actions" :row="row" /></template></div>' },
    BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
    Icon: true, Pagination: true, ConfirmDialog: true, Select: true,
  } } })
}

beforeEach(() => {
  vi.clearAllMocks()
  list.mockResolvedValue({ items: [row], total: 1 })
  get.mockResolvedValue(row)
  writeText.mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
  Object.defineProperty(window, 'isSecureContext', { configurable: true, value: true })
  Object.defineProperty(document, 'execCommand', { configurable: true, value: vi.fn(() => false) })
})

describe('audit method and path copy', () => {
  it('copies the complete recorded method/path from the list and detail, excluding auth query values', async () => {
    const wrapper = mountAudit()
    await flushPromises()
    expect(wrapper.get('[data-testid="audit-request-path"]').text()).toBe(expected)
    await wrapper.get('button[aria-label]').trigger('click')
    expect(writeText).toHaveBeenCalledWith(expected)
    await wrapper.findAll('button').find(button => button.text() === 'admin.audit.columns.detail')!.trigger('click')
    await flushPromises()
    await wrapper.findAll('button[aria-label]').at(-1)!.trigger('click')
    expect(writeText).toHaveBeenLastCalledWith(expected)
    expect(wrapper.html()).not.toContain('access_token=secret')
    expect(wrapper.html()).not.toContain('API-Key=private')
    wrapper.unmount()
  })

  it('leaves full selectable text on clipboard failure without reading clipboard contents', async () => {
    writeText.mockRejectedValue(new Error('denied'))
    const wrapper = mountAudit()
    await flushPromises()
    await wrapper.get('button[aria-label]').trigger('click')
    await flushPromises()
    expect(showError).toHaveBeenCalledWith('common.copyFailed')
    const path = wrapper.get('[data-testid="audit-request-path"]')
    expect(path.text()).toBe(expected)
    expect(path.classes()).toContain('select-text')
    expect(path.classes()).not.toContain('truncate')
    wrapper.unmount()
  })

  it('preserves routing escapes and harmless duplicate fields but strips encoded and repeated auth keys', () => {
    expect(sanitizeAuditPath('/a%2Fb?tag=a&%74oken=one&tag=b&TOKEN=two&x-amz-signature=three&bad%zz=four')).toBe('/a%2Fb?tag=a&tag=b')
    expect(auditRequestText({ method: 'DELETE', path: '/real/42' })).toBe('DELETE /real/42')
  })
})
