<template>
  <AppLayout>
    <header class="dashboard-controls mb-4 flex flex-wrap items-center justify-between gap-3 border-b border-[var(--border)] pb-3">
      <div>
        <h1 class="text-xl font-semibold tracking-tight">{{ isZh ? '运行总览' : 'Operations Overview' }}</h1>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ isZh ? '请求、费用和趋势与用量记录联动，运行脉搏来自运维监控' : 'Requests, spend, and trends linked with usage logs; live pulse powered by Ops monitoring' }}</p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <label class="refresh-switch flex items-center gap-2 rounded-md border px-2.5 py-1.5 text-xs">
          <input v-model="autoRefreshEnabled" type="checkbox" @change="refreshCountdown = 30" />
          {{ isZh ? '自动刷新 30s' : 'Auto Refresh 30s' }}
        </label>
        <span class="min-w-14 font-mono text-xs tabular-nums text-gray-500 dark:text-gray-400" role="status">
          {{ refreshing ? (isZh ? '刷新中…' : 'Refreshing…') : autoRefreshEnabled ? `${refreshCountdown}s` : (isZh ? '已关闭' : 'Off') }}
        </span>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="dashboardBusy" @click="refreshDashboard">
          ↻ {{ isZh ? '刷新' : 'Refresh' }}
        </button>
      </div>
    </header>
    <ReportTimeContext :time-zone="reportTimeZone" class="mb-3" />
    <div class="space-y-4">
      <!-- Loading State -->
      <div v-if="loading" class="flex items-center justify-center py-12">
        <LoadingSpinner />
      </div>

      <UsageStatsState v-else-if="!stats" :state="chartError ? 'error' : 'unknown'" />
      <template v-else-if="stats">
        <AdminSetupBanner :total-accounts="stats.total_accounts" :live-models="liveModelSampleSize" />
        <p
          v-if="summaryScope !== 'all_time'"
          class="inline-flex items-center gap-2 rounded-lg border px-3 py-1.5 text-xs text-gray-600 dark:text-gray-300"
          style="border-color: var(--border); background: var(--surface-2)"
          data-testid="summary-scope"
        >
          <span class="pulse-dot pulse-warn"></span>
          <span>{{ summaryLabel }}<span v-if="summaryWindow"> · {{ summaryWindow }}</span></span>
        </p>

        <!-- 蜂巢运行脉搏 · 实时监控概览横幅卡片 -->
        <button
          type="button"
          class="card pulse-banner w-full p-3 text-left transition-colors sm:p-4"
          aria-haspopup="dialog"
          @click="openPulseDiagnosisDrawer"
          style="
            background: var(--card);
            border-color: var(--border);
          "
        >
          <div class="flex flex-wrap items-center justify-between gap-2">
            <div class="flex flex-wrap items-center gap-2.5">
              <span
                class="pulse-dot"
                :class="stats.error_accounts + stats.ratelimit_accounts + stats.overload_accounts > 0 ? 'pulse-warn' : 'pulse-ok'"
              ></span>
              <b class="text-sm font-semibold tracking-tight text-gray-900 dark:text-white">
                {{ isZh ? '运行脉搏' : 'Runtime Pulse' }}
              </b>
              <span class="pill pill-ghost font-mono text-[11px] tabular-nums">
                {{ stats.normal_accounts }}/{{ stats.total_accounts }} {{ t('common.active') }}
              </span>
            </div>
            <div class="flex items-center gap-2">
              <span
                class="inline-flex items-center gap-1 rounded-md px-2.5 py-1 text-xs font-medium transition-colors hover:bg-black/5 dark:hover:bg-white/5"
                style="color: var(--brand-text)"
              >
                <span>{{ isZh ? '查看诊断抽屉' : 'View Diagnostics' }}</span>
                <span aria-hidden="true">›</span>
              </span>
            </div>
          </div>

          <div class="mt-3 grid grid-cols-2 gap-2 lg:grid-cols-4">
            <div
              class="cursor-pointer rounded-lg border p-3 transition-colors hover:border-amber-500/40"
              style="border-color: var(--border); background: color-mix(in srgb, var(--card) 86%, transparent)"
            >
              <div class="flex items-center justify-between text-xs text-gray-500 dark:text-gray-400">
                <span>{{ isZh ? '当前活跃账号率' : 'Active Account Ratio' }}</span>
                <span class="font-mono text-[11px] tabular-nums">{{ stats.normal_accounts }}/{{ stats.total_accounts }}</span>
              </div>
              <b class="mt-1 block font-mono text-xl font-bold tabular-nums text-gray-900 dark:text-white">
                {{ activeAccountRate }}
              </b>
              <div class="meter mt-2">
                <span :style="{ width: activeAccountRate === '—' ? '0%' : activeAccountRate }"></span>
              </div>
            </div>

            <div
              class="cursor-pointer rounded-lg border p-3 transition-colors hover:border-amber-500/40"
              style="border-color: var(--border); background: color-mix(in srgb, var(--card) 86%, transparent)"
            >
              <div class="text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.dashboard.todayRequests') }}
              </div>
              <b class="mt-1 block font-mono text-xl font-bold tabular-nums text-gray-900 dark:text-white">
                {{ formatNumber(stats.today_requests) }}
              </b>
              <span class="mt-1 block text-[11px] text-gray-500 dark:text-gray-400">
                {{ stats.active_users }} {{ t('admin.dashboard.activeUsers') }}
              </span>
            </div>

            <div
              class="cursor-pointer rounded-lg border p-3 transition-colors hover:border-amber-500/40"
              style="border-color: var(--border); background: color-mix(in srgb, var(--card) 86%, transparent)"
            >
              <div class="text-xs text-gray-500 dark:text-gray-400">
                {{ isZh ? '当前 RPM / TPM' : 'Current RPM / TPM' }}
              </div>
              <b class="mt-1 block font-mono text-xl font-bold tabular-nums text-gray-900 dark:text-white">
                {{ formatTokens(stats.rpm) }}
                <span class="text-xs font-normal text-gray-500 dark:text-gray-400">RPM</span>
              </b>
              <span class="mt-1 block font-mono text-[11px] tabular-nums text-violet-600 dark:text-violet-400">
                {{ formatTokens(stats.tpm) }} TPM
              </span>
            </div>

            <div
              class="cursor-pointer rounded-lg border p-3 transition-colors hover:border-amber-500/40"
              style="border-color: var(--border); background: color-mix(in srgb, var(--card) 86%, transparent)"
            >
              <div class="text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.dashboard.avgResponse') }}
              </div>
              <b class="mt-1 block font-mono text-xl font-bold tabular-nums text-gray-900 dark:text-white">
                {{ summaryScope === 'unknown' ? '—' : formatDuration(stats.average_duration_ms) }}
              </b>
              <span class="mt-1 block text-[11px] text-gray-500 dark:text-gray-400">
                {{ isZh ? '点击查看实时诊断与监控入口' : 'Click to inspect live diagnostics & monitoring' }}
              </span>
            </div>
          </div>
        </button>

        <!-- Row 1: Core Stats -->
        <div class="grid grid-cols-2 gap-3.5 lg:grid-cols-4">
          <!-- Total API Keys -->
          <div
            class="card kpi-card kpi-tone-info p-4"
            role="button"
            tabindex="0"
            @click="router.push('/admin/users')"
            @keydown.enter="router.push('/admin/users')"
          >
            <div class="flex items-start justify-between gap-2">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">
                {{ t('admin.dashboard.apiKeys') }}
              </p>
              <span class="text-xs opacity-35" aria-hidden="true">›</span>
            </div>
            <div class="mt-1.5">
              <p class="font-mono text-2xl font-bold tabular-nums text-gray-900 dark:text-white">
                {{ stats.total_api_keys }}
              </p>
              <p class="mt-0.5 text-xs text-green-600 dark:text-green-400">
                {{ stats.active_api_keys }} {{ t('common.active') }}
              </p>
            </div>
          </div>

          <!-- Service Accounts -->
          <div
            class="card kpi-card kpi-tone-brand p-4"
            role="button"
            tabindex="0"
            @click="router.push('/admin/accounts')"
            @keydown.enter="router.push('/admin/accounts')"
          >
            <div class="flex items-start justify-between gap-2">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">
                {{ t('admin.dashboard.accounts') }}
              </p>
              <span class="text-xs opacity-35" aria-hidden="true">›</span>
            </div>
            <div class="mt-1.5">
              <p class="font-mono text-2xl font-bold tabular-nums text-gray-900 dark:text-white">
                {{ stats.total_accounts }}
              </p>
              <p class="mt-0.5 text-xs">
                <span class="text-green-600 dark:text-green-400"
                  >{{ stats.normal_accounts }} {{ t('common.active') }}</span
                >
                <span v-if="stats.error_accounts > 0" class="ml-1 text-red-500"
                  >{{ stats.error_accounts }} {{ t('common.error') }}</span
                >
              </p>
            </div>
          </div>

          <!-- Today Requests -->
          <div
            class="card kpi-card kpi-tone-ok p-4"
            role="button"
            tabindex="0"
            @click="router.push('/admin/usage')"
            @keydown.enter="router.push('/admin/usage')"
          >
            <div class="flex items-start justify-between gap-2">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">
                {{ t('admin.dashboard.todayRequests') }}
              </p>
              <span class="text-xs opacity-35" aria-hidden="true">›</span>
            </div>
            <div class="mt-1.5">
              <p class="font-mono text-2xl font-bold tabular-nums text-gray-900 dark:text-white">
                {{ stats.today_requests }}
              </p>
              <p class="mt-0.5 font-mono text-xs tabular-nums text-gray-500 dark:text-gray-400">
                {{ summaryLabel }}: {{ summaryScope === 'unknown' ? '—' : formatNumber(stats.total_requests) }}
              </p>
            </div>
          </div>

          <!-- New Users Today -->
          <div
            class="card kpi-card kpi-tone-ok p-4"
            role="button"
            tabindex="0"
            @click="router.push('/admin/users')"
            @keydown.enter="router.push('/admin/users')"
          >
            <div class="flex items-start justify-between gap-2">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">
                {{ t('admin.dashboard.users') }}
              </p>
              <span class="text-xs opacity-35" aria-hidden="true">›</span>
            </div>
            <div class="mt-1.5">
              <p class="font-mono text-2xl font-bold tabular-nums text-emerald-600 dark:text-emerald-400">
                +{{ stats.today_new_users }}
              </p>
              <p class="mt-0.5 font-mono text-xs tabular-nums text-gray-500 dark:text-gray-400">
                {{ t('common.total') }}: {{ formatNumber(stats.total_users) }}
              </p>
            </div>
          </div>
        </div>

        <!-- Row 2: Token Stats -->
        <div class="grid grid-cols-2 gap-3.5 lg:grid-cols-4">
          <!-- Today Tokens -->
          <div
            class="card kpi-card kpi-tone-warn p-4"
            role="button"
            tabindex="0"
            @click="router.push('/admin/usage')"
            @keydown.enter="router.push('/admin/usage')"
          >
            <div class="flex items-start justify-between gap-2">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">
                {{ t('admin.dashboard.todayTokens') }}
              </p>
              <span class="text-xs opacity-35" aria-hidden="true">›</span>
            </div>
            <div class="mt-1.5">
              <p class="font-mono text-2xl font-bold tabular-nums text-gray-900 dark:text-white">
                {{ formatTokens(stats.today_tokens) }}
              </p>
              <p class="mt-0.5 font-mono text-xs tabular-nums">
                <span
                  class="text-green-600 dark:text-green-400"
                  :title="t('admin.dashboard.actual')"
                  >{{ t('admin.dashboard.actual') }} USD {{ formatCost(stats.today_actual_cost) }}</span
                >
                <span class="text-gray-400 dark:text-gray-500"> / </span>
                <span
                  class="text-orange-500 dark:text-orange-400"
                  :title="t('admin.dashboard.accountCost')"
                  >{{ t('admin.dashboard.accountCost') }} USD {{ formatAccountCost(stats.today_account_cost) }}</span
                >
                <span class="text-gray-400 dark:text-gray-500"> / </span>
                <span
                  class="text-gray-400 dark:text-gray-500"
                  :title="t('admin.dashboard.standard')"
                  >{{ t('admin.dashboard.standard') }} USD {{ formatCost(stats.today_cost) }}</span
                >
              </p>
            </div>
          </div>

          <!-- Total Tokens -->
          <div
            class="card kpi-card kpi-tone-info p-4"
            role="button"
            tabindex="0"
            @click="router.push('/admin/usage')"
            @keydown.enter="router.push('/admin/usage')"
          >
            <div class="flex items-start justify-between gap-2">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">
                {{ summaryScope === 'all_time' ? t('admin.dashboard.totalTokens') : summaryLabel + (isZh ? ' · 总 Token' : ' · Tokens') }}
              </p>
              <span class="text-xs opacity-35" aria-hidden="true">›</span>
            </div>
            <div class="mt-1.5">
              <p class="font-mono text-2xl font-bold tabular-nums text-gray-900 dark:text-white">
                {{ summaryScope === 'unknown' ? '—' : formatTokens(stats.total_tokens) }}
              </p>
              <p class="mt-0.5 font-mono text-xs tabular-nums">
                <span
                  class="text-green-600 dark:text-green-400"
                  :title="t('admin.dashboard.actual')"
                  >{{ t('admin.dashboard.actual') }} USD {{ summaryScope === 'unknown' ? '—' : formatCost(stats.total_actual_cost) }}</span
                >
                <span class="text-gray-400 dark:text-gray-500"> / </span>
                <span
                  class="text-orange-500 dark:text-orange-400"
                  :title="t('admin.dashboard.accountCost')"
                  >{{ t('admin.dashboard.accountCost') }} USD {{ summaryScope === 'unknown' ? '—' : formatAccountCost(stats.total_account_cost) }}</span
                >
                <span class="text-gray-400 dark:text-gray-500"> / </span>
                <span
                  class="text-gray-400 dark:text-gray-500"
                  :title="t('admin.dashboard.standard')"
                  >{{ t('admin.dashboard.standard') }} USD {{ summaryScope === 'unknown' ? '—' : formatCost(stats.total_cost) }}</span
                >
              </p>
            </div>
          </div>

          <!-- Performance (RPM/TPM) -->
          <div
            class="card kpi-card kpi-tone-brand p-4"
            role="button"
            tabindex="0"
            @click="router.push('/admin/ops')"
            @keydown.enter="router.push('/admin/ops')"
          >
            <div class="flex items-start justify-between gap-2">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">
                {{ t('admin.dashboard.performance') }}
              </p>
              <span class="text-xs opacity-35" aria-hidden="true">›</span>
            </div>
            <div class="mt-1.5">
              <div class="flex items-baseline gap-2">
                <p class="font-mono text-2xl font-bold tabular-nums text-gray-900 dark:text-white">
                  {{ formatTokens(stats.rpm) }}
                </p>
                <span class="text-xs text-gray-500 dark:text-gray-400">RPM</span>
              </div>
              <div class="mt-0.5 flex items-baseline gap-2">
                <p class="font-mono text-xs font-semibold tabular-nums text-violet-600 dark:text-violet-400">
                  {{ formatTokens(stats.tpm) }}
                </p>
                <span class="text-xs text-gray-500 dark:text-gray-400">TPM</span>
              </div>
            </div>
          </div>

          <!-- Avg Response Time -->
          <div
            class="card kpi-card kpi-tone-warn p-4"
            role="button"
            tabindex="0"
            @click="router.push('/admin/ops')"
            @keydown.enter="router.push('/admin/ops')"
          >
            <div class="flex items-start justify-between gap-2">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">
                {{ t('admin.dashboard.avgResponse') }}
              </p>
              <span class="text-xs opacity-35" aria-hidden="true">›</span>
            </div>
            <div class="mt-1.5">
              <p class="font-mono text-2xl font-bold tabular-nums text-gray-900 dark:text-white">
                {{ summaryScope === 'unknown' ? '—' : formatDuration(stats.average_duration_ms) }}
              </p>
              <p v-if="summaryScope === 'retained' || summaryScope === 'range'" class="text-xs text-gray-500 dark:text-gray-400">{{ summaryLabel }}</p>
              <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                {{ stats.active_users }} {{ t('admin.dashboard.activeUsers') }}
              </p>
            </div>
          </div>
        </div>

        <!-- Quick Actions -->
        <div class="card p-4">
          <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
            <h2 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('admin.dashboard.quickActions') }}
            </h2>
            <div class="flex flex-wrap gap-1.5">
              <button
                v-for="shortcut in workbenchShortcuts"
                :key="shortcut.path"
                type="button"
                class="chip cursor-pointer transition-colors hover:border-amber-500/50 hover:bg-amber-500/5"
                @click="router.push(shortcut.path)"
              >
                <Icon :name="shortcut.icon" size="xs" />
                <span>{{ shortcut.label }}</span>
                <span class="opacity-45" aria-hidden="true">→</span>
              </button>
            </div>
          </div>
          <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
            <button
              v-if="canUseBatchImage"
              type="button"
              class="group flex items-center gap-3 rounded-lg bg-[var(--surface-2)] p-3 text-left transition-colors hover:bg-[var(--accent)]"
              @click="router.push('/batch-image')"
            >
              <span class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg bg-sky-100 text-sky-600 dark:bg-sky-900/30 dark:text-sky-400">
                <Icon name="sparkles" size="md" :stroke-width="2" />
              </span>
              <span class="min-w-0 flex-1">
                <span class="block text-sm font-medium text-gray-900 dark:text-white">
                  {{ t('admin.dashboard.batchImage') }}
                </span>
                <span class="block text-xs text-gray-500 dark:text-gray-400">
                  {{ t('admin.dashboard.batchImageDesc') }}
                </span>
              </span>
              <Icon name="chevronRight" size="sm" class="text-gray-400 group-hover:text-sky-500" />
            </button>
            <button
              type="button"
              class="group flex items-center gap-3 rounded-lg bg-[var(--surface-2)] p-3 text-left transition-colors hover:bg-[var(--accent)]"
              @click="router.push('/admin/groups')"
            >
              <span class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg bg-emerald-100 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-400">
                <Icon name="grid" size="md" :stroke-width="2" />
              </span>
              <span class="min-w-0 flex-1">
                <span class="block text-sm font-medium text-gray-900 dark:text-white">
                  {{ t('admin.dashboard.groupPricing') }}
                </span>
                <span class="block text-xs text-gray-500 dark:text-gray-400">
                  {{ t('admin.dashboard.groupPricingDesc') }}
                </span>
              </span>
              <Icon name="chevronRight" size="sm" class="text-gray-400 group-hover:text-emerald-500" />
            </button>
          </div>
        </div>

        <!-- Charts Section -->
        <div class="space-y-6">
          <!-- Date Range Filter -->
          <div class="card p-4">
            <div class="flex flex-wrap items-center gap-4">
              <div class="flex items-center gap-2">
                <span class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >{{ t('admin.dashboard.timeRange') }}:</span
                >
                <DateRangePicker
                  v-model:start-date="startDate"
                  v-model:end-date="endDate"
                  @change="onDateRangeChange"
                />
              </div>
              <button @click="loadDashboardStats" :disabled="chartsLoading" class="btn btn-secondary">
                {{ t('common.refresh') }}
              </button>
              <div class="ml-auto flex items-center gap-2">
                <span class="text-sm font-medium text-gray-700 dark:text-gray-300"
                  >{{ t('admin.dashboard.granularity') }}:</span
                >
                <div class="w-28">
                  <Select
                    v-model="granularity"
                    :options="granularityOptions"
                    @change="loadChartData"
                  />
                </div>
              </div>
            </div>
          </div>

          <!-- Charts Grid -->
          <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
            <ModelDistributionChart :model-unknown="!!historyGap" :model-error="chartError"
              :filters="reportQueryContext"
              :model-stats="modelStats"
              :enable-ranking-view="true"
              :ranking-items="rankingItems"
              :ranking-total-actual-cost="rankingTotalActualCost"
              :ranking-total-requests="rankingTotalRequests"
              :ranking-total-tokens="rankingTotalTokens"
              :loading="chartsLoading"
              :ranking-loading="rankingLoading"
              :ranking-error="rankingError"
              :ranking-unknown="rankingUnknown"
              :start-date="startDate"
              :end-date="endDate"
              @ranking-click="goToUserUsage"
            />
            <UsageStatsState :state="historyGap ? 'unknown' : chartError ? 'error' : 'known'">
              <TokenUsageTrend :time-zone="reportTimeZone" :trend-data="trendData" :loading="chartsLoading" />
            </UsageStatsState>
          </div>

          <!-- User Usage Trend (Full Width) -->
          <div class="card p-4">
            <h3 class="mb-4 text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('admin.dashboard.recentUsage') }} (Top 12)
            </h3>
            <div class="h-64">
              <div v-if="userTrendLoading" class="flex h-full items-center justify-center">
                <LoadingSpinner size="md" />
              </div>
              <UsageStatsState v-else-if="userTrendState !== 'known'" :state="userTrendState" />
              <Line v-else-if="userTrendChartData" :data="userTrendChartData" :options="lineOptions" />
              <div
                v-else
                class="flex h-full items-center justify-center text-sm text-gray-500 dark:text-gray-400"
              >
                {{ t('admin.dashboard.noDataAvailable') }}
              </div>
            </div>
            <div v-if="!userTrendLoading && userTrendChartData" class="mt-4 overflow-x-auto">
              <table class="w-full whitespace-nowrap text-right text-xs" data-testid="user-trend-table">
                <caption>{{ isZh ? '总 Token' : t('admin.dashboard.tokens') }} · {{ reportTimeZone }} · Top 12</caption>
                <thead><tr><th scope="col" class="p-2 text-left">{{ t('usage.time') }}</th><th v-for="(dataset, index) in userTrendChartData.datasets" :key="index" scope="col" class="p-2">{{ dataset.label }}</th></tr></thead>
                <tbody><tr v-for="(date, dateIndex) in userTrendChartData.labels" :key="date"><th scope="row" class="p-2 text-left font-normal">{{ date }}</th><td v-for="(dataset, index) in userTrendChartData.datasets" :key="index" class="p-2">{{ Number.isFinite(dataset.data[dateIndex]) ? dataset.data[dateIndex] : '-' }}</td></tr></tbody>
              </table>
            </div>
          </div>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { usageScope } from '@/utils/usageScope'
