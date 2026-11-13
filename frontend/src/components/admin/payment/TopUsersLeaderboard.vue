<template>
  <div class="card border-[var(--border)] bg-[var(--card)] p-4 shadow-none">
    <h3 class="mb-4 border-b border-[var(--border)] pb-3 text-sm font-semibold tracking-tight text-[var(--foreground)]">
      {{ t('payment.admin.topUsers') }}
    </h3>
    <div
      v-if="!hasUsers(props.users)"
      class="flex h-32 items-center justify-center text-sm text-[var(--muted-foreground)]"
    >
      {{ t('payment.admin.noData') }}
    </div>
    <div v-else class="space-y-2">
      <div v-for="[currency, currencyUsers] in sortedUsers(props.users)" :key="currency" class="space-y-2">
        <p class="pill pill-ghost font-mono text-[var(--muted-foreground)]">{{ currency }}</p>
        <div
          v-for="(user, idx) in currencyUsers"
          :key="user.user_id"
          class="flex items-center justify-between gap-3 rounded-md px-2 py-2 hover:bg-[var(--surface-2)]"
        >
          <div class="flex min-w-0 items-center gap-3">
            <span
              :class="[
                'flex h-6 w-6 shrink-0 items-center justify-center rounded-md font-mono text-xs tabular-nums',
                rankClass(idx),
              ]"
            >
              {{ idx + 1 }}
            </span>
            <span class="truncate text-sm text-[var(--foreground)]" :title="user.email">{{ user.email }}</span>
          </div>
          <span class="shrink-0 font-mono text-sm font-medium tabular-nums text-[var(--foreground)]">
            {{ formatMoney(currency, user.amount) }}
          </span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { TopUserPaymentStats } from '@/types/payment'

const { t } = useI18n()

const props = defineProps<{
  users: Record<string, TopUserPaymentStats[]>
}>()

function rankClass(idx: number): string {
  if (idx === 0) return 'bg-[var(--surface-2)] text-[var(--brand-text)] font-semibold'
  return 'text-[var(--muted-foreground)]'
}

function hasUsers(usersByCurrency: Record<string, TopUserPaymentStats[]>): boolean {
  return Object.values(usersByCurrency).some(users => users.length > 0)
}

function sortedUsers(usersByCurrency: Record<string, TopUserPaymentStats[]>): [string, TopUserPaymentStats[]][] {
  return Object.entries(usersByCurrency).sort(([left], [right]) => left.localeCompare(right))
}

function formatMoney(currency: string, amount: number): string {
  return new Intl.NumberFormat(undefined, { style: 'currency', currency }).format(amount)
}
</script>
