<script setup lang="ts">
import { computed, onMounted, onScopeDispose, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Card, Input, Modal, Select, pushUiToast } from '@/components/ui'
import { buildGatewayUrl } from '@/api/url'
import { getConcurrencyStats, getRealtimeTrafficSummary } from '@/api/admin/ops'
import { useAuthStore } from '@/stores/auth'
import { useScopedRead } from '@/features/requests/useScopedRead'
import { classifyAsyncError, type AsyncViewState } from '@/features/shared/asyncState'
import OperationState from './OperationState.vue'
import CatalogVerificationWorkbench from './components/CatalogVerificationWorkbench.vue'
import WorkspaceTelemetryPanel from './components/WorkspaceTelemetryPanel.vue'
import {
  generateCommandKey,
  requestGet,
  requestPost,
  type StatusSeverity,
  type StatusSummary
} from './components/operationsApi'
import { formatDateTimeDisplay } from './components/operationsHelpers'

let i18nContext: { locale?: { value?: string } } | null = null
try {
  i18nContext = useI18n()
} catch {
  i18nContext = { locale: { value: 'zh-CN' } }
}
const isZh = computed(() => String(i18nContext?.locale?.value || '').toLowerCase().startsWith('zh'))
const auth = useAuthStore()
const isAdmin = computed(() => auth.user?.role === 'admin')
const key = () => auth.user ? String(auth.sessionRevision) : ''

async function probe(path: string, signal: AbortSignal) {
  const response = await fetch(buildGatewayUrl(path), { cache: 'no-store', redirect: 'error', signal })
  if (response.status === 503 && path === '/ready') return 'not-ready'
  if (!response.ok) throw { status: response.status }
  if (path === '/health') {
    const data: unknown = await response.json()
    if (typeof data !== 'object' || data === null || !('status' in data) || data.status !== 'ok') throw { status: 502 }
  } else if ((response.headers.get('content-type') ?? '').includes('text/html')) {
    throw { status: 404 }
  }
  return 'ok'
}

const live = useScopedRead(key, signal => probe('/health', signal))
const readiness = useScopedRead(key, signal => probe('/ready', signal))
const queue = useScopedRead(key, () => getConcurrencyStats())
const traffic = useScopedRead(key, () => getRealtimeTrafficSummary('5min'))

function panelState(read: typeof live | typeof queue | typeof traffic, hasData = true): AsyncViewState {
  if (read.loading.value) return read.data.value ? 'refreshing' : 'skeleton'
  if (read.error.value?.reason === 'OPS_DISABLED') return 'empty'
  if (read.error.value?.status === 403) return 'forbidden'
  if (read.stale.value) return 'stale'
  if (read.error.value) return 'partial-error'
  return hasData && read.data.value ? 'ready' : 'empty'
}

function localizePanelMessage(viewState: AsyncViewState, override?: string): string | undefined {
  if (override) return override
  if (viewState === 'ready') return undefined
  const map: Record<Exclude<AsyncViewState, 'ready'>, { zh: string; en: string }> = {
    skeleton: { zh: '正在加载运行指标…', en: 'Loading operational signals…' },
    refreshing: { zh: '正在刷新…', en: 'Refreshing…' },
    empty: { zh: '暂无指标数据', en: 'No signal data available' },
    'zero-filter': { zh: '无匹配记录', en: 'No matching records' },
    'partial-error': { zh: '部分信号暂不可用', en: 'Signal temporarily unavailable' },
    forbidden: { zh: '当前账号无权读取该信号', en: 'Permission denied' },
    stale: { zh: '显示上次成功读取的指标快照', en: 'Showing last known snapshot' },
    conflict: { zh: '状态冲突，请刷新核对', en: 'State conflict, please refresh' },
    confirmed: { zh: '操作已确认', en: 'Confirmed' },
    unknown: { zh: '结果待确认', en: 'Outcome pending verification' }
  }
  return isZh.value ? map[viewState].zh : map[viewState].en
}

const platforms = computed(() => queue.data.value?.enabled ? Object.values(queue.data.value.platform ?? {}) : [])
const summary = computed(() => traffic.data.value?.enabled ? traffic.data.value.summary : null)
const reads = [live, readiness, queue, traffic]
const refreshing = computed(() => reads.some(read => read.loading.value))
function refresh() {
  for (const read of reads) {
    if (!read.loading.value) void read.refresh()
  }
}

let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  timer = setInterval(() => {
    if (!document.hidden) refresh()
  }, 15_000)
})
onScopeDispose(() => clearInterval(timer))

const incidentModalOpen = ref(false)
const incidentComponents = ref<Array<{ id: string; name: string }>>([])
const incidentTitle = ref('')
const incidentSeverity = ref<StatusSeverity>('degraded')
const incidentSelectedComponents = ref<string[]>([])
const incidentMessage = ref('')
const incidentSubmitting = ref(false)
const incidentError = ref('')

