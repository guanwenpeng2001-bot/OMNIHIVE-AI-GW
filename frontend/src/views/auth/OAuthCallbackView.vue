<template>
  <AuthLayout>
    <div class="space-y-4">
      <div v-if="isProcessing" role="status" class="space-y-3">
        <Badge tone="accent" dot mono>EXCHANGING SESSION</Badge>
        <h1 class="auth-title">
          {{ t('auth.oauth.callbackTitle') }}
        </h1>
        <p class="auth-intro">
          {{ t('auth.oauth.callbackHint') }}
        </p>
        <div class="space-y-2 pt-2" aria-hidden="true">
          <Skeleton width="100%" height="36px" />
          <Skeleton width="72%" height="20px" />
        </div>
      </div>

      <div v-else-if="needsRegistrationCompletion">
        <div class="mb-2">
          <Badge tone="accent" mono>{{ providerName.toUpperCase() }}</Badge>
        </div>
        <h1 class="auth-title">
          {{ t('auth.oidc.callbackTitle', { providerName }) }}
        </h1>
        <p class="auth-intro">
          {{ registrationHint }}
        </p>

        <div class="space-y-3">
          <div class="auth-summary">
            <label class="input-label">{{ t('auth.emailLabel') }}</label>
            <input
              class="input w-full"
              type="email"
              :value="registrationEmail"
              readonly
              disabled
            />
          </div>
          <div>
            <label class="input-label">{{ t('auth.passwordLabel') }}</label>
            <input
              v-model="password"
              type="password"
              class="input w-full"
              :placeholder="t('auth.createPasswordPlaceholder')"
              :disabled="isSubmitting"
              autocomplete="new-password"
              @keyup.enter="handleSubmitRegistration"
            />
          </div>
          <div>
            <label class="input-label">{{ t('auth.confirmPassword') }}</label>
            <input
              v-model="confirmPassword"
              type="password"
              class="input w-full"
              :placeholder="t('auth.confirmPasswordPlaceholder')"
              :disabled="isSubmitting"
              autocomplete="new-password"
              @keyup.enter="handleSubmitRegistration"
            />
          </div>
          <div v-if="invitationRequired">
            <label class="input-label">{{ t('auth.invitationCodeLabel') }}</label>
            <input
              v-model="invitationCode"
              type="text"
              class="input w-full"
              :placeholder="t('auth.invitationCodePlaceholder')"
              :disabled="isSubmitting"
              @keyup.enter="handleSubmitRegistration"
            />
          </div>
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
          <ErrorState
            v-if="registrationError"
            :title="registrationError"
            compact
            :retryable="false"
          />
          <div class="auth-actions">
          <Button
            variant="primary"
            :loading="isSubmitting"
            class="btn btn-primary w-full"
            type="button"
            :disabled="isSubmitting || !canSubmitRegistration"
            @click="handleSubmitRegistration"
          >
            {{ isSubmitting ? t('common.processing') : t('auth.oidc.completeRegistration') }}
          </Button>
          <Button type="button" variant="secondary" class="btn btn-outline" :disabled="isSubmitting" @click="router.replace('/login')">{{ t('auth.backToLogin') }}</Button>
          </div>
        </div>
      </div>

      <div v-else-if="invalidCallback" class="space-y-3">
        <Badge tone="danger" dot mono>INVALID CALLBACK</Badge>
        <h1 class="auth-title">
          {{ t('auth.oauth.invalidCallbackTitle') }}
        </h1>
        <p class="auth-summary">
          {{ t('auth.oauth.invalidCallbackHint') }}
        </p>
        <div class="auth-actions">
        <Button variant="primary" class="btn btn-primary" type="button" @click="router.replace('/login')">
          {{ t('auth.backToLogin') }}
        </Button>
        <router-link to="/home" class="btn btn-outline">← 首页</router-link>
        </div>
      </div>

      <div v-else class="space-y-3">
        <Badge tone="neutral" mono>OAUTH CALLBACK</Badge>
        <h1 class="auth-title">
          {{ t('auth.oauth.callbackTitle') }}
        </h1>
        <p class="auth-intro">
          {{ t('auth.oauth.callbackHint') }}
        </p>

        <div class="auth-summary space-y-3">
          <div>
            <label class="input-label">{{ t('auth.oauth.code') }}</label>
            <div class="flex gap-2">
              <input class="input flex-1 font-mono text-sm" :value="code" readonly />
              <button class="btn btn-secondary btn-outline" type="button" :disabled="!code" @click="copy(code)">
                {{ t('common.copy') }}
              </button>
            </div>
          </div>

          <div>
            <label class="input-label">{{ t('auth.oauth.state') }}</label>
            <div class="flex gap-2">
              <input class="input flex-1 font-mono text-sm" :value="state" readonly />
              <button
                class="btn btn-secondary btn-outline"
                type="button"
                :disabled="!state"
                @click="copy(state)"
              >
                {{ t('common.copy') }}
              </button>
            </div>
          </div>

          <div>
            <label class="input-label">{{ t('auth.oauth.fullUrl') }}</label>
            <div class="flex gap-2">
              <input class="input flex-1 font-mono text-xs" :value="fullUrl" readonly />
              <button
                class="btn btn-secondary btn-outline"
                type="button"
                :disabled="!fullUrl"
                @click="copy(fullUrl)"
              >
                {{ t('common.copy') }}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </AuthLayout>
