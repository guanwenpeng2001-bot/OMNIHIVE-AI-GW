<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import PublicShell from '@/components/layout/PublicShell.vue'
import Badge from '@/components/ui/Badge.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorState from '@/components/ui/ErrorState.vue'
import { getPublicStatus, type PublicStatusResponse } from '@/features/public/api'

const status = ref<PublicStatusResponse | null>(null)
const loading = ref(true)
const error = ref<unknown>(null)

const components = computed(() => status.value?.components ?? [])

function stateTone(state?: string): 'success' | 'warning' | 'danger' | 'unknown' {
  if (!state) return 'unknown'
  if (state === 'operational') return 'success'
  if (state === 'degraded') return 'warning'
  if (state === 'outage') return 'danger'
  return 'unknown'
}

async function load(): Promise<void> {
  loading.value = true
  error.value = null
  try {
    status.value = await getPublicStatus()
  } catch (err) {
    error.value = err
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <PublicShell>
    <header class="border-b border-oh-default pb-6">
      <p class="font-mono text-xs uppercase tracking-[0.18em] text-oh-muted">STATUS</p>
      <h1 class="mt-3 text-3xl font-semibold tracking-tight text-oh-primary">Service status</h1>
      <p class="mt-2 text-sm text-oh-secondary">
        Published measurements only. Low-sample or missing data is reported as unknown, never as healthy.
      </p>
    </header>

    <Skeleton v-if="loading" variant="table-row" :lines="6" />
    <ErrorState v-else-if="error" title="Status unavailable" :message="String((error as any)?.message ?? error)" @retry="load" />

    <template v-else>
      <div class="divide-y divide-oh-subtle border border-oh-default">
        <div v-for="component in components" :key="component.id" class="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
          <div>
            <p class="text-sm font-medium text-oh-primary">{{ component.name ?? component.id }}</p>
            <p class="font-mono text-[11px] text-oh-muted">
              last success {{ component.last_success_at ?? '—' }}
            </p>
          </div>
          <div class="flex items-center gap-4 font-mono text-xs tabular-nums">
            <span class="text-oh-secondary">{{ component.samples ?? '—' }} samples</span>
            <span class="text-oh-secondary">
              {{ component.success_rate === null || component.success_rate === undefined ? '—' : `${(component.success_rate * 100).toFixed(2)}%` }}
            </span>
            <Badge :tone="stateTone(component.observed_state)" variant="subtle" dot>{{ component.observed_state ?? 'unknown' }}</Badge>
          </div>
        </div>
        <p v-if="components.length === 0" class="px-4 py-10 text-center text-sm text-oh-secondary">
          No published measurements yet.
        </p>
      </div>
      <p v-if="status?.as_of" class="mt-3 font-mono text-[11px] text-oh-muted">as of {{ status.as_of }}</p>
    </template>
  </PublicShell>
</template>
