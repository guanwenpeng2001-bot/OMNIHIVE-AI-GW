<template>
  <AppLayout>
    <BillingNav />
    <div class="space-y-4">
      <div
        v-if="isPaymentDisabled"
        data-testid="payment-disabled-notice"
        class="card border-amber-200/80 bg-amber-50/60 p-4 dark:border-amber-800/50 dark:bg-amber-950/20 sm:p-5"
      >
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="space-y-1">
            <h2 class="text-sm font-semibold text-amber-950 dark:text-amber-200">
              {{ t('publicPortal.paymentDisabled.title') }}
            </h2>
            <p class="text-xs text-amber-900/90 dark:text-amber-100/80">
              {{ t('publicPortal.paymentDisabled.description') }}
            </p>
          </div>
          <div class="flex flex-wrap gap-2">
            <router-link to="/redeem" class="btn btn-secondary text-xs">
              {{ t('publicPortal.paymentDisabled.redeemAction') }}
            </router-link>
            <router-link to="/dashboard" class="btn btn-secondary text-xs">
              {{ t('publicPortal.paymentDisabled.dashboardAction') }}
            </router-link>
          </div>
        </div>
      </div>
      <!-- 界面风格高质感紧凑筛选工具条 + 订单状态概览微标 -->
      <div class="card oh-orders-toolbar p-4 sm:px-5 sm:py-3.5">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="flex flex-wrap items-center gap-2.5">
            <Select
              v-model="currentFilter"
              :options="statusFilters"
              class="w-36 sm:w-40"
              @change="handlePageChange(1)"
            />

            <!-- 订单状态概览微标 -->
            <div class="flex flex-wrap items-center gap-1.5" aria-label="订单状态概览">
              <span class="chip text-xs">
                <span
                  class="pulse-dot"
                  :class="pendingOrderCount > 0 ? 'pulse-warn' : 'pulse-ok'"
                  aria-hidden="true"
                />
                <span class="text-[var(--muted-foreground)]">总计</span>
                <b class="font-mono tabular-nums text-[var(--foreground)]">{{ pagination.total }}</b>
              </span>
              <span class="pill pill-ghost font-mono text-[11px] tabular-nums">
                <span>COMPLETED</span>
                <b>{{ completedOrderCount }}</b>
              </span>
              <span
                v-if="pendingOrderCount > 0"
                class="pill font-mono text-[11px] tabular-nums text-amber-700 dark:text-amber-300"
                style="background: color-mix(in srgb, var(--warn, #d97706) 14%, transparent); border-color: color-mix(in srgb, var(--warn, #d97706) 30%, transparent)"
              >
                <span>PENDING</span>
                <b>{{ pendingOrderCount }}</b>
              </span>
              <span
                v-if="currentFilter"
                class="pill pill-ghost font-mono text-[11px]"
              >
                FILTER: {{ currentFilter }}
              </span>
            </div>
          </div>

          <div class="flex flex-1 items-center justify-end gap-2">
            <button @click="fetchOrders" :disabled="loading" class="btn btn-secondary" :title="t('common.refresh')">
              <Icon name="refresh" size="md" :class="loading ? 'motion-safe:animate-spin' : ''" />
            </button>
            <button class="btn btn-primary" @click="router.push('/purchase')">{{ t('payment.result.backToRecharge') }}</button>
          </div>
        </div>
      </div>

      <!-- Table -->
      <OrderTable :orders="orders" :loading="loading">
        <template #actions="{ row }">
          <div class="flex items-center gap-2">
            <button
              v-if="row.status === 'PENDING'"
              @click="handleCancel(row.id)"
              class="inline-flex items-center gap-1 rounded-md border border-amber-200/80 bg-amber-50/70 px-2.5 py-1 text-xs font-medium text-amber-700 transition-colors hover:bg-amber-100/80 dark:border-amber-800/50 dark:bg-amber-900/20 dark:text-amber-300 dark:hover:bg-amber-900/35"
            >
              <Icon name="x" size="sm" />
              <span>{{ t('payment.orders.cancel') }}</span>
            </button>
            <button
              v-if="canRequestRefund(row)"
              @click="openRefundDialog(row)"
              class="inline-flex items-center gap-1 rounded-md border border-[var(--border)] bg-[var(--surface-2)] px-2.5 py-1 text-xs font-medium text-[var(--foreground)] transition-colors hover:bg-[var(--surface-2)]"
            >
              <Icon name="dollar" size="sm" />
              <span>{{ t('payment.orders.requestRefund') }}</span>
            </button>
          </div>
        </template>
      </OrderTable>

      <!-- Pagination -->
      <Pagination
        v-if="pagination.total > 0"
        :page="pagination.page"
        :total="pagination.total"
        :page-size="pagination.page_size"
        @update:page="handlePageChange"
        @update:pageSize="handlePageSizeChange"
      />

      <div
        data-testid="order-invoice-support"
        class="card p-4 sm:p-5"
      >
        <div class="flex flex-wrap items-start justify-between gap-4">
          <div class="space-y-1">
            <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('publicPortal.accountService.invoiceTitle') }}
            </h3>
            <p class="text-xs text-gray-600 dark:text-dark-300">
              {{ t('publicPortal.accountService.invoiceSummary') }}
            </p>
            <p class="text-xs text-gray-500 dark:text-dark-400">
              {{ t('publicPortal.accountService.invoiceDesc') }}
            </p>
          </div>
          <router-link
            to="/docs#support"
            class="btn btn-secondary shrink-0 text-xs"
          >
            {{ t('publicPortal.accountService.invoiceSupportLink') }}
          </router-link>
        </div>
      </div>
    </div>

    <!-- Cancel Confirm Dialog -->
    <BaseDialog :show="!!cancelTargetId" :title="t('payment.orders.cancel')" width="narrow" @close="cancelTargetId = null">
      <p class="text-sm text-[var(--muted-foreground)]">{{ t('payment.confirmCancel') }}</p>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button class="btn btn-secondary" @click="cancelTargetId = null">{{ t('common.cancel') }}</button>
          <button class="btn btn-danger" :disabled="actionLoading" @click="confirmCancel">{{ actionLoading ? t('common.processing') : t('payment.orders.cancel') }}</button>
        </div>
      </template>
    </BaseDialog>

    <!-- Refund Dialog -->
    <BaseDialog :show="!!refundTarget" :title="t('payment.orders.requestRefund')" @close="refundTarget = null">
      <div v-if="refundTarget" class="space-y-4">
        <div class="rounded-xl border border-[var(--border)] bg-[var(--surface-2)] p-4">
          <div class="flex justify-between text-sm">
            <span class="text-[var(--muted-foreground)]">{{ t('payment.orders.orderId') }}</span>
            <span class="font-mono tabular-nums text-[var(--foreground)]">#{{ refundTarget.id }}</span>
          </div>
          <div class="mt-2 flex justify-between text-sm">
            <span class="text-[var(--muted-foreground)]">{{ t('payment.orders.amount') }}</span>
            <span class="font-mono font-semibold tabular-nums text-[var(--foreground)]">${{ refundTarget.amount.toFixed(2) }}</span>
          </div>
        </div>
        <div>
          <label class="input-label">{{ t('payment.refundReason') }}</label>
          <textarea v-model="refundReason" rows="3" class="input mt-1 w-full" :placeholder="t('payment.refundReasonPlaceholder')" />
        </div>
      </div>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button class="btn btn-secondary" @click="refundTarget = null">{{ t('common.cancel') }}</button>
          <button class="btn btn-primary" :disabled="actionLoading || !refundReason.trim()" @click="confirmRefund">{{ actionLoading ? t('common.processing') : t('payment.orders.requestRefund') }}</button>
        </div>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useAppStore } from '@/stores'