async function openIncidentModal() {
  incidentModalOpen.value = true
  incidentError.value = ''
  if (incidentComponents.value.length === 0) {
    try {
      const summary = await requestGet<StatusSummary>('/public/status/summary')
      incidentComponents.value = (summary.components ?? []).map((item) => ({
        id: item.id,
        name: item.name
      }))
      if (incidentSelectedComponents.value.length === 0 && incidentComponents.value.length > 0) {
        incidentSelectedComponents.value = [incidentComponents.value[0].id]
      }
    } catch {
      incidentComponents.value = []
    }
  }
}

function toggleIncidentComponent(id: string) {
  if (incidentSelectedComponents.value.includes(id)) {
    incidentSelectedComponents.value = incidentSelectedComponents.value.filter((item) => item !== id)
  } else {
    incidentSelectedComponents.value = [...incidentSelectedComponents.value, id]
  }
}

async function submitIncident() {
  if (incidentSubmitting.value) return
  if (!incidentTitle.value.trim() || !incidentMessage.value.trim()) {
    incidentError.value = isZh.value ? '请填写事件标题与说明内容。' : 'Title and message are required.'
    return
  }
  if (incidentSelectedComponents.value.length === 0) {
    incidentError.value = isZh.value ? '请至少选择一个受影响组件。' : 'Select at least one affected component.'
    return
  }
  incidentSubmitting.value = true
  incidentError.value = ''
  try {
    await requestPost(
      '/admin/status/incidents',
      {
        title: incidentTitle.value.trim(),
        severity: incidentSeverity.value,
        component_ids: incidentSelectedComponents.value,
        message: incidentMessage.value.trim()
      },
      { 'Idempotency-Key': generateCommandKey() }
    )
    incidentModalOpen.value = false
    incidentTitle.value = ''
    incidentMessage.value = ''
    pushUiToast({
      tone: 'success',
      title: isZh.value ? '状态事件已发布' : 'Incident published',
      description: isZh.value ? '公开服务状态页已同步更新。' : 'The public status page has been updated.'
    })
  } catch (cause) {
    const issue = classifyAsyncError(cause, isZh.value ? '发布状态事件失败' : 'Failed to publish incident')
    incidentError.value = issue.message
  } finally {
    incidentSubmitting.value = false
  }
}
</script>

