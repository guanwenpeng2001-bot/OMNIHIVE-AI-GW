<template>
  <AppLayout>
    <ReportTimeContext :time-zone="reportTimeZone" class="mb-3" />
    <div class="space-y-6">
      <div v-if="loading" class="flex items-center justify-center py-12"><LoadingSpinner /></div>
      <template v-else>
        <UsageStatsState v-if="statsState !== 'known'" :state="statsState" />
        <UserDashboardStats v-if="stats" :stats="stats" :balance="user?.balance || 0" :is-simple="authStore.isSimpleMode" :platform-quotas="platformQuotas" />
        <div v-if="historyGap || chartError" data-testid="chart-state" class="card space-y-3 p-4">
          <DateRangePicker v-model:start-date="startDate" v-model:end-date="endDate" @change="loadCharts" />
          <Select v-model="granularity" :options="[{ value: 'day', label: t('dashboard.day') }, { value: 'hour', label: t('dashboard.hour') }]" @change="loadCharts" />
          <UsageStatsState :state="historyGap ? 'unknown' : 'error'" />
          <button class="btn btn-secondary" @click="loadCharts">{{ t('common.refresh') }}</button>
        </div>
        <UserDashboardCharts v-show="!historyGap && !chartError" :time-zone="reportTimeZone" v-model:startDate="startDate" v-model:endDate="endDate" v-model:granularity="granularity" :loading="loadingCharts" :trend="trendData" :models="modelStats" @dateRangeChange="loadCharts" @granularityChange="loadCharts" @refresh="refreshAll" />
        <div class="grid grid-cols-1 gap-6 lg:grid-cols-3">
          <div class="lg:col-span-2"><UsageStatsState :state="recentState"><UserDashboardRecentUsage :time-zone="reportTimeZone" :data="recentUsage" :loading="loadingUsage" /></UsageStatsState></div>
          <div class="lg:col-span-1"><UserDashboardQuickActions /></div>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { resolveReportTimeZone, formatReportDateInput } from '@/utils/format'
import ReportTimeContext from '@/components/charts/ReportTimeContext.vue'
const reportProps = defineProps<{ timeZone?: string }>()
const reportTimeZone = resolveReportTimeZone(reportProps.timeZone)
const reportQueryContext = { timezone: reportTimeZone }

import Select from '@/components/common/Select.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import UsageStatsState from '@/components/common/UsageStatsState.vue'
import { historyGapFromError, withTrendGaps, type HistoryGap } from '@/views/trendGaps'
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
import { ref, computed, onMounted, onUnmounted } from 'vue'; import { useAuthStore } from '@/stores/auth'; import { usageAPI, type UserDashboardStats as UserStatsType } from '@/api/usage'
import AppLayout from '@/components/layout/AppLayout.vue'; import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import UserDashboardStats from '@/components/user/dashboard/UserDashboardStats.vue'; import UserDashboardCharts from '@/components/user/dashboard/UserDashboardCharts.vue'
import UserDashboardRecentUsage from '@/components/user/dashboard/UserDashboardRecentUsage.vue'; import UserDashboardQuickActions from '@/components/user/dashboard/UserDashboardQuickActions.vue'
import type { UsageLog, TrendDataPoint, ModelStat, PlatformQuotaItem } from '@/types'
import { getMyPlatformQuotas } from '@/api/user'

const historyGap = ref<HistoryGap | null>(null)
const statsState = ref<'known' | 'unknown' | 'error'>('known')
const chartError = ref(false)
const recentState = ref<'known' | 'unknown' | 'error'>('known')
let statsRequestSequence = 0
let recentRequestSequence = 0
const authStore = useAuthStore(); const user = computed(() => authStore.user)
const stats = ref<UserStatsType | null>(null); const loading = ref(false); const loadingUsage = ref(false); const loadingCharts = ref(false)
const trendData = ref<TrendDataPoint[]>([]); const modelStats = ref<ModelStat[]>([]); const recentUsage = ref<UsageLog[]>([])
const platformQuotas = ref<PlatformQuotaItem[] | null>(null)

const startDate = ref(formatReportDateInput(new Date(Date.now() - 6 * 86400000), reportTimeZone)); const endDate = ref(formatReportDateInput(new Date(), reportTimeZone)); const granularity = ref('day')

const loadStats = async () => {
  const sequence = ++statsRequestSequence
  loading.value = true
  stats.value = null
  statsState.value = 'known'
  try {
    await authStore.refreshUser()
    const response = await usageAPI.getDashboardStats()
    if (sequence !== statsRequestSequence) return
    stats.value = response
  } catch (error) {
    if (sequence !== statsRequestSequence) return
    statsState.value = historyGapFromError(error) ? 'unknown' : 'error'
  } finally {
    if (sequence === statsRequestSequence) loading.value = false
  }
}
let chartRequestSequence = 0
const loadCharts = async () => {
  const sequence = ++chartRequestSequence
  loadingCharts.value = true
  historyGap.value = null
  chartError.value = false
  try {
    const [trend, models] = await Promise.all([
      usageAPI.getDashboardTrend({ ...reportQueryContext, start_date: startDate.value, end_date: endDate.value, granularity: granularity.value as 'day' | 'hour' }),
      usageAPI.getDashboardModels({ ...reportQueryContext, start_date: startDate.value, end_date: endDate.value }),
    ])
    if (sequence !== chartRequestSequence) return
    historyGap.value = null
    trendData.value = withTrendGaps(trend.trend || [], granularity.value)
    modelStats.value = models.models || []
  } catch (error) {
    if (sequence !== chartRequestSequence) return
    historyGap.value = historyGapFromError(error)
    chartError.value = !historyGap.value
    trendData.value = []
    modelStats.value = []
    console.error('Failed to load charts:', error)
  } finally {
    if (sequence === chartRequestSequence) loadingCharts.value = false
  }
}
const loadRecent = async () => {
  const sequence = ++recentRequestSequence
  loadingUsage.value = true
  recentUsage.value = []
  recentState.value = 'known'
  try {
    const response = await usageAPI.query({ ...reportQueryContext, start_date: startDate.value, end_date: endDate.value, page: 1, page_size: 100 })
    if (sequence !== recentRequestSequence) return
    recentUsage.value = response.items.slice(0, 5)
  } catch (error) {
    if (sequence !== recentRequestSequence) return
    recentState.value = historyGapFromError(error) ? 'unknown' : 'error'
  } finally {
    if (sequence === recentRequestSequence) loadingUsage.value = false
  }
}
const loadPlatformQuotas = async () => { try { const data = await getMyPlatformQuotas(); platformQuotas.value = data.platform_quotas ?? [] } catch (error) { console.warn('Failed to load platform quotas:', error); platformQuotas.value = [] } }
const refreshAll = () => { loadStats(); loadCharts(); loadRecent(); loadPlatformQuotas() }

onMounted(() => { refreshAll() })
onUnmounted(() => { statsRequestSequence++; chartRequestSequence++; recentRequestSequence++ })
</script>