import { formatReportDateTime, resolveReportTimeZone, formatReportDateInput } from '@/utils/format'
import { isKnownAmount } from '@/utils/money'
import ReportTimeContext from '@/components/charts/ReportTimeContext.vue'

const reportProps = defineProps<{ timeZone?: string }>()
const reportTimeZone = resolveReportTimeZone(reportProps.timeZone)
const reportQueryContext = { timezone: reportTimeZone }

import UsageStatsState from '@/components/common/UsageStatsState.vue'
import { historyGapFromError, withTrendGaps, type HistoryGap } from '@/views/trendGaps'
import AdminSetupBanner from '@/features/onboarding/AdminSetupBanner.vue'
import { listAdminCatalogModels } from '@/api/admin/modelCatalog'
import { resolveCatalogWorkspace } from '@/features/catalog/adminWorkspace'
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useAppStore } from '@/stores/app'

const i18n = useI18n()
const { t } = i18n
const isZh = computed(() => {
  const rawLocale = typeof i18n.locale === 'string' ? i18n.locale : i18n.locale?.value
  if (rawLocale) return String(rawLocale).toLowerCase().startsWith('zh')
  const probe = t('common.active')
  if (probe && probe !== 'common.active') return /[\u4e00-\u9fa5]/.test(probe)
  return true
})
import type { DashboardStats } from '@/types'
import { adminAPI } from '@/api/admin'
import type {
  TrendDataPoint,
  ModelStat,
  UserUsageTrendPoint,
  UserSpendingRankingItem
} from '@/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import Select from '@/components/common/Select.vue'
import ModelDistributionChart from '@/components/charts/ModelDistributionChart.vue'
import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import { useBatchImageAccess } from '@/composables/useBatchImageAccess'
import { useThemeDirection } from '@/composables/useThemeDirection'