</template>

<script setup lang="ts">
import AuthLayout from '@/components/layout/AuthLayout.vue'
import LoginAgreementPrompt from '@/components/auth/LoginAgreementPrompt.vue'
import { Badge, Button, ErrorState, Skeleton } from '@/components/ui'
import { authStorage } from '@/api/tokenRefresh'
import { extractApiErrorCode, extractApiErrorMessage } from '@/utils/apiError'
import { useCallbackLifetime } from '@/composables/useCallbackLifetime'
import { buildSignupAgreementPayload, resolveAgreementState } from '@/features/public/legal'
import type { LoginAgreementDocument } from '@/types'

import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { useClipboard } from '@/composables/useClipboard'
import { useAppStore, useAuthStore } from '@/stores'
import { apiClient } from '@/api/client'
import { buildApiUrl } from '@/api/url'
import {
  exchangePendingOAuthCompletion,
  getPublicSettings,
  persistOAuthTokenContext,
  type OAuthTokenResponse
} from '@/api/auth'
import {
  clearAllAffiliateReferralCodes,
  loadOAuthAffiliateCode,
  oauthAffiliatePayload
} from '@/utils/oauthAffiliate'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const appStore = useAppStore()
const authStore = useAuthStore()
const { isActive, waitForCallback } = useCallbackLifetime()
const isProcessing = ref(false)
const isSubmitting = ref(false)
const needsRegistrationCompletion = ref(false)
const invitationRequired = ref(false)
const registrationEmail = ref('')
const password = ref('')
const confirmPassword = ref('')
const invitationCode = ref('')
const registrationError = ref('')
const pendingProvider = ref<'github' | 'google'>('github')
const redirectTo = ref('/dashboard')
const invalidCallback = ref(false)
const loginAgreementEnabled = ref(false)
const loginAgreementMode = ref<'modal' | 'checkbox' | string>('modal')
const loginAgreementUpdatedAt = ref('')
const loginAgreementRevision = ref('')
const loginAgreementDocuments = ref<LoginAgreementDocument[]>([])
const agreementAccepted = ref(false)
const showAgreementModal = ref(false)

type EmailOAuthPendingCompletion = Partial<OAuthTokenResponse> & {
  error?: string
  provider?: string
  redirect?: string
  email?: string
  resolved_email?: string
  invitation_required?: boolean
}

const code = computed(() => (route.query.code as string) || '')
const state = computed(() => (route.query.state as string) || '')

