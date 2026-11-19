<script setup lang="ts">
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

const canCompare = computed(() => selected.value.length >= 2 && selected.value.length <= 3)

function syncFromQuery(): void {
  const raw = String(route.query.ids ?? '')
  selected.value = raw ? raw.split(',').filter(Boolean).slice(0, 3) : []
}

function toggle(id: string): void {
  if (selected.value.includes(id)) {
    selected.value = selected.value.filter((item) => item !== id)
  } else if (selected.value.length < 3) {
    selected.value = [...selected.value, id]
  }
  void router.replace({ query: selected.value.length ? { ids: selected.value.join(',') } : {} })
  result.value = null
}

async function run(): Promise<void> {
  if (!canCompare.value) return
  loading.value = true
  error.value = null
  try {
    result.value = await comparePublicModels(selected.value)
  } catch (err) {
    error.value = err
  } finally {
    loading.value = false
  }
}

function metric(rowIndex: number, key: string): string {
  const row = result.value?.rows?.[rowIndex]
  const metrics = row?.metrics as Record<string, unknown> | null | undefined
  const value = metrics?.[key]
  if (value === null || value === undefined) return '—'
  if (typeof value === 'number') return value.toLocaleString()
  return String(value)
}

onMounted(async () => {
  syncFromQuery()
  try {
    const page = await listPublicModels({ limit: 60, sort: 'name' })
    options.value = page.items ?? []
  } catch (err) {
    error.value = err
  } finally {
    loadingOptions.value = false
  }
  if (canCompare.value) void run()
})
</script>

<template>
  <PublicShell>
    <header class="border-b border-oh-default pb-6">
      <p class="font-mono text-xs uppercase tracking-[0.18em] text-oh-muted">COMPARE</p>
      <h1 class="mt-3 text-3xl font-semibold tracking-tight text-oh-primary">Compare up to three models</h1>
      <p class="mt-2 text-sm text-oh-secondary">Pick models below; the comparison keeps your selection order and catalog version.</p>
    </header>

    <Skeleton v-if="loadingOptions" variant="table-row" :lines="6" />
    <template v-else>
      <div class="flex flex-wrap gap-2 py-5">
        <button
          v-for="option in options"
          :key="option.id"
          type="button"
          class="rounded-full border px-3 py-1 text-xs transition-colors disabled:opacity-40"
          :class="selected.includes(option.id)
            ? 'border-oh-accent bg-oh-accent-soft text-oh-accent-text'
            : 'border-oh-default text-oh-secondary hover:border-oh-strong'"
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

      <ErrorState v-if="error" title="Comparison failed" :message="String((error as any)?.message ?? error)" @retry="run" />

      <div v-else-if="result" class="overflow-x-auto border border-oh-default">
        <table class="w-full min-w-[640px] border-collapse text-sm">
          <thead>
            <tr class="border-b border-oh-default bg-oh-subtle text-left font-mono text-[11px] uppercase tracking-wider text-oh-muted">
              <th class="px-4 py-2.5">Metric</th>
              <th v-for="(row, index) in result.rows" :key="row.model_id" class="px-4 py-2.5 text-right">
                {{ options.find((item) => item.id === row.model_id)?.model ?? row.model_id }}
                <span class="ml-1 text-[10px] text-oh-muted">#{{ index + 1 }}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="key in ['success_rate', 'ttft_p50_ms', 'ttft_p95_ms', 'throughput_tps', 'samples']" :key="key"
              class="border-b border-oh-subtle last:border-0">
              <td class="px-4 py-2.5 font-mono text-xs text-oh-secondary">{{ key }}</td>
              <td v-for="(row, index) in result.rows" :key="row.model_id" class="px-4 py-2.5 text-right font-mono tabular-nums text-oh-primary">
                {{ metric(index, key) }}
              </td>
            </tr>
          </tbody>
        </table>
        <p v-if="result.as_of" class="border-t border-oh-default px-4 py-2 font-mono text-[11px] text-oh-muted">
          catalog {{ result.catalog_version ?? 'unknown' }} · as of {{ result.as_of }}
        </p>
      </div>

      <EmptyState
        v-else
        title="Nothing to compare yet"
        description="Select two or three models above, then run the comparison."
      />
    </template>
  </PublicShell>
</template>