import { paymentAPI } from '@/api/payment'
import { extractI18nErrorMessage } from '@/utils/apiError'
import type { PaymentOrder } from '@/types/payment'
import AppLayout from '@/components/layout/AppLayout.vue'
import BillingNav from '@/features/billing/components/BillingNav.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import OrderTable from '@/components/payment/OrderTable.vue'

const { t } = useI18n()
const router = useRouter()
const appStore = useAppStore()
const isPaymentDisabled = computed(() => appStore.cachedPublicSettings?.payment_enabled === false)

const loading = ref(false)
const actionLoading = ref(false)
const orders = ref<PaymentOrder[]>([])
const refundEligibleProviders = ref<Set<string>>(new Set())
const currentFilter = ref('')
const cancelTargetId = ref<number | null>(null)
const refundTarget = ref<PaymentOrder | null>(null)
const refundReason = ref('')
const pagination = reactive({ page: 1, page_size: 20, total: 0 })

const statusFilters = computed(() => [
  { value: '', label: t('common.all') },
  { value: 'PENDING', label: t('payment.status.pending') },
  { value: 'COMPLETED', label: t('payment.status.completed') },
  { value: 'FAILED', label: t('payment.status.failed') },
  { value: 'REFUNDED', label: t('payment.status.refunded') },
])

const completedOrderCount = computed(
  () => orders.value.filter((item) => item.status === 'COMPLETED').length
)
const pendingOrderCount = computed(
  () => orders.value.filter((item) => item.status === 'PENDING').length
)

async function fetchOrders() {
  loading.value = true
  try {
    const res = await paymentAPI.getMyOrders({
      page: pagination.page,
      page_size: pagination.page_size,
      status: currentFilter.value || undefined,
    })
    orders.value = res.data.items || []
    pagination.total = res.data.total || 0
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
  } finally {
    loading.value = false
  }
}

function handlePageChange(page: number) { pagination.page = page; fetchOrders() }
function handlePageSizeChange(size: number) { pagination.page_size = size; pagination.page = 1; fetchOrders() }

function handleCancel(orderId: number) { cancelTargetId.value = orderId }

async function confirmCancel() {
  if (!cancelTargetId.value) return
  actionLoading.value = true
  try {
    await paymentAPI.cancelOrder(cancelTargetId.value)
    appStore.showSuccess(t('common.success'))
    cancelTargetId.value = null
    await fetchOrders()
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
  } finally {
    actionLoading.value = false
  }
}

function openRefundDialog(order: PaymentOrder) { refundTarget.value = order; refundReason.value = '' }

async function confirmRefund() {
  if (!refundTarget.value || !refundReason.value.trim()) return
  actionLoading.value = true
  try {
    await paymentAPI.requestRefund(refundTarget.value.id, { reason: refundReason.value.trim() })
    appStore.showSuccess(t('common.success'))
    refundTarget.value = null
    refundReason.value = ''
    await fetchOrders()
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
  } finally {
    actionLoading.value = false
  }
}

function canRequestRefund(order: PaymentOrder): boolean {
  if (order.status !== 'COMPLETED') return false
  if (!order.provider_instance_id) return false
  return refundEligibleProviders.value.has(order.provider_instance_id)
}

async function loadRefundEligibility() {
  try {
    const res = await paymentAPI.getRefundEligibleProviders()
    refundEligibleProviders.value = new Set(res.data.provider_instance_ids || [])
  } catch { /* ignore — default to hiding refund button */ }
}

onMounted(() => { fetchOrders(); loadRefundEligibility() })
</script>
