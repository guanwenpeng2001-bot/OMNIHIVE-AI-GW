<template>
  <AuthLayout>
    <div class="space-y-4">
      <div>
        <h1 class="auth-title">
          {{ t('auth.dingtalk.createAccountTitle') }}
        </h1>
        <p class="auth-intro">
          {{ t('auth.oauthFlow.createAccountHint') }}
        </p>
      </div>
      <div v-if="initialEmail" class="auth-summary">{{ t('auth.emailLabel') }}：{{ initialEmail }}</div>

      <PendingOAuthCreateAccountForm
        test-id-prefix="dingtalk"
        :initial-email="initialEmail"
        :is-submitting="isSubmitting"
        :error-message="accountActionError"
        @submit="handleCreateAccount"
        @switch-to-bind="handleSwitchToBind"
      />
    </div>
    <template #footer>
      <div class="auth-actions">
        <router-link to="/login" class="btn btn-outline">{{ t('auth.backToLogin') }}</router-link>
        <router-link to="/home" class="btn btn-outline">← 首页</router-link>
      </div>
    </template>
  </AuthLayout>

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
import { extractApiErrorCode, extractApiErrorMessage } from '@/utils/apiError'

import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { AuthLayout } from '@/components/layout'
import TotpLoginModal from '@/components/auth/TotpLoginModal.vue'
import PendingOAuthCreateAccountForm, {
  type PendingOAuthCreateAccountPayload
} from '@/components/auth/PendingOAuthCreateAccountForm.vue'
import { apiClient } from '@/api/client'
import { useAuthStore, useAppStore } from '@/stores'
import { sanitizeInternalRedirect } from '@/router/redirect'
import {
  login2FA,
  persistOAuthTokenContext,
  type PendingOAuthExchangeResponse
} from '@/api/auth'
import { clearAllAffiliateReferralCodes } from '@/utils/oauthAffiliate'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const authStore = useAuthStore()
const appStore = useAppStore()

const isSubmitting = ref(false)
const accountActionError = ref('')
const show2FAModal = ref(false)
const totpTempToken = ref('')
const totpUserEmailMasked = ref('')
const totpRedirectTarget = ref('/dashboard')
const totpModalRef = ref<InstanceType<typeof TotpLoginModal> | null>(null)

const initialEmail = (route.query.email as string | undefined) || ''

function sanitizeRedirectPath(path: string | null | undefined): string {
  return sanitizeInternalRedirect(path, '/dashboard')
}

function getRequestErrorMessage(error: unknown, fallback: string): string {
  const err = error as { message?: string; response?: { data?: { detail?: string; message?: string } } }
  return extractApiErrorMessage(err, '') || fallback
}

async function handleCreateAccount(payload: PendingOAuthCreateAccountPayload) {
  accountActionError.value = ''
  if (!payload.email || !payload.password) return

  isSubmitting.value = true
  try {
    const { data } = await apiClient.post<
      PendingOAuthExchangeResponse & {
        step?: string
        redirect?: string
        existing_account_bindable?: boolean
        requires_2fa?: boolean
        temp_token?: string
        user_email_masked?: string
      }
    >(
      '/auth/oauth/pending/create-account',
      {
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
          : {})
      }
    )

    const redirect = sanitizeRedirectPath(data.redirect || (route.query.redirect as string | undefined))

    if (data.requires_2fa === true && data.temp_token) {
      totpTempToken.value = data.temp_token
      totpUserEmailMasked.value = data.user_email_masked || ''
      totpRedirectTarget.value = redirect
      show2FAModal.value = true
      return
    }

    if (data.access_token) {
      persistOAuthTokenContext(data)
      await authStore.setToken(data.access_token)
      clearAllAffiliateReferralCodes()
      appStore.showSuccess(t('auth.loginSuccess'))
      await router.replace(redirect)
      return
    }

    // 后端把 pending session 转到 choice 状态（用户填的 email 已在系统内）→ 跳回 callback view 走绑定流程
    if (data.step === 'choose_account_action_required' || data.existing_account_bindable === true) {
      navigateToBindLogin(payload.email)
      return
    }

    accountActionError.value = t('auth.loginFailed')
  } catch (e: unknown) {
    // 全局"开放注册"关闭且未开启钉钉企业模式豁免时，引导用户去绑定已有账户而非死路
    if (extractApiErrorCode(e) === 'REGISTRATION_DISABLED') {
      appStore.showInfo(t('auth.dingtalk.registrationDisabledRedirectToBind'))
      navigateToBindLogin(payload.email)
      return
    }
    accountActionError.value = getRequestErrorMessage(e, t('auth.loginFailed'))
  } finally {
    isSubmitting.value = false
  }
}

function navigateToBindLogin(email: string) {
  const query: Record<string, string> = { bind: '1' }
  if (email) query.email = email
  const redirect = route.query.redirect as string | undefined
  if (redirect) query.redirect = sanitizeRedirectPath(redirect)
  router.replace({ path: '/auth/dingtalk/callback', query })
}

function handleSwitchToBind(email: string) {
  navigateToBindLogin(email)
}

async function handle2FAVerify(totpCode: string) {
  if (totpModalRef.value) {
    totpModalRef.value.setVerifying(true)
  }
  try {
    const response = await login2FA({ temp_token: totpTempToken.value, totp_code: totpCode })
    show2FAModal.value = false
    persistOAuthTokenContext(response)
    await authStore.setToken(response.access_token)
    clearAllAffiliateReferralCodes()
    appStore.showSuccess(t('auth.loginSuccess'))
    await router.replace(sanitizeRedirectPath(totpRedirectTarget.value))
  } catch (e: unknown) {
    const message = getRequestErrorMessage(e, t('profile.totp.loginFailed'))
    if (totpModalRef.value) {
      totpModalRef.value.setError(message)
      totpModalRef.value.setVerifying(false)
    } else {
      accountActionError.value = message
    }
  }
}

function handle2FACancel() {
  show2FAModal.value = false
  totpTempToken.value = ''
  totpUserEmailMasked.value = ''
  void router.replace('/login')
}
</script>