import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
  Filler
} from 'chart.js'
import { Line } from 'vue-chartjs'

// Register Chart.js components
ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
  Filler
)

const historyGap = ref<HistoryGap | null>(null)
const chartError = ref(false)
const userTrendState = ref<'known' | 'unknown' | 'error'>('known')
const appStore = useAppStore()
const router = useRouter()
const { openDrawer } = useThemeDirection()
const { canUseBatchImage, refreshBatchImageAccess } = useBatchImageAccess()
const liveModelSampleSize = ref<number | null>(null)
const stats = ref<DashboardStats | null>(null)
const activeAccountRate = computed(() => {
  const total = stats.value?.total_accounts ?? 0
  const normal = stats.value?.normal_accounts ?? 0
  if (total <= 0) return '—'
  return `${Math.min(100, Math.max(0, Math.round((normal / total) * 100)))}%`
})

const workbenchShortcuts = computed(() => [
  { path: '/admin/ops', label: isZh.value ? '运维实时监控' : 'Ops Live Monitor', icon: 'bolt' as const },
  { path: '/admin/channels/monitor', label: isZh.value ? '渠道监控' : 'Channel Monitor', icon: 'chart' as const },
  { path: '/admin/accounts', label: t('admin.dashboard.manageAccounts'), icon: 'server' as const },
  { path: '/admin/groups', label: t('admin.dashboard.groupPricing'), icon: 'grid' as const },
  { path: '/admin/users', label: t('admin.dashboard.manageUsers'), icon: 'users' as const },
  { path: '/admin/settings', label: t('admin.dashboard.systemSettings'), icon: 'cog' as const }
])

