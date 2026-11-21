<script setup lang="ts">
import { extractApiErrorMessage } from '@/utils/apiError'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import PublicShell from '@/components/layout/PublicShell.vue'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'
import CopyButton from '@/components/ui/CopyButton.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import ErrorState from '@/components/ui/ErrorState.vue'
import Input from '@/components/ui/Input.vue'
import SegmentedControl from '@/components/ui/SegmentedControl.vue'
import Tooltip from '@/components/ui/Tooltip.vue'
import { useAuthStore } from '@/stores/auth'
import {
  formatContextTokens,
  formatPublicTimestamp,
  publicTokenPrice,
  publicValue
} from '@/features/public/format'
import {
  addRationals,
  currencyPrefixFor,
  formatExactRationalCurrency,
  parseNonNegativeDecimal,
  reduceRational,
  scaleRateToPerMillion,
  type ExactRational
} from '@/features/shared/pricePresentation'
import { listPublicModels, type ListParams, type PublicModelSummary } from '@/features/public/api'

type ViewMode = 'table' | 'cards'

const { locale } = useI18n()
const isZh = computed(() => String(locale?.value ?? '').startsWith('zh'))
const router = useRouter()
const authStore = useAuthStore()

const models = ref<PublicModelSummary[]>([])
const nextCursor = ref<string | null>(null)
const asOf = ref<string | null>(null)
const loading = ref(true)
const loadingMore = ref(false)
const error = ref<unknown>(null)
const query = ref('')
const sort = ref<NonNullable<ListParams['sort']>>('price_input')
const viewMode = ref<ViewMode>('table')
const selectedProviders = ref<string[]>([])
const selectedModalities = ref<string[]>([])
const providerOptions = ref<string[]>([])
const searchContainerRef = ref<HTMLElement | null>(null)
let requestSeq = 0
let appliedQuery = ''

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
    badge: isZh.value ? '自有凭据' : 'BYOK',
    tone: 'success' as const,
    title: isZh.value ? '租户自有凭据 (BYOK)' : 'Bring Your Own Key (BYOK)',
    summary: isZh.value
      ? '在工作空间直接托管自有上游 API Key 或企业账户，零加价复用网关统一协议转换、智能路由、审计与故障切换。'
      : 'Connect your own upstream provider keys or enterprise seats while inheriting unified routing, fallback, rate limits, and audit telemetry.',
    highlights: isZh.value
      ? ['上游 Token 费用直结您的供应商账单', '支持工作空间级权限与凭据隔离', '兼容多区域合规策略与零留存约束']
      : ['Upstream token spend settles directly with your vendor', 'Workspace-isolated credential vault and RBAC', 'Enforces regional routing & zero-retention policies']
  }
])

const estimatableModels = computed(() =>
  models.value.filter(
    (entry) =>
      entry.price_state === 'free' ||
      scaleRateToPerMillion(entry.input_rate) !== null ||
      scaleRateToPerMillion(entry.output_rate) !== null
  )
)

watch(
  estimatableModels,
  (list) => {
    if (!list.length) return
    if (!list.some((entry) => entry.id === estimatorModelId.value)) {
      const firstPriced = list.find((entry) => entry.price_state !== 'free') ?? list[0]
      estimatorModelId.value = firstPriced.id
    }
  },
  { immediate: true }
)

