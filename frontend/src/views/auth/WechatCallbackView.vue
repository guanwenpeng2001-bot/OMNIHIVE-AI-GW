<template>
  <AuthLayout>
    <div class="space-y-4">
      <div>
        <h1 class="auth-title">
          {{ t('auth.oidc.callbackTitle', { providerName }) }}
        </h1>
        <p class="auth-intro" :role="isProcessing ? 'status' : undefined">
          {{
            isProcessing
              ? t('auth.oidc.callbackProcessing', { providerName })
              : t('auth.oidc.callbackHint')
          }}
        </p>
      </div>

      <transition name="fade">
        <div
          v-if="
            needsInvitation ||
            needsChooser ||
            needsAdoptionConfirmation ||
            needsCreateAccount ||
            needsBindLogin ||
            needsTotpChallenge
          "
          class="space-y-4"
        >
          <div
            v-if="adoptionRequired && (suggestedDisplayName || suggestedAvatarUrl)"
            class="auth-summary"
          >
            <div class="space-y-3">
              <div class="space-y-1">
                <p class="text-sm font-medium text-[var(--foreground)]">
                  {{ t('auth.oauthFlow.profileDetailsTitle', { providerName }) }}
                </p>
                <p class="text-xs text-[var(--muted-foreground)]">
                  {{ t('auth.oauthFlow.profileDetailsDescription', { providerName }) }}
                </p>
              </div>

              <label
                v-if="suggestedDisplayName"
                class="flex items-start gap-3 rounded-lg border border-[var(--border)] bg-[var(--card)] p-3 text-sm"
              >
                <input v-model="adoptDisplayName" type="checkbox" class="mt-1 h-4 w-4" />
                <span class="space-y-1">
                  <span class="block font-medium text-[var(--foreground)]">
                    {{ t('auth.oauthFlow.useDisplayName') }}
                  </span>
                  <span class="block text-[var(--muted-foreground)]">
                    {{ suggestedDisplayName }}
                  </span>
                </span>
              </label>

              <label
                v-if="suggestedAvatarUrl"
                class="flex items-start gap-3 rounded-lg border border-[var(--border)] bg-[var(--card)] p-3 text-sm"
              >
                <input v-model="adoptAvatar" type="checkbox" class="mt-1 h-4 w-4" />
                <img
                  :src="suggestedAvatarUrl"
                  :alt="t('auth.oauthFlow.avatarAlt', { providerName })"
                  class="h-10 w-10 rounded-full border border-[var(--border)] object-cover"
                />
                <span class="space-y-1">
                  <span class="block font-medium text-[var(--foreground)]">
                    {{ t('auth.oauthFlow.useAvatar') }}
                  </span>
                  <span class="block break-all text-[var(--muted-foreground)]">
                    {{ suggestedAvatarUrl }}
                  </span>
                </span>
              </label>
            </div>
          </div>

          <template v-if="needsInvitation">
            <p class="text-sm text-[var(--foreground)]">
              {{ t('auth.oidc.invitationRequired', { providerName }) }}
            </p>
            <div>
              <input
                v-model="invitationCode"
                type="text"
                class="input w-full"
                :placeholder="t('auth.invitationCodePlaceholder')"
                :disabled="isSubmitting"
                @keyup.enter="handleSubmitInvitation"
              />
            </div>
            <button
              class="btn btn-primary w-full"
              :disabled="isSubmitting || !invitationCode.trim()"
              @click="handleSubmitInvitation"
            >
              {{
                isSubmitting
                  ? t('auth.oidc.completing')
                : t('auth.oidc.completeRegistration')
              }}
            </button>

            <div
              class="auth-summary"
            >
              <div class="space-y-3">
                <div class="space-y-1">
                  <p class="text-sm font-medium text-[var(--foreground)]">
                    {{ t('auth.alreadyHaveAccount') }}
                  </p>
                  <p class="text-xs text-[var(--muted-foreground)]">
                    {{
                      hasCurrentAuthToken
                        ? t('auth.oauthFlow.bindCurrentAccountDescription', { providerName })
                        : t('auth.oauthFlow.signInThenBindDescription', { providerName })
                    }}
                  </p>
                </div>

                <input
                  v-if="!hasCurrentAuthToken"
                  v-model="existingAccountEmail"
                  data-testid="existing-account-email"
                  type="email"
                  class="input w-full"
                  :placeholder="t('auth.emailPlaceholder')"
                  :disabled="isSubmitting"
                />

                <button
                  data-testid="existing-account-submit"
                  type="button"
                  class="btn btn-secondary btn-outline w-full"
                  :disabled="isSubmitting"
                  @click="handleExistingAccountBinding"
                >
                  {{ hasCurrentAuthToken ? t('auth.oauthFlow.bindCurrentAccount') : t('auth.signIn') }}
                </button>
              </div>
            </div>
          </template>

          <template v-else-if="needsChooser">
            <div
              class="auth-summary"
            >
              <div class="space-y-4">
                <div class="space-y-1">
                  <p class="text-sm font-medium text-[var(--foreground)]">
                    {{ t('auth.oauthFlow.chooseHowToContinue') }}
                  </p>
                  <p class="text-xs text-[var(--muted-foreground)]">
                    {{ t('auth.oauthFlow.chooseAccountActionHint') }}
                  </p>
                </div>

                <div class="auth-actions">
                <button
                  data-testid="wechat-choice-bind-existing"
                  type="button"
                  class="btn btn-primary w-full"
                  :disabled="isSubmitting"
                  @click="switchToBindLoginMode()"
                >
                  {{ t('auth.oauthFlow.bindExistingAccount') }}
                </button>

                <button
                  data-testid="wechat-choice-create-account"
                  type="button"
                  class="btn btn-secondary btn-outline w-full"
                  :disabled="isSubmitting"
                  @click="switchToCreateAccountMode()"
                >
                  {{ t('auth.oauthFlow.createNewAccount') }}
                </button>
                </div>
              </div>
            </div>
          </template>

          <template v-else-if="needsAdoptionConfirmation">
            <p class="text-sm text-[var(--foreground)]">
              {{ t('auth.oauthFlow.reviewProfileBeforeContinue', { providerName }) }}
            </p>
            <button class="btn btn-primary w-full" :disabled="isSubmitting" @click="handleContinueLogin">
              {{ isSubmitting ? t('common.processing') : t('auth.continue') }}
            </button>
          </template>

          <template v-else-if="needsCreateAccount">
            <p class="text-sm text-[var(--foreground)]">
              {{ t('auth.oauthFlow.createAccountHint') }}
            </p>
            <PendingOAuthCreateAccountForm
              test-id-prefix="wechat"
              :initial-email="pendingAccountEmail"
              :is-submitting="isSubmitting"
              :error-message="accountActionError"
              @submit="handleCreateAccount"
              @switch-to-bind="switchToBindLoginMode"
            />
            <button
              v-if="showBackToChooser"
              class="btn btn-secondary btn-outline w-full"
              :disabled="isSubmitting"
              @click="switchToCreateAccountMode()"
            >
              {{ t('auth.oauthFlow.createNewAccount') }}
            </button>
          </template>

          <template v-else-if="needsBindLogin">
            <p class="text-sm text-[var(--foreground)]">
              {{ t('auth.oauthFlow.bindSignInToExistingAccount', { providerName }) }}
            </p>
            <div
              v-if="hasCurrentAuthToken"
              class="auth-summary"
            >
              <div class="space-y-3">
                <div class="space-y-1">
                  <p class="text-sm font-medium text-[var(--foreground)]">
                    {{ t('auth.oauthFlow.bindCurrentAccountTitle') }}
                  </p>
                  <p class="text-xs text-[var(--muted-foreground)]">
                    {{ t('auth.oauthFlow.bindCurrentAccountDescription', { providerName }) }}
                  </p>
                </div>

                <button
                  data-testid="existing-account-submit"
                  type="button"
                  class="btn btn-primary w-full"
                  :disabled="isSubmitting"
                  @click="handleBindCurrentAccount"
                >
                  {{ isSubmitting ? t('common.processing') : t('auth.oauthFlow.bindCurrentAccount') }}
                </button>
              </div>
            </div>
            <div v-else class="space-y-3">
              <input
                v-model="bindLoginEmail"
                data-testid="wechat-bind-login-email"
                type="email"
                class="input w-full"
                :placeholder="t('auth.emailPlaceholder')"
                :disabled="isSubmitting"
                @keyup.enter="handleBindLogin"
              />
              <input
                v-model="bindLoginPassword"
                data-testid="wechat-bind-login-password"
                type="password"
                class="input w-full"
                :placeholder="t('auth.passwordPlaceholder')"
                :disabled="isSubmitting"
                @keyup.enter="handleBindLogin"
              />
              <button
                data-testid="wechat-bind-login-submit"
                class="btn btn-primary w-full"
                :disabled="isSubmitting || !bindLoginEmail.trim() || !bindLoginPassword"
                @click="handleBindLogin"
              >
                {{ isSubmitting ? t('common.processing') : t('auth.oauthFlow.logInAndBind') }}
              </button>
            </div>
            <button
              v-if="showBackToChooser"
              class="btn btn-secondary btn-outline w-full"
              :disabled="isSubmitting"
              @click="switchToCreateAccountMode()"
            >
              {{ t('auth.oauthFlow.createNewAccount') }}
            </button>
          </template>

          <template v-else-if="needsTotpChallenge">
            <p class="text-sm text-[var(--foreground)]">
              {{
                t('auth.oauthFlow.totpHint', {
                  providerName,
                  account: totpUserEmailMasked || t('auth.oauthFlow.yourAccount')
                })
              }}
            </p>
            <div class="space-y-3">
              <input
                v-model="totpCode"
                data-testid="wechat-bind-login-totp"
                type="text"
                inputmode="numeric"
                maxlength="6"
                class="input w-full"
                placeholder="123456"
                :disabled="isSubmitting"
                @keyup.enter="handleSubmitTotpChallenge"
              />
              <button
                data-testid="wechat-bind-login-totp-submit"
                class="btn btn-primary w-full"
                :disabled="isSubmitting || totpCode.trim().length !== 6"
                @click="handleSubmitTotpChallenge"
              >
                {{ isSubmitting ? t('common.processing') : t('auth.oauthFlow.verifyAndContinue') }}
              </button>
            </div>
          </template>
        </div>
      </transition>
    </div>
    <template #footer>
      <div class="auth-actions">
        <router-link to="/login" class="btn btn-outline">{{ t('auth.backToLogin') }}</router-link>
        <router-link to="/home" class="btn btn-outline">← 首页</router-link>
      </div>
    </template>
  </AuthLayout>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { AuthLayout } from '@/components/layout'
