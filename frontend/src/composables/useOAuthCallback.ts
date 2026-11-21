import { extractApiErrorCode, extractApiErrorMessage } from '@/utils/apiError'

import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  type PendingOAuthCreateAccountPayload
} from '@/components/auth/PendingOAuthCreateAccountForm.vue'
import { apiClient } from '@/api/client'
import { useAuthStore, useAppStore } from '@/stores'
import {
  exchangePendingOAuthCompletion,
  getOAuthCompletionKind,
  getPublicSettings,
  isOAuthLoginCompletion,
  login2FA,
  persistOAuthTokenContext,
  type OAuthAdoptionDecision,
  type PendingOAuthExchangeResponse
} from '@/api/auth'
import {
  buildSignupAgreementPayload,
  resolveAgreementState,
  type SignupAgreementPayload,
} from '@/features/public/legal'
import type { LoginAgreementDocument } from '@/types'
import {
  clearAllAffiliateReferralCodes,
  loadOAuthAffiliateCode,
  oauthAffiliatePayload
} from '@/utils/oauthAffiliate'

import { useCallbackLifetime } from './useCallbackLifetime'

interface CallbackOptions {
  provider: 'linuxdo' | 'oidc' | 'wechat' | 'dingtalk'
  errorPrefix: string
  completeRegistration: (
    code: string,
    decision: OAuthAdoptionDecision,
    affiliateCode?: string,
    agreement?: Partial<SignupAgreementPayload>
  ) => Promise<PendingOAuthExchangeResponse>
  beforeResume?: () => Promise<boolean | void>
  resolveError?: (code: string, description: string) => string
}

