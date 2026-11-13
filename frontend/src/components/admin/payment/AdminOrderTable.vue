<template>
  <div class="admin-order-table space-y-4">
    <div class="card border-[var(--border)] bg-[var(--card)] p-3 shadow-none">
      <div class="flex flex-wrap items-center gap-2">
        <div class="w-full sm:w-64">
          <input
            v-model="searchQuery"
            type="text"
            :placeholder="t('payment.admin.searchOrders')"
            class="input"
            @input="handleSearch"
          />
        </div>
        <Select
          v-model="filters.status"
          :options="statusFilterOptions"
          class="w-36"
          @change="emitFiltersChanged"
        />
        <Select
          v-model="filters.payment_type"
          :options="paymentTypeFilterOptions"
          class="w-40"
          @change="emitFiltersChanged"
        />
        <Select
          v-model="filters.order_type"
          :options="orderTypeFilterOptions"
          class="w-36"
          @change="emitFiltersChanged"
        />
        <div class="flex flex-1 flex-wrap items-center justify-end gap-2">
          <button
            @click="emit('refresh')"
            :disabled="loading"
            class="btn btn-secondary"
            :title="t('common.refresh')"
          >
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>
      </div>
    </div>

    <DataTable :columns="columns" :data="orders" :loading="loading">
      <template #cell-id="{ value }">
        <span class="font-mono text-sm">#{{ value }}</span>
      </template>

      <template #cell-user_id="{ value }">
        <span class="text-sm text-[var(--muted-foreground)]">#{{ value }}</span>
      </template>

      <template #cell-pay_amount="{ value, row }">
        <div class="font-mono text-sm tabular-nums">
          <span class="font-medium text-[var(--foreground)]">{{ paymentAmountSymbol(row) }}{{ value.toFixed(2) }}</span>
          <span v-if="row.fee_rate > 0" class="ml-1 text-xs text-[var(--muted-foreground)]" :title="t('payment.orders.fee') + ': ' + row.fee_rate + '%'">
            ({{ row.fee_rate }}%)
          </span>
          <div v-if="row.amount !== row.pay_amount" class="text-xs text-[var(--muted-foreground)]">
            {{ t('payment.orders.creditedAmount') }}: {{ creditedAmountSymbol }}{{ row.amount.toFixed(2) }}
          </div>
        </div>
      </template>

      <template #cell-payment_type="{ value }">
        <span class="pill pill-ghost text-[var(--foreground)]">
          {{ t('payment.methods.' + value, value) }}
        </span>
      </template>

      <template #cell-status="{ value }">
        <span :class="['badge', statusBadgeClass(value)]">
          <span class="pulse-dot bg-current" aria-hidden="true" />
          {{ t('payment.status.' + value.toLowerCase(), value) }}
        </span>
      </template>

      <template #cell-order_type="{ value }">
        <span class="text-sm text-[var(--foreground)]">
          {{ t('payment.admin.' + value + 'Order', value) }}
        </span>
      </template>

      <template #cell-created_at="{ value }">
        <span class="text-xs text-[var(--muted-foreground)]">{{ formatDateTime(value) }}</span>
      </template>

      <template #cell-actions="{ row }">
        <div class="order-actions flex flex-wrap items-center gap-1">
          <button
            @click="emit('detail', row)"
            class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-[var(--muted-foreground)] transition-colors hover:bg-[var(--surface-2)] hover:text-[var(--foreground)]"
          >
            <Icon name="eye" size="sm" />
            <span class="text-xs">{{ t('common.view') }}</span>
          </button>
          <button
            v-if="row.status === 'PENDING'"
            @click="emit('cancel', row)"
            class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-[var(--muted-foreground)] transition-colors hover:bg-[color-mix(in_srgb,var(--warn)_8%,var(--card))] hover:text-[var(--warn-text)]"
          >
            <Icon name="x" size="sm" />
            <span class="text-xs">{{ t('payment.orders.cancel') }}</span>
          </button>
          <button
            v-if="row.status === 'FAILED'"
            @click="emit('retry', row)"
            class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-[var(--muted-foreground)] transition-colors hover:bg-[color-mix(in_srgb,var(--info)_8%,var(--card))] hover:text-[var(--info-text)]"
          >
            <Icon name="refresh" size="sm" />
            <span class="text-xs">{{ t('payment.admin.retry') }}</span>
          </button>
          <button
            v-if="canRefundRow(row)"
            @click="emit('refund', row)"
            class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-[var(--muted-foreground)] transition-colors hover:bg-[color-mix(in_srgb,var(--bad)_8%,var(--card))] hover:text-[var(--bad-text)]"
          >
            <Icon name="dollar" size="sm" />
            <span class="text-xs">{{ t('payment.admin.refund') }}</span>
          </button>
        </div>
      </template>
    </DataTable>

    <Pagination
      v-if="total > 0"
      :page="page"
      :total="total"
      :page-size="pageSize"
      @update:page="emit('update:page', $event)"
      @update:pageSize="emit('update:pageSize', $event)"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PaymentOrder } from '@/types/payment'
