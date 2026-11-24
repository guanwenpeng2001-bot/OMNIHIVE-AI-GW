<template>
  <AuthLayout>
    <div class="space-y-4">
      <!-- Title -->
      <div>
        <h1 class="auth-title">
          {{ t('auth.forgotPasswordTitle') }}
        </h1>
        <p class="auth-intro">
          {{ t('auth.forgotPasswordHint') }}
        </p>
      </div>

      <!-- Success State -->
      <div v-if="isSubmitted" class="space-y-6">
        <div class="auth-summary" role="status">
          <div class="flex items-start gap-3">
            <div class="flex h-10 w-10 items-center justify-center text-[var(--ok-text)]">
              <Icon name="checkCircle" size="lg" class="text-green-600 dark:text-green-400" />
            </div>
            <div>
              <h3 class="text-lg font-semibold text-[var(--foreground)]">
                {{ t('auth.resetEmailSent') }}
              </h3>
              <p class="mt-2 text-sm text-[var(--muted-foreground)]">
                {{ t('auth.resetEmailSentHint') }}
              </p>
              <p class="mt-2 break-all">{{ formData.email }}</p>
            </div>
          </div>
        </div>

        <div class="auth-actions">
          <button type="button" class="btn btn-outline" @click="isSubmitted = false">{{ t('auth.sendAnotherEmail') }}</button>
          <router-link
            to="/login"
            class="btn btn-primary"
          >
            <Icon name="arrowLeft" size="sm" />
            {{ t('auth.backToLogin') }}
          </router-link>
        </div>
      </div>

      <div
        v-else-if="publicSettingsError"
        data-testid="forgot-password-settings-error"
        role="alert"
        class="auth-summary flex items-center justify-between gap-3 border border-red-200 bg-red-50/80 p-3 text-sm text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200"
      >
        <span>{{ t('publicPortal.legal.settingsLoadFailedHint') }}</span>
        <button
          type="button"
          class="btn btn-outline shrink-0 text-xs"
          :disabled="isLoading"
          @click="loadPublicSettings"
        >
          {{ t('publicPortal.legal.retry') }}
        </button>
      </div>

      <div
        v-else-if="settingsLoaded && !passwordResetEnabled"
        data-testid="forgot-password-disabled"
        class="space-y-4"
      >
        <div class="auth-summary">
          <div class="flex items-start gap-3">
            <div class="flex-shrink-0">
              <Icon name="exclamationCircle" size="md" class="text-amber-500" />
            </div>
            <p class="text-sm text-amber-700 dark:text-amber-400">
              {{ passwordResetDisabledMessage }}
            </p>
          </div>
        </div>
        <div class="auth-actions">
          <router-link to="/login" class="btn btn-primary w-full">
            <Icon name="arrowLeft" size="sm" />
            {{ t('auth.backToLogin') }}
          </router-link>
        </div>
      </div>

      <!-- Form State -->
      <form v-else @submit.prevent="handleSubmit" class="space-y-3">
        <p v-if="errorMessage" role="alert" class="auth-error">{{ errorMessage }}</p>
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
              :disabled="isLoading"
              class="input"
              :class="{ 'input-error': errors.email }"
              :aria-invalid="Boolean(errors.email)"
              :aria-describedby="errors.email ? 'email-error' : undefined"
              :placeholder="t('auth.emailPlaceholder')"
            />
          </div>
          <p v-if="errors.email" id="email-error" role="alert" class="auth-field-error">{{ errors.email }}</p>
        </div>

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
        <button
          type="submit"
          :disabled="isLoading || !settingsLoaded || publicSettingsError || !passwordResetEnabled || (turnstileActive && !turnstileToken)"
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
          <Icon v-else name="mail" size="md" class="mr-2" />
          {{ isLoading ? t('auth.sendingResetLink') : t('auth.sendResetLink') }}
        </button>
        <router-link to="/login" class="btn btn-outline">{{ t('auth.backToLogin') }}</router-link>
        </div>
      </form>
    </div>

  </AuthLayout>
</template>

<script setup lang="ts">
import { extractApiErrorMessage } from '@/utils/apiError'

