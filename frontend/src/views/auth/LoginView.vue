<template>
  <AuthLayout>
    <div class="space-y-4">
      <!-- Title -->
      <div class="space-y-1">
        <div class="flex items-center justify-between gap-2">
          <Badge tone="neutral" mono>CONSOLE AUTH</Badge>
          <Badge v-if="!publicSettingsLoaded" tone="neutral" dot mono>SYNCING</Badge>
        </div>
        <h1 class="auth-title">
          {{ t('auth.welcomeBack') }}
        </h1>
        <p class="auth-intro">
          {{ t('auth.signInToAccount') }}
        </p>
      </div>
      <!-- Login Form -->
      <ErrorState
        v-if="errorMessage"
        :title="errorMessage"
        compact
        :retryable="false"
      />
      <div v-if="!publicSettingsLoaded" class="space-y-2 py-1" aria-hidden="true">
        <Skeleton width="100%" height="36px" />
        <Skeleton width="100%" height="36px" />
      </div>
      <div
        v-if="publicSettingsError"
        data-testid="login-settings-error"
        role="alert"
        class="auth-summary flex items-center justify-between gap-3 border border-red-200 bg-red-50/80 p-3 text-sm text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200"
      >
        <span>{{ t('publicPortal.legal.settingsLoadFailedHint') }}</span>
        <button
          type="button"
          data-testid="login-settings-retry"
          class="btn btn-outline shrink-0 text-xs"
          :disabled="isLoading"
          @click="loadPublicSettings"
        >
          {{ t('publicPortal.legal.retry') }}
        </button>
      </div>
      <form @submit.prevent="handleLogin" class="space-y-3">
        <!-- Email Input -->
        <div>
          <label for="email" class="input-label">
            {{ t('auth.emailLabel') }}
          </label>
          <div class="relative">
            <input
              id="email"
              v-model="formData.email"
              type="email"
              required
              autofocus
              autocomplete="email"
              :disabled="authActionDisabled"
              class="input"
              :class="{ 'input-error': errors.email }"
              :placeholder="t('auth.emailPlaceholder')"
            />
          </div>
        </div>

        <!-- Password Input -->
        <div>
          <label for="password" class="input-label">
            {{ t('auth.passwordLabel') }}
          </label>
          <div class="relative">
            <input
              id="password"
              v-model="formData.password"
              :type="showPassword ? 'text' : 'password'"
              required
              autocomplete="current-password"
              :disabled="authActionDisabled"
              class="input pr-11"
              :class="{ 'input-error': errors.password }"
              :placeholder="t('auth.passwordPlaceholder')"
            />
            <button
              type="button"
              @click="showPassword = !showPassword"
              :aria-label="t('auth.passwordLabel')"
              :aria-pressed="showPassword"
              :disabled="authActionDisabled"
              class="absolute inset-y-0 right-0 flex items-center pr-3.5 text-[var(--muted-foreground)] transition-colors hover:text-[var(--muted-foreground)]"
            >
              <Icon v-if="showPassword" name="eyeOff" size="md" />
              <Icon v-else name="eye" size="md" />
            </button>
          </div>
        </div>

        <LoginAgreementPrompt
          :enabled="loginAgreementEnabled"
          :accepted="agreementAccepted"
          :documents="loginAgreementDocuments"
          :mode="loginAgreementMode"
          :updated-at="loginAgreementUpdatedAt"
          :visible="showAgreementModal"
          @accept="acceptLoginAgreement"
          @reject="rejectLoginAgreement"
          @open="showAgreementModal = true"
          @close="showAgreementModal = false"
        />
        <label class="flex items-center gap-2 text-[13px]" :title="t('auth.rememberMe')">
          <input v-model="rememberMe" type="checkbox" class="h-4 w-4" />
          {{ t('auth.rememberMe') }}
        </label>
        <!-- Turnstile Widget -->
        <div v-if="captchaEnabled">
          <TurnstileWidget
            ref="turnstileRef"
            :turnstile-enabled="turnstileEnabled"
            :turnstile-site-key="turnstileSiteKey"
            :tencent-enabled="tencentCaptchaEnabled"
            :tencent-app-id="tencentCaptchaAppId"
            :tencent-region="tencentCaptchaRegion"
            :aliyun-enabled="aliyunCaptchaEnabled"
            :aliyun-scene-id="aliyunCaptchaSceneId"
            :aliyun-prefix="aliyunCaptchaPrefix"
            :aliyun-region="aliyunCaptchaRegion"
            @verify="onTurnstileVerify"
            @expire="onTurnstileExpire"
            @error="onTurnstileError"
          />
        </div>

        <!-- Submit Button -->
        <div class="auth-actions">
        <Button
          type="submit"
          variant="primary"
          :loading="isLoading"
          :disabled="authActionDisabled || (turnstileActive && !turnstileToken)"
          block
          class="btn btn-primary w-full"
        >
          <Icon v-if="!isLoading" name="login" size="sm" />
          <span>{{ isLoading ? t('auth.signingIn') : t('auth.signIn') }}</span>
        </Button>
        <router-link v-if="passwordResetEnabled && !backendModeEnabled" :to="{ path: '/forgot-password', query: router.currentRoute.value.query }" class="btn btn-outline">
          {{ t('auth.forgotPassword') }}
        </router-link>
        </div>

        <div v-if="showPasskeyLogin || showOAuthLogin" class="space-y-3 pt-1">
          <div class="auth-divider">{{ t('auth.oauthOrContinue') }}</div>

          <Button
            v-if="showPasskeyLogin"
            type="button"
            variant="secondary"
            :loading="passkeyLoading"
            class="btn btn-secondary btn-outline w-full"
            :disabled="authActionDisabled"
            block
            @click="handlePasskeyLogin"
          >
            <Icon v-if="!passkeyLoading" name="key" size="sm" />
            <span>{{ passkeyLoading ? t('auth.passkeySigningIn') : t('auth.passkeySignIn') }}</span>
          </Button>

          <OAuthLoginSections
            v-if="showOAuthLogin"
            :disabled="authActionDisabled"
            :github-enabled="githubOAuthEnabled"
            :google-enabled="googleOAuthEnabled"
            :linuxdo-enabled="linuxdoOAuthEnabled"
            :dingtalk-enabled="dingtalkOAuthEnabled"
            :wechat-enabled="wechatOAuthEnabled"
            :oidc-enabled="oidcOAuthEnabled"
            :oidc-provider-name="oidcOAuthProviderName"
            @start="handleOAuthStart"
          />
        </div>
      </form>
    </div>

    <!-- Footer -->
    <template v-if="!backendModeEnabled && publicSettingsLoaded && registrationEnabled" #footer>
        <router-link
          :to="{ path: '/register', query: router.currentRoute.value.query }"
          class="btn btn-outline w-full"
        >
          {{ t('auth.createAccount') }}<span class="sr-only">{{ t('auth.signUp') }}</span>
        </router-link>
    </template>
  </AuthLayout>

  <!-- 2FA Modal -->
  <TotpLoginModal
    v-if="show2FAModal"
    ref="totpModalRef"
    :temp-token="totpTempToken"
    :user-email-masked="totpUserEmailMasked"
    @verify="handle2FAVerify"
    @cancel="handle2FACancel"
  />
