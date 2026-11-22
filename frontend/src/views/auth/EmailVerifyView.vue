<template>
  <AuthLayout>
    <div class="space-y-4">
      <!-- Title -->
      <div>
        <h1 class="auth-title">
          {{ t('auth.verifyYourEmail') }}
        </h1>
        <p class="auth-intro">
          {{ t('auth.sendCodeDesc') }}
        </p>
      </div>
      <div v-if="hasRegisterData" class="auth-summary">
        <p>{{ t('auth.emailLabel') }}：<strong>{{ email }}</strong></p>
        <p v-if="username">{{ t('profile.username') }}：{{ username }}</p>
        <p v-if="invitationCode">{{ t('auth.invitationCodeLabel') }}：{{ invitationCode }}</p>
        <p v-if="promoCode">{{ t('auth.promoCodeLabel') }}：{{ promoCode }}</p>
        <p v-if="affCode">推荐码：{{ affCode }}</p>
      </div>

      <!-- No Data Warning -->
      <div
        v-if="!hasRegisterData"
        class="auth-summary"
      >
        <div class="flex items-start gap-3">
          <div class="flex-shrink-0">
            <Icon name="exclamationCircle" size="md" class="text-amber-500" />
          </div>
          <div class="text-sm text-amber-700 dark:text-amber-400">
            <p class="font-medium">{{ t('auth.sessionExpired') }}</p>
            <p class="mt-1">{{ t('auth.sessionExpiredDesc') }}</p>
          </div>
        </div>
      </div>

      <!-- Verification Form -->
      <form v-else @submit.prevent="handleVerify" class="space-y-3">
        <!-- Verification Code Input -->
        <div>
          <label for="code" class="input-label">
            {{ t('auth.verificationCode') }}
          </label>
          <input
            id="code"
            v-model="verifyCode"
            type="text"
            required
            autocomplete="one-time-code"
            inputmode="numeric"
            maxlength="6"
            :disabled="isLoading"
            class="input font-mono tabular-nums tracking-[0.3em]"
            :class="{ 'input-error': errors.code }"
            placeholder="000000"
          />
          <p class="input-hint">{{ t('auth.verificationCodeHint') }}</p>
        </div>

        <!-- Code Status -->
        <div
          v-if="codeSent"
          class="auth-summary"
        >
          <div class="flex items-start gap-3">
            <div class="flex-shrink-0">
              <Icon name="checkCircle" size="md" class="text-green-500" />
            </div>
            <p class="text-sm text-green-700 dark:text-green-400">
              {{ t('auth.codeSentSuccess') }}
            </p>
          </div>
        </div>

        <!-- Turnstile Widget for Resend -->
        <div v-if="actionCaptchaEnabled || (turnstileEnabled && showResendTurnstile)">
          <TurnstileWidget
            ref="turnstileRef"
            :site-key="turnstileSiteKey"
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

        <div v-if="pendingOAuthCreateCaptchaEnabled" class="space-y-2">
          <TurnstileWidget
            ref="createAccountTurnstileRef"
            :site-key="turnstileSiteKey"
            :turnstile-enabled="turnstileEnabled"
            :turnstile-site-key="turnstileSiteKey"
            :tencent-enabled="tencentCaptchaEnabled"
            :tencent-app-id="tencentCaptchaAppId"
            :tencent-region="tencentCaptchaRegion"
            :aliyun-enabled="aliyunCaptchaEnabled"
            :aliyun-scene-id="aliyunCaptchaSceneId"
            :aliyun-prefix="aliyunCaptchaPrefix"
            :aliyun-region="aliyunCaptchaRegion"
            @verify="onCreateAccountTurnstileVerify"
            @expire="onCreateAccountTurnstileExpire"
            @error="onCreateAccountTurnstileError"
          />
        </div>

        <LoginAgreementPrompt
          v-if="agreementRefreshRequired && currentAgreementEnabled"
          :enabled="currentAgreementEnabled"
          :accepted="currentAgreementAccepted"
          :documents="currentAgreementDocuments"
          :mode="currentAgreementMode"
          :updated-at="currentAgreementUpdatedAt"
          :visible="showAgreementModal"
          @accept="acceptRefreshedAgreement"
          @reject="rejectRefreshedAgreement"
          @open="showAgreementModal = true"
          @close="showAgreementModal = false"
        />

        <!-- Submit Button -->
        <div class="auth-actions">
        <button
          type="submit"
          :disabled="isLoading || !verifyCode || (pendingOAuthCreateTurnstileRequired && !createAccountTurnstileToken) || (agreementRefreshRequired && currentAgreementEnabled && !currentAgreementAccepted && currentAgreementMode !== 'checkbox')"
          class="btn btn-primary w-full"
        >
          <svg
            v-if="isLoading"
            class="-ml-1 mr-2 h-4 w-4 animate-spin text-current"
            fill="none"
            viewBox="0 0 24 24"
          >
            <circle
              class="opacity-25"
              cx="12"
              cy="12"
              r="10"
              stroke="currentColor"
              stroke-width="4"
            ></circle>
            <path
              class="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
            ></path>
          </svg>
          <Icon v-else name="checkCircle" size="md" class="mr-2" />
          {{ isLoading ? t('auth.verifying') : t('auth.verifyAndCreate') }}
        </button>

        <!-- Resend Code -->
        <div>
          <button
            v-if="countdown > 0"
            type="button"
            disabled
            class="btn btn-outline cursor-not-allowed"
          >
            {{ t('auth.resendCountdown', { countdown }) }}
          </button>
          <button
            v-else
            type="button"
            @click="handleResendCode"
            :disabled="
              isSendingCode || (turnstileActive && showResendTurnstile && !resendTurnstileToken)
            "
            class="btn btn-outline disabled:cursor-not-allowed disabled:opacity-50"
          >
            <span v-if="isSendingCode">{{ t('auth.sendingCode') }}</span>
            <span v-else-if="captchaEnabled && !showResendTurnstile">
              {{ t('auth.clickToResend') }}
            </span>
            <span v-else>{{ t('auth.resendCode') }}</span>
          </button>
        </div>
        </div>
      </form>
    </div>

    <!-- Footer -->
    <template #footer>
      <button
        @click="handleBack"
        class="btn btn-outline w-full"
      >
        <Icon name="arrowLeft" size="sm" />
        {{ t('auth.backToRegistration') }}
      </button>
    </template>
  </AuthLayout>