import PendingOAuthCreateAccountForm from '@/components/auth/PendingOAuthCreateAccountForm.vue'
import { useOAuthCallback } from '@/composables/useOAuthCallback'
import { completeWeChatOAuthRegistration } from '@/api/auth'
import { useRoute, useRouter } from 'vue-router'
import { useAppStore } from '@/stores'
import { getAuthToken, hasExplicitWeChatOAuthCapabilities, resolveWeChatOAuthStartStrict, prepareOAuthBindAccessTokenCookie } from '@/api/auth'

const { t } = useI18n()
const providerName = computed(() => t('auth.wechatProviderName'))
const route = useRoute()
const router = useRouter()
const appStore = useAppStore()
const existingAccountEmail = ref('')
const hasCurrentAuthToken = computed(() => Boolean(getAuthToken()))
const {
  isProcessing,
  errorMessage,
  needsInvitation,
  invitationCode,
  isSubmitting,
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
  accountActionError,
  needsTotpChallenge,
  totpCode,
  totpUserEmailMasked,
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
} = useOAuthCallback({
  provider: 'wechat',
  errorPrefix: 'auth.oidc',
  completeRegistration: completeWeChatOAuthRegistration,
  beforeResume,
})
async function ensurePublicSettingsLoaded(): Promise<void> {
  if (hasExplicitWeChatOAuthCapabilities(appStore.cachedPublicSettings)) {
    return
  }

  if (appStore.publicSettingsLoaded) {
    return
  }

  await appStore.fetchPublicSettings()
}

