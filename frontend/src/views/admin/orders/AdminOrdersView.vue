<template>
  <AppLayout>
    <div class="space-y-4">
      <!-- High-Density Workbench Filter & Status Bar -->
      <div class="card space-y-3 p-4">
        <div
          class="flex flex-wrap items-center justify-between gap-2 border-b pb-3"
          style="border-color: var(--border)"
        >
          <div class="flex flex-wrap items-center gap-2">
            <span class="chip font-medium">
              <span class="pulse-dot" :class="pendingRefundCount > 0 ? 'pulse-warn' : 'pulse-ok'"></span>
              <span>{{ t('payment.orders.status') }}</span>
            </span>
            <span class="pill pill-ghost font-mono text-xs tabular-nums">
              Total {{ orderPagination.total }}
            </span>
            <span
              v-if="pendingRefundCount > 0"
              class="pill font-mono text-xs tabular-nums"
              style="color: var(--warn-text); background: color-mix(in srgb, var(--warn) 12%, transparent)"
            >
              {{ pendingRefundCount }} {{ t('payment.status.refund_requested') }}
            </span>
          </div>
          <div class="flex items-center gap-2">
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="ordersLoading"
              :title="t('common.refresh')"
              @click="loadOrders"
            >
              <Icon name="refresh" size="sm" :class="ordersLoading ? 'animate-spin' : ''" />
            </button>
          </div>
        </div>

        <div class="flex flex-wrap items-center gap-2.5">
          <div class="relative flex-1 sm:max-w-72">
            <Icon
              name="search"
              size="sm"
              class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-[var(--muted-foreground)]"
            />
            <input
              v-model="orderSearch"
              type="text"
              :placeholder="t('payment.admin.searchOrders')"
              class="input pl-9 font-mono tabular-nums text-xs"
              @input="debounceLoadOrders"
            />
          </div>
          <Select v-model="orderFilters.status" :options="statusFilterOptions" class="w-36" @change="loadOrders" />
          <Select v-model="orderFilters.payment_type" :options="paymentTypeFilterOptions" class="w-40" @change="loadOrders" />
          <Select v-model="orderFilters.order_type" :options="orderTypeFilterOptions" class="w-36" @change="loadOrders" />
        </div>
      </div>

      <!-- Table -->
      <OrderTable :orders="orders" :loading="ordersLoading" show-user>
        <template #actions="{ row }">
          <div class="flex flex-wrap items-center gap-1">
            <button
              type="button"
              class="btn btn-ghost btn-sm px-2 py-1 text-xs font-medium"
              @click="showOrderDetail(row)"
            >
              <Icon name="eye" size="sm" />
              {{ t('common.view') }}
            </button>
            <button
              v-if="row.status === 'PENDING'"
              type="button"
              class="inline-flex items-center gap-1 rounded-lg border px-2 py-1 text-xs font-medium text-amber-700 transition-colors hover:bg-amber-50 dark:border-amber-800/50 dark:text-amber-400 dark:hover:bg-amber-900/20"
              @click="handleCancelOrder(row)"
            >
              <Icon name="x" size="sm" />
              {{ t('payment.orders.cancel') }}
            </button>
            <button
              v-if="row.status === 'FAILED'"
              type="button"
              class="inline-flex items-center gap-1 rounded-lg border px-2 py-1 text-xs font-medium text-blue-700 transition-colors hover:bg-blue-50 dark:border-blue-800/50 dark:text-blue-400 dark:hover:bg-blue-900/20"
              @click="handleRetryOrder(row)"
            >
              <Icon name="refresh" size="sm" />
              {{ t('payment.admin.retry') }}
            </button>
            <template v-if="row.status === 'REFUND_REQUESTED'">
              <span
                v-if="row.refund_amount"
                class="pill font-mono text-xs font-medium tabular-nums"
                style="color: var(--info-text); background: color-mix(in srgb, var(--info) 12%, transparent)"
              >
                {{ creditedAmountSymbol }}{{ row.refund_amount.toFixed(2) }}
              </span>
              <button
                type="button"
                class="inline-flex items-center gap-1 rounded-lg border px-2 py-1 text-xs font-medium text-purple-700 transition-colors hover:bg-purple-50 dark:border-purple-800/50 dark:text-purple-400 dark:hover:bg-purple-900/20"
                @click="openRefundDialog(row)"
              >
                <Icon name="check" size="sm" />
                {{ t('payment.admin.approveRefund') }}
              </button>
            </template>
            <button
              v-else-if="row.status === 'REFUND_FAILED'"
              type="button"
              class="inline-flex items-center gap-1 rounded-lg border px-2 py-1 text-xs font-medium text-purple-700 transition-colors hover:bg-purple-50 dark:border-purple-800/50 dark:text-purple-400 dark:hover:bg-purple-900/20"
              @click="openRefundDialog(row)"
            >
              <Icon name="refresh" size="sm" />
              {{ t('payment.admin.retryRefund') }}
            </button>
            <button
              v-else-if="row.status === 'REFUND_PENDING'"
              type="button"
              :disabled="refundQueryingIds.has(row.id)"
              class="inline-flex items-center gap-1 rounded-lg border px-2 py-1 text-xs font-medium text-orange-700 transition-colors hover:bg-orange-50 disabled:opacity-60 dark:border-orange-800/50 dark:text-orange-400 dark:hover:bg-orange-900/20"
              @click="handleQueryRefund(row)"
            >
              <Icon name="refresh" size="sm" :class="refundQueryingIds.has(row.id) ? 'animate-spin' : ''" />
              {{ t('payment.admin.queryRefundStatus') }}
            </button>
            <button
              v-else-if="row.status === 'COMPLETED' || row.status === 'PARTIALLY_REFUNDED'"
              type="button"
              class="inline-flex items-center gap-1 rounded-lg border px-2 py-1 text-xs font-medium text-red-600 transition-colors hover:bg-red-50 dark:border-red-800/50 dark:text-red-400 dark:hover:bg-red-900/20"
              @click="openRefundDialog(row)"
            >
              <Icon name="dollar" size="sm" />
              {{ t('payment.admin.refund') }}
            </button>
          </div>
        </template>
      </OrderTable>
      <Pagination v-if="orderPagination.total > 0" :page="orderPagination.page" :total="orderPagination.total" :page-size="orderPagination.page_size" @update:page="handleOrderPageChange" @update:pageSize="handleOrderPageSizeChange" />
    </div>

    <!-- Order Detail Dialog -->
    <BaseDialog :show="showDetailDialog" :title="t('payment.admin.orderDetail')" width="wide" @close="showDetailDialog = false">
      <div v-if="selectedOrder" class="space-y-4">
        <div class="grid grid-cols-2 gap-3">
          <div class="rounded-xl border p-3" style="border-color: var(--border); background: var(--surface-2)">
            <p class="text-xs text-[var(--muted-foreground)]">{{ t('payment.orders.orderId') }}</p>
            <p class="mt-0.5 font-mono text-sm font-semibold tabular-nums text-[var(--foreground)]">#{{ selectedOrder.id }}</p>
          </div>
          <div class="rounded-xl border p-3" style="border-color: var(--border); background: var(--surface-2)">
            <p class="text-xs text-[var(--muted-foreground)]">{{ t('payment.orders.orderNo') }}</p>
            <p class="mt-0.5 font-mono text-sm font-medium tabular-nums text-[var(--foreground)]">{{ selectedOrder.out_trade_no }}</p>
          </div>
          <div class="rounded-xl border p-3" style="border-color: var(--border); background: var(--surface-2)">
            <p class="text-xs text-[var(--muted-foreground)]">{{ t('payment.orders.status') }}</p>
            <div class="mt-1"><OrderStatusBadge :status="selectedOrder.status" /></div>
          </div>
          <div class="rounded-xl border p-3" style="border-color: var(--border); background: var(--surface-2)">
            <p class="text-xs text-[var(--muted-foreground)]">{{ t('payment.orders.amount') }}</p>
            <p class="mt-0.5 font-mono text-sm font-semibold tabular-nums text-[var(--foreground)]">{{ creditedAmountSymbol }}{{ selectedOrder.amount.toFixed(2) }}</p>
          </div>
          <div class="rounded-xl border p-3" style="border-color: var(--border); background: var(--surface-2)">
            <p class="text-xs text-[var(--muted-foreground)]">{{ t('payment.orders.payAmount') }}</p>
            <p class="mt-0.5 font-mono text-sm font-semibold tabular-nums" style="color: var(--brand-text)">{{ paymentAmountSymbol(selectedOrder) }}{{ selectedOrder.pay_amount.toFixed(2) }}</p>
          </div>
          <div class="rounded-xl border p-3" style="border-color: var(--border); background: var(--surface-2)">
            <p class="text-xs text-[var(--muted-foreground)]">{{ t('payment.orders.paymentMethod') }}</p>
            <p class="mt-0.5 text-sm font-medium text-[var(--foreground)]">{{ t('payment.methods.' + selectedOrder.payment_type, selectedOrder.payment_type) }}</p>
          </div>
          <div class="rounded-xl border p-3" style="border-color: var(--border); background: var(--surface-2)">
            <p class="text-xs text-[var(--muted-foreground)]">{{ t('payment.admin.feeRate') }}</p>
            <p class="mt-0.5 font-mono text-sm tabular-nums text-[var(--foreground)]">{{ selectedOrder.fee_rate }}%</p>
          </div>
          <div class="rounded-xl border p-3" style="border-color: var(--border); background: var(--surface-2)">
            <p class="text-xs text-[var(--muted-foreground)]">{{ t('payment.orders.createdAt') }}</p>
            <p class="mt-0.5 font-mono text-xs tabular-nums text-[var(--foreground)]">{{ formatDateTime(selectedOrder.created_at) }}</p>
          </div>
          <div class="rounded-xl border p-3" style="border-color: var(--border); background: var(--surface-2)">
            <p class="text-xs text-[var(--muted-foreground)]">{{ t('payment.admin.expiresAt') }}</p>
            <p class="mt-0.5 font-mono text-xs tabular-nums text-[var(--foreground)]">{{ formatDateTime(selectedOrder.expires_at) }}</p>
          </div>
          <div v-if="selectedOrder.paid_at" class="rounded-xl border p-3" style="border-color: var(--border); background: var(--surface-2)">
            <p class="text-xs text-[var(--muted-foreground)]">{{ t('payment.admin.paidAt') }}</p>
            <p class="mt-0.5 font-mono text-xs tabular-nums text-[var(--foreground)]">{{ formatDateTime(selectedOrder.paid_at) }}</p>
          </div>
          <div v-if="selectedOrder.refund_amount" class="rounded-xl border p-3" style="border-color: var(--border); background: var(--surface-2)">
            <p class="text-xs text-[var(--muted-foreground)]">{{ t('payment.admin.refundAmount') }}</p>
            <p class="mt-0.5 font-mono text-sm font-semibold tabular-nums text-red-600 dark:text-red-400">{{ creditedAmountSymbol }}{{ selectedOrder.refund_amount.toFixed(2) }}</p>
          </div>
          <div v-if="selectedOrder.refund_reason" class="col-span-2 rounded-xl border p-3" style="border-color: var(--border); background: var(--surface-2)">
            <p class="text-xs text-[var(--muted-foreground)]">{{ t('payment.admin.refundReason') }}</p>
            <p class="mt-0.5 text-sm text-[var(--foreground)]">{{ selectedOrder.refund_reason }}</p>
          </div>
          <!-- Refund request info -->
          <div v-if="selectedOrder.refund_requested_at" class="col-span-2 border-t border-[var(--border)] pt-3">
            <p class="mb-2 text-xs font-medium text-purple-600 dark:text-purple-400">{{ t('payment.admin.refundRequestInfo') }}</p>
            <div class="grid grid-cols-2 gap-4">
              <div>
                <p class="text-xs text-[var(--muted-foreground)]">{{ t('payment.admin.refundRequestedAt') }}</p>
                <p class="text-sm text-[var(--foreground)]">{{ formatDateTime(selectedOrder.refund_requested_at) }}</p>
              </div>
              <div>
                <p class="text-xs text-[var(--muted-foreground)]">{{ t('payment.admin.refundRequestedBy') }}</p>
                <p class="text-sm text-[var(--foreground)]">#{{ selectedOrder.refund_requested_by }}</p>
              </div>
              <div class="col-span-2">
                <p class="text-xs text-[var(--muted-foreground)]">{{ t('payment.admin.refundRequestReason') }}</p>
                <p class="text-sm text-[var(--foreground)]">{{ selectedOrder.refund_request_reason }}</p>
              </div>
            </div>
          </div>
        </div>
        <!-- Audit Logs -->
        <div v-if="orderAuditLogs.length > 0" class="border-t border-[var(--border)] pt-4">
          <p class="mb-2 text-xs font-medium text-[var(--muted-foreground)]">{{ t('payment.admin.auditLogs') }}</p>
          <div class="max-h-48 space-y-2 overflow-y-auto">
            <div v-for="log in orderAuditLogs" :key="log.id" class="rounded-lg border border-[var(--border)] bg-[var(--surface-2)] p-2.5">
              <div class="flex items-center justify-between">
                <span class="text-xs font-medium text-[var(--foreground)]">{{ log.action }}</span>
                <span class="text-xs text-[var(--muted-foreground)]">{{ formatDateTime(log.created_at) }}</span>
              </div>
              <div v-if="log.detail" class="mt-1 break-all text-xs text-[var(--muted-foreground)]">{{ log.detail }}</div>
              <div v-if="log.operator" class="mt-1 text-xs text-[var(--muted-foreground)]">{{ t('payment.admin.operator') }}: {{ log.operator }}</div>
            </div>
          </div>
        </div>
      </div>
    </BaseDialog>

    <AdminRefundDialog :show="showRefundDialog" :order="selectedOrder" :submitting="refundSubmitting" :require-force="refundRequireForce" :warning="refundWarning" @confirm="handleRefund" @cancel="closeRefundDialog" />
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminPaymentAPI } from '@/api/admin/payment'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { formatOrderDateTime } from '@/components/payment/orderUtils'
import type { PaymentOrder } from '@/types/payment'
import AppLayout from '@/components/layout/AppLayout.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import AdminRefundDialog from '@/components/admin/payment/AdminRefundDialog.vue'
import OrderStatusBadge from '@/components/payment/OrderStatusBadge.vue'
import OrderTable from '@/components/payment/OrderTable.vue'
import { currencySymbol } from '@/components/payment/currency'

