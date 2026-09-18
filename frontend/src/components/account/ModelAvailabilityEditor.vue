<script setup lang="ts">
import { computed, onUnmounted, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { modelAvailabilityAPI, type ChangeModelAvailability, type ModelAvailabilityDeclaration, type ModelAvailabilityState } from '@/api/admin/modelAvailability'
import { extractApiErrorCode } from '@/utils/apiError'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'

const props = defineProps<{
  accountId: number
  active: boolean
  configurationDirty: boolean
  connectionContext: unknown
  savedRevision: number
}>()
const emit = defineEmits<{ changed: [] }>()
const { t } = useI18n()
const fieldId = useId()
const opened = ref(false)
const state = ref<ModelAvailabilityState | null>(null)
const busy = ref(false)
const errorKey = ref('')
const model = ref('')
const endpoint = ref('chat_completions')
const reason = ref('')
const expiresAt = ref('')
const pending = ref<{ action: 'declare' | 'revoke'; request: ChangeModelAvailability } | null>(null)
const endpoints = ['chat_completions', 'responses', 'messages', 'generate_content', 'images', 'videos']
let generation = 0
const available = computed(() => props.active && !props.configurationDirty && !busy.value && !!state.value)
const validExpiry = computed(() => !expiresAt.value || new Date(expiresAt.value).getTime() > Date.now())
const canDeclare = computed(() => available.value && !!model.value.trim() && !/[*?]/.test(model.value) && !!reason.value.trim() && validExpiry.value)

watch(() => JSON.stringify([props.accountId, props.active, props.configurationDirty, props.connectionContext, props.savedRevision]), () => {
  generation++
  state.value = null
  pending.value = null
  busy.value = false
  errorKey.value = ''
  model.value = ''
  endpoint.value = 'chat_completions'
  reason.value = ''
  expiresAt.value = ''
  opened.value = false
}, { flush: 'sync' })
onUnmounted(() => { generation++ })

async function load() {
  if (!props.active || props.configurationDirty || busy.value) return
  opened.value = true
  state.value = null
  pending.value = null
  const requestGeneration = ++generation
  busy.value = true
  errorKey.value = ''
  try {
    const result = await modelAvailabilityAPI.get(props.accountId)
    if (requestGeneration === generation) state.value = result
  } catch {
    if (requestGeneration === generation) errorKey.value = 'loadFailed'
  } finally {
    if (requestGeneration === generation) busy.value = false
  }
}

function prepare(action: 'declare' | 'revoke', declaration?: ModelAvailabilityDeclaration) {
  if (!available.value || !state.value || !reason.value.trim() || (action === 'declare' && !canDeclare.value)) return
  pending.value = { action, request: {
    identity_revision: state.value.identity_revision,
    rules_revision: state.value.rules_revision,
    snapshot_revision: state.value.snapshot_revision,
    model: declaration?.model ?? model.value.trim(),
    endpoint: declaration?.endpoint ?? endpoint.value,
    reason: reason.value.trim(),
    confirmed: true,
    ...(action === 'declare' && expiresAt.value ? { expires_at: new Date(expiresAt.value).toISOString() } : {})
  } }
}

async function confirm() {
  if (!pending.value || !available.value) return
  const change = pending.value
  pending.value = null
  const requestGeneration = ++generation
  busy.value = true
  errorKey.value = ''
  try {
    const result = await modelAvailabilityAPI[change.action](props.accountId, change.request)
    if (requestGeneration !== generation) return
    state.value = result
    reason.value = ''
    emit('changed')
  } catch (error) {
    if (requestGeneration !== generation) return
    const status = (error as { status?: number; response?: { status?: number } })?.status ?? (error as { response?: { status?: number } })?.response?.status
    const conflict = status === 409 || extractApiErrorCode(error) === 'model_availability_conflict'
    errorKey.value = conflict ? 'conflict' : 'saveFailed'
    // A conflict cannot reuse the old CAS; only an explicit reload enables a new confirmation.
    if (conflict) state.value = null
  } finally {
    if (requestGeneration === generation) busy.value = false
  }
}

function status(declaration: ModelAvailabilityDeclaration) {
  if (declaration.revoked_at) return 'revoked'
  if (declaration.expires_at && new Date(declaration.expires_at).getTime() <= Date.now()) return 'expired'
  return declaration.valid ? 'valid' : 'invalid'
}
</script>

<template>
  <section class="space-y-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600" data-testid="model-availability-editor">
    <h3 class="font-medium">{{ t('admin.accounts.modelAvailability.title') }}</h3>
    <p class="input-hint">{{ t('admin.accounts.modelAvailability.hint') }}</p>
    <p v-if="configurationDirty" role="status">{{ t('admin.accounts.modelSync.saveBeforePreview') }}</p>
    <button type="button" class="btn btn-secondary" data-testid="load-availability" :disabled="!active || configurationDirty || busy" @click="load">{{ t('admin.accounts.modelAvailability.load') }}</button>
    <p v-if="busy" role="status">{{ t('common.loading') }}</p>
    <p v-if="errorKey" role="alert">{{ t(`admin.accounts.modelAvailability.${errorKey}`) }}</p>
    <div v-if="opened && state" class="space-y-3">
      <p v-if="!state.declarations.length" role="status">{{ t('admin.accounts.modelAvailability.empty') }}</p>
      <label :for="`${fieldId}-reason`" class="input-label">{{ t('admin.accounts.modelAvailability.reason') }}</label>
      <textarea :id="`${fieldId}-reason`" v-model="reason" class="input" data-testid="availability-reason" :disabled="busy" />
      <ul class="space-y-2">
        <li v-for="declaration in state.declarations" :key="`${declaration.model}:${declaration.endpoint}`" class="space-y-1 break-words rounded border p-2" data-testid="availability-evidence">
          <p class="break-all">{{ declaration.model }} · {{ declaration.endpoint }}</p>
          <p>{{ t(`admin.accounts.modelAvailability.${declaration.evidence_kind}`) }} · {{ t(`admin.accounts.modelAvailability.${status(declaration)}`) }}</p>
          <p>{{ declaration.reason }}</p>
          <p>{{ t('admin.accounts.modelAvailability.confirmedAt') }}: {{ declaration.confirmed_at }}</p>
          <p v-if="declaration.expires_at">{{ t('admin.accounts.modelAvailability.expiry') }}: {{ declaration.expires_at }}</p>
          <button v-if="!declaration.revoked_at" type="button" class="btn btn-secondary" data-testid="revoke-availability" :disabled="!available || !reason.trim()" @click="prepare('revoke', declaration)">{{ t('admin.accounts.modelAvailability.revoke') }}</button>
        </li>
      </ul>
      <label :for="`${fieldId}-model`" class="input-label">{{ t('admin.accounts.modelAvailability.model') }}</label>
      <input :id="`${fieldId}-model`" v-model="model" class="input" data-testid="availability-model" :disabled="busy" />
      <label :for="`${fieldId}-endpoint`" class="input-label">{{ t('admin.accounts.modelAvailability.endpoint') }}</label>
      <select :id="`${fieldId}-endpoint`" v-model="endpoint" class="input" data-testid="availability-endpoint" :disabled="busy">
        <option v-for="value in endpoints" :key="value" :value="value">{{ value }}</option>
      </select>
      <label :for="`${fieldId}-expiry`" class="input-label">{{ t('admin.accounts.modelAvailability.expiry') }}</label>
      <input :id="`${fieldId}-expiry`" v-model="expiresAt" type="datetime-local" class="input" data-testid="availability-expiry" :disabled="busy" :aria-invalid="!validExpiry" :aria-describedby="`${fieldId}-expiry-hint`" />
      <p :id="`${fieldId}-expiry-hint`" class="input-hint">{{ t('admin.accounts.modelAvailability.expiryHint') }}</p>
      <button type="button" class="btn btn-primary" data-testid="declare-availability" :disabled="!canDeclare" @click="prepare('declare')">{{ t('admin.accounts.modelAvailability.declare') }}</button>
    </div>
    <ConfirmDialog :show="!!pending" :title="t('admin.accounts.modelAvailability.confirmTitle')" :message="t('admin.accounts.modelAvailability.confirmHint')" @confirm="confirm" @cancel="pending = null">
      <div v-if="pending" class="space-y-2 break-words" data-testid="availability-confirmation">
        <p>{{ t(`admin.accounts.modelAvailability.${pending.action}`) }} · #{{ accountId }}</p>
        <p class="break-all">{{ pending.request.model }} · {{ pending.request.endpoint }}</p>
        <p>{{ pending.request.reason }}</p>
        <p>{{ pending.request.expires_at || t('admin.accounts.modelAvailability.noExpiry') }}</p>
      </div>
    </ConfirmDialog>
  </section>
</template>