const selectedEstimatorModel = computed(() =>
  estimatableModels.value.find((entry) => entry.id === estimatorModelId.value) ?? null
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
  const currency = target.input_rate?.currency || target.output_rate?.currency || 'USD'
  const currencyPrefix = currencyPrefixFor(currency)
  if (target.price_state === 'free') {
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
  if (target.price_state === 'unpriced' || target.price_state === 'error') {
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
  const inputMillions =
    parseNonNegativeDecimal(sanitizedInputMillions.value) ?? zeroRational
  const outputMillions =
    parseNonNegativeDecimal(sanitizedOutputMillions.value) ?? zeroRational
  const inputScaledRaw = scaleRateToPerMillion(target.input_rate)
  const outputScaledRaw = scaleRateToPerMillion(target.output_rate)
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

function formatMoneyAmount(value: ExactRational, _prefix = '$'): string {
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

const modalityChips = computed(() => [
  { value: 'text', label: isZh.value ? '文本 Text' : 'Text' },
  { value: 'image', label: isZh.value ? '视觉 Image' : 'Image' },
  { value: 'audio', label: isZh.value ? '音频 Audio' : 'Audio' },
  { value: 'video', label: isZh.value ? '视频 Video' : 'Video' }
])

const activeFilters = computed(
  () =>
    selectedProviders.value.length > 0 ||
    selectedModalities.value.length > 0 ||
    query.value.trim().length > 0
)

async function load(reset = true): Promise<void> {
  if (!reset && (loading.value || loadingMore.value || !nextCursor.value)) return
  const current = ++requestSeq
  if (reset) {
    appliedQuery = query.value.trim()
    loading.value = true
    loadingMore.value = false
    error.value = null
  } else {
    loadingMore.value = true
  }
  try {
    const page = await listPublicModels({
      q: appliedQuery || undefined,
      provider: selectedProviders.value.length ? selectedProviders.value : undefined,
      modality: selectedModalities.value.length ? selectedModalities.value : undefined,
      sort: sort.value,
      cursor: reset ? undefined : nextCursor.value,
      limit: 100
    })
    if (current !== requestSeq) return
    if (!appliedQuery && !selectedProviders.value.length && !selectedModalities.value.length) {
      const seen = new Set(providerOptions.value)
      for (const item of page.items ?? []) {
        const prov = item.provider?.trim()
        if (prov && prov !== 'unknown') seen.add(prov)
      }
      providerOptions.value = [...seen].sort()
    }
    models.value = reset ? page.items ?? [] : [...models.value, ...(page.items ?? [])]
    nextCursor.value = page.next_cursor ?? null
    asOf.value = page.as_of ?? null
  } catch (err) {
    if (current === requestSeq) error.value = err
  } finally {
    if (current === requestSeq) {
      loading.value = false
      loadingMore.value = false
    }
  }
}

function togglePricingFilter(kind: 'modality' | 'provider', item: string): void {
  const target = kind === 'modality' ? selectedModalities : selectedProviders
  target.value = target.value.includes(item)
    ? target.value.filter((entry) => entry !== item)
    : [...target.value, item]
  void load()
}

function resetFilters(): void {
  query.value = ''
  selectedProviders.value = []
  selectedModalities.value = []
  sort.value = 'price_input'
  void load()
}

function rateText(model: PublicModelSummary, kind: 'input' | 'output'): string {
  if (model.price_state === 'free') return isZh.value ? '免费' : 'Free'
  const rate = kind === 'input' ? model.input_rate : model.output_rate
  return publicTokenPrice(rate, 'adaptive', isZh.value)
}

function priceStateBadge(model: PublicModelSummary): { label: string; tone: 'success' | 'info' | 'unknown' | 'warning' } {
  if (model.price_state === 'free') {
    return { label: isZh.value ? '免费额度' : 'Free', tone: 'success' }
  }
  if (model.price_state === 'priced' || model.input_rate || model.output_rate) {
    return { label: isZh.value ? '精确按量' : 'Per-Token', tone: 'info' }
  }
  return { label: isZh.value ? '未声明单价' : 'Unpriced', tone: 'unknown' }
}

function handleShortcut(event: KeyboardEvent): void {
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
    const tag = (document.activeElement as HTMLElement | null)?.tagName
    if (tag === 'INPUT' || tag === 'TEXTAREA') return
    event.preventDefault()
    searchContainerRef.value?.querySelector('input')?.focus()
  }
}

onMounted(() => {
  window.addEventListener('keydown', handleShortcut)
  void load()
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', handleShortcut)
})
</script>

<template>
  <PublicShell>
    <header class="flex flex-wrap items-end justify-between gap-4 border-b border-oh-default pb-6">
      <div>
        <p class="font-mono text-xs uppercase tracking-[0.18em] text-oh-muted">
          {{ isZh ? '透明计费 · PRICING' : 'PRICING' }}
        </p>
        <h1 class="mt-3 text-3xl font-semibold tracking-tight text-oh-primary sm:text-4xl">
          {{ isZh ? '透明、精确的百万 Token 定价' : 'Transparent, per-token pricing' }}
        </h1>
        <p class="mt-2 max-w-[68ch] text-sm text-oh-secondary">
          {{
            isZh
              ? '参考费率统一按每百万 (1M) Token 展示，十进制高精度结算；每次请求账单均严格遵循调用时冻结的费率版本。'
              : 'Reference rates per one million tokens with exact decimal settlement. Your gateway invoice always follows the frozen price version of each request.'
          }}
        </p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <RouterLink
          to="/model-plaza"
          class="oh-focus-ring inline-flex h-9 items-center rounded-oh-md border border-oh-default bg-oh-surface px-3.5 text-xs font-medium text-oh-primary hover:border-oh-strong hover:bg-oh-hover"
        >
          {{ isZh ? '浏览模型详情' : 'Explore models' }}
        </RouterLink>
      </div>
    </header>

    <section class="mt-6 grid gap-3 md:grid-cols-3" :aria-label="isZh ? '计费模式说明' : 'Billing models overview'">
      <article
        v-for="mode in billingModeCards"
        :key="mode.id"
        class="flex min-w-0 flex-col justify-between rounded-oh-md border border-oh-default bg-oh-surface p-4"
      >
        <div>
          <div class="flex items-center justify-between gap-2">
            <h2 class="text-sm font-semibold text-oh-primary">{{ mode.title }}</h2>
            <Badge :tone="mode.tone" size="xs" variant="subtle">{{ mode.badge }}</Badge>
          </div>
          <p class="mt-2 text-xs leading-relaxed text-oh-secondary">{{ mode.summary }}</p>
        </div>
        <ul class="mt-3 space-y-1.5 border-t border-oh-subtle pt-3 text-xs text-oh-secondary">
          <li v-for="point in mode.highlights" :key="point" class="flex items-start gap-1.5">
            <span class="mt-0.5 font-mono text-[10px] text-oh-accent-text" aria-hidden="true">✓</span>
            <span>{{ point }}</span>
          </li>
        </ul>
      </article>
    </section>

    <section
      data-testid="pricing-estimator"
      class="mt-6 rounded-oh-md border border-oh-default bg-oh-surface p-4 sm:p-5"
      :aria-label="isZh ? '交互式 Token 费用估算器' : 'Interactive token cost estimator'"
    >
      <div class="flex flex-wrap items-start justify-between gap-3 border-b border-oh-subtle pb-4">
        <div class="min-w-0">
          <p class="font-mono text-[11px] uppercase tracking-[0.16em] text-oh-muted">
            {{ isZh ? '实时预算模拟 · COST CALCULATOR' : 'COST CALCULATOR' }}
          </p>
          <h2 class="mt-1 text-base font-semibold text-oh-primary">
            {{ isZh ? '交互式 Token 费用估算器' : 'Interactive Token Cost Estimator' }}
          </h2>
          <p class="mt-1 text-xs text-oh-secondary">
            {{
              isZh
                ? '选择目录内任意已定价模型，拖动或输入百万 Input / Output Tokens 数量，实时预览单次批次与月度预估费用。'
                : 'Pick any published model and adjust million Input / Output tokens to estimate per-batch and monthly gateway spend.'
            }}
          </p>
        </div>
        <div class="flex flex-wrap items-center gap-1.5">
          <span class="font-mono text-[11px] text-oh-muted">{{ isZh ? '快速预设：' : 'Presets:' }}</span>
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

      <div v-if="estimatableModels.length > 0" class="mt-4 grid gap-5 lg:grid-cols-[minmax(0,1.25fr)_minmax(0,1fr)]">
        <div class="min-w-0 space-y-4">
          <label class="block space-y-1.5">
            <span class="block font-mono text-[11px] uppercase tracking-wider text-oh-muted">
              {{ isZh ? '选择估算模型' : 'Target Model' }}
            </span>
            <select
              v-model="estimatorModelId"
              class="oh-focus-ring h-9 w-full rounded-oh-md border border-oh-default bg-oh-subtle px-3 font-mono text-xs text-oh-primary hover:border-oh-strong"
            >
              <option v-for="entry in estimatableModels" :key="entry.id" :value="entry.id">
                {{ entry.display_name || entry.model }} ({{ publicValue(entry.provider, 'N/A') }} · In {{ rateText(entry, 'input') }} / Out {{ rateText(entry, 'output') }})
              </option>
            </select>
          </label>

          <div class="grid gap-4 sm:grid-cols-2">
            <div class="space-y-2 rounded-oh-sm border border-oh-subtle bg-oh-subtle/40 p-3">
              <div class="flex items-center justify-between gap-2">
                <label for="calc-input-m" class="text-xs font-medium text-oh-secondary">
                  {{ isZh ? 'Input Tokens (百万 / 1M)' : 'Input Tokens (Millions)' }}
                </label>
                <input
                  id="calc-input-m"
                  v-model.number="estimatorInputMillions"
                  type="number"
                  min="0"
                  max="10000"
                  step="0.5"
                  class="oh-focus-ring h-7 w-24 rounded-oh-xs border border-oh-default bg-oh-surface px-2 text-right font-mono text-xs tabular-nums text-oh-primary"
                />
              </div>
              <input
                v-model.number="estimatorInputMillions"
                type="range"
                min="0"
                max="200"
                step="0.5"
                :aria-label="isZh ? '拖动调整 Input 百万 Tokens' : 'Slide input million tokens'"
                class="w-full accent-[var(--oh-accent,#f59e0b)]"
              />
              <div class="flex justify-between font-mono text-[10px] text-oh-muted">
                <span>0M</span>
                <span>{{ isZh ? '单价' : 'Rate' }}: {{ selectedEstimatorModel ? rateText(selectedEstimatorModel, 'input') : '—' }}/1M</span>
                <span>200M</span>
              </div>
            </div>

            <div class="space-y-2 rounded-oh-sm border border-oh-subtle bg-oh-subtle/40 p-3">
              <div class="flex items-center justify-between gap-2">
                <label for="calc-output-m" class="text-xs font-medium text-oh-secondary">
                  {{ isZh ? 'Output Tokens (百万 / 1M)' : 'Output Tokens (Millions)' }}
                </label>
                <input
                  id="calc-output-m"
                  v-model.number="estimatorOutputMillions"
                  type="number"
                  min="0"
                  max="10000"
                  step="0.5"
                  class="oh-focus-ring h-7 w-24 rounded-oh-xs border border-oh-default bg-oh-surface px-2 text-right font-mono text-xs tabular-nums text-oh-primary"
                />
              </div>
              <input
                v-model.number="estimatorOutputMillions"
                type="range"
                min="0"
                max="100"
                step="0.5"
                :aria-label="isZh ? '拖动调整 Output 百万 Tokens' : 'Slide output million tokens'"
                class="w-full accent-[var(--oh-accent,#f59e0b)]"
              />
              <div class="flex justify-between font-mono text-[10px] text-oh-muted">
                <span>0M</span>
                <span>{{ isZh ? '单价' : 'Rate' }}: {{ selectedEstimatorModel ? rateText(selectedEstimatorModel, 'output') : '—' }}/1M</span>
                <span>100M</span>
              </div>
            </div>
          </div>

          <div class="flex flex-wrap items-center justify-between gap-2 rounded-oh-sm border border-oh-subtle bg-oh-subtle/40 px-3 py-2 text-xs">
            <label for="calc-runs-month" class="text-oh-secondary">
              {{ isZh ? '月度折算周期（按上述用量每月重复次数 / 天数）' : 'Monthly multiplier (cycles / active days per month)' }}
            </label>
            <div class="flex items-center gap-2">
              <input
                id="calc-runs-month"
                v-model.number="estimatorRunsPerMonth"
                type="number"
                min="1"
                max="365"
                step="1"
                class="oh-focus-ring h-7 w-20 rounded-oh-xs border border-oh-default bg-oh-surface px-2 text-right font-mono text-xs tabular-nums text-oh-primary"
              />
              <span class="font-mono text-[11px] text-oh-muted">{{ isZh ? '次/月' : '× / mo' }}</span>
            </div>
          </div>
        </div>

        <div class="flex min-w-0 flex-col justify-between rounded-oh-md border border-oh-default bg-oh-subtle/50 p-4">
          <div class="space-y-3">
            <div class="flex items-center justify-between gap-2">
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
              <div class="rounded-oh-sm border border-oh-subtle bg-oh-surface p-3">
                <p class="font-mono text-[10px] uppercase text-oh-muted">
                  {{ isZh ? '单次 / 单日预估成本' : 'Single-Cycle Estimate' }}
                </p>
                <p class="mt-1 font-mono text-xl font-semibold tabular-nums text-oh-primary">
                  {{ estimatorBreakdown.state === 'unpriced' ? '—' : formatMoneyAmount(estimatorBreakdown.singleTotal, estimatorBreakdown.currencyPrefix) }}
                </p>
              </div>
              <div class="rounded-oh-sm border border-oh-accent/40 bg-oh-accent-subtle/40 p-3">
                <p class="font-mono text-[10px] uppercase text-oh-accent-text">
                  {{ isZh ? `月度预估 (×${sanitizedRunsPerMonth})` : `Monthly Est. (×${sanitizedRunsPerMonth})` }}
                </p>
                <p class="mt-1 font-mono text-xl font-semibold tabular-nums text-oh-primary">
                  {{ estimatorBreakdown.state === 'unpriced' ? '—' : formatMoneyAmount(estimatorBreakdown.monthlyTotal, estimatorBreakdown.currencyPrefix) }}
                </p>
              </div>
            </div>

            <dl class="space-y-1.5 border-t border-oh-subtle pt-3 font-mono text-xs tabular-nums">
              <div class="flex items-center justify-between gap-2">
                <dt class="text-oh-secondary">{{ isZh ? '输入 Token 费用' : 'Input token subtotal' }}</dt>
                <dd class="text-oh-primary">{{ estimatorBreakdown.state === 'unpriced' ? '—' : formatMoneyAmount(estimatorBreakdown.inputCost, estimatorBreakdown.currencyPrefix) }}</dd>
              </div>
              <div class="flex items-center justify-between gap-2">
                <dt class="text-oh-secondary">{{ isZh ? '输出 Token 费用' : 'Output token subtotal' }}</dt>
                <dd class="text-oh-primary">{{ estimatorBreakdown.state === 'unpriced' ? '—' : formatMoneyAmount(estimatorBreakdown.outputCost, estimatorBreakdown.currencyPrefix) }}</dd>
              </div>
            </dl>
          </div>

          <p class="mt-3 border-t border-oh-subtle pt-2.5 text-[11px] leading-relaxed text-oh-muted">
            {{
              isZh
                ? '估算基于当前公开基准单价；若命中提示词缓存、分时折扣或阶梯费率，实际账单可能进一步降低。'
                : 'Based on published base per-token rates; prompt caching, time-of-day multipliers, or subscription discounts may further reduce actual spend.'
            }}
          </p>
        </div>
      </div>

      <p v-else class="mt-4 text-xs text-oh-muted">
        {{
          loading
            ? (isZh ? '正在加载模型费率目录以初始化估算器…' : 'Loading published model rates to initialize calculator…')
            : (isZh ? '当前筛选结果中暂无按百万 Token 标价的模型，请清空下方筛选或待管理员发布模型费率。' : 'No per-token priced models in the current view; clear filters or wait for catalog rates.')
        }}
      </p>
    </section>

    <section class="flex flex-col gap-3.5 py-6">
      <div class="flex flex-wrap items-center gap-3">
        <div ref="searchContainerRef" class="w-full sm:w-80">
          <Input
            v-model="query"
            type="search"
            clearable
            :placeholder="isZh ? '筛选模型或供应商费率… (⌘K)' : 'Filter model or provider rates... (⌘K)'"
            @keydown.enter="load()"
            @clear="load()"
          />
        </div>
        <Button variant="secondary" @click="load()">{{ isZh ? '筛选' : 'Filter' }}</Button>

        <div class="ml-auto flex flex-wrap items-center gap-2">
          <label class="sr-only" for="pricing-sort">{{ isZh ? '排序方式' : 'Sort pricing' }}</label>
          <select
            id="pricing-sort"
            v-model="sort"
            class="oh-focus-ring h-9 rounded-oh-md border border-oh-default bg-oh-surface px-3 font-mono text-xs text-oh-primary hover:border-oh-strong"
            @change="load()"
          >
            <option value="price_input">{{ isZh ? '输入单价从低到高' : 'Lowest Input Rate' }}</option>
            <option value="name">{{ isZh ? '按模型名称' : 'Model Name' }}</option>
            <option value="context">{{ isZh ? '按上下文容量' : 'Context Window' }}</option>
          </select>

          <SegmentedControl
            :model-value="viewMode"
            :options="[
              { value: 'table', label: isZh ? '费率表' : 'Table' },
              { value: 'cards', label: isZh ? '卡片' : 'Cards' }
            ]"
            @update:model-value="viewMode = $event as ViewMode"
          />
        </div>
      </div>

      <div class="flex flex-wrap items-center gap-1.5">
        <span class="mr-1 font-mono text-[11px] uppercase tracking-wider text-oh-muted">
          {{ isZh ? '模态' : 'Modality' }}
        </span>
        <button
          v-for="chip in modalityChips"
          :key="chip.value"
          type="button"
          class="oh-focus-ring rounded-full border px-2.5 py-1 font-mono text-xs transition-colors"
          :class="
            selectedModalities.includes(chip.value)
              ? 'border-oh-accent bg-oh-accent-subtle font-medium text-oh-accent-text'
              : 'border-oh-default text-oh-secondary hover:border-oh-strong'
          "
          :aria-pressed="selectedModalities.includes(chip.value)"
          @click="togglePricingFilter('modality', chip.value)"
        >
          {{ chip.label }}
        </button>

        <template v-if="providerOptions.length > 0">
          <span class="ml-2 mr-1 font-mono text-[11px] uppercase tracking-wider text-oh-muted">
            {{ isZh ? '供应商' : 'Provider' }}
          </span>
          <button
            v-for="provider in providerOptions"
            :key="provider"
            type="button"
            class="oh-focus-ring rounded-full border px-2.5 py-1 font-mono text-xs transition-colors"
            :class="
              selectedProviders.includes(provider)
                ? 'border-oh-accent bg-oh-accent-subtle font-medium text-oh-accent-text'
                : 'border-oh-default text-oh-secondary hover:border-oh-strong'
            "
            :aria-pressed="selectedProviders.includes(provider)"
            @click="togglePricingFilter('provider', provider)"
          >
            {{ provider }}
          </button>
        </template>

        <button
          v-if="activeFilters"
          type="button"
          class="oh-focus-ring ml-1 rounded-oh-sm text-xs font-medium text-oh-accent-text underline-offset-2 hover:underline"
          @click="resetFilters"
        >
          {{ isZh ? '重置筛选' : 'Clear filters' }}
        </button>

        <span v-if="asOf" class="ml-auto font-mono text-[11px] text-oh-muted">
          {{ isZh ? `更新于 ${formatPublicTimestamp(asOf)}` : `Updated ${formatPublicTimestamp(asOf)}` }}
        </span>
      </div>
    </section>

    <Skeleton v-if="loading" class="py-4" :lines="10" :label="isZh ? '正在加载模型费率' : 'Loading pricing catalog'" />
    <ErrorState
      v-else-if="error"
      :title="isZh ? '费率目录暂时无法加载' : 'Pricing unavailable'"
      :message="extractApiErrorMessage(error, '')"
      :retry-label="isZh ? '重新加载' : 'Retry'"
      @retry="load()"
    />
    <EmptyState
      v-else-if="!models.length"
      :variant="activeFilters ? 'filtered' : 'empty'"
      :title="
        activeFilters
          ? (isZh ? '未找到匹配该筛选条件的模型费率' : 'No model rates match these filters')
          : (isZh ? '暂未发布公开模型费率' : 'No public pricing published yet')
      "
      :description="
        activeFilters
          ? (isZh ? '请清空筛选条件或搜索其他模型名称。' : 'Clear active filters or try another search term.')
          : (isZh
              ? '公开价格表直接映射自已发布的网关模型目录。管理员在控制台完成渠道分组定价与模型广场上架后，实时费率将自动生效。'
              : 'Public pricing projects directly from published catalog rates. Once an administrator configures channel pricing and publishes models, rates appear here automatically.')
      "
      :action-label="
        activeFilters
          ? undefined
          : (authStore.isAuthenticated ? (isZh ? '前往控制台配置定价' : 'Open Console') : (isZh ? '登录控制台' : 'Sign in to Console'))
      "
      :clear-filter-label="isZh ? '清空筛选' : 'Reset filters'"
      @action="router.push(authStore.isAuthenticated ? '/dashboard' : '/login')"
      @clear-filters="resetFilters"
    />

    <template v-else>
      <div
        v-if="viewMode === 'table'"
        class="oh-focus-ring overflow-x-auto rounded-oh-md border border-oh-default bg-oh-surface"
        tabindex="0"
        role="region"
        :aria-label="isZh ? '模型定价一览表' : 'Model pricing table'"
      >
        <table class="w-full min-w-[720px] border-collapse text-sm">
          <thead>
            <tr class="border-b border-oh-default bg-oh-subtle text-left font-mono text-[11px] uppercase tracking-wider text-oh-muted">
              <th class="px-4 py-2.5">{{ isZh ? '模型' : 'Model' }}</th>
              <th class="px-4 py-2.5">{{ isZh ? '计费状态' : 'Billing Mode' }}</th>
              <th class="px-4 py-2.5 text-right">{{ isZh ? '上下文' : 'Context' }}</th>
              <th class="px-4 py-2.5 text-right">{{ isZh ? '输入 / 1M Tokens' : 'Input / 1M' }}</th>
              <th class="px-4 py-2.5 text-right">{{ isZh ? '输出 / 1M Tokens' : 'Output / 1M' }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="model in models" :key="model.id" class="border-b border-oh-subtle last:border-0 hover:bg-oh-hover">
              <td class="max-w-[20rem] break-all px-4 py-3">
                <RouterLink
                  :to="`/model-plaza/${encodeURIComponent(model.id)}`"
                  class="oh-focus-ring rounded-oh-xs font-medium text-oh-primary hover:text-oh-accent-text"
                >
                  {{ model.display_name || model.model }}
                </RouterLink>
                <div class="mt-0.5 flex items-center gap-1.5 font-mono text-[11px] text-oh-muted">
                  <span>{{ publicValue(model.provider, isZh ? '未声明' : 'Undeclared') }} · {{ model.model }}</span>
                  <CopyButton :value="model.model" size="xs" :aria-label="isZh ? '复制模型 ID' : 'Copy model ID'" />
                </div>
              </td>
              <td class="px-4 py-3">
                <Badge :tone="priceStateBadge(model).tone" size="xs" variant="subtle">
                  {{ priceStateBadge(model).label }}
                </Badge>
              </td>
              <td class="px-4 py-3 text-right font-mono tabular-nums text-oh-secondary">
                {{ formatContextTokens(model.context_tokens) }}
              </td>
              <td class="px-4 py-3 text-right font-mono tabular-nums text-oh-primary">
                <span v-if="rateText(model, 'input') !== '—'">{{ rateText(model, 'input') }}</span>
                <Tooltip
                  v-else
                  :content="isZh ? '该模型未按输入 Token 单列费率，点击查看详情中的分段或按次费率' : 'No per-token input rate declared; open model details for band or request pricing'"
                  placement="left"
                >
                  <Badge tone="unknown" size="xs" variant="subtle">{{ isZh ? '详见模型页' : 'See detail' }}</Badge>
                </Tooltip>
              </td>
              <td class="px-4 py-3 text-right font-mono tabular-nums text-oh-primary">
                <span v-if="rateText(model, 'output') !== '—'">{{ rateText(model, 'output') }}</span>
                <Tooltip
                  v-else
                  :content="isZh ? '该模型未按输出 Token 单列费率，点击查看详情中的分段或按次费率' : 'No per-token output rate declared; open model details for band or request pricing'"
                  placement="left"
                >
                  <Badge tone="unknown" size="xs" variant="subtle">{{ isZh ? '详见模型页' : 'See detail' }}</Badge>
                </Tooltip>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-else class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <RouterLink
          v-for="model in models"
          :key="model.id"
          :to="`/model-plaza/${encodeURIComponent(model.id)}`"
          class="oh-focus-ring group flex flex-col justify-between rounded-oh-md border border-oh-default bg-oh-surface p-4 transition-colors hover:border-oh-strong"
        >
          <div class="flex items-start justify-between gap-2">
            <div class="min-w-0">
              <p class="truncate font-medium text-oh-primary group-hover:text-oh-accent-text">
                {{ model.display_name || model.model }}
              </p>
              <p class="mt-0.5 truncate font-mono text-[11px] text-oh-muted">
                {{ publicValue(model.provider, isZh ? '未声明' : 'Undeclared') }} · {{ formatContextTokens(model.context_tokens) }}
              </p>
            </div>
            <Badge :tone="priceStateBadge(model).tone" size="xs" variant="subtle">
              {{ priceStateBadge(model).label }}
            </Badge>
          </div>
          <dl class="mt-4 grid grid-cols-2 gap-3 border-t border-oh-subtle pt-3 font-mono text-xs tabular-nums">
            <div>
              <dt class="text-[10px] uppercase text-oh-muted">{{ isZh ? '输入 / 1M' : 'Input / 1M' }}</dt>
              <dd class="mt-1 text-sm font-semibold text-oh-primary">{{ rateText(model, 'input') }}</dd>
            </div>
            <div>
              <dt class="text-[10px] uppercase text-oh-muted">{{ isZh ? '输出 / 1M' : 'Output / 1M' }}</dt>
              <dd class="mt-1 text-sm font-semibold text-oh-primary">{{ rateText(model, 'output') }}</dd>
            </div>
          </dl>
        </RouterLink>
      </div>

      <div v-if="nextCursor" class="mt-6 flex justify-center">
        <Button variant="secondary" :loading="loadingMore" @click="load(false)">
          {{ isZh ? '加载更多费率' : 'Load more rates' }}
        </Button>
      </div>
    </template>
  </PublicShell>
</template>
