<template>
  <span
    class="pill pill-ghost px-2.5 py-1 text-xs font-medium"
    :class="statusClass"
  >
    {{ statusLabel }}
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { OrderStatus } from '@/types/payment'

const props = defineProps<{
  status: OrderStatus
}>()

const { t } = useI18n()

const statusMap: Record<OrderStatus, { key: string; class: string }> = {
  PENDING: { key: 'payment.status.pending', class: 'text-[var(--warn-text)]' },
  PAID: { key: 'payment.status.paid', class: 'text-[var(--info-text)]' },
  RECHARGING: { key: 'payment.status.recharging', class: 'text-[var(--info-text)]' },
  COMPLETED: { key: 'payment.status.completed', class: 'text-[var(--ok-text)]' },
  EXPIRED: { key: 'payment.status.expired', class: 'text-[var(--muted-foreground)]' },
  CANCELLED: { key: 'payment.status.cancelled', class: 'text-[var(--muted-foreground)]' },
  FAILED: { key: 'payment.status.failed', class: 'text-[var(--bad-text)]' },
  REFUND_REQUESTED: { key: 'payment.status.refund_requested', class: 'text-[var(--warn-text)]' },
  REFUNDING: { key: 'payment.status.refunding', class: 'text-[var(--warn-text)]' },
  REFUND_PENDING: { key: 'payment.status.refund_pending', class: 'text-[var(--warn-text)]' },
  REFUNDED: { key: 'payment.status.refunded', class: 'text-[var(--info-text)]' },
  PARTIALLY_REFUNDED: { key: 'payment.status.partially_refunded', class: 'text-[var(--info-text)]' },
  REFUND_FAILED: { key: 'payment.status.refund_failed', class: 'text-[var(--bad-text)]' },
}

const statusLabel = computed(() => {
  const entry = statusMap[props.status]
  return entry ? t(entry.key) : props.status
})

const statusClass = computed(() => {
  const entry = statusMap[props.status]
  return entry?.class ?? 'bg-gray-100 text-gray-800 dark:bg-gray-900/30 dark:text-gray-400'
})
</script>
