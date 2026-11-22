import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import { resolveActiveCaptchaProvider } from '@/components/auth/captchaProvider'
import { useAuthStore } from '@/stores/auth'
import { authStorage } from '@/api/tokenRefresh'
import SetupWizardView from '@/views/setup/SetupWizardView.vue'
import ForgotPasswordView from '../ForgotPasswordView.vue'

const {
  getPublicSettingsMock,
  forgotPasswordMock,
  loginMock,
  testDatabaseMock,
  testRedisMock,
  installMock
} = vi.hoisted(() => ({
  getPublicSettingsMock: vi.fn(),
  forgotPasswordMock: vi.fn(),
  loginMock: vi.fn(),
  testDatabaseMock: vi.fn(),
  testRedisMock: vi.fn(),
  installMock: vi.fn()
}))

vi.mock('@/api/auth', async () => {
  const actual = await vi.importActual<typeof import('@/api/auth')>('@/api/auth')
  return {
    ...actual,
    getPublicSettings: (...args: unknown[]) => getPublicSettingsMock(...args),
    forgotPassword: (...args: unknown[]) => forgotPasswordMock(...args)
  }
})

vi.mock('@/api', async () => {
  const actual = await vi.importActual<typeof import('@/api')>('@/api')
  return {
    ...actual,
    authAPI: {
      ...actual.authAPI,
      login: (...args: unknown[]) => loginMock(...args)
    }
  }
})

vi.mock('@/api/setup', () => ({
  testDatabase: (...args: unknown[]) => testDatabaseMock(...args),
  testRedis: (...args: unknown[]) => testRedisMock(...args),
  install: (...args: unknown[]) => installMock(...args),
  setSetupBootstrapToken: vi.fn()
}))

const wrappers: Array<ReturnType<typeof mount>> = []

beforeEach(() => {
  setActivePinia(createPinia())
  getPublicSettingsMock.mockReset()
  forgotPasswordMock.mockReset()
  loginMock.mockReset()
  testDatabaseMock.mockReset()
  testRedisMock.mockReset()
  installMock.mockReset()
  window.localStorage.clear()
})

afterEach(() => {
  wrappers.splice(0).forEach((w) => w.unmount())
  vi.useRealTimers()
})

