import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import PlatformUsageBreakdown from '../PlatformUsageBreakdown.vue'
import en from '@/i18n/locales/en/dashboard'
import zh from '@/i18n/locales/zh/dashboard'

const messages = Object.fromEntries(Object.entries({ en, zh }).map(([locale, data]) => [locale, { monitorCommon: { providers: Object.fromEntries(Object.entries(data.monitorCommon.providers).map(([key, label]) => [key, () => label])) } }]))

describe('PlatformUsageBreakdown provider labels', () => {
  it.each([['en', 'Qianwen'], ['zh', '千问AI']])('uses %s labels and preserves unknown platforms', (locale, qianwen) => {
    const wrapper = mount(PlatformUsageBreakdown, {
      props: { today: 3, total: 3, byPlatform: ['qianwen', 'commandcode', 'future-provider'].map(platform => ({ platform, today_actual_cost: 1, total_actual_cost: 1 })) },
      global: { plugins: [createI18n({ legacy: false, locale, messages, missingWarn: false, fallbackWarn: false })], stubs: { Icon: true } },
    })
    expect(wrapper.text()).toContain(qianwen)
    expect(wrapper.text()).toContain('Command Code')
    expect(wrapper.text()).toContain('future-provider')
  })
})
