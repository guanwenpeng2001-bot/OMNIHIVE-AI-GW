<template>
  <p
    v-if="summaryScope !== 'all_time'"
    class="oh-scope-banner text-xs text-[var(--muted-foreground)]"
    data-testid="summary-scope"
  >{{ summaryLabel }}<span v-if="summaryWindow"> · {{ summaryWindow }}</span></p>
  <!-- Row 1: Core Stats -->
  <div class="grid grid-cols-1 gap-4 min-[480px]:grid-cols-2 lg:grid-cols-4">
    <!-- Balance -->
    <div v-if="!isSimple" class="card oh-metric-card group relative overflow-hidden p-4">
      <div class="flex items-start justify-between gap-2">
        <p class="truncate text-xs font-medium text-[var(--muted-foreground)]">{{ t('dashboard.balance') }} (USD)</p>
        <span class="text-xs opacity-35" aria-hidden="true">›</span>
      </div>
      <div class="mt-2 flex items-end justify-between gap-2">
        <div class="min-w-0">
          <p class="font-mono text-xl font-bold tabular-nums tracking-tight text-emerald-600 dark:text-emerald-400" :title="`USD ${formatLedgerAmount(balance)}`">${{ formatBalance(balance) }}</p>
          <p class="mt-0.5 text-xs text-[var(--muted-foreground)]">{{ t('common.available') }}</p>
        </div>
      </div>
    </div>

    <!-- API Keys -->
    <div class="card oh-metric-card group relative overflow-hidden p-4">
      <div class="flex items-start justify-between gap-2">
        <p class="truncate text-xs font-medium text-[var(--muted-foreground)]">{{ t('dashboard.apiKeys') }}</p>
        <span class="text-xs opacity-35" aria-hidden="true">›</span>
      </div>
      <div class="mt-2 flex items-end justify-between gap-2">
        <div class="min-w-0">
          <p class="font-mono text-xl font-bold tabular-nums tracking-tight text-[var(--foreground)]">{{ stats?.total_api_keys || 0 }}</p>
          <p class="mt-0.5 flex items-center gap-1.5 text-xs text-emerald-600 dark:text-emerald-400">
            <span class="pulse-dot pulse-ok" aria-hidden="true" />
            <span class="tabular-nums">{{ stats?.active_api_keys || 0 }} {{ t('common.active') }}</span>
          </p>
        </div>
      </div>
    </div>

    <!-- Today Requests -->
    <div class="card oh-metric-card group relative overflow-hidden p-4">
      <div class="flex items-start justify-between gap-2">
        <p class="truncate text-xs font-medium text-[var(--muted-foreground)]">{{ t('dashboard.todayRequests') }}</p>
        <span class="text-xs opacity-35" aria-hidden="true">›</span>
      </div>
      <div class="mt-2 flex items-end justify-between gap-2">
        <div class="min-w-0">
          <p class="font-mono text-xl font-bold tabular-nums tracking-tight text-[var(--foreground)]">{{ stats?.today_requests || 0 }}</p>
          <p class="mt-0.5 text-xs tabular-nums text-[var(--muted-foreground)]">{{ summaryLabel }}: {{ summaryScope === 'unknown' ? '—' : formatNumber(stats.total_requests) }}</p>
        </div>
      </div>
    </div>

    <!-- Today Cost -->
    <div class="card oh-metric-card group relative overflow-hidden p-4">
      <div class="flex items-start justify-between gap-2">
        <p class="truncate text-xs font-medium text-[var(--muted-foreground)]">{{ t('dashboard.todayCost') }}</p>
        <span class="text-xs opacity-35" aria-hidden="true">›</span>
      </div>
      <div class="mt-2 flex items-end justify-between gap-2">
        <div class="min-w-0 flex-1">
          <p class="oh-cost-pair text-xl font-bold tabular-nums tracking-tight text-[var(--foreground)]">
            <span class="text-[var(--brand-text)]" :title="t('dashboard.actual')"><span class="inline-block">{{ t('dashboard.actual') }}</span> <span class="inline-block whitespace-nowrap">USD {{ formatCost(stats?.today_actual_cost) }}</span></span>
            <span class="text-sm font-normal text-[var(--muted-foreground)]" :title="t('dashboard.standard')"><span class="inline-block whitespace-nowrap"> / {{ t('dashboard.standard') }}</span> <span class="inline-block whitespace-nowrap">USD {{ formatCost(stats?.today_cost) }}</span></span>
          </p>
          <p class="oh-cost-pair mt-0.5 text-xs tabular-nums">
            <span class="text-[var(--muted-foreground)]">{{ summaryLabel }}: </span>
            <span class="text-[var(--brand-text)]" :title="t('dashboard.actual')"><span class="inline-block">{{ t('dashboard.actual') }}</span> <span class="inline-block whitespace-nowrap">USD {{ summaryScope === 'unknown' ? '—' : formatCost(stats?.total_actual_cost) }}</span></span>
            <span class="text-[var(--muted-foreground)]" :title="t('dashboard.standard')"><span class="inline-block whitespace-nowrap"> / {{ t('dashboard.standard') }}</span> <span class="inline-block whitespace-nowrap">USD {{ summaryScope === 'unknown' ? '—' : formatCost(stats?.total_cost) }}</span></span>
          </p>
        </div>
      </div>
    </div>
  </div>

  <!-- Row 2: Token Stats -->
  <div class="grid grid-cols-1 gap-4 min-[480px]:grid-cols-2 lg:grid-cols-4">
    <!-- Today Tokens -->
    <div class="card oh-metric-card group relative overflow-hidden p-4">
      <div class="flex items-start justify-between gap-2">
        <p class="truncate text-xs font-medium text-[var(--muted-foreground)]">{{ t('dashboard.todayTokens') }}</p>
        <span class="text-xs opacity-35" aria-hidden="true">›</span>
      </div>
      <div class="mt-2 flex items-end justify-between gap-2">
        <div class="min-w-0 flex-1">
          <p class="font-mono text-xl font-bold tabular-nums tracking-tight text-[var(--foreground)]">{{ formatTokens(stats?.today_tokens || 0) }}</p>
          <p class="mt-0.5 text-xs tabular-nums text-[var(--muted-foreground)]">{{ t('dashboard.input') }}: {{ formatTokens(stats?.today_input_tokens || 0) }} / {{ t('dashboard.output') }}: {{ formatTokens(stats?.today_output_tokens || 0) }} / {{ t('dashboard.cache') }}: {{ formatTokens((stats?.today_cache_creation_tokens || 0) + (stats?.today_cache_read_tokens || 0)) }}</p>
        </div>
      </div>
    </div>

    <!-- Total Tokens -->
    <div class="card oh-metric-card group relative overflow-hidden p-4">
      <div class="flex items-start justify-between gap-2">
        <p class="truncate text-xs font-medium text-[var(--muted-foreground)]">{{ summaryScope === 'all_time' ? t('dashboard.totalTokens') : summaryLabel + ' · Token' }}</p>
        <span class="text-xs opacity-35" aria-hidden="true">›</span>
      </div>
      <div class="mt-2 flex items-end justify-between gap-2">
        <div class="min-w-0 flex-1">
          <p class="text-xl font-bold tabular-nums tracking-tight text-[var(--foreground)]">{{ summaryScope === 'unknown' ? '—' : formatTokens(stats.total_tokens) }}</p>
          <p class="mt-0.5 text-xs tabular-nums text-[var(--muted-foreground)]">{{ t('dashboard.input') }}: {{ summaryScope === 'unknown' ? '—' : formatTokens(stats.total_input_tokens) }} / {{ t('dashboard.output') }}: {{ summaryScope === 'unknown' ? '—' : formatTokens(stats.total_output_tokens) }} / {{ t('dashboard.cache') }}: {{ summaryScope === 'unknown' ? '—' : formatTokens((stats.total_cache_creation_tokens) + (stats.total_cache_read_tokens)) }}</p>
        </div>
      </div>
    </div>

    <!-- Performance (RPM/TPM) -->
    <div class="card oh-metric-card group relative overflow-hidden p-4">
      <div class="flex items-start justify-between gap-2">
        <p class="truncate text-xs font-medium text-[var(--muted-foreground)]">{{ t('dashboard.performance') }}</p>
        <span class="text-xs opacity-35" aria-hidden="true">›</span>
      </div>
      <div class="mt-2 flex items-end justify-between gap-2">
        <div class="min-w-0 flex-1">
          <div class="flex items-baseline gap-2">
            <p class="text-xl font-bold tabular-nums tracking-tight text-[var(--foreground)]">{{ formatTokens(stats?.rpm || 0) }}</p>
            <span class="font-mono text-xs text-[var(--muted-foreground)]">RPM</span>
          </div>
          <div class="flex items-baseline gap-2">
            <p class="text-sm font-semibold tabular-nums text-violet-600 dark:text-violet-400">{{ formatTokens(stats?.tpm || 0) }}</p>
            <span class="font-mono text-xs text-[var(--muted-foreground)]">TPM</span>
          </div>
        </div>
      </div>
    </div>

    <!-- Avg Response Time -->
    <div class="card oh-metric-card group relative overflow-hidden p-4">
      <div class="flex items-start justify-between gap-2">
        <p class="truncate text-xs font-medium text-[var(--muted-foreground)]">{{ t('dashboard.avgResponse') }}</p>
        <span class="text-xs opacity-35" aria-hidden="true">›</span>
      </div>
      <div class="mt-2 flex items-end justify-between gap-2">
        <div class="min-w-0">
          <p class="text-xl font-bold tabular-nums tracking-tight text-[var(--foreground)]">{{ summaryScope === 'unknown' ? '—' : formatDuration(stats.average_duration_ms) }}</p>
          <p class="mt-0.5 text-xs text-[var(--muted-foreground)]">{{ summaryLabel }}</p>
        </div>
      </div>
    </div>
  </div>

  <!-- Row 3: Per-platform breakdown -->
  <div v-if="!isSimple && platformCards.length > 0" class="card p-4">
    <div class="mb-3 flex items-center justify-between">
      <div class="flex items-center gap-2">
        <h3 class="text-sm font-semibold text-[var(--foreground)]">{{ t('dashboard.platformBreakdown') }}</h3>
      </div>
      <span class="oh-platform-count-pill font-mono text-xs tabular-nums text-[var(--muted-foreground)]">
        {{ t('dashboard.platformCount', { count: platformCount }) }}
      </span>
    </div>
    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
      <div
        v-for="item in platformCards"
        :key="item.platform"
        data-testid="platform-card"
        :data-platform="item.platform"
        :class="[
          'oh-platform-card rounded-xl border p-3.5 transition-all',
          item.isOther
            ? 'border-dashed border-[var(--border)] bg-[var(--surface-2)]  '
            : 'border-[var(--border)] bg-[var(--card)]  '
        ]"
      >
        <div class="flex items-center justify-between">
          <span class="text-sm font-semibold text-[var(--foreground)]">
            {{ item.isOther ? t('dashboard.platformOther') : platformLabel(item.platform) }}
          </span>
          <span class="font-mono text-sm font-semibold tabular-nums text-amber-600 dark:text-amber-400" :title="t('dashboard.actual')">
            {{ summaryScope === 'unknown' ? '—' : `$${formatCost(item.total_actual_cost)}` }}
          </span>
        </div>
        <div class="oh-meter mt-2" aria-hidden="true">
          <span :style="{ width: platformSharePercent(item) + '%' }" />
        </div>
        <div class="mt-2.5 space-y-1 text-xs">
          <div class="flex items-center justify-between">
            <span class="text-[var(--muted-foreground)]">{{ t('dashboard.todayCost') }}</span>
            <span class="font-mono tabular-nums text-[var(--foreground)]">${{ formatCost(item.today_actual_cost) }}</span>
          </div>
          <div class="flex items-center justify-between">
            <span class="text-[var(--muted-foreground)]">{{ t('dashboard.requests') }}</span>
            <span class="font-mono tabular-nums text-[var(--foreground)]">
              {{ summaryScope === 'unknown' ? '—' : formatNumber(item.total_requests) }}
            </span>
          </div>
          <div class="flex items-center justify-between">
            <span class="text-[var(--muted-foreground)]">{{ t('dashboard.tokens') }}</span>
            <span class="font-mono tabular-nums text-[var(--foreground)]">
              {{ summaryScope === 'unknown' ? '—' : formatTokens(item.total_tokens) }}
            </span>
          </div>
        </div>

        <!-- Quota 区：仅当 quota 配置存在、非 __other__ 且至少有一个窗口配了 limit 时显示 -->
        <div v-if="hasAnyLimit(item.quota) && !item.isOther" class="mt-3 space-y-1.5 border-t border-[var(--border)] pt-2">
          <p class="text-[10px] uppercase tracking-wide text-[var(--muted-foreground)]">
            {{ t('dashboard.platformQuota.title') }}
          </p>
          <template v-for="w in (['daily', 'weekly', 'monthly'] as const)" :key="w">
            <div v-if="quotaVal(item.quota, `${w}_limit_usd`) != null" class="space-y-0.5">
              <!-- limit=0：完全禁用 -->
              <template v-if="(quotaVal(item.quota, `${w}_limit_usd`) as number) === 0">
                <div class="flex items-center justify-between text-xs">
                  <span class="text-[var(--muted-foreground)]">{{ t(`dashboard.platformQuota.${w}`) }}</span>
                  <span class="font-mono text-red-500">{{ t('dashboard.platformQuota.disabled') }}</span>
                </div>
                <div class="h-1.5 w-full overflow-hidden rounded-full bg-[var(--surface-2)]">
                  <div class="h-full w-full rounded-full bg-red-500" />
                </div>
              </template>
              <!-- limit>0：正常用量进度条 -->
              <template v-else>
                <div class="flex items-center justify-between text-xs">
                  <span class="text-[var(--muted-foreground)]">{{ t(`dashboard.platformQuota.${w}`) }}</span>
                  <span class="font-mono tabular-nums text-[var(--foreground)]">
                    ${{ formatUsd((quotaVal(item.quota, `${w}_usage_usd`) as number) ?? 0) }} / ${{ formatUsd(quotaVal(item.quota, `${w}_limit_usd`) as number) }}
                  </span>
                </div>
                <div class="h-1.5 w-full overflow-hidden rounded-full bg-[var(--surface-2)]">
                  <div
                    class="h-full rounded-full transition-all"
                    :class="quotaBarClass(calcPercent((quotaVal(item.quota, `${w}_usage_usd`) as number) ?? 0, quotaVal(item.quota, `${w}_limit_usd`) as number))"
                    :style="{ width: calcPercent((quotaVal(item.quota, `${w}_usage_usd`) as number) ?? 0, quotaVal(item.quota, `${w}_limit_usd`) as number) + '%' }"
                  />
                </div>
                <p v-if="quotaVal(item.quota, `${w}_window_resets_at`)" class="text-[10px] text-[var(--muted-foreground)]">
                  {{ t('dashboard.platformQuota.resetsAt', { time: formatResetTime(quotaVal(item.quota, `${w}_window_resets_at`) as string) }) }}
                </p>
              </template>
            </div>
          </template>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { usageScope } from '@/utils/usageScope'
