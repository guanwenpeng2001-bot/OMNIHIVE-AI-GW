<template>
  <div class="usage-window w-full min-w-0 space-y-1">
    <!-- Window stats row (above progress bar) -->
    <div
      v-if="showWindowStats && windowStats && (windowStats.requests > 0 || windowStats.tokens > 0)"
      class="mb-0.5 flex min-w-0 items-center"
    >
      <div class="usage-stats inline-flex min-w-0 max-w-full flex-wrap items-center gap-x-1.5 gap-y-0.5 rounded-md border border-[var(--border)] bg-[color-mix(in_srgb,var(--surface-2)_70%,transparent)] px-1.5 py-0.5 font-mono text-[10px] tabular-nums text-[var(--muted-foreground)]">
        <span>
          {{ formatRequests }} req
        </span>
        <span>
          {{ formatTokens }} tokens
        </span>
        <span :title="t('usage.accountBilled')">
          A ${{ formatAccountCost }}
        </span>
        <span
          v-if="windowStats?.user_cost != null"
          :title="t('usage.userBilled')"
        >
          U ${{ formatUserCost }}
        </span>
      </div>
    </div>

    <!-- Progress bar row -->
    <div class="usage-progress-row flex min-w-0 items-center gap-1">
      <!-- Label badge (label-width: fixed = 定宽居中, auto = 限宽截断左对齐) -->
      <span :class="[labelSizeClass, labelClass]" :title="label">
        {{ label }}
      </span>

      <!-- Progress bar container -->
      <div class="h-1.5 min-w-[44px] flex-1 overflow-hidden rounded-full bg-[var(--surface-2)] ring-1 ring-inset ring-[var(--border)]">
        <div
          :class="['h-full rounded-full transition-[width] duration-300 motion-reduce:transition-none', barClass]"
          :style="{ width: barWidth }"
        ></div>
      </div>

      <!-- Percentage -->
      <span :class="['w-[32px] shrink-0 whitespace-nowrap text-right font-mono text-[11px] font-semibold tabular-nums', textClass]">
        {{ displayPercent }}
      </span>

      <!-- Reset time -->
      <span v-if="shouldShowResetTime" class="shrink-0 whitespace-nowrap rounded px-0.5 font-mono text-[10px] tabular-nums text-[var(--muted-foreground)]" :title="resetsAt || undefined">
        {{ formatResetTime }}
      </span>
    </div>
    <div
      v-if="estimatedTotalCost != null"
      data-test="estimated-total-cost"
      class="break-words font-mono text-[10px] tabular-nums text-[var(--muted-foreground)]"
      :title="t('admin.accounts.usageWindow.estimatedTotalCostTooltip')"
    >
      {{ t('admin.accounts.usageWindow.estimatedTotalCost', { cost: estimatedTotalCost.toFixed(2) }) }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useIntervalFn } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import type { WindowStats } from '@/types'
import { formatCompactNumber } from '@/utils/format'

const props = withDefaults(
  defineProps<{
    label: string
    utilization: number // Percentage (0-100+)
    resetsAt?: string | null
    color: 'indigo' | 'emerald' | 'purple' | 'amber'
    windowStats?: WindowStats | null
    showWindowStats?: boolean
    estimatedTotalCost?: number | null
    showNowWhenIdle?: boolean
    remainingCapacity?: boolean
    /** fixed: 定宽居中徽章（账号页纵向对齐）；auto: 限宽截断左对齐（监控页组合标签） */
    labelWidth?: 'fixed' | 'auto'
  }>(),
  { labelWidth: 'fixed', showWindowStats: true }
)

const { t } = useI18n()

// Reactive clock for countdown — only runs when a reset time is shown,
// to avoid creating many idle timers across large account lists.
const now = ref(new Date())
const { pause: pauseClock, resume: resumeClock } = useIntervalFn(
  () => {
    now.value = new Date()
  },
  60_000,
  { immediate: false },
)
if (props.resetsAt) resumeClock()
watch(
  () => props.resetsAt,
  (val) => {
    if (val) {
      now.value = new Date()
      resumeClock()
    } else {
      pauseClock()
    }
  },
)

// Label background colors
const labelClass = 'border border-[var(--border)] bg-[var(--surface-2)] font-mono text-[10px] font-semibold text-[var(--foreground)]'

