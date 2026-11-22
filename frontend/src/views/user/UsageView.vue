<template>
  <AppLayout>
    <div class="oh-usage-workbench space-y-5 tabular-nums stat-num">
      <!-- OpenRouter Activity 风格工作台顶栏 -->
      <header class="oh-activity-header flex flex-wrap items-start justify-between gap-3">
        <div class="min-w-0 space-y-1">
          <div class="flex flex-wrap items-center gap-2.5">
            <span class="oh-brand-bar" aria-hidden="true" />
            <h1 class="text-xl font-bold tracking-tight text-[var(--foreground)] sm:text-2xl">
              {{ t('usage.tabs.usage') }}
            </h1>
            <span class="oh-activity-pill font-mono tabular-nums">
              <span class="pulse-dot pulse-ok" aria-hidden="true" />
              <span>{{ isZh ? '实时活动' : 'ACTIVITY' }}</span>
            </span>
            <span class="pill pill-ghost font-mono text-xs tabular-nums">
              {{ startDate }} → {{ endDate }}
            </span>
          </div>
          <div class="flex flex-wrap items-center gap-2 text-xs text-[var(--muted-foreground)]">
            <ReportTimeContext :time-zone="reportTimeZone" />
            <span aria-hidden="true">·</span>
            <span class="font-mono tabular-nums">
              {{ activeTab === 'errors' ? errorTotal : pagination.total }} {{ isZh ? '条记录' : 'RECORDS' }}
            </span>
          </div>
        </div>

        <div v-if="errorViewEnabled" class="segmented" role="tablist">
          <button
            type="button"
            class="tab seg-btn"
            :class="{ 'tab-active on seg-active': activeTab === 'usage' }"
            :aria-pressed="activeTab === 'usage'"
            @click="activeTab = 'usage'"
          >
            {{ t('usage.tabs.usage') }}
          </button>
          <button
            type="button"
            class="tab seg-btn"
            :class="{ 'tab-active on seg-active': activeTab === 'errors' }"
            :aria-pressed="activeTab === 'errors'"
            @click="switchToErrors"
          >
            {{ t('usage.tabs.errors') }}
          </button>
        </div>
      </header>

      <UserPendingSettlement :filters="normalizedFilters" />

      <!-- 统计概览卡片（OpenRouter Activity 细边框 + tabular-nums） -->
      <div class="oh-activity-stats-shell tabular-nums stat-num">
        <UsageStatsCards
          :stats="usageStats"
          :loading="endpointStatsLoading"
          :error="statsError"
          :history-gap="statsHistoryGap"
          :show-account-cost="false"
          :strike-standard-cost="true"
        />
      </div>

      <div class="space-y-4">
        <!-- 时间范围与趋势粒度控制栏 -->
        <div class="oh-activity-card card p-3.5 sm:p-4">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div class="flex flex-wrap items-center gap-2.5">
              <span class="text-xs font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">
                {{ t('admin.dashboard.timeRange') }}:
              </span>
              <DateRangePicker
                v-model:start-date="startDate"
                v-model:end-date="endDate"
                @change="onDateRangeChange"
              />
              <span class="pill pill-ghost hidden font-mono text-[11px] tabular-nums sm:inline-flex">
                {{ granularity === 'hour' ? (isZh ? '按小时聚合' : '1H BUCKET') : (isZh ? '按天聚合' : '1D BUCKET') }}
              </span>
            </div>
            <div class="ml-auto flex items-center gap-2">
              <span class="text-xs font-semibold uppercase tracking-wider text-[var(--muted-foreground)]">
                {{ t('admin.dashboard.granularity') }}:
              </span>
              <div class="w-28">
                <Select v-model="granularity" :options="granularityOptions" @change="loadChartData" />
              </div>
            </div>
          </div>
        </div>

        <!-- 用量分布与趋势图表容器 -->
        <div class="oh-activity-charts-grid grid grid-cols-1 gap-4 lg:grid-cols-2">
          <ModelDistributionChart :model-unknown="modelState === 'unknown'" :model-error="modelState === 'error'"
            v-model:metric="modelDistributionMetric"
            :model-stats="requestedModelStats"
            :loading="modelStatsLoading"
            :show-source-toggle="false"
            :show-metric-toggle="true"
            :enable-breakdown="false"
            :show-account-cost="false"
            :start-date="startDate"
            :end-date="endDate"
          />
          <UsageStatsState :state="historyGap ? 'unknown' : chartError ? 'error' : 'known'">
            <GroupDistributionChart
              v-model:metric="groupDistributionMetric"
              :group-stats="groupStats"
              :loading="chartsLoading"
              :show-metric-toggle="true"
              :enable-breakdown="false"
              :show-account-cost="false"
              :start-date="startDate"
              :end-date="endDate"
            />
          </UsageStatsState>
        </div>

        <div class="oh-activity-charts-grid grid grid-cols-1 gap-4 lg:grid-cols-2">
          <UsageStatsState :state="statsHistoryGap ? 'unknown' : statsError ? 'error' : 'known'">
            <EndpointDistributionChart
              v-model:source="endpointDistributionSource"
              v-model:metric="endpointDistributionMetric"
              :endpoint-stats="inboundEndpointStats"
              :upstream-endpoint-stats="upstreamEndpointStats"
              :endpoint-path-stats="endpointPathStats"
              :loading="endpointStatsLoading"
              :show-source-toggle="false"
              :show-metric-toggle="true"
              :enable-breakdown="false"
              :title="t('usage.endpointDistribution')"
              :start-date="startDate"
              :end-date="endDate"
            />
          </UsageStatsState>
          <UsageStatsState :state="historyGap ? 'unknown' : chartError ? 'error' : 'known'">
            <TokenUsageTrend :time-zone="reportTimeZone" :trend-data="trendData" :loading="chartsLoading" />
          </UsageStatsState>
        </div>
      </div>

      <!-- 筛选控制栏 -->
      <div class="oh-activity-card oh-filter-workbench card p-4 sm:p-5">
        <div class="flex flex-wrap items-end justify-between gap-4">
          <div v-if="activeTab === 'errors'" class="flex flex-1 flex-wrap items-end gap-4">
            <div class="w-full sm:w-auto sm:min-w-[220px]">
              <label class="input-label">{{ t('usage.errors.keyName') }}</label>
              <Select v-model="errorFilter.api_key_id" :options="errorKeyOptions" @change="applyErrorFilters" />
            </div>
            <div class="w-full sm:w-auto sm:min-w-[220px]">
              <label class="input-label">{{ t('usage.errors.model') }}</label>
              <Select
                v-model="errorFilter.model"
                :options="errorModelOptions"
                searchable
                creatable
                clearable
                :placeholder="t('usage.errors.modelPlaceholder')"
                @change="applyErrorFilters"
              />
            </div>
            <div class="w-full sm:w-auto sm:min-w-[200px]">
              <label class="input-label">{{ t('usage.errors.category') }}</label>
              <Select v-model="errorFilter.category" :options="errorCategoryOptions" @change="applyErrorFilters" />
            </div>
            <div class="w-full sm:w-auto sm:min-w-[180px]">
              <label class="input-label">{{ t('usage.errors.status') }}</label>
              <Select v-model="errorFilter.status_code" :options="errorStatusOptions" @change="applyErrorFilters" />
            </div>
          </div>
          <UsageFilters
            v-else
            v-model="filters"
            inline
            scope="self"
            :show-actions="false"
            :exporting="exporting"
            :start-date="startDate"
            :end-date="endDate"
            :model-options="modelOptionValues"
            :api-key-options="apiKeyOptions"
            :group-options="groupOptions"
            :show-billing-type="subscriptionFeatureEnabled"
            @change="applyFilters"
          />

          <div class="flex w-full flex-wrap items-center justify-end gap-2.5 sm:w-auto">
            <button type="button" @click="refreshData" :disabled="activeTab === 'errors' ? errorLoading : loading" class="btn btn-secondary">
              {{ t('common.refresh') }}
            </button>
            <button type="button" @click="resetFilters" class="btn btn-secondary">
              {{ t('common.reset') }}
            </button>
            <div class="relative" ref="columnDropdownRef">
              <button
                type="button"
                data-testid="usage-column-settings"
                @click="showColumnDropdown = !showColumnDropdown"
                class="btn btn-secondary px-2 md:px-3"
                :title="t('admin.users.columnSettings')"
              >
                <Icon name="grid" size="sm" />
                <span class="hidden md:inline">{{ t('admin.users.columnSettings') }}</span>
              </button>
              <div
                v-if="showColumnDropdown"
                class="absolute right-0 top-full z-50 mt-1 max-h-80 w-48 overflow-y-auto rounded-lg border border-[var(--border)] bg-[var(--card)] py-1 shadow-lg"
              >
                <button
                  v-for="col in currentToggleableColumns"
                  :key="col.key"
                  type="button"
                  :data-testid="`usage-column-toggle-${col.key}`"
                  @click="toggleCurrentColumn(col.key)"
                  class="flex w-full items-center justify-between px-4 py-2 text-left text-sm text-[var(--foreground)] hover:bg-[var(--secondary)]"
                >
                  <span>{{ col.label }}</span>
                  <Icon v-if="isCurrentColumnVisible(col.key)" name="check" size="sm" class="text-primary-500" />
                </button>
              </div>
            </div>
            <button v-if="activeTab !== 'errors'" type="button" @click="exportToCSV" :disabled="exporting" class="btn btn-primary">
              {{ exporting ? t('usage.exporting') : t('usage.exportCsv') }}
            </button>
          </div>
        </div>
      </div>

      <!-- 明细表格工作台容器 -->
      <div class="oh-activity-table-shell space-y-3 tabular-nums stat-num">
        <div class="flex flex-wrap items-center justify-between gap-2 px-0.5">
          <div class="flex flex-wrap items-center gap-2">
            <h2 class="text-sm font-semibold text-[var(--foreground)]">
              {{ activeTab === 'errors' ? t('usage.tabs.errors') : t('usage.tabs.usage') }}
            </h2>
            <span class="pill pill-ghost font-mono text-[11px] tabular-nums">
              {{ activeTab === 'errors' ? errorTotal : pagination.total }}
            </span>
            <span class="pill pill-ghost font-mono text-[11px] tabular-nums">
              {{ visibleColumnsCount }} {{ isZh ? '列' : 'COLS' }}
            </span>
          </div>
          <span class="font-mono text-xs tabular-nums text-[var(--muted-foreground)]">
            {{ activeTab === 'errors' ? errorSortBy : sortState.sort_by }} · {{ (activeTab === 'errors' ? errorSortOrder : sortState.sort_order).toUpperCase() }}
          </span>
        </div>

        <template v-if="activeTab === 'usage'">
          <UsageStatsState :state="logsState">
            <UsageTable :time-zone="reportTimeZone"
              :data="usageLogs"
              :loading="loading"
              :columns="visibleColumns"
              :server-side-sort="true"
              :show-account-billing="false"
              :show-upstream-endpoint="false"
              default-sort-key="created_at"
              default-sort-order="desc"
              @sort="handleSort"
              @ipGeoBatchFailed="handleIpGeoBatchFailed"
            />
          </UsageStatsState>

          <Pagination
            v-if="pagination.total > 0"
            :page="pagination.page"
            :total="pagination.total"
            :page-size="pagination.page_size"
            @update:page="handlePageChange"
            @update:pageSize="handlePageSizeChange"
          />
        </template>

        <template v-else-if="errorViewEnabled">
          <ConsoleAsyncBanner
            v-if="errorNotice"
            data-testid="usage-errors-notice"
            :notice="errorNotice"
            :retry-label="t('common.refresh')"
            class="mb-3"
            @retry="loadErrors"
          />
          <UserErrorRequestsTable
            v-if="!errorNotice || errorNotice.kind !== 'permission'"
            :rows="errorRows"
            :total="errorTotal"
            :loading="errorLoading"
            :page="errorPage"
            :page-size="errorPageSize"
            :visible-column-keys="errVisibleColumnKeys"
            @sort="onErrorSort"
            @update:page="onErrorPage"
            @update:pageSize="onErrorPageSize"
            @ipGeoBatchFailed="handleIpGeoBatchFailed"
          />
        </template>
      </div>
    </div>
  </AppLayout>

