<template>
  <AppLayout>
    <div class="space-y-5">
      <!-- Workbench Header with Settlement Health Capsule & Segmented Day Switcher -->
      <div
        class="card flex flex-wrap items-center justify-between gap-3 p-4"
        style="
          background: linear-gradient(135deg, color-mix(in srgb, var(--brand) 6%, var(--card)), var(--card));
          border-color: color-mix(in srgb, var(--brand) 24%, var(--border));
        "
      >
        <div class="flex flex-wrap items-center gap-2.5">
          <span class="chip font-medium">
            <span class="pulse-dot" :class="loading ? 'pulse-warn' : 'pulse-ok'"></span>
            <span>{{ t('payment.admin.paymentDistribution') }}</span>
          </span>
          <span class="pill pill-ghost font-mono text-xs tabular-nums">
            {{ days }}{{ t('payment.admin.daySuffix') }}
          </span>
          <span v-if="stats" class="pill pill-ghost font-mono text-xs tabular-nums">
            #{{ stats.total_count }} · {{ stats.payment_methods?.length || 0 }} channels
          </span>
        </div>

        <div class="flex items-center gap-2">
          <div class="segmented" role="group">
            <button
              v-for="d in DAYS_OPTIONS"
              :key="d"
              type="button"
              class="font-mono tabular-nums"
              :class="{ on: days === d, active: days === d }"
              @click="days = d"
            >
              {{ d }}{{ t('payment.admin.daySuffix') }}
            </button>
          </div>
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="loading"
            :title="t('common.refresh')"
            @click="loadDashboard"
          >
            <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>
      </div>

      <!-- Dashboard Content -->
      <div v-if="loading" class="flex items-center justify-center py-12">
        <LoadingSpinner />
      </div>
      <template v-else-if="stats">
        <OrderStatsCards :stats="stats" />
        <DailyRevenueChart :data="stats.daily_series || []" :loading="loading" />
        <div class="grid grid-cols-1 gap-5 lg:grid-cols-2">
          <!-- Payment Methods Distribution -->
          <div class="card p-5">
            <div class="mb-4 flex items-center justify-between gap-2">
              <h3 class="text-sm font-semibold tracking-tight text-[var(--foreground)]">
                {{ t('payment.admin.paymentDistribution') }}
              </h3>
              <span class="pill pill-ghost font-mono text-[11px] tabular-nums">
                {{ stats.payment_methods?.length || 0 }}
              </span>
            </div>
            <div
              v-if="!stats.payment_methods?.length"
              class="flex h-32 items-center justify-center text-sm text-[var(--muted-foreground)]"
            >
              {{ t('payment.admin.noData') }}
            </div>
            <div v-else class="space-y-3.5">
              <div
                v-for="method in stats.payment_methods"
                :key="method.type"
                class="space-y-1.5 rounded-xl border p-3 transition-colors"
                style="border-color: var(--border); background: color-mix(in srgb, var(--surface-2) 55%, var(--card))"
              >
                <div class="flex items-start justify-between gap-3">
                  <div class="flex items-center gap-2">
                    <span :class="['inline-block h-2.5 w-2.5 rounded-full', methodColor(method.type)]"></span>
                    <span class="text-sm font-medium text-[var(--foreground)]">
                      {{ t('payment.methods.' + method.type, method.type) }}
                    </span>
                    <span class="pill pill-ghost font-mono text-[11px] tabular-nums">
                      {{ method.count }} · {{ methodSharePercent(method.count) }}%
                    </span>
                  </div>
                  <div class="space-y-0.5 text-right font-mono tabular-nums">
                    <span
                      v-for="[currency, amount] in sortedAmounts(method.amount)"
                      :key="currency"
                      class="block text-sm font-semibold text-[var(--foreground)]"
                    >
                      {{ formatMoney(currency, amount) }}
                    </span>
                  </div>
                </div>
                <div class="meter">
                  <span :style="{ width: `${methodSharePercent(method.count)}%` }"></span>
                </div>
              </div>
            </div>
          </div>

          <!-- Top Users Leaderboard -->
          <div class="card p-5">
            <div class="mb-4 flex items-center justify-between gap-2">
              <h3 class="text-sm font-semibold tracking-tight text-[var(--foreground)]">
                {{ t('payment.admin.topUsers') }}
              </h3>
              <span class="pill pill-ghost font-mono text-[11px] tabular-nums">
                TOP
              </span>
            </div>
            <div
              v-if="!hasTopUsers(stats.top_users)"
              class="flex h-32 items-center justify-center text-sm text-[var(--muted-foreground)]"
            >
              {{ t('payment.admin.noData') }}
            </div>
            <div v-else class="space-y-4">
              <div v-for="[currency, users] in sortedTopUsers(stats.top_users)" :key="currency" class="space-y-2.5">
                <div class="flex items-center justify-between">
                  <span class="pill pill-ghost font-mono tabular-nums text-[11px] font-semibold uppercase tracking-wider">
                    {{ currency }}
                  </span>
                </div>
                <div
                  v-for="(user, idx) in users"
                  :key="user.user_id"
                  class="space-y-1.5 rounded-xl border px-3 py-2.5 transition-colors hover:bg-[color-mix(in_srgb,var(--surface-2)_80%,transparent)]"
                  style="border-color: var(--border)"
                >
                  <div class="flex items-center justify-between gap-3">
                    <div class="flex min-w-0 items-center gap-2.5">
                      <span
                        :class="[
                          'flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full font-mono text-xs font-bold tabular-nums',
                          rankClass(idx),
                        ]"
                      >
                        {{ idx + 1 }}
                      </span>
                      <span class="truncate text-sm font-medium text-[var(--foreground)]">
                        {{ user.email }}
                      </span>
                    </div>
                    <span
                      class="flex-shrink-0 font-mono text-sm font-semibold tabular-nums"
                      style="color: var(--brand-text)"
                    >
                      {{ formatMoney(currency, user.amount) }}
                    </span>
                  </div>
                  <div class="meter">
                    <span :style="{ width: `${userSharePercent(users, user.amount)}%` }"></span>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminPaymentAPI } from '@/api/admin/payment'
