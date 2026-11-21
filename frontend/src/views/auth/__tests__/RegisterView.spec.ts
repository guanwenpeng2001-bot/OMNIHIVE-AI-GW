import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import RegisterView from '@/views/auth/RegisterView.vue'
import { authStorage } from '@/api/tokenRefresh'

const { getPublicSettingsMock, registerMock, showErrorMock, appStoreMock, pushMock, validateInvitationCodeMock, routeMock, verifyActionMock } = vi.hoisted(() => ({
  getPublicSettingsMock: vi.fn(),
  registerMock: vi.fn(),
  showErrorMock: vi.fn(),
  pushMock: vi.fn(),
  verifyActionMock: vi.fn(),
  validateInvitationCodeMock: vi.fn(),
  routeMock: { query: {} as Record<string, string> },
  appStoreMock: {
    cachedPublicSettings: null as { promo_code_enabled?: boolean } | null,
    showError: (...args: unknown[]) => showErrorMock(...args),
    showSuccess: vi.fn(),
    showWarning: vi.fn()
  }
}))

const publicSettings = {
  registration_enabled: true,
  email_verify_enabled: false,
  promo_code_enabled: false,
  invitation_code_enabled: false,
  affiliate_enabled: true,
  turnstile_enabled: true,
  turnstile_site_key: 'site-key',
  site_name: 'OmniHive',
  registration_email_suffix_whitelist: [],
  linuxdo_oauth_enabled: false,
  wechat_oauth_enabled: false,
  oidc_oauth_enabled: false,
  github_oauth_enabled: false,
  google_oauth_enabled: false
}

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: pushMock }),
  useRoute: () => routeMock
}))

vi.mock('vue-i18n', () => ({
  createI18n: () => ({
    global: {
      t: (key: string) => key
    }
  }),
  useI18n: () => ({
    t: (key: string) =>
      key === 'auth.emailDomainRegistrationLimit'
        ? '该邮箱域名无法注册新账户。请使用主流邮箱注册；如需使用企业邮箱，请联系客服添加域名白名单。'
        : key,
    locale: { value: 'en' }
  })
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => ({ register: (...args: unknown[]) => registerMock(...args) }),
  useAppStore: () => appStoreMock
}))

vi.mock('@/api/auth', async () => {
  const actual = await vi.importActual<typeof import('@/api/auth')>('@/api/auth')
  return {
    ...actual,
    getPublicSettings: (...args: unknown[]) => getPublicSettingsMock(...args),
    validateInvitationCode: (...args: unknown[]) => validateInvitationCodeMock(...args)
  }
})

function mountRegister() {
  return mount(RegisterView, {
    global: {
      stubs: {
        AuthLayout: { template: '<div><slot /><slot name="footer" /></div>' },
        Icon: true,
        TurnstileWidget: {
          template: '<div data-testid="turnstile-widget" />',
          methods: { verifyAction: verifyActionMock, reset: vi.fn() }
        },
        LoginAgreementPrompt: true,
        EmailOAuthButtons: true,
        LinuxDoOAuthSection: true,
        WechatOAuthSection: true,
        OidcOAuthSection: true,
        RouterLink: true,
        transition: false
      }
    }
  })
}

