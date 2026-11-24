<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import PublicShell from '@/components/layout/PublicShell.vue'
import CatalogFilterBar from '@/components/catalog/CatalogFilterBar.vue'
import ModelOriginBadge from '@/components/catalog/ModelOriginBadge.vue'
import ModelPriceCell from '@/components/catalog/ModelPriceCell.vue'
import ModelStatusBadge from '@/components/catalog/ModelStatusBadge.vue'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import ErrorState from '@/components/ui/ErrorState.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import { useAuthStore } from '@/stores/auth'
import { useCatalogBrowser } from '@/composables/useCatalogBrowser'
import { getCatalogCopy } from '@/features/catalog/copy'
import {
  addRationals,
  currencyPrefixFor,
  formatExactRationalCurrency,
  parseNonNegativeDecimal,
  reduceRational,
  scaleRateToPerMillion,
  type ExactRational
} from '@/features/shared/pricePresentation'
import { extractApiErrorMessage } from '@/utils/apiError'

const { locale } = useI18n()
const isZh = computed(() => String(locale?.value ?? '').startsWith('zh'))
const copy = computed(() => getCatalogCopy(isZh.value))
const router = useRouter()
const authStore = useAuthStore()
const filterBarRef = ref<InstanceType<typeof CatalogFilterBar> | null>(null)

const {
  MAX_SNAPSHOT_RESET_ATTEMPTS,
  viewModels,
  displayedViewModels,
  nextCursor,
  asOf,
  loading,
  loadingMore,
  error,
  paginationError,
  paginationSnapshotExpired,
  snapshotResetAttempts,
  query,
  sort,
  availabilityState,
  billingFilter,
  originFilter,
  activeModalityPill,
  viewMode,
  selectedModalities,
  selectedProtocols,
  providers,
  providerOptions,
  compareIds,
  compareSet,
  activeFilters,
  loadPersistedBrowserState,
  load,
  retryPagination,
  toggleFilterByKey,
  selectAvailabilityState,
  selectBillingFilter,
  selectOriginFilter,
  selectModalityPill,
  setSort,
  setViewMode,
  submitSearch,
  resetFilters,
  clearAllFiltersIncludingAvailability,
  toggleCompare,
  clearCompare,
  compareLabel
} = useCatalogBrowser({
  isZh,
  defaultSort: 'price_input',
  defaultAvailability: 'live',
  pageLimit: 100
})

const estimatorModelId = ref('')
const estimatorInputMillions = ref(10)
const estimatorOutputMillions = ref(3)
const estimatorRunsPerMonth = ref(30)

const estimatorPresets = computed(() => [
  {
    id: 'starter',
    label: isZh.value ? '原型验证' : 'Prototype',
    inputM: 1,
    outputM: 0.5,
    runs: 30
  },
  {
    id: 'team',
    label: isZh.value ? '团队开发' : 'Team Coding',
    inputM: 10,
    outputM: 3,
    runs: 30
  },
  {
    id: 'production',
    label: isZh.value ? '生产集群' : 'Production',
    inputM: 50,
    outputM: 15,
    runs: 30
  }
])

const billingModeCards = computed(() => [
  {
    id: 'payg',
    badge: isZh.value ? '弹性按量' : 'Pay-as-you-go',
    tone: 'info' as const,
    title: isZh.value ? '按量计费 (Pay-as-you-go)' : 'Pay-as-you-go Metering',
    summary: isZh.value
      ? '按实际消耗的 Input / Output Token 精确十进制结算，零最低消费，每次请求均绑定冻结的费率快照版本。'
      : 'Exact decimal settlement per Input / Output token with zero minimum commit. Every request locks its active price version at dispatch.',
    highlights: isZh.value
      ? ['单次请求透明回执与成本对账', '支持按项目/密钥设置预算上限', '适合探索评估与波峰波谷生产流量']
      : ['Per-request receipt & cost reconciliation', 'Project and API key budget guardrails', 'Ideal for prototyping & bursty production workloads']
  },
  {
    id: 'subscription',
    badge: isZh.value ? '周期配额' : 'Subscription',
    tone: 'accent' as const,
    title: isZh.value ? '订阅套餐 (Subscription Plans)' : 'Subscription & Coding Plans',
    summary: isZh.value
      ? '固定月度/周期额度与阶梯折扣，内置 5 小时 / 日 / 周滑动窗口配额调度，助力研发团队平滑控制支出。'
      : 'Predictable recurring quotas with tiered discounts and 5h / daily / weekly rolling window protection for steady team usage.',
    highlights: isZh.value
      ? ['适配 Claude Code / Codex CLI / Cursor 高频编码', '可叠加多套餐自动路由与配额续期', '支持兑换码与自助在线续订']
      : ['Tailored for Claude Code, Codex CLI & Cursor workflows', 'Stackable plans with automatic quota routing', 'Supports redemption codes and self-serve renewals']
  },
  {
    id: 'byok',
    badge: isZh.value ? '自带凭据' : 'BYOK',
    tone: 'success' as const,
    title: isZh.value ? '自带上游凭据 (BYOK)' : 'BYOK Credential',
    summary: isZh.value
      ? '在工作空间直接托管自有上游 API Key 或企业账户，零加价复用网关统一协议转换、智能路由、审计与故障切换。'
      : 'Connect your own upstream provider keys or enterprise seats while inheriting unified routing, fallback, rate limits, and audit telemetry.',
    highlights: isZh.value
      ? ['上游 Token 费用直结您的供应商账单', '支持工作空间级权限与凭据隔离', '兼容多区域合规策略与零留存约束']
      : ['Upstream token spend settles directly with your vendor', 'Workspace-isolated credential vault and RBAC', 'Enforces regional routing & zero-retention policies']
  }
])