function resolveConfiguredWeChatOAuthMode(): 'open' | 'mp' | null {
  if (!hasExplicitWeChatOAuthCapabilities(appStore.cachedPublicSettings)) {
    return null
  }

  return resolveWeChatOAuthStartStrict(appStore.cachedPublicSettings).mode
}

function resolveWeChatOAuthUnavailableMessage(): string {
  const resolved = resolveWeChatOAuthStartStrict(appStore.cachedPublicSettings)

  switch (resolved.unavailableReason) {
    case 'capability_unknown':
      return t('auth.oauthFlow.wechatAvailabilityUnknown')
    case 'external_browser_required':
      return t('auth.oauthFlow.wechatSystemBrowserOnly')
    case 'wechat_browser_required':
      return t('auth.oauthFlow.wechatBrowserOnly')
    case 'native_app_required':
      return 'This WeChat sign-in flow is only available from the native mobile app.'
    case 'not_configured':
      return t('auth.oauthFlow.wechatNotConfigured')
    default:
      return t('auth.loginFailed')
  }
}

function resolveRuntimeWeChatOAuthMode(): 'open' | 'mp' {
  if (typeof navigator === 'undefined') {
    return 'open'
  }
  return /MicroMessenger/i.test(navigator.userAgent) ? 'mp' : 'open'
}