describe('RegisterView', () => {
  beforeEach(() => {
    getPublicSettingsMock.mockReset()
    registerMock.mockReset()
    showErrorMock.mockReset()
    pushMock.mockReset()
    validateInvitationCodeMock.mockReset()
    validateInvitationCodeMock.mockResolvedValue({ valid: true })
    routeMock.query = {}
    appStoreMock.cachedPublicSettings = null
    sessionStorage.clear()
    localStorage.clear()
    verifyActionMock.mockReset()
    verifyActionMock.mockResolvedValue({ token: 'ticket', randstr: 'randstr' })
    getPublicSettingsMock.mockResolvedValue(publicSettings)
    registerMock.mockResolvedValue({})
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it.each([true, false])('renders seven independent fields with affiliate enabled %j', async affiliateEnabled => {
    vi.useFakeTimers()
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings,
      promo_code_enabled: true,
      affiliate_enabled: affiliateEnabled,
      turnstile_enabled: false
    })
    const wrapper = mountRegister()
    await flushPromises()
    const fields = ['email', 'username', 'password', 'confirmPassword', 'affiliate_code', 'promo_code', 'aff_code']
    expect(wrapper.findAll('input').map(input => input.attributes('id'))).toEqual(fields)
    await wrapper.get('#email').setValue('user@example.com')
    await wrapper.get('#password').setValue('test-password')
    await wrapper.get('#confirmPassword').setValue('test-password')
    await wrapper.get('#affiliate_code').setValue('INVITE-123')
    expect((wrapper.get('#aff_code').element as HTMLInputElement).value).toBe('')
    await wrapper.get('#aff_code').setValue('REFERRAL-456')
    expect((wrapper.get('#affiliate_code').element as HTMLInputElement).value).toBe('INVITE-123')
    await vi.advanceTimersByTimeAsync(500)
    expect(validateInvitationCodeMock).toHaveBeenCalledWith('INVITE-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(registerMock).toHaveBeenCalledWith(expect.objectContaining({
      invitation_code: 'INVITE-123',
      aff_code: 'REFERRAL-456'
    }))
    wrapper.unmount()
  })

  it('blocks pending and invalid optional invitations, then omits a cleared invitation', async () => {
    vi.useFakeTimers()
    validateInvitationCodeMock.mockResolvedValueOnce({ valid: false, error_code: 'INVITATION_CODE_NOT_FOUND' })
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings,
      turnstile_enabled: false,
      email_verify_enabled: true
    })
    const wrapper = mountRegister()
    await flushPromises()
    await wrapper.get('#email').setValue('user@example.com')
    await wrapper.get('#password').setValue('test-password')
    await wrapper.get('#confirmPassword').setValue('test-password')
    await wrapper.get('#affiliate_code').setValue('INVALID')
    await wrapper.get('form').trigger('submit.prevent')
    expect(wrapper.get('[role="alert"]').text()).toBe('auth.invitationCodeValidating')
    expect(pushMock).not.toHaveBeenCalled()
    expect(sessionStorage.getItem('register_data')).toBeNull()
    await vi.advanceTimersByTimeAsync(500)
    expect(wrapper.get('#affiliate_code').attributes('aria-invalid')).toBe('true')
    expect(wrapper.get('#affiliate_code').attributes('aria-describedby')).toBe('invitation-error')
    expect(wrapper.get('#invitation-error').text()).toBe('auth.invitationCodeInvalid')
    await wrapper.get('form').trigger('submit.prevent')
    expect(wrapper.text()).toContain('auth.invitationCodeInvalidCannotRegister')
    expect(pushMock).not.toHaveBeenCalled()
    expect(registerMock).not.toHaveBeenCalled()
    await wrapper.get('#affiliate_code').setValue('   ')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(JSON.parse(sessionStorage.getItem('register_data')!)).toEqual({
      email: 'user@example.com',
      password: 'test-password'
    })
    expect(pushMock).toHaveBeenCalledWith('/email-verify')
    wrapper.unmount()
  })

  it('ignores an optional invitation response after the field is cleared', async () => {
    vi.useFakeTimers()
    let finishValidation!: (result: { valid: boolean }) => void
    validateInvitationCodeMock.mockReturnValueOnce(new Promise(resolve => { finishValidation = resolve }))
    const wrapper = mountRegister()
    await flushPromises()
    await wrapper.get('#affiliate_code').setValue('OLD-INVITE')
    await vi.advanceTimersByTimeAsync(500)
    await wrapper.get('#affiliate_code').setValue('')
    finishValidation({ valid: false })
    await flushPromises()
    expect(wrapper.get('#affiliate_code').attributes('aria-invalid')).toBe('false')
    expect(wrapper.find('#invitation-error').exists()).toBe(false)
    await wrapper.get('#affiliate_code').setValue('CANCELLED')
    await wrapper.get('#affiliate_code').setValue('')
    await vi.advanceTimersByTimeAsync(500)
    expect(validateInvitationCodeMock).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it.each([true, false])('loads URL invitation independently from referral with invitation required %j', async invitationRequired => {
    routeMock.query = { invitation_code: ' URL-INVITE ', aff_code: 'URL-REFERRAL' }
    getPublicSettingsMock.mockResolvedValueOnce({ ...publicSettings, invitation_code_enabled: invitationRequired })
    const wrapper = mountRegister()
    await flushPromises()
    expect((wrapper.get(invitationRequired ? '#invitation_code' : '#affiliate_code').element as HTMLInputElement).value).toBe('URL-INVITE')
    expect((wrapper.get('#aff_code').element as HTMLInputElement).value).toBe('URL-REFERRAL')
    expect(validateInvitationCodeMock).toHaveBeenCalledWith('URL-INVITE')
    expect(
      wrapper.get('[data-testid="referral-code-field"]').element.compareDocumentPosition(
        wrapper.get('[data-testid="registration-turnstile"]').element
      ) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    wrapper.unmount()
  })


  it.each([
    ['', 'auth.confirmPasswordRequired'],
    ['different-password', 'auth.passwordsDoNotMatch']
  ])('blocks invalid confirmation %j before captcha and allows correction', async (confirmation, error) => {
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings, turnstile_enabled: false,
      tencent_captcha_enabled: true, tencent_captcha_app_id: 'app-id'
    })
    const wrapper = mountRegister()
    await flushPromises()
    await wrapper.get('#email').setValue('user@example.com')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('#confirmPassword').setValue(confirmation)
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(showErrorMock).toHaveBeenCalledWith(error)
    expect(wrapper.get('#confirmPassword').attributes('aria-invalid')).toBe('true')
    expect(registerMock).not.toHaveBeenCalled()
    expect(verifyActionMock).not.toHaveBeenCalled()
    expect(pushMock).not.toHaveBeenCalled()
    expect(sessionStorage.getItem('register_data')).toBeNull()
    await wrapper.get('#confirmPassword').setValue('secret-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(registerMock).toHaveBeenCalledWith(expect.objectContaining({ password: 'secret-123' }))
    expect(registerMock.mock.calls[0][0]).not.toHaveProperty('confirmPassword')
    expect(verifyActionMock).toHaveBeenCalledOnce()
    expect(registerMock.mock.calls[0][0]).toMatchObject({
      tencent_captcha_ticket: 'ticket', tencent_captcha_randstr: 'randstr'
    })
    expect(wrapper.get('#confirmPassword').attributes('aria-invalid')).toBe('false')
  })

  it('confirms the password before continuing to email verification', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({ ...publicSettings, turnstile_enabled: false, email_verify_enabled: true })
    const wrapper = mountRegister()
    await flushPromises()
    await wrapper.get('#email').setValue('user@example.com')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('#confirmPassword').setValue('different-password')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(pushMock).not.toHaveBeenCalled()
    expect(sessionStorage.getItem('register_data')).toBeNull()
    await wrapper.get('#confirmPassword').setValue('secret-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(JSON.parse(sessionStorage.getItem('register_data')!)).toEqual({ email: 'user@example.com', password: 'secret-123' })
    expect(pushMock).toHaveBeenCalledWith('/email-verify')
    expect(registerMock).not.toHaveBeenCalled()
  })

  it.each([null, {}, { promo_code_enabled: false }])('keeps promo hidden with initial settings %j', async cached => {
    appStoreMock.cachedPublicSettings = cached
    let resolveSettings!: (settings: typeof publicSettings) => void
    getPublicSettingsMock.mockReturnValueOnce(new Promise(resolve => { resolveSettings = resolve }))
    const wrapper = mountRegister()
    expect(wrapper.find('#promo_code').exists()).toBe(false)
    resolveSettings(publicSettings)
    await flushPromises()
    expect(wrapper.find('#promo_code').exists()).toBe(false)
  })

  it('shows promo immediately when injected settings explicitly enable it', () => {
    appStoreMock.cachedPublicSettings = { promo_code_enabled: true }
    getPublicSettingsMock.mockReturnValueOnce(new Promise(() => {}))
    expect(mountRegister().find('#promo_code').exists()).toBe(true)
  })

  it('shows promo only after async settings explicitly enable it', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({ ...publicSettings, promo_code_enabled: true })
    const wrapper = mountRegister()
    expect(wrapper.find('#promo_code').exists()).toBe(false)
    await flushPromises()
    expect(wrapper.find('#promo_code').exists()).toBe(true)
  })

  it('blocks submission while settings load and after registration is closed', async () => {
    let resolveSettings!: (settings: typeof publicSettings) => void
    getPublicSettingsMock.mockReturnValueOnce(new Promise(resolve => { resolveSettings = resolve }))
    const wrapper = mountRegister()
    await wrapper.get('#email').setValue('user@example.com')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('#confirmPassword').setValue('secret-123')
    const form = wrapper.get('form')
    await form.trigger('submit.prevent')
    expect(registerMock).not.toHaveBeenCalled()
    resolveSettings({ ...publicSettings, registration_enabled: false, turnstile_enabled: false })
    await flushPromises()
    // A retained DOM listener must not bypass the closed-registration guard.
    await form.trigger('submit.prevent')
    await flushPromises()
    expect(registerMock).not.toHaveBeenCalled()
    expect(pushMock).not.toHaveBeenCalled()
    expect(sessionStorage.getItem('register_data')).toBeNull()
    expect(wrapper.find('form').exists()).toBe(false)
  })

  it('keeps email verification working with password confirmation', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({ ...publicSettings, email_verify_enabled: true, turnstile_enabled: false })
    const wrapper = mountRegister()
    await flushPromises()
    await wrapper.get('#email').setValue('user@example.com')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('#confirmPassword').setValue('secret-123')
    expect(wrapper.find('#confirmPassword').exists()).toBe(true)
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(registerMock).not.toHaveBeenCalled()
    expect(pushMock).toHaveBeenCalledWith('/email-verify')
    expect(JSON.parse(sessionStorage.getItem('register_data')!)).toMatchObject({ email: 'user@example.com', password: 'secret-123' })
  })

  it('ignores another submission while registration is in progress', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({ ...publicSettings, turnstile_enabled: false })
    let finishRegister!: () => void
    registerMock.mockReturnValueOnce(new Promise<void>(resolve => { finishRegister = resolve }))
    const wrapper = mountRegister()
    await flushPromises()
    await wrapper.get('#email').setValue('user@example.com')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('#confirmPassword').setValue('secret-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    await wrapper.get('form').trigger('submit.prevent')
    expect(registerMock).toHaveBeenCalledTimes(1)
    finishRegister()
    await flushPromises()
  })

  it('associates invalid email and password with accessible error messages', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({ ...publicSettings, turnstile_enabled: false })
    const wrapper = mountRegister()
    await flushPromises()
    await wrapper.get('form').trigger('submit.prevent')
    for (const field of ['email', 'password']) {
      expect(wrapper.get(`#${field}`).attributes('aria-invalid')).toBe('true')
      expect(wrapper.get(`#${field}`).attributes('aria-describedby')).toBe(`${field}-error`)
      expect(wrapper.get(`#${field}-error`).attributes('role')).toBe('alert')
    }
    expect(registerMock).not.toHaveBeenCalled()
  })

  it('keeps the optional affiliate invitation field before Turnstile', async () => {
    const wrapper = mountRegister()
    await flushPromises()

    const invitationField = wrapper.get('[data-testid="affiliate-invitation-field"]')
    const turnstile = wrapper.get('[data-testid="registration-turnstile"]')

    expect(invitationField.get('input').attributes('id')).toBe('affiliate_code')
    expect(invitationField.text()).toContain('common.optional')
    expect(
      invitationField.element.compareDocumentPosition(turnstile.element) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
  })

  it('uses the mandatory invitation field without duplicating the affiliate field', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings,
      invitation_code_enabled: true
    })

    const wrapper = mountRegister()
    await flushPromises()

    expect(wrapper.find('[data-testid="affiliate-invitation-field"]').exists()).toBe(false)
    expect(wrapper.get('#invitation_code').exists()).toBe(true)
  })

  it('submits a non-whitelist email domain so the backend can enforce its registration quota', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings,
      turnstile_enabled: false,
      registration_email_suffix_whitelist: ['allowed.com'],
      registration_email_domain_quota_enabled: true
    })

    const wrapper = mountRegister()
    await flushPromises()
    await wrapper.get('#email').setValue('first@custom.example')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('#confirmPassword').setValue('secret-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(registerMock).toHaveBeenCalledWith(
      expect.objectContaining({ email: 'first@custom.example' })
    )
    expect(showErrorMock).not.toHaveBeenCalled()
  })

  it('shows the localized registration domain quota message returned by the backend', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings,
      turnstile_enabled: false,
      registration_email_suffix_whitelist: ['allowed.com'],
      registration_email_domain_quota_enabled: true
    })
    registerMock.mockRejectedValueOnce({
      reason: 'EMAIL_DOMAIN_REGISTRATION_LIMIT',
      message: 'raw backend message'
    })

    const wrapper = mountRegister()
    await flushPromises()
    await wrapper.get('#email').setValue('second@custom.example')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('#confirmPassword').setValue('secret-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(showErrorMock).toHaveBeenCalledWith(
      '该邮箱域名无法注册新账户。请使用主流邮箱注册；如需使用企业邮箱，请联系客服添加域名白名单。'
    )
  })

  // 域名限量注册开关默认关闭：恢复 PR5423 之前的客户端白名单预检，非白名单域名不发起注册请求。
  it('rejects a non-whitelist email domain locally when the domain quota switch is disabled', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings,
      turnstile_enabled: false,
      registration_email_suffix_whitelist: ['allowed.com']
    })

    const wrapper = mountRegister()
    await flushPromises()
    await wrapper.get('#email').setValue('first@custom.example')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('#confirmPassword').setValue('secret-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(registerMock).not.toHaveBeenCalled()
    // 校验失败通过 validationToastMessage watcher 弹 toast
    expect(showErrorMock).toHaveBeenCalledWith('auth.emailSuffixNotAllowedWithAllowed')
    expect(wrapper.get('#email').classes()).toContain('input-error')
  })

  it('still submits whitelisted email domains when the domain quota switch is disabled', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings,
      turnstile_enabled: false,
      registration_email_suffix_whitelist: ['allowed.com']
    })

    const wrapper = mountRegister()
    await flushPromises()
    await wrapper.get('#email').setValue('user@allowed.com')
    await wrapper.get('#password').setValue('secret-123')
    await wrapper.get('#confirmPassword').setValue('secret-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(registerMock).toHaveBeenCalledWith(
      expect.objectContaining({ email: 'user@allowed.com' })
    )
    expect(showErrorMock).not.toHaveBeenCalled()
  })
})