import { formatReportDateTime, resolveReportTimeZone } from '@/utils/format'

import { formatBalanceAmount, formatLedgerAmount } from '@/utils/money'
import { CONCRETE_PLATFORM_OPTIONS, getPlatformLabel } from '@/constants/platforms'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlatformDashboardStats, UserDashboardStats as UserStatsType } from '@/api/usage'
import type { PlatformQuotaItem } from '@/types'

interface FusedPlatformCard {
  platform: string
  total_actual_cost: number
  today_actual_cost: number
  total_requests: number
  total_tokens: number
  isOther?: boolean
  quota?: PlatformQuotaItem
}

const props = defineProps<{
  timeZone?: string
  stats: UserStatsType
  balance: number
  isSimple: boolean
  platformQuotas?: PlatformQuotaItem[] | null
}>()
const { t } = useI18n()
const summaryScope = computed(() => usageScope(props.stats?.coverage))
const summaryLabel = computed(() => summaryScope.value === 'all_time' ? t('common.total')
  : t('dashboard.' + (summaryScope.value === 'retained' ? 'scopeRetained' : summaryScope.value === 'range' ? 'scopeRange' : 'scopeUnknown')))
const summaryWindow = computed(() => {
  const coverage = props.stats?.coverage
  if (!coverage?.window_from || !coverage.window_until || !['retained', 'range'].includes(summaryScope.value)) return ''
  const zone = props.timeZone || props.stats?.coverage?.timezone || resolveReportTimeZone()
  return t('dashboard.scopeFrom', { start: formatReportDateTime(coverage.window_from, zone) }) + ' · '
    + t('dashboard.scopeUntil', { end: formatReportDateTime(coverage.window_until, zone) })
})

