<template>
  <form class="space-y-3" @submit.prevent="handleSubmit">
    <input
      v-model="email"
      :data-testid="`${testIdPrefix}-create-account-email`"
      type="email"
      class="input w-full"
      :placeholder="t('auth.emailPlaceholder')"
      :disabled="isSubmitting || isSendingCode"
    />
    <input
      v-model="password"
      :data-testid="`${testIdPrefix}-create-account-password`"
      type="password"
      class="input w-full"
      :placeholder="t('auth.passwordPlaceholder')"
      :disabled="isSubmitting"
    />
    <div v-if="captchaEnabled" class="space-y-2">
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
    <div v-if="emailVerifyEnabled" class="flex flex-wrap gap-3">
      <input
        v-model="verifyCode"
        :data-testid="`${testIdPrefix}-create-account-verify-code`"
        type="text"
        inputmode="numeric"
        maxlength="6"
        class="input min-w-0 flex-1 basis-28 font-mono tabular-nums"
        placeholder="123456"
        :disabled="isSubmitting"
      />
      <button
        :data-testid="`${testIdPrefix}-create-account-send-code`"
        type="button"
        class="btn btn-secondary btn-outline shrink-0"
        :disabled="isSubmitting || isSendingCode || countdown > 0 || !email.trim() || (turnstileEnabled && !turnstileToken)"
        @click="handleSendCode"
      >
        {{
          isSendingCode
            ? t('auth.sendingCode')
            : countdown > 0
              ? t('auth.resendCountdown', { countdown })
              : t('auth.sendCode')
        }}
      </button>
    </div>
    <p v-if="emailVerifyEnabled && sendCodeSuccess" class="text-sm text-green-600 dark:text-green-400">
      {{ t('auth.codeSentSuccess') }}
    </p>
    <p v-else-if="emailVerifyEnabled" class="text-xs text-[var(--muted-foreground)]">
      {{ t('auth.verificationCodeHint') }}
    </p>
    <input
      v-if="invitationCodeEnabled"
      v-model="invitationCode"
      :data-testid="`${testIdPrefix}-create-account-invitation-code`"
      type="text"
      class="input w-full"
      :placeholder="t('auth.invitationCodePlaceholder')"
      :disabled="isSubmitting"
    />
    <LoginAgreementPrompt
      v-if="loginAgreementEnabled"
      :enabled="loginAgreementEnabled"
      :accepted="agreementAccepted"
      :documents="loginAgreementDocuments"
      :mode="loginAgreementMode"
      :updated-at="loginAgreementUpdatedAt"
      :visible="showAgreementModal"
      @accept="acceptAgreement"
      @reject="rejectAgreement"
      @open="showAgreementModal = true"
      @close="showAgreementModal = false"
    />
    <button
      :data-testid="`${testIdPrefix}-create-account-submit`"
      type="button"
      class="btn btn-primary w-full"
      :disabled="isSubmitting || !email.trim() || password.length < 6 || (invitationCodeEnabled && !invitationCode.trim()) || (turnstileEnabled && !turnstileToken) || (loginAgreementEnabled && !agreementAccepted)"
      @click="handleSubmit"
    >
      {{ isSubmitting ? t('common.processing') : t('auth.createAccount') }}
    </button>
    <button
      type="button"
      class="btn btn-secondary btn-outline w-full"
      :disabled="isSubmitting"
      @click="emitSwitchToBind"
    >
      {{ t('auth.alreadyHaveAccount') }}
    </button>
  </form>
</template>

<script setup lang="ts">
import { extractApiErrorMessage } from '@/utils/apiError'

import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import TurnstileWidget from '@/components/CaptchaChallenge.vue'
import LoginAgreementPrompt from '@/components/auth/LoginAgreementPrompt.vue'
import { getPublicSettings, sendPendingOAuthVerifyCode } from '@/api/auth'
import { useAppStore } from '@/stores'
import { buildSignupAgreementPayload, resolveAgreementState } from '@/features/public/legal'
import type { LoginAgreementDocument } from '@/types'

