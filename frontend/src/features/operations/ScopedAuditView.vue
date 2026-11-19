<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import { Button, Card, CopyButton, Drawer, Input, PageHeader, Select } from '@/components/ui'
import ScopePicker from '@/features/requests/components/ScopePicker.vue'
import CursorButtons from '@/features/requests/components/CursorButtons.vue'
import { useProjectScope } from '@/features/requests/useProjectScope'
import { useScopedRead } from '@/features/requests/useScopedRead'
import { classifyAsyncError, redactSensitiveText, type AsyncViewState } from '@/features/shared/asyncState'
import { createIdempotencyKey, v2Get, v2Post } from '@/features/shared/v2Client'
import type { components } from '@/api/generated/v2'
import OperationState from './OperationState.vue'

type AuditEvent = components['schemas']['ScopedAuditEvent']
type AuditPage = { items: AuditEvent[]; as_of: string; next_cursor: string | null }

const { scope, scopeKey, choose } = useProjectScope()
const path = computed(() => scope.value ? `/workspaces/${scope.value.workspaceId}/projects/${scope.value.projectId}/audit/events` : '')
const actionOptions = ['requests.export', 'tasks.artifacts.revoke', 'billing.adjust', 'billing.reconcile', 'audit.retention.set', 'audit.retention.purge', 'audit.export', 'scope.read', 'scope.write']
const resourceOptions = ['request_export', 'artifact', 'billing_command', 'retention', 'audit_event', 'project']
const options = (values: string[]) => [{ value: '', label: '全部' }, ...values.map(value => ({ value, label: value }))]
function formatAuditTime(value?: string | null): string {
  if (!value) return '—'
  const parsed = Date.parse(value)
  return Number.isFinite(parsed) ? new Date(parsed).toLocaleString() : '—'
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
const detailPath = computed(() => selected.value && path.value && `${path.value}/${encodeURIComponent(selected.value)}`)
const detailKey = computed(() => detailPath.value && JSON.stringify([scopeKey.value, detailPath.value]))

watch(scopeKey, () => {
  cursors.value = [null]
  selected.value = ''
}, { flush: 'sync' })

const page = useScopedRead(() => readKey.value, () => v2Get<AuditPage>(path.value, {
  ...query.value, cursor: cursors.value.at(-1) || undefined,
}))
const detail = useScopedRead(() => detailKey.value, () => v2Get<AuditEvent>(detailPath.value))

function applyFilters() {
  const start = from.value ? new Date(from.value) : null
  const end = to.value ? new Date(to.value) : null
  if ((start && !Number.isFinite(+start)) || (end && !Number.isFinite(+end)) || (start && end && start >= end)) {
    dateError.value = '结束时间必须晚于开始时间。'
    return
  }
  dateError.value = ''
  query.value = Object.fromEntries(Object.entries({
    action: action.value, resource: resource.value, resource_id: resourceId.value.trim(),
    from: start?.toISOString(), to: end?.toISOString(),
  }).filter((entry): entry is [string, string] => Boolean(entry[1])))
  cursors.value = [null]
  if (page.error.value) void page.refresh()
}

function next() {
  const cursor = page.data.value?.next_cursor
  if (cursor) cursors.value = [...cursors.value, cursor]
}

const state = computed<AsyncViewState>(() => {
  if (page.loading.value) return page.data.value ? 'refreshing' : 'skeleton'
  if (page.error.value?.status === 403) return 'forbidden'
  if (page.stale.value) return 'stale'
  if (page.error.value) return 'partial-error'
  if (page.data.value?.items.length) return 'ready'
  return Object.keys(query.value).length ? 'zero-filter' : 'empty'
})

const exportState = ref<AsyncViewState>('ready')
const exportMessage = ref('')
const exportBusy = ref(false)
const exportAttempts = new Map<string, { key: string; id?: string }>()
let exportRevision = 0
watch(detailKey, () => {
  exportRevision++
  exportBusy.value = false
  exportState.value = 'ready'
  exportMessage.value = ''
}, { flush: 'sync' })
watch(scopeKey, () => exportAttempts.clear(), { flush: 'sync' })

async function exportEvent() {
  if (!detail.data.value || exportBusy.value) return
  const target = detailPath.value
  const key = detailKey.value
  const revision = exportRevision
  const attempt = exportAttempts.get(key) ?? { key: createIdempotencyKey() }
  exportAttempts.set(key, attempt)
  exportBusy.value = true
  exportState.value = 'refreshing'
  exportMessage.value = ''
  try {
    if (!attempt.id) {
      const operation = await v2Post<{ operation_id: string; state: string }>(`${target}/exports`, undefined, {
        'Idempotency-Key': attempt.key,
      })
      if (operation.state !== 'succeeded' || !operation.operation_id) throw { reason: 'UNKNOWN_OUTCOME' }
      attempt.id = operation.operation_id
    }
    if (revision !== exportRevision) return
    const result = await v2Get<AuditEvent>(`${target}/exports/${encodeURIComponent(attempt.id)}/download`)
    if (revision !== exportRevision) return
    const url = URL.createObjectURL(new Blob([JSON.stringify(result, null, 2)], { type: 'application/json' }))
    const link = document.createElement('a')
    link.href = url
    link.download = 'audit-event.json'
    link.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
    exportState.value = 'confirmed'
    exportMessage.value = '已交给浏览器下载。'
  } catch (cause) {
    if (revision !== exportRevision) return
    const issue = classifyAsyncError(cause)
    exportState.value = issue.status === 410 ? 'stale' : issue.state === 'error' ? 'partial-error' : issue.state
    if (issue.status === 410) {
      exportAttempts.delete(key)
      exportMessage.value = '导出已过期或事件超出保留期限。请重新申请导出，或刷新事件列表。'
    }
  } finally {
    if (revision === exportRevision) exportBusy.value = false
  }
}
</script>

<template>
  <AppLayout contained>
    <div class="min-w-0 space-y-4">
      <PageHeader title="项目审计" description="查看所选项目的审计事件。">
        <template #actions><ScopePicker :scope="scope" @change="choose" /></template>
      </PageHeader>
      <OperationState v-if="!scope" state="empty" message="请选择一个项目。" />
      <template v-else>
        <form class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3" @submit.prevent="applyFilters">
          <Select v-model="action" label="动作" :options="options(actionOptions)" />
          <Select v-model="resource" label="资源" :options="options(resourceOptions)" />
          <Input v-model="resourceId" label="资源标识" />
          <label class="grid min-w-0 gap-1.5 text-xs font-medium text-oh-secondary">开始时间（含）
            <input v-model="from" type="datetime-local" class="input oh-focus-ring w-full min-w-0" />
          </label>
          <label class="grid min-w-0 gap-1.5 text-xs font-medium text-oh-secondary">结束时间（不含）
            <input v-model="to" type="datetime-local" class="input oh-focus-ring w-full min-w-0" />
          </label>
          <div class="flex flex-wrap items-end gap-2">
            <Button type="submit">筛选</Button>
            <Button variant="secondary" :disabled="page.loading.value" @click="page.refresh">刷新</Button>
          </div>
          <p v-if="dateError" role="alert" class="text-sm text-oh-status-danger-fg">{{ dateError }}</p>
        </form>
        <OperationState :state="state" />
        <Card v-if="page.data.value?.items.length">
          <div class="overflow-x-auto">
            <table class="w-full text-left text-sm">
              <thead><tr><th class="p-2">时间</th><th class="p-2">动作</th><th class="p-2">资源</th><th class="p-2">结果</th></tr></thead>
              <tbody>
                <tr v-for="event in page.data.value.items" :key="event.audit_event_id" class="border-t border-oh-subtle">
                  <td class="p-2 font-mono text-xs tabular-nums whitespace-nowrap">{{ formatAuditTime(event.at) }}</td>
                  <td class="p-2"><Button size="sm" variant="ghost" @click="selected = event.audit_event_id">{{ event.action || '—' }}</Button></td>
                  <td class="p-2">{{ event.resource || '—' }}</td><td class="p-2">{{ event.effect || '—' }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </Card>
        <CursorButtons :next="Boolean(page.data.value?.next_cursor)" :previous="cursors.length > 1"
          :loading="page.loading.value" @next="next" @previous="cursors = cursors.slice(0, -1)" />
      </template>
      <Drawer :open="Boolean(selected)" title="审计事件详情" @close="selected = ''">
        <OperationState v-if="detail.loading.value" state="skeleton" />
        <OperationState v-else-if="detail.error.value"
          :state="detail.error.value.status === 403 ? 'forbidden' : 'partial-error'"
          :message="detail.error.value.status === 410 ? '事件已超出保留期限。' : undefined" />
        <div v-else-if="detail.data.value" class="space-y-3">
          <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 break-all text-sm">
            <dt>事件</dt>
            <dd class="flex flex-wrap items-center gap-1.5 font-mono text-xs">
              <span>{{ detail.data.value.audit_event_id || '—' }}</span>
              <CopyButton v-if="detail.data.value.audit_event_id" :value="detail.data.value.audit_event_id" size="xs" />
            </dd>
            <dt>操作者</dt><dd>{{ detail.data.value.actor_id ?? '—' }}</dd>
            <dt>时间</dt><dd class="font-mono text-xs tabular-nums">{{ formatAuditTime(detail.data.value.at) }}</dd>
            <dt>动作</dt><dd>{{ detail.data.value.action || '—' }}</dd>
            <dt>资源</dt><dd>{{ detail.data.value.resource || '—' }} · {{ redactSensitiveText(detail.data.value.resource_id) || '—' }}</dd>
            <dt>结果</dt><dd>{{ detail.data.value.effect || '—' }}</dd>
            <dt v-if="detail.data.value.affected">影响数量</dt><dd v-if="detail.data.value.affected">{{ detail.data.value.affected }}</dd>
          </dl>
          <Button :loading="exportBusy" @click="exportEvent">导出事件</Button>
          <OperationState :state="exportState" :message="exportMessage" />
        </div>
      </Drawer>
    </div>
  </AppLayout>
</template>
