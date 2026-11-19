<script setup lang="ts">
import { extractApiErrorMessage } from '@/utils/apiError'
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import PublicShell from '@/components/layout/PublicShell.vue'
import Button from '@/components/ui/Button.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import ErrorState from '@/components/ui/ErrorState.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import { comparePublicModels, listPublicModels, type PublicCompareResponse, type PublicModelSummary } from '@/features/public/api'

const route = useRoute()
const router = useRouter()
const options = ref<PublicModelSummary[]>([])
const selected = ref<string[]>([])
const result = ref<PublicCompareResponse | null>(null)
const loading = ref(false)
const loadingOptions = ref(true)
const error = ref<unknown>(null)
const optionsError = ref<unknown>(null)
let request = 0

const canCompare = computed(() => selected.value.length >= 2 && selected.value.length <= 3)

function syncFromQuery(): void {
  const raw = String(route.query.ids ?? '')
  selected.value = raw ? [...new Set(raw.split(',').filter(Boolean))].slice(0, 3) : []
}

function toggle(id: string): void {
  if (selected.value.includes(id)) {
    selected.value = selected.value.filter((item) => item !== id)
  } else if (selected.value.length < 3) {
    selected.value = [...selected.value, id]
  }
  request++
  loading.value = false
  error.value = null
  void router.replace({ query: selected.value.length ? { ids: selected.value.join(',') } : {} })
  result.value = null
}

async function run(): Promise<void> {
  if (!canCompare.value || loading.value) return
  const currentRequest = ++request
  loading.value = true
  error.value = null
  try {
    const response = await comparePublicModels(selected.value)
    if (currentRequest === request) result.value = response
  } catch (err) {
    if (currentRequest === request) error.value = err
  } finally {
    if (currentRequest === request) loading.value = false
  }
}

function formatMetric(value?: number | string | null, suffix = ''): string {
  return value == null ? '—' : `${value.toLocaleString()}${suffix}`
}

const metricRows = computed(() => [
  { label: 'Success rate', values: result.value?.rows.map(({ metrics }) =>
    metrics?.success_rate == null ? '—' : `${(metrics.success_rate * 100).toFixed(2)}%`) },
  { label: 'Median TTFT', values: result.value?.rows.map(({ metrics }) => formatMetric(metrics?.ttft_p50_ms, ' ms')) },
  { label: 'P95 TTFT', values: result.value?.rows.map(() => '—') },
  { label: 'Throughput', values: result.value?.rows.map(({ metrics }) => formatMetric(metrics?.output_tokens_per_second, ' t/s')) },
  { label: 'Requests', values: result.value?.rows.map(({ metrics }) => formatMetric(metrics?.requests)) }
])

async function loadOptions(): Promise<void> {
  loadingOptions.value = true
  optionsError.value = null
  try {
    const page = await listPublicModels({ limit: 60, sort: 'name' })
    options.value = page.items ?? []
  } catch (err) {
    optionsError.value = err
    return
  } finally {
    loadingOptions.value = false
  }
  if (canCompare.value) void run()
}

onMounted(() => {
  syncFromQuery()
  void loadOptions()
})
</script>

<template>
  <PublicShell>
    <header class="border-b border-oh-default pb-6">
      <p class="font-mono text-xs uppercase tracking-[0.18em] text-oh-muted">COMPARE</p>
      <h1 class="mt-3 text-3xl font-semibold tracking-tight text-oh-primary">Compare up to three models</h1>
      <p class="mt-2 text-sm text-oh-secondary">Pick models below; the comparison keeps your selection order and catalog version.</p>
    </header>

    <Skeleton v-if="loadingOptions" class="py-4" :lines="6" />
    <ErrorState v-else-if="optionsError" title="Model choices unavailable" @retry="loadOptions" />
    <EmptyState v-else-if="!options.length" title="No models available to compare" />
    <template v-else>
      <div class="flex flex-wrap gap-2 py-5">
        <button
          v-for="option in options"
          :key="option.id"
          type="button"
          class="oh-focus-ring max-w-full break-all rounded-full border px-3 py-1 text-xs transition-colors disabled:opacity-40"
          :class="selected.includes(option.id)
            ? 'border-oh-accent bg-oh-accent-subtle text-oh-accent-text'
            : 'border-oh-default text-oh-secondary hover:border-oh-strong'"
          :aria-pressed="selected.includes(option.id)"
          :disabled="!selected.includes(option.id) && selected.length >= 3"
          @click="toggle(option.id)"
        >
          {{ option.model }}
        </button>
      </div>
      <div class="flex items-center gap-3 pb-4">
        <Button :disabled="!canCompare" :loading="loading" @click="run">Compare {{ selected.length }} models</Button>
        <span class="text-xs text-oh-muted">{{ selected.length }}/3 selected</span>
      </div>

      <Skeleton v-if="loading" class="py-4" :lines="5" label="Loading comparison" />
      <ErrorState v-else-if="error" title="Comparison failed" :message="extractApiErrorMessage(error, '')" @retry="run" />

      <div v-else-if="result?.rows.length" class="oh-focus-ring overflow-x-auto border border-oh-default" tabindex="0" role="region" aria-label="Model comparison table">
        <table class="w-full min-w-[540px] table-fixed border-collapse text-sm">
          <thead>
            <tr class="border-b border-oh-default bg-oh-subtle text-left font-mono text-[11px] uppercase tracking-wider text-oh-muted">
              <th class="px-4 py-2.5">Metric</th>
              <th v-for="(row, index) in result.rows" :key="row.model_id" class="break-all px-4 py-2.5 text-right">
                {{ options.find((item) => item.id === row.model_id)?.model ?? row.model_id }}
                <span class="ml-1 text-[10px] text-oh-muted">#{{ index + 1 }}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="metric in metricRows" :key="metric.label"
              class="border-b border-oh-subtle last:border-0">
              <td class="px-4 py-2.5 font-mono text-xs text-oh-secondary">{{ metric.label }}</td>
              <td v-for="(value, index) in metric.values" :key="index" class="px-4 py-2.5 text-right font-mono tabular-nums text-oh-primary">
                {{ value }}
              </td>
            </tr>
          </tbody>
        </table>
        <p v-if="result.as_of" class="border-t border-oh-default px-4 py-2 font-mono text-[11px] text-oh-muted">
          catalog {{ result.catalog_version ?? '—' }} · as of {{ result.as_of }}
        </p>
      </div>

      <EmptyState
        v-else
        :title="result ? 'No comparison data published' : 'Nothing to compare yet'"
        :description="result ? undefined : 'Select two or three models above, then run the comparison.'"
      />
    </template>
  </PublicShell>
</template>