interface AuditLog {
  id: number
  action: string
  detail: string | null
  operator: string | null
  created_at: string
}

const { t } = useI18n()
const appStore = useAppStore()

const ordersLoading = ref(false)
const orders = ref<PaymentOrder[]>([])
const orderSearch = ref('')
const orderFilters = reactive({ status: '', payment_type: '', order_type: '' })
const orderPagination = reactive({ page: 1, page_size: 20, total: 0 })
const selectedOrder = ref<PaymentOrder | null>(null)
const showDetailDialog = ref(false)
const showRefundDialog = ref(false)
const refundSubmitting = ref(false)
const refundRequireForce = ref(false)
const refundWarning = ref('')
const refundQueryingIds = ref(new Set<number>())
const orderAuditLogs = ref<AuditLog[]>([])
const creditedAmountSymbol = currencySymbol('USD')
const pendingRefundCount = computed(
  () => orders.value.filter(o => o.status === 'REFUND_REQUESTED').length,
)

function paymentAmountSymbol(order: PaymentOrder | null | undefined): string {
  return currencySymbol(order?.currency)
}

let debounceTimer: ReturnType<typeof setTimeout> | null = null
function debounceLoadOrders() {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => loadOrders(), 300)
}

async function loadOrders() {
  ordersLoading.value = true
  try {
    const res = await adminPaymentAPI.getOrders({
      page: orderPagination.page, page_size: orderPagination.page_size,
      keyword: orderSearch.value || undefined, status: orderFilters.status || undefined,
      payment_type: orderFilters.payment_type || undefined, order_type: orderFilters.order_type || undefined,
    })
    orders.value = res.data.items || []
    orderPagination.total = res.data.total || 0
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
  } finally { ordersLoading.value = false }
}