<template>
  <section aria-labelledby="operational-signals-title" class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h2 id="operational-signals-title" class="font-semibold">{{ isZh ? '运行信号' : 'Operational Signals' }}</h2>
      <div class="flex flex-wrap items-center gap-2">
        <Button v-if="isAdmin" variant="secondary" size="sm" @click="openIncidentModal">
          {{ isZh ? '发布状态事件' : 'Publish Incident' }}
        </Button>
        <Button variant="secondary" size="sm" :disabled="refreshing" @click="refresh">
          {{ isZh ? '刷新运行信号' : 'Refresh Signals' }}
        </Button>
      </div>
    </div>
    <div class="grid gap-3 lg:grid-cols-3">
      <Card class="space-y-2">
        <h3 class="font-semibold">{{ isZh ? '服务健康' : 'Service Health' }}</h3>
        <div class="text-sm">{{ isZh ? '进程存活' : 'Liveness' }}
          <span v-if="live.data.value"> · {{ isZh ? '正常' : 'OK' }}</span>
          <OperationState
            :state="panelState(live)"
            :message="localizePanelMessage(panelState(live), panelState(live) === 'partial-error' ? (isZh ? '无法读取存活探针。' : 'Unable to read liveness probe.') : undefined)"
          />
        </div>
        <div class="text-sm">{{ isZh ? '依赖就绪' : 'Readiness' }}
          <span v-if="readiness.data.value"> · {{ readiness.data.value === 'ok' ? (isZh ? '就绪' : 'Ready') : (isZh ? '未就绪（503）' : 'Not Ready (503)') }}</span>
          <OperationState
            :state="panelState(readiness)"
            :message="localizePanelMessage(panelState(readiness), panelState(readiness) === 'partial-error' ? (isZh ? '无法读取就绪探针。' : 'Unable to read readiness probe.') : undefined)"
          />
        </div>
      </Card>
      <Card class="space-y-2">
        <h3 class="font-semibold">{{ isZh ? '并发队列' : 'Concurrency Queue' }}</h3>
        <OperationState
          :state="panelState(queue, platforms.length > 0)"
          :message="localizePanelMessage(panelState(queue, platforms.length > 0), queue.error.value?.reason === 'OPS_DISABLED' || queue.data.value?.enabled === false ? (isZh ? '运维并发监控已关闭。' : 'Concurrency monitoring is disabled.') : undefined)"
        />
        <dl v-if="platforms.length" class="grid grid-cols-[1fr_auto] gap-x-2 gap-y-1 text-sm">
          <template v-for="platform in platforms" :key="platform.platform">
            <dt>{{ platform.platform }}</dt><dd>{{ isZh ? `${platform.waiting_in_queue} 个等待` : `${platform.waiting_in_queue} waiting` }}</dd>
          </template>
        </dl>
      </Card>
      <Card class="space-y-2">
        <h3 class="font-semibold">{{ isZh ? '实时流量' : 'Realtime Traffic' }}</h3>
        <OperationState
          :state="panelState(traffic, !!summary)"
          :message="localizePanelMessage(panelState(traffic, !!summary), traffic.error.value?.reason === 'OPS_DISABLED' || traffic.data.value?.enabled === false ? (isZh ? '实时监控已关闭。' : 'Realtime monitoring is disabled.') : undefined)"
        />
        <dl v-if="summary" class="grid grid-cols-[1fr_auto] gap-x-2 gap-y-1 text-sm">
          <dt>{{ isZh ? '当前 QPS' : 'Current QPS' }}</dt><dd class="font-mono tabular-nums">{{ summary.qps?.current ?? '—' }}</dd>
          <dt>{{ isZh ? '当前 TPS' : 'Current TPS' }}</dt><dd class="font-mono tabular-nums">{{ summary.tps?.current ?? '—' }}</dd>
          <dt>{{ isZh ? '统计结束时间' : 'Window End' }}</dt><dd class="font-mono text-xs tabular-nums">{{ formatDateTimeDisplay(summary.end_time) }}</dd>
        </dl>
      </Card>
    </div>

    <WorkspaceTelemetryPanel :is-zh="isZh" />

    <CatalogVerificationWorkbench :is-zh="isZh" />

    <Modal
      v-model:open="incidentModalOpen"
      :title="isZh ? '发布服务状态事件' : 'Publish Status Incident'"
      :description="isZh ? '发布的事件标题与说明将以纯文本形式展示在公开服务状态页。' : 'Title and message will be published as plain text on the public status page.'"
    >
      <div class="space-y-3 text-sm">
        <Input
          v-model="incidentTitle"
          :label="isZh ? '事件标题' : 'Incident Title'"
          :placeholder="isZh ? '例如：上游推理通道延迟升高' : 'e.g. Elevated upstream latency'"
        />
        <Select
          v-model="incidentSeverity"
          :label="isZh ? '影响等级' : 'Severity'"
          :options="[
            { value: 'degraded', label: isZh ? '性能降级 (degraded)' : 'Degraded' },
            { value: 'outage', label: isZh ? '服务中断 (outage)' : 'Outage' },
            { value: 'maintenance', label: isZh ? '计划维护 (maintenance)' : 'Maintenance' }
          ]"
        />
        <div class="space-y-1.5">
          <p class="text-xs font-medium text-oh-secondary">{{ isZh ? '受影响组件' : 'Affected Components' }}</p>
          <div v-if="incidentComponents.length" class="flex flex-wrap gap-2">
            <button
              v-for="comp in incidentComponents"
              :key="comp.id"
              type="button"
              :class="[
                'rounded-oh-md border px-2.5 py-1 text-xs transition-colors',
                incidentSelectedComponents.includes(comp.id)
                  ? 'border-oh-accent bg-oh-selected font-medium text-oh-accent-text'
                  : 'border-oh-default bg-oh-surface text-oh-secondary hover:border-oh-strong'
              ]"
              @click="toggleIncidentComponent(comp.id)"
            >
              {{ comp.name }}
            </button>
          </div>
          <p v-else class="text-xs text-oh-muted">
            {{ isZh ? '暂未加载到状态组件列表。' : 'No status components available.' }}
          </p>
        </div>
        <label class="grid gap-1 text-xs font-medium text-oh-secondary">
          <span>{{ isZh ? '初始进展说明' : 'Initial Update Message' }}</span>
          <textarea
            v-model="incidentMessage"
            rows="3"
            :placeholder="isZh ? '描述当前排查进展与预计恢复时间…' : 'Describe current investigation and impact…'"
            class="w-full rounded-oh-md border border-oh-default bg-oh-surface px-3 py-2 text-sm text-oh-primary focus:border-oh-accent focus:outline-none"
          />
        </label>
        <p v-if="incidentError" class="rounded-oh-md border border-red-300 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-300">
          {{ incidentError }}
        </p>
      </div>
      <template #footer>
        <Button size="sm" variant="ghost" :disabled="incidentSubmitting" @click="incidentModalOpen = false">
          {{ isZh ? '取消' : 'Cancel' }}
        </Button>
        <Button size="sm" variant="primary" :loading="incidentSubmitting" @click="submitIncident">
          {{ isZh ? '发布事件' : 'Publish' }}
        </Button>
      </template>
    </Modal>
  </section>
</template>