const summaryScope = computed(() => usageScope(stats.value?.coverage))
const summaryLabel = computed(() => summaryScope.value === 'all_time' ? t('common.total')
  : t('dashboard.' + (summaryScope.value === 'retained' ? 'scopeRetained' : summaryScope.value === 'range' ? 'scopeRange' : 'scopeUnknown')))
const summaryWindow = computed(() => {
  const coverage = stats.value?.coverage
  if (!coverage?.window_from || !coverage.window_until || !['retained', 'range'].includes(summaryScope.value)) return ''
  const zone = reportTimeZone
  return t('dashboard.scopeFrom', { start: formatReportDateTime(coverage.window_from, zone) }) + ' · '
    + t('dashboard.scopeUntil', { end: formatReportDateTime(coverage.window_until, zone) })
})

const loading = ref(false)
const chartsLoading = ref(false)
const userTrendLoading = ref(false)
const rankingLoading = ref(false)
const autoRefreshEnabled = ref(false)
const refreshCountdown = ref(30)
const refreshing = ref(false)
const dashboardBusy = computed(() => refreshing.value || loading.value || chartsLoading.value || userTrendLoading.value || rankingLoading.value)
let refreshTimer: ReturnType<typeof setInterval> | undefined

function openPulseDiagnosisDrawer() {
  const current = stats.value
  if (!current) return
  const metrics = [
    [isZh.value ? '当前活跃账号率' : 'Active Account Ratio', activeAccountRate.value],
    [isZh.value ? '正常 / 总账号' : 'Active / Total Accounts', `${current.normal_accounts} / ${current.total_accounts}`],
    [isZh.value ? '异常账号' : 'Error Accounts', formatNumber(current.error_accounts)],
    [isZh.value ? '限流账号' : 'Rate-Limited Accounts', formatNumber(current.ratelimit_accounts)],
    [isZh.value ? '过载账号' : 'Overloaded Accounts', formatNumber(current.overload_accounts)],
    [isZh.value ? '今日请求' : 'Today Requests', formatNumber(current.today_requests)],
    [isZh.value ? '当前 RPM / TPM' : 'Current RPM / TPM', `${formatTokens(current.rpm)} / ${formatTokens(current.tpm)}`],
    [isZh.value ? '平均延迟' : 'Average Latency', summaryScope.value === 'unknown' ? '—' : formatDuration(current.average_duration_ms)]
  ]
  const escapeHtml = (value: string) => value.replace(/[&<>"']/g, character => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
  }[character] || character))
  openDrawer(
    isZh.value ? '蜂巢运行脉搏 · 实时诊断' : 'OmniHive Runtime Pulse · Live Diagnostics',
    `<p class="mb-4 text-xs opacity-60">${isZh.value ? '当前总览快照；平均延迟沿用总览统计范围，实时链路状态请进入运维监控。' : 'Current overview snapshot; average latency follows the overview window. Open Ops Monitor for live link telemetry.'}</p>
    <dl class="space-y-3">${metrics.map(([label, value]) => `<div class="flex items-center justify-between gap-4 border-b pb-2" style="border-color:var(--border)"><dt>${label}</dt><dd class="font-mono font-semibold tabular-nums">${escapeHtml(value)}</dd></div>`).join('')}</dl>`,
    `<div class="flex flex-wrap gap-2"><button type="button" class="btn btn-primary btn-sm" data-drawer-nav="/admin/ops">${isZh.value ? '运维实时监控' : 'Ops Monitor'}</button><button type="button" class="btn btn-secondary btn-sm" data-drawer-nav="/admin/accounts">${isZh.value ? '上游连接' : 'Upstream Connections'}</button><button type="button" class="btn btn-secondary btn-sm" data-drawer-nav="/admin/channels/monitor">${isZh.value ? '渠道监控' : 'Channel Monitor'}</button></div>`
  )
}
const rankingError = ref(false)
const rankingUnknown = ref(false)