</template>

<script setup lang="ts">
import UserPendingSettlement from '@/components/usage/UserPendingSettlement.vue'
import { resolveReportTimeZone, formatReportDateInput } from '@/utils/format'
import ReportTimeContext from '@/components/charts/ReportTimeContext.vue'
const reportProps = defineProps<{ timeZone?: string }>()
const reportTimeZone = resolveReportTimeZone(reportProps.timeZone)
const reportQueryContext = { timezone: reportTimeZone }

import UsageStatsState from '@/components/common/UsageStatsState.vue'
import { historyGapFromError, usageExportFailureMessage, withTrendGaps, type HistoryGap } from '@/views/trendGaps'
import { billingEvidenceFields } from '@/utils/usageBillingExplanation'
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { FeatureFlags, resolveFeatureFlag } from '@/utils/featureFlags'
import { keysAPI, usageAPI, userGroupsAPI } from '@/api'
import AppLayout from '@/components/layout/AppLayout.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import UsageStatsCards from '@/components/admin/usage/UsageStatsCards.vue'
import UsageTable from '@/components/admin/usage/UsageTable.vue'
import ModelDistributionChart from '@/components/charts/ModelDistributionChart.vue'
import GroupDistributionChart from '@/components/charts/GroupDistributionChart.vue'
import EndpointDistributionChart from '@/components/charts/EndpointDistributionChart.vue'
import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import Icon from '@/components/icons/Icon.vue'
import UserErrorRequestsTable from '@/components/user/UserErrorRequestsTable.vue'
import ConsoleAsyncBanner from '@/features/console/components/ConsoleAsyncBanner.vue'
import { classifyConsoleError, type ConsoleAsyncNotice } from '@/features/console/asyncState'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import UsageFilters from '@/components/admin/usage/UsageFilters.vue'
import { USER_USAGE_EXPORT_ENDPOINT, downloadUsageCsv, usageExportFileName } from '@/utils/usageExport'
import { requestTypeToLegacyStream } from '@/utils/usageRequestType'
import type {
  ApiKey,
  EndpointStat,
  Group,
  GroupStat,
  ModelStat,
  TrendDataPoint,
  UsageLog,
  UsageQueryParams,
  UsageStatsResponse,
  UserErrorRequest,
} from '@/types'
import type { Column } from '@/components/common/types'
import { COMMON_ERROR_STATUS_CODES } from '@/utils/errorBadges'

