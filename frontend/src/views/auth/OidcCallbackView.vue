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
              test-id-prefix="oidc"
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
                data-testid="oidc-bind-login-email"
                type="email"
                class="input w-full"
                :placeholder="t('auth.emailPlaceholder')"
                :disabled="isSubmitting"
                @keyup.enter="handleBindLogin"
              />
              <input
                v-model="bindLoginPassword"
                data-testid="oidc-bind-login-password"
                type="password"
                class="input w-full"
                :placeholder="t('auth.passwordPlaceholder')"
                :disabled="isSubmitting"
                @keyup.enter="handleBindLogin"
              />
              <div class="auth-actions">
              <button
                data-testid="oidc-bind-login-submit"
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
                data-testid="oidc-bind-login-totp"
                type="text"
                inputmode="numeric"
                maxlength="6"
                class="input w-full"
                placeholder="123456"
                :disabled="isSubmitting"
                @keyup.enter="handleSubmitTotpChallenge"
              />
              <button
                data-testid="oidc-bind-login-totp-submit"
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
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { AuthLayout } from '@/components/layout'
import PendingOAuthCreateAccountForm from '@/components/auth/PendingOAuthCreateAccountForm.vue'
import { useOAuthCallback } from '@/composables/useOAuthCallback'
import { completeOIDCOAuthRegistration, getPublicSettings } from '@/api/auth'
const { t } = useI18n()
const providerName = ref('OIDC')
async function loadProviderName() {
  try {
    const settings = await getPublicSettings()
    if (isActive() && settings.oidc_oauth_provider_name?.trim()) providerName.value = settings.oidc_oauth_provider_name.trim()
  } catch { /* Keep the default provider label. */ }
}
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
  isActive,
} = useOAuthCallback({
  provider: 'oidc',
  errorPrefix: 'auth.oidc',
  completeRegistration: completeOIDCOAuthRegistration,
  beforeResume: async () => { void loadProviderName() },
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

div.card {
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--card);
  color: var(--foreground);
  box-shadow: none;
}

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