// Label badge width mode: fixed 定宽保证账号页纵向对齐；auto 限宽截断适配
// 监控页「Pro/7 天」类组合标签。百分比列在两种模式下保持不变。
const labelSizeClass = computed(() =>
  props.labelWidth === 'auto'
    ? 'max-w-[72px] shrink-0 truncate rounded px-1 py-0.5 text-left'
    : 'w-[32px] min-w-max shrink-0 whitespace-nowrap rounded px-0.5 py-0.5 text-center'
)

// Progress bar color based on utilization
const barClass = computed(() => {
  if (props.remainingCapacity) {
    if (props.utilization <= 20) {
      return 'bg-red-500'
    } else if (props.utilization <= 50) {
      return 'bg-amber-500'
    }
    return 'bg-green-500'
  }
  if (props.utilization >= 90) {
    return 'bg-red-500'
  } else if (props.utilization >= 75) {
    return 'bg-amber-500'
  } else {
    return 'bg-green-500'
  }
})

// Text color based on utilization
const textClass = computed(() => {
  if (props.remainingCapacity) {
    if (props.utilization <= 20) {
      return 'text-red-600'
    } else if (props.utilization <= 50) {
      return 'text-amber-600'
    }
    return 'text-gray-600'
  }
  if (props.utilization >= 90) {
    return 'text-red-600'
  } else if (props.utilization >= 75) {
    return 'text-amber-600'
  } else {
    return 'text-gray-600'
  }
})

// Bar width (capped at 100%)
const barWidth = computed(() => {
  return `${Math.min(Math.max(props.utilization, 0), 100)}%`
})

// Display percentage (cap at 999% for readability)
const displayPercent = computed(() => {
  const percent = Math.round(
    props.remainingCapacity
      ? Math.min(Math.max(props.utilization, 0), 100)
      : props.utilization
  )
  return percent > 999 ? '>999%' : `${percent}%`
})

const shouldShowResetTime = computed(() => {
  if (props.resetsAt) return true
  return Boolean(props.showNowWhenIdle && props.utilization <= 0)
})

// Format reset time
const formatResetTime = computed(() => {
  if (!props.resetsAt) {
    // Idle rolling window without a known reset time: treat as immediately available.
    return props.showNowWhenIdle && props.utilization <= 0 ? t('usage.resetNow') : '-'
  }

  const date = new Date(props.resetsAt)
  const diffMs = date.getTime() - now.value.getTime()

  // resetsAt 已过期：utilization>0 说明后端窗口数据还没刷新（active poll 没回写），
  // 显示「待刷新」以区别于真正可用的「现在」。
  if (diffMs <= 0) {
    return props.utilization > 0 ? t('usage.resetPending') : t('usage.resetNow')
  }

  const diffHours = Math.floor(diffMs / (1000 * 60 * 60))
  const diffMins = Math.floor((diffMs % (1000 * 60 * 60)) / (1000 * 60))

  if (diffHours >= 24) {
    const days = Math.floor(diffHours / 24)
    return `${days}d ${diffHours % 24}h`
  } else if (diffHours > 0) {
    return `${diffHours}h ${diffMins}m`
  } else {
    return `${diffMins}m`
  }
})

// Window stats formatters
const formatRequests = computed(() => {
  if (!props.windowStats) return ''
  return formatCompactNumber(props.windowStats.requests, { allowBillions: false })
})

const formatTokens = computed(() => {
  if (!props.windowStats) return ''
  return formatCompactNumber(props.windowStats.tokens)
})

const formatAccountCost = computed(() => {
  if (!props.windowStats) return '0.00'
  return props.windowStats.cost.toFixed(2)
})

const formatUserCost = computed(() => {
  if (!props.windowStats || props.windowStats.user_cost == null) return '0.00'
  return props.windowStats.user_cost.toFixed(2)
})

</script>

<style scoped>
.usage-window {
  container-type: inline-size;
}

@container (max-width: 175px) {
  .usage-progress-row {
    flex-wrap: wrap;
  }
}

.usage-stats > span {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  min-width: 0;
  max-width: 100%;
  overflow-wrap: anywhere;
}

.usage-stats > span + span::before {
  content: '';
  width: 2px;
  height: 2px;
  border-radius: 50%;
  background: var(--muted-foreground);
  opacity: 0.5;
}

.bg-green-500 { background-color: var(--ok); }
.bg-amber-500 { background-color: var(--warn); }
.bg-red-500 { background-color: var(--bad); }
.text-gray-600 { color: var(--muted-foreground); }
.text-amber-600 { color: var(--warn-text); }
.text-red-600 { color: var(--bad-text); }
</style>
