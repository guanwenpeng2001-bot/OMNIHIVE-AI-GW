<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import PublicShell from '@/components/layout/PublicShell.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import ErrorState from '@/components/ui/ErrorState.vue'
import { listPublicModels, type PublicModelSummary } from '@/features/public/api'

const models = ref<PublicModelSummary[]>([])
const loading = ref(true)
const error = ref<unknown>(null)

const priced = computed(() =>
  [...models.value].sort((a, b) => Number(a.input_price?.amount ?? 0) - Number(b.input_price?.amount ?? 0))
)

function perMillion(rate?: { amount: string; currency: string; per: string } | null): string {
  if (!rate) return '—'
  const amount = Number(rate.amount)
  const per = Number(rate.per) || 1_000_000
  if (!Number.isFinite(amount)) return '—'
  if (amount === 0) return 'Free'
  return `$${(amount * 1_000_000 / per).toFixed(2)}`
}

async function load(): Promise<void> {
  loading.value = true
  error.value = null
  try {
    const page = await listPublicModels({ limit: 100, sort: 'price_input' })
    models.value = page.items ?? []
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
      <p class="font-mono text-xs uppercase tracking-[0.18em] text-oh-muted">PRICING</p>
      <h1 class="mt-3 text-3xl font-semibold tracking-tight text-oh-primary">Transparent, per-token pricing</h1>
      <p class="mt-2 text-sm text-oh-secondary">
        Reference rates per one million tokens. Your gateway invoice always follows the frozen price of each request.
      </p>
    </header>

    <Skeleton v-if="loading" variant="table-row" :lines="10" />
    <ErrorState v-else-if="error" title="Pricing unavailable" :message="String((error as any)?.message ?? error)" @retry="load" />
    <div v-else class="overflow-x-auto border-x border-b border-oh-default">
      <table class="w-full min-w-[640px] border-collapse text-sm">
        <thead>
          <tr class="border-b border-oh-default bg-oh-subtle text-left font-mono text-[11px] uppercase tracking-wider text-oh-muted">
            <th class="px-4 py-2.5">Model</th>
            <th class="px-4 py-2.5 text-right">Input / 1M</th>
            <th class="px-4 py-2.5 text-right">Output / 1M</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="model in priced" :key="model.id" class="border-b border-oh-subtle last:border-0">
            <td class="px-4 py-3">
              <RouterLink :to="`/models/${encodeURIComponent(model.id)}`" class="text-oh-primary hover:text-oh-accent-text">
                {{ model.model }}
              </RouterLink>
              <span class="ml-2 font-mono text-[11px] text-oh-muted">{{ model.provider ?? '' }}</span>
            </td>
            <td class="px-4 py-3 text-right font-mono tabular-nums">{{ perMillion(model.input_price) }}</td>
            <td class="px-4 py-3 text-right font-mono tabular-nums">{{ perMillion(model.output_price) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </PublicShell>
</template>