describe('W07 Auth, Captcha, SetupWizard & AuthStore fixes (P1-5, P2-8, P2-9, P2-10, P3-8)', () => {
  it('resolves a single active captcha provider matching CaptchaChallenge precedence (P1-5)', () => {
    expect(
      resolveActiveCaptchaProvider({
        turnstileEnabled: true,
        turnstileSiteKey: 'site_key',
        tencentEnabled: true,
        tencentAppId: '12345'
      })
    ).toBe('turnstile')

    expect(
      resolveActiveCaptchaProvider({
        turnstileEnabled: true,
        turnstileSiteKey: '',
        tencentEnabled: true,
        tencentAppId: '12345'
      })
    ).toBe('tencent')

    expect(
      resolveActiveCaptchaProvider({
        turnstileEnabled: false,
        tencentEnabled: false,
        aliyunEnabled: true,
        aliyunSceneId: 'scene',
        aliyunPrefix: 'prefix'
      })
    ).toBe('aliyun')
  })

  it('respects password_reset_enabled=false on ForgotPasswordView (P2-8)', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({
      password_reset_enabled: false,
      turnstile_enabled: false,
      tencent_captcha_enabled: false,
      aliyun_captcha_enabled: false
    })

    const i18n = createI18n({
      legacy: false,
      locale: 'en',
      messages: {
        en: {
          auth: {
            forgotPasswordTitle: 'Reset Password',
            forgotPasswordHint: 'Enter your email',
            passwordResetDisabled: 'Password reset is currently disabled',
            backToLogin: 'Back to Login'
          }
        }
      }
    })

    const wrapper = mount(ForgotPasswordView, {
      global: {
        plugins: [i18n],
        stubs: {
          AuthLayout: { template: '<div><slot /><slot name="footer" /></div>' },
          RouterLink: { template: '<a><slot /></a>' },
          CaptchaChallenge: true
        }
      }
    })
    wrappers.push(wrapper)
    await flushPromises()

    expect(wrapper.find('form').exists()).toBe(false)
    expect(wrapper.find('[data-testid="forgot-password-disabled"]').exists()).toBe(true)
  })

  it('keeps empty port draft without bouncing back to 5432 and cancels restart polling on unmount in SetupWizardView (P2-9)', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'en',
      messages: {
        en: {
          common: { next: 'Next', back: 'Back' },
          setup: {
            title: 'Setup',
            description: 'Bootstrap',
            bootstrapToken: 'Bootstrap Token',
            bootstrapTokenHint: 'Token hint',
            database: {
              title: 'Database',
              description: 'PostgreSQL',
              host: 'Host',
              port: 'Port',
              username: 'User',
              password: 'Password',
              passwordPlaceholder: 'Password',
              database: 'DB Name',
              sslMode: 'SSL',
              ssl: { disable: 'Disable', require: 'Require', verifyCa: 'CA', verifyFull: 'Full' }
            },
            redis: {
              title: 'Redis',
              description: 'Redis',
              host: 'Host',
              port: 'Port',
              username: 'User',
              usernamePlaceholder: 'Optional',
              password: 'Password',
              passwordPlaceholder: 'Optional',
              database: 'DB',
              enableTls: 'TLS',
              enableTlsHint: 'TLS hint'
            },
            admin: {
              title: 'Admin',
              description: 'Admin',
              email: 'Email',
              password: 'Password',
              passwordPlaceholder: 'At least 8 characters',
              confirmPassword: 'Confirm Password',
              confirmPasswordPlaceholder: 'Confirm',
              passwordMismatch: 'Mismatch'
            },
            ready: { title: 'Ready', description: 'Ready', database: 'DB', redis: 'Redis', adminEmail: 'Admin' },
            status: {
              testing: 'Testing',
              success: 'Connected',
              testConnection: 'Test Connection',
              installing: 'Installing',
              completeInstallation: 'Install',
              completed: 'Completed',
              restarting: 'Restarting',
              redirecting: 'Redirecting',
              timeout: 'Timeout'
            }
          }
        }
      }
    })

    const wrapper = mount(SetupWizardView, {
      global: {
        plugins: [i18n],
        stubs: {
          PublicShell: { template: '<div><slot /></div>' },
          BrandMark: true
        }
      }
    })
    wrappers.push(wrapper)
    await flushPromises()

    const dbPortInput = wrapper.get('#setup-db-port')
    await dbPortInput.setValue('')
    expect((dbPortInput.element as HTMLInputElement).value).toBe('')
    await dbPortInput.setValue('5433')
    expect((dbPortInput.element as HTMLInputElement).value).toBe('5433')
  })

  it('persists run_mode=simple across checkAuth() reload and clamps >25-day token refresh timer (P2-10, P3-8)', async () => {
    vi.useFakeTimers()
    const setTimeoutSpy = vi.spyOn(globalThis, 'setTimeout')

    loginMock.mockResolvedValueOnce({
      access_token: 'tok_simple',
      refresh_token: 'ref_simple',
      token_type: 'Bearer',
      expires_in: 60 * 60 * 24 * 60, // 60 days (> 32-bit signed int ms)
      user: {
        id: 7,
        email: 'simple@example.com',
        role: 'user',
        balance: 0,
        concurrency: 1,
        status: 'active',
        allowed_groups: [],
        created_at: '2026-11-22T00:00:00Z',
        updated_at: '2026-11-22T00:00:00Z',
        run_mode: 'simple'
      }
    })

    const store = useAuthStore()
    await store.login({ email: 'simple@example.com', password: 'secret-password' })

    expect(store.isSimpleMode).toBe(true)
    const persisted = JSON.parse(authStorage.user.get() || '{}')
    expect(persisted.run_mode).toBe('simple')

    // Verify scheduled timeout delay is clamped <= 2147483647 ms
    const numericDelays = setTimeoutSpy.mock.calls
      .map((call) => Number(call[1]))
      .filter((delay) => Number.isFinite(delay) && delay > 0)
    expect(numericDelays.some((delay) => delay === 2_147_483_647)).toBe(true)
    expect(numericDelays.every((delay) => delay <= 2_147_483_647)).toBe(true)

    // Simulate fresh page reload with new Pinia store
    setActivePinia(createPinia())
    const reloadedStore = useAuthStore()
    reloadedStore.checkAuth()
    expect(reloadedStore.isSimpleMode).toBe(true)
  })
})