</template>

<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { AuthLayout } from '@/components/layout'
import Icon from '@/components/icons/Icon.vue'
import TurnstileWidget from '@/components/CaptchaChallenge.vue'
import LoginAgreementPrompt from '@/components/auth/LoginAgreementPrompt.vue'
import { resolveActiveCaptchaProvider } from '@/components/auth/captchaProvider'
import { resolveAgreementState } from '@/features/public/legal'
import { sanitizeInternalRedirect } from '@/router/redirect'
import { useAuthStore, useAppStore } from '@/stores'
import type { LoginAgreementDocument } from '@/types'
import {
  persistOAuthTokenContext,
  getPublicSettings,
  isOAuthLoginCompletion,
  type PendingOAuthSendVerifyCodeResponse,
  sendPendingOAuthVerifyCode,
  sendVerifyCode,
} from '@/api/auth'
import { apiClient } from '@/api/client'
import { buildAuthErrorMessage } from '@/utils/authError'
import { extractApiErrorCode } from '@/utils/apiError'
import {
  formatRegistrationEmailSuffixWhitelistForMessage,
  isRegistrationEmailSuffixAllowed,
  normalizeRegistrationEmailSuffixWhitelist
} from '@/utils/registrationEmailPolicy'
import {
  clearAllAffiliateReferralCodes,
  loadAffiliateReferralCode,
  oauthAffiliatePayload
} from '@/utils/oauthAffiliate'
import { authStorage } from '@/api/tokenRefresh'

const { t, locale } = useI18n()

// ==================== Router & Stores ====================

const router = useRouter()
const authStore = useAuthStore()
const appStore = useAppStore()

// ==================== State ====================