// Chart data
const trendData = ref<TrendDataPoint[]>([])
const modelStats = ref<ModelStat[]>([])
const userTrend = ref<UserUsageTrendPoint[]>([])
const rankingItems = ref<UserSpendingRankingItem[]>([])
const rankingTotalActualCost = ref(0)
const rankingTotalRequests = ref(0)
const rankingTotalTokens = ref(0)
let chartLoadSeq = 0
let usersTrendLoadSeq = 0
let rankingLoadSeq = 0
const rankingLimit = 12

// Helper function to format date in local timezone
const formatLocalDate = (date: Date): string => formatReportDateInput(date, reportTimeZone)

const getLast24HoursRangeDates = (): { start: string; end: string } => {
  const end = new Date()
  const start = new Date(end.getTime() - 24 * 60 * 60 * 1000)
  return {
    start: formatLocalDate(start),
    end: formatLocalDate(end)
  }
}

// Date range
const granularity = ref<'day' | 'hour'>('hour')
const defaultRange = getLast24HoursRangeDates()
const startDate = ref(defaultRange.start)
const endDate = ref(defaultRange.end)

// Granularity options for Select component
const granularityOptions = computed(() => [
  { value: 'day', label: t('admin.dashboard.day') },
  { value: 'hour', label: t('admin.dashboard.hour') }
])