const estimatableModels = computed(() =>
  viewModels.value.filter(
    (vm) =>
      vm.billing.isEstimatable &&
      (vm.billing.isFree ||
        scaleRateToPerMillion(vm.billing.inputRate) !== null ||
        scaleRateToPerMillion(vm.billing.outputRate) !== null)
  )
)

watch(
  estimatableModels,
  (list) => {
    if (!list.length) return
    if (!list.some((vm) => vm.id === estimatorModelId.value)) {
      const firstPriced = list.find((vm) => !vm.billing.isFree) ?? list[0]
      estimatorModelId.value = firstPriced.id
    }
  },
  { immediate: true }
)

const selectedEstimatorModel = computed(
  () => estimatableModels.value.find((vm) => vm.id === estimatorModelId.value) ?? null
)

const sanitizedInputMillions = computed(() => {
  const numeric = Number(estimatorInputMillions.value)
  return Number.isFinite(numeric) && numeric >= 0 ? numeric : 0
})

const sanitizedOutputMillions = computed(() => {
  const numeric = Number(estimatorOutputMillions.value)
  return Number.isFinite(numeric) && numeric >= 0 ? numeric : 0
})

const sanitizedRunsPerMonth = computed(() => {
  const numeric = Number(estimatorRunsPerMonth.value)
  return Number.isFinite(numeric) && numeric >= 1 ? Math.min(365, Math.round(numeric)) : 1
})

const estimatorBreakdown = computed(() => {
  const target = selectedEstimatorModel.value
  const zeroRational: ExactRational = { num: 0n, den: 1n }
  if (!target) {
    return {
      state: 'unpriced' as const,
      currency: 'USD',
      currencyPrefix: '$',
      isFree: false,
      inputCost: zeroRational,
      outputCost: zeroRational,
      singleTotal: zeroRational,
      monthlyTotal: zeroRational
    }
  }
  const currency =
    target.billing.inputRate?.currency || target.billing.outputRate?.currency || 'USD'
  const currencyPrefix = currencyPrefixFor(currency)
  if (target.billing.isFree) {
    return {
      state: 'free' as const,
      currency,
      currencyPrefix,
      isFree: true,
      inputCost: zeroRational,
      outputCost: zeroRational,
      singleTotal: zeroRational,
      monthlyTotal: zeroRational
    }
  }
  if (!target.billing.isEstimatable || target.billing.isError || target.billing.isGroupVarying) {
    return {
      state: 'unpriced' as const,
      currency,
      currencyPrefix,
      isFree: false,
      inputCost: zeroRational,
      outputCost: zeroRational,
      singleTotal: zeroRational,
      monthlyTotal: zeroRational
    }
  }
  const inputMillions = parseNonNegativeDecimal(sanitizedInputMillions.value) ?? zeroRational
  const outputMillions = parseNonNegativeDecimal(sanitizedOutputMillions.value) ?? zeroRational
  const inputScaledRaw = scaleRateToPerMillion(target.billing.inputRate)
  const outputScaledRaw = scaleRateToPerMillion(target.billing.outputRate)
  const activeCurrencies = new Set<string>()
  if (inputMillions.num > 0n && inputScaledRaw?.currency) {
    activeCurrencies.add(inputScaledRaw.currency.trim().toUpperCase())
  }
  if (outputMillions.num > 0n && outputScaledRaw?.currency) {
    activeCurrencies.add(outputScaledRaw.currency.trim().toUpperCase())
  }
  const missingRequiredRate =
    (inputMillions.num > 0n && !inputScaledRaw) ||
    (outputMillions.num > 0n && !outputScaledRaw) ||
    (inputMillions.num === 0n && outputMillions.num === 0n && !inputScaledRaw && !outputScaledRaw) ||
    activeCurrencies.size > 1
  if (missingRequiredRate) {
    return {
      state: 'unpriced' as const,
      currency,
      currencyPrefix,
      isFree: false,
      inputCost: zeroRational,
      outputCost: zeroRational,
      singleTotal: zeroRational,
      monthlyTotal: zeroRational
    }
  }
  const inputScaled = inputScaledRaw ?? { ...zeroRational, currency }
  const outputScaled = outputScaledRaw ?? { ...zeroRational, currency }
  const inputCost = reduceRational(
    inputMillions.num * inputScaled.num,
    inputMillions.den * inputScaled.den
  )
  const outputCost = reduceRational(
    outputMillions.num * outputScaled.num,
    outputMillions.den * outputScaled.den
  )
  const singleTotal = addRationals(inputCost, outputCost)
  const monthlyTotal = reduceRational(
    singleTotal.num * BigInt(sanitizedRunsPerMonth.value),
    singleTotal.den
  )
  return {
    state: 'estimated' as const,
    currency,
    currencyPrefix,
    isFree: false,
    inputCost,
    outputCost,
    singleTotal,
    monthlyTotal
  }
})

