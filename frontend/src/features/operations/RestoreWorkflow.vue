<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { computed, onMounted, onScopeDispose, ref, watch } from 'vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { Button, Card, CopyButton, Input } from '@/components/ui'
import { isStepUpCancelled, useStepUp } from '@/composables/useStepUp'
import { useAuthStore } from '@/stores/auth'
import { classifyAsyncError, type AsyncViewState } from '@/features/shared/asyncState'
import { createIdempotencyKey, controlGet, controlPost } from '@/features/shared/controlClient'
import type { components } from '@/api/generated/openapi'
import OperationState from './OperationState.vue'

type Preview = components['schemas']['RestorePreview']
type Operation = components['schemas']['RestoreOperation']
type Target = Operation['target']
type ExecuteBody = components['schemas']['RestoreExecuteInput']
type RestoreCheckpoint = { operationId: string } | { body: ExecuteBody; key: string }

const props = defineProps<{ backupId: string }>()
const emit = defineEmits<{ lock: [locked: boolean] }>()
const { t } = useI18n()
const auth = useAuthStore()
const stepUp = useStepUp()
const target = ref<Target>('isolated')
const digest = ref('')
const rehearsal = ref('')
const rehearsalId = ref('')
const operationId = ref('')
const preview = ref<Preview | null>(null)
const operation = ref<Operation | null>(null)
const state = ref<AsyncViewState>('ready')
const message = ref('')
const busy = ref(false)
const unresolved = ref(false)
const confirming = ref(false)
const pending = ref<{ body: ExecuteBody; key: string } | null>(null)
const pollRetryCount = ref(0)
const maintenanceActive = ref(false)
const transientPollFailure = ref(false)
const active = computed(() => operation.value?.state === 'queued' || operation.value?.state === 'running')
const confirmed = computed(() => operation.value?.state === 'succeeded' && operation.value.stage === 'verified' && Boolean(operation.value.report))
const hasPreview = computed(() => preview.value?.verified && Boolean(preview.value.expires_at))
const canRetryStatus = computed(() => Boolean(operationId.value.trim()) && (state.value === 'unknown' || maintenanceActive.value || transientPollFailure.value))
const canAbandonTracking = computed(() => state.value === 'unknown' && !busy.value && Boolean(operationId.value.trim() || pending.value))
const path = '/admin/restores'
const MAX_TRANSIENT_POLL_RETRIES = 6
const MAX_MAINTENANCE_POLL_RETRIES = 30
let previewKey = ''
let executeKey = ''
const storageKey = computed(() => auth.user ? `omnihive:restore:${auth.user.id}` : '')
let pollTimer: ReturnType<typeof setTimeout> | undefined
let revision = 0

function reset() {
  revision++
  clearTimeout(pollTimer)
  stepUp.onCancel()
  target.value = 'isolated'
  digest.value = ''
  rehearsalId.value = ''
  rehearsal.value = ''
  operationId.value = ''
  preview.value = null
  operation.value = null
  state.value = 'ready'
  message.value = ''
  busy.value = false
  confirming.value = false
  unresolved.value = false
  pending.value = null
  pollRetryCount.value = 0
  maintenanceActive.value = false
  transientPollFailure.value = false
  previewKey = ''
  executeKey = ''
}

watch(() => props.backupId, reset, { flush: 'sync' })
watch(() => auth.sessionRevision, () => {
  reset()
  recover()
}, { flush: 'post' })
watch(() => busy.value || active.value || unresolved.value, locked => emit('lock', locked), { flush: 'sync' })
onMounted(recover)
onScopeDispose(reset)

function checkpoint(value: RestoreCheckpoint | null) {
  if (!storageKey.value) return
  try {
    if (value) sessionStorage.setItem(storageKey.value, JSON.stringify(value))
    else sessionStorage.removeItem(storageKey.value)
  } catch {
    // Tracking continues in memory when storage is unavailable.
  }
}

function recover() {
  if (!storageKey.value) return
  try {
    const saved: RestoreCheckpoint | null = JSON.parse(sessionStorage.getItem(storageKey.value) ?? 'null')
    if (saved && 'operationId' in saved && typeof saved.operationId === 'string') {
      operationId.value = saved.operationId
      void readOperation(saved.operationId)
    } else if (saved && 'body' in saved && saved.body?.preview_id && saved.key) {
      pending.value = saved
      unresolved.value = true
      state.value = 'unknown'
    }
  } catch {
    // Unreadable storage does not prevent manual lookup.
  }
}

function resetPreview() {
  preview.value = null
  confirming.value = false
  previewKey = ''
  executeKey = ''
}

function isRestoreMaintenanceError(issue: ReturnType<typeof classifyAsyncError>): boolean {
  const reason = String(issue.reason ?? '').toUpperCase()
  return reason === 'RESTORE_MAINTENANCE' || reason === 'RESTORE_STATUS_UNAVAILABLE'
}

