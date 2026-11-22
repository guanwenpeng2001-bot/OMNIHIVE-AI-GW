<script setup lang="ts">
import { extractApiErrorMessage } from '@/utils/apiError'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import PublicShell from '@/components/layout/PublicShell.vue'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'
import Drawer from '@/components/ui/Drawer.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorState from '@/components/ui/ErrorState.vue'
import SegmentedControl from '@/components/ui/SegmentedControl.vue'
import {
  componentDisplayName,
  evidenceStateLabel,
  formatPublicTimestamp,
  incidentSeverityLabel,
  incidentStateLabel,
  statusReasonLabel,
  statusStateLabel,
  statusStateTone
} from '@/features/public/format'
import {
  getPublicIncident,
  getPublicStatus,
  listPublicIncidents,
  type PublicStatusIncident,
  type PublicStatusIncidentSummary,
  type PublicStatusResponse,
  type PublicStatusWindow
} from '@/features/public/api'

const { locale } = useI18n()
const isZh = computed(() => String(locale?.value ?? '').startsWith('zh'))

const windowRange = ref<PublicStatusWindow>('24h')
const status = ref<PublicStatusResponse | null>(null)
const incidentItems = ref<PublicStatusIncidentSummary[]>([])
const loading = ref(true)
const error = ref<unknown>(null)

const detailOpen = ref(false)
const detailLoading = ref(false)
const detailError = ref<unknown>(null)
const activeIncidentDetail = ref<PublicStatusIncident | null>(null)
const selectedIncidentId = ref<string>('')
const selectedIncidentSummary = ref<PublicStatusIncidentSummary | null>(null)
const compactHistoryViewport = ref(typeof window !== 'undefined' ? window.innerWidth < 640 : false)
let statusRequestSeq = 0
let statusAbortController: AbortController | null = null
let detailRequestSeq = 0
let detailAbortController: AbortController | null = null

function isAbortError(err: unknown): boolean {
  if (!err || typeof err !== 'object') return false
  const candidate = err as { name?: string; code?: string }
  return candidate.name === 'AbortError' || candidate.name === 'CanceledError' || candidate.code === 'ERR_CANCELED'
}

const components = computed(() => status.value?.components ?? [])
const isOverallUnknown = computed(() => !status.value?.overall || status.value.overall === 'unknown')

function updateViewportMode(): void {
  if (typeof window === 'undefined') return
  compactHistoryViewport.value = window.innerWidth < 640
}

function visibleHistoryDays<T>(history?: T[] | null): T[] {
  if (!history?.length) return []
  const maxBars = compactHistoryViewport.value ? 30 : 90
  return history.length > maxBars ? history.slice(-maxBars) : history
}

const mergedIncidents = computed<PublicStatusIncidentSummary[]>(() => {
  const byId = new Map<string, PublicStatusIncidentSummary>()
  for (const item of status.value?.incidents ?? []) {
    byId.set(item.id, item)
  }
  for (const item of incidentItems.value) {
    if (!byId.has(item.id)) byId.set(item.id, item)
  }
  return [...byId.values()].sort((a, b) => b.started_at.localeCompare(a.started_at))
})

function severityTone(severity?: string): 'danger' | 'warning' | 'info' {
  if (severity === 'outage') return 'danger'
  if (severity === 'degraded') return 'warning'
  return 'info'
}

function formatPercent(val?: number | null): string {
  if (val === null || val === undefined || !Number.isFinite(val)) return '—'
  const normalized = val <= 1 && val >= 0 ? val * 100 : val
  return `${normalized.toFixed(2)}%`
}

async function load(): Promise<void> {
  const seq = ++statusRequestSeq
  statusAbortController?.abort()
  const controller = new AbortController()
  statusAbortController = controller
  loading.value = true
  error.value = null
  try {
    const [statusResp, incidentsResp] = await Promise.all([
      getPublicStatus(windowRange.value, { signal: controller.signal }),
      listPublicIncidents({ limit: 20 }, { signal: controller.signal }).catch(() => ({ items: [] }))
    ])
    if (seq !== statusRequestSeq) return
    status.value = statusResp
    incidentItems.value = incidentsResp.items ?? []
  } catch (err) {
    if (seq !== statusRequestSeq || isAbortError(err)) return
    error.value = err
  } finally {
    if (seq === statusRequestSeq) {
      loading.value = false
    }
  }
}

