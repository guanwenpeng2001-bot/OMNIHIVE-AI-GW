<template>
  <AppLayout>
    <div class="space-y-4">
      <!-- High-Density Workbench Toolbar & Search Bar -->
      <div class="card flex flex-wrap items-center justify-between gap-3 p-4">
        <div class="flex flex-wrap items-center gap-2.5">
          <span class="chip font-medium">
            <span class="pulse-dot" :class="onSaleCount > 0 ? 'pulse-ok' : 'pulse-warn'"></span>
            <span>{{ t('payment.admin.forSale') }}</span>
          </span>
          <span class="pill pill-ghost font-mono text-xs tabular-nums">
            {{ onSaleCount }}/{{ plans.length }}
          </span>
          <div class="relative sm:w-64">
            <Icon
              name="search"
              size="sm"
              class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-[var(--muted-foreground)]"
            />
            <input
              v-model.trim="planSearch"
              type="text"
              :placeholder="t('payment.admin.planName')"
              class="input py-1.5 pl-9 text-xs"
            />
          </div>
        </div>

        <div class="flex items-center gap-2">
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="plansLoading"
            :title="t('common.refresh')"
            @click="loadPlans"
          >
            <Icon name="refresh" size="sm" :class="plansLoading ? 'animate-spin' : ''" />
          </button>
          <button type="button" class="btn btn-primary btn-sm" @click="openPlanEdit(null)">
            <Icon name="plus" size="sm" />
            {{ t('payment.admin.createPlan') }}
          </button>
        </div>
      </div>

      <!-- Plans Table -->
      <DataTable :columns="planColumns" :data="filteredPlans" :loading="plansLoading">
        <template #cell-id="{ value }">
          <span class="font-mono text-xs tabular-nums text-[var(--muted-foreground)]">#{{ value }}</span>
        </template>
        <template #cell-name="{ value, row }">
          <div class="space-y-0.5">
            <span class="text-sm font-semibold" :class="getPlanNameClass(row.group_id)">{{ value }}</span>
            <p v-if="row.description" class="max-w-xs truncate text-xs text-[var(--muted-foreground)]">
              {{ row.description }}
            </p>
          </div>
        </template>
        <template #cell-group_id="{ value }">
          <span v-if="isGroupMissing(value)" class="text-sm">
            <span class="font-mono tabular-nums text-[var(--muted-foreground)]">#{{ value }}</span>
            <span class="ml-1 badge badge-danger">{{ t('payment.admin.groupMissing') }}</span>
          </span>
          <GroupBadge
            v-else-if="getGroup(value)"
            :name="getGroup(value)!.name"
            :platform="getGroup(value)!.platform"
            :rate-multiplier="getGroup(value)!.rate_multiplier"
          />
          <span v-else class="text-sm text-[var(--muted-foreground)]">-</span>
        </template>
        <template #cell-price="{ value, row }">
          <div class="font-mono text-sm tabular-nums">
            <span class="font-semibold text-[var(--foreground)]">{{ planCurrencySymbol(row.currency) }}{{ (value ?? 0).toFixed(2) }}</span>
            <span v-if="row.currency" class="pill pill-ghost ml-1 text-[11px]">{{ row.currency }}</span>
            <span v-if="row.original_price" class="ml-1.5 text-xs text-[var(--muted-foreground)] line-through">{{ planCurrencySymbol(row.currency) }}{{ row.original_price.toFixed(2) }}</span>
          </div>
        </template>
        <template #cell-validity_days="{ value, row }">
          <span class="pill pill-ghost font-mono text-xs tabular-nums">
            {{ value }} {{ t('payment.admin.' + (row.validity_unit || 'days')) }}
          </span>
        </template>
        <template #cell-for_sale="{ value, row }">
          <button
            type="button"
            :class="[
              'relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-[var(--brand)] focus:ring-offset-2',
              value ? 'bg-[var(--brand)]' : 'bg-[var(--surface-2)]'
            ]"
            @click="toggleForSale(row)"
          >
            <span :class="[
              'pointer-events-none inline-block h-4 w-4 transform rounded-full bg-[var(--card)] shadow ring-0 transition duration-200 ease-in-out',
              value ? 'translate-x-4' : 'translate-x-0'
            ]" />
          </button>
        </template>
        <template #cell-sort_order="{ value }">
          <span class="font-mono text-xs tabular-nums text-[var(--muted-foreground)]">{{ value }}</span>
        </template>
        <template #cell-actions="{ row }">
          <div class="flex items-center gap-1.5">
            <button
              type="button"
              class="btn btn-ghost btn-sm px-2 py-1 text-xs font-medium text-[var(--muted-foreground)] hover:text-blue-600 dark:hover:text-blue-400"
              @click="openPlanEdit(row)"
            >
              <Icon name="edit" size="sm" />
              <span class="text-xs">{{ t('common.edit') }}</span>
            </button>
            <button
              type="button"
              class="btn btn-ghost btn-sm px-2 py-1 text-xs font-medium text-[var(--muted-foreground)] hover:text-red-600 dark:hover:text-red-400"
              @click="confirmDeletePlan(row)"
            >
              <Icon name="trash" size="sm" />
              <span class="text-xs">{{ t('common.delete') }}</span>
            </button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- Plan Edit Dialog -->
    <PlanEditDialog :show="showPlanDialog" :plan="editingPlan" :groups="groups" :payment-config="paymentConfig" @close="showPlanDialog = false" @saved="loadPlans" />

    <ConfirmDialog :show="showDeletePlanDialog" :title="t('payment.admin.deletePlan')" :message="t('payment.admin.deletePlanConfirm')" :confirm-text="t('common.delete')" danger @confirm="handleDeletePlan" @cancel="showDeletePlanDialog = false" />
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminPaymentAPI } from '@/api/admin/payment'
import type { AdminPaymentConfig } from '@/api/admin/payment'
import { extractI18nErrorMessage } from '@/utils/apiError'
import adminAPI from '@/api/admin'
import type { SubscriptionPlan } from '@/types/payment'
import type { AdminGroup } from '@/types'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import PlanEditDialog from './PlanEditDialog.vue'
import { currencySymbol } from '@/components/payment/currency'
import { platformTextClass } from '@/utils/platformColors'