// Dark mode detection
const isDarkMode = computed(() => {
  return document.documentElement.classList.contains('dark')
})

// Chart colors
const chartColors = computed(() => ({
  text: isDarkMode.value ? '#e5e7eb' : '#374151',
  grid: isDarkMode.value ? '#374151' : '#e5e7eb'
}))

// Line chart options (for user trend chart)
const lineOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: {
    intersect: false,
    mode: 'index' as const
  },
  plugins: {
    legend: {
      position: 'top' as const,
      labels: {
        color: chartColors.value.text,
        usePointStyle: true,
        pointStyle: 'circle',
        padding: 15,
        font: {
          size: 11
        }
      }
    },
    tooltip: {
      itemSort: (a: any, b: any) => {
        const aValue = typeof a?.raw === 'number' ? a.raw : Number(a?.parsed?.y ?? 0)
        const bValue = typeof b?.raw === 'number' ? b.raw : Number(b?.parsed?.y ?? 0)
        return bValue - aValue
      },
      callbacks: {
        label: (context: any) => {
          return `${context.dataset.label}: ${formatTokens(context.raw)}`
        }
      }
    }
  },
  scales: {
    x: {
      grid: {
        color: chartColors.value.grid
      },
      ticks: {
        color: chartColors.value.text,
        font: {
          size: 10
        }
      }
    },
    y: {
      grid: {
        color: chartColors.value.grid
      },
      ticks: {
        color: chartColors.value.text,
        font: {
          size: 10
        },
        callback: (value: string | number) => formatTokens(Number(value))
      }
    }
  }
}))

