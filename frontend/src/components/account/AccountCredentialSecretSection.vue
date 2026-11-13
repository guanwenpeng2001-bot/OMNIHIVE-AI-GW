<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  convertCredentialSecret,
  getCredentialSecret,
  revokeCredentialSecret,
  rotateCredentialSecret,
  type CredentialSecretMetadata,
} from '@/api/admin/credentialSecrets'
import { extractApiErrorCode } from '@/utils/apiError'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'

const props = defineProps<{
  accountId: number
  // Optimistic-concurrency token for convertCredentialSecret. Callers may not
  // always hold it (a payload without updated_at), so it is optional here and
  // the convert action stays unavailable until it is present.
  accountRevision?: string
  active: boolean
}>()
const emit = defineEmits<{ changed: [] }>()
const { t } = useI18n()

const metadata = ref<CredentialSecretMetadata | null>(null)
const inline = ref(false)
const busy = ref(false)
const errorKey = ref('')
const saved = ref(false)
const rotatePayload = ref('')
const pending = ref<{ action: 'convert' | 'rotate' | 'revoke'; credentials?: Record<string, unknown> } | null>(null)
let generation = 0

const revoked = computed(() => !!metadata.value?.revoked_at)
const rotateError = computed(() => {
  if (!rotatePayload.value.trim()) return ''
  try {
    const parsed = JSON.parse(rotatePayload.value)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed) || !Object.keys(parsed).length) return 'invalidPayload'
    return ''
  } catch {
    return 'invalidJSON'
  }
})
const canRotate = computed(() => !!metadata.value && !revoked.value && !busy.value && !!rotatePayload.value.trim() && !rotateError.value)
// Conversion needs the account revision as its concurrency token; without one the
// action stays unavailable instead of sending an undefined revision upstream.
const canConvert = computed(() => !!props.accountRevision && !busy.value && inline.value)

watch(() => [props.accountId, props.active] as const, () => {
  generation++
  metadata.value = null
  inline.value = false
  busy.value = false
  errorKey.value = ''
  saved.value = false
  rotatePayload.value = ''
  pending.value = null
  if (props.active && props.accountId > 0) void load()
}, { immediate: true, flush: 'sync' })
onUnmounted(() => { generation++ })

async function load() {
  const requestGeneration = ++generation
  busy.value = true
  errorKey.value = ''
  try {
    const result = await getCredentialSecret(props.accountId)
    if (requestGeneration !== generation) return
    metadata.value = result
    inline.value = false
  } catch (error) {
    if (requestGeneration !== generation) return
    if (extractApiErrorCode(error) === 'CREDENTIAL_SECRET_NOT_FOUND') {
      metadata.value = null
      inline.value = true
    } else {
      errorKey.value = 'loadFailed'
    }
  } finally {
    if (requestGeneration === generation) busy.value = false
  }
}

function prepareConvert() {
  if (!canConvert.value) return
  pending.value = { action: 'convert' }
}

function prepareRotate() {
  if (!canRotate.value) return
  pending.value = { action: 'rotate', credentials: JSON.parse(rotatePayload.value) }
}

function prepareRevoke() {
  if (busy.value || !metadata.value || revoked.value) return
  pending.value = { action: 'revoke' }
}

async function confirm() {
  if (!pending.value) return
  const change = pending.value
  pending.value = null
  const requestGeneration = ++generation
  busy.value = true
  errorKey.value = ''
  try {
    if (change.action === 'convert') {
      // No revision means no safe concurrency token; refuse rather than send an
      // undefined revision upstream. The convert action is gated on canConvert.
      if (!props.accountRevision) return
      await convertCredentialSecret(props.accountId, props.accountRevision)
    } else if (change.action === 'rotate' && metadata.value && change.credentials) {
      await rotateCredentialSecret(props.accountId, metadata.value.version, change.credentials)
    } else if (change.action === 'revoke' && metadata.value) {
      await revokeCredentialSecret(props.accountId, metadata.value.version)
    }
    if (requestGeneration !== generation) return
    rotatePayload.value = ''
    saved.value = true
    emit('changed')
    await load()
  } catch (error) {
    if (requestGeneration !== generation) return
    const code = extractApiErrorCode(error)
    if (code === 'CREDENTIAL_SECRET_CONFLICT') {
      metadata.value = null
      await load()
      errorKey.value = 'conflict'
    } else if (code === 'CREDENTIAL_SECRET_INVALID') {
      errorKey.value = 'invalidPayload'
    } else {
      errorKey.value = 'saveFailed'
    }
  } finally {
    if (requestGeneration === generation) busy.value = false
  }
}
</script>