function mountRegisterWithAgreementStub() {
  return mount(RegisterView, {
    global: {
      stubs: {
        AuthLayout: { template: '<div><slot /><slot name="footer" /></div>' },
        Icon: true,
        TurnstileWidget: {
          template: '<div data-testid="turnstile-widget" />',
          methods: { verifyAction: verifyActionMock, reset: vi.fn() }
        },
        LoginAgreementPrompt: {
          props: ['enabled', 'accepted', 'documents', 'mode', 'updatedAt', 'visible'],
          emits: ['accept', 'reject', 'open'],
          template: `
            <div data-testid="agreement-stub" :data-enabled="String(enabled)" :data-accepted="String(accepted)">
              <button type="button" data-testid="agreement-accept" @click="$emit('accept')">accept</button>
              <button type="button" data-testid="agreement-reject" @click="$emit('reject')">reject</button>
            </div>
          `
        },
        EmailOAuthButtons: true,
        LinuxDoOAuthSection: true,
        WechatOAuthSection: true,
        OidcOAuthSection: true,
        RouterLink: true,
        transition: false
      }
    }
  })
}

describe('RegisterView E1-1 login agreement', () => {
  beforeEach(() => {
    getPublicSettingsMock.mockReset()
    registerMock.mockReset()
    showErrorMock.mockReset()
    appStoreMock.showWarning.mockReset()
    pushMock.mockReset()
    sessionStorage.clear()
    localStorage.clear()
    registerMock.mockResolvedValue({})
  })

  it('requires explicit consent per registration (ignores cached localStorage consent) and submits revision', async () => {
    authStorage.loginAgreement.setJSON({ revision: 'rev-1', accepted_at: '2026-11-20T00:00:00Z' })
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings,
      turnstile_enabled: false,
      login_agreement_enabled: true,
      login_agreement_mode: 'checkbox',
      login_agreement_revision: 'rev-1',
      login_agreement_documents: [
        { id: 'terms', title: 'Terms', content_md: 'Terms body' },
        { id: 'privacy-policy', title: 'Privacy Policy', content_md: 'Privacy body' }
      ]
    })

    const wrapper = mountRegisterWithAgreementStub()
    await flushPromises()

    expect(wrapper.get('[data-testid="agreement-stub"]').attributes('data-accepted')).toBe('false')

    await wrapper.get('#email').setValue('second-account@example.com')
    await wrapper.get('#password').setValue('test-pass-123')
    await wrapper.get('#confirmPassword').setValue('test-pass-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(registerMock).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="agreement-accept"]').trigger('click')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(registerMock).toHaveBeenCalledWith(expect.objectContaining({
      email: 'second-account@example.com',
      login_agreement_accepted: true,
      login_agreement_revision: 'rev-1'
    }))
  })

  it('blocks registration and allows retry when loading public settings fails', async () => {
    getPublicSettingsMock
      .mockRejectedValueOnce(new Error('network down'))
      .mockResolvedValueOnce({
        ...publicSettings,
        turnstile_enabled: false,
        login_agreement_enabled: true,
        login_agreement_mode: 'checkbox',
        login_agreement_revision: 'rev-1',
        login_agreement_documents: [{ id: 'terms', title: 'Terms', content_md: 'T' }]
      })

    const wrapper = mountRegisterWithAgreementStub()
    await flushPromises()

    expect(wrapper.find('[data-testid="register-settings-error"]').exists()).toBe(true)
    await wrapper.get('#email').setValue('user@example.com')
    await wrapper.get('#password').setValue('test-pass-123')
    await wrapper.get('#confirmPassword').setValue('test-pass-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(registerMock).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="register-settings-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="register-settings-error"]').exists()).toBe(false)
  })

  it('refreshes agreement revision and forces re-acceptance when backend returns LOGIN_AGREEMENT_REQUIRED', async () => {
    getPublicSettingsMock
      .mockResolvedValueOnce({
        ...publicSettings,
        turnstile_enabled: false,
        login_agreement_enabled: true,
        login_agreement_mode: 'checkbox',
        login_agreement_revision: 'rev-v1',
        login_agreement_documents: [{ id: 'terms', title: 'Terms v1', content_md: 'v1' }]
      })
      .mockResolvedValueOnce({
        ...publicSettings,
        turnstile_enabled: false,
        login_agreement_enabled: true,
        login_agreement_mode: 'checkbox',
        login_agreement_revision: 'rev-v2',
        login_agreement_documents: [{ id: 'terms', title: 'Terms v2', content_md: 'v2' }]
      })
    registerMock.mockRejectedValueOnce({
      response: { status: 400, data: { reason: 'LOGIN_AGREEMENT_REQUIRED', message: 'Agreement updated' } }
    })

    const wrapper = mountRegisterWithAgreementStub()
    await flushPromises()
    await wrapper.get('[data-testid="agreement-accept"]').trigger('click')
    await wrapper.get('#email').setValue('user@example.com')
    await wrapper.get('#password').setValue('test-pass-123')
    await wrapper.get('#confirmPassword').setValue('test-pass-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(getPublicSettingsMock).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-testid="agreement-stub"]').attributes('data-accepted')).toBe('false')

    await wrapper.get('[data-testid="agreement-accept"]').trigger('click')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(registerMock).toHaveBeenLastCalledWith(expect.objectContaining({
      login_agreement_accepted: true,
      login_agreement_revision: 'rev-v2'
    }))
  })

  it('persists agreement acceptance and revision into registerDraft when email verification is enabled', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({
      ...publicSettings,
      email_verify_enabled: true,
      turnstile_enabled: false,
      login_agreement_enabled: true,
      login_agreement_mode: 'checkbox',
      login_agreement_revision: 'rev-draft-1',
      login_agreement_documents: [
        { id: 'terms', title: 'Terms', content_md: 'T' },
        { id: 'privacy-policy', title: 'Privacy Policy', content_md: 'P' }
      ]
    })

    const wrapper = mountRegisterWithAgreementStub()
    await flushPromises()
    await wrapper.get('[data-testid="agreement-accept"]').trigger('click')
    await wrapper.get('#email').setValue('verify-flow@example.com')
    await wrapper.get('#password').setValue('test-pass-123')
    await wrapper.get('#confirmPassword').setValue('test-pass-123')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(pushMock).toHaveBeenCalledWith('/email-verify')
    expect(authStorage.registerDraft.getJSON<Record<string, unknown> | null>(null)).toMatchObject({
      email: 'verify-flow@example.com',
      login_agreement_accepted: true,
      login_agreement_revision: 'rev-draft-1'
    })
  })
})
