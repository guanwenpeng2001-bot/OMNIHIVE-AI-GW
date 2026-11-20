import { flushPromises, mount } from '@vue/test-utils'
import { expect, it, vi } from 'vitest'
import MonitorTemplateManagerDialog from '../MonitorTemplateManagerDialog.vue'

const { listTemplates } = vi.hoisted(() => ({ listTemplates: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { channelMonitorTemplate: { list: listTemplates } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

it('shows preserved Cursor template fields without edit, apply, or delete actions', async () => {
  listTemplates.mockResolvedValue({ items: [{
    id: 1, name: 'Historical Cursor', provider: 'cursor', description: 'Original description',
    extra_headers: { 'x-original': 'preserved' }, body_override_mode: 'merge',
    body_override: { original: true }, associated_monitors: 1,
  }] })
  const wrapper = mount(MonitorTemplateManagerDialog, {
    props: { show: true },
    global: { stubs: {
      BaseDialog: { template: '<section><slot /></section>' }, ConfirmDialog: true,
      Icon: true, MonitorAdvancedRequestConfig: true, MonitorTemplateApplyPickerDialog: true,
    } },
  })
  await flushPromises()

  expect(wrapper.text()).toContain('Historical Cursor')
  expect(wrapper.text()).toContain('Original description')
  expect(wrapper.text()).toContain('channelTelemetry.cursorLegacyReadOnly')
  expect(wrapper.find('pre').text()).toContain('"x-original": "preserved"')
  expect(wrapper.find('pre').text()).toContain('"original": true')
  expect(wrapper.find('pre').element.parentElement?.querySelector('button')).toBeNull()
  wrapper.unmount()
})