import { computed, ref, reactive, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { AuthLayout } from '@/components/layout'
import Icon from '@/components/icons/Icon.vue'
import TurnstileWidget from '@/components/CaptchaChallenge.vue'
import { resolveActiveCaptchaProvider } from '@/components/auth/captchaProvider'
import { useAppStore } from '@/stores'
import { getPublicSettings, forgotPassword } from '@/api/auth'

const i18n = useI18n()
const { t } = i18n
const isZh = computed(() => String(i18n.locale?.value ?? 'zh').toLowerCase().startsWith('zh'))
const passwordResetDisabledMessage = computed(() => {
  const translated = i18n.te?.('auth.passwordResetDisabled') ? t('auth.passwordResetDisabled') : ''
  if (translated && translated !== 'auth.passwordResetDisabled') {
    return translated
  }
  return isZh.value
    ? '密码重置功能暂时关闭，请联系管理员。'
    : 'Password reset is currently disabled. Please contact the administrator.'
})

// ==================== Stores ====================

const appStore = useAppStore()

// ==================== State ====================

const isLoading = ref<boolean>(false)
const isSubmitted = ref<boolean>(false)
const errorMessage = ref<string>('')
const settingsLoaded = ref<boolean>(false)
const publicSettingsError = ref<boolean>(false)
const passwordResetEnabled = ref<boolean>(true)

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
// 动作触发式验证码（腾讯/阿里云）：提交时弹窗验证
const actionCaptchaEnabled = computed(() => tencentActive.value || aliyunActive.value)
const captchaEnabled = computed(() => activeCaptchaProvider.value !== 'none')

const formData = reactive({
  email: ''
})

const errors = reactive({
  email: '',
  turnstile: ''
})

const validationToastMessage = computed(() => errors.email || errors.turnstile || '')

watch(validationToastMessage, (value, previousValue) => {
  if (value && value !== previousValue) {
    appStore.showError(value)
  }
})

// ==================== Lifecycle ====================

async function loadPublicSettings(): Promise<void> {
  settingsLoaded.value = false
  publicSettingsError.value = false
  try {
    const settings = await getPublicSettings()
    passwordResetEnabled.value =
      settings.password_reset_enabled !== false && settings.backend_mode_enabled !== true
    turnstileEnabled.value = settings.turnstile_enabled
    turnstileSiteKey.value = settings.turnstile_site_key || ''
    tencentCaptchaEnabled.value = settings.tencent_captcha_enabled === true
    tencentCaptchaAppId.value = settings.tencent_captcha_app_id || ''
    tencentCaptchaRegion.value = settings.tencent_captcha_region || 'cn'
    aliyunCaptchaEnabled.value = settings.aliyun_captcha_enabled === true
    aliyunCaptchaSceneId.value = settings.aliyun_captcha_scene_id || ''
    aliyunCaptchaPrefix.value = settings.aliyun_captcha_prefix || ''
    aliyunCaptchaRegion.value = settings.aliyun_captcha_region || 'cn'
  } catch (error) {
    console.error('Failed to load public settings:', error)
    publicSettingsError.value = true
  } finally {
    settingsLoaded.value = true
  }
}

onMounted(async () => {
  await loadPublicSettings()
})

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
  errors.email = ''
  errors.turnstile = ''

  let isValid = true

  // Email validation
  const normalizedEmail = formData.email.trim()
  if (!normalizedEmail) {
    errors.email = t('auth.emailRequired')
    isValid = false
  } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(normalizedEmail)) {
    errors.email = t('auth.invalidEmail')
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

async function handleSubmit(): Promise<void> {
  if (isLoading.value || !settingsLoaded.value || publicSettingsError.value || !passwordResetEnabled.value) {
    return
  }
  errorMessage.value = ''

  if (!validateForm()) {
    return
  }

  if (!(await acquireActionProof())) {
    return
  }

  isLoading.value = true

  try {
    const normalizedEmail = formData.email.trim()
    formData.email = normalizedEmail
    await forgotPassword({
      email: normalizedEmail,
      turnstile_token:
        turnstileActive.value || aliyunActive.value ? turnstileToken.value : undefined,
      tencent_captcha_ticket: tencentActive.value ? turnstileToken.value : undefined,
      tencent_captcha_randstr: tencentActive.value ? tencentCaptchaRandstr.value : undefined
    })

    isSubmitted.value = true
    appStore.showSuccess(t('auth.resetEmailSent'))
  } catch (error: unknown) {
    errorMessage.value = extractApiErrorMessage(error, t('auth.sendResetLinkFailed'))

    appStore.showError(errorMessage.value)
  } finally {
    if (captchaEnabled.value) {
      resetCaptchaProof()
    }
    isLoading.value = false
  }
}
</script>