const i18n = useI18n()
const { t } = i18n
const isZh = computed(() => {
  const rawLocale = typeof i18n.locale === 'string' ? i18n.locale : i18n.locale?.value
  if (rawLocale) return String(rawLocale).toLowerCase().startsWith('zh')
  const probe = t('usage.tabs.usage')
  return Boolean(probe && probe !== 'usage.tabs.usage' && /[\u4e00-\u9fa5]/.test(probe))
})
const historyGap = ref<HistoryGap | null>(null)
const statsError = ref(false)
const chartError = ref(false)
const modelState = ref<'known' | 'unknown' | 'error'>('known')
const logsState = ref<'known' | 'unknown' | 'error'>('known')
const statsHistoryGap = ref<HistoryGap | null>(null)
const appStore = useAppStore()

type DistributionMetric = 'tokens' | 'actual_cost'
type EndpointSource = 'inbound' | 'upstream' | 'path'

const usageStats = ref<UsageStatsResponse | null>(null)
const usageLogs = ref<UsageLog[]>([])
const trendData = ref<TrendDataPoint[]>([])
const requestedModelStats = ref<ModelStat[]>([])
const groupStats = ref<GroupStat[]>([])
const inboundEndpointStats = ref<EndpointStat[]>([])
const upstreamEndpointStats = ref<EndpointStat[]>([])
const endpointPathStats = ref<EndpointStat[]>([])

