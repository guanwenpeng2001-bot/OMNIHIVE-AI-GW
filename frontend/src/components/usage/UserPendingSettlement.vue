<template>
  <section class="card space-y-3 border border-[var(--border)] bg-[var(--card)] p-4 tabular-nums" aria-labelledby="pending-settlement-title">
    <div class="flex items-center justify-between gap-3">
      <h2 id="pending-settlement-title" class="font-semibold">{{ t('usage.pending.title') }} <span v-if="!hasError && !isLoading">({{ total }})</span></h2>
      <button class="btn btn-secondary" :disabled="isLoading" @click="reload">{{ t('usage.pending.refresh') }}</button>
    </div>
    <p class="text-sm text-[var(--muted-foreground)]">{{ t('usage.pending.description') }}</p>
    <div v-for="source in sources" :key="source" class="space-y-2">
      <p v-if="pages[source].error" role="alert" class="text-sm text-red-600">{{ t('usage.pending.loadError') }} ({{ source }})</p>
      <details v-for="item in pages[source].items" :key="`${source}:${item.receipt_id}`" class="rounded border border-[var(--border)] p-3">
        <summary class="cursor-pointer space-y-1 text-sm">
          <div>{{ t('usage.requestedModel') }}: {{ item.requested_model || t('usage.notCollected') }}</div>
          <div>{{ t('usage.upstreamResponseModel') }}: {{ item.upstream_response_model || t('usage.notCollected') }}</div>
          <div class="flex flex-wrap gap-x-4 gap-y-1 text-[var(--muted-foreground)]">
            <span :class="item.pricing_state === 'error' ? 'text-red-600' : 'text-amber-600'">{{ t(item.pricing_state === 'error' ? 'usage.pending.pricingError' : 'usage.pending.title') }}</span>
            <span>{{ t('usage.pending.amount') }}: {{ t('usage.pending.unknown') }}</span>
            <span>{{ t('usage.pending.tokens') }}: {{ item.input_tokens ?? t('usage.notCollected') }} / {{ item.output_tokens ?? t('usage.notCollected') }}</span>
            <span>{{ item.completion_status }}</span>
            <span>{{ source }} #{{ item.receipt_id }}</span>
            <time :datetime="item.created_at">{{ item.created_at }}</time>
          </div>
        </summary>
        <dl class="mt-3 grid grid-cols-1 gap-2 break-all text-sm sm:grid-cols-2">
          <div><dt>{{ t('usage.pending.requestId') }}</dt><dd>{{ item.request_id }}</dd></div>
          <div><dt>API Key ID</dt><dd>{{ item.api_key_id }}</dd></div>
          <div><dt>{{ t('usage.sentUpstreamModel') }}</dt><dd>{{ item.upstream_model || t('usage.notCollected') }}</dd></div>
          <div><dt>{{ t('usage.pending.state') }}</dt><dd>{{ item.state }} / {{ item.pricing_state }} / {{ item.amount_state }} / {{ item.completion_status }}</dd></div>
        </dl>
      </details>
      <button v-if="pages[source].cursor" class="btn btn-secondary" :disabled="pages[source].loading" @click="load(source, false)">{{ t('usage.pending.more') }} ({{ source }})</button>
    </div>
    <p v-if="isLoading" role="status" class="text-sm">{{ t('usage.pending.loading') }}</p>
    <p v-else-if="!hasError && total === 0" class="text-sm text-[var(--muted-foreground)]">{{ t('usage.pending.empty') }}</p>
  </section>
</template>

<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { listPendingSettlement, type PendingSettlement, type PendingSource } from '@/api/pendingSettlement'
import type { UsageQueryParams } from '@/types/user'
const props = defineProps<{ filters: UsageQueryParams }>()
const { t } = useI18n()
const sources: PendingSource[] = ['receipt', 'upstream_job']
const emptyPage = () => ({ items: [] as PendingSettlement[], total: 0, cursor: 0, loading: false, error: false, generation: 0 })
const pages = reactive({ receipt: emptyPage(), upstream_job: emptyPage() })
const total = computed(() => pages.receipt.total + pages.upstream_job.total)
const isLoading = computed(() => sources.some(source => pages[source].loading))
const hasError = computed(() => sources.some(source => pages[source].error))
async function load(source: PendingSource, reset: boolean) {
  const page = pages[source]
  const generation = ++page.generation
  if (reset) { page.items = []; page.total = 0; page.cursor = 0 }
  page.loading = true
  page.error = false
  try {
    const result = await listPendingSettlement(props.filters, source, reset ? 0 : page.cursor)
    if (generation !== page.generation) return
    page.items = reset ? result.items : [...page.items, ...result.items]
    page.total = result.total
    page.cursor = result.next_cursor
  } catch {
    if (generation === page.generation) page.error = true
  } finally {
    if (generation === page.generation) page.loading = false
  }
}
function reload() { for (const source of sources) void load(source, true) }
watch(() => [props.filters.start_date, props.filters.end_date, props.filters.timezone, props.filters.model, props.filters.api_key_id], reload, { immediate: true })
</script>
