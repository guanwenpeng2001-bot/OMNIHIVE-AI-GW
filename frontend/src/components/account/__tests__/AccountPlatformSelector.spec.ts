import { supplierPlatformOptions } from '@/constants/supplierFamilies'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'

import AccountPlatformSelector from '../AccountPlatformSelector.vue'
import { platformFamily } from '@/constants/platforms'
import enCommon from '@/i18n/locales/en/common'
import enAccounts from '@/i18n/locales/en/admin/accounts'
import zhCommon from '@/i18n/locales/zh/common'
import zhAccounts from '@/i18n/locales/zh/admin/accounts'

// 该测试构建里的 vue-i18n 是 runtime-only（无消息编译器），因此词条统一包成函数。
function asMessages(table: Record<string, string>): Record<string, () => string> {
  return Object.fromEntries(Object.entries(table).map(([key, label]) => [key, () => label]))
}

const FamilySelectStub = defineComponent({
  name: 'FamilySelect',
  props: { modelValue: [String, Number, Boolean, null], options: Array },
  template: '<div data-testid="family-select" />'
})

function mountSelector(locale: 'en' | 'zh') {
  const common = locale === 'en' ? enCommon.common : zhCommon.common
  const accounts = locale === 'en' ? enAccounts.accounts : zhAccounts.accounts
  const i18n = createI18n({
    legacy: false,
    locale,
    messages: {
      [locale]: {
        common: asMessages({ all: common.all, search: common.search, noData: common.noData }),
        admin: {
          accounts: {
            platform: accounts.platform,
            platforms: asMessages(accounts.platforms),
            platformFamilies: asMessages(accounts.platformFamilies)
          }
        }
      }
    },
    missingWarn: false,
    fallbackWarn: false
  })
  return mount(AccountPlatformSelector, {
    props: { modelValue: 'anthropic' },
    global: { plugins: [i18n], stubs: { Select: FamilySelectStub, PlatformIcon: true } }
  })
}

function familyOptions(wrapper: ReturnType<typeof mountSelector>) {
  return wrapper.getComponent(FamilySelectStub).props('options') as Array<{ value: string; label: string }>
}

describe('AccountPlatformSelector platform families', () => {
  it('renders every family label from the locale instead of a hardcoded English string', () => {
    expect(familyOptions(mountSelector('zh')).map(option => option.label)).toEqual([
      '全部',
      'Anthropic / Amazon Bedrock',
      'Google / Vertex AI',
      'OpenAI 兼容 / 其它'
    ])
    expect(familyOptions(mountSelector('en')).map(option => option.label)).toEqual([
      'All',
      'Anthropic / Amazon Bedrock',
      'Google / Vertex AI',
      'OpenAI-compatible / Other'
    ])
  })

  it('keeps every platform button reachable through exactly one family', () => {
    const wrapper = mountSelector('en')
    const rendered = wrapper.findAll('[data-testid$="-platform"]').map(button => button.attributes('data-testid')!.replace('-platform', ''))
    expect(rendered.sort()).toEqual(supplierPlatformOptions().map(option => option.value).sort())

    const grouped: Record<string, string[]> = { anthropic: [], google: [], openaiCompatible: [] }
    for (const platform of rendered) grouped[platformFamily(platform)].push(platform)
    // 协议族按真实分派划分：Bedrock 走 Anthropic Messages，Vertex/Antigravity 走 Google，
    // 其余（含 Cursor 的 relay 协议）落在 OpenAI 兼容 / 其它那一栏。
    expect(grouped.anthropic.sort()).toEqual(['anthropic', 'bedrock'])
    expect(grouped.google.sort()).toEqual(['antigravity', 'gemini', 'vertex'])
    expect(grouped.openaiCompatible).toContain('cursor')
  })
})