function formatMoneyAmount(value: ExactRational): string {
  return formatExactRationalCurrency(value, estimatorBreakdown.value.currency, {
    minDecimals: 2,
    maxDecimals: 4,
    zeroAsFree: false,
    groupThousands: true
  })
}

function applyEstimatorPreset(preset: { inputM: number; outputM: number; runs: number }): void {
  estimatorInputMillions.value = preset.inputM
  estimatorOutputMillions.value = preset.outputM
  estimatorRunsPerMonth.value = preset.runs
}

function handleToggleFilter(
  kind: 'modality' | 'protocol' | 'provider' | 'region' | 'retention',
  value: string
): void {
  toggleFilterByKey(kind, value)
}

function handleEmptyClearFilters(): void {
  if (
    query.value.trim() ||
    billingFilter.value !== 'all' ||
    originFilter.value !== 'all' ||
    activeModalityPill.value !== 'all' ||
    providers.value.length > 0 ||
    selectedModalities.value.length > 0 ||
    selectedProtocols.value.length > 0
  ) {
    resetFilters()
    return
  }
  clearAllFiltersIncludingAvailability()
}

function openConsoleEmptyAction(): void {
  void router.push(authStore.isAuthenticated ? '/dashboard' : '/login')
}

function handleShortcut(event: KeyboardEvent): void {
  if (event.defaultPrevented) return
  if (!event.metaKey && !event.ctrlKey && !event.altKey && event.key === '/') {
    const activeEl = document.activeElement as HTMLElement | null
    const tag = activeEl?.tagName
    if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || activeEl?.isContentEditable) {
      return
    }
    event.preventDefault()
    filterBarRef.value?.focusSearch()
  }
}

onMounted(() => {
  loadPersistedBrowserState()
  if (typeof window !== 'undefined') {
    window.addEventListener('keydown', handleShortcut)
  }
  void load(true)
})

onBeforeUnmount(() => {
  if (typeof window !== 'undefined') {
    window.removeEventListener('keydown', handleShortcut)
  }
})
</script>

