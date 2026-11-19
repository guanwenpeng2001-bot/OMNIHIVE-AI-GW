<script setup lang="ts">
import { extractApiErrorMessage } from '@/utils/apiError'
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import PublicShell from '@/components/layout/PublicShell.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import ErrorState from '@/components/ui/ErrorState.vue'
import { publicTokenPrice, publicValue } from '@/features/public/format'
import { listPublicModels, type PublicModelSummary } from '@/features/public/api'

const models = ref<PublicModelSummary[]>([])
const loading = ref(true)
const error = ref<unknown>(null)

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

    <Skeleton v-if="loading" class="py-4" :lines="10" />
    <ErrorState v-else-if="error" title="Pricing unavailable" :message="extractApiErrorMessage(error, '')" @retry="load" />
    <EmptyState v-else-if="!models.length" title="No pricing published yet" />
    <div v-else class="oh-focus-ring overflow-x-auto border-x border-b border-oh-default" tabindex="0" role="region" aria-label="Model pricing table">
      <table class="w-full table-fixed border-collapse text-sm">
        <thead>
          <tr class="border-b border-oh-default bg-oh-subtle text-left font-mono text-[11px] uppercase tracking-wider text-oh-muted">
            <th class="w-1/2 px-2 py-2.5 sm:px-4">Model</th>
            <th class="px-2 py-2.5 text-right sm:px-4">Input / 1M</th>
            <th class="px-2 py-2.5 text-right sm:px-4">Output / 1M</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="model in models" :key="model.id" class="border-b border-oh-subtle last:border-0">
            <td class="break-all px-2 py-3 sm:px-4">
              <RouterLink :to="`/model-plaza/${encodeURIComponent(model.id)}`" class="oh-focus-ring rounded-oh-xs text-oh-primary hover:text-oh-accent-text">
                {{ model.model }}
              </RouterLink>
              <span class="block font-mono text-[11px] text-oh-muted">{{ publicValue(model.provider) }}</span>
            </td>
            <td class="break-words px-2 py-3 text-right sm:px-4 font-mono tabular-nums">{{ publicTokenPrice(model.input_rate) }}</td>
            <td class="break-words px-2 py-3 text-right sm:px-4 font-mono tabular-nums">{{ publicTokenPrice(model.output_rate) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </PublicShell>
</template>