// User trend chart data
const userTrendChartData = computed(() => {
  if (!userTrend.value?.length) return null

  const getDisplayName = (point: UserUsageTrendPoint): string => {
    const username = point.username?.trim()
    if (username) {
      return username
    }

    const email = point.email?.trim()
    if (email) {
      return email
    }

    return t('admin.redeem.userPrefix', { id: point.user_id })
  }

  // Group by user_id to avoid merging different users with the same display name
  const userGroups = new Map<number, { name: string; data: Map<string, number> }>()
  const allDates = new Set<string>()

  userTrend.value.forEach((point) => {
    allDates.add(point.date)
    const key = point.user_id
    if (!userGroups.has(key)) {
      userGroups.set(key, { name: getDisplayName(point), data: new Map() })
    }
    userGroups.get(key)!.data.set(point.date, point.tokens)
  })

  const sortedDates = Array.from(allDates).sort()
  const colors = [
    '#3b82f6',
    '#10b981',
    '#f59e0b',
    '#ef4444',
    '#8b5cf6',
    '#ec4899',
    '#14b8a6',
    '#f97316',
    '#6366f1',
    '#84cc16',
    '#06b6d4',
    '#a855f7'
  ]

  const datasets = Array.from(userGroups.values()).map((group, idx) => ({
    label: group.name,
    data: sortedDates.map((date) => group.data.get(date) ?? NaN),
    borderColor: colors[idx % colors.length],
    backgroundColor: `${colors[idx % colors.length]}20`,
    fill: false,
    tension: 0.3
  }))

  return {
    labels: sortedDates,
    datasets
  }
})