export type PendingOAuthCreateAccountPayload = {
  email: string
  password: string
  verifyCode: string
  turnstileToken?: string
  tencentCaptchaTicket?: string
  tencentCaptchaRandstr?: string
  invitationCode?: string
  loginAgreementAccepted?: boolean
  loginAgreementRevision?: string
}

const props = defineProps<{
  initialEmail: string
  testIdPrefix: string
  isSubmitting: boolean
  errorMessage?: string
}>()

const emit = defineEmits<{
  submit: [payload: PendingOAuthCreateAccountPayload]
  switchToBind: [email: string]
}>()

const { t } = useI18n()
const appStore = useAppStore()

const email = ref('')
const password = ref('')
const verifyCode = ref('')
const invitationCode = ref('')
const isSendingCode = ref(false)
const sendCodeError = ref('')
const sendCodeSuccess = ref(false)
const countdown = ref(0)
const invitationCodeEnabled = ref(false)
const emailVerifyEnabled = ref(true)
const turnstileEnabled = ref(false)
const turnstileSiteKey = ref('')
const tencentCaptchaEnabled = ref(false)
const tencentCaptchaAppId = ref('')
const tencentCaptchaRegion = ref('cn')
const aliyunCaptchaEnabled = ref(false)
const aliyunCaptchaSceneId = ref('')
const aliyunCaptchaPrefix = ref('')
const aliyunCaptchaRegion = ref('cn')
const turnstileToken = ref('')
const tencentCaptchaRandstr = ref('')
const loginAgreementEnabled = ref(false)
const loginAgreementMode = ref<'modal' | 'checkbox' | string>('modal')
const loginAgreementUpdatedAt = ref('')
const loginAgreementRevision = ref('')
const loginAgreementDocuments = ref<LoginAgreementDocument[]>([])
const agreementAccepted = ref(false)
const showAgreementModal = ref(false)
const turnstileRef = ref<InstanceType<typeof TurnstileWidget> | null>(null)
const aliyunCaptchaReady = computed(
  () =>
    aliyunCaptchaEnabled.value &&
    Boolean(aliyunCaptchaSceneId.value) &&
    Boolean(aliyunCaptchaPrefix.value)
)
// 动作触发式验证码（腾讯/阿里云）：发送验证码、提交时弹窗验证
const actionCaptchaEnabled = computed(
  () =>
    (tencentCaptchaEnabled.value && Boolean(tencentCaptchaAppId.value)) ||
    aliyunCaptchaReady.value
)
const captchaEnabled = computed(
  () =>
    (turnstileEnabled.value && Boolean(turnstileSiteKey.value)) || actionCaptchaEnabled.value
)

let countdownTimer: ReturnType<typeof setInterval> | null = null

watch(
  () => props.initialEmail,
  value => {
    email.value = value || ''
  },
  { immediate: true }
)

watch(sendCodeError, value => {
  if (value) {
    appStore.showError(value)
  }
})

watch(
  () => props.errorMessage,
  value => {
    if (value) {
      appStore.showError(value)
      if (captchaEnabled.value) {
        resetTurnstile()
      }
    }
  }
)

function clearCountdown() {
  if (countdownTimer) {
    clearInterval(countdownTimer)
    countdownTimer = null
  }
}

function startCountdown(seconds: number) {
  clearCountdown()
  countdown.value = Math.max(0, seconds)

  if (countdown.value <= 0) {
    return
  }

  countdownTimer = setInterval(() => {
    if (countdown.value <= 1) {
      countdown.value = 0
      clearCountdown()
      return
    }

    countdown.value -= 1
  }, 1000)
}

function getRequestErrorMessage(error: unknown, fallback: string): string {
  const err = error as { message?: string; response?: { data?: { detail?: string; message?: string } } }
  return extractApiErrorMessage(err, '') || fallback
}

function resetTurnstile() {
  turnstileToken.value = ''
  tencentCaptchaRandstr.value = ''
  turnstileRef.value?.reset()
}

function onTurnstileVerify(token: string, randstr = '') {
  turnstileToken.value = token
  tencentCaptchaRandstr.value = randstr
  sendCodeError.value = ''
}

