<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { Badge, Button, Card, CopyButton, Drawer, Input, PageHeader, Select, pushUiToast } from '@/components/ui'
import { useProjectScope } from '@/features/requests/useProjectScope'
import { useScopedRead } from '@/features/requests/useScopedRead'
import { classifyAsyncError, redactSensitiveText, type AsyncViewState } from '@/features/shared/asyncState'
import OperationState from './OperationState.vue'
import AuditScopeSelector from './components/AuditScopeSelector.vue'
import {
  generateCommandKey,
  requestGet,
  requestPost,
  requestPut,
  type ScopedAuditAction,
  type ScopedAuditEvent,
  type ScopedAuditPage,
  type ScopedAuditResource,
  type ScopedAuditRetentionPolicy
} from './components/operationsApi'
import { formatDateTimeDisplay, isValidNonNegativeIntString, isValidPositiveIntString } from './components/operationsHelpers'

let i18nContext: { locale?: { value?: string } } | null = null
try {
  i18nContext = useI18n()
} catch {
  i18nContext = { locale: { value: 'zh-CN' } }
}
const isZh = computed(() => String(i18nContext?.locale?.value || '').toLowerCase().startsWith('zh'))

const { scope, scopeKey, choose } = useProjectScope()
const eventsPath = computed(() =>
  scope.value
    ? `/workspaces/${encodeURIComponent(scope.value.workspaceId)}/projects/${encodeURIComponent(scope.value.projectId)}/audit/events`
    : ''
)
const retentionPath = computed(() =>
  scope.value
    ? `/workspaces/${encodeURIComponent(scope.value.workspaceId)}/projects/${encodeURIComponent(scope.value.projectId)}/audit/retention`
    : ''
)

const ACTION_VALUES: ScopedAuditAction[] = [
  'requests.export',
  'tasks.artifacts.revoke',
  'billing.adjust',
  'billing.reconcile',
  'audit.retention.set',
  'audit.retention.purge',
  'audit.export',
  'scope.read',
  'scope.write'
]

const RESOURCE_VALUES: ScopedAuditResource[] = [
  'request_export',
  'artifact',
  'billing_command',
  'retention',
  'audit_event',
  'project'
]

const ACTION_LABELS: Record<ScopedAuditAction, { zh: string; en: string }> = {
  'requests.export': { zh: '请求记录导出', en: 'Export Requests' },
  'tasks.artifacts.revoke': { zh: '撤销任务产物', en: 'Revoke Task Artifact' },
  'billing.adjust': { zh: '账务额度调整', en: 'Adjust Billing' },
  'billing.reconcile': { zh: '账务对账处理', en: 'Reconcile Billing' },
  'audit.retention.set': { zh: '设置保留策略', en: 'Set Retention Policy' },
  'audit.retention.purge': { zh: '清理超期审计', en: 'Purge Expired Audit' },
  'audit.export': { zh: '导出审计事件', en: 'Export Audit Event' },
  'scope.read': { zh: '作用域读取', en: 'Scope Read' },
  'scope.write': { zh: '作用域写入', en: 'Scope Write' }
}

const RESOURCE_LABELS: Record<ScopedAuditResource, { zh: string; en: string }> = {
  request_export: { zh: '请求导出任务', en: 'Request Export' },
  artifact: { zh: '任务产物', en: 'Task Artifact' },
  billing_command: { zh: '账务指令', en: 'Billing Command' },
  retention: { zh: '保留策略', en: 'Retention Policy' },
  audit_event: { zh: '审计事件', en: 'Audit Event' },
  project: { zh: '项目空间', en: 'Project' }
}

function formatActionLabel(value?: string | null, withCode = false): string {
  if (!value) return '—'
  const entry = ACTION_LABELS[value as ScopedAuditAction]
  if (!entry) return value
  const base = isZh.value ? entry.zh : entry.en
  return withCode ? `${base} (${value})` : base
}

function formatResourceLabel(value?: string | null, withCode = false): string {
  if (!value) return '—'
  const entry = RESOURCE_LABELS[value as ScopedAuditResource]
  if (!entry) return value
  const base = isZh.value ? entry.zh : entry.en
  return withCode ? `${base} (${value})` : base
}

function formatEffectLabel(effect?: string | null): string {
  if (!effect) return '—'
  if (effect === 'committed') return isZh.value ? '已提交' : 'Committed'
  if (effect === 'observed') return isZh.value ? '已记录' : 'Observed'
  return effect
}

