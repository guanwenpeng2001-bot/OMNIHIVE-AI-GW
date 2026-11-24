import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ProfileView from '@/views/user/ProfileView.vue'

const {
  fetchPublicSettingsMock,
  refreshUserMock,
  authState
} = vi.hoisted(() => ({
  fetchPublicSettingsMock: vi.fn(),
  refreshUserMock: vi.fn(),
  authState: {
    user: null as Record<string, unknown> | null,
    refreshUser: vi.fn()
  }
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authState
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    fetchPublicSettings: fetchPublicSettingsMock
  })
}))

vi.mock('@/utils/format', () => ({
  formatDate: () => 'April 2026'
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

describe('ProfileView', () => {
  beforeEach(() => {
    refreshUserMock.mockReset()
    fetchPublicSettingsMock.mockReset()
    refreshUserMock.mockResolvedValue(undefined)
    authState.refreshUser = refreshUserMock
    authState.user = {
      id: 1,
      username: 'alice',
      email: 'alice@example.com',
      role: 'user',
      balance: 10,
      concurrency: 2,
      status: 'active',
      allowed_groups: null,
      balance_notify_enabled: true,
      balance_notify_threshold: null,
      balance_notify_extra_emails: [],
      created_at: '2026-04-20T00:00:00Z',
      updated_at: '2026-04-20T00:00:00Z'
    }
    fetchPublicSettingsMock.mockResolvedValue({
      contact_info: '',
      balance_low_notify_enabled: false,
      balance_low_notify_threshold: 0,
      linuxdo_oauth_enabled: true,
      wechat_oauth_enabled: true,
      wechat_oauth_open_enabled: true,
      wechat_oauth_mp_enabled: false,
      oidc_oauth_enabled: true,
      oidc_oauth_provider_name: 'OIDC'
    })
  })

  it('renders the simplified single-column profile shell without separate stat cards', async () => {
    const wrapper = mount(ProfileView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          StatCard: { template: '<div class="stat-card" />' },
          ProfileInfoCard: { template: '<div data-testid="profile-info-card" />' },
          ProfileBalanceNotifyCard: { template: '<div data-testid="profile-balance-notify-card" />' },
          ProfilePasswordForm: { template: '<div data-testid="profile-password-form" />' },
          ProfileSessions: true,
          ProfileTotpCard: { template: '<div data-testid="profile-totp-card" />' },
          Icon: true
        }
      }
    })

    await flushPromises()

    expect(wrapper.get('h1').text()).toBe('nav.profile')
    expect(wrapper.text()).not.toMatch(/\p{Script=Han}/u)
    expect(wrapper.findAll('.stat-card')).toHaveLength(0)
    expect(wrapper.get('[data-testid="profile-shell"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="profile-shell"]').html()).toContain('profile-info-card')
    expect(wrapper.get('[data-testid="profile-shell"]').html()).toContain('profile-password-form')
    expect(wrapper.get('[data-testid="profile-shell"]').html()).toContain('profile-totp-card')
  })

  it('renders account deletion and personal data export human-support instructions [E1-4]', async () => {
    const wrapper = mount(ProfileView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          ProfileInfoCard: true,
          ProfileBalanceNotifyCard: true,
          ProfilePasswordForm: true,
          ProfileSessions: true,
          ProfileTotpCard: true,
          HumanSupportCard: true,
          Icon: true,
          RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' }
        }
      }
    })

    await flushPromises()

    const lifecycle = wrapper.find('[data-testid="profile-account-lifecycle"]')
    expect(lifecycle.exists()).toBe(true)
    expect(lifecycle.html()).toContain('/docs#support')
  })

  it('shows disabled TOTP summary in header when feature_enabled is false [E1-8]', async () => {
    const wrapper = mount(ProfileView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          ProfileInfoCard: true,
          ProfileBalanceNotifyCard: true,
          ProfilePasswordForm: true,
          ProfileSessions: true,
          ProfileTotpCard: {
            emits: ['status-loaded'],
            mounted() {
              this.$emit('status-loaded', { enabled: false, feature_enabled: false })
            },
            template: '<div data-testid="profile-totp-card" />'
          },
          HumanSupportCard: true,
          Icon: true,
          RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' }
        }
      }
    })

    await flushPromises()

    const chip = wrapper.find('[data-testid="profile-totp-summary-chip"]')
    expect(chip.exists()).toBe(true)
    expect(chip.text()).not.toContain('可配置')
    expect(chip.text()).toContain('publicPortal.totpSummary.disabled')
  })
})