<template>
  <section class="space-y-3 rounded-xl border border-[var(--border)] bg-[var(--surface-2)]/30 p-4 sm:p-5" data-testid="credential-secret-section">
    <h3 class="text-sm font-semibold text-[var(--foreground)]">{{ t('admin.accounts.credentialSecret.title') }}</h3>
    <p v-if="busy" role="status">{{ t('common.loading') }}</p>
    <p v-if="errorKey" role="alert">{{ t(`admin.accounts.credentialSecret.${errorKey}`) }}</p>
    <button v-if="errorKey === 'loadFailed'" type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="load">{{ t('common.tryAgain') }}</button>
    <p v-if="saved && !busy && !errorKey && !rotatePayload.trim()" role="status" class="input-hint">{{ t('admin.accounts.credentialSecret.saved') }}</p>

    <p v-if="inline && !busy" role="status">{{ t('admin.accounts.credentialSecret.inlineState') }}</p>
    <p v-if="metadata" role="status" data-testid="credential-secret-state">{{ t(revoked ? 'admin.accounts.credentialSecret.revokedState' : 'admin.accounts.credentialSecret.referenceState') }}</p>
    <details :open="!!rotateError || undefined" class="space-y-3" data-testid="credential-secret-management">
      <summary class="cursor-pointer rounded-lg py-1.5 text-sm font-medium text-[var(--foreground)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)]">{{ t('admin.accounts.editSections.credentials') }}</summary>
      <p class="input-hint">{{ t('admin.accounts.credentialSecret.hint') }}</p>
    <div v-if="inline && !busy" class="space-y-2" data-testid="credential-secret-inline">
      <p v-if="!accountRevision" role="status" data-testid="credential-secret-revision-missing">
        {{ t('admin.accounts.credentialSecret.revisionUnavailable') }}
      </p>
      <button type="button" class="btn btn-secondary" data-testid="convert-credential-secret" :disabled="!canConvert" @click="prepareConvert">
        {{ t('admin.accounts.credentialSecret.convert') }}
      </button>
    </div>

    <div v-if="metadata" class="space-y-2" data-testid="credential-secret-metadata">
      <p class="break-all">{{ t('admin.accounts.credentialSecret.reference') }}: {{ metadata.reference }}</p>
      <p>{{ t('admin.accounts.credentialSecret.version') }}: {{ metadata.version }}</p>
      <p>{{ t('admin.accounts.credentialSecret.updatedAt') }}: {{ metadata.updated_at }}</p>
      <p v-if="revoked" role="status" data-testid="credential-secret-revoked">{{ t('admin.accounts.credentialSecret.revokedState') }}</p>
      <template v-if="!revoked">
        <label for="credential-secret-rotate-payload" class="input-label">{{ t('admin.accounts.credentialSecret.rotatePayload') }}</label>
        <textarea
          id="credential-secret-rotate-payload"
          v-model="rotatePayload"
          class="input font-mono text-xs"
          rows="3"
          data-testid="rotate-credential-payload"
          :disabled="busy"
          :placeholder="t('admin.accounts.credentialSecret.rotatePlaceholder')"
        />
        <p v-if="rotateError" role="alert">{{ t(`admin.accounts.credentialSecret.${rotateError}`) }}</p>
        <div class="flex flex-wrap gap-2">
          <button type="button" class="btn btn-secondary" data-testid="rotate-credential-secret" :disabled="!canRotate" @click="prepareRotate">
            {{ t('admin.accounts.credentialSecret.rotate') }}
          </button>
          <button type="button" class="btn btn-danger" data-testid="revoke-credential-secret" :disabled="busy" @click="prepareRevoke">
            {{ t('admin.accounts.credentialSecret.revoke') }}
          </button>
        </div>
      </template>
    </div>

    </details>
    <ConfirmDialog
      :show="!!pending"
      :title="t('admin.accounts.credentialSecret.confirmTitle')"
      :message="pending ? t(`admin.accounts.credentialSecret.confirm.${pending.action}`) : ''"
      :confirm-text="t('common.confirm')"
      :cancel-text="t('common.cancel')"
      :danger="pending?.action === 'revoke'"
      @confirm="confirm"
      @cancel="pending = null"
    />
  </section>
</template>
