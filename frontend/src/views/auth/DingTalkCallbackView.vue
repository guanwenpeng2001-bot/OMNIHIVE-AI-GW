<template>
  <AuthLayout>
    <div class="space-y-4">
      <div>
        <h1 class="auth-title">
          {{ t('auth.dingtalk.callbackTitle') }}
        </h1>
        <p class="auth-intro" :role="isProcessing ? 'status' : undefined">
          {{ isProcessing ? t('auth.dingtalk.callbackProcessing') : t('auth.dingtalk.callbackHint') }}
        </p>
      </div>

      <transition name="fade">
        <div
          v-if="
            needsInvitation ||
            needsAdoptionConfirmation ||
            needsChooser ||
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
              {{ t('auth.dingtalk.invitationRequired') }}
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
              @open="openAgreementModal"
              @close="closeAgreementModal"
            />
            <button
              class="btn btn-primary w-full"
              :disabled="isSubmitting || !invitationCode.trim() || (loginAgreementEnabled && !agreementAccepted)"
              @click="handleSubmitInvitation"
            >
              {{ isSubmitting ? t('auth.dingtalk.completing') : t('auth.dingtalk.completeRegistration') }}
            </button>
          </template>

          <template v-else-if="needsAdoptionConfirmation">
            <p class="text-sm text-[var(--foreground)]">
              {{ t('auth.oauthFlow.reviewProfileBeforeContinue', { providerName }) }}
            </p>
            <button class="btn btn-primary w-full" :disabled="isSubmitting" @click="handleContinueLogin">
              {{ isSubmitting ? t('common.processing') : t('auth.continue') }}
            </button>
          </template>

          <template v-else-if="needsChooser">
            <div class="auth-summary">
              <div class="space-y-4">
                <div class="space-y-1">
                  <p class="text-sm font-medium text-[var(--foreground)]">
                    {{ t('auth.oauthFlow.chooseHowToContinue') }}
                  </p>
                  <p class="text-xs text-[var(--muted-foreground)]">
                    {{
                      pendingAccountEmail
                        ? t('auth.oauthFlow.suggestedEmail', { email: pendingAccountEmail })
                        : t('auth.oauthFlow.chooseAccountActionHint')
                    }}
                  </p>
                </div>

                <div class="auth-actions">
                  <button
                    class="btn btn-secondary btn-outline w-full"
                    :disabled="isSubmitting"
                    @click="switchToBindLoginMode()"
                  >
                    {{ t('auth.oauthFlow.bindExistingAccount') }}
                  </button>
                  <button
                    class="btn btn-primary w-full"
                    :disabled="isSubmitting"
                    @click="switchToCreateAccountMode"
                  >
                    {{ t('auth.oauthFlow.createNewAccount') }}
                  </button>
                </div>
              </div>
            </div>
          </template>

          <template v-else-if="needsCreateAccount">
            <p class="text-sm text-[var(--foreground)]">
              {{ t('auth.oauthFlow.createAccountHint') }}
            </p>
            <PendingOAuthCreateAccountForm
              test-id-prefix="dingtalk"
              :initial-email="pendingAccountEmail"
              :is-submitting="isSubmitting"
              :error-message="accountActionError"
              @submit="handleCreateAccount"
              @switch-to-bind="switchToBindLoginMode"
            />
          </template>

          <template v-else-if="needsBindLogin">
            <p class="text-sm text-[var(--foreground)]">
              {{ t('auth.oauthFlow.bindLoginHint', { providerName }) }}
            </p>
            <div class="space-y-3">
              <input
                v-model="bindLoginEmail"
                data-testid="dingtalk-bind-login-email"
                type="email"
                class="input w-full"
                :placeholder="t('auth.emailPlaceholder')"
                :disabled="isSubmitting"
                @keyup.enter="handleBindLogin"
              />
              <input
                v-model="bindLoginPassword"
                data-testid="dingtalk-bind-login-password"
                type="password"
                class="input w-full"
                :placeholder="t('auth.passwordPlaceholder')"
                :disabled="isSubmitting"
                @keyup.enter="handleBindLogin"
              />
              <div class="auth-actions">
              <button
                data-testid="dingtalk-bind-login-submit"
                class="btn btn-primary w-full"
                :disabled="isSubmitting || !bindLoginEmail.trim() || !bindLoginPassword"
                @click="handleBindLogin"
              >
                {{ isSubmitting ? t('common.processing') : t('auth.oauthFlow.logInAndBind') }}
              </button>
              <button
                v-if="canReturnToCreateAccount"
                class="btn btn-secondary btn-outline w-full"
                :disabled="isSubmitting"
                @click="switchToCreateAccountMode"
              >
                {{ t('auth.oauthFlow.useDifferentEmail') }}
              </button>
              </div>
            </div>
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
                data-testid="dingtalk-bind-login-totp"
                type="text"
                inputmode="numeric"
                maxlength="6"
                class="input w-full"
                placeholder="123456"
                :disabled="isSubmitting"
                @keyup.enter="handleSubmitTotpChallenge"
              />
              <button
                data-testid="dingtalk-bind-login-totp-submit"
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
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { AuthLayout } from '@/components/layout'
import LoginAgreementPrompt from '@/components/auth/LoginAgreementPrompt.vue'
import PendingOAuthCreateAccountForm from '@/components/auth/PendingOAuthCreateAccountForm.vue'
import { useOAuthCallback } from '@/composables/useOAuthCallback'
import { apiClient } from '@/api/client'
import type { PendingOAuthExchangeResponse } from '@/api/auth'
import { oauthAffiliatePayload } from '@/utils/oauthAffiliate'
const { t, te } = useI18n()
const providerName = computed(() => t('auth.dingtalkProviderName'))
const {
  isProcessing,
  needsInvitation,
  invitationCode,
  isSubmitting,
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
  canReturnToCreateAccount,
  needsTotpChallenge,
  totpCode,
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
} = useOAuthCallback({
  provider: 'dingtalk',
  errorPrefix: 'auth.dingtalk',
  completeRegistration: async (code, decision, affiliateCode, agreement) => {
    const { data } = await apiClient.post<PendingOAuthExchangeResponse>('/auth/oauth/dingtalk/complete-registration', {
      invitation_code: code,
      ...oauthAffiliatePayload(affiliateCode),
      adopt_display_name: decision.adoptDisplayName,
      adopt_avatar: decision.adoptAvatar,
      ...(agreement?.login_agreement_accepted !== undefined ? agreement : {}),
    })
    return data
  },
  resolveError: (code, description) => te(`auth.dingtalk.error.${code}`) ? t(`auth.dingtalk.error.${code}`) : description || code,
})
</script>