const isLoading = ref<boolean>(false)
const isSendingCode = ref<boolean>(false)
const errorMessage = ref<string>('')
const codeSent = ref<boolean>(false)
const verifyCode = ref<string>('')
const countdown = ref<number>(0)
let countdownTimer: ReturnType<typeof setInterval> | null = null

// Registration data from sessionStorage
type PendingAuthTokenField = 'pending_auth_token' | 'pending_oauth_token'
type PendingAuthSessionSummary = {
  token: string
  token_field: PendingAuthTokenField
  provider: string
  redirect?: string
}
type PendingOAuthCreateAccountResponse = {
  auth_result?: string
  access_token: string
  refresh_token?: string
  expires_in?: number
  token_type?: string
  provider?: string
  redirect?: string
}

const email = ref<string>('')
const username = ref('')
const registrationRedirect = ref('/dashboard')
const password = ref<string>('')
const initialTurnstileToken = ref<string>('')
const initialTencentCaptchaRandstr = ref<string>('')
const promoCode = ref<string>('')
const invitationCode = ref<string>('')
const affCode = ref<string>('')
const loginAgreementAccepted = ref<boolean | undefined>(undefined)
const loginAgreementRevision = ref<string | undefined>(undefined)
const agreementRefreshRequired = ref<boolean>(false)
const currentAgreementEnabled = ref<boolean>(false)
const currentAgreementMode = ref<'modal' | 'checkbox' | string>('modal')
const currentAgreementUpdatedAt = ref<string>('')
const currentAgreementRevision = ref<string>('')
const currentAgreementDocuments = ref<LoginAgreementDocument[]>([])
const currentAgreementAccepted = ref<boolean>(false)
const showAgreementModal = ref<boolean>(false)
const pendingAuthToken = ref<string>('')
const pendingAuthTokenField = ref<PendingAuthTokenField>('pending_auth_token')
const pendingProvider = ref<string>('')
const pendingRedirect = ref<string>('')
const pendingAdoptionDecision = ref<{
  adoptDisplayName?: boolean
  adoptAvatar?: boolean
} | null>(null)
const hasRegisterData = ref<boolean>(false)

// Public settings
const turnstileEnabled = ref<boolean>(false)
const turnstileSiteKey = ref<string>('')
const tencentCaptchaEnabled = ref<boolean>(false)
const tencentCaptchaAppId = ref<string>('')
const tencentCaptchaRegion = ref<string>('cn')
const aliyunCaptchaEnabled = ref<boolean>(false)
const aliyunCaptchaSceneId = ref<string>('')
const aliyunCaptchaPrefix = ref<string>('')
const aliyunCaptchaRegion = ref<string>('cn')
const siteName = ref<string>('OmniHive')
const registrationEmailSuffixWhitelist = ref<string[]>([])
// 域名限量注册开关：开启时非白名单域名可注册 1 个账户（由后端判定），前端不做白名单预检。
const emailDomainQuotaEnabled = ref<boolean>(false)

// Turnstile for resend
const turnstileRef = ref<InstanceType<typeof TurnstileWidget> | null>(null)
const createAccountTurnstileRef = ref<InstanceType<typeof TurnstileWidget> | null>(null)
const resendTurnstileToken = ref<string>('')
const resendTencentCaptchaRandstr = ref<string>('')
const createAccountTurnstileToken = ref<string>('')
const createAccountTencentCaptchaRandstr = ref<string>('')
const showResendTurnstile = ref<boolean>(false)
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
// 动作触发式验证码（腾讯/阿里云）：重发验证码、创建账号时弹窗验证
const actionCaptchaEnabled = computed(() => tencentActive.value || aliyunActive.value)
const captchaEnabled = computed(() => activeCaptchaProvider.value !== 'none')

const errors = ref({
  code: '',
  turnstile: ''
})

const validationToastMessage = computed(
  () => errors.value.code || errors.value.turnstile || ''
)
const pendingOAuthCreateTurnstileRequired = computed(
  () => isPendingOAuthFlow() && turnstileActive.value
)
const pendingOAuthCreateCaptchaEnabled = computed(
  () => isPendingOAuthFlow() && captchaEnabled.value
)

watch(validationToastMessage, (value, previousValue) => {
  if (value && value !== previousValue) {
    appStore.showError(value)
  }
})