// 平台名统一取自共享词条表 admin.accounts.platforms.*（getPlatformLabel），
// 避免本页自建一份缺平台的名表后回落到机器 id。
const platformLabel = (p: string) => getPlatformLabel(p, t)

// 处理"各平台之和 < 总值"的差值：后端按平台聚合时过滤了无法归属平台的行
// （group 与 account 都缺 platform）。这里把差值作为"其他"卡片显式展示，
// 避免 Row 1 总值与 Row 3 平台拆分加总对不上、用户困惑。
const OTHER_THRESHOLD = 0.0001
const platformCards = computed<FusedPlatformCard[]>(() => {
  // 建立 by_platform Map
  const byPlat = new Map<string, PlatformDashboardStats>()
  for (const item of props.stats?.by_platform ?? []) byPlat.set(item.platform, item)

  // 建立 quota Map。三档全空的记录不产生卡片，挂到卡片上也不渲染配额区。
  const byQuota = new Map<string, PlatformQuotaItem>()
  for (const q of props.platformQuotas ?? []) byQuota.set(q.platform, q)

  // 卡片集合 = 有用量的平台 ∪ 至少配置了一档限额的平台。
  // 三档全空的限额记录等价于不限额，不单独产生卡片。
  // 后端 by_platform / quota 接口均不会返回 platform='__other__'，
  // 无需显式排除；__other__ 由下方差值补差逻辑单独追加。
  const platforms = new Set<string>(byPlat.keys())
  for (const [platform, q] of byQuota) {
    if (hasAnyLimit(q)) platforms.add(platform)
  }

  const PLATFORM_ORDER: string[] = CONCRETE_PLATFORM_OPTIONS.map(option => option.value)
  const cards: FusedPlatformCard[] = []

  for (const p of platforms) {
    const stat = byPlat.get(p)
    cards.push({
      platform: p,
      total_actual_cost: stat?.total_actual_cost ?? 0,
      today_actual_cost: stat?.today_actual_cost ?? 0,
      total_requests: stat?.total_requests ?? 0,
      total_tokens: stat?.total_tokens ?? 0,
      quota: byQuota.get(p),
    })
  }

  // 排序：按 PLATFORM_ORDER，未知平台按名称排序
  cards.sort((a, b) => {
    const ai = PLATFORM_ORDER.indexOf(a.platform)
    const bi = PLATFORM_ORDER.indexOf(b.platform)
    if (ai === -1 && bi === -1) return a.platform.localeCompare(b.platform)
    if (ai === -1) return 1
    if (bi === -1) return -1
    return ai - bi
  })

  // __other__ 补差逻辑：只对 by_platform 有 usage 数据的总和计算
  const total = props.stats?.total_actual_cost ?? 0
  const today = props.stats?.today_actual_cost ?? 0
  const sumTotal = cards.reduce((s, c) => s + c.total_actual_cost, 0)
  const sumToday = cards.reduce((s, c) => s + c.today_actual_cost, 0)
  const diffTotal = Math.max(0, total - sumTotal)
  const diffToday = Math.max(0, today - sumToday)

  if (diffTotal > OTHER_THRESHOLD || diffToday > OTHER_THRESHOLD) {
    cards.push({
      platform: '__other__',
      total_actual_cost: diffTotal,
      today_actual_cost: diffToday,
      total_requests: 0,
      total_tokens: 0,
      isOther: true,
    })
  }

  return cards
})

