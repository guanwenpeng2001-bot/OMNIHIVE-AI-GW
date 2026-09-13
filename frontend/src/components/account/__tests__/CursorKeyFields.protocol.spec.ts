import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import CursorKeyFields from '../CursorKeyFields.vue'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
describe('Cursor protocol explanation', () => {
  it.each(['auto', 'official', 'relay'] as const)('explains %s without a protocol selector', (mode) => {
    const wrapper = mount(CursorKeyFields, { props: { mode, apiKey: '', baseUrl: '', clientVersion: '' }, global: { stubs: { Icon: true } } })
    expect(wrapper.get('[data-testid="cursor-protocol-hint"]').text()).toBe(`admin.accounts.cursor.${mode === 'relay' ? 'relay' : 'native'}ProtocolHint`)
    expect(wrapper.find('select').exists()).toBe(false)
  })
})