function formatMaintenanceMessage(reason?: string): string {
  if (String(reason ?? '').toUpperCase() === 'RESTORE_STATUS_UNAVAILABLE') {
    return t('adminPlatform.restore.temporary')
  }
  return t('adminPlatform.restore.maintenance')
}

function isTransientPollError(issue: ReturnType<typeof classifyAsyncError>): boolean {
  if (issue.status === 401 || issue.status === 403 || issue.status === 404 || issue.status === 400 || issue.status === 422) {
    return false
  }
  return issue.state === 'unknown' || issue.status === 0 || issue.status >= 500
}

function fail(cause: unknown, fallback: string) {
  const issue = classifyAsyncError(cause)
  if (isRestoreMaintenanceError(issue)) {
    maintenanceActive.value = true
    state.value = 'unknown'
    message.value = formatMaintenanceMessage(issue.reason)
    return
  }
  state.value = issue.state === 'error' ? 'partial-error' : issue.state
  message.value = issue.state === 'error' ? fallback : ''
}

async function createPreview() {
  if (!props.backupId || busy.value || active.value || unresolved.value) return
  if (target.value === 'production' && !rehearsalId.value.trim()) return
  const expectedDigest = digest.value.trim()
  if (expectedDigest && !/^sha256:[a-f0-9]{64}$/.test(expectedDigest)) {
    state.value = 'partial-error'
    message.value = t('adminPlatform.restore.digestError')
    return
  }
  operation.value = null
  operationId.value = ''
  preview.value = null
  state.value = 'refreshing'
  message.value = ''
  busy.value = true
  previewKey ||= createIdempotencyKey()
  const current = revision
  try {
    const result = await controlPost<Preview>(`${path}/previews`, {
      backup_id: props.backupId,
      target: target.value,
      ...(expectedDigest ? { expected_digest: expectedDigest } : {}),
    }, { 'Idempotency-Key': previewKey })
    if (current !== revision) return
    preview.value = result
    previewKey = ''
    executeKey = ''
    state.value = result.verified ? 'ready' : 'partial-error'
    if (!result.verified) message.value = t('adminPlatform.restore.integrity')
  } catch (cause) {
    if (current === revision) fail(cause, t('adminPlatform.restore.previewError'))
  } finally {
    if (current === revision) busy.value = false
  }
}

function showOperation(result: Operation) {
  operation.value = result
  operationId.value = result.id
  pending.value = null
  pollRetryCount.value = 0
  maintenanceActive.value = false
  transientPollFailure.value = false
  checkpoint({ operationId: result.id })
  message.value = ''
  unresolved.value = false
  if (active.value) {
    state.value = 'refreshing'
    message.value = t('adminPlatform.restore.accepted')
    pollTimer = setTimeout(() => void readOperation(result.id, true), 2500)
  } else if (confirmed.value) {
    state.value = 'confirmed'
    message.value = t('adminPlatform.restore.complete')
    if (result.target === 'isolated') rehearsal.value = result.id
  } else if (result.state === 'failed') {
    state.value = 'partial-error'
    message.value = t('adminPlatform.restore.failed')
  } else {
    state.value = 'unknown'
  }
}

async function readOperation(id: string, isAutoPoll = false) {
  const trimmedId = id.trim()
  if (!trimmedId || busy.value) return
  clearTimeout(pollTimer)
  if (!isAutoPoll) {
    pollRetryCount.value = 0
  }
  const current = revision
  busy.value = true
  state.value = 'refreshing'
  if (!maintenanceActive.value) {
    message.value = ''
  }
  try {
    const result = await controlGet<Operation>(`${path}/${encodeURIComponent(trimmedId)}`)
    if (current === revision) showOperation(result)
  } catch (cause) {
    if (current === revision) {
      const issue = classifyAsyncError(cause)
      const inTrackingFlow = active.value || unresolved.value || isAutoPoll
      if (isRestoreMaintenanceError(issue)) {
        maintenanceActive.value = true
        transientPollFailure.value = false
        if (pollRetryCount.value < MAX_MAINTENANCE_POLL_RETRIES) {
          pollRetryCount.value += 1
          const delay = Math.min(2500 * Math.min(pollRetryCount.value, 3), 8000)
          state.value = 'refreshing'
          message.value = t('adminPlatform.restore.maintenanceRetry', { message: formatMaintenanceMessage(issue.reason), count: pollRetryCount.value, max: MAX_MAINTENANCE_POLL_RETRIES })
          pollTimer = setTimeout(() => void readOperation(trimmedId, true), delay)
        } else {
          state.value = 'unknown'
          message.value = t('adminPlatform.restore.maintenanceLimit', { message: formatMaintenanceMessage(issue.reason) })
        }
      } else if (inTrackingFlow && isTransientPollError(issue)) {
        transientPollFailure.value = true
        maintenanceActive.value = false
        if (pollRetryCount.value < MAX_TRANSIENT_POLL_RETRIES) {
          pollRetryCount.value += 1
          const delay = Math.min(2500 * Math.pow(2, pollRetryCount.value - 1), 15000)
          state.value = 'refreshing'
          message.value = t('adminPlatform.restore.networkRetry', { count: pollRetryCount.value, max: MAX_TRANSIENT_POLL_RETRIES })
          pollTimer = setTimeout(() => void readOperation(trimmedId, true), delay)
        } else {
          state.value = 'unknown'
          message.value = t('adminPlatform.restore.retryLimit')
        }
      } else {
        maintenanceActive.value = false
        transientPollFailure.value = false
        fail(cause, t('adminPlatform.restore.statusError'))
        if (inTrackingFlow) {
          state.value = issue.state === 'forbidden' ? 'forbidden' : 'unknown'
          message.value =
            issue.state === 'forbidden'
              ? t('adminPlatform.restore.forbidden')
              : t('adminPlatform.restore.unknown')
        }
      }
    }
  } finally {
    if (current === revision) busy.value = false
  }
}

