<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  WORKBUDDY_SITES,
  defaultWorkBuddyBaseUrl,
  defaultWorkBuddyDomain,
  type WorkBuddyOAuthCredentials,
  type WorkBuddySite
} from '@/api/admin/workbuddy'
import type { WorkBuddyOAuthDraft } from './credentialsBuilder'
import { useWorkBuddyOAuth } from '@/composables/useWorkBuddyOAuth'
import type { OAuthProxySelection } from '@/utils/accountProxy'
import Icon from '@/components/icons/Icon.vue'

const props = withDefaults(
  defineProps<{
    mode: 'oauth' | 'apikey'
    editing?: boolean
    proxyId?: number | null
    proxyGroupId?: number | null
    hideSiteSelector?: boolean
    hideApiKeyFields?: boolean
    hideManualFields?: boolean
  }>(),
  {
    editing: false,
    proxyId: null,
    proxyGroupId: null,
    hideSiteSelector: false,
    hideApiKeyFields: false,
    hideManualFields: false
  }
)

const emit = defineEmits<{
  'suggested-name': [name: string]
  'oauth-completed': [payload: { site: WorkBuddySite; accountName?: string; credentials: WorkBuddyOAuthCredentials }]
  busy: [value: boolean]
}>()

const site = defineModel<WorkBuddySite>('site', { default: 'cn' })
const oauthCredentials = defineModel<WorkBuddyOAuthDraft>('oauthCredentials', {
  default: () => ({
    access_token: '',
    refresh_token: '',
    uid: '',
    domain: defaultWorkBuddyDomain('cn'),
    expires_at: ''
  })
})
const apiKey = defineModel<string>('apiKey', { default: '' })
const baseUrl = defineModel<string>('baseUrl', { default: '' })

const { t } = useI18n()
const oauth = useWorkBuddyOAuth()
const copied = ref(false)

const siteOptions = computed(() =>
  (['cn', 'global'] as const).map((value) => ({
    value,
    label: t(`admin.accounts.workbuddy.sites.${value}.label`),
    subtitle: t(`admin.accounts.workbuddy.sites.${value}.subtitle`),
    domain: WORKBUDDY_SITES[value].domain
  }))
)

const proxySelection = computed<OAuthProxySelection>(() => {
  if (props.proxyGroupId) return { proxy_group_id: props.proxyGroupId }
  return props.proxyId ?? null
})

const activeSite = computed<WorkBuddySite>(() => site.value ?? 'cn')
const baseUrlPlaceholder = computed(() => defaultWorkBuddyBaseUrl(activeSite.value))
const domainPlaceholder = computed(() => defaultWorkBuddyDomain(activeSite.value))

watch(
  () => oauth.loading.value,
  (busy) => emit('busy', busy),
  { immediate: true }
)

watch(
  () => props.mode,
  (nextMode) => {
    if (nextMode !== 'oauth') {
      oauth.cancel()
    }
  }
)

watch(site, (nextSite, prevSite) => {
  const prevDefaultDomain = prevSite ? defaultWorkBuddyDomain(prevSite) : ''
  const resolvedSite: WorkBuddySite = nextSite ?? 'cn'
  const currentDomain = (oauthCredentials.value?.domain || '').trim()
  if (!currentDomain || currentDomain === prevDefaultDomain) {
    oauthCredentials.value = {
      access_token: oauthCredentials.value?.access_token ?? '',
      refresh_token: oauthCredentials.value?.refresh_token ?? '',
      uid: oauthCredentials.value?.uid ?? '',
      expires_at: oauthCredentials.value?.expires_at ?? '',
      refresh_expires_at: oauthCredentials.value?.refresh_expires_at,
      enterprise_id: oauthCredentials.value?.enterprise_id,
      domain: defaultWorkBuddyDomain(resolvedSite)
    }
  }
})

const selectSite = (nextSite: WorkBuddySite) => {
  if (oauth.loading.value) return
  site.value = nextSite
}

const handleStartLogin = async () => {
  copied.value = false
  await oauth.startLogin(activeSite.value, proxySelection.value, {
    onCompleted: (response) => {
      const creds = response.credentials
      if (creds) {
        const resolvedSite = creds.site || activeSite.value
        site.value = resolvedSite
        oauthCredentials.value = {
          access_token: creds.access_token || '',
          refresh_token: creds.refresh_token || '',
          uid: creds.uid || '',
          domain: creds.domain || defaultWorkBuddyDomain(resolvedSite),
          expires_at: creds.expires_at || '',
          refresh_expires_at: creds.refresh_expires_at,
          enterprise_id: creds.enterprise_id
        }
        emit('oauth-completed', {
          site: resolvedSite,
          accountName: response.account_name,
          credentials: creds
        })
      }
      if (response.account_name?.trim()) {
        emit('suggested-name', response.account_name.trim())
      }
    }
  })
}