</template>

<script setup lang="ts">
import { computed, ref, reactive, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { AuthLayout } from '@/components/layout'
import { Badge, Button, ErrorState, Skeleton } from '@/components/ui'
import OAuthLoginSections from '@/components/auth/OAuthLoginSections.vue'
import LoginAgreementPrompt from '@/components/auth/LoginAgreementPrompt.vue'
import TotpLoginModal from '@/components/auth/TotpLoginModal.vue'
import { resolveActiveCaptchaProvider } from '@/components/auth/captchaProvider'
import Icon from '@/components/icons/Icon.vue'
import TurnstileWidget from '@/components/CaptchaChallenge.vue'
import { useAuthStore, useAppStore } from '@/stores'
import { sanitizeInternalRedirect } from '@/router/redirect'
import { authStorage } from '@/api/tokenRefresh'
import {
  buildOAuthLoginStartURL,
  getPublicSettings,
  isTotp2FARequired,
  isWeChatWebOAuthEnabled,
  startOAuthLogin,
  type OAuthLoginStart
} from '@/api/auth'
import type {
  ActionCaptchaRequestProof,
  LoginAgreementDocument,
  TotpLoginResponse
} from '@/types'
import { extractApiErrorMessage, extractI18nErrorMessage } from '@/utils/apiError'
import { clearAllAffiliateReferralCodes } from '@/utils/oauthAffiliate'
import { resolveAgreementState } from '@/features/public/legal'

const { t } = useI18n()

// ==================== Router & Stores ====================

const router = useRouter()
const authStore = useAuthStore()
const appStore = useAppStore()

// ==================== State ====================

const isLoading = ref<boolean>(false)
const passkeyLoading = ref<boolean>(false)
const errorMessage = ref<string>('')
const showPassword = ref<boolean>(false)
const publicSettingsLoaded = ref<boolean>(false)
const publicSettingsError = ref<boolean>(false)
const rememberMe = ref(false)

// Public settings
const registrationEnabled = ref<boolean>(false)
const turnstileEnabled = ref<boolean>(false)
const turnstileSiteKey = ref<string>('')
const tencentCaptchaEnabled = ref<boolean>(false)
const tencentCaptchaAppId = ref<string>('')
const tencentCaptchaRegion = ref<string>('cn')
const aliyunCaptchaEnabled = ref<boolean>(false)
const aliyunCaptchaSceneId = ref<string>('')
const aliyunCaptchaPrefix = ref<string>('')
const aliyunCaptchaRegion = ref<string>('cn')
const linuxdoOAuthEnabled = ref<boolean>(false)
const dingtalkOAuthEnabled = ref<boolean>(false)
const wechatOAuthEnabled = ref<boolean>(false)
const backendModeEnabled = ref<boolean>(false)
const oidcOAuthEnabled = ref<boolean>(false)
const oidcOAuthProviderName = ref<string>('OIDC')
const githubOAuthEnabled = ref<boolean>(false)
const googleOAuthEnabled = ref<boolean>(false)
const passwordResetEnabled = ref<boolean>(false)
const passkeyEnabled = ref<boolean>(false)
const loginAgreementEnabled = ref<boolean>(false)
const loginAgreementMode = ref<'modal' | 'checkbox' | string>('modal')
const loginAgreementUpdatedAt = ref<string>('')
const loginAgreementRevision = ref<string>('')
const loginAgreementDocuments = ref<LoginAgreementDocument[]>([])
const agreementAccepted = ref<boolean>(false)
const showAgreementModal = ref<boolean>(false)

// Turnstile
const turnstileRef = ref<InstanceType<typeof TurnstileWidget> | null>(null)
const turnstileToken = ref<string>('')
const tencentCaptchaRandstr = ref<string>('')
const activeCaptchaProvider = computed(() =>
  resolveActiveCaptchaProvider({
    turnstileEnabled: turnstileEnabled.value,
    turnstileSiteKey: turnstileSiteKey.value,
    tencentEnabled: tencentCaptchaEnabled.value,
    tencentAppId: tencentCaptchaAppId.value,
    aliyunEnabled: aliyunCaptchaEnabled.value,
    aliyunSceneId: aliyunCaptchaSceneId.value,
    aliyunPrefix: aliyunCaptchaPrefix.value
  })
)
const turnstileActive = computed(() => activeCaptchaProvider.value === 'turnstile')
const tencentActive = computed(() => activeCaptchaProvider.value === 'tencent')
const aliyunActive = computed(() => activeCaptchaProvider.value === 'aliyun')
// 动作触发式验证码（腾讯/阿里云）：提交、OAuth 启动、passkey 时弹窗验证
const actionCaptchaEnabled = computed(() => tencentActive.value || aliyunActive.value)
const captchaEnabled = computed(() => activeCaptchaProvider.value !== 'none')

// 2FA state
const show2FAModal = ref<boolean>(false)
const totpTempToken = ref<string>('')
const totpUserEmailMasked = ref<string>('')
const totpModalRef = ref<InstanceType<typeof TotpLoginModal> | null>(null)

const formData = reactive({
  email: '',
  password: ''
})

const errors = reactive({
  email: '',
  password: '',
  turnstile: ''
})

const validationToastMessage = computed(
  () => errors.email || errors.password || errors.turnstile || ''
)

const agreementGateActive = computed(
  () => loginAgreementEnabled.value && !agreementAccepted.value
)

const authActionDisabled = computed(
  () =>
    isLoading.value ||
    passkeyLoading.value ||
    !publicSettingsLoaded.value ||
    publicSettingsError.value ||
    (agreementGateActive.value && loginAgreementMode.value !== 'checkbox')
)

const showPasskeyLogin = computed(() => passkeyEnabled.value)

const showOAuthLogin = computed(
  () =>
    !backendModeEnabled.value &&
    (linuxdoOAuthEnabled.value ||
      dingtalkOAuthEnabled.value ||
      wechatOAuthEnabled.value ||
      oidcOAuthEnabled.value ||
      githubOAuthEnabled.value ||
      googleOAuthEnabled.value)
)

watch(validationToastMessage, (value, previousValue) => {
  if (value && value !== previousValue) {
    appStore.showError(value)
  }
})

// ==================== Lifecycle ====================

async function loadPublicSettings(): Promise<void> {
  publicSettingsLoaded.value = false
  publicSettingsError.value = false
  try {
    const settings = await getPublicSettings()
    registrationEnabled.value = settings.registration_enabled === true
    turnstileEnabled.value = settings.turnstile_enabled
    turnstileSiteKey.value = settings.turnstile_site_key || ''
    tencentCaptchaEnabled.value = settings.tencent_captcha_enabled === true
    tencentCaptchaAppId.value = settings.tencent_captcha_app_id || ''
    tencentCaptchaRegion.value = settings.tencent_captcha_region || 'cn'
    aliyunCaptchaEnabled.value = settings.aliyun_captcha_enabled === true
    aliyunCaptchaSceneId.value = settings.aliyun_captcha_scene_id || ''
    aliyunCaptchaPrefix.value = settings.aliyun_captcha_prefix || ''
    aliyunCaptchaRegion.value = settings.aliyun_captcha_region || 'cn'
    linuxdoOAuthEnabled.value = settings.linuxdo_oauth_enabled
    dingtalkOAuthEnabled.value = settings.dingtalk_oauth_enabled ?? false
    wechatOAuthEnabled.value = isWeChatWebOAuthEnabled(settings)
    backendModeEnabled.value = settings.backend_mode_enabled
    oidcOAuthEnabled.value = settings.oidc_oauth_enabled
    oidcOAuthProviderName.value = settings.oidc_oauth_provider_name || 'OIDC'
    githubOAuthEnabled.value = settings.github_oauth_enabled
    googleOAuthEnabled.value = settings.google_oauth_enabled
    passwordResetEnabled.value = settings.password_reset_enabled
    passkeyEnabled.value = settings.passkey_enabled === true
    applyLoginAgreementSettings(settings)
    if (errorMessage.value === t('publicPortal.legal.settingsLoadFailedHint')) {
      errorMessage.value = ''
    }
  } catch (error) {
    console.error('Failed to load public settings:', error)
    publicSettingsError.value = true
    loginAgreementEnabled.value = false
    agreementAccepted.value = false
  } finally {
    publicSettingsLoaded.value = true
  }
}

onMounted(async () => {
  try {
    const rememberedEmail = authStorage.rememberedEmail.get()
    if (rememberedEmail) {
      formData.email = rememberedEmail
      rememberMe.value = true
    }
  } catch {
    rememberMe.value = false
  }
  const expiredFlag = authStorage.expiredNotice.get()
  if (expiredFlag) {
    authStorage.expiredNotice.remove()
    const message = t('auth.reloginRequired')
    errorMessage.value = message
    appStore.showWarning(message)
  }

  await loadPublicSettings()
})

// ==================== Login Agreement ====================

function applyLoginAgreementSettings(settings: {
  login_agreement_enabled?: boolean
  login_agreement_mode?: string
  login_agreement_updated_at?: string
  login_agreement_revision?: string
  login_agreement_documents?: LoginAgreementDocument[]
}): void {
  const resolved = resolveAgreementState(settings)
  loginAgreementDocuments.value = resolved.documents
  loginAgreementEnabled.value = resolved.enabled
  loginAgreementMode.value = resolved.mode
  loginAgreementUpdatedAt.value = resolved.updatedAt
  loginAgreementRevision.value = resolved.revision

  agreementAccepted.value = !loginAgreementEnabled.value || hasAcceptedLoginAgreement(loginAgreementRevision.value)
  showAgreementModal.value =
    loginAgreementEnabled.value && !agreementAccepted.value && loginAgreementMode.value !== 'checkbox'
}

function hasAcceptedLoginAgreement(revision: string): boolean {
  if (!revision) {
    return false
  }
  const parsed = authStorage.loginAgreement.getJSON<{ revision?: string } | null>(null)
  return parsed?.revision === revision
}

function acceptLoginAgreement(): void {
  if (loginAgreementRevision.value) {
    authStorage.loginAgreement.setJSON({
      revision: loginAgreementRevision.value,
      accepted_at: new Date().toISOString()
    })
  }
  agreementAccepted.value = true
  showAgreementModal.value = false
}

function rejectLoginAgreement(): void {
  authStorage.loginAgreement.remove()
  agreementAccepted.value = false
  showAgreementModal.value = false
  appStore.showWarning(t('legal.loginAgreementPrompt.loginRejectedWarning'))
}

// ==================== Turnstile Handlers ====================

function onTurnstileVerify(token: string, randstr = ''): void {
  turnstileToken.value = token
  tencentCaptchaRandstr.value = randstr
  errors.turnstile = ''
}

function onTurnstileExpire(): void {
  turnstileToken.value = ''
  tencentCaptchaRandstr.value = ''
  errors.turnstile = t('auth.turnstileExpired')
}

function onTurnstileError(): void {
  turnstileToken.value = ''
  tencentCaptchaRandstr.value = ''
  errors.turnstile = t('auth.turnstileFailed')
}

function resetCaptchaProof(): void {
  turnstileRef.value?.reset()
  turnstileToken.value = ''
  tencentCaptchaRandstr.value = ''
  errors.turnstile = ''
}

async function acquireActionProof(): Promise<boolean> {
  if (!actionCaptchaEnabled.value) return true

  const proof = await turnstileRef.value?.verifyAction()
  if (!proof) return false

  turnstileToken.value = proof.token
  tencentCaptchaRandstr.value = proof.randstr
  return true
}

// ==================== Validation ====================

function validateForm(): boolean {
  // Reset errors
  errors.email = ''
  errors.password = ''
  errors.turnstile = ''

  let isValid = true

  if (agreementGateActive.value) {
    appStore.showWarning(t('legal.loginAgreementPrompt.loginRequiredWarning'))
    if (loginAgreementMode.value !== 'checkbox') {
      showAgreementModal.value = true
    }
    return false
  }

  // Email validation
  const normalizedEmail = formData.email.trim()
  if (!normalizedEmail) {
    errors.email = t('auth.emailRequired')
    isValid = false
  } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(normalizedEmail)) {
    errors.email = t('auth.invalidEmail')
    isValid = false
  }

  // Password validation
  if (!formData.password) {
    errors.password = t('auth.passwordRequired')
    isValid = false
  } else if (formData.password.length < 6) {
    errors.password = t('auth.passwordMinLength')
    isValid = false
  }

  // Turnstile validation
  if (turnstileActive.value && !turnstileToken.value) {
    errors.turnstile = t('auth.completeVerification')
    isValid = false
  }

  return isValid
}