function onTurnstileExpire() {
  turnstileToken.value = ''
  tencentCaptchaRandstr.value = ''
  sendCodeError.value = t('auth.turnstileExpired')
}

function onTurnstileError() {
  turnstileToken.value = ''
  tencentCaptchaRandstr.value = ''
  sendCodeError.value = t('auth.turnstileFailed')
}

async function acquireActionProof(): Promise<boolean> {
  if (!actionCaptchaEnabled.value) return true

  const proof = await turnstileRef.value?.verifyAction()
  if (!proof) return false

  turnstileToken.value = proof.token
  tencentCaptchaRandstr.value = proof.randstr
  return true
}

async function handleSendCode() {
  const trimmedEmail = email.value.trim()
  if (!trimmedEmail) {
    return
  }

  if (turnstileEnabled.value && !turnstileToken.value) {
    sendCodeError.value = t('auth.completeVerification')
    return
  }

  if (!(await acquireActionProof())) {
    return
  }

  isSendingCode.value = true
  sendCodeError.value = ''
  sendCodeSuccess.value = false

  try {
    const response = await sendPendingOAuthVerifyCode({
      email: trimmedEmail,
      turnstile_token:
        turnstileEnabled.value || aliyunCaptchaEnabled.value ? turnstileToken.value : undefined,
      tencent_captcha_ticket: tencentCaptchaEnabled.value ? turnstileToken.value : undefined,
      tencent_captcha_randstr: tencentCaptchaEnabled.value ? tencentCaptchaRandstr.value : undefined
    })
    sendCodeSuccess.value = true
    startCountdown(response.countdown)
  } catch (error: unknown) {
    sendCodeError.value = getRequestErrorMessage(error, t('auth.sendCodeFailed'))
  } finally {
    if (captchaEnabled.value) {
      resetTurnstile()
    }
    isSendingCode.value = false
  }
}

async function handleSubmit() {
  const trimmedEmail = email.value.trim()
  if (!trimmedEmail || password.value.length < 6) {
    return
  }

  // Turnstile 票据一次性：发送验证码已消耗上一枚，reset 后要等新票据回调。
  // 缺票时不能提交——create-account 端点会校验验证码，空 token 直接被判失败。
  // 表单的隐式提交（输入框回车）绕得过按钮的 disabled，所以这里必须再挡一次。
  if (turnstileEnabled.value && !turnstileToken.value) {
    sendCodeError.value = t('auth.completeVerification')
    return
  }

  if (!(await acquireActionProof())) {
    return
  }

  if (loginAgreementEnabled.value && !agreementAccepted.value) {
    if (loginAgreementMode.value !== 'checkbox') {
      showAgreementModal.value = true
    }
    return
  }

  const agreementPayload = buildSignupAgreementPayload(
    loginAgreementEnabled.value,
    agreementAccepted.value,
    loginAgreementRevision.value
  )

  emit('submit', {
    email: trimmedEmail,
    password: password.value,
    verifyCode: emailVerifyEnabled.value ? verifyCode.value.trim() : '',
    ...((turnstileEnabled.value || aliyunCaptchaEnabled.value) && turnstileToken.value
      ? { turnstileToken: turnstileToken.value }
      : {}),
    ...(tencentCaptchaEnabled.value && turnstileToken.value
      ? {
          tencentCaptchaTicket: turnstileToken.value,
          tencentCaptchaRandstr: tencentCaptchaRandstr.value
        }
      : {}),
    invitationCode: invitationCode.value.trim() || undefined,
    ...(agreementPayload.login_agreement_accepted !== undefined
      ? {
          loginAgreementAccepted: agreementPayload.login_agreement_accepted,
          loginAgreementRevision: agreementPayload.login_agreement_revision
        }
      : {})
  })

  if (actionCaptchaEnabled.value) {
    resetTurnstile()
  }
}

function emitSwitchToBind() {
  emit('switchToBind', email.value.trim())
}

function acceptAgreement() {
  agreementAccepted.value = true
  showAgreementModal.value = false
}

function rejectAgreement() {
  agreementAccepted.value = false
  showAgreementModal.value = false
}

