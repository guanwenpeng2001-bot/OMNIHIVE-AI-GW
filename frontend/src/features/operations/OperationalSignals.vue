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
import WorkspaceTelemetryPanel from './components/WorkspaceTelemetryPanel.vue'
import {
  generateCommandKey,
  requestGet,
  requestPost,
  type StatusSeverity,
  type StatusSummary
} from './components/operationsApi'
import { formatDateTimeDisplay } from './components/operationsHelpers'

const { t, locale } = useI18n()
const isZh = computed(() => String(locale?.value || '').toLowerCase().startsWith('zh'))
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
  return t(`adminPlatform.ops.state.${viewState}`)
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
      const summary = await requestGet<StatusSummary>('/public/status')
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
    incidentError.value = t('adminPlatform.ops.titleAndMessageAreRequired')
    return
  }
  if (incidentSelectedComponents.value.length === 0) {
    incidentError.value = t('adminPlatform.ops.selectAtLeastOneAffectedComponent')
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
      title: t('adminPlatform.ops.incidentPublished'),
      description: t('adminPlatform.ops.thePublicStatusPageHasBeenUpdated')
    })
  } catch (cause) {
    const issue = classifyAsyncError(cause, t('adminPlatform.ops.failedToPublishIncident'))
    incidentError.value = issue.message
  } finally {
    incidentSubmitting.value = false
  }
}
</script>

<template>
  <section aria-labelledby="operational-signals-title" class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h2 id="operational-signals-title" class="font-semibold">{{ t('adminPlatform.ops.operationalSignals') }}</h2>
      <div class="flex flex-wrap items-center gap-2">
        <Button v-if="isAdmin" variant="secondary" size="sm" @click="openIncidentModal">
          {{ t('adminPlatform.ops.publishIncident') }}
        </Button>
        <Button variant="secondary" size="sm" :disabled="refreshing" @click="refresh">
          {{ t('adminPlatform.ops.refreshSignals') }}
        </Button>
      </div>
    </div>
    <div class="grid gap-3 lg:grid-cols-3">
      <Card class="space-y-2">
        <h3 class="font-semibold">{{ t('adminPlatform.ops.serviceHealth') }}</h3>
        <div class="text-sm">{{ t('adminPlatform.ops.liveness') }}
          <span v-if="live.data.value"> · {{ t('adminPlatform.ops.ok') }}</span>
          <OperationState
            :state="panelState(live)"
            :message="localizePanelMessage(panelState(live), panelState(live) === 'partial-error' ? (t('adminPlatform.ops.unableToReadLivenessProbe')) : undefined)"
          />
        </div>
        <div class="text-sm">{{ t('adminPlatform.ops.readiness') }}
          <span v-if="readiness.data.value"> · {{ readiness.data.value === 'ok' ? (t('adminPlatform.ops.ready')) : (t('adminPlatform.ops.notReady503')) }}</span>
          <OperationState
            :state="panelState(readiness)"
            :message="localizePanelMessage(panelState(readiness), panelState(readiness) === 'partial-error' ? (t('adminPlatform.ops.unableToReadReadinessProbe')) : undefined)"
          />
        </div>
      </Card>
      <Card class="space-y-2">
        <h3 class="font-semibold">{{ t('adminPlatform.ops.concurrencyQueue') }}</h3>
        <OperationState
          :state="panelState(queue, platforms.length > 0)"
          :message="localizePanelMessage(panelState(queue, platforms.length > 0), queue.error.value?.reason === 'OPS_DISABLED' || queue.data.value?.enabled === false ? (t('adminPlatform.ops.concurrencyMonitoringIsDisabled')) : undefined)"
        />
        <dl v-if="platforms.length" class="grid grid-cols-[1fr_auto] gap-x-2 gap-y-1 text-sm">
          <template v-for="platform in platforms" :key="platform.platform">
            <dt>{{ platform.platform }}</dt><dd>{{ t('adminPlatform.ops.waiting', { count: platform.waiting_in_queue }) }}</dd>
          </template>
        </dl>
      </Card>
      <Card class="space-y-2">
        <h3 class="font-semibold">{{ t('adminPlatform.ops.realtimeTraffic') }}</h3>
        <OperationState
          :state="panelState(traffic, !!summary)"
          :message="localizePanelMessage(panelState(traffic, !!summary), traffic.error.value?.reason === 'OPS_DISABLED' || traffic.data.value?.enabled === false ? (t('adminPlatform.ops.realtimeMonitoringIsDisabled')) : undefined)"
        />
        <dl v-if="summary" class="grid grid-cols-[1fr_auto] gap-x-2 gap-y-1 text-sm">
          <dt>{{ t('adminPlatform.ops.currentQPS') }}</dt><dd class="font-mono tabular-nums">{{ summary.qps?.current ?? '—' }}</dd>
          <dt>{{ t('adminPlatform.ops.currentTPS') }}</dt><dd class="font-mono tabular-nums">{{ summary.tps?.current ?? '—' }}</dd>
          <dt>{{ t('adminPlatform.ops.windowEnd') }}</dt><dd class="font-mono text-xs tabular-nums">{{ formatDateTimeDisplay(summary.end_time) }}</dd>
        </dl>
      </Card>
    </div>

    <WorkspaceTelemetryPanel :is-zh="isZh" />

    <RouterLink to="/admin/models" data-testid="ops-relocated-catalog-workbench-link" class="text-xs underline">
      {{ t('adminPlatform.ops.viewModelCatalog') }}
    </RouterLink>

    <Modal
      v-model:open="incidentModalOpen"
      :title="t('adminPlatform.ops.publishStatusIncident')"
      :description="t('adminPlatform.ops.titleAndMessageWillBePublishedAs')"
    >
      <div class="space-y-3 text-sm">
        <Input
          v-model="incidentTitle"
          :label="t('adminPlatform.ops.incidentTitle')"
          :placeholder="t('adminPlatform.ops.eGElevatedUpstreamLatency')"
        />
        <Select
          v-model="incidentSeverity"
          :label="t('adminPlatform.ops.severity')"
          :options="[
            { value: 'degraded', label: t('adminPlatform.ops.degraded') },
            { value: 'outage', label: t('adminPlatform.ops.outage') },
            { value: 'maintenance', label: t('adminPlatform.ops.maintenance') }
          ]"
        />
        <div class="space-y-1.5">
          <p class="text-xs font-medium text-oh-secondary">{{ t('adminPlatform.ops.affectedComponents') }}</p>
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
            {{ t('adminPlatform.ops.noStatusComponentsAvailable') }}
          </p>
        </div>
        <label class="grid gap-1 text-xs font-medium text-oh-secondary">
          <span>{{ t('adminPlatform.ops.initialUpdateMessage') }}</span>
          <textarea
            v-model="incidentMessage"
            rows="3"
            :placeholder="t('adminPlatform.ops.describeCurrentInvestigationAndImpact')"
            class="w-full rounded-oh-md border border-oh-default bg-oh-surface px-3 py-2 text-sm text-oh-primary focus:border-oh-accent focus:outline-none"
          />
        </label>
        <p v-if="incidentError" class="rounded-oh-md border border-red-300 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-300">
          {{ incidentError }}
        </p>
      </div>
      <template #footer>
        <Button size="sm" variant="ghost" :disabled="incidentSubmitting" @click="incidentModalOpen = false">
          {{ t('adminPlatform.ops.cancel') }}
        </Button>
        <Button size="sm" variant="primary" :loading="incidentSubmitting" @click="submitIncident">
          {{ t('adminPlatform.ops.publish') }}
        </Button>
      </template>
    </Modal>
  </section>
</template>