export function useOAuthCallback(options: CallbackOptions) {
  const route = useRoute()
  const router = useRouter()
  const { t: translate } = useI18n()

  const authStore = useAuthStore()
  const appStore = useAppStore()
  const { isActive, waitForCallback } = useCallbackLifetime()

  const isProcessing = ref(true)
  const errorMessage = ref('')
  const needsInvitation = ref(false)
  const invitationCode = ref('')
  const isSubmitting = ref(false)
  const invitationError = ref('')
  const redirectTo = ref('/dashboard')

  const adoptionRequired = ref(false)
  const suggestedDisplayName = ref('')
  const suggestedAvatarUrl = ref('')
  const adoptDisplayName = ref(true)
  const adoptAvatar = ref(true)
  const needsAdoptionConfirmation = ref(false)
  const pendingAccountAction = ref<'none' | 'choose_account_action' | 'create_account' | 'bind_login'>('none')
  const pendingAccountEmail = ref('')
  const bindLoginEmail = ref('')
  const bindLoginPassword = ref('')
  const legacyPendingOAuthToken = ref('')
  const accountActionError = ref('')
  const canReturnToCreateAccount = ref(false)
  const needsTotpChallenge = ref(false)
  const totpTempToken = ref('')
  const totpCode = ref('')
  const totpError = ref('')
  const totpUserEmailMasked = ref('')
  const loginAgreementEnabled = ref(false)
  const loginAgreementMode = ref<'modal' | 'checkbox' | string>('modal')
  const loginAgreementUpdatedAt = ref('')
  const loginAgreementRevision = ref('')
  const loginAgreementDocuments = ref<LoginAgreementDocument[]>([])
  const agreementAccepted = ref(false)
  const showAgreementModal = ref(false)

  const needsCreateAccount = computed(() => pendingAccountAction.value === 'create_account')
  const needsChooser = computed(() => pendingAccountAction.value === 'choose_account_action')
  const needsBindLogin = computed(() => pendingAccountAction.value === 'bind_login')

  // Every callback failure has one notification owner, including provider errors.
  watch([invitationError, accountActionError, totpError, errorMessage], (messages, previous) => {
    const message = messages.find((value, index) => value && value !== previous[index])
    if (isActive() && message) appStore.showError(message)
  })

  type PendingCallbackCompletion = PendingOAuthExchangeResponse & {
    status?: string
    state?: string
    requires_email_completion?: boolean
    step?: string
    pending_email?: string
    resolved_email?: string
    existing_account_email?: string
    compat_email?: string
    email?: string
    suggested_email?: string
    provider_fallback?: string
    intent?: string
    requires_2fa?: boolean
    temp_token?: string
    user_email_masked?: string
  }

  function persistPendingAuthSession(redirect?: string) {
    authStore.setPendingAuthSession({
      token: '',
      token_field: 'pending_oauth_token',
      provider: options.provider,
      redirect: sanitizeRedirectPath(redirect || redirectTo.value)
    })
  }

  function clearPendingAuthSession() {
    authStore.clearPendingAuthSession()
  }

  function parseFragmentParams(): URLSearchParams {
    const raw = typeof window !== 'undefined' ? window.location.hash : ''
    const hash = raw.startsWith('#') ? raw.slice(1) : raw
    if (hash) window.history.replaceState(null, '', window.location.pathname + window.location.search)
    return new URLSearchParams(hash)
  }


  function sanitizeRedirectPath(path: string | null | undefined): string {
    if (!path) return '/dashboard'
    if (!path.startsWith('/')) return '/dashboard'
    if (path.includes('\\')) return '/dashboard'
    if (path.startsWith('//')) return '/dashboard'
    if (path.includes('://')) return '/dashboard'
    if (path.includes('\n') || path.includes('\r')) return '/dashboard'
    return path
  }

  function currentAdoptionDecision(): OAuthAdoptionDecision {
    return {
      adoptDisplayName: adoptDisplayName.value,
      adoptAvatar: adoptAvatar.value
    }
  }

  function serializeAdoptionDecision(decision: OAuthAdoptionDecision): Record<string, boolean> {
    const payload: Record<string, boolean> = {}
    if (typeof decision.adoptDisplayName === 'boolean') {
      payload.adopt_display_name = decision.adoptDisplayName
    }
    if (typeof decision.adoptAvatar === 'boolean') {
      payload.adopt_avatar = decision.adoptAvatar
    }
    return payload
  }

  function applyAdoptionSuggestionState(completion: {
    adoption_required?: boolean
    suggested_display_name?: string
    suggested_avatar_url?: string
  }) {
    adoptionRequired.value = completion.adoption_required === true
    suggestedDisplayName.value = completion.suggested_display_name || ''
    suggestedAvatarUrl.value = completion.suggested_avatar_url || ''

    if (!suggestedDisplayName.value) {
      adoptDisplayName.value = false
    }
    if (!suggestedAvatarUrl.value) {
      adoptAvatar.value = false
    }
  }

  function hasSuggestedProfile(completion: {
    suggested_display_name?: string
    suggested_avatar_url?: string
  }): boolean {
    return Boolean(completion.suggested_display_name || completion.suggested_avatar_url)
  }

  function normalizedPendingState(value: string | null | undefined): string {
    return value?.trim().toLowerCase() || ''
  }

  function extractPendingAccountEmail(completion: PendingCallbackCompletion): string {
    return (
      completion.pending_email ||
      completion.existing_account_email ||
      completion.compat_email ||
      completion.resolved_email ||
      completion.email ||
      completion.suggested_email ||
      (options.provider === 'wechat' && typeof route.query.email === 'string' ? route.query.email : '') ||
      ''
    ).trim()
  }

  function resolvePendingAccountAction(
    completion: PendingCallbackCompletion
  ): 'none' | 'choose_account_action' | 'create_account' | 'bind_login' {
    const raw = normalizedPendingState(completion.step || completion.status || completion.state || completion.error || completion.intent)
    if (
      raw === 'choice' ||
      raw === 'choose_account_action_required' ||
      raw === 'choose_account_action' ||
      raw === 'choose_account' ||
      raw === 'choose'
    ) {
      return 'choose_account_action'
    }
    if (raw === 'email_required' || raw === 'create_account_required' || raw === 'create_account') {
      return 'create_account'
    }
    if (
      raw === 'existing_account' ||
      raw === 'bind_login_required' ||
      raw === 'bind_login' ||
      raw === 'existing_account_binding_required' ||
      raw === 'existing_account_required' ||
      raw === 'adopt_existing_user_by_email'
    ) {
      return 'bind_login'
    }
    return 'none'
  }

  function applyPendingAccountAction(completion: PendingCallbackCompletion) {
    const action = resolvePendingAccountAction(completion)
    pendingAccountAction.value = action
    accountActionError.value = ''
    needsTotpChallenge.value = false
    totpTempToken.value = ''
    totpCode.value = ''
    totpError.value = ''
    totpUserEmailMasked.value = ''

    const email = extractPendingAccountEmail(completion)
    if (action === 'choose_account_action') {
      pendingAccountEmail.value = email
      bindLoginEmail.value = email
      bindLoginPassword.value = ''
      canReturnToCreateAccount.value = false
      return
    }

    if (action === 'create_account') {
      pendingAccountEmail.value = email
      canReturnToCreateAccount.value = true
      return
    }

    if (action === 'bind_login') {
      bindLoginEmail.value = email
      bindLoginPassword.value = ''
      canReturnToCreateAccount.value = false
      return
    }

    canReturnToCreateAccount.value = false
  }

  function applyTotpChallenge(completion: PendingCallbackCompletion): boolean {
    if (completion.requires_2fa !== true || !completion.temp_token) {
      return false
    }

    pendingAccountAction.value = 'none'
    needsInvitation.value = false
    needsAdoptionConfirmation.value = false
    needsTotpChallenge.value = true
    totpTempToken.value = completion.temp_token
    totpCode.value = ''
    totpError.value = ''
    totpUserEmailMasked.value = completion.user_email_masked || ''
    isProcessing.value = false
    return true
  }

  function switchToBindLoginMode(nextEmail?: string) {
    pendingAccountAction.value = 'bind_login'
    bindLoginEmail.value = bindLoginEmail.value.trim() || nextEmail?.trim() || pendingAccountEmail.value.trim()
    bindLoginPassword.value = ''
    accountActionError.value = ''
    canReturnToCreateAccount.value = true
  }

  function switchToCreateAccountMode() {
    pendingAccountAction.value = 'create_account'
    pendingAccountEmail.value = pendingAccountEmail.value.trim() || bindLoginEmail.value.trim()
    accountActionError.value = ''
  }

  function getRequestErrorMessage(error: unknown, fallback: string): string {
    const err = error as { message?: string; response?: { data?: { detail?: string; message?: string } } }
    return extractApiErrorMessage(err, '') || fallback
  }

  function isCreateAccountRecoveryError(error: unknown): boolean {
    const reason = extractApiErrorCode(error)?.trim().toLowerCase()
    return reason === 'email_exists' ||
      reason === 'bind_login_required' ||
      reason === 'bind_login' ||
      reason === 'adopt_existing_user_by_email' ||
      reason === 'existing_account_required' ||
      reason === 'existing_account_binding_required'
  }

  async function finalizeCompletion(completion: PendingOAuthExchangeResponse, redirect: string) {
    if (getOAuthCompletionKind(completion) === 'bind') {
      const bindRedirect = sanitizeRedirectPath(completion.redirect || '/profile')
      clearPendingAuthSession()
      clearAllAffiliateReferralCodes()
      appStore.showSuccess(translate('profile.authBindings.bindSuccess'))
      await waitForCallback(router.replace(bindRedirect))
      return
    }

    if (!isOAuthLoginCompletion(completion)) {
      throw new Error(translate(`${options.errorPrefix}.callbackMissingToken`))
    }

    persistOAuthTokenContext(completion)
    await waitForCallback(authStore.setToken(completion.access_token))
    clearAllAffiliateReferralCodes()
    appStore.showSuccess(translate('auth.loginSuccess'))
    await waitForCallback(router.replace(redirect))
  }

  async function handleProviderCompletion(completion: PendingCallbackCompletion, redirect: string, initial = false) {
    if (options.provider !== 'dingtalk' || (completion.step !== 'email_completion' && !completion.requires_email_completion)) return false
    if (initial && route.query.bind === '1') {
      pendingAccountAction.value = 'bind_login'
      bindLoginEmail.value = typeof route.query.email === 'string' ? route.query.email.trim() : ''
      bindLoginPassword.value = ''
      canReturnToCreateAccount.value = true
      isProcessing.value = false
      persistPendingAuthSession(redirect)
    } else {
      await waitForCallback(router.replace('/auth/dingtalk/email-completion?redirect=' + encodeURIComponent(redirect)))
    }
    return true
  }

  async function finalizePendingAccountResponse(completion: PendingCallbackCompletion, initial = false) {
    applyAdoptionSuggestionState(completion)
    const redirect = sanitizeRedirectPath(completion.redirect || redirectTo.value)
    redirectTo.value = redirect
    if (await waitForCallback(handleProviderCompletion(completion, redirect, initial))) return

    if (completion.error === 'invitation_required') {
      pendingAccountAction.value = 'none'
      await loadAgreementSettings()
      needsInvitation.value = true
      needsAdoptionConfirmation.value = false
      isProcessing.value = false
      persistPendingAuthSession(redirect)
      return
    }

    if (applyTotpChallenge(completion)) {
      persistPendingAuthSession(redirect)
      return
    }

    applyPendingAccountAction(completion)
    if (pendingAccountAction.value !== 'none') {
      needsInvitation.value = false
      needsAdoptionConfirmation.value = false
      isProcessing.value = false
      persistPendingAuthSession(redirect)
      return
    }

    if (initial && adoptionRequired.value && hasSuggestedProfile(completion)) {
      needsAdoptionConfirmation.value = true
      isProcessing.value = false
      persistPendingAuthSession(redirect)
      return
    }

    if (completion.auth_result === 'pending_session') {
      needsInvitation.value = false
      needsAdoptionConfirmation.value = false
      isProcessing.value = false
      persistPendingAuthSession(redirect)
      return
    }

    await waitForCallback(finalizeCompletion(completion, redirect))
  }

  async function handleSubmitInvitation() {
    if (!isActive() || isSubmitting.value) return
    invitationError.value = ''
    if (!invitationCode.value.trim()) return
    if (loginAgreementEnabled.value && !agreementAccepted.value) {
      if (loginAgreementMode.value !== 'checkbox') {
        showAgreementModal.value = true
      }
      return
    }

    isSubmitting.value = true
    try {
      const affCode = loadOAuthAffiliateCode()
      const decision = currentAdoptionDecision()
      const agreementPayload = buildSignupAgreementPayload(
        loginAgreementEnabled.value,
        agreementAccepted.value,
        loginAgreementRevision.value
      )
      const hasAgreement = agreementPayload.login_agreement_accepted !== undefined
      const completion: PendingCallbackCompletion = legacyPendingOAuthToken.value
        ? (
            await waitForCallback(apiClient.post<PendingCallbackCompletion>(`/auth/oauth/${options.provider}/complete-registration`, {
              pending_oauth_token: legacyPendingOAuthToken.value,
              invitation_code: invitationCode.value.trim(),
              ...oauthAffiliatePayload(affCode),
              ...serializeAdoptionDecision(decision),
              ...(hasAgreement ? agreementPayload : {})
            }))
          ).data
        : hasAgreement
          ? await waitForCallback(options.completeRegistration(invitationCode.value.trim(), decision, affCode || undefined, agreementPayload))
          : affCode
            ? await waitForCallback(options.completeRegistration(invitationCode.value.trim(), decision, affCode))
            : await waitForCallback(options.completeRegistration(invitationCode.value.trim(), decision))
      await waitForCallback(finalizePendingAccountResponse(completion))
    } catch (failure: unknown) {
      if (!isActive()) return
      if (extractApiErrorCode(failure) === 'LOGIN_AGREEMENT_REQUIRED') {
        agreementAccepted.value = false
        await loadAgreementSettings()
        invitationError.value = translate('publicPortal.legal.agreementRequiredRefresh')
        return
      }
      const err = failure as { message?: string; response?: { data?: { message?: string } } }
      invitationError.value =
        extractApiErrorMessage(err, '') || translate(`${options.errorPrefix}.completeRegistrationFailed`)
    } finally {
      if (isActive()) isSubmitting.value = false
    }
  }

  async function loadAgreementSettings(): Promise<void> {
    if (typeof getPublicSettings !== 'function') return
    try {
      const settings = await waitForCallback(getPublicSettings())
      if (!isActive() || !settings) return
      const resolved = resolveAgreementState(settings)
      loginAgreementDocuments.value = resolved.documents
      loginAgreementEnabled.value = resolved.enabled
      loginAgreementMode.value = resolved.mode
      loginAgreementUpdatedAt.value = resolved.updatedAt
      loginAgreementRevision.value = resolved.revision
      agreementAccepted.value = !resolved.enabled
      showAgreementModal.value = resolved.enabled && resolved.mode !== 'checkbox'
    } catch {
      // Keep existing agreement state; backend enforces if required.
    }
  }

  function acceptAgreement() {
    agreementAccepted.value = true
    showAgreementModal.value = false
  }

  function rejectAgreement() {
    agreementAccepted.value = false
    showAgreementModal.value = false
  }

  function openAgreementModal() {
    showAgreementModal.value = true
  }

  function closeAgreementModal() {
    showAgreementModal.value = false
  }

  async function handleContinueLogin() {
    if (!isActive() || isSubmitting.value) return
    isSubmitting.value = true
    try {
      const completion = await waitForCallback(exchangePendingOAuthCompletion(currentAdoptionDecision())) as PendingCallbackCompletion
      await waitForCallback(finalizePendingAccountResponse(completion))
    } catch (failure: unknown) {
      if (!isActive()) return
      errorMessage.value = getRequestErrorMessage(failure, translate('auth.loginFailed'))
      needsAdoptionConfirmation.value = false
    } finally {
      if (isActive()) isSubmitting.value = false
    }
  }

  async function handleCreateAccount(payload: PendingOAuthCreateAccountPayload) {
    if (!isActive() || isSubmitting.value) return
    accountActionError.value = ''
    if (!payload.email || !payload.password) return

    isSubmitting.value = true
    try {
      const { data } = await waitForCallback(apiClient.post<PendingCallbackCompletion>('/auth/oauth/pending/create-account', {
        email: payload.email,
        password: payload.password,
        verify_code: payload.verifyCode || undefined,
        ...(payload.turnstileToken ? { turnstile_token: payload.turnstileToken } : {}),
        ...(payload.tencentCaptchaTicket
          ? {
              tencent_captcha_ticket: payload.tencentCaptchaTicket,
              tencent_captcha_randstr: payload.tencentCaptchaRandstr
          }
          : {}),
        invitation_code: payload.invitationCode || undefined,
        ...(payload.loginAgreementAccepted !== undefined
          ? {
              login_agreement_accepted: payload.loginAgreementAccepted,
              login_agreement_revision: payload.loginAgreementRevision || ''
            }
          : {}),
        ...oauthAffiliatePayload(loadOAuthAffiliateCode()),
        ...serializeAdoptionDecision(currentAdoptionDecision())
      }))
      await waitForCallback(finalizePendingAccountResponse(data))
    } catch (failure: unknown) {
      if (!isActive()) return
      if (isCreateAccountRecoveryError(failure)) {
        switchToBindLoginMode(payload.email.trim())
        return
      }
      accountActionError.value = getRequestErrorMessage(failure, translate('auth.loginFailed'))
    } finally {
      if (isActive()) isSubmitting.value = false
    }
  }

  async function handleBindLogin() {
    if (!isActive() || isSubmitting.value) return
    accountActionError.value = ''
    const email = bindLoginEmail.value.trim()
    const password = bindLoginPassword.value
    if (!email || !password) return

    isSubmitting.value = true
    try {
      const { data } = await waitForCallback(apiClient.post<PendingCallbackCompletion>('/auth/oauth/pending/bind-login', {
        email,
        password,
        ...serializeAdoptionDecision(currentAdoptionDecision())
      }))
      await waitForCallback(finalizePendingAccountResponse(data))
    } catch (failure: unknown) {
      if (!isActive()) return
      accountActionError.value = getRequestErrorMessage(failure, translate('auth.loginFailed'))
    } finally {
      if (isActive()) isSubmitting.value = false
    }
  }

  async function handleSubmitTotpChallenge() {
    if (!isActive() || isSubmitting.value) return
    totpError.value = ''
    const code = totpCode.value.trim()
    if (!totpTempToken.value || code.length !== 6) return

    isSubmitting.value = true
    try {
      const completion = await waitForCallback(login2FA({
        temp_token: totpTempToken.value,
        totp_code: code
      }))
      persistOAuthTokenContext(completion)
      await waitForCallback(authStore.setToken(completion.access_token))
      clearAllAffiliateReferralCodes()
      appStore.showSuccess(translate('auth.loginSuccess'))
      await waitForCallback(router.replace(redirectTo.value))
    } catch (failure: unknown) {
      if (!isActive()) return
      totpError.value = getRequestErrorMessage(failure, translate('auth.loginFailed'))
    } finally {
      if (isActive()) isSubmitting.value = false
    }
  }

  onMounted(async () => {
    if (options.beforeResume && await options.beforeResume()) return
    if (!isActive()) return

    const params = parseFragmentParams()
    const legacyPendingToken = params.get('pending_oauth_token')?.trim() || ''
    const error = params.get('error')
    const errorDesc = params.get('error_description') || params.get('error_message') || ''
    const redirect = sanitizeRedirectPath(
      params.get('redirect') || (route.query.redirect as string | undefined) || '/dashboard'
    )

    try {
      if (params.has('access_token') || params.has('refresh_token')) {
        errorMessage.value = translate('auth.loginFailed')
        isProcessing.value = false
        return
      }

      if (error === 'invitation_required' && legacyPendingToken) {
        legacyPendingOAuthToken.value = legacyPendingToken
        redirectTo.value = redirect
        await loadAgreementSettings()
        needsInvitation.value = true
        isProcessing.value = false
        return
      }

      if (error) {
        errorMessage.value = options.resolveError?.(error, errorDesc) || errorDesc || error
        isProcessing.value = false
        return
      }

      const completion = await waitForCallback(exchangePendingOAuthCompletion(undefined, params.get('code') || undefined)) as PendingCallbackCompletion
      redirectTo.value = sanitizeRedirectPath(
        completion.redirect || (route.query.redirect as string | undefined) || '/dashboard'
      )
      await waitForCallback(finalizePendingAccountResponse(completion, true))
    } catch (failure: unknown) {
      if (!isActive()) return
      clearPendingAuthSession()
      errorMessage.value = getRequestErrorMessage(failure, translate('auth.loginFailed'))
      isProcessing.value = false
    }
  })

  return {
    isProcessing,
    errorMessage,
    needsInvitation,
    invitationCode,
    isSubmitting,
    invitationError,
    redirectTo,
    adoptionRequired,
    suggestedDisplayName,
    suggestedAvatarUrl,
    adoptDisplayName,
    adoptAvatar,
    needsAdoptionConfirmation,
    pendingAccountEmail,
    bindLoginEmail,
    bindLoginPassword,
    legacyPendingOAuthToken,
    accountActionError,
    canReturnToCreateAccount,
    needsTotpChallenge,
    totpTempToken,
    totpCode,
    totpError,
    totpUserEmailMasked,
    loginAgreementEnabled,
    loginAgreementMode,
    loginAgreementUpdatedAt,
    loginAgreementDocuments,
    agreementAccepted,
    showAgreementModal,
    acceptAgreement,
    rejectAgreement,
    openAgreementModal,
    closeAgreementModal,
    needsCreateAccount,
    needsChooser,
    needsBindLogin,
    handleSubmitInvitation,
    handleContinueLogin,
    handleCreateAccount,
    handleBindLogin,
    handleSubmitTotpChallenge,
    switchToBindLoginMode,
    switchToCreateAccountMode,
    sanitizeRedirectPath,
    getRequestErrorMessage,
    isActive,
    waitForCallback,
  }
}
