<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.reAuthorizeAccount')"
    width="normal"
    @close="handleClose"
  >
    <div v-if="account" class="space-y-4">
      <!-- Account Info -->
      <div
        class="rounded-xl border border-[var(--border)] bg-[color-mix(in_srgb,var(--surface-2)_60%,transparent)] p-3.5"
      >
        <div class="flex items-center gap-3">
          <div
            class="flex h-9 w-9 items-center justify-center rounded-lg border border-[var(--border)] bg-[var(--primary)] text-[var(--primary-foreground)]"
          >
            <Icon name="sparkles" size="sm" />
          </div>
          <div>
            <span class="block text-sm font-semibold text-[var(--foreground)]">{{
              account.name
            }}</span>
            <span class="text-xs text-[var(--muted-foreground)]">
              {{
                isOpenAI
                  ? t('admin.accounts.openaiAccount')
                  : isGemini
                    ? t('admin.accounts.geminiAccount')
                    : isAntigravity
                      ? t('admin.accounts.antigravityAccount')
                      : isGrok
                        ? t('admin.accounts.grokAccount')
                        : t('admin.accounts.claudeCodeAccount')
              }}
            </span>
          </div>
        </div>
      </div>

      <!-- Add Method Selection (Claude only) -->
      <fieldset v-if="isAnthropic" class="border-0 p-0">
        <legend class="input-label">{{ t('admin.accounts.oauth.authMethod') }}</legend>
        <div class="mt-2 flex gap-4">
          <label class="flex cursor-pointer items-center">
            <input
              v-model="addMethod"
              type="radio"
              value="oauth"
              class="mr-2 text-[var(--brand-text)] focus:ring-[var(--brand)]"
            />
            <span class="text-sm text-[var(--foreground)]">{{
              t('admin.accounts.types.oauth')
            }}</span>
          </label>
          <label class="flex cursor-pointer items-center">
            <input
              v-model="addMethod"
              type="radio"
              value="setup-token"
              class="mr-2 text-[var(--brand-text)] focus:ring-[var(--brand)]"
            />
            <span class="text-sm text-[var(--foreground)]">{{
              t('admin.accounts.setupTokenLongLived')
            }}</span>
          </label>
        </div>
      </fieldset>

      <!-- Gemini OAuth Type Display (read-only) -->
      <div v-if="isGemini" class="rounded-lg border border-[var(--border)] bg-[var(--surface-2)] p-4">
        <div class="mb-2 text-sm font-medium text-[var(--foreground)]">
          {{ t('admin.accounts.oauth.gemini.oauthTypeLabel') }}
        </div>
        <div class="flex items-center gap-3">
          <div
            :class="[
              'flex h-8 w-8 shrink-0 items-center justify-center rounded-lg',
              geminiOAuthType === 'google_one'
                ? 'bg-purple-500 text-white'
                : geminiOAuthType === 'code_assist'
                  ? 'bg-blue-500 text-white'
                  : 'bg-amber-500 text-white'
            ]"
          >
            <Icon v-if="geminiOAuthType === 'google_one'" name="user" size="sm" />
            <Icon v-else-if="geminiOAuthType === 'code_assist'" name="cloud" size="sm" />
            <Icon v-else name="sparkles" size="sm" />
          </div>
          <div>
            <span class="block text-sm font-medium text-[var(--foreground)]">
              {{
                geminiOAuthType === 'google_one'
                  ? 'Google One'
                  : geminiOAuthType === 'code_assist'
                    ? t('admin.accounts.gemini.oauthType.builtInTitle')
                    : t('admin.accounts.gemini.oauthType.customTitle')
              }}
            </span>
            <span class="text-xs text-[var(--muted-foreground)]">
              {{
                geminiOAuthType === 'google_one'
                  ? t('admin.accounts.gemini.oauthType.googleOneDesc')
                  : geminiOAuthType === 'code_assist'
                    ? t('admin.accounts.gemini.oauthType.builtInDesc')
                    : t('admin.accounts.gemini.oauthType.customDesc')
              }}
            </span>
          </div>
        </div>
      </div>

      <AccountModelConfigurationSection v-if="pendingAuthorization" v-model="authorizationModels" v-model:mappings="authorizationMappings" :platform="account.platform" :candidate-identity="authorizationCandidateIdentity" :sync-credentials="authorizationPreview" :active="show" />
      <p v-if="authorizationError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ authorizationError }}</p>
      <WorkBuddyAuthFields
        v-if="isWorkBuddy && !pendingAuthorization"
        mode="oauth"
        editing
        :proxy-id="account?.proxy_id"
        :proxy-group-id="account?.proxy_group_id"
        v-model:site="workbuddySite"
        v-model:oauth-credentials="workbuddyOAuthCredentials"
        @busy="workbuddyAuthorizing = $event"
      />
      <OAuthAuthorizationFlow v-else-if="!pendingAuthorization"
        ref="oauthFlowRef"
        :add-method="addMethod"
        :auth-url="currentAuthUrl"
        :session-id="currentSessionId"
        :loading="currentLoading"
        :error="currentError"
        :show-help="isAnthropic"
        :show-proxy-warning="isAnthropic"
        :show-cookie-option="isAnthropic"
        :show-refresh-token-option="isOpenAI || isAntigravity || isGrok"
        :show-sso-option="isGrok"
        :show-email-password-option="false"
        :allow-multiple="false"
        :method-label="t('admin.accounts.inputMethod')"
        :platform="isOpenAI ? 'openai' : isGemini ? 'gemini' : isAntigravity ? 'antigravity' : isGrok ? 'grok' : 'anthropic'"
        :show-project-id="isGemini && geminiOAuthType === 'code_assist'"
        :initial-input-method="grokInitialInputMethod"
        @generate-url="handleGenerateUrl"
        @cookie-auth="handleCookieAuth"
        @validate-refresh-token="handleValidateRefreshToken"
        @import-sso="handleGrokImportSSO"
      />

    </div>

    <template #footer>
      <button v-if="pendingAuthorization" type="button" class="btn btn-primary" data-testid="confirm-reauthorization" :disabled="confirmingAuthorization" @click="confirmAuthorizationReview">{{ t('common.confirm') }}</button>
      <div v-if="account" class="flex justify-between gap-3">
        <button type="button" class="btn btn-secondary" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button
          v-if="isWorkBuddy && !pendingAuthorization"
          type="button"
          data-testid="workbuddy-reauth-submit"
          :disabled="workbuddyAuthorizing || workbuddySubmitting || !workbuddyOAuthCredentials.access_token.trim()"
          class="btn btn-primary"
          @click="handleWorkBuddyReauth"
        >
          {{
            workbuddySubmitting
              ? t('admin.accounts.oauth.verifying')
              : t('admin.accounts.oauth.completeAuth')
          }}
        </button>
        <button
          v-else-if="isManualInputMethod && !pendingAuthorization"
          type="button"
          :disabled="!canExchangeCode"
          class="btn btn-primary"
          @click="handleExchangeCode"
        >
          <svg
            v-if="currentLoading"
            class="-ml-1 mr-2 h-4 w-4 animate-spin"
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
          {{
            currentLoading
              ? t('admin.accounts.oauth.verifying')
              : t('admin.accounts.oauth.completeAuth')
          }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { oauthProxyPayload, type OAuthProxySelection } from '@/utils/accountProxy'
import { apiClient } from '@/api/client'
import AccountModelConfigurationSection from '@/components/account/AccountModelConfigurationSection.vue'
import WorkBuddyAuthFields from '@/components/account/WorkBuddyAuthFields.vue'
import { buildWorkBuddyCredentials, extractWorkBuddyOAuthDraft, extractWorkBuddySite, type WorkBuddyOAuthDraft } from '@/components/account/credentialsBuilder'
import type { WorkBuddySite } from '@/api/admin/workbuddy'
import { readAccountCredentialDraft, credentialsFromAuthorization, type AccountCredentialDraft } from '@/components/account/accountCredentialDraft'
import { splitModelMappingObject, buildModelMappingObject, validateModelRestrictions, translateModelRestrictionErrors, type ModelMappingEntry } from '@/composables/useModelWhitelist'

import { extractApiErrorMessage } from '@/utils/apiError'

import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import {
  useAccountOAuth,
  type AddMethod,
  type AuthInputMethod
} from '@/composables/useAccountOAuth'
import { useOpenAIOAuth } from '@/composables/useOpenAIOAuth'
import { useGeminiOAuth } from '@/composables/useGeminiOAuth'
import { useAntigravityOAuth } from '@/composables/useAntigravityOAuth'
import { useGrokOAuth } from '@/composables/useGrokOAuth'
import type { Account } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import OAuthAuthorizationFlow from '@/components/account/OAuthAuthorizationFlow.vue'

// Type for exposed OAuthAuthorizationFlow component
// Note: defineExpose automatically unwraps refs, so we use the unwrapped types
interface OAuthFlowExposed {
  authCode: string
  oauthState: string
  projectId: string
  sessionKey: string
  inputMethod: AuthInputMethod
  reset: () => void
}

interface Props {
  show: boolean
  account: Account | null
}

const props = defineProps<Props>()
const emit = defineEmits<{
  close: []
  reauthorized: [account: Account]
}>()

const appStore = useAppStore()
const { t } = useI18n()
const reauthorizationPending = Symbol('reauthorization-pending')
const pendingAuthorization = ref<{ accountId: number; draft: AccountCredentialDraft; type: 'oauth' | 'setup-token'; extra?: Record<string, unknown> } | null>(null)
const authorizationModels = ref<string[]>([])
const authorizationMappings = ref<ModelMappingEntry[]>([])
const confirmingAuthorization = ref(false)
const authorizationError = ref('')
const authorizationCandidateIdentity = computed(() => ({
  host: String(props.account?.credentials?.base_url ?? ''),
  type: pendingAuthorization.value?.type ?? props.account?.type,
  profile: String(props.account?.credentials?.provider_profile ?? ''),
  region: String(props.account?.credentials?.aws_region ?? props.account?.credentials?.location ?? ''),
  mode: String(props.account?.credentials?.account_mode ?? '')
}))
let reviewedAuthorization = ''
const authorizationPreview = computed(() => pendingAuthorization.value ? {
  platform: pendingAuthorization.value.draft.platform, type: pendingAuthorization.value.type,
  credential_draft: pendingAuthorization.value.draft.credential_draft, credential_draft_revision: pendingAuthorization.value.draft.revision,
  credentials: { model_mapping: buildModelMappingObject('combined', authorizationModels.value, authorizationMappings.value) ?? {} }
} : undefined)
async function stageReauthorization(accountId: number, payload: { type: 'oauth' | 'setup-token'; credentials: Record<string, unknown>; extra?: Record<string, unknown> }): Promise<never> {
  const draft = readAccountCredentialDraft(payload.credentials)
  if (!draft) throw new Error(t('admin.accounts.oauth.authFailed'))
  if (!props.show || props.account?.id !== accountId) {
    await apiClient.delete(`/admin/accounts/credential-drafts/${encodeURIComponent(draft.credential_draft)}`).catch(() => {})
    throw reauthorizationPending
  }
  const selection = splitModelMappingObject(props.account.credentials?.model_mapping as Record<string, unknown> | undefined)
  authorizationModels.value = selection.allowedModels
  authorizationMappings.value = selection.modelMappings
  pendingAuthorization.value = { accountId, draft, type: payload.type, extra: payload.extra }
  reviewedAuthorization = ''
  authorizationError.value = ''
  throw reauthorizationPending
}
async function cancelAuthorizationReview() {
  const pending = pendingAuthorization.value
  pendingAuthorization.value = null
  reviewedAuthorization = ''
  if (pending) await apiClient.delete(`/admin/accounts/credential-drafts/${encodeURIComponent(pending.draft.credential_draft)}`).catch(() => {})
}
async function confirmAuthorizationReview() {
  const pending = pendingAuthorization.value
  if (!pending || confirmingAuthorization.value || props.account?.id !== pending.accountId) return
  const errors = validateModelRestrictions('combined', authorizationModels.value, authorizationMappings.value)
  if (errors.length) { authorizationError.value = translateModelRestrictionErrors(errors, t); return }
  const credentials = { model_mapping: buildModelMappingObject('combined', authorizationModels.value, authorizationMappings.value) ?? {} }
  const selection = JSON.stringify(credentials)
  confirmingAuthorization.value = true
  authorizationError.value = ''
  try {
    const review = { name: `account:${pending.accountId}`, platform: pending.draft.platform, type: pending.type, credentials, extra: pending.extra,
      credential_draft: pending.draft.credential_draft, credential_draft_revision: pending.draft.revision }
    const fingerprint = JSON.stringify({ ...review, credential_draft_revision: undefined })
    if (reviewedAuthorization !== fingerprint) {
      const { data } = await apiClient.post<AccountCredentialDraft>(`/admin/accounts/credential-drafts/${encodeURIComponent(pending.draft.credential_draft)}/review`, review)
      pending.draft.revision = data.revision
      reviewedAuthorization = fingerprint
    }
    if (pendingAuthorization.value !== pending || selection !== JSON.stringify({ model_mapping: buildModelMappingObject('combined', authorizationModels.value, authorizationMappings.value) ?? {} })) return
    const updatedAccount = await adminAPI.accounts.applyOAuthCredentials(pending.accountId, {
      type: pending.type, credentials, extra: pending.extra, credential_draft: pending.draft.credential_draft, credential_draft_revision: pending.draft.revision
    })
    pendingAuthorization.value = null
    appStore.showSuccess(t('admin.accounts.reAuthorizedSuccess'))
    emit('reauthorized', updatedAccount)
    emit('close')
  } catch (error) { authorizationError.value = extractApiErrorMessage(error, t('admin.accounts.oauth.authFailed')) }
  finally { confirmingAuthorization.value = false }
}
watch(() => [props.show, props.account?.id], () => { if (!props.show || (pendingAuthorization.value && pendingAuthorization.value.accountId !== props.account?.id)) void cancelAuthorizationReview() })


// OAuth composables
const accountOAuthProxy = computed<OAuthProxySelection>(() => props.account?.proxy_group_id ? { proxy_group_id: props.account.proxy_group_id } : props.account?.proxy_id ?? null)
const claudeOAuth = useAccountOAuth()
const openaiOAuth = useOpenAIOAuth()
const geminiOAuth = useGeminiOAuth()
const antigravityOAuth = useAntigravityOAuth()
const grokOAuth = useGrokOAuth()

// Refs
const oauthFlowRef = ref<OAuthFlowExposed | null>(null)

// State
const addMethod = ref<AddMethod>('oauth')
const geminiOAuthType = ref<'code_assist' | 'google_one' | 'ai_studio'>('code_assist')

// Computed - check platform
const isOpenAI = computed(() => props.account?.platform === 'openai')
const isOpenAILike = computed(() => isOpenAI.value)
const isGemini = computed(() => props.account?.platform === 'gemini')
const isAnthropic = computed(() => props.account?.platform === 'anthropic')
const isAntigravity = computed(() => props.account?.platform === 'antigravity')
const isGrok = computed(() => props.account?.platform === 'grok')
const isWorkBuddy = computed(() => props.account?.platform === 'workbuddy')
const workbuddySite = ref<WorkBuddySite>('cn')
const workbuddyAuthorizing = ref(false)
const workbuddySubmitting = ref(false)
const workbuddyOAuthCredentials = ref<WorkBuddyOAuthDraft>(extractWorkBuddyOAuthDraft(undefined, 'cn'))

/**
 * Grok reauth default tab (password auth is hidden):
 * - refresh_token when RT may still work
 * - SSO cookie otherwise
 */
const grokInitialInputMethod = computed<AuthInputMethod>(() => {
  if (!isGrok.value) return 'manual'
  const creds = (props.account?.credentials || {}) as Record<string, unknown>
  const hasRT =
    (typeof creds.refresh_token === 'string' && creds.refresh_token.trim() !== '') ||
    (typeof creds.has_refresh_token === 'boolean' && creds.has_refresh_token)
  if (hasRT) return 'refresh_token'
  return 'sso_cookie'
})

// Computed - current OAuth state based on platform
const currentAuthUrl = computed(() => {
  if (isOpenAILike.value) return openaiOAuth.authUrl.value
  if (isGemini.value) return geminiOAuth.authUrl.value
  if (isAntigravity.value) return antigravityOAuth.authUrl.value
  if (isGrok.value) return grokOAuth.authUrl.value
  return claudeOAuth.authUrl.value
})
const currentSessionId = computed(() => {
  if (isOpenAILike.value) return openaiOAuth.sessionId.value
  if (isGemini.value) return geminiOAuth.sessionId.value
  if (isAntigravity.value) return antigravityOAuth.sessionId.value
  if (isGrok.value) return grokOAuth.sessionId.value
  return claudeOAuth.sessionId.value
})
const currentLoading = computed(() => {
  if (isOpenAILike.value) return openaiOAuth.loading.value
  if (isGemini.value) return geminiOAuth.loading.value
  if (isAntigravity.value) return antigravityOAuth.loading.value
  if (isGrok.value) return grokOAuth.loading.value
  return claudeOAuth.loading.value
})
const currentError = computed(() => {
  if (isOpenAILike.value) return openaiOAuth.error.value
  if (isGemini.value) return geminiOAuth.error.value
  if (isAntigravity.value) return antigravityOAuth.error.value
  if (isGrok.value) return grokOAuth.error.value
  return claudeOAuth.error.value
})

// Computed — footer "complete auth" only for code-exchange flows, not SSO/password/RT.
const isManualInputMethod = computed(() => {
  const method = oauthFlowRef.value?.inputMethod
  if (method === 'sso_cookie' || method === 'email_password' || method === 'refresh_token') {
    return false
  }
  // OpenAI/Gemini/Antigravity/Grok use manual code paste by default (no cookie auth)
  return (
    isOpenAILike.value ||
    isGemini.value ||
    isAntigravity.value ||
    isGrok.value ||
    method === 'manual'
  )
})

const canExchangeCode = computed(() => {
  const authCode = oauthFlowRef.value?.authCode || ''
  const sessionId = currentSessionId.value
  const loading = currentLoading.value
  return authCode.trim() && sessionId && !loading
})

// Watchers
watch(
  () => props.show,
  (newVal) => {
    if (newVal && props.account) {
      // Initialize addMethod based on current account type (Claude only)
      if (
        isAnthropic.value &&
        (props.account.type === 'oauth' || props.account.type === 'setup-token')
      ) {
        addMethod.value = props.account.type as AddMethod
      }
      if (isGemini.value) {
        const creds = (props.account.credentials || {}) as Record<string, unknown>
        geminiOAuthType.value =
          creds.oauth_type === 'google_one'
            ? 'google_one'
            : creds.oauth_type === 'ai_studio'
              ? 'ai_studio'
              : 'code_assist'
      }
      if (isWorkBuddy.value) {
        workbuddySite.value = extractWorkBuddySite(props.account.credentials, props.account.extra)
        workbuddyOAuthCredentials.value = extractWorkBuddyOAuthDraft(props.account.credentials, workbuddySite.value)
        workbuddyAuthorizing.value = false
        workbuddySubmitting.value = false
      }
    } else {
      resetState()
    }
  }
)

// Methods
const resetState = () => {
  addMethod.value = 'oauth'
  geminiOAuthType.value = 'code_assist'
  workbuddySite.value = 'cn'
  workbuddyAuthorizing.value = false
  workbuddySubmitting.value = false
  workbuddyOAuthCredentials.value = extractWorkBuddyOAuthDraft(undefined, 'cn')
  claudeOAuth.resetState()
  openaiOAuth.resetState()
  geminiOAuth.resetState()
  antigravityOAuth.resetState()
  grokOAuth.resetState()
  oauthFlowRef.value?.reset()
}

const handleClose = () => {
  if (confirmingAuthorization.value) return
  void cancelAuthorizationReview()
  emit('close')
}

const handleWorkBuddyReauth = async () => {
  if (!props.account || !isWorkBuddy.value || workbuddySubmitting.value) return
  if (!workbuddyOAuthCredentials.value.access_token.trim()) {
    appStore.showError(t('admin.accounts.workbuddy.oauth.accessTokenRequired'))
    return
  }
  workbuddySubmitting.value = true
  try {
    const credentials = buildWorkBuddyCredentials({
      mode: 'oauth',
      site: workbuddySite.value,
      oauthCredentials: workbuddyOAuthCredentials.value,
      existing: props.account.credentials as Record<string, unknown> | undefined
    })
    const updatedAccount = await adminAPI.accounts.update(props.account.id, { credentials })
    appStore.showSuccess(t('admin.accounts.reAuthorizedSuccess'))
    emit('reauthorized', updatedAccount)
    emit('close')
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accounts.oauth.authFailed')))
  } finally {
    workbuddySubmitting.value = false
  }
}

const handleGenerateUrl = async () => {
  if (!props.account) return

  if (isOpenAILike.value) {
    await openaiOAuth.generateAuthUrl(accountOAuthProxy.value)
  } else if (isGemini.value) {
    const creds = (props.account.credentials || {}) as Record<string, unknown>
    const tierId = typeof creds.tier_id === 'string' ? creds.tier_id : undefined
    const projectId = geminiOAuthType.value === 'code_assist' ? oauthFlowRef.value?.projectId : undefined
    await geminiOAuth.generateAuthUrl(accountOAuthProxy.value, projectId, geminiOAuthType.value, tierId)
  } else if (isAntigravity.value) {
    await antigravityOAuth.generateAuthUrl(accountOAuthProxy.value)
  } else if (isGrok.value) {
    await grokOAuth.generateAuthUrl(accountOAuthProxy.value)
  } else {
    await claudeOAuth.generateAuthUrl(addMethod.value, accountOAuthProxy.value)
  }
}

const handleExchangeCode = async () => {
  if (!props.account) return

  const authCode = oauthFlowRef.value?.authCode || ''
  if (!authCode.trim()) return

  if (isOpenAILike.value) {
    // OpenAI OAuth flow
    const oauthClient = openaiOAuth
    const sessionId = oauthClient.sessionId.value
    if (!sessionId) return
    const stateToUse = (oauthFlowRef.value?.oauthState || oauthClient.oauthState.value || '').trim()
    if (!stateToUse) {
      oauthClient.error.value = t('admin.accounts.oauth.authFailed')
      appStore.showError(oauthClient.error.value)
      return
    }

    const tokenInfo = await oauthClient.exchangeAuthCode(
      authCode.trim(),
      sessionId,
      stateToUse,
      accountOAuthProxy.value
    )
    if (!tokenInfo) return

    // Build credentials and extra info
    const credentials = credentialsFromAuthorization(tokenInfo, () => oauthClient.buildCredentials(tokenInfo))
    const extra = oauthClient.buildExtraInfo(tokenInfo)

    try {
      await stageReauthorization(props.account.id, {
        type: 'oauth',
        credentials,
        extra
      })
    } catch (error: unknown) {
      if (error === reauthorizationPending) return
      oauthClient.error.value = extractApiErrorMessage(error, '') || t('admin.accounts.oauth.authFailed')
      appStore.showError(oauthClient.error.value)
    }
  } else if (isGemini.value) {
    const sessionId = geminiOAuth.sessionId.value
    if (!sessionId) return

    const stateFromInput = oauthFlowRef.value?.oauthState || ''
    const stateToUse = stateFromInput || geminiOAuth.state.value
    if (!stateToUse) return

    const tokenInfo = await geminiOAuth.exchangeAuthCode({
      code: authCode.trim(),
      sessionId,
      state: stateToUse,
      proxyId: accountOAuthProxy.value,
      oauthType: geminiOAuthType.value,
      tierId: typeof props.account.credentials?.tier_id === 'string' ? (props.account.credentials.tier_id as string) : undefined
    })
    if (!tokenInfo) return

    const credentials = credentialsFromAuthorization(tokenInfo, () => geminiOAuth.buildCredentials(tokenInfo))

    try {
      await stageReauthorization(props.account.id, { type: 'oauth', credentials })
    } catch (error: unknown) {
      if (error === reauthorizationPending) return
      geminiOAuth.error.value = extractApiErrorMessage(error, '') || t('admin.accounts.oauth.authFailed')
      appStore.showError(geminiOAuth.error.value)
    }
  } else if (isAntigravity.value) {
    // Antigravity OAuth flow
    const sessionId = antigravityOAuth.sessionId.value
    if (!sessionId) return

    const stateFromInput = oauthFlowRef.value?.oauthState || ''
    const stateToUse = stateFromInput || antigravityOAuth.state.value
    if (!stateToUse) return

    const tokenInfo = await antigravityOAuth.exchangeAuthCode({
      code: authCode.trim(),
      sessionId,
      state: stateToUse,
      proxyId: accountOAuthProxy.value
    })
    if (!tokenInfo) return

    const credentials = credentialsFromAuthorization(tokenInfo, () => antigravityOAuth.buildCredentials(tokenInfo))

    try {
      await stageReauthorization(props.account.id, { type: 'oauth', credentials })
    } catch (error: unknown) {
      if (error === reauthorizationPending) return
      antigravityOAuth.error.value = extractApiErrorMessage(error, '') || t('admin.accounts.oauth.authFailed')
      appStore.showError(antigravityOAuth.error.value)
    }
  } else if (isGrok.value) {
    const sessionId = grokOAuth.sessionId.value
    if (!sessionId) return

    const stateFromInput = oauthFlowRef.value?.oauthState || ''
    const stateToUse = stateFromInput || grokOAuth.state.value
    if (!stateToUse) return

    const tokenInfo = await grokOAuth.exchangeAuthCode({
      code: authCode.trim(),
      sessionId,
      state: stateToUse,
      proxyId: accountOAuthProxy.value
    })
    if (!tokenInfo) return

    const credentials = credentialsFromAuthorization(tokenInfo, () => grokOAuth.buildCredentials(tokenInfo))
    const extra = grokOAuth.buildExtraInfo(tokenInfo)

    try {
      await stageReauthorization(props.account.id, {
        type: 'oauth',
        credentials,
        extra
      })
    } catch (error: unknown) {
      if (error === reauthorizationPending) return
      grokOAuth.error.value = extractApiErrorMessage(error, '') || t('admin.accounts.oauth.authFailed')
      appStore.showError(grokOAuth.error.value)
    }
  } else {
    // Claude OAuth flow
    const sessionId = claudeOAuth.sessionId.value
    if (!sessionId) return

    claudeOAuth.loading.value = true
    claudeOAuth.error.value = ''

    try {
      const proxyConfig = oauthProxyPayload(accountOAuthProxy.value)
      const endpoint =
        addMethod.value === 'oauth'
          ? '/admin/accounts/exchange-code'
          : '/admin/accounts/exchange-setup-token-code'

      const tokenInfo = await adminAPI.accounts.exchangeCode(endpoint, {
        session_id: sessionId,
        code: authCode.trim(),
        ...proxyConfig
      })

      const extra = claudeOAuth.buildExtraInfo(tokenInfo)

      await stageReauthorization(props.account.id, {
        type: addMethod.value as 'oauth' | 'setup-token',
        credentials: tokenInfo as unknown as Record<string, unknown>,
        extra
      })
    } catch (error: unknown) {
      if (error === reauthorizationPending) return
      claudeOAuth.error.value = extractApiErrorMessage(error, '') || t('admin.accounts.oauth.authFailed')
      appStore.showError(claudeOAuth.error.value)
    } finally {
      claudeOAuth.loading.value = false
    }
  }
}

const handleCookieAuth = async (sessionKey: string) => {
  if (!props.account || isOpenAILike.value) return

  claudeOAuth.loading.value = true
  claudeOAuth.error.value = ''

  try {
    const proxyConfig = oauthProxyPayload(accountOAuthProxy.value)
    const endpoint =
      addMethod.value === 'oauth'
        ? '/admin/accounts/cookie-auth'
        : '/admin/accounts/setup-token-cookie-auth'

    const tokenInfo = await adminAPI.accounts.exchangeCode(endpoint, {
      session_id: '',
      code: sessionKey.trim(),
      ...proxyConfig
    })

    const extra = claudeOAuth.buildExtraInfo(tokenInfo)

    await stageReauthorization(props.account.id, {
      type: addMethod.value as 'oauth' | 'setup-token',
      credentials: tokenInfo as unknown as Record<string, unknown>,
      extra
    })
  } catch (error: unknown) {
      if (error === reauthorizationPending) return
    claudeOAuth.error.value =
      extractApiErrorMessage(error, '') || t('admin.accounts.oauth.cookieAuthFailed')
  } finally {
    claudeOAuth.loading.value = false
  }
}

/** Apply Grok Build OAuth tokens onto the existing account (never store password/SSO). */
const applyGrokReauthTokenInfo = async (tokenInfo: Parameters<typeof grokOAuth.buildCredentials>[0]) => {
  if (!props.account) return
  const credentials = credentialsFromAuthorization(tokenInfo, () => grokOAuth.buildCredentials(tokenInfo))
  const extra = grokOAuth.buildExtraInfo(tokenInfo)
  await stageReauthorization(props.account.id, {
    type: 'oauth',
    credentials,
    extra
  })
}

/** Re-auth the existing account with one refresh token. */
const handleValidateRefreshToken = async (refreshTokenInput: string) => {
  if (!props.account) return
  if (isGrok.value) {
    await handleGrokValidateRefreshToken(refreshTokenInput)
    return
  }

  const refreshToken = refreshTokenInput
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)[0]
  if (!refreshToken) return

  if (isOpenAILike.value) {
    openaiOAuth.loading.value = true
    openaiOAuth.error.value = ''
    try {
      const tokenInfo = await openaiOAuth.validateRefreshToken(refreshToken, accountOAuthProxy.value)
      if (!tokenInfo) return

      await stageReauthorization(props.account.id, {
        type: 'oauth',
        credentials: credentialsFromAuthorization(tokenInfo, () => openaiOAuth.buildCredentials(tokenInfo)),
        extra: openaiOAuth.buildExtraInfo(tokenInfo)
      })
    } catch (error: unknown) {
      if (error === reauthorizationPending) return
      openaiOAuth.error.value =
        extractApiErrorMessage(error, '') ||
        t('admin.accounts.oauth.authFailed')
      appStore.showError(openaiOAuth.error.value)
    } finally {
      openaiOAuth.loading.value = false
    }
    return
  }

  if (!isAntigravity.value) return
  antigravityOAuth.loading.value = true
  antigravityOAuth.error.value = ''
  try {
    const tokenInfo = await antigravityOAuth.validateRefreshToken(refreshToken, accountOAuthProxy.value)
    if (!tokenInfo) return

    await stageReauthorization(props.account.id, {
      type: 'oauth',
      credentials: credentialsFromAuthorization(tokenInfo, () => antigravityOAuth.buildCredentials(tokenInfo, refreshToken))
    })
  } catch (error: unknown) {
      if (error === reauthorizationPending) return
    antigravityOAuth.error.value =
      extractApiErrorMessage(error, '') ||
      t('admin.accounts.oauth.authFailed')
    appStore.showError(antigravityOAuth.error.value)
  } finally {
    antigravityOAuth.loading.value = false
  }
}

