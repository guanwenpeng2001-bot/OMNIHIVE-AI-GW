<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, RouterLink } from 'vue-router'
import PublicShell from '@/components/layout/PublicShell.vue'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'
import ErrorState from '@/components/ui/ErrorState.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import { getPublicModel, type PublicModelDetail } from '@/features/public/api'

const route = useRoute()
const model = ref<PublicModelDetail | null>(null)
const loading = ref(true)
const error = ref<unknown>(null)

const modelId = computed(() => String(route.params.id ?? ''))

const facts = computed<Array<{ label: string; value: string }>>(() => {
  const facts = model.value?.facts ?? {}
  const pick = (key: string): string => {
    const value = (facts as Record<string, unknown>)[key]
    if (value === null || value === undefined) return '—'
    if (typeof value === 'boolean') return value ? 'Yes' : 'No'
    if (typeof value === 'object') return JSON.stringify(value)
    return String(value)
  }
  return [
    { label: 'Context window', value: model.value?.context_window ? `${model.value.context_window.toLocaleString()} tokens` : '—' },
    { label: 'Modalities', value: (model.value?.modalities ?? []).join(', ') || '—' },
    { label: 'Tools', value: pick('tools') },
    { label: 'Reasoning', value: pick('reasoning') },
    { label: 'Training on data', value: pick('training') },
    { label: 'Stability', value: model.value?.stability ?? '—' }
  ]
})

function priceText(rate?: { amount: string; currency: string; per: string } | null): string {
  if (!rate) return '—'
  const amount = Number(rate.amount)
  const per = Number(rate.per) || 1_000_000
  if (!Number.isFinite(amount)) return '—'
  if (amount === 0) return rate.currency === 'USD' ? 'Free' : '0'
  return `${rate.currency} ${(amount * 1_000_000 / per).toFixed(4)} / 1M`
}

async function load(): Promise<void> {
  loading.value = true
  error.value = null
  try {
    model.value = await getPublicModel(modelId.value)
  } catch (err) {
    error.value = err
  } finally {
    loading.value = false
  }
}

watch(modelId, () => void load())
onMounted(() => void load())
</script>

<template>
  <PublicShell>
    <div class="mb-6 flex items-center gap-2 text-xs text-oh-muted">
      <RouterLink to="/models" class="hover:text-oh-primary">Models</RouterLink>
      <span>/</span>
      <span class="font-mono">{{ modelId }}</span>
    </div>

    <Skeleton v-if="loading" variant="table-row" :lines="10" />
    <ErrorState v-else-if="error" title="Model unavailable" :message="String((error as any)?.message ?? error)" @retry="load()" />

    <template v-else-if="model">
      <header class="flex flex-wrap items-start justify-between gap-4 border-b border-oh-default pb-6">
        <div class="min-w-0">
          <p class="font-mono text-xs uppercase tracking-[0.18em] text-oh-muted">{{ model.provider ?? 'provider unknown' }}</p>
          <h1 class="mt-2 break-words text-3xl font-semibold tracking-tight text-oh-primary">{{ model.model }}</h1>
          <p class="mt-1 font-mono text-xs text-oh-muted">{{ model.id }}</p>
        </div>
        <div class="flex items-center gap-2">
          <Badge tone="neutral" variant="subtle">{{ model.route_state ?? 'route unknown' }}</Badge>
          <RouterLink to="/models/compare"><Button variant="secondary">Compare</Button></RouterLink>
        </div>
      </header>

      <section class="grid gap-4 py-6 sm:grid-cols-2 lg:grid-cols-3">
        <div class="border border-oh-default p-4">
          <p class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">Input</p>
          <p class="mt-2 font-mono text-lg tabular-nums text-oh-primary">{{ priceText(model.input_price) }}</p>
        </div>
        <div class="border border-oh-default p-4">
          <p class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">Output</p>
          <p class="mt-2 font-mono text-lg tabular-nums text-oh-primary">{{ priceText(model.output_price) }}</p>
        </div>
        <div class="border border-oh-default p-4">
          <p class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">Observed TTFT</p>
          <p class="mt-2 font-mono text-lg tabular-nums text-oh-primary">{{ model.ttft_ms ? `${model.ttft_ms} ms` : '—' }}</p>
        </div>
      </section>

      <section class="grid gap-6 py-2 lg:grid-cols-2">
        <div>
          <h2 class="text-sm font-semibold text-oh-primary">Capabilities</h2>
          <dl class="mt-3 divide-y divide-oh-subtle border border-oh-default">
            <div v-for="fact in facts" :key="fact.label" class="flex items-baseline justify-between gap-4 px-4 py-2.5">
              <dt class="text-xs text-oh-secondary">{{ fact.label }}</dt>
              <dd class="text-right font-mono text-xs tabular-nums text-oh-primary">{{ fact.value }}</dd>
            </div>
          </dl>
        </div>
        <div>
          <h2 class="text-sm font-semibold text-oh-primary">Endpoints</h2>
          <div v-if="model.capabilities?.length" class="mt-3 divide-y divide-oh-subtle border border-oh-default">
            <div v-for="(cap, index) in model.capabilities" :key="index" class="flex items-center justify-between gap-3 px-4 py-2.5">
              <span class="font-mono text-xs text-oh-primary">{{ cap.action ?? 'unknown action' }}</span>
              <span class="text-xs text-oh-secondary">{{ cap.protocol ?? '—' }}</span>
              <Badge tone="neutral" variant="outline">{{ cap.state ?? 'unknown' }}</Badge>
            </div>
          </div>
          <p v-else class="mt-3 border border-oh-default px-4 py-6 text-sm text-oh-secondary">
            No verified endpoint evidence is published for this model yet.
          </p>
        </div>
      </section>
    </template>
  </PublicShell>
</template>