const handleCopyUrl = async () => {
  if (!oauth.authUrl.value) return
  try {
    await navigator.clipboard.writeText(oauth.authUrl.value)
    copied.value = true
  } catch {
    copied.value = false
  }
}

const statusMessage = computed(() => {
  if (oauth.status.value === 'failed' && oauth.error.value) {
    return oauth.error.value
  }
  if (oauth.status.value === 'idle') return ''
  return t(`admin.accounts.workbuddy.oauth.status.${oauth.status.value}`)
})

const statusClass = computed(() => {
  switch (oauth.status.value) {
    case 'completed':
      return 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
    case 'failed':
    case 'timeout':
      return 'border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-300'
    case 'cancelled':
      return 'border-[var(--border)] bg-[var(--surface-2)] text-[var(--muted-foreground)]'
    default:
      return 'border-emerald-500/30 bg-emerald-500/5 text-[var(--foreground)]'
  }
})
</script>

<template>
  <div class="space-y-4">
    <!-- Site selector -->
    <div v-if="!hideSiteSelector" class="space-y-2">
      <label class="input-label mb-0">{{ t('admin.accounts.workbuddy.siteLabel') }}</label>
      <p class="input-hint">{{ t('admin.accounts.workbuddy.siteHint') }}</p>
      <div
        class="grid grid-cols-1 gap-2.5 sm:grid-cols-2"
        role="radiogroup"
        :aria-label="t('admin.accounts.workbuddy.siteLabel')"
        data-testid="workbuddy-site-selector"
      >
        <button
          v-for="option in siteOptions"
          :key="option.value"
          type="button"
          role="radio"
          :aria-checked="site === option.value"
          :data-testid="`workbuddy-site-${option.value}`"
          :disabled="oauth.loading.value"
          :class="[
            'flex items-center gap-3 rounded-lg border-2 p-3 text-left transition-all disabled:cursor-not-allowed disabled:opacity-60',
            site === option.value
              ? 'border-emerald-500 bg-emerald-50 dark:bg-emerald-900/20'
              : 'border-[var(--border)] hover:border-emerald-400'
          ]"
          @click="selectSite(option.value)"
        >
          <div
            :class="[
              'flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-xs font-bold',
              site === option.value
                ? 'bg-emerald-500 text-white'
                : 'bg-[var(--surface-2)] text-[var(--muted-foreground)]'
            ]"
          >
            {{ option.value.toUpperCase() }}
          </div>
          <div class="min-w-0">
            <span class="block text-sm font-medium text-[var(--foreground)]">{{ option.label }}</span>
            <span class="block truncate font-mono text-xs text-[var(--muted-foreground)]">{{ option.subtitle }}</span>
          </div>
        </button>
      </div>
    </div>

    <!-- OAuth flow -->
    <template v-if="mode === 'oauth'">
      <div class="space-y-3 rounded-lg border border-[var(--border)] bg-[var(--card)] p-3.5">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <div>
            <h4 class="text-sm font-semibold text-[var(--foreground)]">
              {{ t('admin.accounts.workbuddy.oauth.title') }}
            </h4>
            <p class="input-hint mt-0.5">{{ t('admin.accounts.workbuddy.oauth.hint') }}</p>
          </div>
          <div class="flex items-center gap-2">
            <button
              type="button"
              class="btn btn-primary btn-sm"
              data-testid="workbuddy-oauth-start"
              :disabled="oauth.loading.value"
              @click="handleStartLogin"
            >
              <Icon name="link" size="xs" class="mr-1.5" />
              {{ t('admin.accounts.workbuddy.oauth.startLogin') }}
            </button>
            <button
              v-if="oauth.loading.value"
              type="button"
              class="btn btn-secondary btn-sm"
              data-testid="workbuddy-oauth-cancel"
              @click="oauth.cancel"
            >
              {{ t('common.cancel') }}
            </button>
          </div>
        </div>

        <div v-if="oauth.authUrl.value" class="space-y-2">
          <a
            :href="oauth.authUrl.value"
            target="_blank"
            rel="noopener noreferrer"
            data-testid="workbuddy-oauth-url"
            class="block break-all rounded-md border border-[var(--border)] bg-[var(--surface-2)] p-2 font-mono text-xs text-emerald-700 underline hover:text-emerald-600 dark:text-emerald-300"
          >
            {{ oauth.authUrl.value }}
          </a>
          <div class="flex flex-wrap items-center gap-2">
            <a
              :href="oauth.authUrl.value"
              target="_blank"
              rel="noopener noreferrer"
              class="btn btn-secondary btn-xs"
            >
              {{ t('admin.accounts.workbuddy.oauth.openLoginLink') }}
            </a>
            <button
              type="button"
              class="btn btn-secondary btn-xs"
              data-testid="workbuddy-oauth-copy"
              @click="handleCopyUrl"
            >
              {{ t(copied ? 'admin.accounts.workbuddy.oauth.copied' : 'admin.accounts.workbuddy.oauth.copyUrl') }}
            </button>
          </div>
        </div>

        <div
          v-if="statusMessage"
          role="status"
          aria-live="polite"
          data-testid="workbuddy-oauth-status"
          :class="['rounded-md border px-3 py-2 text-xs font-medium', statusClass]"
        >
          {{ statusMessage }}
        </div>
      </div>

      <!-- Manual OAuth token import fields -->
      <fieldset
        v-if="!hideManualFields"
        class="min-w-0 space-y-3 rounded-lg border border-[var(--border)] bg-[var(--card)] p-3.5"
      >
        <div>
          <h4 class="text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
            {{ t('admin.accounts.workbuddy.oauth.manualSectionTitle') }}
          </h4>
          <p class="input-hint mt-0.5">{{ t('admin.accounts.workbuddy.oauth.manualHint') }}</p>
        </div>

        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <label class="block sm:col-span-2">
            <span class="input-label">{{ t('admin.accounts.workbuddy.oauth.fields.accessToken') }}</span>
            <input
              v-model="oauthCredentials.access_token"
              type="password"
              autocomplete="new-password"
              class="input font-mono"
              data-testid="workbuddy-access-token"
              :placeholder="editing ? t('admin.accounts.workbuddy.oauth.keepTokenPlaceholder') : 'wb_at_...'"
            />
          </label>

          <label class="block sm:col-span-2">
            <span class="input-label">{{ t('admin.accounts.workbuddy.oauth.fields.refreshToken') }}</span>
            <input
              v-model="oauthCredentials.refresh_token"
              type="password"
              autocomplete="new-password"
              class="input font-mono"
              data-testid="workbuddy-refresh-token"
              :placeholder="editing ? t('admin.accounts.workbuddy.oauth.keepTokenPlaceholder') : 'wb_rt_...'"
            />
          </label>

          <label class="block">
            <span class="input-label">{{ t('admin.accounts.workbuddy.oauth.fields.uid') }}</span>
            <input
              v-model="oauthCredentials.uid"
              type="text"
              class="input font-mono"
              data-testid="workbuddy-uid"
              placeholder="uid"
            />
          </label>

          <label class="block">
            <span class="input-label">{{ t('admin.accounts.workbuddy.oauth.fields.domain') }}</span>
            <input
              v-model="oauthCredentials.domain"
              type="text"
              class="input font-mono"
              data-testid="workbuddy-domain"
              :placeholder="domainPlaceholder"
            />
          </label>

          <label class="block sm:col-span-2">
            <span class="input-label">{{ t('admin.accounts.workbuddy.oauth.fields.expiresAt') }}</span>
            <input
              v-model="oauthCredentials.expires_at"
              type="text"
              class="input font-mono"
              data-testid="workbuddy-expires-at"
              placeholder="2026-12-31T23:59:59Z"
            />
          </label>
        </div>
      </fieldset>
    </template>

    <!-- API Key mode -->
    <div v-else-if="!hideApiKeyFields" class="space-y-3">
      <label class="block">
        <span class="input-label">{{ t('admin.accounts.apiKeyRequired') }}</span>
        <input
          v-model="apiKey"
          data-account-field="apiKeyValue"
          data-testid="workbuddy-api-key"
          type="password"
          autocomplete="new-password"
          :required="!editing"
          class="input font-mono"
          :placeholder="editing ? t('admin.accounts.leaveEmptyToKeep') : t('admin.accounts.workbuddy.apikey.apiKeyPlaceholder')"
        />
      </label>

      <label class="block">
        <span class="input-label">{{ t('admin.accounts.workbuddy.apikey.baseUrlLabel') }}</span>
        <input
          v-model="baseUrl"
          data-account-field="apiKeyBaseUrl"
          data-testid="workbuddy-base-url"
          type="url"
          class="input font-mono"
          :placeholder="baseUrlPlaceholder"
        />
        <span class="input-hint">{{ t('admin.accounts.workbuddy.apikey.baseUrlHint') }}</span>
      </label>
    </div>
  </div>
</template>