// ==================== Form Handlers ====================

async function handleLogin(): Promise<void> {
  if (isLoading.value || passkeyLoading.value || !publicSettingsLoaded.value) return
  // Clear previous error
  errorMessage.value = ''

  // Validate form
  if (!validateForm()) {
    return
  }

  if (!(await acquireActionProof())) {
    return
  }

  isLoading.value = true

  try {
    // Call auth store login（阿里云 captchaVerifyParam 复用 turnstile_token 字段）
    const normalizedEmail = formData.email.trim()
    formData.email = normalizedEmail
    const response = await authStore.login({
      email: normalizedEmail,
      password: formData.password,
      turnstile_token:
        turnstileActive.value || aliyunActive.value ? turnstileToken.value : undefined,
      tencent_captcha_ticket: tencentActive.value ? turnstileToken.value : undefined,
      tencent_captcha_randstr: tencentActive.value
        ? tencentCaptchaRandstr.value
        : undefined
    })

    persistRememberedEmail()
    // Check if 2FA is required
    if (isTotp2FARequired(response)) {
      const totpResponse = response as TotpLoginResponse
      totpTempToken.value = totpResponse.temp_token || ''
      totpUserEmailMasked.value = totpResponse.user_email_masked || ''
      show2FAModal.value = true
      isLoading.value = false
      return
    }

    // Show success toast
    clearAllAffiliateReferralCodes()
    appStore.showSuccess(t('auth.loginSuccess'))

    // Redirect to dashboard or intended route
    const redirectTo = sanitizeInternalRedirect(
      router.currentRoute.value.query.redirect,
      '/dashboard'
    )
    await router.push(redirectTo)
  } catch (error: unknown) {
    errorMessage.value = extractI18nErrorMessage(error, t, 'auth.errors', t('auth.loginFailed'))

    // Also show error toast
    appStore.showError(errorMessage.value)
  } finally {
    if (captchaEnabled.value) {
      resetCaptchaProof()
    }
    isLoading.value = false
  }
}