import type { Column } from '@/components/common/types'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { statusBadgeClass, canRefund, formatOrderDateTime } from '@/components/payment/orderUtils'
import { currencySymbol } from '@/components/payment/currency'

const { t } = useI18n()

defineProps<{
  orders: PaymentOrder[]
  loading: boolean
  page: number
  pageSize: number
  total: number
}>()

const emit = defineEmits<{
  (e: 'detail', order: PaymentOrder): void
  (e: 'cancel', order: PaymentOrder): void
  (e: 'retry', order: PaymentOrder): void
  (e: 'refund', order: PaymentOrder): void
  (e: 'refresh'): void
  (e: 'update:page', page: number): void
  (e: 'update:pageSize', size: number): void
  (e: 'filter', filters: { keyword?: string; status?: string; payment_type?: string; order_type?: string }): void
}>()

const searchQuery = ref('')
const filters = reactive({ status: '', payment_type: '', order_type: '' })
const creditedAmountSymbol = currencySymbol('USD')

function paymentAmountSymbol(order: PaymentOrder): string {
  return currencySymbol(order.currency)
}

let debounceTimer: ReturnType<typeof setTimeout> | null = null
function handleSearch() {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => emitFiltersChanged(), 300)
}

function emitFiltersChanged() {
  emit('filter', {
    keyword: searchQuery.value || undefined,
    status: filters.status || undefined,
    payment_type: filters.payment_type || undefined,
    order_type: filters.order_type || undefined,
  })
}

const columns = computed<Column[]>(() => [
  { key: 'id', label: t('payment.orders.orderId') },
  { key: 'user_id', label: t('payment.orders.userId') },
  { key: 'pay_amount', label: t('payment.orders.payAmount') },
  { key: 'payment_type', label: t('payment.orders.paymentMethod') },
  { key: 'status', label: t('payment.orders.status') },
  { key: 'order_type', label: t('payment.orders.orderType') },
  { key: 'created_at', label: t('payment.orders.createdAt') },
  { key: 'actions', label: t('payment.orders.actions') },
])

const statusFilterOptions = computed(() => [
  { value: '', label: t('payment.admin.allStatuses') },
  { value: 'PENDING', label: t('payment.status.pending') },
  { value: 'PAID', label: t('payment.status.paid') },
  { value: 'COMPLETED', label: t('payment.status.completed') },
  { value: 'EXPIRED', label: t('payment.status.expired') },
  { value: 'CANCELLED', label: t('payment.status.cancelled') },
  { value: 'FAILED', label: t('payment.status.failed') },
  { value: 'REFUNDED', label: t('payment.status.refunded') },
  { value: 'REFUND_REQUESTED', label: t('payment.status.refund_requested') },
  { value: 'REFUND_PENDING', label: t('payment.status.refund_pending') },
  { value: 'REFUND_FAILED', label: t('payment.status.refund_failed') },
])

const paymentTypeFilterOptions = computed(() => [
  { value: '', label: t('payment.admin.allPaymentTypes') },
  { value: 'alipay', label: t('payment.methods.alipay') },
  { value: 'wxpay', label: t('payment.methods.wxpay') },
  { value: 'stripe', label: t('payment.methods.stripe') },
  { value: 'airwallex', label: t('payment.methods.airwallex') },
])

const orderTypeFilterOptions = computed(() => [
  { value: '', label: t('payment.admin.allOrderTypes') },
  { value: 'balance', label: t('payment.admin.balanceOrder') },
  { value: 'subscription', label: t('payment.admin.subscriptionOrder') },
])

function canRefundRow(order: PaymentOrder): boolean {
  return canRefund(order.status)
}

function formatDateTime(dateStr: string): string {
  return formatOrderDateTime(dateStr)
}
</script>

<style scoped>
.admin-order-table :deep(td) {
  padding-top: 0.625rem;
  padding-bottom: 0.625rem;
}

.order-actions {
  max-width: 17rem;
}

.order-actions > button {
  flex-direction: row;
  gap: 0.375rem;
  padding: 0.375rem 0.5rem;
  border: 1px solid var(--border);
  border-radius: 0.375rem;
  background: var(--card);
  white-space: nowrap;
}

.order-actions > button:hover {
  background: var(--surface-2);
}

.order-actions > button:focus-visible {
  outline: 2px solid var(--ring);
  outline-offset: 2px;
}
</style>