// 标题右侧的平台计数 = 实际渲染的平台卡片数，不含"其他"差额卡。
const platformCount = computed(() => platformCards.value.filter((c) => !c.isOther).length)

const maxPlatformCost = computed(() => {
  const costs = platformCards.value.map((c) => c.total_actual_cost || c.today_actual_cost || 0)
  return Math.max(0.000001, ...costs)
})

function platformSharePercent(item: FusedPlatformCard): number {
  const val = item.total_actual_cost || item.today_actual_cost || 0
  if (val <= 0) return 0
  return Math.min(100, Math.max(0, (val / maxPlatformCost.value) * 100))
}

// Quota helpers

type QuotaWindow = 'daily' | 'weekly' | 'monthly'
type QuotaField = `${QuotaWindow}_limit_usd` | `${QuotaWindow}_usage_usd` | `${QuotaWindow}_window_resets_at`

function quotaVal(q: PlatformQuotaItem | undefined, key: QuotaField): PlatformQuotaItem[QuotaField] {
  return q?.[key]
}

function hasAnyLimit(q: PlatformQuotaItem | undefined): boolean {
  if (!q) return false
  return q.daily_limit_usd != null || q.weekly_limit_usd != null || q.monthly_limit_usd != null
}

function calcPercent(usage: number, limit: number): number {
  if (!limit || limit <= 0) return 0
  return Math.min(100, Math.max(0, Math.round((usage / limit) * 100)))
}