function handleOrderPageChange(page: number) { orderPagination.page = page; loadOrders() }
function handleOrderPageSizeChange(size: number) { orderPagination.page_size = size; orderPagination.page = 1; loadOrders() }

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

async function showOrderDetail(order: PaymentOrder) {
  selectedOrder.value = order
  orderAuditLogs.value = []
  showDetailDialog.value = true
  try {
    const res = await adminPaymentAPI.getOrder(order.id)
    const data = res.data as unknown as Record<string, unknown>
    if (data.order) selectedOrder.value = data.order as PaymentOrder
    orderAuditLogs.value = ((data.auditLogs || data.audit_logs || []) as unknown) as AuditLog[]
  } catch (_err: unknown) { /* keep cached order data */ }
}

async function handleCancelOrder(order: PaymentOrder) {
  try { await adminPaymentAPI.cancelOrder(order.id); appStore.showSuccess(t('payment.admin.orderCancelled')); loadOrders() }
  catch (err: unknown) { appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error'))) }
}

async function handleRetryOrder(order: PaymentOrder) {
  try { await adminPaymentAPI.retryRecharge(order.id); appStore.showSuccess(t('payment.admin.retrySuccess')); loadOrders() }
  catch (err: unknown) { appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error'))) }
}

function openRefundDialog(order: PaymentOrder) {
  selectedOrder.value = order
  refundRequireForce.value = false
  refundWarning.value = ''
  showRefundDialog.value = true
}

function closeRefundDialog() {
  showRefundDialog.value = false
  refundRequireForce.value = false
  refundWarning.value = ''
}

function isRefundPendingWarning(warning: string | undefined): boolean {
  return /pending|处理中|待/.test(String(warning || '').toLowerCase())
}

async function handleRefund(data: { amount: number; reason: string; deduct_balance: boolean; force: boolean }) {
  if (!selectedOrder.value) return
  refundSubmitting.value = true
  try {
    const res = await adminPaymentAPI.refundOrder(selectedOrder.value.id, { amount: data.amount, reason: data.reason, deduct_balance: data.deduct_balance, force: data.force })
    if (res.data.success) {
      appStore.showSuccess(t('payment.admin.refundSuccess'))
      closeRefundDialog()
      loadOrders()
      return
    }
    if (isRefundPendingWarning(res.data.warning)) {
      appStore.showSuccess(t('payment.admin.refundPending'))
      closeRefundDialog()
      loadOrders()
      return
    }
    if (res.data.require_force) {
      // Backend needs an explicit force confirmation (e.g. the user spent their
      // balance after requesting the refund). Keep the dialog open and surface
      // the force checkbox instead of dropping the admin back to the list.
      refundRequireForce.value = true
      refundWarning.value = res.data.warning || ''
      return
    }
    appStore.showError(res.data.warning || t('common.error'))
  } catch (err: unknown) { appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error'))) }
  finally { refundSubmitting.value = false }
}

async function handleQueryRefund(order: PaymentOrder) {
  refundQueryingIds.value = new Set(refundQueryingIds.value).add(order.id)
  try {
    const res = await adminPaymentAPI.queryRefund(order.id)
    if (res.data.success) {
      appStore.showSuccess(t('payment.admin.refundSuccess'))
    } else if (isRefundPendingWarning(res.data.warning)) {
      appStore.showSuccess(t('payment.admin.refundPending'))
    } else {
      appStore.showError(res.data.warning || t('common.error'))
    }
    loadOrders()
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
  } finally {
    const next = new Set(refundQueryingIds.value)
    next.delete(order.id)
    refundQueryingIds.value = next
  }
}

function formatDateTime(dateStr: string): string { return formatOrderDateTime(dateStr) }

onMounted(() => loadOrders())
</script>