onMounted(async () => {
  try {
    const settings = await getPublicSettings()
    invitationCodeEnabled.value = settings.invitation_code_enabled === true
    emailVerifyEnabled.value = settings.email_verify_enabled !== false
    turnstileEnabled.value = settings.turnstile_enabled === true
    turnstileSiteKey.value = settings.turnstile_site_key || ''
    tencentCaptchaEnabled.value = settings.tencent_captcha_enabled === true
    tencentCaptchaAppId.value = settings.tencent_captcha_app_id || ''
    tencentCaptchaRegion.value = settings.tencent_captcha_region || 'cn'
    aliyunCaptchaEnabled.value = settings.aliyun_captcha_enabled === true
    aliyunCaptchaSceneId.value = settings.aliyun_captcha_scene_id || ''
    aliyunCaptchaPrefix.value = settings.aliyun_captcha_prefix || ''
    aliyunCaptchaRegion.value = settings.aliyun_captcha_region || 'cn'
    const agreement = resolveAgreementState(settings)
    loginAgreementDocuments.value = agreement.documents
    loginAgreementEnabled.value = agreement.enabled
    loginAgreementMode.value = agreement.mode
    loginAgreementUpdatedAt.value = agreement.updatedAt
    loginAgreementRevision.value = agreement.revision
    agreementAccepted.value = !agreement.enabled
    showAgreementModal.value = agreement.enabled && agreement.mode !== 'checkbox'
  } catch {
    invitationCodeEnabled.value = false
    emailVerifyEnabled.value = true
    turnstileEnabled.value = false
    turnstileSiteKey.value = ''
    tencentCaptchaEnabled.value = false
    tencentCaptchaAppId.value = ''
    tencentCaptchaRegion.value = 'cn'
    aliyunCaptchaEnabled.value = false
    aliyunCaptchaSceneId.value = ''
    aliyunCaptchaPrefix.value = ''
    aliyunCaptchaRegion.value = 'cn'
  }
})

onUnmounted(() => {
  clearCountdown()
})
</script>

<style scoped>
:is(input, select, textarea).input {
  min-height: 40px;
  min-width: 0;
  border-radius: 8px;
  border-color: var(--field-border, var(--input));
  background: var(--card);
  color: var(--foreground);
  box-shadow: none;
  transition: border-color 150ms ease, background-color 150ms ease;
}

:is(input, select, textarea).input::placeholder { color: var(--muted-foreground); }
:is(input, select, textarea).input:disabled { background: var(--surface-2); }
:is(input, select, textarea).input:focus {
  border-color: var(--field-border, var(--brand));
  outline: 2px solid color-mix(in srgb, var(--field-border, var(--brand)) 22%, transparent);
  outline-offset: 2px;
  box-shadow: none;
}

.input-error { --field-border: var(--bad); }
label.input-label { color: var(--foreground); }
p.input-hint { color: var(--muted-foreground); }

:is(button, a).btn {
  min-height: 40px;
  border-radius: 8px;
  background-image: none;
  box-shadow: none;
  transform: none;
  transition: background-color 150ms ease, border-color 150ms ease;
}

.btn.btn-primary { background: var(--primary); color: var(--primary-foreground); }
.btn.btn-primary:hover:not(:disabled) { background: color-mix(in srgb, var(--primary) 90%, var(--card)); }
.btn.btn-outline { border-color: var(--border); background: var(--card); color: var(--foreground); }
.btn.btn-outline:hover:not(:disabled) { background: var(--surface-2); }

button:focus-visible, a:focus-visible, summary:focus-visible {
  outline: 2px solid var(--brand);
  outline-offset: 3px;
}

input[type='checkbox'] { accent-color: var(--brand); }


.fade-enter-active,
.fade-leave-active {
  transition: all 0.3s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
@media (prefers-reduced-motion: reduce) {
  :is(button, a).btn, :is(input, select, textarea).input, .fade-enter-active, .fade-leave-active,
  .agreement-fade-enter-active, .agreement-fade-leave-active,
  .agreement-fade-enter-active > div, .agreement-fade-leave-active > div {
    transition: none;
  }
}
</style>