<template>
  <PublicShell>
    <header class="flex flex-wrap items-end justify-between gap-4 border-b border-oh-default pb-6">
      <div class="min-w-0">
        <p class="font-mono text-xs uppercase tracking-[0.18em] text-oh-muted">
          {{ isZh ? '透明计费 · PRICING' : 'TRANSPARENT PRICING' }}
        </p>
        <h1 class="mt-3 text-3xl font-semibold tracking-tight text-oh-primary sm:text-4xl">
          {{ isZh ? '计费方案与模型费率' : 'Pricing & Rate Directory' }}
        </h1>
        <p class="mt-2 max-w-[68ch] text-sm text-oh-secondary">
          {{
            isZh
              ? '所有费率直接映射自服务端当前生效的销售价格目录。未定价或参考价模型不会进入估算总额，参考价始终标明「非网关结算价」。'
              : 'Published sale rates mirror active gateway tariffs. Unpriced or reference-only models are excluded from calculator totals, and Community Catalog (pi) rates stay clearly labeled non-settlement.'
          }}
        </p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <RouterLink
          :to="
            compareIds.length
              ? { path: '/model-plaza/compare', query: { ids: compareIds.join(',') } }
              : '/model-plaza/compare'
          "
          class="oh-focus-ring inline-flex h-9 items-center rounded-oh-md border border-oh-default bg-oh-surface px-3.5 text-xs font-medium text-oh-primary hover:border-oh-strong hover:bg-oh-hover"
        >
          {{
            compareIds.length
              ? (isZh ? `横向对比 (${compareIds.length})` : `Compare (${compareIds.length})`)
              : (isZh ? '多模型对比' : 'Compare models')
          }}
        </RouterLink>
        <RouterLink
          to="/model-plaza"
          class="oh-focus-ring inline-flex h-9 items-center rounded-oh-md border border-oh-default bg-oh-surface px-3.5 text-xs font-medium text-oh-secondary hover:border-oh-strong hover:text-oh-primary"
        >
          {{ isZh ? '返回模型广场' : 'Browse Model Plaza' }}
        </RouterLink>
        <RouterLink
          :to="authStore.isAuthenticated ? '/purchase' : '/register'"
          class="oh-focus-ring inline-flex h-9 items-center rounded-oh-md bg-[var(--oh-accent,#f59e0b)] px-4 text-xs font-semibold text-slate-950 hover:opacity-90"
        >
          {{ authStore.isAuthenticated ? (isZh ? '控制台充值 / 购买套餐' : 'Purchase & Top Up') : (isZh ? '注册开始使用' : 'Get Started') }}
        </RouterLink>
      </div>
    </header>

    <section class="grid gap-4 py-6 lg:grid-cols-3" :aria-label="isZh ? '计费模式对比' : 'Billing modes'">
      <article
        v-for="card in billingModeCards"
        :key="card.id"
        class="flex min-w-0 flex-col justify-between rounded-oh-md border border-oh-default bg-oh-surface p-5"
      >
        <div>
          <Badge :tone="card.tone" size="xs" variant="subtle">{{ card.badge }}</Badge>
          <h2 class="mt-3 text-base font-semibold text-oh-primary">{{ card.title }}</h2>
          <p class="mt-2 text-xs leading-relaxed text-oh-secondary">{{ card.summary }}</p>
        </div>
        <ul class="mt-4 space-y-1.5 border-t border-oh-subtle pt-3 text-xs text-oh-secondary">
          <li v-for="point in card.highlights" :key="point" class="flex items-start gap-2">
            <span class="mt-1 h-1.5 w-1.5 shrink-0 rounded-full bg-[var(--oh-accent,#f59e0b)]" />
            <span>{{ point }}</span>
          </li>
        </ul>
      </article>
    </section>

    <section
      class="min-w-0 max-w-full rounded-oh-md border border-oh-default bg-oh-surface p-5"
      data-testid="pricing-estimator"
      :aria-label="isZh ? '月度 Token 成本估算器' : 'Monthly Token Cost Estimator'"
    >
      <div class="flex flex-wrap items-center justify-between gap-3 border-b border-oh-subtle pb-4">
        <div>
          <p class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">
            {{ isZh ? '成本规划 · COST CALCULATOR' : 'COST CALCULATOR' }}
          </p>
          <h2 class="mt-1 text-base font-semibold text-oh-primary">
            {{ isZh ? '实时用量与月度成本估算器' : 'Interactive Monthly Cost Estimator' }}
          </h2>
        </div>
        <div class="flex flex-wrap items-center gap-1.5">
          <span class="mr-1 font-mono text-[11px] text-oh-muted">
            {{ isZh ? '预设场景：' : 'Presets:' }}
          </span>
          <button
            v-for="preset in estimatorPresets"
            :key="preset.id"
            type="button"
            class="oh-focus-ring rounded-full border border-oh-default px-2.5 py-1 text-xs text-oh-secondary transition-colors hover:border-oh-strong hover:text-oh-primary"
            @click="applyEstimatorPreset(preset)"
          >
            {{ preset.label }}
          </button>
        </div>
      </div>

      <div v-if="estimatableModels.length > 0" class="mt-4 grid min-w-0 grid-cols-1 gap-6 lg:grid-cols-12">
        <div class="grid min-w-0 grid-cols-1 gap-4 sm:grid-cols-2 lg:col-span-7">
          <label class="flex min-w-0 flex-col gap-1.5 sm:col-span-2">
            <span class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">
              {{ isZh ? '选择估算模型（仅含已定价或免费销售模型）' : 'Target Model (Priced or Free Sale Rate)' }}
            </span>
            <select
              v-model="estimatorModelId"
              data-testid="pricing-estimator-model-select"
              class="oh-focus-ring h-9 w-full min-w-0 max-w-full truncate rounded-oh-md border border-oh-default bg-oh-canvas px-3 text-sm text-oh-primary hover:border-oh-strong"
            >
              <option v-for="item in estimatableModels" :key="item.id" :value="item.id">
                {{ item.displayName }} ({{ item.provider }})
              </option>
            </select>
          </label>

          <label class="flex min-w-0 flex-col gap-1.5">
            <span class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">
              {{ isZh ? '单次 / 单日输入 (百万 Tokens)' : 'Input Volume (Million Tokens)' }}
            </span>
            <input
              v-model.number="estimatorInputMillions"
              type="number"
              min="0"
              step="0.5"
              class="oh-focus-ring h-9 w-full min-w-0 max-w-full rounded-oh-md border border-oh-default bg-oh-canvas px-3 font-mono text-sm tabular-nums text-oh-primary"
            />
          </label>

          <label class="flex min-w-0 flex-col gap-1.5">
            <span class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">
              {{ isZh ? '单次 / 单日输出 (百万 Tokens)' : 'Output Volume (Million Tokens)' }}
            </span>
            <input
              v-model.number="estimatorOutputMillions"
              type="number"
              min="0"
              step="0.5"
              class="oh-focus-ring h-9 w-full min-w-0 max-w-full rounded-oh-md border border-oh-default bg-oh-canvas px-3 font-mono text-sm tabular-nums text-oh-primary"
            />
          </label>

          <label class="flex min-w-0 flex-col gap-1.5 sm:col-span-2">
            <div class="flex flex-wrap items-center justify-between gap-2">
              <span class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">
                {{ isZh ? '每月计费周期 / 天数倍率' : 'Cycles / Days per Month' }}
              </span>
              <span class="font-mono text-xs font-semibold tabular-nums text-oh-primary">
                ×{{ sanitizedRunsPerMonth }}
              </span>
            </div>
            <input
              v-model.number="estimatorRunsPerMonth"
              type="range"
              min="1"
              max="31"
              class="w-full min-w-0 max-w-full accent-[var(--oh-accent,#f59e0b)]"
            />
          </label>
        </div>

        <div class="flex min-w-0 flex-col justify-between rounded-oh-md border border-oh-default bg-oh-subtle p-4 lg:col-span-5">
          <div class="space-y-3">
            <div class="flex flex-wrap items-center justify-between gap-2">
              <span class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">
                {{ isZh ? '估算结果摘要' : 'Estimated Spend' }}
              </span>
              <Badge v-if="estimatorBreakdown.isFree" tone="success" size="xs" variant="subtle">
                {{ isZh ? '免费额度模型' : 'Free Tier' }}
              </Badge>
              <Badge v-else-if="estimatorBreakdown.state === 'unpriced'" tone="unknown" size="xs" variant="subtle">
                {{ isZh ? '暂未完整定价' : 'Unpriced' }}
              </Badge>
              <Badge v-else tone="info" size="xs" variant="subtle" mono>
                {{ sanitizedInputMillions }}M In · {{ sanitizedOutputMillions }}M Out
              </Badge>
            </div>

            <div class="grid grid-cols-2 gap-3">
              <div class="min-w-0 rounded-oh-sm border border-oh-subtle bg-oh-surface p-3">
                <p class="font-mono text-[10px] uppercase text-oh-muted">
                  {{ isZh ? '单次 / 单日预估成本' : 'Single-Cycle Estimate' }}
                </p>
                <p class="mt-1 break-words font-mono text-xl font-semibold tabular-nums text-oh-primary">
                  {{ estimatorBreakdown.state === 'unpriced' ? '—' : formatMoneyAmount(estimatorBreakdown.singleTotal) }}
                </p>
              </div>
              <div class="min-w-0 rounded-oh-sm border border-oh-accent/40 bg-oh-accent-subtle/40 p-3">
                <p class="font-mono text-[10px] uppercase text-oh-accent-text">
                  {{ isZh ? `月度预估 (×${sanitizedRunsPerMonth})` : `Monthly Est. (×${sanitizedRunsPerMonth})` }}
                </p>
                <p class="mt-1 break-words font-mono text-xl font-semibold tabular-nums text-oh-primary">
                  {{ estimatorBreakdown.state === 'unpriced' ? '—' : formatMoneyAmount(estimatorBreakdown.monthlyTotal) }}
                </p>
              </div>
            </div>

            <dl class="space-y-1.5 border-t border-oh-subtle pt-3 font-mono text-xs tabular-nums">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <dt class="text-oh-secondary">{{ isZh ? '输入 Token 费用' : 'Input token subtotal' }}</dt>
                <dd class="text-oh-primary">{{ estimatorBreakdown.state === 'unpriced' ? '—' : formatMoneyAmount(estimatorBreakdown.inputCost) }}</dd>
              </div>
              <div class="flex flex-wrap items-center justify-between gap-2">
                <dt class="text-oh-secondary">{{ isZh ? '输出 Token 费用' : 'Output token subtotal' }}</dt>
                <dd class="text-oh-primary">{{ estimatorBreakdown.state === 'unpriced' ? '—' : formatMoneyAmount(estimatorBreakdown.outputCost) }}</dd>
              </div>
            </dl>
          </div>

          <p class="mt-3 border-t border-oh-subtle pt-2.5 text-[11px] leading-relaxed text-oh-muted">
            {{
              isZh
                ? '估算基于当前公开基准销售单价（不包含参考价）；若命中提示词缓存、分时折扣或阶梯费率，实际账单可能进一步降低。'
                : 'Based on published sale rates only (reference prices excluded); prompt caching, time-of-day multipliers, or subscription discounts may further reduce actual spend.'
            }}
          </p>
        </div>
      </div>

      <p v-else class="mt-4 text-xs text-oh-muted">
        {{
          loading
            ? (isZh ? '正在加载模型费率目录以初始化估算器…' : 'Loading published model rates to initialize calculator…')
            : (isZh ? '当前筛选结果中暂无完整标价的模型，请清空下方筛选或查看「仅目录收录」模型。' : 'No estimatable sale-priced models in the current view; clear filters or switch availability.')
        }}
      </p>
    </section>

    <CatalogFilterBar
      ref="filterBarRef"
      :is-zh="isZh"
      :query="query"
      :availability-state="availabilityState"
      :billing-filter="billingFilter"
      :origin-filter="originFilter"
      :show-origin-filter="authStore.isAuthenticated"
      :active-modality-pill="activeModalityPill"
      :selected-modalities="selectedModalities"
      :selected-protocols="selectedProtocols"
      :selected-providers="providers"
      :provider-options="providerOptions"
      :sort="sort"
      :view-mode="viewMode"
      :show-favorites-toggle="false"
      :show-extended-filters="true"
      :show-governance-filters="false"
      availability-row-test-id="pricing-availability-filter"
      availability-test-id-prefix="pricing-availability-pill"
      :as-of="asOf"
      :active-filters="activeFilters"
      @update:query="query = $event"
      @update:sort="setSort"
      @update:view-mode="setViewMode"
      @select-availability="selectAvailabilityState"
      @select-billing="selectBillingFilter"
      @select-origin="selectOriginFilter"
      @select-modality-pill="selectModalityPill"
      @toggle-filter="handleToggleFilter"
      @search="submitSearch"
      @reset="resetFilters"
    />

    <Skeleton v-if="loading" class="py-4" :lines="10" :label="isZh ? '正在加载模型费率' : 'Loading pricing catalog'" />
    <ErrorState
      v-else-if="error"
      :title="isZh ? '费率目录暂时无法加载' : 'Pricing unavailable'"
      :message="extractApiErrorMessage(error, '')"
      :retry-label="isZh ? '重新加载' : 'Retry'"
      @retry="load(true)"
    />
    <template v-else-if="!displayedViewModels.length">
      <EmptyState
        :variant="activeFilters || availabilityState === 'live' ? 'filtered' : 'empty'"
        :title="
          availabilityState === 'live'
            ? copy.empty.liveNoMatchesTitle
            : activeFilters
              ? (isZh ? '未找到匹配该筛选条件的模型费率' : 'No model rates match these filters')
              : (isZh ? '暂未发布公开模型费率' : 'No public pricing published yet')
        "
        :description="
          availabilityState === 'live'
            ? copy.empty.liveNoMatchesDesc
            : activeFilters
              ? (isZh ? '请清空筛选条件或搜索其他模型名称。' : 'Clear active filters or try another search term.')
              : (isZh
                  ? '公开价格表直接映射自已发布的网关模型目录。管理员在控制台完成渠道定价与模型发布后，实时费率将自动生效。'
                  : 'Public pricing projects directly from published catalog rates. Once an administrator configures channel pricing and publishes models, rates appear here automatically.')
        "
        :action-label="
          activeFilters || availabilityState === 'live'
            ? undefined
            : (authStore.isAuthenticated ? (isZh ? '前往配置渠道定价' : 'Configure Pricing') : (isZh ? '管理员登录配置' : 'Sign in'))
        "
        :clear-filter-label="isZh ? '清除筛选' : 'Clear filters'"
        @action="openConsoleEmptyAction"
        @clear-filters="handleEmptyClearFilters"
      >
        <template v-if="availabilityState === 'live'" #actions>
          <div class="flex flex-wrap items-center justify-center gap-2 pt-1">
            <Button variant="secondary" @click="handleEmptyClearFilters">
              {{ copy.filters.resetFilters }}
            </Button>
            <Button
              data-testid="pricing-empty-view-catalog-only"
              variant="primary"
              @click="selectAvailabilityState('catalog_only')"
            >
              {{ copy.empty.viewCatalogOnly }}
            </Button>
            <RouterLink
              to="/docs#support"
              class="oh-focus-ring inline-flex h-9 items-center rounded-oh-md border border-oh-default bg-oh-surface px-3 text-xs font-medium text-oh-secondary hover:border-oh-strong hover:text-oh-primary"
            >
              {{ copy.empty.contactSupport }}
            </RouterLink>
          </div>
        </template>
      </EmptyState>
    </template>

    <template v-else>
      <div
        v-if="viewMode === 'table'"
        class="oh-focus-ring min-w-0 max-w-full overflow-x-auto rounded-oh-md border border-oh-default bg-oh-surface"
        tabindex="0"
        role="region"
        :aria-label="isZh ? '公开模型费率表' : 'Public pricing rate table'"
      >
        <table class="w-full min-w-[760px] border-collapse text-sm">
          <thead>
            <tr class="border-b border-oh-default bg-oh-subtle text-left font-mono text-[11px] uppercase tracking-wider text-oh-muted">
              <th class="px-4 py-2.5">{{ isZh ? '模型' : 'Model' }}</th>
              <th class="px-4 py-2.5">{{ isZh ? '计费状态' : 'Pricing Mode' }}</th>
              <th class="px-4 py-2.5">{{ isZh ? '供应商 · 上下文' : 'Provider · Context' }}</th>
              <th class="px-4 py-2.5 text-right">{{ isZh ? '输入 / 1M' : 'Input / 1M' }}</th>
              <th class="px-4 py-2.5 text-right">{{ isZh ? '输出 / 1M' : 'Output / 1M' }}</th>
              <th class="px-4 py-2.5 text-right">{{ isZh ? '对比' : 'Compare' }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="vm in displayedViewModels" :key="vm.id" class="border-b border-oh-subtle last:border-0 hover:bg-oh-hover">
              <td class="max-w-[18rem] break-all px-4 py-3 xl:max-w-[24rem]">
                <div class="flex flex-wrap items-center gap-2">
                  <RouterLink
                    :to="`/model-plaza/${encodeURIComponent(vm.id)}`"
                    class="oh-focus-ring break-all rounded-oh-xs font-medium text-oh-primary hover:text-oh-accent-text"
                  >
                    {{ vm.displayName }}
                  </RouterLink>
                  <ModelStatusBadge
                    :badge="vm.availability.badge"
                    test-id-prefix="pricing-availability-badge"
                  />
                  <ModelOriginBadge v-if="authStore.isAuthenticated" :badge="vm.origin.primaryBadge" />
                </div>
                <p class="mt-0.5 truncate font-mono text-[11px] text-oh-muted">{{ vm.model }}</p>
              </td>
              <td class="px-4 py-3">
                <Badge
                  :tone="vm.billing.modeBadge?.tone ?? 'unknown'"
                  size="xs"
                  variant="subtle"
                >
                  {{ vm.billing.modeBadge?.label ?? copy.billing.unpriced }}
                </Badge>
              </td>
              <td class="px-4 py-3 font-mono text-xs text-oh-secondary">
                {{ vm.provider }} · {{ vm.contextDisplay }}
              </td>
              <td class="px-4 py-3 text-right">
                <ModelPriceCell
                  :billing="vm.billing"
                  kind="input"
                  :is-zh="isZh"
                  :detail-to="`/model-plaza/${encodeURIComponent(vm.id)}`"
                  see-detail-test-id="pricing-see-detail-input"
                  reference-test-id="pricing-reference-rate-input"
                  @retry="load(true)"
                />
              </td>
              <td class="px-4 py-3 text-right">
                <ModelPriceCell
                  :billing="vm.billing"
                  kind="output"
                  :is-zh="isZh"
                  :detail-to="`/model-plaza/${encodeURIComponent(vm.id)}`"
                  see-detail-test-id="pricing-see-detail-output"
                  reference-test-id="pricing-reference-rate-output"
                  @retry="load(true)"
                />
              </td>
              <td class="px-4 py-3 text-right">
                <button
                  type="button"
                  class="oh-focus-ring rounded-oh-sm border px-2.5 py-1 text-xs transition-colors disabled:opacity-40"
                  :class="
                    compareSet.has(vm.id)
                      ? 'border-oh-accent bg-oh-accent-subtle font-medium text-oh-accent-text'
                      : 'border-oh-default text-oh-secondary hover:border-oh-strong hover:text-oh-primary'
                  "
                  :disabled="!compareSet.has(vm.id) && compareIds.length >= 3"
                  :aria-pressed="compareSet.has(vm.id)"
                  @click="toggleCompare(vm.id)"
                >
                  {{ compareSet.has(vm.id) ? (isZh ? '已加入对比' : 'In compare') : (isZh ? '+ 加入对比' : '+ Compare') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-else class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <div
          v-for="vm in displayedViewModels"
          :key="vm.id"
          class="flex min-w-0 flex-col justify-between rounded-oh-md border border-oh-default bg-oh-surface p-4 transition-colors hover:border-oh-strong"
        >
          <div class="flex min-w-0 flex-col gap-2">
            <div class="min-w-0">
              <RouterLink
                :to="`/model-plaza/${encodeURIComponent(vm.id)}`"
                class="oh-focus-ring block truncate font-medium text-oh-primary hover:text-oh-accent-text"
              >
                {{ vm.displayName }}
              </RouterLink>
              <p class="mt-0.5 truncate font-mono text-[11px] text-oh-muted">
                {{ vm.provider }} · {{ vm.contextDisplay }}
              </p>
            </div>
            <div class="flex flex-wrap items-center gap-1.5">
              <Badge :tone="vm.billing.modeBadge?.tone ?? 'unknown'" size="xs" variant="subtle">
                {{ vm.billing.modeBadge?.label ?? copy.billing.unpriced }}
              </Badge>
              <ModelStatusBadge
                :badge="vm.availability.badge"
                test-id-prefix="pricing-card-availability-badge"
              />
              <ModelOriginBadge v-if="authStore.isAuthenticated" :badge="vm.origin.primaryBadge" />
            </div>
          </div>
          <div class="mt-4 grid grid-cols-2 gap-3 border-t border-oh-subtle pt-3">
            <div class="min-w-0">
              <p class="font-mono text-[10px] uppercase text-oh-muted">{{ isZh ? '输入 / 1M' : 'Input / 1M' }}</p>
              <div class="mt-1 flex min-w-0 justify-start">
                <ModelPriceCell
                  :billing="vm.billing"
                  kind="input"
                  :is-zh="isZh"
                  compact
                  @retry="load(true)"
                />
              </div>
            </div>
            <div class="min-w-0">
              <p class="font-mono text-[10px] uppercase text-oh-muted">{{ isZh ? '输出 / 1M' : 'Output / 1M' }}</p>
              <div class="mt-1 flex min-w-0 justify-start">
                <ModelPriceCell
                  :billing="vm.billing"
                  kind="output"
                  :is-zh="isZh"
                  compact
                  @retry="load(true)"
                />
              </div>
            </div>
          </div>
        </div>
      </div>

    </template>

    <template v-if="!loading && !error">
      <ErrorState
        v-if="paginationError"
        compact
        class="mt-4"
        :title="
          paginationSnapshotExpired
            ? (isZh ? '目录快照已更新' : 'Catalog snapshot expired')
            : (isZh ? '加载更多费率失败' : 'Could not load more rates')
        "
        :message="
          paginationSnapshotExpired
            ? (isZh
                ? '模型目录已发布新快照，请刷新最新费率列表。'
                : 'A newer catalog snapshot was published. Reload to view the latest rates.')
            : extractApiErrorMessage(paginationError, '')
        "
        :retryable="!paginationSnapshotExpired || snapshotResetAttempts < MAX_SNAPSHOT_RESET_ATTEMPTS"
        :retry-label="
          paginationSnapshotExpired
            ? (isZh ? '刷新最新费率' : 'Reload latest rates')
            : (isZh ? '重试加载' : 'Retry')
        "
        @retry="retryPagination()"
      />
      <div v-if="nextCursor && !paginationError" class="mt-6 flex justify-center">
        <Button variant="secondary" :loading="loadingMore" @click="load(false)">
          {{ isZh ? '加载更多费率' : 'Load more rates' }}
        </Button>
      </div>
    </template>

    <section
      v-if="compareIds.length > 0"
      data-testid="models-compare-tray"
      class="sticky bottom-4 z-20 mt-4 flex flex-wrap items-center justify-between gap-3 rounded-oh-md border border-oh-accent/50 bg-oh-surface/95 px-4 py-2.5 shadow-lg backdrop-blur"
      role="region"
      :aria-label="isZh ? '快捷模型对比栏' : 'Quick model comparison bar'"
    >
      <div class="flex min-w-0 flex-wrap items-center gap-2 text-xs">
        <span class="font-mono font-semibold text-oh-accent-text">
          {{ isZh ? `已选对比 (${compareIds.length}/3)` : `Compare tray (${compareIds.length}/3)` }}
        </span>
        <span
          v-for="selectedId in compareIds"
          :key="selectedId"
          class="inline-flex max-w-full items-center gap-1.5 rounded-full border border-oh-default bg-oh-surface px-2.5 py-0.5 text-xs text-oh-primary"
        >
          <span class="max-w-[12rem] truncate">{{ compareLabel(selectedId) }}</span>
          <button
            type="button"
            class="oh-focus-ring rounded-full px-0.5 font-mono text-oh-muted hover:text-oh-primary"
            :aria-label="isZh ? `从对比移除 ${compareLabel(selectedId)}` : `Remove ${compareLabel(selectedId)} from comparison`"
            @click="toggleCompare(selectedId)"
          >
            ×
          </button>
        </span>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <button
          type="button"
          class="oh-focus-ring rounded-oh-xs text-xs text-oh-secondary hover:text-oh-primary hover:underline"
          @click="clearCompare"
        >
          {{ isZh ? '清空' : 'Clear' }}
        </button>
        <RouterLink
          :to="{ path: '/model-plaza/compare', query: { ids: compareIds.join(',') } }"
          class="oh-focus-ring inline-flex h-8 items-center rounded-oh-md bg-[var(--oh-accent,#f59e0b)] px-3 text-xs font-semibold text-slate-950 transition-opacity hover:opacity-90"
        >
          {{
            compareIds.length >= 2
              ? (isZh ? `横向对比 (${compareIds.length}) →` : `Compare (${compareIds.length}) →`)
              : (isZh ? '进入对比页添加更多 →' : 'Open Compare →')
          }}
        </RouterLink>
      </div>
    </section>
  </PublicShell>
</template>
