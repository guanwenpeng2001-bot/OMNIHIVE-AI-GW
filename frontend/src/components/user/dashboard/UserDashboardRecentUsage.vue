<template>
  <div class="card oh-recent-card overflow-hidden">
    <div class="oh-recent-header flex items-center justify-between border-b px-5 py-3.5">
      <div class="flex items-center gap-2.5">
        <h2 class="text-base font-semibold">{{ t('dashboard.recentUsage') }}</h2>
        <span class="rounded border border-[var(--border)] px-1.5 font-mono text-xs tabular-nums text-[var(--muted-foreground)]">{{ loading ? '—' : data.length }}</span>
      </div>
    </div>
    <div class="px-5">
      <div v-if="loading" class="flex items-center justify-center py-12">
        <LoadingSpinner size="lg" />
      </div>
      <div v-else-if="data.length === 0" class="py-6" role="status">
        <div class="request-shape" aria-hidden="true">
          <div v-for="row in 4" :key="row" class="request-shape-row"><i /><span :style="{ width: `${58 - row * 7}%` }" /><b /><em /></div>
        </div>
        <div class="mt-4 flex flex-wrap items-center justify-between gap-3">
          <div><span class="empty-status">{{ t('dashboard.noUsageRecords') }}</span><p class="mt-2 text-xs text-[var(--muted-foreground)]">{{ t('dashboard.startUsingApi') }}</p></div>
          <router-link to="/keys" class="btn btn-secondary btn-sm">{{ t('dashboard.createApiKey') }}</router-link>
        </div>
      </div>
      <div v-else>
        <div
          v-for="log in data"
          :key="log.id"
          class="oh-usage-row flex flex-wrap items-center justify-between gap-3 py-3.5"
        >
          <div class="flex min-w-0 flex-1 items-center gap-3">
            <span class="h-2 w-2 shrink-0 rounded-full" :style="{ backgroundColor: modelColor(log.model) }" aria-hidden="true" />
            <div class="min-w-0">
              <div class="flex items-center gap-2">
                <p class="truncate text-sm font-bold" :title="log.model">{{ log.model }}</p>
              </div>
              <p class="mt-1 font-mono text-xs tabular-nums opacity-60">{{ formatDateTimeWithZone(log.created_at, timeZone) }}</p>
            </div>
          </div>
          <div class="ml-auto text-right font-mono tabular-nums">
            <p class="text-sm font-semibold">
              <span :title="t('dashboard.actual')">{{ t('dashboard.actual') }} USD {{ formatCost(log.actual_cost) }}</span>
            </p>
            <p class="mt-1 text-xs opacity-60">{{ (log.input_tokens + log.output_tokens).toLocaleString() }} tok</p>
            <p class="mt-1 text-xs opacity-50">{{ t('dashboard.standard') }} USD {{ formatCost(log.total_cost) }}</p>
          </div>
        </div>

        <router-link to="/usage" class="oh-usage-link flex items-center justify-between border-t py-3.5 text-xs font-semibold">
          {{ t('dashboard.viewAllUsage') }}
          <span class="text-xl opacity-40" aria-hidden="true">›</span>
        </router-link>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { formatDateTimeWithZone } from '@/utils/format'
import { formatLedgerAmount as formatCost } from '@/utils/money'
import type { UsageLog } from '@/types'

defineProps<{
  timeZone?: string
  data: UsageLog[]
  loading: boolean
}>()
const { t } = useI18n()
const modelColor = (model: string) => {
  const name = model.toLowerCase()
  if (name.includes('claude')) return '#c08457'
  if (name.includes('gpt') || /^o[134](?:-|$)/.test(name)) return '#10a37f'
  if (name.includes('gemini')) return '#4285f4'
  if (name.includes('deepseek')) return '#6366f1'
  return 'var(--muted-foreground)'
}
</script>

<style scoped>
.oh-recent-card { color: var(--foreground); background: var(--card); border: 1px solid var(--border); }
.oh-recent-header, .oh-usage-link, .oh-usage-row + .oh-usage-row { border-color: var(--border); }
.oh-usage-row + .oh-usage-row { border-top-width: 1px; }
.oh-usage-link:hover { text-decoration: underline; text-underline-offset: 4px; }
.oh-usage-link:focus-visible { outline: 2px solid var(--brand); outline-offset: 2px; }
.request-shape-row { height: 32px; display: flex; align-items: center; gap: 12px; border-bottom: 1px dashed var(--border); }
.request-shape-row i { width: 6px; height: 6px; border: 1px solid var(--border); border-radius: 50%; }
.request-shape-row span, .request-shape-row em { height: 4px; background: var(--border); border-radius: 2px; }
.request-shape-row em { width: 30px; }
.request-shape-row b { width: 42px; height: 14px; margin-left: auto; border: 1px solid var(--border); border-radius: 999px; }
.empty-status { display: inline-flex; border: 1px solid var(--border); border-radius: 999px; padding: 3px 9px; color: var(--muted-foreground); font-size: 12px; }
</style>