function onWindowChange(next: string): void {
  windowRange.value = next as PublicStatusWindow
  void load()
}

async function inspectIncident(summary: PublicStatusIncidentSummary): Promise<void> {
  const seq = ++detailRequestSeq
  detailAbortController?.abort()
  const controller = new AbortController()
  detailAbortController = controller
  selectedIncidentSummary.value = summary
  selectedIncidentId.value = summary.id
  activeIncidentDetail.value = null
  detailError.value = null
  detailOpen.value = true
  detailLoading.value = true
  try {
    const detail = await getPublicIncident(summary.id, { signal: controller.signal })
    if (seq !== detailRequestSeq) return
    activeIncidentDetail.value = detail
  } catch (err) {
    if (seq !== detailRequestSeq || isAbortError(err)) return
    detailError.value = err
  } finally {
    if (seq === detailRequestSeq) {
      detailLoading.value = false
    }
  }
}

onMounted(() => {
  updateViewportMode()
  if (typeof window !== 'undefined') {
    window.addEventListener('resize', updateViewportMode, { passive: true })
  }
  void load()
})

onBeforeUnmount(() => {
  statusRequestSeq++
  detailRequestSeq++
  statusAbortController?.abort()
  detailAbortController?.abort()
  statusAbortController = null
  detailAbortController = null
  if (typeof window !== 'undefined') {
    window.removeEventListener('resize', updateViewportMode)
  }
})
</script>