// ==================== Lifecycle ====================

onMounted(async () => {
  const activePendingSession = authStore.pendingAuthSession as PendingAuthSessionSummary | null

  // Load registration data from sessionStorage
  const registerDataStr = authStorage.registerDraft.get()
  if (registerDataStr) {
    try {
      const registerData = JSON.parse(registerDataStr)
      email.value = registerData.email || ''
      username.value = typeof registerData.username === 'string' ? registerData.username : ''
      registrationRedirect.value = sanitizeInternalRedirect(registerData.redirect, '/dashboard')
      password.value = registerData.password || ''
      initialTurnstileToken.value =
        registerData.tencent_captcha_ticket || registerData.turnstile_token || ''
      initialTencentCaptchaRandstr.value = registerData.tencent_captcha_randstr || ''
      promoCode.value = registerData.promo_code || ''
      invitationCode.value = registerData.invitation_code || ''
      affCode.value = registerData.aff_code || loadAffiliateReferralCode()
      loginAgreementAccepted.value =
        typeof registerData.login_agreement_accepted === 'boolean'
          ? registerData.login_agreement_accepted
          : undefined
      loginAgreementRevision.value =
        typeof registerData.login_agreement_revision === 'string'
          ? registerData.login_agreement_revision
          : undefined
      pendingAuthToken.value = registerData.pending_auth_token || activePendingSession?.token || ''
      pendingAuthTokenField.value = registerData.pending_auth_token_field || activePendingSession?.token_field || 'pending_auth_token'
      pendingProvider.value = registerData.pending_provider || activePendingSession?.provider || ''
      const rawPendingRedirect = registerData.pending_redirect || activePendingSession?.redirect || ''
      pendingRedirect.value = rawPendingRedirect
        ? sanitizeInternalRedirect(rawPendingRedirect, '/dashboard')
        : ''
      pendingAdoptionDecision.value = registerData.pending_adoption_decision
        ? {
            adoptDisplayName: registerData.pending_adoption_decision.adopt_display_name === true,
            adoptAvatar: registerData.pending_adoption_decision.adopt_avatar === true
          }
        : null
      hasRegisterData.value = !!(email.value && password.value)
    } catch {
      hasRegisterData.value = false
    }
  } else if (activePendingSession) {
    pendingAuthToken.value = activePendingSession.token
    pendingAuthTokenField.value = activePendingSession.token_field
    pendingProvider.value = activePendingSession.provider
    pendingRedirect.value = activePendingSession.redirect
      ? sanitizeInternalRedirect(activePendingSession.redirect, '/dashboard')
      : ''
  }

  // Load public settings
  try {
    const settings = await getPublicSettings()
    turnstileEnabled.value = settings.turnstile_enabled
    turnstileSiteKey.value = settings.turnstile_site_key || ''
    tencentCaptchaEnabled.value = settings.tencent_captcha_enabled === true
    tencentCaptchaAppId.value = settings.tencent_captcha_app_id || ''
    tencentCaptchaRegion.value = settings.tencent_captcha_region || 'cn'
    aliyunCaptchaEnabled.value = settings.aliyun_captcha_enabled === true
    aliyunCaptchaSceneId.value = settings.aliyun_captcha_scene_id || ''
    aliyunCaptchaPrefix.value = settings.aliyun_captcha_prefix || ''
    aliyunCaptchaRegion.value = settings.aliyun_captcha_region || 'cn'
    siteName.value = settings.site_name || 'OmniHive'
    registrationEmailSuffixWhitelist.value = normalizeRegistrationEmailSuffixWhitelist(
      settings.registration_email_suffix_whitelist || []
    )
    emailDomainQuotaEnabled.value = settings.registration_email_domain_quota_enabled === true
  } catch (error) {
    console.error('Failed to load public settings:', error)
  }

  // Auto-send verification code if we have valid data
  if (hasRegisterData.value) {
    await sendCode()
  }
})

onUnmounted(() => {
  if (countdownTimer) {
    clearInterval(countdownTimer)
    countdownTimer = null
  }
})

// ==================== Countdown ====================