/** Re-auth with a single SSO cookie → Build OAuth (not batch create). */
const handleGrokImportSSO = async (ssoInput: string) => {
  if (!props.account || !isGrok.value) return
  const ssoToken = ssoInput
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)[0]
  if (!ssoToken) return

  grokOAuth.loading.value = true
  grokOAuth.error.value = ''
  try {
    const tokenInfo = await grokOAuth.validateSSOToken(ssoToken, accountOAuthProxy.value)
    if (!tokenInfo) return
    await applyGrokReauthTokenInfo(tokenInfo)
  } catch (error: unknown) {
      if (error === reauthorizationPending) return
    grokOAuth.error.value =
      extractApiErrorMessage(error, '') ||
      t('admin.accounts.oauth.grok.failedToValidateSSO', 'Failed to validate Grok SSO')
    appStore.showError(grokOAuth.error.value)
  } finally {
    grokOAuth.loading.value = false
  }
}

/** Re-auth with a single refresh token. */
const handleGrokValidateRefreshToken = async (refreshTokenInput: string) => {
  if (!props.account || !isGrok.value) return
  const refreshToken = refreshTokenInput
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)[0]
  if (!refreshToken) {
    grokOAuth.error.value = t('admin.accounts.oauth.grok.pleaseEnterRefreshToken')
    return
  }

  grokOAuth.loading.value = true
  grokOAuth.error.value = ''
  try {
    const tokenInfo = await grokOAuth.validateRefreshToken(refreshToken, accountOAuthProxy.value)
    if (!tokenInfo) return
    await applyGrokReauthTokenInfo(tokenInfo)
  } catch (error: unknown) {
      if (error === reauthorizationPending) return
    grokOAuth.error.value =
      extractApiErrorMessage(error, '') ||
      t('admin.accounts.oauth.grok.failedToValidateRT')
    appStore.showError(grokOAuth.error.value)
  } finally {
    grokOAuth.loading.value = false
  }
}
</script>