const fullUrl = computed(() => {
  if (typeof window === 'undefined') return ''
  return window.location.href
})
const providerName = computed(() =>
  pendingProvider.value === 'google' ? 'Google' : 'GitHub'
)
const registrationHint = computed(() =>
  invitationRequired.value
    ? t('auth.oidc.invitationRequired', { providerName: providerName.value })
    : t('auth.oidc.completeRegistration')
)
const canSubmitRegistration = computed(() => {
  if (!registrationEmail.value.trim()) return false
  if (password.value.length < 6) return false
  if (password.value !== confirmPassword.value) return false
  if (invitationRequired.value && !invitationCode.value.trim()) return false
  if (loginAgreementEnabled.value && !agreementAccepted.value) return false
  return true
})

function parseFragmentParams(): URLSearchParams {
  const raw = typeof window !== 'undefined' ? window.location.hash : ''
  const hash = raw.startsWith('#') ? raw.slice(1) : raw
  if (hash) window.history.replaceState(null, '', window.location.pathname + window.location.search)
  return new URLSearchParams(hash)
}


function sanitizeRedirectPath(path: string | null | undefined): string {
  if (!path) return '/dashboard'
  if (!path.startsWith('/')) return '/dashboard'
  if (path.startsWith('//')) return '/dashboard'
  if (path.includes('://')) return '/dashboard'
  if (path.includes('\n') || path.includes('\r')) return '/dashboard'
  return path
}

function readPendingEmailOAuthProvider(): 'github' | 'google' | null {
  if (typeof window === 'undefined') return null
  const provider = authStorage.oauthPendingProvider.get()
  if (provider === 'github' || provider === 'google') return provider
  return null
}

function redirectProviderCallbackToBackend(provider: 'github' | 'google'): void {
  if (typeof window === 'undefined') return
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(route.query)) {
    if (Array.isArray(value)) {
      value.forEach((item) => {
        if (item != null) params.append(key, String(item))
      })
    } else if (value != null) {
      params.set(key, String(value))
    }
  }
  const suffix = params.toString() ? `?${params.toString()}` : ''
  window.location.href = buildApiUrl(`/auth/oauth/${provider}/callback${suffix}`)
}

async function finalizeTokenResponse(tokenResponse: OAuthTokenResponse, redirect: string) {
  persistOAuthTokenContext(tokenResponse)
  await waitForCallback(authStore.setToken(tokenResponse.access_token))
  if (typeof window !== 'undefined') {
    authStorage.oauthPendingProvider.remove()
  }
  clearAllAffiliateReferralCodes()
  appStore.showSuccess(t('auth.loginSuccess'))
  await router.replace(sanitizeRedirectPath(redirect))
}

function hasOAuthTokenResponse(value: Partial<OAuthTokenResponse>): value is OAuthTokenResponse {
  return typeof value.access_token === 'string' && value.access_token.trim() !== ''
}

async function resumePendingEmailOAuth(code?: string) {
  isProcessing.value = true
  try {
    const completion = await waitForCallback(exchangePendingOAuthCompletion(undefined, code)) as EmailOAuthPendingCompletion
    const completionRedirect = completion.redirect || '/dashboard'
    if (hasOAuthTokenResponse(completion)) {
      await finalizeTokenResponse(completion, completionRedirect)
      return
    }

    const provider = String(completion.provider || '').toLowerCase()
    if (provider === 'github' || provider === 'google') {
      pendingProvider.value = provider
    }
    redirectTo.value = sanitizeRedirectPath(completionRedirect)

    if (completion.error === 'invitation_required' || completion.error === 'registration_completion_required') {
      invitationRequired.value = completion.error === 'invitation_required' || completion.invitation_required === true
      registrationEmail.value = String(completion.resolved_email || completion.email || '').trim()
      await loadAgreementSettings()
      needsRegistrationCompletion.value = true
      isProcessing.value = false
      return
    }

    appStore.showError(completion.error || t('auth.loginFailed'))
  } catch (e: unknown) {
    if (!isActive()) return
    const err = e as { message?: string; response?: { data?: { message?: string } } }
    const message = extractApiErrorMessage(err, '') || t('auth.loginFailed')
    appStore.showError(message)
    invalidCallback.value = true
  } finally {
    if (isActive() && !needsRegistrationCompletion.value) {
      isProcessing.value = false
    }
  }
}