function startCountdown(seconds: number): void {
  countdown.value = seconds

  if (countdownTimer) {
    clearInterval(countdownTimer)
  }

  countdownTimer = setInterval(() => {
    if (countdown.value > 0) {
      countdown.value--
    } else {
      if (countdownTimer) {
        clearInterval(countdownTimer)
        countdownTimer = null
      }
    }
  }, 1000)
}

// ==================== Turnstile Handlers ====================

function onTurnstileVerify(token: string, randstr = ''): void {
  resendTurnstileToken.value = token
  resendTencentCaptchaRandstr.value = randstr
  errors.value.turnstile = ''
}

function onTurnstileExpire(): void {
  resendTurnstileToken.value = ''
  resendTencentCaptchaRandstr.value = ''
  errors.value.turnstile = t('auth.turnstileExpired')
}

function onTurnstileError(): void {
  resendTurnstileToken.value = ''
  resendTencentCaptchaRandstr.value = ''
  errors.value.turnstile = t('auth.turnstileFailed')
}

function onCreateAccountTurnstileVerify(token: string, randstr = ''): void {
  createAccountTurnstileToken.value = token
  createAccountTencentCaptchaRandstr.value = randstr
  errors.value.turnstile = ''
}

function onCreateAccountTurnstileExpire(): void {
  createAccountTurnstileToken.value = ''
  createAccountTencentCaptchaRandstr.value = ''
  errors.value.turnstile = t('auth.turnstileExpired')
}

function onCreateAccountTurnstileError(): void {
  createAccountTurnstileToken.value = ''
  createAccountTencentCaptchaRandstr.value = ''
  errors.value.turnstile = t('auth.turnstileFailed')
}

function resetCreateAccountTurnstile(): void {
  createAccountTurnstileToken.value = ''
  createAccountTencentCaptchaRandstr.value = ''
  createAccountTurnstileRef.value?.reset()
}

async function acquireResendActionProof(): Promise<boolean> {
  if (!actionCaptchaEnabled.value) return true

  const proof = await turnstileRef.value?.verifyAction()
  if (!proof) return false

  resendTurnstileToken.value = proof.token
  resendTencentCaptchaRandstr.value = proof.randstr
  return true
}

async function acquireCreateAccountActionProof(): Promise<boolean> {
  if (!isPendingOAuthFlow() || !actionCaptchaEnabled.value) return true

  const proof = await createAccountTurnstileRef.value?.verifyAction()
  if (!proof) return false

  createAccountTurnstileToken.value = proof.token
  createAccountTencentCaptchaRandstr.value = proof.randstr
  return true
}

function isPendingOAuthFlow(): boolean {
  return Boolean(pendingProvider.value.trim())
}

// 域名限量注册开启时交由后端按额度判定；pending OAuth / 换绑流程沿用后端策略，前端不预检。
function shouldBypassRegistrationEmailPolicy(): boolean {
  return (
    emailDomainQuotaEnabled.value || isPendingOAuthFlow() || Boolean(pendingAuthToken.value.trim())
  )
}

function resolvePendingOAuthCallbackRoute(provider: string): string {
  switch (provider.trim().toLowerCase()) {
    case 'linuxdo':
      return '/auth/linuxdo/callback'
    case 'oidc':
      return '/auth/oidc/callback'
    case 'wechat':
      return '/auth/wechat/callback'
    default:
      return '/auth/callback'
  }
}

function isPendingOAuthSessionResponse(data: PendingOAuthCreateAccountResponse): boolean {
  return data.auth_result === 'pending_session'
}

function getPendingOAuthSendCodeSessionResponse(
  data: PendingOAuthSendVerifyCodeResponse,
): PendingOAuthSendVerifyCodeResponse | null {
  return data.auth_result === 'pending_session' ? data : null
}

function persistPendingOAuthSession(provider: string, redirect?: string): void {
  const safeRedirect = redirect
    ? sanitizeInternalRedirect(redirect, '/dashboard')
    : pendingRedirect.value || undefined
  authStore.setPendingAuthSession({
    token: pendingAuthToken.value,
    token_field: pendingAuthTokenField.value,
    provider: provider.trim() || pendingProvider.value.trim(),
    redirect: safeRedirect,
  })
}

