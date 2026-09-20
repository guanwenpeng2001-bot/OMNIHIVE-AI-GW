import { describe, expect, it, vi } from 'vitest'
import { CHAT_PLATFORM_BASE_URLS, CONCRETE_PLATFORM_OPTIONS, isChatOnlyPlatform, platformSupportsOAuth, platformSupportsPricingSync } from '@/constants/platforms'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('platform expansion account flow', () => {
  for (const platform of Object.keys(CHAT_PLATFORM_BASE_URLS)) {
    it(`${platform} exposes explicit runtime defaults`, () => {
      expect(CONCRETE_PLATFORM_OPTIONS.some(option => option.value === platform)).toBe(true)
      expect(isChatOnlyPlatform(platform)).toBe(true)
      expect(platformSupportsOAuth(platform)).toBe(false)
      expect(platformSupportsPricingSync(platform)).toBe(false)
      expect(CHAT_PLATFORM_BASE_URLS[platform]).toMatch(/^https:\/\//)
    })
  }
  it('does not register unverified WorkBuddy', () => {
    expect(isChatOnlyPlatform('workbuddy')).toBe(false)
    expect(CONCRETE_PLATFORM_OPTIONS.some(option => String(option.value) === 'workbuddy')).toBe(false)
  })
})