const { t } = useI18n()
const appStore = useAppStore()

function planCurrencySymbol(currency?: string): string {
  return currencySymbol(currency || 'USD')
}

// ==================== Groups ====================

const groups = ref<AdminGroup[]>([])
const paymentConfig = ref<AdminPaymentConfig | null>(null)

async function loadGroups() {
  try {
    groups.value = await adminAPI.groups.getAll()
  } catch { /* ignore */ }
}

async function loadPaymentConfig() {
  try {
    const res = await adminPaymentAPI.getConfig()
    paymentConfig.value = res.data
  } catch { /* preview only */ }
}

function getGroup(id: number): AdminGroup | undefined {
  return groups.value.find(g => g.id === id)
}

function isGroupMissing(id: number): boolean {
  return id > 0 && !groups.value.find(g => g.id === id)
}

function getPlanNameClass(groupId: number): string {
  const group = getGroup(groupId)
  return group ? platformTextClass(group.platform) : 'text-[var(--foreground)]'
}


// ==================== Plans ====================

const plansLoading = ref(false)
const plans = ref<SubscriptionPlan[]>([])
const planSearch = ref('')
const showPlanDialog = ref(false)
const showDeletePlanDialog = ref(false)
const editingPlan = ref<SubscriptionPlan | null>(null)
const deletingPlanId = ref<number | null>(null)

const onSaleCount = computed(() => plans.value.filter(p => p.for_sale).length)

const filteredPlans = computed(() => {
  const query = planSearch.value.trim().toLowerCase()
  if (!query) return plans.value
  return plans.value.filter(plan => {
    const groupName = getGroup(plan.group_id)?.name || ''
    return `${plan.name} ${plan.description || ''} ${groupName}`.toLowerCase().includes(query)
  })
})

const planColumns = computed((): Column[] => [
  { key: 'id', label: 'ID' },
  { key: 'name', label: t('payment.admin.planName') },
  { key: 'group_id', label: t('payment.admin.group') },
  { key: 'price', label: t('payment.admin.price') },
  { key: 'validity_days', label: t('payment.admin.validity') },
  { key: 'for_sale', label: t('payment.admin.forSale') },
  { key: 'sort_order', label: t('payment.admin.sortOrder') },
  { key: 'actions', label: t('common.actions') },
])

async function loadPlans() {
  plansLoading.value = true
  try {
    const res = await adminPaymentAPI.getPlans()
    // Backend returns features as newline-separated string; parse to array
    plans.value = (res.data || []).map((p: Omit<SubscriptionPlan, 'features'> & { features: string | string[] }) => ({
      ...p,
      features: typeof p.features === 'string'
        ? p.features.split('\n').map((f: string) => f.trim()).filter(Boolean)
        : (p.features || []),
    }))
  }
  catch (err: unknown) { appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error'))) }
  finally { plansLoading.value = false }
}

function openPlanEdit(plan: SubscriptionPlan | null) {
  editingPlan.value = plan
  showPlanDialog.value = true
}


/** Quick toggle for_sale from the list */
async function toggleForSale(plan: SubscriptionPlan) {
  try {
    await adminPaymentAPI.updatePlan(plan.id, { for_sale: !plan.for_sale })
    plan.for_sale = !plan.for_sale
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
  }
}

function confirmDeletePlan(plan: SubscriptionPlan) { deletingPlanId.value = plan.id; showDeletePlanDialog.value = true }
async function handleDeletePlan() {
  if (!deletingPlanId.value) return
  try { await adminPaymentAPI.deletePlan(deletingPlanId.value); appStore.showSuccess(t('common.deleted')); showDeletePlanDialog.value = false; loadPlans() }
  catch (err: unknown) { appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error'))) }
}

// ==================== Lifecycle ====================

onMounted(() => {
  loadGroups()
  loadPaymentConfig()
  loadPlans()
})
</script>