async function refreshAgreementForRecovery(): Promise<void> {
  agreementRefreshRequired.value = true
  currentAgreementAccepted.value = false
  try {
    const settings = await getPublicSettings()
    const resolved = resolveAgreementState(settings)
    currentAgreementDocuments.value = resolved.documents
    currentAgreementEnabled.value = resolved.enabled
    currentAgreementMode.value = resolved.mode
    currentAgreementUpdatedAt.value = resolved.updatedAt
    currentAgreementRevision.value = resolved.revision
    if (!resolved.enabled) {
      agreementRefreshRequired.value = false
      currentAgreementAccepted.value = true
      loginAgreementAccepted.value = undefined
      loginAgreementRevision.value = undefined
    } else {
      showAgreementModal.value = resolved.mode !== 'checkbox'
    }
  } catch (error) {
    console.error('Failed to refresh agreement settings:', error)
  }
}

function acceptRefreshedAgreement(): void {
  currentAgreementAccepted.value = true
  showAgreementModal.value = false
  loginAgreementAccepted.value = true
  loginAgreementRevision.value = currentAgreementRevision.value
  const registerDataStr = authStorage.registerDraft.get()
  if (registerDataStr) {
    try {
      const registerData = JSON.parse(registerDataStr) as Record<string, unknown>
      registerData.login_agreement_accepted = true
      registerData.login_agreement_revision = currentAgreementRevision.value
      authStorage.registerDraft.setJSON(registerData)
    } catch {
      // Ignore malformed draft payload.
    }
  }
}

function rejectRefreshedAgreement(): void {
  currentAgreementAccepted.value = false
  showAgreementModal.value = false
  loginAgreementAccepted.value = false
}

// ==================== Send Code ====================

async function sendCode(): Promise<void> {
  isSendingCode.value = true
  errorMessage.value = ''
  let requestSucceeded = false
  let captchaProofUsed = false

  try {
    if (!shouldBypassRegistrationEmailPolicy() && !isRegistrationEmailSuffixAllowed(email.value, registrationEmailSuffixWhitelist.value)) {
      errorMessage.value = buildEmailSuffixNotAllowedMessage()
      appStore.showError(errorMessage.value)
      return
    }

    const requestPayload = {
      email: email.value,
      [pendingAuthTokenField.value]: pendingAuthToken.value || undefined,
      // 优先使用重发时新获取的 token（因为初始 token 可能已被使用）
      turnstile_token:
        turnstileActive.value || aliyunActive.value
          ? resendTurnstileToken.value || initialTurnstileToken.value || undefined
          : undefined,
      tencent_captcha_ticket: tencentActive.value
        ? resendTurnstileToken.value || initialTurnstileToken.value || undefined
        : undefined,
      tencent_captcha_randstr: tencentActive.value
        ? resendTencentCaptchaRandstr.value || initialTencentCaptchaRandstr.value || undefined
        : undefined
    } as Parameters<typeof sendVerifyCode>[0]
    captchaProofUsed = Boolean(
      requestPayload.turnstile_token || requestPayload.tencent_captcha_ticket
    )
    const response = isPendingOAuthFlow()
      ? await sendPendingOAuthVerifyCode(requestPayload)
      : await sendVerifyCode(requestPayload)
    requestSucceeded = true

    const pendingSendCodeSession = isPendingOAuthFlow()
      ? getPendingOAuthSendCodeSessionResponse(response as PendingOAuthSendVerifyCodeResponse)
      : null
    if (pendingSendCodeSession) {
      authStorage.registerDraft.remove()
      persistPendingOAuthSession(
        pendingSendCodeSession.provider || pendingProvider.value,
        pendingSendCodeSession.redirect,
      )
      await router.push(
        resolvePendingOAuthCallbackRoute(pendingSendCodeSession.provider || pendingProvider.value),
      )
      return
    }

    codeSent.value = true
    startCountdown(response.countdown)

    showResendTurnstile.value = false
  } catch (error: unknown) {
    errorMessage.value = buildRegistrationErrorMessage(error, t('auth.sendCodeFailed'))

    appStore.showError(errorMessage.value)
  } finally {
    if (captchaProofUsed) {
      clearStoredCaptchaProof()
      initialTurnstileToken.value = ''
      initialTencentCaptchaRandstr.value = ''
      resendTurnstileToken.value = ''
      resendTencentCaptchaRandstr.value = ''
      turnstileRef.value?.reset()
      if (!requestSucceeded && turnstileActive.value) {
        showResendTurnstile.value = true
      }
    }
    isSendingCode.value = false
  }
}

