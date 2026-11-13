<template>
  <div class="card p-4">
    <h3 class="mb-4 border-b border-[var(--border)] pb-3 text-sm font-semibold text-[var(--foreground)]">
      {{ t('payment.admin.paymentDistribution') }}
    </h3>
    <div
      v-if="!methods?.length"
      class="relative flex h-40 flex-col items-center justify-center gap-4 text-sm text-[var(--muted-foreground)]"
      role="status"
    >
      <div class="w-full space-y-4" aria-hidden="true">
        <div v-for="index in 3" :key="index" class="flex items-center gap-3">
          <span class="h-2 w-2 rounded-full border border-[var(--border)]"></span>
          <span class="h-0.5 flex-1 border-t border-dashed border-[var(--border)]"></span>
          <span class="font-mono text-[10px]">0</span>
        </div>
      </div>
      <span class="text-xs">{{ t('payment.admin.noData') }}</span>
    </div>
    <div v-else class="divide-y divide-[var(--border)]">
      <div v-for="method in methods" :key="method.type" class="space-y-2 py-3 first:pt-0 last:pb-0">
        <div class="flex items-center justify-between">
          <div class="flex items-center gap-2">
            <span :class="['inline-block h-2 w-2 rounded-full', colorMap[method.type] || 'bg-[var(--muted-foreground)]']"></span>
            <span class="text-sm text-[var(--foreground)]">
              {{ t('payment.methods.' + method.type, method.type) }}
            </span>
          </div>
          <div class="space-y-1 text-right">
            <span v-for="[currency, amount] in sortedAmounts(method.amount)" :key="currency" class="block font-mono text-sm font-medium tabular-nums text-[var(--foreground)]">
              {{ formatMoney(currency, amount) }}
            </span>
            <span class="ml-2 text-xs text-gray-500 dark:text-gray-400">
              ({{ method.count }})
            </span>
          </div>
        </div>
        <div v-for="[currency, amount] in sortedAmounts(method.amount)" :key="currency" class="flex items-center gap-2">
          <span class="w-10 text-xs text-gray-500 dark:text-gray-400">{{ currency }}</span>
          <div class="h-1.5 flex-1 overflow-hidden rounded-full bg-[var(--surface-2)]">
            <div
              :class="['h-full rounded-full transition-[width] motion-reduce:transition-none', colorMap[method.type] || 'bg-[var(--muted-foreground)]']"
              :style="{ width: barWidth(currency, amount) + '%' }"
            ></div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CurrencyAmounts, PaymentMethodStats } from '@/types/payment'

const { t } = useI18n()

const props = defineProps<{
  methods: PaymentMethodStats[]
}>()

const colorMap: Record<string, string> = {
  alipay: 'bg-[var(--info)]',
  wxpay: 'bg-[var(--ok)]',
  alipay_direct: 'bg-[var(--info)]',
  wxpay_direct: 'bg-[var(--ok)]',
  stripe: 'bg-[var(--brand)]',
}

const maxAmounts = computed<CurrencyAmounts>(() => {
  return props.methods.reduce<CurrencyAmounts>((maximums, method) => {
    for (const [currency, amount] of Object.entries(method.amount)) {
      maximums[currency] = Math.max(maximums[currency] || 0, amount)
    }
    return maximums
  }, {})
})

function sortedAmounts(amounts: CurrencyAmounts): [string, number][] {
  return Object.entries(amounts).sort(([left], [right]) => left.localeCompare(right))
}

function barWidth(currency: string, amount: number): number {
  return Math.min((amount / (maxAmounts.value[currency] || 1)) * 100, 100)
}

function formatMoney(currency: string, amount: number): string {
  return new Intl.NumberFormat(undefined, { style: 'currency', currency }).format(amount)
}
</script>