async function handleSubmitRegistration() {
  if (!isActive() || isSubmitting.value) return
  registrationError.value = ''
  if (!registrationEmail.value.trim()) {
    registrationError.value = t('auth.emailRequired')
    return
  }
  if (password.value.length < 6) {
    registrationError.value = t('auth.passwordMinLength')
    return
  }
  if (password.value !== confirmPassword.value) {
    registrationError.value = t('auth.passwordsDoNotMatch')
    return
  }
  const code = invitationCode.value.trim()
  if (invitationRequired.value && !code) return

  isSubmitting.value = true
  try {
    const agreementPayload = buildSignupAgreementPayload(
      loginAgreementEnabled.value,
      agreementAccepted.value,
      loginAgreementRevision.value
    )
    const payload: {
      password: string
      invitation_code?: string
      aff_code?: string
      login_agreement_accepted?: boolean
      login_agreement_revision?: string
    } = {
      password: password.value,
      ...oauthAffiliatePayload(loadOAuthAffiliateCode()),
      ...agreementPayload
    }
    if (invitationRequired.value) {
      payload.invitation_code = code
    }
    const { data } = await apiClient.post<OAuthTokenResponse>(
      `/auth/oauth/${pendingProvider.value}/complete-registration`,
      payload
    )
    if (!isActive()) return
    await finalizeTokenResponse(data, redirectTo.value)
  } catch (e: unknown) {
    if (!isActive()) return
    if (extractApiErrorCode(e) === 'LOGIN_AGREEMENT_REQUIRED') {
      agreementAccepted.value = false
      await loadAgreementSettings()
      registrationError.value = t('publicPortal.legal.agreementRequiredRefresh')
      return
    }
    const err = e as { message?: string; response?: { data?: { message?: string } } }
    registrationError.value =
      extractApiErrorMessage(err, '') || t('auth.oidc.completeRegistrationFailed')
  } finally {
    if (isActive()) isSubmitting.value = false
  }
}

async function loadAgreementSettings(): Promise<void> {
  try {
    const settings = await getPublicSettings()
    if (!isActive()) return
    const agreement = resolveAgreementState(settings)
    loginAgreementDocuments.value = agreement.documents
    loginAgreementEnabled.value = agreement.enabled
    loginAgreementMode.value = agreement.mode
    loginAgreementUpdatedAt.value = agreement.updatedAt
    loginAgreementRevision.value = agreement.revision
    agreementAccepted.value = !agreement.enabled
    showAgreementModal.value = agreement.enabled && agreement.mode !== 'checkbox'
  } catch {
    // Keep existing agreement state; backend still enforces if enabled.
  }
}

function acceptAgreement(): void {
  agreementAccepted.value = true
  showAgreementModal.value = false
}

function rejectAgreement(): void {
  agreementAccepted.value = false
  showAgreementModal.value = false
}

onMounted(async () => {
  const params = parseFragmentParams()
  if (params.has('access_token') || params.has('refresh_token')) {
    invalidCallback.value = true
    isProcessing.value = false
    return
  }
  const fragmentError = params.get('error') || (route.query.error as string) || (route.query.error_description as string) || ''
  const fragmentErrorDescription =
    params.get('error_description') || params.get('error_message') || (route.query.error_description as string) || ''

  if (fragmentError) {
    appStore.showError(fragmentErrorDescription || fragmentError)
    return
  }
  if (route.path === '/auth/oauth/callback') {
    const pendingEmailOAuthProvider = readPendingEmailOAuthProvider()
    if (pendingEmailOAuthProvider && code.value && state.value) {
      redirectProviderCallbackToBackend(pendingEmailOAuthProvider)
      return
    }
    await resumePendingEmailOAuth(params.get('code') || undefined)
  }
})

const copy = (value: string) => {
  if (!value) return
  copyToClipboard(value)
}
</script>