const actionSelectOptions = computed(() => [
  { value: '', label: isZh.value ? '全部动作' : 'All Actions' },
  ...ACTION_VALUES.map((item) => ({ value: item, label: formatActionLabel(item, true) }))
])

const resourceSelectOptions = computed(() => [
  { value: '', label: isZh.value ? '全部资源' : 'All Resources' },
  ...RESOURCE_VALUES.map((item) => ({ value: item, label: formatResourceLabel(item, true) }))
])

function localizeStateMessage(viewState: AsyncViewState): string | undefined {
  if (viewState === 'ready') return undefined
  const map: Record<Exclude<AsyncViewState, 'ready'>, { zh: string; en: string }> = {
    skeleton: { zh: '正在加载项目审计记录…', en: 'Loading project audit events…' },
    refreshing: { zh: '正在同步最新审计记录…', en: 'Refreshing audit events…' },
    empty: { zh: '当前项目暂无审计事件记录。', en: 'No audit events recorded for this project yet.' },
    'zero-filter': { zh: '没有符合当前筛选条件的审计事件。', en: 'No audit events match the active filters.' },
    'partial-error': { zh: '审计记录暂时无法读取，请稍后重试。', en: 'Unable to load audit events right now. Please retry.' },
    forbidden: { zh: '当前账号缺少该项目审计日志读取权限。', en: 'Your account does not have permission to read audit logs for this project.' },
    stale: { zh: '网络波动，当前展示上次成功读取的审计快照。', en: 'Showing the last successfully loaded audit snapshot.' },
    conflict: { zh: '状态发生变更，请刷新后重试。', en: 'Resource state changed. Refresh and try again.' },
    confirmed: { zh: '操作已完成。', en: 'Operation completed.' },
    unknown: { zh: '执行结果暂未确认，请刷新核对状态。', en: 'Outcome could not be verified. Refresh to confirm current state.' }
  }
  return isZh.value ? map[viewState].zh : map[viewState].en
}

const action = ref('')
const resource = ref('')
const resourceId = ref('')
const from = ref('')
const to = ref('')
const dateError = ref('')
const query = ref<Record<string, string>>({})
const cursors = ref<(string | null)[]>([null])
const selected = ref('')
const readKey = computed(() => scopeKey.value && JSON.stringify([scopeKey.value, query.value, cursors.value.at(-1)]))
const detailPath = computed(() => selected.value && eventsPath.value && `${eventsPath.value}/${encodeURIComponent(selected.value)}`)
const detailKey = computed(() => detailPath.value && JSON.stringify([scopeKey.value, detailPath.value]))

watch(
  scopeKey,
  () => {
    cursors.value = [null]
    selected.value = ''
  },
  { flush: 'sync' }
)

const page = useScopedRead(
  () => readKey.value,
  (signal) =>
    requestGet<ScopedAuditPage>(
      eventsPath.value,
      { ...query.value, cursor: cursors.value.at(-1) || undefined },
      undefined,
      signal
    )
)
const detail = useScopedRead(
  () => detailKey.value,
  (signal) => requestGet<ScopedAuditEvent>(detailPath.value, undefined, undefined, signal)
)

function applyFilters() {
  const start = from.value ? new Date(from.value) : null
  const end = to.value ? new Date(to.value) : null
  if ((start && !Number.isFinite(+start)) || (end && !Number.isFinite(+end)) || (start && end && start >= end)) {
    dateError.value = isZh.value
      ? '结束时间必须晚于开始时间。'
      : 'End time must be later than start time.'
    return
  }
  dateError.value = ''
  query.value = Object.fromEntries(
    Object.entries({
      action: action.value,
      resource: resource.value,
      resource_id: resourceId.value.trim(),
      from: start?.toISOString(),
      to: end?.toISOString()
    }).filter((entry): entry is [string, string] => Boolean(entry[1]))
  )
  cursors.value = [null]
  if (page.error.value) void page.refresh()
}

function resetFilters() {
  action.value = ''
  resource.value = ''
  resourceId.value = ''
  from.value = ''
  to.value = ''
  dateError.value = ''
  query.value = {}
  cursors.value = [null]
}

function nextPage() {
  const cursor = page.data.value?.next_cursor
  if (cursor) cursors.value = [...cursors.value, cursor]
}

