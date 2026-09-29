import { baseCompile } from '@intlify/message-compiler'
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import UsageStatsState from '../UsageStatsState.vue'
import en from '@/i18n/locales/en/dashboard'
import zh from '@/i18n/locales/zh/dashboard'

describe('UsageStatsState', () => {
  it.each(['en', 'zh'])('separates successful zero, unknown and failure without coverage text in %s', async locale => {
    const wrapper = mount(UsageStatsState, {
      props: { state: 'known', historyGap: { reason: 'USAGE_HISTORY_INCOMPLETE', rawRetainedFrom: '2026-09-01T00:00:00Z' } },
      slots: { default: '0' },
      global: { plugins: [createI18n({ messageCompiler: message => new Function('return ' + baseCompile(message as string).code)(), legacy: false, locale, messages: { en, zh } })] },
    })
    expect(wrapper.text()).toBe('0')
    await wrapper.setProps({ state: 'unknown' })
    expect(wrapper.text()).toBe('—')
    expect(wrapper.html()).not.toMatch(/USAGE_HISTORY_INCOMPLETE|2026-09-01|rawRetained|coverage|覆盖/)
    await wrapper.setProps({ state: 'error' })
    expect(wrapper.text()).toBe(locale === 'en' ? en.usage.failedToLoad : zh.usage.failedToLoad)
    expect(wrapper.text()).not.toBe('—')
    wrapper.unmount()
  })
})
