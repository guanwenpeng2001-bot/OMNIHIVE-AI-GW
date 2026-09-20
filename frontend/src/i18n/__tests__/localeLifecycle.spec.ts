import { beforeEach, describe, expect, it, vi } from 'vitest'

// Runtime-only vue-i18n expects precompiled messages, as in the production Vite transform.
vi.mock('../locales/en', () => ({ default: { nav: {
  recharge: () => 'Recharge', subscribe: () => 'Subscribe', buySubscription: () => 'Recharge / Subscribe',
} } }))
vi.mock('../locales/zh', () => ({ default: { nav: {
  recharge: () => '充值', subscribe: () => '订阅', buySubscription: () => '充值 / 订阅',
} } }))

const settings = vi.hoisted(() => ({
  subscription_enabled: false,
  payment_balance_disabled: false,
}))
vi.mock('@/router', () => ({
  default: { currentRoute: { value: {
    name: 'PurchaseSubscription', params: {}, meta: { titleKey: 'nav.buySubscription' },
  } } },
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ siteName: 'Example', cachedPublicSettings: settings }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isAdmin: false }) }))
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: () => ({ customMenuItems: [] }) }))

describe('locale and document lifecycle', () => {
  beforeEach(() => {
    vi.resetModules()
    localStorage.clear()
    document.documentElement.lang = 'incorrect'
    settings.subscription_enabled = false
    settings.payment_balance_disabled = false
  })

  it.each(['en', 'zh'])('initializes document language from the saved %s locale before loading messages', async locale => {
    localStorage.setItem('sub2api_locale', locale)
    const { initI18n, getLocale } = await import('@/i18n')
    const initialized = initI18n()
    expect(document.documentElement.lang).toBe(locale)
    await initialized
    expect(document.documentElement.lang).toBe(getLocale())
  })

  it.each([
    { subscription: false, balanceDisabled: false, key: 'nav.recharge' },
    { subscription: true, balanceDisabled: true, key: 'nav.subscribe' },
    { subscription: true, balanceDisabled: false, key: 'nav.buySubscription' },
  ])('retains the billing title when switching language: $key', async ({ subscription, balanceDisabled, key }) => {
    settings.subscription_enabled = subscription
    settings.payment_balance_disabled = balanceDisabled
    const { i18n, setLocale } = await import('@/i18n')
    await setLocale('zh')
    expect(document.title).toBe(`${i18n.global.t(key)} - Example`)
    await setLocale('en')
    expect(document.title).toBe(`${i18n.global.t(key)} - Example`)
    expect(document.documentElement.lang).toBe('en')
  })

  it('keeps the last requested language when message imports overlap', async () => {
    const { setLocale, getLocale } = await import('@/i18n')
    await Promise.all([setLocale('zh'), setLocale('en')])
    expect(getLocale()).toBe('en')
    expect(document.documentElement.lang).toBe('en')
    expect(localStorage.getItem('sub2api_locale')).toBe('en')
  })
})