const loading = ref(false)
const chartsLoading = ref(false)
const modelStatsLoading = ref(false)
const endpointStatsLoading = ref(false)
const exporting = ref(false)
const errorRows = ref<UserErrorRequest[]>([])
const errorLoading = ref(false)
const errorNotice = ref<ConsoleAsyncNotice | null>(null)
const errorPage = ref(1)
const errorPageSize = ref(20)
const errorSortBy = ref('created_at')
const errorSortOrder = ref<'asc' | 'desc'>('desc')
const errorTotal = ref(0)
const errorFilter = ref<{ model: string | null; category: string; api_key_id: number | null; status_code: number | null }>({
  model: '',
  category: '',
  api_key_id: null,
  status_code: null,
})

const errorKeyOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.errors.allKeys') },
  ...apiKeys.value.map((k) => ({ value: k.id, label: k.name })),
])

// 模型候选取自当前已加载错误中出现过的模型；creatable 允许输入任意片段做后端模糊。
const errorModelOptions = computed<SelectOption[]>(() => {
  const seen = new Set<string>()
  const opts: SelectOption[] = []
  for (const r of errorRows.value) {
    if (r.model && !seen.has(r.model)) {
      seen.add(r.model)
      opts.push({ value: r.model, label: r.model })
    }
  }
  return opts
})

const errorCategoryCodes = ['auth', 'rate_limit', 'quota', 'invalid_request', 'service_unavailable', 'upstream', 'internal', 'cyber']

const errorCategoryOptions = computed<SelectOption[]>(() => [
  { value: '', label: t('usage.errors.allCategories') },
  ...errorCategoryCodes.map((c) => ({ value: c, label: t('usage.errors.categories.' + c) })),
])

// 状态码候选用固定常用列表(与管理端 UsageFilters 共用常量),不受当前页数据限制:
// 后端 status_code 过滤对全量生效,若只列当前页出现过的码,用户就选不到仅在后续页的码。
const errorStatusOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.errors.allStatuses') },
  ...COMMON_ERROR_STATUS_CODES.map((c) => ({ value: c, label: String(c) })),
])

const applyErrorFilters = () => {
  errorPage.value = 1
  void loadErrors()
}

let abortController: AbortController | null = null
let exportAbortController: AbortController | null = null
let chartReqSeq = 0
let statsReqSeq = 0
let modelStatsReqSeq = 0

const formatLocalDate = (date: Date): string => formatReportDateInput(date, reportTimeZone)

const getLast24HoursRangeDates = () => {
  const end = new Date()
  const start = new Date(end.getTime() - 24 * 60 * 60 * 1000)
  return { start: formatLocalDate(start), end: formatLocalDate(end) }
}

const getGranularityForRange = (start: string, end: string): 'day' | 'hour' => {
  const startTime = new Date(`${start}T00:00:00`).getTime()
  const endTime = new Date(`${end}T00:00:00`).getTime()
  return Math.ceil((endTime - startTime) / (1000 * 60 * 60 * 24)) <= 1 ? 'hour' : 'day'
}

const defaultRange = getLast24HoursRangeDates()
const startDate = ref(defaultRange.start)
const endDate = ref(defaultRange.end)
const granularity = ref<'day' | 'hour'>(getGranularityForRange(startDate.value, endDate.value))

const modelDistributionMetric = ref<DistributionMetric>('tokens')
const groupDistributionMetric = ref<DistributionMetric>('tokens')
const endpointDistributionMetric = ref<DistributionMetric>('tokens')
const endpointDistributionSource = ref<EndpointSource>('inbound')
const activeTab = ref<'usage' | 'errors'>('usage')
const errorViewEnabled = computed(() => appStore.cachedPublicSettings?.allow_user_view_error_requests ?? false)

const filters = ref<UsageQueryParams>({
  ...reportQueryContext, start_date: startDate.value,
  end_date: endDate.value,
  request_type: undefined,
  native_compaction_v2: null,
  billing_type: null,
  billing_mode: null,
})

const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
})
const sortState = reactive({
  sort_by: 'created_at',
  sort_order: 'desc' as 'asc' | 'desc',
})

