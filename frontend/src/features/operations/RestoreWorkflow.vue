<script setup lang="ts">
import { computed, onMounted, onScopeDispose, ref, watch } from 'vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { Button, Card, CopyButton, Input } from '@/components/ui'
import { isStepUpCancelled, useStepUp } from '@/composables/useStepUp'
import { useAuthStore } from '@/stores/auth'
import { classifyAsyncError, type AsyncViewState } from '@/features/shared/asyncState'
import { createIdempotencyKey, v2Get, v2Post } from '@/features/shared/v2Client'
import type { components } from '@/api/generated/v2'
import OperationState from './OperationState.vue'

type Preview = components['schemas']['RestorePreview']
type Operation = components['schemas']['RestoreOperation']
type Target = Operation['target']
type ExecuteBody = components['schemas']['RestoreExecuteInput']
type RestoreCheckpoint = { operationId: string } | { body: ExecuteBody; key: string }

const props = defineProps<{ backupId: string }>()
const emit = defineEmits<{ lock: [locked: boolean] }>()
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
const active = computed(() => operation.value?.state === 'queued' || operation.value?.state === 'running')
const confirmed = computed(() => operation.value?.state === 'succeeded' && operation.value.stage === 'verified' && Boolean(operation.value.report))
const hasPreview = computed(() => preview.value?.verified && Boolean(preview.value.expires_at))
const path = '/admin/restores'
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

function fail(cause: unknown, fallback: string) {
  const issue = classifyAsyncError(cause)
  state.value = issue.state === 'error' ? 'partial-error' : issue.state
  message.value = issue.state === 'error' ? fallback : ''
}

async function createPreview() {
  if (!props.backupId || busy.value || active.value || unresolved.value) return
  if (target.value === 'production' && !rehearsalId.value.trim()) return
  const expectedDigest = digest.value.trim()
  if (expectedDigest && !/^sha256:[a-f0-9]{64}$/.test(expectedDigest)) {
    state.value = 'partial-error'
    message.value = '请输入 sha256: 后接 64 位小写十六进制字符。'
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
    const result = await v2Post<Preview>(`${path}/previews`, {
      backup_id: props.backupId,
      target: target.value,
      ...(expectedDigest ? { expected_digest: expectedDigest } : {}),
    }, { 'Idempotency-Key': previewKey })
    if (current !== revision) return
    preview.value = result
    previewKey = ''
    executeKey = ''
    state.value = result.verified ? 'ready' : 'partial-error'
    if (!result.verified) message.value = '预览完整性校验未通过。'
  } catch (cause) {
    if (current === revision) fail(cause, '无法创建恢复预览。请检查备份和目标。')
  } finally {
    if (current === revision) busy.value = false
  }
}

function showOperation(result: Operation) {
  operation.value = result
  operationId.value = result.id
  pending.value = null
  checkpoint({ operationId: result.id })
  message.value = ''
  unresolved.value = false
  if (active.value) {
    state.value = 'refreshing'
    message.value = '恢复已受理，正在读取进度。'
    pollTimer = setTimeout(() => void readOperation(result.id), 2500)
  } else if (confirmed.value) {
    state.value = 'confirmed'
    message.value = '恢复已完成并通过校验。'
    if (result.target === 'isolated') rehearsal.value = result.id
  } else if (result.state === 'failed') {
    state.value = 'partial-error'
    message.value = '恢复失败，请核对报告。'
  } else {
    state.value = 'unknown'
  }
}

async function readOperation(id: string) {
  if (!id.trim() || busy.value) return
  clearTimeout(pollTimer)
  const current = revision
  busy.value = true
  state.value = 'refreshing'
  message.value = ''
  try {
    const result = await v2Get<Operation>(`${path}/${encodeURIComponent(id.trim())}`)
    if (current === revision) showOperation(result)
  } catch (cause) {
    if (current === revision) {
      fail(cause, '无法读取操作状态。请核对操作编号并重试。')
      if (active.value || unresolved.value) {
        state.value = classifyAsyncError(cause).state === 'forbidden' ? 'forbidden' : 'unknown'
        message.value = '暂时无法确认恢复结果。请重新查询该操作。'
      }
    }
  } finally {
    if (current === revision) busy.value = false
  }
}