import { extractI18nErrorMessage } from '@/utils/apiError'
import type { CurrencyAmounts, DashboardStats, TopUserPaymentStats } from '@/types/payment'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import OrderStatsCards from '@/components/admin/payment/OrderStatsCards.vue'
import DailyRevenueChart from '@/components/admin/payment/DailyRevenueChart.vue'

const { t } = useI18n()
const appStore = useAppStore()

const DAYS_OPTIONS = [7, 30, 90] as const
const days = ref<number>(30)
const loading = ref(false)
const stats = ref<DashboardStats | null>(null)

function methodColor(type: string): string {
  const c: Record<string, string> = {
    alipay: 'bg-blue-500', wxpay: 'bg-green-500',
    alipay_direct: 'bg-blue-400', wxpay_direct: 'bg-green-400',
    stripe: 'bg-purple-500',
  }
  return c[type] || 'bg-[var(--surface-2)]'
}

function rankClass(idx: number): string {
  if (idx === 0) return 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
  if (idx === 1) return 'bg-[var(--surface-2)] text-[var(--muted-foreground)]'
  if (idx === 2) return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400'
  return 'bg-[var(--surface-2)] text-[var(--muted-foreground)]'
}

function sortedAmounts(amounts: CurrencyAmounts): [string, number][] {
  return Object.entries(amounts).sort(([left], [right]) => left.localeCompare(right))
}

function sortedTopUsers(usersByCurrency: Record<string, TopUserPaymentStats[]>): [string, TopUserPaymentStats[]][] {
  return Object.entries(usersByCurrency).sort(([left], [right]) => left.localeCompare(right))
}

function hasTopUsers(usersByCurrency: Record<string, TopUserPaymentStats[]>): boolean {
  return Object.values(usersByCurrency).some(users => users.length > 0)
}

function methodSharePercent(count: number): number {
  const methods = stats.value?.payment_methods || []
  const total = methods.reduce((sum, item) => sum + (item.count || 0), 0)
  if (total <= 0) return 0
  return Math.min(100, Math.max(4, Math.round((count / total) * 100)))
}

function userSharePercent(users: TopUserPaymentStats[], amount: number): number {
  const maxAmount = Math.max(...users.map(u => u.amount || 0), 0)
  if (maxAmount <= 0) return 0
  return Math.min(100, Math.max(6, Math.round((amount / maxAmount) * 100)))
}

function formatMoney(currency: string, amount: number): string {
  return new Intl.NumberFormat(undefined, { style: 'currency', currency }).format(amount)
}

async function loadDashboard() {
  loading.value = true
  try {
    const res = await adminPaymentAPI.getDashboard(days.value)
    stats.value = res.data
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
  } finally {
    loading.value = false
  }
}

watch(days, () => loadDashboard())
onMounted(() => loadDashboard())
</script>