function retryStatusCheck() {
  const targetId = operationId.value.trim()
  if (!targetId) return
  clearTimeout(pollTimer)
  pollRetryCount.value = 0
  void readOperation(targetId, false)
}

function abandonTracking() {
  checkpoint(null)
  reset()
}

async function execute() {
  const approved = preview.value
  if (busy.value || active.value || (!pending.value && !approved?.verified)) return
  if (!pending.value && approved && !(Date.parse(approved.expires_at) > Date.now())) {
    state.value = 'stale'
    message.value = t('adminPlatform.restore.expired')
    resetPreview()
    return
  }
  const current = revision
  confirming.value = false
  state.value = 'refreshing'
  message.value = ''
  busy.value = true
  executeKey ||= createIdempotencyKey()
  const attempt = pending.value ?? {
    key: executeKey,
    body: {
      preview_id: approved!.id, target: target.value,
      rehearsal_id: target.value === 'production' ? rehearsalId.value.trim() : null,
    },
  }
  checkpoint(attempt)
  try {
    const result = await stepUp.run(() => {
      if (current !== revision) throw { reason: 'AUTH_SESSION_CHANGED' }
      return controlPost<Operation>(path, attempt.body, { 'Idempotency-Key': attempt.key })
    })
    if (current !== revision) return
    if (!result.id) throw { reason: 'UNKNOWN_OUTCOME' }
    preview.value = null
    showOperation(result)
  } catch (cause) {
    if (current !== revision) return
    if (isStepUpCancelled(cause)) {
      if (!unresolved.value) checkpoint(null)
      state.value = unresolved.value ? 'unknown' : 'ready'
      return
    }
    fail(cause, t('adminPlatform.restore.executeError'))
    const issue = classifyAsyncError(cause)
    unresolved.value ||= issue.state === 'unknown' || issue.status >= 500
    if (unresolved.value) {
      pending.value = attempt
      state.value = 'unknown'
    } else {
      checkpoint(null)
    }
  } finally {
    if (current === revision) busy.value = false
  }
}

function changeTarget() {
  if (target.value === 'production' && rehearsal.value) rehearsalId.value = rehearsal.value
  resetPreview()
}

const counts = computed(() => [
  ['users', t('adminPlatform.restore.users')], ['api_keys', t('adminPlatform.restore.keys')],
  ['billing_usage_entries', t('adminPlatform.restore.entries')], ['upstream_jobs', t('adminPlatform.restore.jobs')],
] as const)

const reasons = computed<Record<string, string>>(() => ({
  STEP_UP_REQUIRED: t('adminPlatform.restore.verifyTotp'),
  ARCHIVE_DIGEST_MISMATCH: t('adminPlatform.restore.mismatch'),
}))

function formatRestoreTime(value?: string | null): string {
  if (!value) return '—'
  const parsed = Date.parse(value)
  return Number.isFinite(parsed) ? new Date(parsed).toLocaleString() : '—'
}
</script>

