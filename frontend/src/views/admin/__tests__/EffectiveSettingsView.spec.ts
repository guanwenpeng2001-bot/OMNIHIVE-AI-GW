import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { ref } from 'vue'
import EffectiveSettingsView from '../EffectiveSettingsView.vue'

const { get, reset } = vi.hoisted(() => ({ get: vi.fn(), reset: vi.fn() }))
vi.mock('@/api/admin/effectiveSettings', () => ({ getEffectiveSettings: get, resetSettingOverrides: reset }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ locale: ref('en') }) }))

function fixture() {
  return {
    setting_version: '4', reset_groups: ['github_oauth'],
    items: [{ key: 'github_oauth_client_secret', group: 'github_oauth', effective_value: 'must-never-render', default_value: 'also-hidden', source: 'database_override', override_state: 'set', default_version: 'v0.3.5', setting_version: '4', applied_version: '4', restart_required: false, can_reset: true, sensitive: true, configured: true, validation_state: 'valid', blocked_by: [] }]
  }
}
let wrapper: VueWrapper | undefined
async function render() {
  wrapper = mount(EffectiveSettingsView, { global: { stubs: { AppLayout: { template: '<main><slot /></main>' }, RouterLink: { template: '<a><slot /></a>' } } } })
  await flushPromises()
  return wrapper
}

describe('effective settings diagnosis and safe override reset', () => {
  beforeEach(() => { get.mockReset(); reset.mockReset(); get.mockResolvedValue(fixture()) })
  afterEach(() => { wrapper?.unmount() })

  it('renders configured status without rendering any sensitive value or default', async () => {
    const view = await render()
    expect(view.text()).toContain('Configured (hidden)')
    expect(view.text()).not.toContain('must-never-render')
    expect(view.text()).not.toContain('also-hidden')
    expect(view.text()).toContain('Database override')
  })

  it('resets OAuth as one provider group under the inspected version', async () => {
    reset.mockResolvedValue({ ...fixture(), setting_version: '5' })
    const view = await render()
    await view.findAll('button').find(button => button.text() === 'Reset group')!.trigger('click')
    expect(reset).not.toHaveBeenCalled()
    expect(view.get('[role="alertdialog"]').text()).toContain('may affect subsequent sign-ins')
    await view.findAll('button').find(button => button.text() === 'Confirm reset')!.trigger('click')
    await flushPromises()
    expect(reset).toHaveBeenCalledWith({ expected_version: '4', groups: ['github_oauth'], keys: [] })
  })

  it('requires reloading and reviewing after a conflict instead of automatically resubmitting', async () => {
    reset.mockRejectedValue({ status: 409, reason: 'SETTING_VERSION_CONFLICT' })
    const view = await render()
    await view.findAll('button').find(button => button.text() === 'Reset group')!.trigger('click')
    await view.findAll('button').find(button => button.text() === 'Confirm reset')!.trigger('click')
    await flushPromises()
    expect(view.get('[role="alert"]').text()).toContain('Refresh and review')
    expect(reset).toHaveBeenCalledTimes(1)
    expect(view.findAll('button').find(button => button.text() === 'Reset group')!.attributes('disabled')).toBeDefined()
  })

  it('does not offer mutation controls when diagnostics cannot be loaded', async () => {
    get.mockRejectedValue({ status: 503, message: 'Configuration unavailable' })
    const view = await render()
    expect(view.get('[role="alert"]').text()).toContain('unavailable')
    expect(view.findAll('button').some(button => button.text() === 'Reset group')).toBe(false)
    expect(reset).not.toHaveBeenCalled()
  })
})