function previousPage() {
  if (cursors.value.length > 1) {
    cursors.value = cursors.value.slice(0, -1)
  }
}

const state = computed<AsyncViewState>(() => {
  if (page.loading.value) return page.data.value ? 'refreshing' : 'skeleton'
  if (page.error.value?.status === 403) return 'forbidden'
  if (page.stale.value) return 'stale'
  if (page.error.value) return 'partial-error'
  if (page.data.value?.items.length) return 'ready'
  return Object.keys(query.value).length ? 'zero-filter' : 'empty'
})

// ==================== Project Audit Retention Policy ====================
const retentionLoading = ref(false)
const retentionSaving = ref(false)
const retentionReadOnly = ref(false)
const retentionError = ref('')
const retentionConfirmed = ref(false)
const currentRetention = ref<ScopedAuditRetentionPolicy | null>(null)

const eventDaysInput = ref('90')
const traceDaysInput = ref('7')
const contentHoursInput = ref('24')
const retentionValidationError = ref('')

let retentionRequestCounter = 0

async function loadRetentionPolicy() {
  const targetPath = retentionPath.value
  if (!targetPath) {
    currentRetention.value = null
    retentionReadOnly.value = false
    retentionError.value = ''
    retentionConfirmed.value = false
    return
  }
  const token = ++retentionRequestCounter
  retentionLoading.value = true
  retentionError.value = ''
  retentionReadOnly.value = false
  try {
    const policy = await requestGet<ScopedAuditRetentionPolicy>(targetPath)
    if (token !== retentionRequestCounter) return
    currentRetention.value = policy
    eventDaysInput.value = String(policy.event_days)
    traceDaysInput.value = String(policy.trace_days)
    contentHoursInput.value = String(policy.content_hours)
  } catch (cause) {
    if (token !== retentionRequestCounter) return
    const issue = classifyAsyncError(
      cause,
      isZh.value ? '无法读取项目审计保留策略。' : 'Failed to load project audit retention policy.'
    )
    if (issue.status === 403 || issue.state === 'forbidden') {
      retentionReadOnly.value = true
      retentionError.value = ''
    } else {
      retentionError.value = issue.message
    }
  } finally {
    if (token === retentionRequestCounter) {
      retentionLoading.value = false
    }
  }
}

watch(
  retentionPath,
  () => {
    retentionConfirmed.value = false
    retentionValidationError.value = ''
    void loadRetentionPolicy()
  },
  { immediate: true }
)

function validateRetentionInputs(): ScopedAuditRetentionPolicy | null {
  const rawEventDays = String(eventDaysInput.value ?? '').trim()
  const rawTraceDays = String(traceDaysInput.value ?? '').trim()
  const rawContentHours = String(contentHoursInput.value ?? '').trim()

  if (!isValidPositiveIntString(rawEventDays)) {
    retentionValidationError.value = isZh.value
      ? '审计事件保留天数必须为 1 至 3650 之间的整数。'
      : 'Audit event retention must be an integer between 1 and 3650 days.'
    return null
  }
  const eventDays = Number(rawEventDays)
  if (eventDays < 1 || eventDays > 3650) {
    retentionValidationError.value = isZh.value
      ? '审计事件保留天数需介于 1 至 3650 天之间。'
      : 'Audit event retention must be between 1 and 3650 days.'
    return null
  }

  if (!isValidPositiveIntString(rawTraceDays)) {
    retentionValidationError.value = isZh.value
      ? '追踪元数据保留天数必须为 1 至 3650 之间的整数。'
      : 'Trace metadata retention must be an integer between 1 and 3650 days.'
    return null
  }
  const traceDays = Number(rawTraceDays)
  if (traceDays < 1 || traceDays > 3650) {
    retentionValidationError.value = isZh.value
      ? '追踪元数据保留天数需介于 1 至 3650 天之间。'
      : 'Trace metadata retention must be between 1 and 3650 days.'
    return null
  }

  if (!isValidNonNegativeIntString(rawContentHours)) {
    retentionValidationError.value = isZh.value
      ? '负载内容保留时长必须为 0 至 24 之间的整数小时。'
      : 'Payload content retention must be an integer between 0 and 24 hours.'
    return null
  }
  const contentHours = Number(rawContentHours)
  if (contentHours < 0 || contentHours > 24) {
    retentionValidationError.value = isZh.value
      ? '负载内容保留时长需介于 0 至 24 小时之间。'
      : 'Payload content retention must be between 0 and 24 hours.'
    return null
  }

  retentionValidationError.value = ''
  return {
    event_days: eventDays,
    trace_days: traceDays,
    content_hours: contentHours
  }
}