const granularityOptions = computed<SelectOption[]>(() => [
  { value: 'day', label: t('admin.dashboard.day') },
  { value: 'hour', label: t('admin.dashboard.hour') },
])
// 订阅功能关闭后只剩余额计费，「计费类型」筛选（余额/订阅）失去意义，整块隐藏。
const subscriptionFeatureEnabled = computed(() => resolveFeatureFlag(appStore.cachedPublicSettings, FeatureFlags.subscription))

const apiKeys = ref<ApiKey[]>([])
const groups = ref<Group[]>([])
const modelOptionValues = ref<string[]>([])

const apiKeyOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.allApiKeys') },
  ...apiKeys.value.map((key) => ({ value: key.id, label: key.name })),
])
const groupOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allGroups') },
  ...groups.value.map((group) => ({ value: group.id, label: group.name })),
])

const normalizedFilters = computed<UsageQueryParams>(() => {
  const requestType = filters.value.request_type
  const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : filters.value.stream
  return {
    ...filters.value,
    ...reportQueryContext, start_date: startDate.value,
    end_date: endDate.value,
    stream: legacyStream === null ? undefined : legacyStream,
  }
})

const buildUsageListParams = (page: number, pageSize: number): UsageQueryParams => ({
  page,
  page_size: pageSize,
  ...normalizedFilters.value,
  sort_by: sortState.sort_by,
  sort_order: sortState.sort_order,
})

const loadLogs = async () => {
  abortController?.abort()
  const controller = new AbortController()
  abortController = controller
  loading.value = true
  logsState.value = 'known'
  usageLogs.value = []
  pagination.total = 0
  try {
    const response = await usageAPI.query(buildUsageListParams(pagination.page, pagination.page_size), { signal: controller.signal })
    if (controller.signal.aborted || abortController !== controller) return
    usageLogs.value = response.items
    pagination.total = response.total
  } catch (error: any) {
    if (controller.signal.aborted || abortController !== controller || error?.name === 'AbortError' || error?.code === 'ERR_CANCELED') return
    logsState.value = historyGapFromError(error) ? 'unknown' : 'error'
    if (logsState.value === 'error') appStore.showError(t('usage.failedToLoad'))
  } finally {
    if (abortController === controller) loading.value = false
  }
}

const loadStats = async () => {
  const seq = ++statsReqSeq
  usageStats.value = null
  statsError.value = false
  statsHistoryGap.value = null
  endpointStatsLoading.value = true
  try {
    const stats = await usageAPI.getStats(normalizedFilters.value)
    if (seq !== statsReqSeq) return
    usageStats.value = stats
    inboundEndpointStats.value = stats.endpoints || []
    upstreamEndpointStats.value = []
    endpointPathStats.value = []
  } catch (error) {
    if (seq !== statsReqSeq) return
    usageStats.value = null
    statsError.value = true
    statsHistoryGap.value = historyGapFromError(error)
    console.error('Failed to load usage stats:', error)
    inboundEndpointStats.value = []
    upstreamEndpointStats.value = []
    endpointPathStats.value = []
  } finally {
    if (seq === statsReqSeq) endpointStatsLoading.value = false
  }
}

const loadModelStats = async () => {
  const seq = ++modelStatsReqSeq
  modelStatsLoading.value = true
  modelState.value = 'known'
  try {
    const response = await usageAPI.getDashboardModels({
      ...normalizedFilters.value,
      model_source: 'requested',
    })
    if (seq !== modelStatsReqSeq) return
    requestedModelStats.value = response.models || []
    refreshModelOptions(response.models || [])
  } catch (error) {
    if (seq !== modelStatsReqSeq) return
    modelState.value = historyGapFromError(error) ? 'unknown' : 'error'
    console.error('Failed to load model stats:', error)
    requestedModelStats.value = []
  } finally {
    if (seq === modelStatsReqSeq) modelStatsLoading.value = false
  }
}

const loadChartData = async () => {
  const seq = ++chartReqSeq
  chartsLoading.value = true
  chartError.value = false
  historyGap.value = null
  try {
    const snapshot = await usageAPI.getDashboardSnapshotV2({
      ...normalizedFilters.value,
      granularity: granularity.value,
      include_trend: true,
      include_model_stats: false,
      include_group_stats: true,
    })
    if (seq !== chartReqSeq) return
    historyGap.value = null
    trendData.value = withTrendGaps(snapshot.trend || [], granularity.value)
    groupStats.value = snapshot.groups || []
  } catch (error) {
    if (seq !== chartReqSeq) return
    historyGap.value = historyGapFromError(error)
    chartError.value = !historyGap.value
    console.error('Failed to load chart data:', error)
    trendData.value = []
    groupStats.value = []
  } finally {
    if (seq === chartReqSeq) chartsLoading.value = false
  }
}

const refreshModelOptions = (models: ModelStat[]) => {
  const current = filters.value.model
  const set = new Set(modelOptionValues.value)
  models.forEach((item) => {
    if (item.model) set.add(item.model)
  })
  if (current) set.add(current)
  modelOptionValues.value = Array.from(set).sort()
}

