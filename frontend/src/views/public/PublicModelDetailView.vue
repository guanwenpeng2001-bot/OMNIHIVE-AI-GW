<script setup lang="ts">
import { extractApiErrorMessage } from '@/utils/apiError'
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, RouterLink } from 'vue-router'
import PublicShell from '@/components/layout/PublicShell.vue'
import Badge from '@/components/ui/Badge.vue'
import CopyButton from '@/components/ui/CopyButton.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import ErrorState from '@/components/ui/ErrorState.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import { publicTokenPrice, publicValue } from '@/features/public/format'
import { getPublicModel, type PublicModelDetail } from '@/features/public/api'

const route = useRoute()
const detail = ref<PublicModelDetail | null>(null)
const model = computed(() => detail.value?.model)
let request = 0
const loading = ref(true)
const error = ref<unknown>(null)

const modelId = computed(() => String(route.params.id ?? ''))

const facts = computed<Array<{ label: string; value: string }>>(() => {
  const fact = detail.value?.model_facts?.find((entry) => entry.state === 'fresh')
  const support = (value?: boolean | null) => value == null ? '—' : value ? 'Yes' : 'No'
  return [
    { label: 'Context window', value: model.value?.context_tokens ? `${Number(model.value.context_tokens).toLocaleString()} tokens` : '—' },
    { label: 'Modalities', value: (model.value?.modalities ?? []).join(', ') || '—' },
    { label: 'Tools', value: support(fact?.tools) },
    { label: 'Reasoning', value: support(fact?.reasoning) },
    { label: 'Training on data', value: '—' },
    { label: 'Stability', value: '—' }
  ]
})

async function load(): Promise<void> {
  const currentRequest = ++request
  loading.value = true
  error.value = null
  try {
    const response = await getPublicModel(modelId.value)
    if (currentRequest === request) detail.value = response
  } catch (err) {
    if (currentRequest === request) error.value = err
  } finally {
    if (currentRequest === request) loading.value = false
  }
}

watch(modelId, () => void load())
onMounted(() => void load())
</script>

<template>
  <PublicShell>
    <div class="mb-6 flex items-center gap-2 text-xs text-oh-muted">
      <RouterLink to="/model-plaza" class="oh-focus-ring shrink-0 rounded-oh-xs hover:text-oh-primary">Models</RouterLink>
      <span>/</span>
      <span class="min-w-0 break-all font-mono">{{ modelId }}</span>
    </div>

    <Skeleton v-if="loading" class="py-4" :lines="10" />
    <ErrorState v-else-if="error" title="Model unavailable" :message="extractApiErrorMessage(error, '')" @retry="load()" />

    <template v-else-if="model">
      <header class="flex flex-wrap items-start justify-between gap-4 border-b border-oh-default pb-6">
        <div class="min-w-0">
          <p class="font-mono text-xs uppercase tracking-[0.18em] text-oh-muted">{{ publicValue(model.provider) }}</p>
          <h1 class="mt-2 break-words text-3xl font-semibold tracking-tight text-oh-primary">{{ model.model }}</h1>
          <div class="mt-1 flex flex-wrap items-center gap-2">
            <span class="min-w-0 break-all font-mono text-xs text-oh-muted">{{ model.id }}</span>
            <CopyButton :value="model.id" variant="secondary" aria-label="Copy model ID" />
          </div>
        </div>
        <div class="flex items-center gap-2">
          <Badge tone="neutral" variant="subtle">{{ publicValue(model.route_state) }}</Badge>
          <RouterLink
            :to="{ path: '/model-plaza/compare', query: { ids: model.id } }"
            class="oh-focus-ring inline-flex h-9 items-center rounded-oh-md border border-oh-default bg-oh-surface px-3.5 text-[13px] font-medium text-oh-primary hover:border-oh-strong hover:bg-oh-hover"
          >Compare</RouterLink>
        </div>
      </header>

      <section class="grid gap-4 py-6 sm:grid-cols-2 lg:grid-cols-3">
        <div class="border border-oh-default p-4">
          <p class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">Input / 1M</p>
          <p class="mt-2 font-mono text-lg tabular-nums text-oh-primary">{{ publicTokenPrice(model.input_rate, 4) }}</p>
        </div>
        <div class="border border-oh-default p-4">
          <p class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">Output / 1M</p>
          <p class="mt-2 font-mono text-lg tabular-nums text-oh-primary">{{ publicTokenPrice(model.output_rate, 4) }}</p>
        </div>
        <div class="border border-oh-default p-4">
          <p class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">Observed TTFT</p>
          <p class="mt-2 font-mono text-lg tabular-nums text-oh-primary">{{ detail?.metrics?.ttft_p50_ms != null ? `${detail.metrics.ttft_p50_ms} ms` : '—' }}</p>
        </div>
      </section>

      <section class="grid gap-6 py-2 lg:grid-cols-2">
        <div>
          <h2 class="text-sm font-semibold text-oh-primary">Capabilities</h2>
          <dl class="mt-3 divide-y divide-oh-subtle border border-oh-default">
            <div v-for="fact in facts" :key="fact.label" class="flex items-baseline justify-between gap-4 px-4 py-2.5">
              <dt class="text-xs text-oh-secondary">{{ fact.label }}</dt>
              <dd class="min-w-0 break-words text-right font-mono text-xs tabular-nums text-oh-primary">{{ fact.value }}</dd>
            </div>
          </dl>
        </div>
        <div>
          <h2 class="text-sm font-semibold text-oh-primary">Endpoints</h2>
          <div v-if="detail?.capabilities?.length" class="mt-3 divide-y divide-oh-subtle border border-oh-default">
            <div v-for="(cap, index) in detail.capabilities" :key="index" class="flex flex-wrap items-center justify-between gap-3 px-4 py-2.5">
              <span class="font-mono text-xs text-oh-primary">{{ publicValue(cap.action) }}</span>
              <span class="text-xs text-oh-secondary">{{ publicValue(cap.protocol) }}</span>
              <Badge tone="neutral" variant="outline">{{ publicValue(cap.state) }}</Badge>
            </div>
          </div>
          <p v-else class="mt-3 border border-oh-default px-4 py-6 text-sm text-oh-secondary">
            No verified endpoint evidence is published for this model yet.
          </p>
        </div>
      </section>
    </template>
    <EmptyState v-else title="No model details published" />
  </PublicShell>
</template>