async function saveRetentionPolicy() {
  const targetPath = retentionPath.value
  if (!targetPath || retentionSaving.value || retentionReadOnly.value) return
  const payload = validateRetentionInputs()
  if (!payload) return

  retentionSaving.value = true
  retentionError.value = ''
  retentionConfirmed.value = false
  try {
    const updated = await requestPut<ScopedAuditRetentionPolicy>(targetPath, payload)
    currentRetention.value = updated
    eventDaysInput.value = String(updated.event_days)
    traceDaysInput.value = String(updated.trace_days)
    contentHoursInput.value = String(updated.content_hours)
    retentionConfirmed.value = true
    pushUiToast({
      tone: 'success',
      title: isZh.value ? '保留策略已保存' : 'Retention policy saved',
      description: isZh.value
        ? `事件 ${updated.event_days} 天 · 追踪 ${updated.trace_days} 天 · 内容 ${updated.content_hours} 小时`
        : `Events ${updated.event_days}d · Traces ${updated.trace_days}d · Content ${updated.content_hours}h`
    })
  } catch (cause) {
    const issue = classifyAsyncError(
      cause,
      isZh.value ? '保存审计保留策略失败。' : 'Failed to save audit retention policy.'
    )
    if (issue.status === 403 || issue.state === 'forbidden') {
      retentionReadOnly.value = true
      retentionError.value = ''
    } else {
      retentionError.value = issue.message
    }
  } finally {
    retentionSaving.value = false
  }
}

// ==================== Single Event Export ====================
const exportState = ref<AsyncViewState>('ready')
const exportMessage = ref('')
const exportBusy = ref(false)
const exportAttempts = new Map<string, { key: string; id?: string }>()
let exportRevision = 0
watch(
  detailKey,
  () => {
    exportRevision++
    exportBusy.value = false
    exportState.value = 'ready'
    exportMessage.value = ''
  },
  { flush: 'sync' }
)
watch(scopeKey, () => exportAttempts.clear(), { flush: 'sync' })

async function exportEvent() {
  if (!detail.data.value || exportBusy.value) return
  const target = detailPath.value
  const key = detailKey.value
  const revision = exportRevision
  const attempt = exportAttempts.get(key) ?? { key: generateCommandKey() }
  exportAttempts.set(key, attempt)
  exportBusy.value = true
  exportState.value = 'refreshing'
  exportMessage.value = isZh.value ? '正在生成事件导出文件…' : 'Preparing audit event export…'
  try {
    if (!attempt.id) {
      const operation = await requestPost<{ operation_id: string; state: string }>(`${target}/exports`, undefined, {
        'Idempotency-Key': attempt.key
      })
      if (operation.state !== 'succeeded' || !operation.operation_id) throw { reason: 'UNKNOWN_OUTCOME' }
      attempt.id = operation.operation_id
    }
    if (revision !== exportRevision) return
    const result = await requestGet<ScopedAuditEvent>(`${target}/exports/${encodeURIComponent(attempt.id)}/download`)
    if (revision !== exportRevision) return
    const url = URL.createObjectURL(new Blob([JSON.stringify(result, null, 2)], { type: 'application/json' }))
    const link = document.createElement('a')
    link.href = url
    link.download = 'audit-event.json'
    link.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
    exportState.value = 'confirmed'
    exportMessage.value = isZh.value ? '已交给浏览器下载。' : 'Handed off to browser download.'
  } catch (cause) {
    if (revision !== exportRevision) return
    const issue = classifyAsyncError(
      cause,
      isZh.value ? '导出审计事件失败。' : 'Failed to export audit event.'
    )
    exportState.value = issue.status === 410 ? 'stale' : issue.state === 'error' ? 'partial-error' : issue.state
    if (issue.status === 410) {
      exportAttempts.delete(key)
      exportMessage.value = isZh.value
        ? '导出链接已过期或该事件超出保留期限，请重新申请导出或刷新事件列表。'
        : 'Export link expired or event exceeded retention window. Request a new export or refresh the list.'
    } else {
      exportMessage.value = issue.message
    }
  } finally {
    if (revision === exportRevision) exportBusy.value = false
  }
}
</script>