const applyFilters = () => {
  pagination.page = 1
  void loadLogs()
  void loadStats()
  void loadModelStats()
  void loadChartData()
  resetErrorRows()
}

const refreshData = () => {
  void loadLogs()
  void loadStats()
  void loadModelStats()
  void loadChartData()
  if (activeTab.value === 'errors') void loadErrors()
}

const resetFilters = () => {
  const range = getLast24HoursRangeDates()
  startDate.value = range.start
  endDate.value = range.end
  filters.value = {
    start_date: range.start,
    end_date: range.end,
    request_type: undefined,
    native_compaction_v2: null,
    billing_type: null,
    billing_mode: null,
  }
  granularity.value = getGranularityForRange(range.start, range.end)
  applyFilters()
  if (activeTab.value === 'errors') {
    errorFilter.value = { model: '', category: '', api_key_id: null, status_code: null }
    applyErrorFilters()
  }
}

const onDateRangeChange = (range: { startDate: string; endDate: string; preset: string | null }) => {
  startDate.value = range.startDate
  endDate.value = range.endDate
  filters.value.start_date = range.startDate
  filters.value.end_date = range.endDate
  granularity.value = getGranularityForRange(range.startDate, range.endDate)
  applyFilters()
}

const handlePageChange = (page: number) => {
  pagination.page = page
  void loadLogs()
}

const handlePageSizeChange = (pageSize: number) => {
  pagination.page_size = pageSize
  pagination.page = 1
  void loadLogs()
}

const handleSort = (key: string, order: 'asc' | 'desc') => {
  sortState.sort_by = key
  sortState.sort_order = order
  pagination.page = 1
  void loadLogs()
}

const handleIpGeoBatchFailed = () => {
  appStore.showError(t('usage.ipGeo.batchFailed'))
}

// 用户端 CSV 表头（界面语言）；列顺序由服务端导出契约决定。
const usageExportLabels = (): string[] => [
  t('usage.time'),
  t('usage.apiKeyFilter'),
  t('usage.model'),
  t('usage.reasoningEffort'),
  t('usage.inboundEndpoint'),
  t('admin.usage.ipAddress'),
  t('usage.type'),
  t('admin.usage.billingMode'),
  t('admin.usage.inputTokens'),
  t('admin.usage.outputTokens'),
  t('admin.usage.cacheReadTokens'),
  t('admin.usage.cacheCreationTokens'),
  t('usage.rate'),
  t('usage.userBilled') + ' (USD)',
  t('usage.original') + ' (USD)',
  t('usage.firstToken'),
  t('usage.duration'),
  ...billingEvidenceFields.map(field => t(`usage.evidence.${field}`)),
]

// 导出交给服务端：一次请求流式取回 CSV，浏览器不再按页循环拉取。
const exportToCSV = async () => {
  if (exporting.value) return
  exporting.value = true
  const c = new AbortController()
  exportAbortController = c
  appStore.showInfo(t('usage.preparingExport'))
  try {
    await downloadUsageCsv({
      endpoint: USER_USAGE_EXPORT_ENDPOINT,
      params: buildUsageListParams(1, 100),
      labels: usageExportLabels(),
      fileName: usageExportFileName(startDate.value, endDate.value, 'csv'),
      signal: c.signal,
    })
    if (!c.signal.aborted) appStore.showSuccess(t('usage.exportSuccess'))
  } catch (error) {
    if (!c.signal.aborted) {
      console.error('CSV Export failed:', error)
      appStore.showError(usageExportFailureMessage(error, t) ?? t('usage.exportFailed'))
    }
  } finally {
    if (exportAbortController === c) { exportAbortController = null; exporting.value = false }
  }
}

const ALWAYS_VISIBLE = ['created_at']
const DEFAULT_HIDDEN_COLUMNS = ['user_agent']
const HIDDEN_COLUMNS_KEY = 'user-usage-hidden-columns'

const allColumns = computed<Column[]>(() => [
  { key: 'api_key', label: t('usage.apiKeyFilter'), sortable: false },
  { key: 'model', label: t('usage.model'), sortable: true },
  { key: 'reasoning_effort', label: t('usage.reasoningEffort'), sortable: false },
  { key: 'endpoint', label: t('usage.endpoint'), sortable: false },
  { key: 'ip_address', label: 'IP', sortable: false },
  { key: 'group', label: t('admin.usage.group'), sortable: false },
  { key: 'stream', label: t('usage.type'), sortable: false },
  { key: 'billing_mode', label: t('admin.usage.billingMode'), sortable: false },
  { key: 'tokens', label: t('usage.tokens'), sortable: false },
  { key: 'cost', label: t('usage.cost'), sortable: false },
  { key: 'latency', label: t('usage.latency'), sortable: false },
  { key: 'created_at', label: t('usage.time'), sortable: true },
  { key: 'user_agent', label: t('usage.userAgent'), sortable: false },
])