// Format helpers
const formatTokens = (value: number | undefined): string => {
  if (value === undefined || value === null) return '0'
  if (value >= 1_000_000_000) {
    return `${(value / 1_000_000_000).toFixed(2)}B`
  } else if (value >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(2)}M`
  } else if (value >= 1_000) {
    return `${(value / 1_000).toFixed(2)}K`
  }
  return value.toLocaleString()
}

const toFiniteNumber = (value: unknown): number => {
  const numberValue = Number(value)
  return Number.isFinite(numberValue) ? numberValue : 0
}

const formatNumber = (value: number | null | undefined): string => {
  return toFiniteNumber(value).toLocaleString()
}

// 账号成本缺失时明确显示不可用：降级概览(overview fallback)不返回该字段，
// 直接走 formatCost 会把未知值渲染成 0.0000，等于低估账号成本。
const formatAccountCost = (value: number | null | undefined): string =>
  isKnownAmount(value) ? formatCost(value) : t('common.notAvailable')

const formatCost = (value: number | null | undefined): string => {
  const safeValue = toFiniteNumber(value)
  if (safeValue >= 1000) {
    return (safeValue / 1000).toFixed(2) + 'K'
  } else if (safeValue >= 1) {
    return safeValue.toFixed(2)
  } else if (safeValue >= 0.01) {
    return safeValue.toFixed(3)
  }
  return safeValue.toFixed(4)
}

const formatDuration = (ms: number): string => {
  if (ms >= 1000) {
    return `${(ms / 1000).toFixed(2)}s`
  }
  return `${Math.round(ms)}ms`
}

const goToUserUsage = (item: UserSpendingRankingItem) => {
  void router.push({
    path: '/admin/usage',
    query: {
      user_id: String(item.user_id),
      ...reportQueryContext, start_date: startDate.value,
      end_date: endDate.value
    }
  })
}

// Date range change handler
const onDateRangeChange = (range: {
  startDate: string
  endDate: string
  preset: string | null
}) => {
  // Auto-select granularity based on date range
  const start = new Date(range.startDate)
  const end = new Date(range.endDate)
  const daysDiff = Math.ceil((end.getTime() - start.getTime()) / (1000 * 60 * 60 * 24))

  // If range is 1 day, use hourly granularity
  if (daysDiff <= 1) {
    granularity.value = 'hour'
  } else {
    granularity.value = 'day'
  }

  loadChartData()
}

// Load data
const loadDashboardSnapshot = async (includeStats: boolean) => {
  const currentSeq = ++chartLoadSeq
  if (includeStats && !stats.value) {
    loading.value = true
  }
  chartsLoading.value = true
  chartError.value = false
  historyGap.value = null
  try {
    const response = await adminAPI.dashboard.getSnapshotV2({
      ...reportQueryContext, start_date: startDate.value,
      end_date: endDate.value,
      granularity: granularity.value,
      include_stats: includeStats,
      include_trend: true,
      include_model_stats: true,
      include_group_stats: false,
      include_users_trend: false
    })
    if (currentSeq !== chartLoadSeq) return
    if (includeStats && response.stats) {
      stats.value = response.stats
    }
    historyGap.value = null
    trendData.value = withTrendGaps(response.trend || [], granularity.value)
    modelStats.value = response.models || []
  } catch (error) {
    if (currentSeq !== chartLoadSeq) return
    trendData.value = []
    modelStats.value = []
    if (includeStats) stats.value = null
    historyGap.value = historyGapFromError(error)
    chartError.value = !historyGap.value
    if (!historyGap.value) appStore.showError(t('admin.dashboard.failedToLoad'))
    // A failed snapshot has no partial payload. Fetch the independent overview explicitly.
    if (includeStats && historyGap.value) {
      try {
        const overview = await adminAPI.dashboard.getStats()
        if (currentSeq === chartLoadSeq) stats.value = overview
      } catch (overviewError) {
        if (currentSeq !== chartLoadSeq) return
        chartError.value = !historyGapFromError(overviewError)
        if (chartError.value) appStore.showError(t('admin.dashboard.failedToLoad'))
        console.error('Error loading dashboard overview:', overviewError)
      }
    }
    console.error('Error loading dashboard snapshot:', error)
  } finally {
    if (currentSeq === chartLoadSeq) {
      loading.value = false
      chartsLoading.value = false
    }
  }
}

const loadUsersTrend = async () => {
  const currentSeq = ++usersTrendLoadSeq
  userTrendLoading.value = true
  userTrendState.value = 'known'
  try {
    const response = await adminAPI.dashboard.getUserUsageTrend({
      ...reportQueryContext, start_date: startDate.value,
      end_date: endDate.value,
      granularity: granularity.value,
      limit: 12
    })
    if (currentSeq !== usersTrendLoadSeq) return
    userTrend.value = response.trend || []
  } catch (error) {
    if (currentSeq !== usersTrendLoadSeq) return
    userTrendState.value = historyGapFromError(error) ? 'unknown' : 'error'
    console.error('Error loading users trend:', error)
    userTrend.value = []
  } finally {
    if (currentSeq === usersTrendLoadSeq) {
      userTrendLoading.value = false
    }
  }
}

const loadUserSpendingRanking = async () => {
  const currentSeq = ++rankingLoadSeq
  rankingLoading.value = true
  rankingError.value = false
  rankingUnknown.value = false
  try {
    const response = await adminAPI.dashboard.getUserSpendingRanking({
      ...reportQueryContext, start_date: startDate.value,
      end_date: endDate.value,
      limit: rankingLimit
    })
    if (currentSeq !== rankingLoadSeq) return
    rankingItems.value = response.ranking || []
    rankingTotalActualCost.value = response.total_actual_cost || 0
    rankingTotalRequests.value = response.total_requests || 0
    rankingTotalTokens.value = response.total_tokens || 0
  } catch (error) {
    if (currentSeq !== rankingLoadSeq) return
    console.error('Error loading user spending ranking:', error)
    rankingItems.value = []
    rankingTotalActualCost.value = 0
    rankingTotalRequests.value = 0
    rankingTotalTokens.value = 0
    rankingUnknown.value = !!historyGapFromError(error)
    rankingError.value = !rankingUnknown.value
  } finally {
    if (currentSeq === rankingLoadSeq) {
      rankingLoading.value = false
    }
  }
}

const loadDashboardStats = async () => {
  await Promise.all([
    loadDashboardSnapshot(true),
    loadUsersTrend(),
    loadUserSpendingRanking()
  ])
}

const loadChartData = async () => {
  await Promise.all([
    loadDashboardSnapshot(false),
    loadUsersTrend(),
    loadUserSpendingRanking()
  ])
}

async function refreshDashboard() {
  if (dashboardBusy.value) return
  refreshing.value = true
  try {
    await loadDashboardStats()
  } finally {
    refreshing.value = false
    refreshCountdown.value = 30
  }
}

onUnmounted(() => {
  chartLoadSeq++
  usersTrendLoadSeq++
  rankingLoadSeq++
  clearInterval(refreshTimer)
})

watch(() => appStore.activeScope.workspaceId, async (_id, _previous, onCleanup) => {
  const controller = new AbortController()
  onCleanup(() => controller.abort())
  liveModelSampleSize.value = null
  try {
    const workspace = await resolveCatalogWorkspace(appStore)
    if (!workspace || controller.signal.aborted) return
    const page = await listAdminCatalogModels({ workspace_id: workspace, availability: 'live', limit: 1 }, controller.signal)
    if (!controller.signal.aborted) liveModelSampleSize.value = page.items.length
  } catch {
    // Unknown availability keeps the setup checklist visible.
  }
}, { immediate: true })

onMounted(() => {
  void refreshBatchImageAccess()
  loadDashboardStats()
  refreshTimer = setInterval(() => {
    if (!autoRefreshEnabled.value || document.hidden || dashboardBusy.value) return
    refreshCountdown.value = Math.max(0, refreshCountdown.value - 1)
    if (refreshCountdown.value === 0) void refreshDashboard()
  }, 1000)
})
</script>

<style scoped>
.refresh-switch {
  border-color: var(--border);
  background: var(--card);
  accent-color: var(--brand);
}

.pulse-banner:focus-visible {
  outline: 2px solid var(--brand);
  outline-offset: 3px;
}

.pulse-banner:hover {
  box-shadow: inset 3px 0 var(--brand);
}

.kpi-card {
  position: relative;
  overflow: hidden;
  cursor: pointer;
  transition:
    transform 0.2s var(--ease, cubic-bezier(0.2, 0.8, 0.2, 1)),
    box-shadow 0.2s var(--ease, cubic-bezier(0.2, 0.8, 0.2, 1)),
    border-color 0.2s var(--ease, cubic-bezier(0.2, 0.8, 0.2, 1));
}

.kpi-card::before {
  content: '';
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 3px;
  background: var(--kpi-tone, var(--brand, #f59e0b));
}

.kpi-card:hover {
  transform: translateY(-2px);
  border-color: color-mix(in srgb, var(--brand, #f59e0b) 42%, var(--border, #e5e7eb));
}

.kpi-tone-brand {
  --kpi-tone: var(--brand, #f59e0b);
}

.kpi-tone-ok {
  --kpi-tone: var(--ok, #16a34a);
}

.kpi-tone-warn {
  --kpi-tone: var(--warn, #d97706);
}

.kpi-tone-info {
  --kpi-tone: var(--info, #2563eb);
}

@media (prefers-reduced-motion: reduce) {
  .pulse-dot {
    animation: none;
  }

  .kpi-card,
  .kpi-card:hover {
    transition: none;
    transform: none;
  }
}
</style>
