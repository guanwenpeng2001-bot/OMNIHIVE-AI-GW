import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import PlatformUsageBreakdown from '../PlatformUsageBreakdown.vue'
import en from '@/i18n/locales/en/dashboard'
import zh from '@/i18n/locales/zh/dashboard'
import enAccounts from '@/i18n/locales/en/admin/accounts'
import zhAccounts from '@/i18n/locales/zh/admin/accounts'

// 平台名统一由共享词条表 admin.accounts.platforms.* 提供（getPlatformLabel），
// 这里只挂载该表与 monitorCommon.providers，确认标签来自共享来源。
// 该测试构建里的 vue-i18n 是 runtime-only（无消息编译器），因此词条统一包成函数。
function asMessages(table: Record<string, string>): Record<string, () => string> {
  return Object.fromEntries(Object.entries(table).map(([key, label]) => [key, () => label]))
}

const messages = Object.fromEntries(Object.entries({ en, zh }).map(([locale, data]) => [locale, {
  admin: { accounts: { platforms: asMessages((locale === 'en' ? enAccounts : zhAccounts).accounts.platforms) } },
  monitorCommon: { providers: asMessages(data.monitorCommon.providers) }
}]))

describe('PlatformUsageBreakdown provider labels', () => {
  it.each([['en', 'Qianwen AI'], ['zh', '千问AI']])('uses %s labels and preserves unknown platforms', (locale, qianwen) => {
    const wrapper = mount(PlatformUsageBreakdown, {
      props: { today: 3, total: 3, byPlatform: ['qianwen', 'commandcode', 'future-provider'].map(platform => ({ platform, today_actual_cost: 1, total_actual_cost: 1 })) },
      global: { plugins: [createI18n({ legacy: false, locale, messages, missingWarn: false, fallbackWarn: false })], stubs: { Icon: true } },
    })
    expect(wrapper.text()).toContain(qianwen)
    expect(wrapper.text()).toContain('Command Code')
    expect(wrapper.text()).toContain('future-provider')
  })
})