async function handlePasskeyLogin(): Promise<void> {
  if (isLoading.value || passkeyLoading.value || !publicSettingsLoaded.value) return
  if (agreementGateActive.value) {
    appStore.showWarning(t('legal.loginAgreementPrompt.loginRequiredWarning'))
    if (loginAgreementMode.value !== 'checkbox') {
      showAgreementModal.value = true
    }
    return
  }

  passkeyLoading.value = true
  try {
    if (typeof window.PublicKeyCredential === 'undefined') {
      throw new Error(t('auth.passkeyFailed'))
    }
    let proof: ActionCaptchaRequestProof | undefined
    if (actionCaptchaEnabled.value) {
      const result = await turnstileRef.value?.verifyAction()
      if (!result) return
      proof = tencentActive.value
        ? {
            tencent_captcha_ticket: result.token,
            tencent_captcha_randstr: result.randstr
          }
        : { turnstile_token: result.token }
    }

    await authStore.loginWithPasskey(proof)
    clearAllAffiliateReferralCodes()
    appStore.showSuccess(t('auth.loginSuccess'))
    const redirectTo = sanitizeInternalRedirect(
      router.currentRoute.value.query.redirect,
      '/dashboard'
    )
    await router.push(redirectTo)
  } catch (error: unknown) {
    const fallback = error instanceof DOMException && error.name === 'NotAllowedError'
      ? t('auth.passkeyCancelled')
      : t('auth.passkeyFailed')
    errorMessage.value = extractI18nErrorMessage(error, t, 'auth.errors', fallback)
    appStore.showError(errorMessage.value)
  } finally {
    if (actionCaptchaEnabled.value) {
      resetCaptchaProof()
    }
    passkeyLoading.value = false
  }
}