async function execute() {
  const approved = preview.value
  if (busy.value || active.value || (!pending.value && !approved?.verified)) return
  if (!pending.value && approved && !(Date.parse(approved.expires_at) > Date.now())) {
    state.value = 'stale'
    message.value = '预览已过期，请重新生成。'
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
      return v2Post<Operation>(path, attempt.body, { 'Idempotency-Key': attempt.key })
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
    fail(cause, '恢复请求未能完成。')
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

const counts = [
  ['users', '用户'], ['api_keys', 'API 密钥数量'],
  ['billing_usage_entries', '计费用量记录'], ['upstream_jobs', '上游任务'],
] as const

const reasons: Record<string, string> = {
  STEP_UP_REQUIRED: '需要再次验证 TOTP。',
  ARCHIVE_DIGEST_MISMATCH: '归档摘要不匹配。',
}

function formatRestoreTime(value?: string | null): string {
  if (!value) return '—'
  const parsed = Date.parse(value)
  return Number.isFinite(parsed) ? new Date(parsed).toLocaleString() : '—'
}
</script>

<template>
  <Card class="space-y-4" aria-labelledby="restore-workflow-title">
    <h3 id="restore-workflow-title" tabindex="-1" class="font-semibold">恢复备份</h3>
    <div v-if="backupId" class="space-y-4">
      <p class="break-all text-sm text-oh-secondary">所选备份 {{ backupId }}</p>
      <form class="grid gap-3 sm:grid-cols-2" @submit.prevent="createPreview">
        <label class="grid gap-1 text-sm">恢复目标
          <select v-model="target" class="input oh-focus-ring" :disabled="busy || active || unresolved" @change="changeTarget">
            <option value="isolated">隔离环境</option><option value="production">生产环境</option>
          </select>
        </label>
        <Input v-model="digest" label="预期 SHA-256 摘要（选填）" mono
          :disabled="busy || active || unresolved" @update:model-value="resetPreview" />
        <Input v-if="target === 'production'" v-model="rehearsalId" label="已验证的隔离恢复操作编号" required mono
          :disabled="busy || active || unresolved" @update:model-value="resetPreview" />
        <div class="flex items-end"><Button type="submit" :disabled="busy || active || unresolved">生成预览</Button></div>
      </form>
    </div>
    <p v-else class="text-sm text-oh-secondary">从备份列表中选择已完成的备份以生成预览。</p>

    <div v-if="preview" class="space-y-2 border-t border-oh-subtle pt-3 text-sm">
      <p>{{ preview.verified ? '预览已验证' : '预览校验失败' }} · 有效至 {{ formatRestoreTime(preview.expires_at) }}</p>
      <div class="flex flex-wrap items-center gap-1.5">
        <p class="break-all font-mono text-xs">{{ preview.impact.source_digest || '—' }}</p>
        <CopyButton v-if="preview.impact.source_digest" :value="preview.impact.source_digest" size="xs" />
      </div>
      <p>数据库 {{ preview.impact.databases ?? '—' }}</p>
      <p v-for="[key, label] in counts" :key="key">{{ label }} {{ preview.impact[key] ?? '—' }}</p>
      <div v-if="confirming" class="flex flex-wrap items-center gap-2">
        <span>{{ target === 'production' ? '生产恢复会替换目标数据。' : '确认在隔离环境执行恢复。' }}</span>
        <Button :variant="target === 'production' ? 'danger' : 'primary'" @click="execute">确认执行</Button>
        <Button variant="secondary" @click="confirming = false">取消</Button>
      </div>
      <Button v-else-if="!unresolved" :disabled="!hasPreview || busy || active" @click="confirming = true">执行恢复</Button>
    </div>

    <Button v-if="unresolved" :loading="busy" @click="execute">使用原请求确认结果</Button>
    <OperationState :state="state" :message="message" />
    <div v-if="operation" class="space-y-2 border-t border-oh-subtle pt-3 text-sm">
      <div class="flex flex-wrap items-center gap-1.5">
        <p class="break-all">操作 {{ operation.id }} · {{ operation.target }} · {{ operation.state || '—' }} / {{ operation.stage || '—' }}</p>
        <CopyButton v-if="operation.id" :value="operation.id" size="xs" />
      </div>
      <div v-if="operation.report">
        <h4 class="font-semibold">恢复报告</h4>
        <p v-if="operation.report.source_digest" class="break-all font-mono">{{ operation.report.source_digest }}</p>
        <p v-for="[key, label] in counts" :key="key">{{ label }} {{ operation.report.counts?.[key] ?? '—' }}</p>
        <p v-if="operation.report.reason">{{ reasons[operation.report.reason] ?? '后端报告了恢复异常，请使用操作编号排查。' }}</p>
      </div>
    </div>
    <form class="flex flex-wrap items-end gap-2" @submit.prevent="readOperation(operationId)">
      <Input v-model="operationId" label="按操作编号查询恢复报告" mono :disabled="busy || active || unresolved" />
      <Button type="submit" variant="secondary" :disabled="busy || !operationId.trim() || unresolved">查询</Button>
    </form>
    <TotpStepUpDialog :controller="stepUp" />
  </Card>
</template>