function clearStoredCaptchaProof(): void {
  const registerDataStr = authStorage.registerDraft.get()
  if (!registerDataStr) return

  try {
    const registerData = JSON.parse(registerDataStr) as Record<string, unknown>
    delete registerData.turnstile_token
    delete registerData.tencent_captcha_ticket
    delete registerData.tencent_captcha_randstr
    authStorage.registerDraft.setJSON(registerData)
  } catch {
    // Invalid registration state is handled by the existing onMounted parser.
  }
}

// ==================== Handlers ====================

async function handleResendCode(): Promise<void> {
  // Turnstile stays staged; Tencent is acquired from this action.
  if (turnstileActive.value && !showResendTurnstile.value) {
    showResendTurnstile.value = true
    return
  }

  if (turnstileActive.value && !resendTurnstileToken.value) {
    errors.value.turnstile = t('auth.completeVerification')
    return
  }

  if (!(await acquireResendActionProof())) {
    return
  }

  await sendCode()
}

function validateForm(): boolean {
  errors.value.code = ''

  if (!verifyCode.value.trim()) {
    errors.value.code = t('auth.codeRequired')
    return false
  }

  if (!/^\d{6}$/.test(verifyCode.value.trim())) {
    errors.value.code = t('auth.invalidCode')
    return false
  }

  return true
}

