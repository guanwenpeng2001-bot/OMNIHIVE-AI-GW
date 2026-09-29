import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import { baseCompile } from '@intlify/message-compiler'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import { GROUP_PLATFORM_OPTIONS } from '@/constants/platforms'
import PlatformIcon from '../PlatformIcon.vue'
import PlatformTypeBadge from '../PlatformTypeBadge.vue'

function badge(platform: string, type = 'bedrock', showAwsSuffix?: boolean, locale = 'en') {
  return mount(PlatformTypeBadge, {
    props: { platform, type, ...(showAwsSuffix === undefined ? {} : { showAwsSuffix }) },
    global: { plugins: [createI18n({
      legacy: false, locale, messages: { en, zh },
      messageCompiler: message => new Function('return ' + baseCompile(message as string).code)()
    })] }
  })
}
const labelSelector = '[data-testid="platform-label"]'

describe('account-list AWS suffix', () => {
  it('adds the suffix to the legacy Bedrock platform when enabled', () => {
    expect(badge('bedrock', 'apikey', true).get(labelSelector).text()).toBe('Anthropic (AWS)')
  })

  it('adds the same suffix to migrated Anthropic Bedrock accounts', () => {
    const legacy = badge('bedrock', 'bedrock', true)
    const migrated = badge('anthropic', 'bedrock', true)
    expect(legacy.get(labelSelector).text()).toBe('Anthropic (AWS)')
    expect(migrated.get(labelSelector).text()).toBe(legacy.get(labelSelector).text())
    expect(migrated.findComponent(PlatformIcon).html()).toBe(legacy.findComponent(PlatformIcon).html())
  })

  it.each([undefined, false])('keeps the default badge unchanged outside the account list (%s)', enabled => {
    for (const platform of ['bedrock', 'anthropic']) {
      expect(badge(platform, 'bedrock', enabled).get(labelSelector).text()).toBe('Anthropic')
    }
  })

  it.each([
    ['anthropic', 'apikey'], ['anthropic', 'oauth'],
    ['anthropic', 'service_account'], ['gemini', 'service_account'], ['gemini', 'bedrock']
  ])('does not label ordinary Anthropic or Vertex accounts as AWS (%s/%s)', (platform, type) => {
    expect(badge(platform, type, true).get(labelSelector).text()).not.toContain('(AWS)')
  })

  it('keeps the platform segment and account type segment separate', () => {
    const wrapper = badge('anthropic', 'bedrock', true)
    expect(wrapper.get(labelSelector).text()).toBe('Anthropic (AWS)')
    expect(wrapper.element.firstElementChild?.children[1].textContent?.trim()).toBe('AWS')
  })

  it.each([...GROUP_PLATFORM_OPTIONS.map(option => option.value), 'unknown'])('preserves platform palette classes in both themes for %s', platform => {
    const original = badge(platform, 'bedrock', false)
    const enabled = badge(platform, 'bedrock', true)
    const classes = (wrapper: ReturnType<typeof badge>) => Array.from(wrapper.element.querySelectorAll('*')).map(element => element.className)
    expect(classes(enabled)).toEqual(classes(original))
  })

  it.each(['zh', 'en'])('uses the same AWS suffix in Chinese and English (%s)', locale => {
    const messages = locale === 'zh' ? zh : en
    expect(badge('qianwen', 'apikey', true, locale).get(labelSelector).text())
      .toBe(messages.admin.accounts.platforms.qianwen)
    expect(badge('anthropic', 'bedrock', true, locale).get(labelSelector).text())
      .toBe(`${messages.admin.accounts.platforms.anthropic} (AWS)`)
  })
})
