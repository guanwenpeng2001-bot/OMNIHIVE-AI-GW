<template>
  <AppLayout>
    <div
      v-if="blockedReason"
      role="note"
      data-test="channel-telemetry-blocked"
      class="card mb-4 w-full px-4 py-3 text-sm text-[var(--warn-text)]"
    >
      {{ blockedMessage }}
    </div>
    <p v-else role="note" class="card mb-4 w-full px-4 py-3 text-sm text-[var(--muted-foreground)]">{{ t('channelTelemetry.cursorProbeUnsupported') }}</p>
    <div class="w-full min-w-0 space-y-4 pb-8">
      <!-- Ops-style elevated shell: title toolbar + filters (mirrors OpsDashboardHeader) -->
      <section
        class="card monitor-surface rounded-3xl p-0 ring-1 ring-gray-900/5"
      >
        <header class="page-header mb-0 flex flex-wrap items-start justify-between gap-4 border-b border-[var(--border)] px-5 py-4 sm:px-6">
          <div class="min-w-0">
            <h1 class="page-title flex items-center gap-2 text-xl font-black text-[var(--foreground)]">
              <span class="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-[var(--border)] bg-[var(--surface-2)] text-[var(--foreground)]">
                <Icon name="chart" size="sm" />
              </span>
              {{ t('channelTelemetry.title') }}
            </h1>
            <div class="page-description mt-1.5 flex flex-wrap items-center gap-2 text-xs text-[var(--muted-foreground)]">
              <span class="relative flex h-2 w-2 shrink-0">
                <span
                  class="pulse-dot relative inline-flex h-2 w-2 motion-reduce:!animate-none"
                  :class="loading || refreshing ? 'bg-[var(--muted-foreground)]' : 'pulse-ok'"
                ></span>
              </span>
              <span v-if="refreshing" class="inline-flex items-center gap-1 text-primary-600 dark:text-primary-300">
                <LoadingSpinner size="sm" />
                {{ t('channelTelemetry.updating') }}
              </span>
              <span v-else-if="snapshot?.coverage.data_through">
                {{ t('channelTelemetry.updatedTo', { time: formatTime(snapshot.coverage.data_through) }) }}
              </span>
              <span v-else class="text-[var(--muted-foreground)]">{{ t('common.loading') }}</span>
              <span
                v-if="snapshot && !snapshot.coverage.coverage_complete && !bootstrapActive"
                class="badge badge-warning"
              >
                {{ t('channelTelemetry.partialCoverage') }}
              </span>
              <span
                v-if="bootstrapActive"
                class="badge badge-primary inline-flex items-center gap-1"
              >
                <LoadingSpinner size="sm" />
                {{ t('channelTelemetry.bootstrap.progress', { percent: bootstrapPercent }) }}
              </span>
            </div>
          </div>
          <button
            class="btn btn-secondary btn-icon flex h-8 w-8 items-center justify-center rounded-lg bg-[var(--secondary)] text-[var(--muted-foreground)] hover:bg-[var(--surface-2)]"
            type="button"
            :title="t('common.refresh')"
            :disabled="loading"
            @click="reload(false)"
          >
            <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
          </button>
        </header>

        <!-- First-upgrade silent backfill: show until 30d product window is covered -->
        <div
          v-if="bootstrapActive"
          class="border-b border-blue-100 bg-blue-50/90 px-5 py-3 dark:border-blue-900/40 dark:bg-blue-950/40 sm:px-6"
          role="status"
          aria-live="polite"
        >
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div class="min-w-0 flex-1">
              <p class="text-sm font-semibold text-blue-900 dark:text-blue-100">
                {{ t('channelTelemetry.bootstrap.title') }}
              </p>
              <p class="mt-0.5 text-xs text-blue-800/80 dark:text-blue-200/80">
                {{ t('channelTelemetry.bootstrap.description') }}
              </p>
            </div>
            <span class="shrink-0 text-xs font-medium tabular-nums text-blue-700 dark:text-blue-300">
              {{ t('channelTelemetry.bootstrap.progress', { percent: bootstrapPercent }) }}
            </span>
          </div>
          <div
            class="mt-2.5 h-1.5 overflow-hidden rounded-full bg-blue-200/80 dark:bg-blue-900/60"
            role="progressbar"
            :aria-valuenow="bootstrapPercent"
            aria-valuemin="0"
            aria-valuemax="100"
            :aria-label="t('channelTelemetry.bootstrap.working')"
          >
            <div
              class="h-full rounded-full bg-blue-500 transition-[width] duration-500 ease-out dark:bg-blue-400"
              :style="{ width: `${bootstrapPercent}%` }"
            />
          </div>
        </div>

        <!-- Single compact toolbar row: range · filters · view controls -->
        <div class="monitor-toolbar flex flex-nowrap items-center gap-1.5 overflow-x-auto px-4 py-3 sm:gap-2 sm:px-5">
          <div
            class="tabs segmented inline-flex shrink-0"
            role="group"
            :aria-label="t('channelTelemetry.timeRange')"
          >
            <button
              v-for="option in ranges"
              :key="option.value"
              type="button"
              class="tab seg-btn !px-2 !py-1 text-xs sm:!px-2.5"
              :class="filter.range === option.value ? 'tab-active seg-active' : ''"
              @click="setRange(option.value)"
            >
              {{ option.label }}
            </button>
          </div>

          <span class="mx-0.5 hidden h-5 w-px shrink-0 bg-[var(--border)] sm:block" aria-hidden="true"></span>

          <FilterMultiSelect
            v-model="filter.platforms"
            compact
            :label="t('channelTelemetry.filters.platform')"
            :all-label="t('channelTelemetry.filters.allPlatforms')"
            :options="platformOptions"
          />
          <FilterMultiSelect
            v-model="selectedGroupIds"
            compact
            :label="t('channelTelemetry.filters.group')"
            :all-label="t('channelTelemetry.filters.allGroups')"
            :options="groupOptions"
          />
          <FilterMultiSelect
            v-model="filter.models"
            compact
            :label="t('channelTelemetry.filters.model')"
            :all-label="t('channelTelemetry.filters.allModels')"
            :options="modelOptions"
          />
          <button
            type="button"
            class="btn btn-ghost btn-sm shrink-0 !px-2 !py-1 text-xs"
            :disabled="!hasDimensionFilter"
            :class="!hasDimensionFilter ? 'opacity-40' : ''"
            @click="clearDimensions"
          >
            {{ t('channelTelemetry.clearFilters') }}
          </button>

          <span class="mx-0.5 hidden h-5 w-px shrink-0 bg-[var(--border)] md:block" aria-hidden="true"></span>

          <Select
            v-model="matrixGroupBy"
            :options="matrixGroupOptions"
            :placeholder="t('channelTelemetry.groupBy.label')"
            class="monitor-toolbar-select w-[7.5rem] shrink-0 sm:w-[8.5rem]"
          />

          <div
            class="tabs segmented inline-flex shrink-0"
            role="group"
            :aria-label="t('channelTelemetry.trendView.label')"
          >
            <button
              type="button"
              class="tab seg-btn !px-2 !py-1 text-xs"
              :class="trendView === 'pulse' ? 'tab-active seg-active' : ''"
              @click="trendView = 'pulse'"
            >
              {{ t('channelTelemetry.trendView.pulse') }}
            </button>
            <button
              type="button"
              class="tab seg-btn !px-2 !py-1 text-xs"
              :class="trendView === 'line' ? 'tab-active seg-active' : ''"
              @click="trendView = 'line'"
            >
              {{ t('channelTelemetry.trendView.line') }}
            </button>
          </div>

          <div
            v-if="trendView === 'pulse'"
            class="tabs segmented inline-flex shrink-0"
            role="group"
            :aria-label="t('channelTelemetry.healthMode.label')"
          >
            <button
              v-for="option in healthModeOptions"
              :key="option.value"
              type="button"
              class="tab seg-btn !px-2 !py-1 text-xs"
              :class="healthMode === option.value ? 'tab-active seg-active' : ''"
              @click="healthMode = option.value"
            >
              {{ option.label }}
            </button>
          </div>
        </div>
      </section>

      <!-- Overview KPI: success · TTFT · tokens/s(optional) · cache · (+ RPM when throughput visible) -->
      <section
        v-if="snapshot"
        class="grid grid-cols-2 gap-3 sm:grid-cols-3"
        :class="showThroughput ? 'xl:grid-cols-5' : 'xl:grid-cols-4'"
        :aria-label="t('channelTelemetry.summaryAria')"
      >
        <MetricCell
          :label="t('channelTelemetry.metrics.successRate')"
          :value="successRateValue(snapshot.metrics)"
          :detail="successRateDetail(snapshot.metrics)"
          :state="snapshot.health.error_rate"
        />
        <MetricCell
          :label="t('channelTelemetry.metrics.ttftP50')"
          :value="formatMs(snapshot.metrics.ttft.p50_ms)"
          :detail="latencyKpiSecondary(snapshot.metrics.ttft)"
          :title="latencyDetail(snapshot.metrics.ttft)"
          :state="ttftCellState(snapshot.health.ttft, snapshot.metrics.ttft)"
        />
        <MetricCell
          v-if="showThroughput"
          :label="t('channelTelemetry.metrics.tps')"
          :value="formatTps(snapshot.metrics.tpm)"
          :detail="tpsDetail(snapshot.metrics)"
          :title="exactTps(snapshot.metrics.tpm)"
        />
        <MetricCell
          :label="t('channelTelemetry.metrics.cacheRate')"
          :value="formatPercent(snapshot.metrics.cache_rate)"
          :detail="t('channelTelemetry.metrics.cacheDetail')"
          :state="snapshot.health.cache || snapshot.health.overall"
        />
        <MetricCell
          v-if="showThroughput"
          :label="t('channelTelemetry.metrics.rpm')"
          :value="formatRate(snapshot.metrics.rpm)"
          :detail="t('channelTelemetry.metrics.rpmDetail')"
          :title="exactRate(snapshot.metrics.rpm)"
        />
      </section>
      <section
        v-else-if="loading"
        class="grid grid-cols-2 gap-3 sm:grid-cols-3"
        :class="showThroughput ? 'xl:grid-cols-5' : 'xl:grid-cols-4'"
        aria-hidden="true"
      >
        <div
          v-for="i in (showThroughput ? 5 : 4)"
          :key="i"
          class="h-24 animate-pulse rounded-2xl border border-[var(--border)] bg-[var(--surface-2)] motion-reduce:animate-none"
        />
      </section>

      <div class="relative min-h-[320px]">
        <MonitorTrendChart
          v-if="trendView === 'line'"
          :trend="snapshot?.trend || []"
          :coverage="snapshot?.coverage || null"
          :loading="loading && !snapshot"
        />
        <RelayPulseMatrix
          v-else-if="matrix"
          :rows="matrixRows"
          :coverage="matrix.coverage"
          :health-mode="healthMode"
          :show-throughput="showThroughput"
        />
        <div
          v-else-if="loading"
          class="card monitor-surface flex min-h-[320px] items-center justify-center rounded-3xl text-sm text-[var(--muted-foreground)] ring-1 ring-gray-900/5"
        >
          <span class="animate-pulse">{{ t('common.loading') }}</span>
        </div>
      </div>

      <section class="card monitor-surface flex min-h-0 flex-col overflow-hidden rounded-3xl ring-1 ring-gray-900/5">
        <div class="border-b border-[var(--border)] px-5 pt-4 sm:px-6">
          <nav class="tabs w-full max-w-md sm:w-auto" role="tablist" :aria-label="t('channelTelemetry.tabs.aria')">
            <button
              v-for="item in tabs"
              :key="item.value"
              type="button"
              role="tab"
              class="tab flex-1 sm:flex-none"
              :aria-selected="activeTab === item.value"
              :class="activeTab === item.value ? 'tab-active' : ''"
              @click="activeTab = item.value"
            >
              {{ item.label }}
            </button>
          </nav>
        </div>
        <div class="min-h-0 max-h-[min(52vh,520px)] overflow-auto p-4 sm:p-5">
          <div v-if="activeTab === 'models'" class="table-container border-0">
            <table class="table monitor-table min-w-[720px]">
              <thead>
                <tr>
                  <th>{{ t('channelTelemetry.table.platformModel') }}</th>
                  <th>{{ t('channelTelemetry.metrics.successRate') }}</th>
                  <th>{{ t('channelTelemetry.metrics.ttftP50') }}</th>
                  <th v-if="showThroughput">{{ t('channelTelemetry.metrics.tps') }}</th>
                  <th>{{ t('channelTelemetry.metrics.cacheRate') }}</th>
                  <th v-if="showThroughput">{{ t('channelTelemetry.metrics.rpm') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="row in modelRows"
                  :key="`${row.platform}:${row.model}`"
                  class="cursor-pointer"
                  @click="drillModel(row)"
                >
                  <td>
                    <div class="flex items-center gap-2">
                      <span :class="statusDot(row.health)" aria-hidden="true"></span>
                      <div>
                        <span :title="row.platform" class="block text-xs text-[var(--muted-foreground)]">{{ getPlatformLabel(row.platform, t) }}</span>
                        <strong class="font-semibold text-[var(--foreground)]">
                          {{ row.model === '__other__' ? t('channelTelemetry.otherModels') : row.model }}
                        </strong>
                      </div>
                    </div>
                  </td>
                  <td>
                    <span class="block">{{ successRateValue(row.metrics) }}</span>
                    <small class="text-xs text-[var(--muted-foreground)]">{{ successRateDetail(row.metrics) }}</small>
                  </td>
                  <td>
                    <span class="block">{{ formatMs(row.metrics.ttft.p50_ms) }}</span>
                    <small class="text-xs text-[var(--muted-foreground)]">{{ latencyDetail(row.metrics.ttft) }}</small>
                  </td>
                  <td v-if="showThroughput" :title="exactTps(row.metrics.tpm)">{{ formatTps(row.metrics.tpm) }}</td>
                  <td>{{ formatPercent(row.metrics.cache_rate) }}</td>
                  <td v-if="showThroughput">{{ formatRate(row.metrics.rpm) }}</td>
                </tr>
              </tbody>
            </table>
          </div>

          <div v-else-if="activeTab === 'errors'" class="space-y-3">
            <div
              v-for="row in errorRows"
              :key="row.category"
              class="rounded-2xl border border-[var(--border)] bg-[var(--surface-2)] p-4 text-sm"
              :class="row.ignored ? 'opacity-60' : ''"
            >
              <button
                type="button"
                class="grid w-full grid-cols-[minmax(100px,200px)_1fr_auto_auto] items-center gap-3 text-left"
                @click="toggleError(row.category)"
              >
                <span class="flex min-w-0 items-center gap-1.5 truncate text-[var(--foreground)]">
                  <span class="truncate">{{ errorLabel(row.category) }}</span>
                  <span v-if="row.ignored" class="badge badge-gray shrink-0 !px-1.5 !py-0 text-[10px]">{{ t('channelTelemetry.ignored') }}</span>
                </span>
                <span class="h-2 overflow-hidden rounded-full bg-[var(--secondary)]">
                  <i
                    class="block h-full rounded-full"
                    :class="row.ignored ? 'bg-[var(--muted-foreground)]' : 'bg-gradient-to-r from-red-400 to-red-500'"
                    :style="{ width: `${Math.max(2, row.rate * 100)}%` }"
                  ></i>
                </span>
                <small
                  class="w-14 text-right text-xs tabular-nums"
                  :class="row.ignored ? 'text-[var(--muted-foreground)]' : 'text-[var(--foreground)]'"
                >{{ formatPercent(row.rate) }}</small>
                <Icon name="chevronDown" size="sm" :class="['text-[var(--muted-foreground)] transition-transform', expandedErrors.has(row.category) ? 'rotate-180' : '']" />
              </button>
              <div v-if="expandedErrors.has(row.category)" class="mt-3 space-y-2 border-t border-[var(--border)] pt-3">
                <template v-if="isAdmin && (row.details || []).length">
                  <div
                    v-for="(detail, index) in row.details || []"
                    :key="`${row.category}:${index}:${detail.message}`"
                    class="rounded-lg border border-[var(--border)] bg-[var(--card)] px-3 py-2 text-xs text-[var(--foreground)]"
                  >
                    <div class="mb-1 flex flex-wrap items-center gap-2">
                      <span class="badge badge-gray !px-1.5 !py-0 text-[10px]">{{ detail.platform || '-' }}</span>
                      <span class="truncate font-medium">{{ detail.model || '-' }}</span>
                      <span v-if="detail.status_code" class="text-[var(--muted-foreground)]">{{ t('channelTelemetry.errorDetail.http', { code: detail.status_code }) }}</span>
                      <span v-if="detail.upstream_status_code" class="text-[var(--muted-foreground)]">{{ t('channelTelemetry.errorDetail.upstream', { code: detail.upstream_status_code }) }}</span>
                      <span class="ml-auto text-[var(--muted-foreground)]">×{{ detail.count }}</span>
                    </div>
                    <p class="break-words leading-relaxed">{{ detail.message || detail.error_type || t('channelTelemetry.errorDetail.noMessage') }}</p>
                  </div>
                </template>
                <p v-else class="text-xs text-[var(--muted-foreground)]">{{ t('channelTelemetry.errorDetail.empty') }}</p>
              </div>
            </div>
          </div>

          <div v-else class="table-container border-0">
            <table class="table monitor-table min-w-[640px]">
              <thead>
                <tr>
                  <th class="w-16">{{ t('channelTelemetry.table.rank') }}</th>
                  <th>{{ t('channelTelemetry.table.user') }}</th>
                  <th>{{ t('channelTelemetry.metrics.successRate') }}</th>
                  <th>{{ t('channelTelemetry.metrics.ttftP50') }}</th>
                  <th v-if="showThroughput">{{ t('channelTelemetry.metrics.tps') }}</th>
                  <th>{{ t('channelTelemetry.metrics.cacheRate') }}</th>
                  <th v-if="showThroughput">{{ t('channelTelemetry.metrics.rpm') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="row in userRows"
                  :key="row.user_id || row.display_label"
                  :class="row.is_self
                    ? 'bg-primary-50 ring-1 ring-inset ring-primary-200/80 dark:bg-primary-900/25 dark:ring-primary-700/50'
                    : ''"
                >
                  <td><MonitorRankBadge :rank="row.rank" /></td>
                  <td>
                    <strong
                      class="font-semibold"
                      :class="row.is_self ? 'text-[var(--brand-text)]' : 'text-[var(--foreground)]'"
                    >
                      {{ row.display_label }}
                      <span
                        v-if="row.is_self"
                        class="badge badge-primary ml-2 !px-1.5 !py-0 text-[10px]"
                      >{{ t('channelTelemetry.currentUser') }}</span>
                    </strong>
                  </td>
                  <td>
                    <span class="block">{{ successRateValue(row.metrics) }}</span>
                    <small class="text-xs text-[var(--muted-foreground)]">{{ successRateDetail(row.metrics) }}</small>
                  </td>
                  <td>
                    <span class="block">{{ formatMs(row.metrics.ttft.p50_ms) }}</span>
                    <small class="text-xs text-[var(--muted-foreground)]">{{ latencyDetail(row.metrics.ttft) }}</small>
                  </td>
                  <td v-if="showThroughput" :title="exactTps(row.metrics.tpm)">{{ formatTps(row.metrics.tpm) }}</td>
                  <td>{{ formatPercent(row.metrics.cache_rate) }}</td>
                  <td v-if="showThroughput">{{ formatRate(row.metrics.rpm) }}</td>
                </tr>
              </tbody>
            </table>
          </div>

          <div v-if="tabLoading" class="empty-state py-10 text-sm text-[var(--muted-foreground)]">{{ t('common.loading') }}</div>
          <div v-else-if="activeRowsEmpty" class="empty-state py-10">
            <p class="empty-state-title text-base">
              {{
                bootstrapActive
                  ? t('channelTelemetry.bootstrap.title')
                  : t('channelTelemetry.empty.title')
              }}
            </p>
            <p class="empty-state-description">
              {{
                bootstrapActive
                  ? t('channelTelemetry.bootstrap.description')
                  : t('channelTelemetry.empty.description')
              }}
            </p>
          </div>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { getPlatformLabel } from '@/constants/platforms'
import { useI18n } from 'vue-i18n'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Select from '@/components/common/Select.vue'
import FilterMultiSelect from '@/features/channel-telemetry/FilterMultiSelect.vue'
import MetricCell from '@/features/channel-telemetry/MetricCell.vue'
import MonitorRankBadge from '@/features/channel-telemetry/MonitorRankBadge.vue'
import MonitorTrendChart from '@/features/channel-telemetry/MonitorTrendChart.vue'
import RelayPulseMatrix from '@/features/channel-telemetry/RelayPulseMatrix.vue'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage, extractApiErrorCode } from '@/utils/apiError'
import { isChannelMonitorThroughputHidden, isChannelMonitorUserRankingHidden } from '@/utils/featureFlags'
import * as api from '@/api/channelTelemetry'
import type {
  HealthState,
  MonitorDimensions,
  MonitorErrorRow,
  MonitorFilter,
  MonitorHealth,
  MonitorMatrixGroupBy,
  MonitorMatrixResponse,
  MonitorModelRow,
  MonitorRange,
  MonitorSnapshot,
  MonitorMetric,
  MonitorUserRow,
} from '@/api/channelTelemetry'
import {
  formatLatencyKpiSecondary,
  formatLatencyPrivacy,
  formatMonitorMs,
  formatMonitorPercent,
  formatMonitorSuccessRateFromMetric,
  formatMonitorThroughput,
  formatMonitorTokensPerSecond,
  hasMonitorTokenSplit,
  monitorTokenSplitParts,
  tokensPerSecondFromTpm,
  healthScoreClass,
  monitorErrorCategoryLabel,
  ttftDisplayState,
} from '@/features/channel-telemetry/monitorFormat'

type Tab = 'models' | 'errors' | 'users'
type HealthMode = 'overall' | 'success' | 'ttft' | 'cache'
type TrendView = 'pulse' | 'line'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const appStore = useAppStore()
const { t, te, locale } = useI18n()
const isAdmin = computed(() => authStore.isAdmin)
/** 后端规范机器码命中时展示真实原因（功能关闭 / 模式不符），而不是空页 + toast。 */
const blockedReason = ref<'disabled' | 'modeMismatch' | null>(null)
const blockedMessage = computed(() =>
  blockedReason.value === 'modeMismatch'
    ? t('channelTelemetry.blocked.modeMismatch')
    : t('channelTelemetry.blocked.disabled'),
)
/** Admins always see RPM/TPM; users honor the hide-throughput system setting. */
const showThroughput = computed(() => isAdmin.value || !isChannelMonitorThroughputHidden())
/** Admins always see ranking; users honor the hide-user-ranking system setting. */
const showUserRanking = computed(() => isAdmin.value || !isChannelMonitorUserRankingHidden())

const ranges = computed(() => [
  { value: '90m' as MonitorRange, label: t('channelTelemetry.ranges.90m') },
  { value: '24h' as MonitorRange, label: t('channelTelemetry.ranges.24h') },
  { value: '7d' as MonitorRange, label: t('channelTelemetry.ranges.7d') },
  { value: '30d' as MonitorRange, label: t('channelTelemetry.ranges.30d') },
])
const tabs = computed(() => {
  const items: Array<{ value: Tab; label: string }> = [
    { value: 'models', label: t('channelTelemetry.tabs.models') },
    { value: 'errors', label: t('channelTelemetry.tabs.errors') },
  ]
  if (showUserRanking.value) {
    items.push({ value: 'users', label: t('channelTelemetry.tabs.users') })
  }
  return items
})
const matrixGroupOptions = computed(() => [
  { value: 'platform' as MonitorMatrixGroupBy, label: t('channelTelemetry.groupBy.platform') },
  { value: 'platform_group' as MonitorMatrixGroupBy, label: t('channelTelemetry.groupBy.platformGroup') },
  { value: 'platform_model' as MonitorMatrixGroupBy, label: t('channelTelemetry.groupBy.platformModel') },
  { value: 'platform_group_model' as MonitorMatrixGroupBy, label: t('channelTelemetry.groupBy.platformGroupModel') },
])
const healthModeOptions = computed(() => [
  { value: 'overall' as HealthMode, label: t('channelTelemetry.healthMode.overall') },
  { value: 'success' as HealthMode, label: t('channelTelemetry.healthMode.success') },
  { value: 'ttft' as HealthMode, label: t('channelTelemetry.healthMode.ttft') },
  { value: 'cache' as HealthMode, label: t('channelTelemetry.healthMode.cache') },
])

const filter = ref<MonitorFilter>({
  range: parseRange(route.query.range),
  platforms: csv(route.query.platform),
  groupIds: csv(route.query.group).map(Number).filter(Boolean),
  models: csv(route.query.model),
})
const activeTab = ref<Tab>(parseTab(route.query.tab, showUserRanking.value))
const matrixGroupBy = ref<MonitorMatrixGroupBy>(parseMatrixGroupBy(route.query.group_by))
const healthMode = ref<HealthMode>(parseHealthMode(route.query.health_mode))
const trendView = ref<TrendView>(parseTrendView(route.query.trend_view))
const dimensions = ref<MonitorDimensions>({ platforms: [], groups: [], models: [] })
const snapshot = ref<MonitorSnapshot | null>(null)
const matrix = ref<MonitorMatrixResponse | null>(null)
const modelRows = ref<MonitorModelRow[]>([])
const errorRows = ref<MonitorErrorRow[]>([])
const userRows = ref<MonitorUserRow[]>([])
const loading = ref(false)
const tabLoading = ref(false)
const refreshing = ref(false)
const expandedErrors = ref(new Set<string>())
let controller: AbortController | null = null
let sequence = 0
let autoRefreshTimer: number | null = null

const hasDimensionFilter = computed(
  () => filter.value.platforms.length + filter.value.groupIds.length + filter.value.models.length > 0
)
// Full platform catalog (never pruned). Groups/models cascade by selected platforms
// so choosing a platform narrows the other pickers without collapsing platforms.
// The API sends the machine id as `label`, so names always come from the shared
// platform name table instead.
const platformOptions = computed(() =>
  (dimensions.value.platforms || []).map((item) => ({
    value: item.value,
    label: getPlatformLabel(item.value, t),
  }))
)
const selectedPlatforms = computed(() => new Set(filter.value.platforms))
const groupOptions = computed(() =>
  (dimensions.value.groups || [])
    .filter(
      (item) =>
        selectedPlatforms.value.size === 0 ||
        !item.platform ||
        selectedPlatforms.value.has(item.platform),
    )
    .map((item) => ({
      value: String(item.id),
      label: item.platform ? `${item.platform} / ${item.name || `#${item.id}`}` : item.name || `#${item.id}`,
    }))
)
const modelOptions = computed(() =>
  (dimensions.value.models || [])
    .filter(
      (item) =>
        selectedPlatforms.value.size === 0 ||
        !item.platform ||
        selectedPlatforms.value.has(item.platform),
    )
    .map((item) => ({
      value: item.value,
      label:
        item.platform && !item.label.includes(item.platform)
          ? `${item.platform} / ${item.label}`
          : item.label,
    }))
)
const selectedGroupIds = computed({
  get: () => filter.value.groupIds.map(String),
  set: (value: string[]) => {
    filter.value.groupIds = value.map(Number).filter((id) => Number.isInteger(id) && id > 0)
  },
})
// Soft-prune group/model selections that fall outside the platform cascade.
// Do NOT wipe when options are temporarily empty (loading); only drop invalid ids.
watch(
  [groupOptions, modelOptions],
  () => {
    if (groupOptions.value.length > 0) {
      const allowed = new Set(groupOptions.value.map((item) => item.value))
      const next = filter.value.groupIds.filter((id) => allowed.has(String(id)))
      if (next.length !== filter.value.groupIds.length) {
        filter.value.groupIds = next
      }
    }
    if (modelOptions.value.length > 0) {
      const allowed = new Set(modelOptions.value.map((item) => item.value))
      const next = filter.value.models.filter((model) => allowed.has(model))
      if (next.length !== filter.value.models.length) {
        filter.value.models = next
      }
    }
  },
  { flush: 'post' },
)
const activeRowsEmpty = computed(() =>
  activeTab.value === 'models'
    ? modelRows.value.length === 0
    : activeTab.value === 'errors'
      ? errorRows.value.length === 0
      : userRows.value.length === 0
)
/** First-upgrade backfill toward 90m/24h/7d/30d; banner hides when backend omits bootstrap. */
const bootstrapActive = computed(() => Boolean(snapshot.value?.coverage?.bootstrap?.active))
const bootstrapPercent = computed(() => {
  const raw = snapshot.value?.coverage?.bootstrap?.progress_percent
  if (typeof raw !== 'number' || Number.isNaN(raw)) return 0
  return Math.min(100, Math.max(0, Math.round(raw)))
})
const matrixRows = computed(() => {
  const items = matrix.value?.items || []
  // platform_group views should only show real groups, never bare platform placeholders.
  if (matrixGroupBy.value === 'platform_group' || matrixGroupBy.value === 'platform_group_model') {
    return items.filter((row) => row.group_id != null && Number(row.group_id) > 0)
  }
  return items
})

function csv(value: unknown) {
  return typeof value === 'string' ? value.split(',').filter(Boolean) : []
}
function parseRange(value: unknown): MonitorRange {
  return ['90m', '24h', '7d', '30d'].includes(String(value)) ? (value as MonitorRange) : '90m'
}
function parseMatrixGroupBy(value: unknown): MonitorMatrixGroupBy {
  const allowed: MonitorMatrixGroupBy[] = [
    'platform',
    'platform_group',
    'platform_model',
    'platform_group_model',
  ]
  return allowed.includes(value as MonitorMatrixGroupBy)
    ? (value as MonitorMatrixGroupBy)
    : 'platform_group'
}
function parseTab(value: unknown, allowUsers: boolean): Tab {
  const allowed: Tab[] = allowUsers ? ['models', 'errors', 'users'] : ['models', 'errors']
  return allowed.includes(value as Tab) ? (value as Tab) : 'models'
}
function parseHealthMode(value: unknown): HealthMode {
  const allowed: HealthMode[] = ['overall', 'success', 'ttft', 'cache']
  return allowed.includes(value as HealthMode) ? (value as HealthMode) : 'overall'
}
function parseTrendView(value: unknown): TrendView {
  return value === 'line' ? 'line' : 'pulse'
}
function syncQuery() {
  void router.replace({
    query: {
      range: filter.value.range,
      platform: filter.value.platforms.join(',') || undefined,
      group: filter.value.groupIds.join(',') || undefined,
      model: filter.value.models.join(',') || undefined,
      group_by: matrixGroupBy.value,
      health_mode: healthMode.value,
      trend_view: trendView.value === 'line' ? 'line' : undefined,
      tab: activeTab.value,
    },
  })
}
/** Dimensions catalog: range only — never re-filtered by platform/group/model selection. */
async function loadDimensions(signal?: AbortSignal, id = sequence) {
  const rangeOnly: MonitorFilter = {
    range: filter.value.range,
    platforms: [],
    groupIds: [],
    models: [],
  }
  const next = await api.getDimensions(rangeOnly, isAdmin.value, signal)
  if (id !== sequence) return
  dimensions.value = next
}

async function loadMetrics(signal?: AbortSignal, id = sequence) {
  const [nextSnapshot, nextMatrix] = await Promise.all([
    api.getSnapshot(filter.value, isAdmin.value, signal),
    api.getMatrix(filter.value, matrixGroupBy.value, isAdmin.value, signal),
  ])
  if (id !== sequence) return
  snapshot.value = nextSnapshot
  matrix.value = nextMatrix
  scheduleAutoRefresh()
  await loadTab(signal, id)
}

async function reload(silent = true) {
  controller?.abort()
  const request = new AbortController()
  controller = request
  const id = ++sequence
  refreshing.value = true
  if (!silent) loading.value = true
  try {
    // Catalog + metrics in parallel; catalog ignores dimension filters so options never shrink.
    await Promise.all([
      loadDimensions(request.signal, id),
      loadMetrics(request.signal, id),
    ])
    if (id === sequence) blockedReason.value = null
  } catch (error) {
    if ((error as { name?: string }).name !== 'CanceledError') {
      const reason = extractApiErrorCode(error)
      if (reason === 'CHANNEL_MONITOR_DISABLED' || reason === 'CHANNEL_MONITOR_MODE_MISMATCH') {
        // 规范机器码：展示真实原因并停止轮询，不重复报同一个配置问题。
        blockedReason.value = reason === 'CHANNEL_MONITOR_MODE_MISMATCH' ? 'modeMismatch' : 'disabled'
        stopAutoRefresh()
      } else {
        appStore.showError(extractApiErrorMessage(error, t('channelTelemetry.loadFailed')))
      }
    }
  } finally {
    if (id === sequence) {
      loading.value = false
      tabLoading.value = false
      refreshing.value = false
    }
  }
}

/** When only range changes, still refresh dimensions; dimension filters only re-load metrics. */
async function reloadMetricsOnly(silent = true) {
  controller?.abort()
  const request = new AbortController()
  controller = request
  const id = ++sequence
  refreshing.value = true
  if (!silent) loading.value = true
  try {
    await loadMetrics(request.signal, id)
  } catch (error) {
    if ((error as { name?: string }).name !== 'CanceledError') {
      appStore.showError(extractApiErrorMessage(error, t('channelTelemetry.loadFailed')))
    }
  } finally {
    if (id === sequence) {
      loading.value = false
      tabLoading.value = false
      refreshing.value = false
    }
  }
}
async function loadTab(signal?: AbortSignal, id = sequence) {
  tabLoading.value = true
  try {
    if (activeTab.value === 'models') {
      modelRows.value = (await api.getModels(filter.value, isAdmin.value, signal)).items || []
    } else if (activeTab.value === 'errors') {
      errorRows.value = (await api.getErrors(filter.value, isAdmin.value, signal)).items || []
    } else if (showUserRanking.value) {
      userRows.value = (await api.getUsers(filter.value, isAdmin.value, signal)).items || []
    } else {
      userRows.value = []
    }
  } catch (error) {
    const e = error as { name?: string; code?: string }
    if (e?.name === 'AbortError' || e?.name === 'CanceledError' || e?.code === 'ERR_CANCELED') return
    appStore.showError(extractApiErrorMessage(error, t('channelTelemetry.detailLoadFailed')))
  } finally {
    if (id === sequence) tabLoading.value = false
  }
}
function setRange(value: MonitorRange) {
  filter.value.range = value
}
function clearDimensions() {
  // Replace arrays so deep watch always fires and metrics reload full window.
  filter.value = {
    ...filter.value,
    platforms: [],
    groupIds: [],
    models: [],
  }
}
function stopAutoRefresh() {
  if (autoRefreshTimer) {
    window.clearInterval(autoRefreshTimer)
    autoRefreshTimer = null
  }
}
function scheduleAutoRefresh() {
  if (autoRefreshTimer) {
    window.clearInterval(autoRefreshTimer)
    autoRefreshTimer = null
  }
  if (blockedReason.value) return
  // Poll faster while first-upgrade bootstrap is filling 90m→30d so the progress bar moves.
  const seconds = bootstrapActive.value
    ? 10
    : snapshot.value?.config?.refresh_interval_seconds || 300
  autoRefreshTimer = window.setInterval(() => {
    if (!loading.value && !refreshing.value) {
      void reload(true)
    }
  }, Math.max(bootstrapActive.value ? 10 : 60, seconds) * 1000)
}
function drillModel(row: MonitorModelRow) {
  filter.value.platforms = [row.platform]
  filter.value.models = [row.model]
}
function formatRate(value: number) {
  return formatMonitorThroughput(value)
}
function exactRate(value: number) {
  return Intl.NumberFormat(locale.value || undefined, { maximumFractionDigits: 2 }).format(value || 0)
}
function formatTps(tpm: number | null | undefined) {
  return formatMonitorTokensPerSecond(tpm)
}

// 成功率优先用后端下发的 success_rate（与 ignored 错误分类共存时 1 - error_rate 不等价）。
function successRateValue(metrics: MonitorMetric) {
  return formatMonitorSuccessRateFromMetric(metrics)
}

// 管理端才展示绝对 token 拆分（用户视图由后端清零，不能当真实拆分展示）。
function successRateDetail(metrics: MonitorMetric) {
  if (typeof metrics.success_rate === 'number' && Number.isFinite(metrics.success_rate)) {
    return t('channelTelemetry.metrics.successRateValue', { value: formatMonitorPercent(metrics.success_rate) })
  }
  return t('channelTelemetry.metrics.errorRateValue', { value: formatMonitorPercent(metrics.error_rate) })
}

function tpsDetail(metrics: MonitorMetric) {
  const base = t('channelTelemetry.metrics.tpsDetail')
  if (!isAdmin.value || !hasMonitorTokenSplit(metrics)) return base
  return `${base} · ${t('channelTelemetry.metrics.tokenSplit', monitorTokenSplitParts(metrics))}`
}
function exactTps(tpm: number | null | undefined) {
  return Intl.NumberFormat(locale.value || undefined, { maximumFractionDigits: 3 }).format(
    tokensPerSecondFromTpm(tpm),
  )
}
function formatPercent(value: number) {
  return formatMonitorPercent(value)
}
function formatMs(value: number | null) {
  return formatMonitorMs(value)
}
function ttftCellState(state: HealthState | undefined, metric: { p50_ms: number | null; sample_count?: number }) {
  return ttftDisplayState(state, metric)
}
function latencyDetail(metric: {
  p50_ms: number | null
  p90_ms?: number | null
  p95_ms: number | null
  avg_ms?: number | null
}) {
  return formatLatencyPrivacy(metric.p50_ms, metric.p90_ms, metric.avg_ms, metric.p95_ms)
}
/** KPI secondary: AVG · P90 under the P50 primary value. */
function latencyKpiSecondary(metric: {
  p90_ms?: number | null
  p95_ms: number | null
  avg_ms?: number | null
}) {
  return formatLatencyKpiSecondary(metric.avg_ms, metric.p90_ms, metric.p95_ms)
}
function formatTime(value: string) {
  return new Intl.DateTimeFormat(locale.value || undefined, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(value))
}
function statusDot(health?: MonitorHealth | HealthState) {
  if (!health || typeof health === 'string') {
    return `status-dot health-${health || 'unknown'}`
  }
  // Prefer multi-band score when available; otherwise fall back to the coarse
  // overall state for mixed-version/older payloads.
  const klass =
    health.score != null
      ? healthScoreClass(health, 'overall', 0)
      : `health-${health.overall || 'unknown'}`
  return `status-dot ${klass}`
}
function errorLabel(value: string) {
  const key = `channelTelemetry.errorCategories.${value}`
  return te(key) ? t(key) : monitorErrorCategoryLabel(value)
}
function toggleError(category: string) {
  const next = new Set(expandedErrors.value)
  if (next.has(category)) next.delete(category)
  else next.add(category)
  expandedErrors.value = next
}

let lastRange: MonitorRange = filter.value.range
watch(
  filter,
  () => {
    syncQuery()
    const rangeChanged = filter.value.range !== lastRange
    lastRange = filter.value.range
    if (rangeChanged) void reload(true)
    else void reloadMetricsOnly(true)
  },
  { deep: true }
)
watch(matrixGroupBy, () => {
  syncQuery()
  void reloadMetricsOnly(true)
})
watch(healthMode, syncQuery)
watch(trendView, syncQuery)
watch(activeTab, () => {
  syncQuery()
  void loadTab()
})
watch(showUserRanking, (allowed) => {
  if (!allowed && activeTab.value === 'users') {
    activeTab.value = 'models'
  }
})
onMounted(() => void reload(false))
onBeforeUnmount(() => {
  controller?.abort()
  if (autoRefreshTimer) window.clearInterval(autoRefreshTimer)
})
</script>

<style scoped>
.monitor-surface {
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--card);
  box-shadow: none;
}
.segmented .tab::after {
  display: none;
}
.status-dot {
  display: inline-block;
  height: 0.5rem;
  width: 0.5rem;
  flex: none;
  border-radius: 9999px;
}
/* Multi-stop green → yellow → red score bands */
.health-score10 { background: #16a34a; }
.health-score9  { background: #22c55e; }
.health-score8  { background: #4ade80; }
.health-score7  { background: #a3e635; }
.health-score6  { background: #facc15; }
.health-score5  { background: #fbbf24; }
.health-score4  { background: #f59e0b; }
.health-score3  { background: #f97316; }
.health-score2  { background: #fb7185; }
.health-score1  { background: #f87171; }
.health-score0  { background: rgb(239, 67, 67); }
.health-healthy  { background: #22c55e; }
.health-warning  { background: #f59e0b; }
.health-critical { background: #ef4444; }
.health-unknown  { background: #9ca3af; }
.matrix-select {
  min-width: 10rem;
}
details > summary::-webkit-details-marker {
  display: none;
}
</style>