function normalizeWeChatOAuthMode(value: unknown): 'open' | 'mp' | null {
  return value === 'open' || value === 'mp' ? value : null
}

function resolveRequestedWeChatOAuthMode(): 'open' | 'mp' | null {
  const configuredMode = resolveConfiguredWeChatOAuthMode()
  if (configuredMode) {
    return configuredMode
  }

  const queryMode = normalizeWeChatOAuthMode(route.query.mode)
  if (queryMode) {
    return queryMode
  }

  return resolveRuntimeWeChatOAuthMode()
}

function resolveRedirectTarget(): string {
  return sanitizeRedirectPath(
    (route.query.redirect as string | undefined) || redirectTo.value || '/dashboard'
  )
}

function resolveWeChatStartURL(intent: 'bind_current_user' | 'adopt_existing_user_by_email'): string | null {
  const apiBase = (import.meta.env.VITE_API_BASE_URL as string | undefined) || '/api/v1'
  const normalized = apiBase.replace(/\/$/, '')
  const mode = resolveRequestedWeChatOAuthMode()
  if (!mode) {
    return null
  }
  const params = new URLSearchParams({
    mode,
    redirect: resolveRedirectTarget(),
    intent,
  })

  return `${normalized}/auth/oauth/wechat/bind/start?${params.toString()}`
}

function buildExistingAccountResumePath(): string | null {
  const mode = resolveRequestedWeChatOAuthMode()
  if (!mode) {
    return null
  }

  const params = new URLSearchParams({
    wechat_bind_existing: '1',
    redirect: resolveRedirectTarget(),
    mode,
  })

  const email = existingAccountEmail.value.trim()
  if (email) {
    params.set('email', email)
  }

  return `/auth/wechat/callback?${params.toString()}`
}

async function handleBindCurrentAccount() {
  const unavailableMessage = resolveConfiguredWeChatOAuthMode() === null
    ? resolveWeChatOAuthUnavailableMessage()
    : ''

  const startURL = resolveWeChatStartURL('bind_current_user')
  if (!startURL) {
    errorMessage.value = unavailableMessage || resolveWeChatOAuthUnavailableMessage()
    return
  }

  try {
    const csrfToken = await prepareOAuthBindAccessTokenCookie()
    if (!isActive()) return
    window.location.href = `${startURL}&csrf_token=${encodeURIComponent(csrfToken)}`
  } catch (e: unknown) {
    if (!isActive()) return
    errorMessage.value = getRequestErrorMessage(e, t('auth.loginFailed'))
  }
}

async function handleExistingAccountBinding() {
  if (getAuthToken()) {
    await handleBindCurrentAccount()
    return
  }

  const resumePath = buildExistingAccountResumePath()
  if (!resumePath) {
    errorMessage.value = resolveWeChatOAuthUnavailableMessage()
    return
  }

  const params = new URLSearchParams({
    redirect: resumePath,
  })
  const email = existingAccountEmail.value.trim()
  if (email) {
    params.set('email', email)
  }
  await router.replace(`/login?${params.toString()}`)
}

async function beforeResume() {
  try {
    await ensurePublicSettingsLoaded()
  } catch {
    // Binding recovery requires confirmed capability flags. Use the strict guard below.
  }

  if (!isActive()) return true

  if (typeof route.query.email === 'string') {
    const email = route.query.email.trim()
    existingAccountEmail.value = email
    bindLoginEmail.value = email
    pendingAccountEmail.value = email
  }

  if (route.query.wechat_bind_existing === '1') {
    if (getAuthToken()) {
      await handleBindCurrentAccount()
      return true
    }

    const resumePath = buildExistingAccountResumePath()
    if (!resumePath) {
      errorMessage.value = resolveWeChatOAuthUnavailableMessage()
      isProcessing.value = false
      return true
    }

    const params = new URLSearchParams({
      redirect: resumePath,
    })
    const email = existingAccountEmail.value.trim()
    if (email) {
      params.set('email', email)
    }
    await router.replace(`/login?${params.toString()}`)
    return true
  }


}
const showBackToChooser = computed(() => needsCreateAccount.value || needsBindLogin.value)
</script>