<template>
  <AppLayout contained>
    <div class="min-w-0 space-y-4">
      <PageHeader
        :title="isZh ? '项目审计与数据保留' : 'Project Audit & Retention'"
        :description="isZh ? '按项目检索脱敏审计事件、导出审计凭证并配置分级保留周期。' : 'Inspect redacted project audit events, export evidence projections, and configure tiered retention windows.'"
      >
        <template #actions>
          <AuditScopeSelector :scope="scope" :is-zh="isZh" @change="choose" />
        </template>
      </PageHeader>

      <OperationState
        v-if="!scope"
        state="empty"
        :message="isZh ? '请先在右上角选择一个项目以查看审计记录与保留策略。' : 'Select a workspace project first to inspect audit logs and retention settings.'"
      />

      <template v-else>
        <Card
          data-testid="audit-retention-panel"
          :title="isZh ? '项目审计保留策略' : 'Project Audit Retention Policy'"
          :subtitle="isZh ? '按合规要求设置审计事件、追踪元数据与负载内容的自动过期清理周期。' : 'Configure automatic expiration windows for audit events, trace metadata, and payload content.'"
        >
          <template #actions>
            <div class="flex items-center gap-2">
              <Badge v-if="retentionReadOnly" tone="warning" dot>
                {{ isZh ? '只读权限' : 'Read-only' }}
              </Badge>
              <Badge v-else-if="retentionConfirmed" tone="success" dot>
                {{ isZh ? '已保存生效' : 'Saved & Active' }}
              </Badge>
              <Badge v-else-if="currentRetention" tone="info">
                {{
                  isZh
                    ? `当前：${currentRetention.event_days} 天 / ${currentRetention.trace_days} 天 / ${currentRetention.content_hours} 小时`
                    : `Current: ${currentRetention.event_days}d / ${currentRetention.trace_days}d / ${currentRetention.content_hours}h`
                }}
              </Badge>
              <Button
                size="xs"
                variant="ghost"
                :disabled="retentionLoading || retentionSaving"
                @click="loadRetentionPolicy"
              >
                {{ isZh ? '同步策略' : 'Reload Policy' }}
              </Button>
            </div>
          </template>

          <div class="space-y-3">
            <div
              v-if="retentionReadOnly"
              role="status"
              data-testid="audit-retention-readonly"
              class="rounded-oh-md border border-oh-status-warn-border bg-oh-status-warn-bg px-3 py-2 text-xs text-oh-status-warn-fg"
            >
              {{
                isZh
                  ? '当前账号无权读取或修改该项目的审计保留策略（需要项目审计策略管理权限），已切换为只读视图。'
                  : 'Read-only view: your account does not have permission to read or update audit retention settings for this project.'
              }}
            </div>

            <form
              class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4"
              @submit.prevent="saveRetentionPolicy"
            >
              <Input
                id="retention-event-days"
                v-model="eventDaysInput"
                type="number"
                mono
                :disabled="retentionLoading || retentionSaving || retentionReadOnly"
                :label="isZh ? '审计事件保留（天）' : 'Audit Events (days)'"
                :hint="isZh ? '范围 1 - 3650 天（默认 90）' : 'Range 1 - 3650 days (default 90)'"
              />
              <Input
                id="retention-trace-days"
                v-model="traceDaysInput"
                type="number"
                mono
                :disabled="retentionLoading || retentionSaving || retentionReadOnly"
                :label="isZh ? '追踪元数据保留（天）' : 'Trace Metadata (days)'"
                :hint="isZh ? '范围 1 - 3650 天（默认 7）' : 'Range 1 - 3650 days (default 7)'"
              />
              <Input
                id="retention-content-hours"
                v-model="contentHoursInput"
                type="number"
                mono
                :disabled="retentionLoading || retentionSaving || retentionReadOnly"
                :label="isZh ? '负载内容保留（小时）' : 'Payload Content (hours)'"
                :hint="isZh ? '范围 0 - 24 小时（0 表示不落盘）' : 'Range 0 - 24 hours (0 disables storage)'"
              />
              <div class="flex items-end gap-2 pb-5">
                <Button
                  type="submit"
                  data-testid="audit-retention-save"
                  :loading="retentionSaving"
                  :disabled="retentionLoading || retentionReadOnly"
                >
                  {{ isZh ? '保存保留策略' : 'Save Retention Policy' }}
                </Button>
              </div>
            </form>

            <p
              v-if="retentionValidationError"
              role="alert"
              data-testid="audit-retention-validation-error"
              class="text-xs text-oh-status-danger-fg"
            >
              {{ retentionValidationError }}
            </p>
            <p
              v-if="retentionError"
              role="alert"
              data-testid="audit-retention-error"
              class="text-xs text-oh-status-danger-fg"
            >
              {{ retentionError }}
            </p>
            <p
              v-if="retentionConfirmed"
              role="status"
              data-testid="audit-retention-confirmed"
              class="text-xs text-oh-status-ok-fg"
            >
              {{
                isZh
                  ? '项目审计保留策略已更新并立即生效。'
                  : 'Project audit retention policy has been updated and is now in effect.'
              }}
            </p>
          </div>
        </Card>

        <Card
          :title="isZh ? '审计事件检索' : 'Audit Event Explorer'"
          :subtitle="isZh ? '按动作类型、目标资源与时间窗口筛选项目级审计轨迹。' : 'Filter project-scoped audit trails by action, target resource, and time window.'"
        >
          <form class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3" @submit.prevent="applyFilters">
            <Select v-model="action" :label="isZh ? '审计动作' : 'Action'" :options="actionSelectOptions" />
            <Select v-model="resource" :label="isZh ? '目标资源' : 'Resource'" :options="resourceSelectOptions" />
            <Input
              v-model="resourceId"
              mono
              :label="isZh ? '资源标识' : 'Resource Identifier'"
              :placeholder="isZh ? '输入审计事件关联的资源 ID' : 'Opaque resource reference ID'"
            />
            <label class="grid min-w-0 gap-1.5 text-xs font-medium text-oh-secondary">
              <span>{{ isZh ? '开始时间（含）' : 'Start Time (Inclusive)' }}</span>
              <input v-model="from" type="datetime-local" class="input oh-focus-ring w-full min-w-0" />
            </label>
            <label class="grid min-w-0 gap-1.5 text-xs font-medium text-oh-secondary">
              <span>{{ isZh ? '结束时间（不含）' : 'End Time (Exclusive)' }}</span>
              <input v-model="to" type="datetime-local" class="input oh-focus-ring w-full min-w-0" />
            </label>
            <div class="flex flex-wrap items-end gap-2">
              <Button type="submit">{{ isZh ? '应用筛选' : 'Apply Filters' }}</Button>
              <Button variant="secondary" type="button" @click="resetFilters">
                {{ isZh ? '重置' : 'Reset' }}
              </Button>
              <Button variant="secondary" type="button" :disabled="page.loading.value" @click="page.refresh">
                {{ isZh ? '刷新列表' : 'Refresh' }}
              </Button>
            </div>
            <p v-if="dateError" role="alert" class="text-sm text-oh-status-danger-fg sm:col-span-2 xl:col-span-3">
              {{ dateError }}
            </p>
          </form>
        </Card>

        <OperationState :state="state" :message="localizeStateMessage(state)" />

        <Card v-if="page.data.value?.items.length" padding="none">
          <div class="overflow-x-auto">
            <table class="w-full text-left text-sm">
              <thead class="border-b border-oh-subtle bg-oh-subtle text-xs font-medium text-oh-secondary">
                <tr>
                  <th class="px-4 py-2.5">{{ isZh ? '发生时间' : 'Timestamp' }}</th>
                  <th class="px-4 py-2.5">{{ isZh ? '审计动作' : 'Action' }}</th>
                  <th class="px-4 py-2.5">{{ isZh ? '目标资源' : 'Resource' }}</th>
                  <th class="px-4 py-2.5">{{ isZh ? '执行结果' : 'Effect' }}</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="event in page.data.value.items"
                  :key="event.audit_event_id"
                  class="border-t border-oh-subtle transition-colors hover:bg-oh-hover/40"
                >
                  <td class="px-4 py-2.5 font-mono text-xs tabular-nums whitespace-nowrap text-oh-secondary">
                    {{ formatDateTimeDisplay(event.at) }}
                  </td>
                  <td class="px-4 py-2.5">
                    <Button size="sm" variant="ghost" @click="selected = event.audit_event_id">
                      <span>{{ formatActionLabel(event.action) }}</span>
                      <span class="ml-1.5 font-mono text-xs text-oh-muted">({{ event.action }})</span>
                    </Button>
                  </td>
                  <td class="px-4 py-2.5">
                    <div class="flex flex-wrap items-center gap-1.5">
                      <span>{{ formatResourceLabel(event.resource) }}</span>
                      <span class="font-mono text-xs text-oh-muted">({{ event.resource }})</span>
                    </div>
                  </td>
                  <td class="px-4 py-2.5">
                    <Badge :tone="event.effect === 'committed' ? 'success' : 'neutral'" size="xs">
                      {{ formatEffectLabel(event.effect) }}
                    </Badge>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </Card>

        <div
          v-if="Boolean(page.data.value?.next_cursor) || cursors.length > 1"
          class="flex flex-wrap items-center justify-end gap-2"
        >
          <Button
            size="sm"
            variant="secondary"
            :disabled="page.loading.value || cursors.length <= 1"
            @click="previousPage"
          >
            {{ isZh ? '上一页' : 'Previous' }}
          </Button>
          <Button
            size="sm"
            variant="secondary"
            :disabled="page.loading.value || !page.data.value?.next_cursor"
            @click="nextPage"
          >
            {{ isZh ? '下一页' : 'Next' }}
          </Button>
        </div>
      </template>

      <Drawer
        :open="Boolean(selected)"
        :title="isZh ? '审计事件详情' : 'Audit Event Detail'"
        @close="selected = ''"
      >
        <OperationState
          v-if="detail.loading.value"
          state="skeleton"
          :message="isZh ? '正在加载事件详情…' : 'Loading audit event details…'"
        />
        <OperationState
          v-else-if="detail.error.value"
          :state="detail.error.value.status === 403 ? 'forbidden' : 'partial-error'"
          :message="
            detail.error.value.status === 410
              ? (isZh ? '该审计事件已超出保留期限。' : 'This audit event has exceeded its retention period.')
              : detail.error.value.status === 403
                ? (isZh ? '当前账号无权查看该审计事件详情。' : 'You do not have permission to view this audit event.')
                : (isZh ? '无法读取审计事件详情，请稍后重试。' : 'Unable to load audit event details.')
          "
        />
        <div v-else-if="detail.data.value" class="space-y-3">
          <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 break-all text-sm">
            <dt class="font-medium text-oh-secondary">{{ isZh ? '事件 ID' : 'Event ID' }}</dt>
            <dd class="flex flex-wrap items-center gap-1.5 font-mono text-xs">
              <span>{{ detail.data.value.audit_event_id || '—' }}</span>
              <CopyButton v-if="detail.data.value.audit_event_id" :value="detail.data.value.audit_event_id" size="xs" />
            </dd>
            <dt class="font-medium text-oh-secondary">{{ isZh ? '操作者标识' : 'Actor ID' }}</dt>
            <dd class="font-mono text-xs">{{ detail.data.value.actor_id ?? '—' }}</dd>
            <dt class="font-medium text-oh-secondary">{{ isZh ? '发生时间' : 'Timestamp' }}</dt>
            <dd class="font-mono text-xs tabular-nums">{{ formatDateTimeDisplay(detail.data.value.at) }}</dd>
            <dt class="font-medium text-oh-secondary">{{ isZh ? '审计动作' : 'Action' }}</dt>
            <dd>{{ formatActionLabel(detail.data.value.action, true) }}</dd>
            <dt class="font-medium text-oh-secondary">{{ isZh ? '关联资源' : 'Resource' }}</dt>
            <dd>
              {{ formatResourceLabel(detail.data.value.resource, true) }}
              ·
              <span class="font-mono text-xs">{{ redactSensitiveText(detail.data.value.resource_id) || '—' }}</span>
            </dd>
            <dt class="font-medium text-oh-secondary">{{ isZh ? '执行结果' : 'Effect' }}</dt>
            <dd>{{ formatEffectLabel(detail.data.value.effect) }}</dd>
            <dt v-if="detail.data.value.affected" class="font-medium text-oh-secondary">
              {{ isZh ? '影响条目数' : 'Affected Count' }}
            </dt>
            <dd v-if="detail.data.value.affected" class="font-mono text-xs">
              {{ detail.data.value.affected }}
            </dd>
          </dl>
          <Button :loading="exportBusy" @click="exportEvent">
            {{ isZh ? '导出事件 JSON' : 'Export Event JSON' }}
          </Button>
          <OperationState :state="exportState" :message="exportMessage || localizeStateMessage(exportState)" />
        </div>
      </Drawer>
    </div>
  </AppLayout>
</template>
