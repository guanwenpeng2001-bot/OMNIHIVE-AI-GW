<script setup lang="ts">
import { computed, onMounted, onScopeDispose } from 'vue'
import { Button, Card } from '@/components/ui'
import { buildGatewayUrl } from '@/api/url'
import { getConcurrencyStats, getRealtimeTrafficSummary } from '@/api/admin/ops'
import { useAuthStore } from '@/stores/auth'
import { useScopedRead } from '@/features/requests/useScopedRead'
import type { AsyncViewState } from '@/features/shared/asyncState'
import OperationState from './OperationState.vue'

const auth = useAuthStore()
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
</script>

<template>
  <section aria-labelledby="operational-signals-title" class="space-y-3">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h2 id="operational-signals-title" class="font-semibold">运行信号</h2>
      <Button variant="secondary" size="sm" :disabled="refreshing" @click="refresh">刷新运行信号</Button>
    </div>
    <div class="grid gap-3 lg:grid-cols-3">
      <Card class="space-y-2">
        <h3 class="font-semibold">服务健康</h3>
        <div class="text-sm">进程存活
          <span v-if="live.data.value"> · 正常</span>
          <OperationState :state="panelState(live)" :message="panelState(live) === 'partial-error' ? '无法读取存活探针。' : undefined" />
        </div>
        <div class="text-sm">依赖就绪
          <span v-if="readiness.data.value"> · {{ readiness.data.value === 'ok' ? '就绪' : '未就绪（503）' }}</span>
          <OperationState :state="panelState(readiness)" :message="panelState(readiness) === 'partial-error' ? '无法读取就绪探针。' : undefined" />
        </div>
      </Card>
      <Card class="space-y-2">
        <h3 class="font-semibold">并发队列</h3>
        <OperationState :state="panelState(queue, platforms.length > 0)"
          :message="queue.error.value?.reason === 'OPS_DISABLED' || queue.data.value?.enabled === false ? '运维并发监控已关闭。' : undefined" />
        <dl v-if="platforms.length" class="grid grid-cols-[1fr_auto] gap-x-2 gap-y-1 text-sm">
          <template v-for="platform in platforms" :key="platform.platform">
            <dt>{{ platform.platform }}</dt><dd>{{ platform.waiting_in_queue }} 个等待</dd>
          </template>
        </dl>
      </Card>
      <Card class="space-y-2">
        <h3 class="font-semibold">实时流量</h3>
        <OperationState :state="panelState(traffic, !!summary)"
          :message="traffic.error.value?.reason === 'OPS_DISABLED' || traffic.data.value?.enabled === false ? '实时监控已关闭。' : undefined" />
        <dl v-if="summary" class="grid grid-cols-[1fr_auto] gap-x-2 gap-y-1 text-sm">
          <dt>当前 QPS</dt><dd>{{ summary.qps.current }}</dd>
          <dt>当前 TPS</dt><dd>{{ summary.tps.current }}</dd>
          <dt>统计结束时间</dt><dd>{{ new Date(summary.end_time).toLocaleString() }}</dd>
        </dl>
      </Card>
    </div>
  </section>
</template>