function persistRememberedEmail(): void {
  try {
    if (rememberMe.value) {
      authStorage.rememberedEmail.set(formData.email.trim())
    } else {
      authStorage.rememberedEmail.remove()
    }
  } catch {
    rememberMe.value = false
  }
}

async function handleOAuthStart(request: OAuthLoginStart): Promise<void> {
  if (authActionDisabled.value) return
  if (agreementGateActive.value) {
    appStore.showWarning(t('legal.loginAgreementPrompt.loginRequiredWarning'))
    if (loginAgreementMode.value !== 'checkbox') {
      showAgreementModal.value = true
    }
    return
  }

  if (!actionCaptchaEnabled.value) {
    window.location.href = buildOAuthLoginStartURL(request)
    return
  }

  isLoading.value = true
  try {
    const proof = await turnstileRef.value?.verifyAction()
    if (!proof) return

    const result = await startOAuthLogin(
      request,
      tencentActive.value
        ? {
            tencent_captcha_ticket: proof.token,
            tencent_captcha_randstr: proof.randstr
          }
        : { turnstile_token: proof.token }
    )
    window.location.href = result.authorize_url
  } catch (error: unknown) {
    errorMessage.value = extractI18nErrorMessage(
      error,
      t,
      'auth.errors',
      t('auth.turnstileFailed')
    )
    appStore.showError(errorMessage.value)
  } finally {
    resetCaptchaProof()
    isLoading.value = false
  }
}

// ==================== 2FA Handlers ====================

async function handle2FAVerify(code: string): Promise<void> {
  if (totpModalRef.value) {
    totpModalRef.value.setVerifying(true)
  }

  try {
    await authStore.login2FA(totpTempToken.value, code)

    // Close modal and show success
    show2FAModal.value = false
    clearAllAffiliateReferralCodes()
    appStore.showSuccess(t('auth.loginSuccess'))

    // Redirect to dashboard or intended route
    const redirectTo = sanitizeInternalRedirect(
      router.currentRoute.value.query.redirect,
      '/dashboard'
    )
    await router.push(redirectTo)
  } catch (error: unknown) {
    const err = error as { message?: string; response?: { data?: { message?: string } } }
    const message = extractApiErrorMessage(err, '') || t('profile.totp.loginFailed')

    if (totpModalRef.value) {
      totpModalRef.value.setError(message)
      totpModalRef.value.setVerifying(false)
    }
  }
}

function handle2FACancel(): void {
  show2FAModal.value = false
  totpTempToken.value = ''
  totpUserEmailMasked.value = ''
}
</script>