const hiddenColumns = reactive<Set<string>>(new Set())
const toggleableColumns = computed(() => allColumns.value.filter((col) => !ALWAYS_VISIBLE.includes(col.key)))
const visibleColumns = computed(() =>
  allColumns.value.filter((col) => ALWAYS_VISIBLE.includes(col.key) || !hiddenColumns.has(col.key))
)
const isColumnVisible = (key: string) => !hiddenColumns.has(key)
const toggleColumn = (key: string) => {
  if (hiddenColumns.has(key)) hiddenColumns.delete(key)
  else hiddenColumns.add(key)
  localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify([...hiddenColumns]))
}
const loadSavedColumns = () => {
  try {
    const saved = localStorage.getItem(HIDDEN_COLUMNS_KEY)
    const values = saved ? JSON.parse(saved) as string[] : DEFAULT_HIDDEN_COLUMNS
    values.forEach((key) => hiddenColumns.add(key))
  } catch {
    DEFAULT_HIDDEN_COLUMNS.forEach((key) => hiddenColumns.add(key))
  }
}

// 错误请求 tab 独立列设置(机制同用量列设置,存储互不影响)
const ERR_ALWAYS_VISIBLE = ['status', 'created_at']
const ERR_DEFAULT_HIDDEN_COLUMNS = ['user_agent']
const ERR_HIDDEN_COLUMNS_KEY = 'user-usage-error-hidden-columns'

// key 须与 UserErrorRequestsTable 的 allColumns 一致
const errAllColumns = computed<Column[]>(() => [
  { key: 'key_name', label: t('usage.errors.keyName') },
  { key: 'model', label: t('usage.errors.model') },
  { key: 'endpoint', label: t('usage.errors.endpoint') },
  { key: 'client_ip', label: 'IP' },
  { key: 'group', label: t('admin.usage.group') },
  { key: 'type', label: t('usage.type') },
  { key: 'platform', label: t('usage.errors.platform') },
  { key: 'category', label: t('usage.errors.category') },
  { key: 'status', label: t('usage.errors.status') },
  { key: 'message', label: t('usage.errors.message') },
  { key: 'created_at', label: t('usage.errors.time') },
  { key: 'user_agent', label: t('usage.userAgent') },
])

const errHiddenColumns = reactive<Set<string>>(new Set())
const errToggleableColumns = computed(() =>
  errAllColumns.value.filter((col) => !ERR_ALWAYS_VISIBLE.includes(col.key))
)
const errVisibleColumnKeys = computed(() =>
  errAllColumns.value
    .filter((col) => ERR_ALWAYS_VISIBLE.includes(col.key) || !errHiddenColumns.has(col.key))
    .map((col) => col.key)
)
const isErrColumnVisible = (key: string) => !errHiddenColumns.has(key)
const toggleErrColumn = (key: string) => {
  if (errHiddenColumns.has(key)) errHiddenColumns.delete(key)
  else errHiddenColumns.add(key)
  localStorage.setItem(ERR_HIDDEN_COLUMNS_KEY, JSON.stringify([...errHiddenColumns]))
}
const loadSavedErrColumns = () => {
  try {
    const saved = localStorage.getItem(ERR_HIDDEN_COLUMNS_KEY)
    const values = saved ? (JSON.parse(saved) as string[]) : ERR_DEFAULT_HIDDEN_COLUMNS
    values.forEach((key) => errHiddenColumns.add(key))
  } catch {
    ERR_DEFAULT_HIDDEN_COLUMNS.forEach((key) => errHiddenColumns.add(key))
  }
}

// 列设置下拉按当前 tab 分发
const currentToggleableColumns = computed(() =>
  activeTab.value === 'errors' ? errToggleableColumns.value : toggleableColumns.value
)
const visibleColumnsCount = computed(() =>
  activeTab.value === 'errors' ? errVisibleColumnKeys.value.length : visibleColumns.value.length
)
const isCurrentColumnVisible = (key: string) =>
  activeTab.value === 'errors' ? isErrColumnVisible(key) : isColumnVisible(key)
const toggleCurrentColumn = (key: string) => {
  if (activeTab.value === 'errors') toggleErrColumn(key)
  else toggleColumn(key)
}

const showColumnDropdown = ref(false)
const columnDropdownRef = ref<HTMLElement | null>(null)
const handleColumnClickOutside = (event: MouseEvent) => {
  if (columnDropdownRef.value && !columnDropdownRef.value.contains(event.target as HTMLElement)) {
    showColumnDropdown.value = false
  }
}

const loadApiKeys = async () => {
  const firstPage = await keysAPI.list(1, 100)
  const keys = [...firstPage.items]
  for (let page = 2; page <= firstPage.pages && keys.length > 0; page++) {
    const response = await keysAPI.list(page, 100)
    if (response.items.length === 0) break
    keys.push(...response.items)
  }
  return keys
}