function quotaBarClass(p: number): string {
  if (p >= 95) return 'bg-red-500'
  if (p >= 75) return 'bg-amber-500'
  return 'bg-green-500'
}

// 与 formatBalance 一致使用 Intl.NumberFormat 做半偶舍入，避免 toFixed 在不同 JS 引擎
// 下偶发截断而非四舍五入（与后端展示精度不一致）。
const usdFormatter = new Intl.NumberFormat('en-US', {
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
})
function formatUsd(n: number): string {
  if (!Number.isFinite(n)) return '0.00'
  return usdFormatter.format(n)
}

function formatResetTime(iso: string | null | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString(undefined, {
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}

const formatBalance = formatBalanceAmount

const formatNumber = (n: number) => n.toLocaleString()
const formatCost = formatLedgerAmount
const formatTokens = (t: number) => {
  if (t >= 1_000_000) return `${(t / 1_000_000).toFixed(1)}M`
  if (t >= 1000) return `${(t / 1000).toFixed(1)}K`
  return t.toString()
}
const formatDuration = (ms: number) => ms >= 1000 ? `${(ms / 1000).toFixed(2)}s` : `${ms.toFixed(0)}ms`
</script>

<style scoped>
.oh-scope-banner {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 5px 11px;
  border-radius: 8px;
  border: 1px solid var(--border, #e9e9ee);
  background: color-mix(in srgb, var(--brand, #f59e0b) 8%, var(--card, #ffffff));
}

.dark .oh-scope-banner {
  border-color: var(--border, #232329);
  background: color-mix(in srgb, var(--brand, #fbbf24) 10%, var(--card, #131316));
}

.oh-metric-card {
  container-type: inline-size;
  min-width: 0;
  min-height: 8.25rem;
  border-radius: 8px;
  border: 1px solid var(--border, #e9e9ee);
  background: var(--card);
  color: var(--foreground);
  transition: border-color .15s;
}

.oh-metric-card p {
  word-break: keep-all;
  overflow-wrap: break-word;
}

.oh-metric-card .text-xl {
  font-size: clamp(1rem, 9cqi, 1.25rem);
  line-height: 1.4;
}

.oh-metric-card .text-xs {
  line-height: 1.65;
}

.oh-cost-pair {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 0.2rem 0.375rem;
}

.oh-cost-pair > span {
  max-width: 100%;
  word-break: keep-all;
  overflow-wrap: normal;
}

.dark .oh-metric-card {
  border-color: var(--border, #232329);
}

.oh-metric-card:hover {
  border-color: color-mix(in srgb, var(--brand, #f59e0b) 42%, var(--border, #e9e9ee));
}

.oh-micro-badge {
  display: inline-flex;
  align-items: center;
  padding: 1.5px 6px;
  border-radius: 5px;
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.04em;
  flex-shrink: 0;
}

.oh-micro-emerald {
  color: var(--ok-text, #15803d);
  background: color-mix(in srgb, var(--ok, #16a34a) 12%, transparent);
}

.oh-micro-amber {
  color: var(--brand-text, #92610a);
  background: color-mix(in srgb, var(--brand, #f59e0b) 14%, transparent);
}

.oh-micro-indigo {
  color: #4338ca;
  background: rgba(99, 102, 241, 0.12);
}

.oh-micro-rose {
  color: #be123c;
  background: rgba(244, 63, 94, 0.12);
}

.dark .oh-micro-emerald {
  color: #4ade80;
}

.dark .oh-micro-amber {
  color: #fbbf24;
}

.dark .oh-micro-indigo {
  color: #a5b4fc;
}

.dark .oh-micro-rose {
  color: #fda4af;
}


.pulse-dot {
  width: 6.5px;
  height: 6.5px;
  border-radius: 9999px;
  display: inline-block;
  flex: none;
}

.pulse-ok {
  background: var(--ok, #16a34a);
  color: var(--ok, #16a34a);
  animation: oh-stat-breath 2s cubic-bezier(0.4, 0, 0.6, 1) infinite;
}

@keyframes oh-stat-breath {
  0% {
    box-shadow: 0 0 0 0 color-mix(in srgb, currentColor 65%, transparent);
  }
  65% {
    box-shadow: 0 0 0 5px color-mix(in srgb, currentColor 0%, transparent);
  }
  100% {
    box-shadow: 0 0 0 0 color-mix(in srgb, currentColor 0%, transparent);
  }
}

.oh-platform-count-pill {
  padding: 2px 8px;
  border-radius: 9999px;
  border: 1px solid var(--border, #e9e9ee);
  background: var(--surface-2, #f4f4f5);
}

.dark .oh-platform-count-pill {
  border-color: var(--border, #232329);
  background: var(--surface-2, #1d1d21);
}

.oh-platform-card:hover {
  border-color: color-mix(in srgb, var(--brand, #f59e0b) 40%, var(--border, #e9e9ee));
}

.oh-meter {
  height: 5px;
  border-radius: 9999px;
  background: var(--surface-2, #f4f4f5);
  overflow: hidden;
}

.dark .oh-meter {
  background: var(--surface-2, #1d1d21);
}

.oh-meter > span {
  display: block;
  height: 100%;
  border-radius: 9999px;
  background: var(--brand);
}

@media (prefers-reduced-motion: reduce) {
  .pulse-ok { animation: none; }
  .oh-metric-card { transition: none; }
}
</style>
