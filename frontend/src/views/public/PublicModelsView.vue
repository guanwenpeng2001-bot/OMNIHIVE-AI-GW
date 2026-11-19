<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import PublicShell from '@/components/layout/PublicShell.vue'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import ErrorState from '@/components/ui/ErrorState.vue'
import Input from '@/components/ui/Input.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import SegmentedControl from '@/components/ui/SegmentedControl.vue'
import { listPublicModels, type ListParams, type PublicModelSummary } from '@/features/public/api'

type ViewMode = 'table' | 'cards'

const models = ref<PublicModelSummary[]>([])
const nextCursor = ref<string | null>(null)
const asOf = ref<string | null>(null)
const loading = ref(false)
const loadingMore = ref(false)
const error = ref<unknown>(null)
const query = ref('')
const sort = ref<NonNullable<ListParams['sort']>>('name')
const viewMode = ref<ViewMode>('table')
const providers = ref<string[]>([])

const providerOptions = computed(() => {
  const seen = new Set<string>()
  for (const model of models.value) {
    const provider = model.provider?.trim()
    if (provider) seen.add(provider)
  }
  return Array.from(seen).sort()
})

const activeFilters = computed(() => providers.value.length > 0 || query.value.trim().length > 0)

async function load(reset = true): Promise<void> {
  if (reset) {
    loading.value = true
    error.value = null
  } else {
    loadingMore.value = true
  }
  try {
    const page = await listPublicModels({
      q: query.value.trim() || undefined,
      provider: providers.value.length ? providers.value : undefined,
      sort: sort.value,
      cursor: reset ? undefined : nextCursor.value,
      limit: 20
    })
    models.value = reset ? page.items ?? [] : [...models.value, ...(page.items ?? [])]
    nextCursor.value = page.next_cursor ?? null
    asOf.value = page.as_of ?? null
  } catch (err) {
    error.value = err
  } finally {
    loading.value = false
    loadingMore.value = false
  }
}

function toggleProvider(provider: string): void {
  providers.value = providers.value.includes(provider)
    ? providers.value.filter((item) => item !== provider)
    : [...providers.value, provider]
  void load()
}

function resetFilters(): void {
  query.value = ''
  providers.value = []
  sort.value = 'name'
  void load()
}

function priceText(rate: PublicModelSummary['input_price']): string {
  if (!rate) return '—'
  const amount = Number(rate.amount)
  if (!Number.isFinite(amount) || amount === 0) return rate.currency === 'USD' ? 'Free' : rate.amount
  const per = Number(rate.per) || 1_000_000
  return `$${(amount * 1_000_000 / per).toFixed(2)}/M`
}