async function handleVerify(): Promise<void> {
  errorMessage.value = ''

  if (agreementRefreshRequired.value && currentAgreementEnabled.value && !currentAgreementAccepted.value) {
    errorMessage.value = t('publicPortal.legal.agreementRequiredRefresh')
    if (typeof appStore.showWarning === 'function') {
      appStore.showWarning(errorMessage.value)
    } else {
      appStore.showError(errorMessage.value)
    }
    if (currentAgreementMode.value !== 'checkbox') {
      showAgreementModal.value = true
    }
    return
  }

  if (!validateForm()) {
    return
  }

  if (!shouldBypassRegistrationEmailPolicy() && !isRegistrationEmailSuffixAllowed(email.value, registrationEmailSuffixWhitelist.value)) {
    errorMessage.value = buildEmailSuffixNotAllowedMessage()
    appStore.showError(errorMessage.value)
    return
  }

  if (!(await acquireCreateAccountActionProof())) {
    return
  }

  isLoading.value = true

  try {
    if (isPendingOAuthFlow()) {
      const payload: Record<string, unknown> = {
        email: email.value,
        password: password.value,
        verify_code: verifyCode.value.trim(),
        ...((turnstileActive.value || aliyunActive.value) &&
        createAccountTurnstileToken.value
          ? { turnstile_token: createAccountTurnstileToken.value }
          : {}),
        ...(tencentActive.value && createAccountTurnstileToken.value
          ? {
              tencent_captcha_ticket: createAccountTurnstileToken.value,
              tencent_captcha_randstr: createAccountTencentCaptchaRandstr.value
          }
          : {}),
        ...oauthAffiliatePayload(affCode.value || loadAffiliateReferralCode()),
      }
      if (loginAgreementAccepted.value !== undefined) {
        payload.login_agreement_accepted = loginAgreementAccepted.value
      }
      if (loginAgreementRevision.value !== undefined) {
        payload.login_agreement_revision = loginAgreementRevision.value
      }
      if (invitationCode.value) {
        payload.invitation_code = invitationCode.value
      }
      if (pendingAdoptionDecision.value?.adoptDisplayName !== undefined) {
        payload.adopt_display_name = pendingAdoptionDecision.value.adoptDisplayName
      }
      if (pendingAdoptionDecision.value?.adoptAvatar !== undefined) {
        payload.adopt_avatar = pendingAdoptionDecision.value.adoptAvatar
      }

      const { data } = await apiClient.post<PendingOAuthCreateAccountResponse>(
        '/auth/oauth/pending/create-account',
        payload
      )
      if (isPendingOAuthSessionResponse(data)) {
        authStorage.registerDraft.remove()
        persistPendingOAuthSession(data.provider || pendingProvider.value, data.redirect)
        await router.push(resolvePendingOAuthCallbackRoute(data.provider || pendingProvider.value))
        return
      }
      if (!isOAuthLoginCompletion(data)) {
        throw new Error(t('auth.verifyFailed'))
      }

      persistOAuthTokenContext(data)
      await authStore.setToken(data.access_token)
      authStore.clearPendingAuthSession?.()
    } else {
      // Register with verification code
      await authStore.register({
        email: email.value,
        ...(username.value.trim() ? { username: username.value.trim() } : {}),
        password: password.value,
        verify_code: verifyCode.value.trim(),
        turnstile_token:
          turnstileActive.value || aliyunActive.value
            ? initialTurnstileToken.value || undefined
            : undefined,
        tencent_captcha_ticket: tencentActive.value ? initialTurnstileToken.value || undefined : undefined,
        tencent_captcha_randstr: tencentActive.value ? initialTencentCaptchaRandstr.value || undefined : undefined,
        promo_code: promoCode.value || undefined,
        invitation_code: invitationCode.value || undefined,
        ...(affCode.value ? { aff_code: affCode.value } : {}),
        ...(loginAgreementAccepted.value !== undefined
          ? {
              login_agreement_accepted: loginAgreementAccepted.value,
              login_agreement_revision: loginAgreementRevision.value || ''
            }
          : {})
      })
    }

    // Clear session data
    authStorage.registerDraft.remove()
    clearAllAffiliateReferralCodes()

    // Show success toast
    appStore.showSuccess(t('auth.accountCreatedSuccess', { siteName: siteName.value }))

    // Redirect to dashboard
    await router.push(
      sanitizeInternalRedirect(pendingRedirect.value || registrationRedirect.value, '/dashboard')
    )
  } catch (error: unknown) {
    if (extractApiErrorCode(error) === 'LOGIN_AGREEMENT_REQUIRED') {
      await refreshAgreementForRecovery()
      errorMessage.value = t('publicPortal.legal.agreementRequiredRefresh')
      if (typeof appStore.showWarning === 'function') {
        appStore.showWarning(errorMessage.value)
      } else {
        appStore.showError(errorMessage.value)
      }
      return
    }
    errorMessage.value = buildRegistrationErrorMessage(error, t('auth.verifyFailed'))

    appStore.showError(errorMessage.value)
  } finally {
    initialTurnstileToken.value = ''
    initialTencentCaptchaRandstr.value = ''
    if (pendingOAuthCreateCaptchaEnabled.value) {
      resetCreateAccountTurnstile()
    }
    isLoading.value = false
  }
}

function handleBack(): void {
  // Clear session data
  authStorage.registerDraft.remove()

  // Go back to registration
  router.push('/register')
}

function buildEmailSuffixNotAllowedMessage(): string {
  const normalizedWhitelist = normalizeRegistrationEmailSuffixWhitelist(
    registrationEmailSuffixWhitelist.value
  )
  if (normalizedWhitelist.length === 0) {
    return t('auth.emailSuffixNotAllowed')
  }
  const separator = String(locale.value || '').toLowerCase().startsWith('zh') ? '、' : ', '
  return t('auth.emailSuffixNotAllowedWithAllowed', {
    suffixes: formatRegistrationEmailSuffixWhitelistForMessage(normalizedWhitelist, {
      separator,
      more: (count) => t('auth.emailSuffixAllowedMore', { count })
    })
  })
}

function buildRegistrationErrorMessage(error: unknown, fallback: string): string {
  if (extractApiErrorCode(error) === 'EMAIL_DOMAIN_REGISTRATION_LIMIT') {
    return t('auth.emailDomainRegistrationLimit')
  }
  return buildAuthErrorMessage(error, { fallback })
}
</script>
