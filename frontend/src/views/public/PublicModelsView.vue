<script setup lang="ts">
import { extractApiErrorMessage } from '@/utils/apiError'
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
import { publicTokenPrice, publicValue } from '@/features/public/format'
import { listPublicModels, type ListParams, type PublicModelSummary } from '@/features/public/api'

type ViewMode = 'table' | 'cards'

const models = ref<PublicModelSummary[]>([])
const nextCursor = ref<string | null>(null)
const asOf = ref<string | null>(null)
const loading = ref(true)
const loadingMore = ref(false)
const error = ref<unknown>(null)
const paginationError = ref<unknown>(null)
let request = 0
const query = ref('')
const sort = ref<NonNullable<ListParams['sort']>>('name')
const viewMode = ref<ViewMode>('table')
const providers = ref<string[]>([])

const providerOptions = ref<string[]>([])
let appliedQuery = ''

const activeFilters = computed(() => providers.value.length > 0 || query.value.trim().length > 0)

async function load(reset = true): Promise<void> {
  if (!reset && (loading.value || loadingMore.value || !nextCursor.value)) return
  const currentRequest = ++request
  paginationError.value = null
  if (reset) {
    appliedQuery = query.value.trim()
    loadingMore.value = false
    loading.value = true
    error.value = null
  } else {
    loadingMore.value = true
  }
  try {
    const page = await listPublicModels({
      q: appliedQuery || undefined,
      provider: providers.value.length ? providers.value : undefined,
      sort: sort.value,
      cursor: reset ? undefined : nextCursor.value,
      limit: 20
    })
    if (currentRequest !== request) return
    if (!appliedQuery && !providers.value.length) {
      const seen = new Set(providerOptions.value)
      for (const model of page.items ?? []) {
        const provider = model.provider?.trim()
        if (provider && provider !== 'unknown') seen.add(provider)
      }
      providerOptions.value = [...seen].sort()
    }
    models.value = reset ? page.items ?? [] : [...models.value, ...(page.items ?? [])]
    nextCursor.value = page.next_cursor ?? null
    asOf.value = page.as_of ?? null
  } catch (err) {
    if (currentRequest === request) {
      if (reset) error.value = err
      else paginationError.value = err
    }
  } finally {
    if (currentRequest === request) {
      loading.value = false
      loadingMore.value = false
    }
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

function contextText(value?: string | null): string {
  if (!value) return '—'
  const tokens = Number(value)
  if (!Number.isFinite(tokens)) return '—'
  if (tokens >= 1_000_000) return `${(tokens / 1_000_000).toFixed(1)}M`
  if (tokens >= 1_000) return `${Math.round(tokens / 1_000)}K`
  return String(tokens)
}

onMounted(() => void load())
</script>

<template>
  <PublicShell>
    <section class="border-b border-oh-default pb-6">
      <p class="font-mono text-xs uppercase tracking-[0.18em] text-oh-muted">MODEL DIRECTORY</p>
      <h1 class="mt-3 text-3xl font-semibold tracking-tight text-oh-primary sm:text-4xl">Models</h1>
      <p class="mt-2 max-w-[65ch] text-sm text-oh-secondary">
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
          class="oh-focus-ring max-w-full break-all rounded-full border px-3 py-1 font-mono text-xs transition-colors"
          :class="providers.includes(provider)
            ? 'border-oh-accent bg-oh-accent-subtle text-oh-accent-text'
            : 'border-oh-default text-oh-secondary hover:border-oh-strong'"
          :aria-pressed="providers.includes(provider)"
          @click="toggleProvider(provider)"
        >
          {{ provider }}
        </button>
        <button
          v-if="activeFilters"
          type="button"
          class="oh-focus-ring rounded-oh-sm text-xs text-oh-muted underline-offset-2 hover:underline"
          @click="resetFilters"
        >
          Clear filters
        </button>
        <span v-if="asOf" class="ml-auto font-mono text-[11px] text-oh-muted">as of {{ asOf }}</span>
      </div>
    </section>

    <Skeleton v-if="loading" class="py-4" :lines="8" />
    <ErrorState v-else-if="error" title="Model catalog unavailable" :message="extractApiErrorMessage(error, '')" @retry="load()" />
    <EmptyState
      v-else-if="models.length === 0"
      :title="activeFilters ? 'No models match these filters' : 'No models published yet'"
      :description="activeFilters ? 'Clear the filters or try a different search term.' : undefined"
      :action-label="activeFilters ? 'Reset filters' : undefined"
      @action="resetFilters"
    />

    <template v-else>
      <div v-if="viewMode === 'table'" class="oh-focus-ring overflow-x-auto border border-oh-default" tabindex="0" role="region" aria-label="Model catalog table">
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
            <tr v-for="model in models" :key="model.id" class="border-b border-oh-subtle last:border-0 hover:bg-oh-hover">
              <td class="max-w-[14rem] break-all px-4 py-3 xl:max-w-[22rem]">
                <RouterLink :to="`/model-plaza/${encodeURIComponent(model.id)}`" class="oh-focus-ring rounded-oh-xs font-medium text-oh-primary hover:text-oh-accent-text">
                  {{ model.model }}
                </RouterLink>
                <div class="font-mono text-[11px] text-oh-muted">{{ model.id }}</div>
              </td>
              <td class="px-4 py-3 text-oh-secondary">{{ publicValue(model.provider) }}</td>
              <td class="px-4 py-3 text-right font-mono tabular-nums">{{ contextText(model.context_tokens) }}</td>
              <td class="px-4 py-3 text-right font-mono tabular-nums">{{ publicTokenPrice(model.input_rate) }}</td>
              <td class="px-4 py-3 text-right font-mono tabular-nums">{{ publicTokenPrice(model.output_rate) }}</td>
              <td class="px-4 py-3 text-right font-mono tabular-nums">—</td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-else class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
        <RouterLink
          v-for="model in models"
          :key="model.id"
          :to="`/model-plaza/${encodeURIComponent(model.id)}`"
          class="oh-focus-ring group min-w-0 border border-oh-default p-4 transition-colors hover:border-oh-strong"
        >
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <p class="truncate font-medium text-oh-primary">{{ model.model }}</p>
              <p class="break-all font-mono text-[11px] text-oh-muted">{{ publicValue(model.provider) }}</p>
            </div>
            <Badge tone="neutral" variant="subtle">{{ contextText(model.context_tokens) }}</Badge>
          </div>
          <dl class="mt-4 grid grid-cols-2 gap-2 font-mono text-xs tabular-nums">
            <div><dt class="text-oh-muted">in</dt><dd>{{ publicTokenPrice(model.input_rate) }}</dd></div>
            <div><dt class="text-oh-muted">out</dt><dd>{{ publicTokenPrice(model.output_rate) }}</dd></div>
            <div><dt class="text-oh-muted">ttft</dt><dd>—</dd></div>
            <div><dt class="text-oh-muted">stability</dt><dd>—</dd></div>
          </dl>
        </RouterLink>
      </div>

      <ErrorState
        v-if="paginationError"
        compact
        class="mt-4"
        title="Could not load more models"
        @retry="load(false)"
      />
      <div class="flex flex-col items-center gap-2 py-8">
        <p role="status" class="text-xs text-oh-muted">{{ models.length }} models loaded</p>
        <Button v-if="nextCursor && !paginationError" variant="secondary" :loading="loadingMore" @click="load(false)">Load more</Button>
      </div>
    </template>
  </PublicShell>
</template>
