import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import CursorKeyFields from '../CursorKeyFields.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

type CursorKeyMode = 'auto' | 'official' | 'relay'

function mountCursorKeyFields(mode: CursorKeyMode, overrides: Record<string, unknown> = {}) {
  return mount(CursorKeyFields, {
    props: { mode, apiKey: '', baseUrl: '', clientVersion: '', ...overrides },
  })
}

describe('Cursor key field presentation', () => {
  it.each(['auto', 'official', 'relay'] as const)('renders credentials and protocol guidance for the parent selected mode (%s)', (mode) => {
    const wrapper = mountCursorKeyFields(mode)

    const expectedHint = mode === 'relay' ? 'admin.accounts.cursor.relayProtocolHint' : 'admin.accounts.cursor.nativeProtocolHint'
    expect(wrapper.get('[data-testid="cursor-protocol-hint"]').text()).toBe(expectedHint)

    if (mode === 'auto') {
      for (const field of ['access_token', 'refresh_token', 'machine_id', 'mac_machine_id']) {
        expect(wrapper.find(`[data-testid="cursor-${field}"]`).exists()).toBe(true)
      }
      expect(wrapper.find('[data-testid="cursor-api-key"]').exists()).toBe(false)
    } else {
      expect(wrapper.get('[data-testid="cursor-api-key"]').attributes('data-account-field')).toBe('apiKeyValue')
    }

    expect(wrapper.find('[data-testid="cursor-base-url"]').exists()).toBe(mode === 'relay')
    expect(wrapper.find('[data-testid="cursor-native-settings-hint"]').exists()).toBe(mode !== 'relay')
    if (mode === 'relay') {
      expect(wrapper.get('[data-testid="cursor-base-url"]').element.closest('label')?.textContent).toContain('admin.accounts.cursor.relayHint')
    } else {
      expect(wrapper.get('[data-testid="cursor-native-settings-hint"]').text()).toBe('admin.accounts.cursor.nativeSettingsHint')
    }

    // Local field-component scope only: having no select here does not prove the wizard omits the relay protocol choice.
    expect(wrapper.find('select').exists()).toBe(false)
  })

  it('does not own account type selection and never emits a mode update', async () => {
    const onUpdateMode = vi.fn()
    const wrapper = mount(CursorKeyFields, {
      props: { mode: 'official', apiKey: '', baseUrl: '', clientVersion: '' },
      attrs: { 'onUpdate:mode': onUpdateMode },
    })

    expect(wrapper.find('[data-testid="cursor-key-mode"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid^="cursor-key-mode-"]').exists()).toBe(false)
    expect(wrapper.find('[role="radiogroup"]').exists()).toBe(false)
    expect(wrapper.findAll('button')).toHaveLength(0)

    await wrapper.get('[data-testid="cursor-api-key"]').setValue('crsr_spec')
    expect(onUpdateMode).not.toHaveBeenCalled()
    expect(wrapper.emitted('update:mode')).toBeUndefined()
  })

  it('follows the mode the parent selects later', async () => {
    const wrapper = mountCursorKeyFields('official')
    expect(wrapper.get('[data-testid="cursor-protocol-hint"]').text()).toBe('admin.accounts.cursor.nativeProtocolHint')

    await wrapper.setProps({ mode: 'relay' })
    expect(wrapper.get('[data-testid="cursor-protocol-hint"]').text()).toBe('admin.accounts.cursor.relayProtocolHint')
    expect(wrapper.get('[data-testid="cursor-base-url"]').attributes('data-account-field')).toBe('apiKeyBaseUrl')
    expect(wrapper.find('[data-testid="cursor-native-settings-hint"]').exists()).toBe(false)

    await wrapper.setProps({ mode: 'auto' })
    expect(wrapper.get('[data-testid="cursor-protocol-hint"]').text()).toBe('admin.accounts.cursor.nativeProtocolHint')
    expect(wrapper.find('[data-testid="cursor-access_token"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="cursor-api-key"]').exists()).toBe(false)
  })

  it('keeps the credential field inputs and editing semantics intact', async () => {
    const wrapper = mountCursorKeyFields('relay')
    await wrapper.get('[data-testid="cursor-api-key"]').setValue('crsr_spec')
    await wrapper.get('[data-testid="cursor-base-url"]').setValue('https://relay.example/v1')
    expect(wrapper.emitted('update:apiKey')).toEqual([['crsr_spec']])
    expect(wrapper.emitted('update:baseUrl')).toEqual([['https://relay.example/v1']])
    expect(wrapper.get('[data-testid="cursor-api-key"]').attributes('required')).toBeDefined()

    const autoCredentials = { access_token: '', refresh_token: '', machine_id: '', mac_machine_id: '' }
    const autoWrapper = mountCursorKeyFields('auto', { autoCredentials })
    await autoWrapper.get('[data-testid="cursor-access_token"]').setValue('auto-token')
    expect(autoCredentials.access_token).toBe('auto-token')
    expect(autoWrapper.get('[data-testid="cursor-machine_id"]').attributes('placeholder')).toBe('admin.accounts.cursor.machineFormat')

    const nativeWrapper = mountCursorKeyFields('official')
    await nativeWrapper.get('input[placeholder="admin.accounts.cursor.globalDefault"]').setValue('0.48.0')
    expect(nativeWrapper.emitted('update:clientVersion')).toEqual([['0.48.0']])

    const editingWrapper = mountCursorKeyFields('official', { editing: true })
    const editingApiKey = editingWrapper.get('[data-testid="cursor-api-key"]')
    expect(editingApiKey.attributes('required')).toBeUndefined()
    expect(editingApiKey.attributes('placeholder')).toBe('admin.accounts.cursor.keepKey')
  })
})