<template>
  <PublicShell>
    <header class="flex flex-wrap items-end justify-between gap-4 border-b border-oh-default pb-6">
      <div>
        <p class="font-mono text-xs uppercase tracking-[0.18em] text-oh-muted">
          {{ isZh ? '网关遥测 · SERVICE STATUS' : 'STATUS' }}
        </p>
        <h1 class="mt-3 text-3xl font-semibold tracking-tight text-oh-primary">
          {{ isZh ? '公开服务运行状态' : 'Service status' }}
        </h1>
        <p class="mt-2 max-w-[68ch] text-sm text-oh-secondary">
          {{
            isZh
              ? '仅展示基于网关真实流量的被动观测结果。当采样窗口内样本数低于最小统计阈值时，诚实报告为“样本不足”，绝不伪造健康状态。'
              : 'Published passive gateway measurements only. Low-sample or stale windows are explicitly reported as unobserved, never assumed healthy.'
          }}
        </p>
      </div>
      <div class="flex items-center gap-2">
        <SegmentedControl
          :model-value="windowRange"
          :options="[
            { value: '1h', label: '1H' },
            { value: '24h', label: '24H' },
            { value: '7d', label: '7D' },
            { value: '30d', label: '30D' }
          ]"
          @update:model-value="onWindowChange"
        />
        <Button variant="secondary" size="sm" :loading="loading" @click="load">
          {{ isZh ? '刷新' : 'Refresh' }}
        </Button>
      </div>
    </header>

    <Skeleton v-if="loading" class="py-6" :lines="6" :label="isZh ? '正在加载网关运行状态' : 'Loading gateway status'" />
    <ErrorState
      v-else-if="error"
      :title="isZh ? '暂时无法获取公开服务状态' : 'Status unavailable'"
      :message="extractApiErrorMessage(error, '')"
      :retry-label="isZh ? '重新加载' : 'Retry'"
      @retry="load"
    />

    <template v-else>
      <section class="my-6 flex flex-wrap items-center justify-between gap-4 rounded-oh-md border border-oh-default bg-oh-surface p-4 sm:p-5">
        <div class="flex flex-wrap items-center gap-3">
          <Badge :tone="statusStateTone(status?.overall)" size="md" variant="subtle" dot>
            {{ statusStateLabel(status?.overall, isZh) }}
          </Badge>
          <span class="text-sm font-medium text-oh-primary">
            {{
              status?.active_incident_count && status.active_incident_count !== '0'
                ? (isZh ? `当前有 ${status.active_incident_count} 起活跃事件处理中` : `${status.active_incident_count} active incident(s)`)
                : (isZh ? '当前无活跃故障通报' : 'No active service incidents')
            }}
          </span>
        </div>
        <div class="flex flex-wrap items-center gap-x-4 gap-y-1 font-mono text-[11px] text-oh-muted">
          <span v-if="status?.window_start && status?.window_end">
            {{ isZh ? '窗口' : 'Window' }}: {{ formatPublicTimestamp(status.window_start) }} → {{ formatPublicTimestamp(status.window_end) }}
          </span>
          <span v-if="status?.data_lag_seconds != null">
            {{ isZh ? `聚合延迟 ${status.data_lag_seconds}s` : `Lag ${status.data_lag_seconds}s` }}
          </span>
          <span v-if="status?.as_of">
            {{ isZh ? `更新于 ${formatPublicTimestamp(status.as_of)}` : `As of ${formatPublicTimestamp(status.as_of)}` }}
          </span>
        </div>
      </section>

      <section
        v-if="isOverallUnknown"
        role="status"
        class="mb-6 rounded-oh-md border border-oh-accent/30 bg-oh-accent-subtle/50 px-4 py-3.5 text-xs text-oh-primary"
      >
        <div class="flex flex-wrap items-start justify-between gap-2">
          <div class="min-w-0 space-y-1">
            <p class="font-semibold text-oh-primary">
              {{
                isZh
                  ? '正在汇聚实时探测与网关流量样本 · Aggregating live probe & gateway telemetry'
                  : 'Aggregating live probe & gateway telemetry · 正在汇聚实时探测与网关流量样本'
              }}
            </p>
            <p class="leading-relaxed text-oh-secondary">
              {{
                isZh
                  ? '当前所选统计窗口内的有效请求样本数尚未达到最小置信阈值，因此系统如实标记为“样本不足 / 未知”而非默认展示 100% 正常。您可以切换 7D / 30D 窗口，或在网关持续产生请求后刷新查看实时成功率与 TTFT 分位。'
                  : 'The selected rollup window has not yet reached the minimum statistical sample threshold, so status stays transparently marked as unobserved rather than assuming 100% uptime. Try a wider 7D / 30D window or refresh as live gateway traffic accumulates.'
              }}
            </p>
          </div>
          <Badge tone="accent" size="xs" variant="outline" mono>
            {{ windowRange.toUpperCase() }}
          </Badge>
        </div>
      </section>

      <section aria-labelledby="status-components-heading">
        <h2 id="status-components-heading" class="mb-3 text-sm font-semibold text-oh-primary">
          {{ isZh ? '网关子系统观测指标' : 'Gateway Components' }}
        </h2>

        <div v-if="components.length > 0" class="min-w-0 overflow-hidden divide-y divide-oh-subtle rounded-oh-md border border-oh-default bg-oh-surface">
          <div v-for="component in components" :key="component.id" class="min-w-0 space-y-2.5 px-4 py-4">
            <div class="flex min-w-0 flex-wrap items-start justify-between gap-3">
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <p class="text-sm font-medium text-oh-primary">
                    {{ componentDisplayName(component.id, component.name, isZh) }}
                  </p>
                  <code class="font-mono text-[11px] text-oh-muted">{{ component.id }}</code>
                </div>
                <div class="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 font-mono text-[11px] text-oh-muted">
                  <span>
                    {{ isZh ? '最近成功：' : 'Last success: ' }}{{ formatPublicTimestamp(component.last_success) }}
                  </span>
                  <span v-if="component.freshness">
                    · {{ evidenceStateLabel(component.freshness, isZh) }}
                  </span>
                  <span v-if="component.reason" class="text-oh-secondary">
                    · {{ statusReasonLabel(component.reason, isZh) }}
                  </span>
                </div>
              </div>

              <div class="flex flex-wrap items-center gap-x-4 gap-y-2 font-mono text-xs tabular-nums">
                <div class="text-right">
                  <span class="block text-[10px] uppercase text-oh-muted">{{ isZh ? '有效样本' : 'Samples' }}</span>
                  <span class="text-oh-primary">
                    {{ component.sample_count ?? '0' }}
                    <template v-if="component.minimum_sample_count">
                      <span class="text-oh-muted">/ {{ component.minimum_sample_count }}</span>
                    </template>
                  </span>
                </div>
                <div class="text-right">
                  <span class="block text-[10px] uppercase text-oh-muted">{{ isZh ? '成功率' : 'Success' }}</span>
                  <span class="text-oh-primary">{{ formatPercent(component.success_rate) }}</span>
                </div>
                <div class="text-right">
                  <span class="block text-[10px] uppercase text-oh-muted">TTFT P50 / P95</span>
                  <span class="text-oh-primary">
                    {{ component.ttft_p50_ms != null ? `${component.ttft_p50_ms}ms` : '—' }}
                    /
                    {{ component.ttft_p95_ms != null ? `${component.ttft_p95_ms}ms` : '—' }}
                  </span>
                </div>
                <Badge :tone="statusStateTone(component.state ?? component.observed_state)" variant="subtle" dot>
                  {{ statusStateLabel(component.state ?? component.observed_state, isZh) }}
                </Badge>
              </div>
            </div>

            <div v-if="component.history?.length" class="min-w-0 space-y-1 pt-1">
              <div
                class="flex min-w-0 max-w-full items-center gap-0.5 overflow-hidden sm:gap-1"
                :aria-label="isZh ? '每日健康历史' : 'Daily status history'"
              >
                <span
                  v-for="day in visibleHistoryDays(component.history)"
                  :key="day.date"
                  :title="`${day.date} · ${statusStateLabel(day.state, isZh)} (${day.sample_count} samples)`"
                  :class="[
                    'h-2.5 min-w-0 flex-1 rounded-[2px]',
                    day.state === 'operational'
                      ? 'bg-emerald-500/80'
                      : day.state === 'outage'
                        ? 'bg-red-500/80'
                        : day.state === 'degraded' || day.state === 'maintenance'
                          ? 'bg-amber-500/80'
                          : 'bg-oh-default/40 sm:border sm:border-oh-default sm:bg-oh-subtle'
                  ]"
                />
              </div>
              <div class="flex items-center justify-between font-mono text-[10px] text-oh-muted">
                <span>{{ visibleHistoryDays(component.history)[0]?.date }}</span>
                <span>
                  {{
                    isZh
                      ? `展示最近 ${visibleHistoryDays(component.history).length} 天`
                      : `Last ${visibleHistoryDays(component.history).length}d`
                  }}
                </span>
                <span>{{ visibleHistoryDays(component.history)[visibleHistoryDays(component.history).length - 1]?.date }}</span>
              </div>
            </div>
          </div>
        </div>

        <EmptyState
          v-else
          compact
          :title="isZh ? '尚未发布子系统观测指标' : 'No published component measurements yet'"
          :description="isZh ? '网关聚合任务完成首轮流量统计后将自动展示各协议层指标。' : 'Component telemetry appears automatically after the initial rollup cycle.'"
        />
      </section>

      <section class="mt-8" aria-labelledby="status-incidents-heading">
        <div class="mb-3 flex items-center justify-between gap-2">
          <h2 id="status-incidents-heading" class="text-sm font-semibold text-oh-primary">
            {{ isZh ? '服务事件与历史通报' : 'Incidents & Maintenance History' }}
          </h2>
          <span class="font-mono text-xs text-oh-muted">
            {{ isZh ? `共 ${mergedIncidents.length} 条记录` : `${mergedIncidents.length} record(s)` }}
          </span>
        </div>

        <div
          v-if="mergedIncidents.length > 0"
          class="divide-y divide-oh-subtle rounded-oh-md border border-oh-default bg-oh-surface"
        >
          <div
            v-for="inc in mergedIncidents"
            :key="inc.id"
            class="flex flex-wrap items-center justify-between gap-3 px-4 py-3.5 hover:bg-oh-hover"
          >
            <div class="min-w-0 space-y-1">
              <div class="flex flex-wrap items-center gap-2">
                <Badge :tone="severityTone(inc.severity)" size="xs" variant="subtle">
                  {{ incidentSeverityLabel(inc.severity, isZh) }}
                </Badge>
                <Badge :tone="inc.state === 'resolved' ? 'success' : 'warning'" size="xs" variant="outline">
                  {{ incidentStateLabel(inc.state, isZh) }}
                </Badge>
                <span class="font-medium text-oh-primary">{{ inc.title }}</span>
              </div>
              <div class="flex flex-wrap items-center gap-x-3 gap-y-1 font-mono text-[11px] text-oh-muted">
                <span>{{ isZh ? '发生于' : 'Started' }} {{ formatPublicTimestamp(inc.started_at) }}</span>
                <span v-if="inc.resolved_at">
                  · {{ isZh ? '恢复于' : 'Resolved' }} {{ formatPublicTimestamp(inc.resolved_at) }}
                </span>
                <span v-if="inc.component_ids?.length">
                  · {{ inc.component_ids.join(', ') }}
                </span>
              </div>
            </div>
            <Button variant="secondary" size="xs" @click="inspectIncident(inc)">
              {{ isZh ? '查看进展' : 'Timeline' }}
            </Button>
          </div>
        </div>
        <EmptyState
          v-else
          compact
          :title="isZh ? '近期无服务异常或计划维护记录' : 'No incidents or maintenance events recorded'"
        />
      </section>
    </template>

    <Drawer
      :open="detailOpen"
      :title="activeIncidentDetail?.title || (isZh ? '事件进展时间线' : 'Incident Timeline')"
      :description="selectedIncidentId ? `ID: ${selectedIncidentId}` : undefined"
      size="md"
      @update:open="detailOpen = $event"
    >
      <Skeleton v-if="detailLoading" :lines="5" :label="isZh ? '正在加载事件详情' : 'Loading incident updates'" />
      <ErrorState
        v-else-if="detailError"
        compact
        :title="isZh ? '事件时间线加载失败' : 'Failed to load incident updates'"
        :message="extractApiErrorMessage(detailError, '')"
        :retry-label="isZh ? '重试' : 'Retry'"
        @retry="selectedIncidentSummary ? inspectIncident(selectedIncidentSummary) : (detailOpen = false)"
      />
      <div v-else-if="activeIncidentDetail" class="space-y-4">
        <div class="flex flex-wrap items-center gap-2">
          <Badge :tone="severityTone(activeIncidentDetail.severity)" variant="subtle">
            {{ incidentSeverityLabel(activeIncidentDetail.severity, isZh) }}
          </Badge>
          <Badge :tone="activeIncidentDetail.state === 'resolved' ? 'success' : 'warning'" variant="outline">
            {{ incidentStateLabel(activeIncidentDetail.state, isZh) }}
          </Badge>
        </div>

        <div v-if="activeIncidentDetail.component_ids?.length" class="text-xs text-oh-secondary">
          {{ isZh ? '受影响组件：' : 'Affected components: ' }}
          <span class="font-mono text-oh-primary">{{ activeIncidentDetail.component_ids.join(', ') }}</span>
        </div>

        <ol class="space-y-3 border-l border-oh-default pl-4">
          <li
            v-for="(upd, idx) in activeIncidentDetail.updates ?? []"
            :key="`${upd.at}-${idx}`"
            class="space-y-1"
          >
            <div class="flex items-center gap-2 font-mono text-[11px] text-oh-muted">
              <Badge tone="neutral" size="xs" variant="subtle">
                {{ incidentStateLabel(upd.state, isZh) }}
              </Badge>
              <span>{{ formatPublicTimestamp(upd.at) }}</span>
            </div>
            <p class="text-sm leading-relaxed text-oh-primary">{{ upd.message }}</p>
          </li>
        </ol>
      </div>
    </Drawer>
  </PublicShell>
</template>