<template>
  <Card class="space-y-4" aria-labelledby="restore-workflow-title">
    <h3 id="restore-workflow-title" tabindex="-1" class="font-semibold">{{ t('adminPlatform.restore.title') }}</h3>
    <div v-if="backupId" class="space-y-4">
      <p class="break-all text-sm text-oh-secondary">{{ t('adminPlatform.restore.selected') }} {{ backupId }}</p>
      <form class="grid gap-3 sm:grid-cols-2" @submit.prevent="createPreview">
        <label class="grid gap-1 text-sm">{{ t('adminPlatform.restore.target') }}
          <select v-model="target" class="input oh-focus-ring" :disabled="busy || active || unresolved" @change="changeTarget">
            <option value="isolated">{{ t('adminPlatform.restore.isolated') }}</option><option value="production">{{ t('adminPlatform.restore.production') }}</option>
          </select>
        </label>
        <Input v-model="digest" :label="t('adminPlatform.restore.digest')" mono
          :disabled="busy || active || unresolved" @update:model-value="resetPreview" />
        <Input v-if="target === 'production'" v-model="rehearsalId" :label="t('adminPlatform.restore.rehearsal')" required mono
          :disabled="busy || active || unresolved" @update:model-value="resetPreview" />
        <div class="flex items-end"><Button type="submit" :disabled="busy || active || unresolved">{{ t('adminPlatform.restore.preview') }}</Button></div>
      </form>
    </div>
    <p v-else class="text-sm text-oh-secondary">{{ t('adminPlatform.restore.select') }}</p>

    <div v-if="preview" class="space-y-2 border-t border-oh-subtle pt-3 text-sm">
      <p>{{ preview.verified ? t('adminPlatform.restore.verified') : t('adminPlatform.restore.invalid') }} · {{ t('adminPlatform.restore.expires') }} {{ formatRestoreTime(preview.expires_at) }}</p>
      <div class="flex flex-wrap items-center gap-1.5">
        <p class="break-all font-mono text-xs">{{ preview.impact.source_digest || '—' }}</p>
        <CopyButton v-if="preview.impact.source_digest" :value="preview.impact.source_digest" size="xs" />
      </div>
      <p>{{ t('adminPlatform.restore.databases') }} {{ preview.impact.databases ?? '—' }}</p>
      <p v-for="[key, label] in counts" :key="key">{{ label }} {{ preview.impact[key] ?? '—' }}</p>
      <div v-if="confirming" class="flex flex-wrap items-center gap-2">
        <span>{{ target === 'production' ? t('adminPlatform.restore.replace') : t('adminPlatform.restore.confirmIsolated') }}</span>
        <Button :variant="target === 'production' ? 'danger' : 'primary'" @click="execute">{{ t('adminPlatform.restore.confirm') }}</Button>
        <Button variant="secondary" @click="confirming = false">{{ t('adminPlatform.restore.cancel') }}</Button>
      </div>
      <Button v-else-if="!unresolved" :disabled="!hasPreview || busy || active" @click="confirming = true">{{ t('adminPlatform.restore.execute') }}</Button>
    </div>

    <Button v-if="unresolved" :loading="busy" @click="execute">{{ t('adminPlatform.restore.original') }}</Button>
    <div v-if="canRetryStatus || canAbandonTracking" class="flex flex-wrap items-center gap-2">
      <Button
        v-if="canRetryStatus"
        data-testid="restore-retry-status-btn"
        variant="secondary"
        :disabled="busy"
        @click="retryStatusCheck"
      >
        {{ t('adminPlatform.restore.retry') }}
      </Button>
      <Button
        v-if="canAbandonTracking"
        data-testid="restore-abandon-tracking-btn"
        variant="secondary"
        :disabled="busy"
        @click="abandonTracking"
      >
        {{ t('adminPlatform.restore.clear') }}
      </Button>
    </div>
    <OperationState :state="state" :message="message" />
    <div v-if="operation" class="space-y-2 border-t border-oh-subtle pt-3 text-sm">
      <div class="flex flex-wrap items-center gap-1.5">
        <p class="break-all">{{ t('adminPlatform.restore.operation') }} {{ operation.id }} · {{ operation.target }} · {{ operation.state || '—' }} / {{ operation.stage || '—' }}</p>
        <CopyButton v-if="operation.id" :value="operation.id" size="xs" />
      </div>
      <div v-if="operation.report">
        <h4 class="font-semibold">{{ t('adminPlatform.restore.report') }}</h4>
        <p v-if="operation.report.source_digest" class="break-all font-mono">{{ operation.report.source_digest }}</p>
        <p v-for="[key, label] in counts" :key="key">{{ label }} {{ operation.report.counts?.[key] ?? '—' }}</p>
        <p v-if="operation.report.reason">{{ reasons[operation.report.reason] ?? t('adminPlatform.restore.reportError') }}</p>
      </div>
    </div>
    <form class="flex flex-wrap items-end gap-2" @submit.prevent="readOperation(operationId)">
      <Input v-model="operationId" :label="t('adminPlatform.restore.lookup')" mono :disabled="busy || active || unresolved" />
      <Button type="submit" variant="secondary" :disabled="busy || !operationId.trim() || unresolved">{{ t('adminPlatform.restore.query') }}</Button>
    </form>
    <TotpStepUpDialog :controller="stepUp" />
  </Card>
</template>