const loadFilterOptions = async () => {
  const [keysResult, groupsResult] = await Promise.allSettled([
    loadApiKeys(),
    userGroupsAPI.getAvailable(),
  ])
  if (keysResult.status === 'fulfilled') {
    apiKeys.value = keysResult.value
  } else {
    apiKeys.value = []
    const status = (keysResult.reason as { status?: number; response?: { status?: number } })?.status ??
      (keysResult.reason as { response?: { status?: number } })?.response?.status
    if (status !== 403) {
      console.error('Failed to load usage API key filters:', keysResult.reason)
    }
  }
  if (groupsResult.status === 'fulfilled') {
    groups.value = groupsResult.value
  } else {
    groups.value = []
    const status = (groupsResult.reason as { status?: number; response?: { status?: number } })?.status ??
      (groupsResult.reason as { response?: { status?: number } })?.response?.status
    if (status !== 403) {
      console.error('Failed to load usage group filters:', groupsResult.reason)
    }
  }
}

const resetErrorRows = () => {
  errorPage.value = 1
  if (activeTab.value === 'errors') {
    void loadErrors()
  } else {
    errorRows.value = []
    errorTotal.value = 0
  }
}

const loadErrors = async () => {
  errorLoading.value = true
  errorNotice.value = null
  try {
    const resp = await usageAPI.listMyErrorRequests({
      page: errorPage.value,
      page_size: errorPageSize.value,
      ...reportQueryContext, start_date: startDate.value,
      end_date: endDate.value,
      model: (errorFilter.value.model ?? '').trim() || undefined,
      category: errorFilter.value.category || undefined,
      api_key_id: errorFilter.value.api_key_id ?? undefined,
      status_code: errorFilter.value.status_code ?? undefined,
      sort_by: errorSortBy.value,
      sort_order: errorSortOrder.value,
    })
    errorRows.value = resp.items
    errorTotal.value = resp.total
  } catch (error) {
    errorNotice.value = classifyConsoleError(error, {
      hasCachedData: errorRows.value.length > 0,
      requiredGrantFallback: 'usage.read',
      isZh: isZh.value,
    })
  } finally {
    errorLoading.value = false
  }
}

const onErrorSort = (sortBy: string, sortOrder: 'asc' | 'desc') => {
  errorSortBy.value = sortBy
  errorSortOrder.value = sortOrder
  errorPage.value = 1
  void loadErrors()
}

const onErrorPage = (page: number) => {
  errorPage.value = page
  void loadErrors()
}

const onErrorPageSize = (pageSize: number) => {
  errorPageSize.value = pageSize
  errorPage.value = 1
  void loadErrors()
}

const switchToErrors = () => {
  activeTab.value = 'errors'
  if (errorRows.value.length === 0) void loadErrors()
}

onMounted(() => {
  loadSavedColumns()
  loadSavedErrColumns()
  document.addEventListener('click', handleColumnClickOutside)
  void loadFilterOptions()
  refreshData()
})

onUnmounted(() => {
  chartReqSeq++
  statsReqSeq++
  modelStatsReqSeq++

  abortController?.abort()
  document.removeEventListener('click', handleColumnClickOutside)
})

watch(endpointDistributionSource, () => {
  // Endpoint source switching is handled by the chart component using already loaded stats.
})
</script>

<style scoped>
.oh-brand-bar {
  width: 3.5px;
  height: 20px;
  border-radius: 9999px;
  background: var(--brand, #f59e0b);
}

.oh-activity-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 2px 9px;
  border-radius: 9999px;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.04em;
  background: color-mix(in srgb, var(--brand, #f59e0b) 13%, var(--card, #ffffff));
  color: var(--brand-text, #92610a);
  border: 1px solid color-mix(in srgb, var(--brand, #f59e0b) 30%, transparent);
}

/* OpenRouter Activity 细边框卡片体系 */
.oh-activity-card,
.oh-activity-stats-shell :deep(.card),
.oh-activity-charts-grid :deep(.card),
.oh-activity-table-shell :deep(.card) {
  background: var(--card, #ffffff);
  border: 1px solid var(--border, #e9e9ee);
  border-radius: var(--radius-md, 12px);
  box-shadow: none;
  transition: border-color 0.18s ease, background-color 0.18s ease;
}

.oh-activity-stats-shell :deep(.card:hover),
.oh-activity-charts-grid :deep(.card:hover) {
  border-color: color-mix(in srgb, var(--brand, #f59e0b) 32%, var(--border, #e9e9ee));
}

/* 强制统计卡片、图表与明细表格内的数字使用等宽对齐 (tabular-nums) */
.oh-activity-stats-shell :deep(.text-xl),
.oh-activity-stats-shell :deep(.font-bold),
.oh-activity-charts-grid :deep(.tabular-nums),
.oh-activity-table-shell :deep(td) {
  font-variant-numeric: tabular-nums;
}

.oh-activity-table-shell :deep(thead tr) {
  background: color-mix(in srgb, var(--surface-2, #f4f4f5) 68%, var(--card, #ffffff));
}

.oh-activity-table-shell :deep(thead th) {
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 0.02em;
  color: var(--muted-foreground, #6f6f78);
}

.oh-activity-table-shell :deep(tbody tr) {
  transition: background-color 0.16s ease;
}

.oh-activity-table-shell :deep(tbody tr:hover) {
  background: color-mix(in srgb, var(--brand, #f59e0b) 4.5%, transparent);
}
</style>