function contextText(value?: number | null): string {
  if (!value) return '—'
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`
  if (value >= 1_000) return `${Math.round(value / 1_000)}K`
  return String(value)
}

onMounted(() => void load())
</script>

<template>
  <PublicShell>
    <section class="border-b border-oh-default pb-6">
      <p class="font-mono text-xs uppercase tracking-[0.18em] text-oh-muted">MODEL DIRECTORY</p>
      <h1 class="mt-3 text-3xl font-semibold tracking-tight text-oh-primary sm:text-4xl">Models</h1>
      <p class="mt-2 max-w-2xl text-sm text-oh-secondary">
        Compare capabilities, pricing and observed performance across every provider OmniHive routes to.
        Unknown values stay unknown; they are never reported as zero.
      </p>
    </section>

    <section class="flex flex-col gap-4 py-6">
      <div class="flex flex-wrap items-center gap-3">
        <Input v-model="query" class="w-full sm:w-72" placeholder="Search models, providers..." @keyup.enter="load()" />
        <Button variant="secondary" @click="load()">Search</Button>
        <div class="ml-auto flex items-center gap-2">
          <SegmentedControl
            :model-value="viewMode"
            :options="[
              { value: 'table', label: 'Table' },
              { value: 'cards', label: 'Cards' }
            ]"
            @update:model-value="viewMode = $event as ViewMode"
          />
        </div>
      </div>

      <div class="flex flex-wrap items-center gap-2">
        <button
          v-for="provider in providerOptions"
          :key="provider"
          type="button"
          class="rounded-full border px-3 py-1 font-mono text-xs transition-colors"
          :class="providers.includes(provider)
            ? 'border-oh-accent bg-oh-accent-soft text-oh-accent-text'
            : 'border-oh-default text-oh-secondary hover:border-oh-strong'"
          @click="toggleProvider(provider)"
        >
          {{ provider }}
        </button>
        <button
          v-if="activeFilters"
          type="button"
          class="text-xs text-oh-muted underline-offset-2 hover:underline"
          @click="resetFilters"
        >
          Clear filters
        </button>
        <span v-if="asOf" class="ml-auto font-mono text-[11px] text-oh-muted">as of {{ asOf }}</span>
      </div>
    </section>

    <Skeleton v-if="loading" variant="table-row" :lines="8" />
    <ErrorState v-else-if="error" title="Model catalog unavailable" :message="String((error as any)?.message ?? error)" @retry="load()" />
    <EmptyState
      v-else-if="models.length === 0"
      title="No models match these filters"
      description="Clear the filters or try a different search term."
      action-label="Reset filters"
      @action="resetFilters"
    />

    <template v-else>
      <div v-if="viewMode === 'table'" class="overflow-x-auto border border-oh-default">
        <table class="w-full min-w-[720px] border-collapse text-sm">
          <thead>
            <tr class="border-b border-oh-default bg-oh-subtle text-left font-mono text-[11px] uppercase tracking-wider text-oh-muted">
              <th class="px-4 py-2.5">Model</th>
              <th class="px-4 py-2.5">Provider</th>
              <th class="px-4 py-2.5 text-right">Context</th>
              <th class="px-4 py-2.5 text-right">Input</th>
              <th class="px-4 py-2.5 text-right">Output</th>
              <th class="px-4 py-2.5 text-right">TTFT</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="model in models" :key="model.id" class="border-b border-oh-subtle last:border-0 hover:bg-oh-subtle/60">
              <td class="px-4 py-3">
                <RouterLink :to="`/models/${encodeURIComponent(model.id)}`" class="font-medium text-oh-primary hover:text-oh-accent-text">
                  {{ model.model }}
                </RouterLink>
                <div class="font-mono text-[11px] text-oh-muted">{{ model.id }}</div>
              </td>
              <td class="px-4 py-3 text-oh-secondary">{{ model.provider ?? '—' }}</td>
              <td class="px-4 py-3 text-right font-mono tabular-nums">{{ contextText(model.context_window) }}</td>
              <td class="px-4 py-3 text-right font-mono tabular-nums">{{ priceText(model.input_price) }}</td>
              <td class="px-4 py-3 text-right font-mono tabular-nums">{{ priceText(model.output_price) }}</td>
              <td class="px-4 py-3 text-right font-mono tabular-nums">{{ model.ttft_ms ? `${model.ttft_ms} ms` : '—' }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-else class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <RouterLink
          v-for="model in models"
          :key="model.id"
          :to="`/models/${encodeURIComponent(model.id)}`"
          class="group border border-oh-default p-4 transition-colors hover:border-oh-strong"
        >
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <p class="truncate font-medium text-oh-primary">{{ model.model }}</p>
              <p class="font-mono text-[11px] text-oh-muted">{{ model.provider ?? 'unknown provider' }}</p>
            </div>
            <Badge tone="neutral" variant="subtle">{{ contextText(model.context_window) }}</Badge>
          </div>
          <dl class="mt-4 grid grid-cols-2 gap-2 font-mono text-xs tabular-nums">
            <div><dt class="text-oh-muted">in</dt><dd>{{ priceText(model.input_price) }}</dd></div>
            <div><dt class="text-oh-muted">out</dt><dd>{{ priceText(model.output_price) }}</dd></div>
            <div><dt class="text-oh-muted">ttft</dt><dd>{{ model.ttft_ms ? `${model.ttft_ms} ms` : '—' }}</dd></div>
            <div><dt class="text-oh-muted">stability</dt><dd>{{ model.stability ?? '—' }}</dd></div>
          </dl>
        </RouterLink>
      </div>

      <div class="flex justify-center py-8">
        <Button v-if="nextCursor" variant="secondary" :loading="loadingMore" @click="load(false)">Load more</Button>
      </div>
    </template>
  </PublicShell>
</template>
