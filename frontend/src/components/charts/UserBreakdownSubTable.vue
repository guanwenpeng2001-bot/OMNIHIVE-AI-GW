<template>
  <div class="overflow-x-auto border-l-2 border-[var(--border)] bg-[var(--surface-2)] tabular-nums">
    <div v-if="loading" class="flex items-center justify-center py-3">
      <LoadingSpinner />
    </div>
    <div v-else-if="items.length === 0" class="space-y-2 p-3 text-xs text-[var(--muted-foreground)]" role="status">
      <div aria-hidden="true" class="space-y-2"><div v-for="row in 4" :key="row" class="h-1 rounded bg-[var(--border)]" :style="{ width: `${60 - row * 8}%` }" /></div>
      <span class="inline-flex rounded-full border border-[var(--border)] px-2 py-0.5">{{ t('admin.dashboard.noDataAvailable') }}</span>
    </div>
    <table v-else class="w-full text-xs">
      <tbody>
        <tr
          v-for="user in items"
          :key="user.user_id"
          class="border-t border-[var(--border)]"
        >
          <td class="max-w-[120px] truncate py-1 pl-6 text-[var(--muted-foreground)]" :title="user.email">
            {{ user.email || `User #${user.user_id}` }}
          </td>
          <td class="py-1 text-right text-[var(--muted-foreground)]">
            {{ user.requests.toLocaleString() }}
          </td>
          <td class="py-1 text-right text-[var(--muted-foreground)]">
            {{ formatTokens(user.total_tokens) }}
          </td>
          <td class="py-1 text-right text-green-600 dark:text-green-400">
            ${{ formatCost(user.actual_cost) }}
          </td>
          <td v-if="showAccountCost" class="py-1 text-right text-orange-500 dark:text-orange-400">
            ${{ formatCost(user.account_cost) }}
          </td>
          <td class="py-1 pr-1 text-right text-[var(--muted-foreground)]">
            ${{ formatCost(user.cost) }}
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { UserBreakdownItem } from '@/types'

const { t } = useI18n()

const props = withDefaults(defineProps<{
  items: UserBreakdownItem[]
  loading?: boolean
  showAccountCost?: boolean
}>(), {
  loading: false,
  showAccountCost: true,
})

const showAccountCost = computed(() => props.showAccountCost)

const formatTokens = (value: number): string => {
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(2)}B`
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(2)}K`
  return value.toLocaleString()
}

const formatCost = (value: number | undefined | null): string => {
  if (value == null) return '0.0000'
  if (value >= 1000) return (value / 1000).toFixed(2) + 'K'
  if (value >= 1) return value.toFixed(2)
  if (value >= 0.01) return value.toFixed(3)
  return value.toFixed(4)
}
</script>
