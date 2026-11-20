<script setup lang="ts">
import { extractApiErrorMessage } from '@/utils/apiError'
import { computed, onMounted, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import PublicShell from '@/components/layout/PublicShell.vue'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import ErrorState from '@/components/ui/ErrorState.vue'
import Input from '@/components/ui/Input.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import {
  formatContextTokens,
  formatMetricsSource,
  formatMetricsWindow,
  formatPublicTimestamp,
  formatShortVersion,
  publicTokenPrice,
  publicValue
} from '@/features/public/format'
import {
  calculateExactScenarioBreakdown,
  formatExactRationalCurrency,
  parseNonNegativeDecimal,
  parseNonNegativeInt
} from '@/features/shared/pricePresentation'
import {
  comparePublicModels,
  listPublicModels,
  type PublicCompareResponse,
  type PublicCompareRow,
  type PublicModelSummary
} from '@/features/public/api'

const COMPARE_MESSAGES = {
  en: {
    eyebrow: 'COMPARE',
    title: 'Compare up to three models',
    subtitle:
      'Pick two or three models below; compare capability declarations, interactive exact-decimal scenario quotes, and live telemetry with explicit observation windows and sources.',
    backToPlaza: '← Back to Models',
    loadingCatalog: 'Loading candidate models',
    loadOptionsError: 'Model choices unavailable',
    retry: 'Retry',
    emptyCatalogTitle: 'No models available to compare',
    emptyCatalogDesc: 'Publish at least two models to the catalog to run side-by-side comparisons.',
    filterPlaceholder: 'Filter candidate models...',
    compareBtnPrefix: 'Compare ',
    compareBtnSuffix: ' models',
    selectedBadgeMid: '/3 selected',
    clearSelection: 'Clear selection',

    scenarioTitle: 'Interactive Scenario Cost Calculator',
    scenarioSubtitle:
      'Adjust per-request tokens and request multiplier to recompute authoritative quotes and exact BigInt decimal verification.',
    presetChat: 'Interactive Chat',
    presetAgent: 'Cached Coding Agent',
    presetBatch: 'Production Batch',
    inputTokensLabel: 'Input tokens / req',
    cachedTokensLabel: 'Cached input / req',
    outputTokensLabel: 'Output tokens / req',
    requestCountLabel: 'Request count (×)',
    recalculateBtn: 'Recalculate quote',
    formulaPrefix: 'Formula: ',
    formulaExpr: '((input − cached) × input_rate + cached × cache_rate + output × output_rate) × requests',
    cachedClampedHint: 'Cached input clamped to total input tokens.',

    loadingComparison: 'Loading comparison',
    comparisonFailed: 'Comparison failed',
    tableAria: 'Model comparison table',
    colDimension: 'Dimension',

    secCapabilities: 'Capabilities & Specifications',
    secPricing: 'Scenario Cost & Exact Per-1M Rates',
    secTelemetry: 'Observed Telemetry, Window & Source',

    rowProvider: 'Provider',
    rowContext: 'Context window',
    rowTools: 'Tool calling',
    rowStream: 'Streaming',
    rowInputRate: 'Input / 1M',
    rowOutputRate: 'Output / 1M',
    rowScenarioQuote: 'Scenario quote',
    rowScenarioBreakdown: 'Exact breakdown (× requests)',
    rowWindow: 'Observation window',
    rowSource: 'Telemetry source',
    rowRequests: 'Observed requests',
    rowSuccessRate: 'Success rate',
    rowTtft: 'Median TTFT (P50)',
    rowThroughput: 'Output throughput',

    undeclared: 'Undeclared',
    unobserved: 'Unobserved',
    supported: 'Supported',
    unsupported: 'Unsupported',
    free: 'Free',
    unpriced: 'Unpriced',
    snapshotPrefix: 'Catalog ',
    snapshotMid: ' · Snapshot ',
    emptyResultTitle: 'No comparison data published',
    selectPromptTitle: 'Select models to compare',
    selectPromptDesc: 'Select two or three models above, then run the comparison.'
  },
  zh: {
    eyebrow: '多模型对比 · COMPARE',
    title: '横向对比至多 3 个模型',
    subtitle:
      '选择 2 至 3 个模型进行并排对比，核对能力声明、可交互复算的精确十进制场景费用，以及附带统计时间窗、观测来源与样本量的真实流量指标。',
    backToPlaza: '← 返回模型广场',
    loadingCatalog: '正在加载候选模型',
    loadOptionsError: '无法加载可选模型列表',
    retry: '重新加载',
    emptyCatalogTitle: '暂无可对比的已发布模型',
    emptyCatalogDesc: '待公开目录发布至少两个模型后即可使用并排对比。',
    filterPlaceholder: '快速过滤候选模型…',
    compareBtnPrefix: '开始对比 (',
    compareBtnSuffix: ')',
    selectedBadgeMid: '/3 已选',
    clearSelection: '清空已选',

    scenarioTitle: '交互式场景费用估算器',
    scenarioSubtitle:
      '调整单次请求输入/缓存/输出 Token 与调用次数倍率，实时调用服务端报价并执行前端精确十进制核验。',
    presetChat: '轻量对话',
    presetAgent: '高缓存编码 Agent',
    presetBatch: '生产批处理',
    inputTokensLabel: '输入 Tokens / 次',
    cachedTokensLabel: '缓存命中 Tokens / 次',
    outputTokensLabel: '输出 Tokens / 次',
    requestCountLabel: '请求次数倍率 (×)',
    recalculateBtn: '重新计算费用',
    formulaPrefix: '计费公式：',
    formulaExpr: '((输入 − 缓存输入) × 输入单价 + 缓存输入 × 缓存单价 + 输出 × 输出单价) × 请求次数',
    cachedClampedHint: '缓存命中 Token 已自动限制为不超过总输入 Token。',

    loadingComparison: '正在生成对比矩阵',
    comparisonFailed: '对比请求失败',
    tableAria: '多模型对比结果表',
    colDimension: '对比维度',

    secCapabilities: '能力与规格声明',
    secPricing: '场景费用估算与百万 Token 单价',
    secTelemetry: '实测运行指标、统计时间窗与来源',

    rowProvider: '供应商',
    rowContext: '上下文窗口',
    rowTools: '工具调用 (Tools)',
    rowStream: '流式输出 (Stream)',
    rowInputRate: '输入单价 / 1M',
    rowOutputRate: '输出单价 / 1M',
    rowScenarioQuote: '标准场景估算费用',
    rowScenarioBreakdown: '分项核验明细 (含倍率)',
    rowWindow: '统计时间窗 (Window)',
    rowSource: '指标来源 (Source)',
    rowRequests: '窗口观测请求数',
    rowSuccessRate: '实测成功率',
    rowTtft: '中位首字延迟 (P50 TTFT)',
    rowThroughput: '输出吞吐速率',

    undeclared: '未声明',
    unobserved: '未观测',
    supported: '支持',
    unsupported: '不支持',
    free: '免费',
    unpriced: '未定价',
    snapshotPrefix: '目录版本 ',
    snapshotMid: ' · 快照时间 ',
    emptyResultTitle: '暂无对比结果数据',
    selectPromptTitle: '请选择至少 2 个模型开始对比',
    selectPromptDesc: '点击上方胶囊按钮选中 2 至 3 个模型，然后点击“开始对比”。'
  }
} as const

type CompareKey = keyof typeof COMPARE_MESSAGES.en

function toMessageFunctions<T extends Record<string, string>>(dict: T) {
  return Object.fromEntries(
    Object.entries(dict).map(([k, v]) => [k, () => v])
  ) as Record<keyof T, () => string>
}

const { t, locale } = useI18n({
  useScope: 'local',
  messages: {
    en: toMessageFunctions(COMPARE_MESSAGES.en),
    zh: toMessageFunctions(COMPARE_MESSAGES.zh),
    'zh-CN': toMessageFunctions(COMPARE_MESSAGES.zh)
  }
})

function tr(key: CompareKey): string {
  const translated = t(key)
  if (translated && translated !== key) return translated
  const active = String(locale?.value || 'en').toLowerCase()
  const dict = active.startsWith('zh') ? COMPARE_MESSAGES.zh : COMPARE_MESSAGES.en
  return dict[key]
}

const activeLocaleCode = computed(() => String(locale?.value || 'en'))
const zhFlag = computed(() => activeLocaleCode.value.toLowerCase().startsWith('zh'))

const route = useRoute()
const router = useRouter()
const options = ref<PublicModelSummary[]>([])
const filterQuery = ref('')
const selected = ref<string[]>([])
const result = ref<PublicCompareResponse | null>(null)
const loading = ref(false)
const loadingOptions = ref(true)
const error = ref<unknown>(null)
const optionsError = ref<unknown>(null)
let request = 0

const scenarioInputTokens = ref('10000')
const scenarioCachedTokens = ref('2000')
const scenarioOutputTokens = ref('2000')
const scenarioRequestCount = ref('100')

const scenarioPresets = computed(() => [
  { id: 'chat', label: tr('presetChat'), input: '4000', cached: '0', output: '1000', requests: '50' },
  { id: 'agent', label: tr('presetAgent'), input: '40000', cached: '30000', output: '4000', requests: '100' },
  { id: 'batch', label: tr('presetBatch'), input: '10000', cached: '2000', output: '2000', requests: '1000' }
])

const cachedWasClamped = computed(() => {
  const inputVal = parseNonNegativeInt(scenarioInputTokens.value, 0n)
  const cachedVal = parseNonNegativeInt(scenarioCachedTokens.value, 0n)
  return cachedVal > inputVal
})

const normalizedScenario = computed(() => {
  const inputVal = parseNonNegativeInt(scenarioInputTokens.value, 0n)
  const rawCached = parseNonNegativeInt(scenarioCachedTokens.value, 0n)
  const cachedVal = rawCached > inputVal ? inputVal : rawCached
  const outputVal = parseNonNegativeInt(scenarioOutputTokens.value, 0n)
  const rawReqs = parseNonNegativeInt(scenarioRequestCount.value, 1n)
  const reqs = rawReqs <= 0n ? 1n : rawReqs
  return {
    input_tokens: String(inputVal),
    cached_input_tokens: String(cachedVal),
    output_tokens: String(outputVal),
    requests: String(reqs)
  }
})

const canCompare = computed(() => selected.value.length >= 2 && selected.value.length <= 3)

const filteredOptions = computed(() => {
  const q = filterQuery.value.trim().toLowerCase()
  if (!q) return options.value
  return options.value.filter(
    (opt) =>
      opt.model.toLowerCase().includes(q) ||
      (opt.display_name || '').toLowerCase().includes(q) ||
      (opt.provider || '').toLowerCase().includes(q)
  )
})

function syncFromQuery(): void {
  const raw = String(route.query.ids ?? route.query.models ?? '')
  selected.value = raw ? [...new Set(raw.split(',').filter(Boolean))].slice(0, 3) : []
}

function normalizeSelectedWithCatalog(): void {
  if (!selected.value.length || !options.value.length) return
  const resolved = selected.value
    .map((token) => {
      const exact = options.value.find(
        (opt) =>
          opt.id === token ||
          opt.model === token ||
          Boolean(opt.provider && `${opt.provider}/${opt.model}` === token)
      )
      return exact ? exact.id : token
    })
    .filter((id, idx, arr) => Boolean(id) && arr.indexOf(id) === idx)
    .slice(0, 3)
  selected.value = resolved
}

function applyScenarioPreset(preset: {
  input: string
  cached: string
  output: string
  requests: string
}): void {
  scenarioInputTokens.value = preset.input
  scenarioCachedTokens.value = preset.cached
  scenarioOutputTokens.value = preset.output
  scenarioRequestCount.value = preset.requests
  if (canCompare.value) {
    void run()
  }
}

function handleScenarioInputChange(): void {
  if (canCompare.value) {
    void run()
  }
}

function toggle(id: string): void {
  if (selected.value.includes(id)) {
    selected.value = selected.value.filter((item) => item !== id)
  } else if (selected.value.length < 3) {
    selected.value = [...selected.value, id]
  }
  request++
  loading.value = false
  error.value = null
  void router.replace({ query: selected.value.length ? { ids: selected.value.join(',') } : {} })
  result.value = null
}

async function run(): Promise<void> {
  if (!canCompare.value) return
  const currentRequest = ++request
  loading.value = true
  error.value = null
  try {
    const response = await comparePublicModels(selected.value, {
      scenario: normalizedScenario.value
    })
    if (currentRequest === request) result.value = response
  } catch (err) {
    if (currentRequest === request) error.value = err
  } finally {
    if (currentRequest === request) loading.value = false
  }
}

function findSummary(modelId: string): PublicModelSummary | undefined {
  return options.value.find(
    (item) =>
      item.id === modelId ||
      item.model === modelId ||
      Boolean(item.provider && `${item.provider}/${item.model}` === modelId)
  )
}

function formatMetric(value?: number | string | null, suffix = ''): string {
  if (value === null || value === undefined || value === '') {
    return tr('unobserved')
  }
  const numeric = typeof value === 'number' ? value : Number(value)
  if (Number.isFinite(numeric)) {
    return `${numeric.toLocaleString()}${suffix}`
  }
  return `${value}${suffix}`
}

function supportText(val?: string | null): string {
  if (val === 'supported') return tr('supported')
  if (val === 'unsupported') return tr('unsupported')
  return tr('undeclared')
}

function buildClientScenarioBreakdown(row: PublicCompareRow) {
  const summary = findSummary(row.model_id)
  return calculateExactScenarioBreakdown({
    inputRate: summary?.input_rate,
    outputRate: summary?.output_rate,
    priceState: summary?.price_state,
    inputTokens: normalizedScenario.value.input_tokens,
    cachedInputTokens: normalizedScenario.value.cached_input_tokens,
    outputTokens: normalizedScenario.value.output_tokens,
    requests: normalizedScenario.value.requests,
    locale: activeLocaleCode.value
  })
}

function formatQuoteCell(row: PublicCompareRow): string {
  const summary = findSummary(row.model_id)
  if (summary?.price_state === 'free') return tr('free')
  const charge = row.quote?.estimated_charge
  if (charge) {
    const parsed = parseNonNegativeDecimal(charge.amount)
    if (parsed) {
      return formatExactRationalCurrency(parsed, charge.currency, {
        minDecimals: 2,
        maxDecimals: 4,
        locale: activeLocaleCode.value,
        groupThousands: true
      })
    }
    return charge.currency === 'USD' ? `$${charge.amount}` : `${charge.currency} ${charge.amount}`
  }
  const local = buildClientScenarioBreakdown(row)
  if (local.state === 'estimated' || local.state === 'free') {
    return local.totalDisplay
  }
  return tr('unpriced')
}

function formatBreakdownDetailCell(row: PublicCompareRow): string {
  const local = buildClientScenarioBreakdown(row)
  if (local.state === 'free') return tr('free')
  const components = row.quote?.components
  if (components?.length) {
    const parts = components.map((comp) => {
      const parsed = parseNonNegativeDecimal(comp.raw_amount)
      const money = parsed
        ? formatExactRationalCurrency(parsed, row.quote?.estimated_charge?.currency || 'USD', {
            minDecimals: 2,
            maxDecimals: 4,
            groupThousands: true
          })
        : comp.raw_amount
      return `${comp.kind}: ${money}`
    })
    const versionSuffix = row.quote?.price_version
      ? ` · ${formatShortVersion(row.quote.price_version)}`
      : ''
    return `${parts.join(' + ')}${versionSuffix}`
  }
  if (local.state === 'estimated') {
    return `In ${local.uncachedInputCost} + Cache ${local.cachedInputCost} + Out ${local.outputCost}`
  }
  return '—'
}

interface CompareSection {
  id: string
  title: string
  rows: Array<{
    key: string
    label: string
    values: string[]
  }>
}

const comparisonSections = computed<CompareSection[]>(() => {
  const rows = result.value?.rows ?? []
  return [
    {
      id: 'capabilities',
      title: tr('secCapabilities'),
      rows: [
        {
          key: 'provider',
          label: tr('rowProvider'),
          values: rows.map((row) =>
            publicValue(findSummary(row.model_id)?.provider, tr('undeclared'))
          )
        },
        {
          key: 'context',
          label: tr('rowContext'),
          values: rows.map((row) =>
            formatContextTokens(findSummary(row.model_id)?.context_tokens)
          )
        },
        {
          key: 'tools',
          label: tr('rowTools'),
          values: rows.map((row) => supportText(row.capabilities?.tools))
        },
        {
          key: 'stream',
          label: tr('rowStream'),
          values: rows.map((row) => supportText(row.capabilities?.stream))
        }
      ]
    },
    {
      id: 'pricing',
      title: tr('secPricing'),
      rows: [
        {
          key: 'input-rate',
          label: tr('rowInputRate'),
          values: rows.map((row) => {
            const summary = findSummary(row.model_id)
            if (summary?.price_state === 'free') return tr('free')
            return publicTokenPrice(summary?.input_rate, 'adaptive', zhFlag.value)
          })
        },
        {
          key: 'output-rate',
          label: tr('rowOutputRate'),
          values: rows.map((row) => {
            const summary = findSummary(row.model_id)
            if (summary?.price_state === 'free') return tr('free')
            return publicTokenPrice(summary?.output_rate, 'adaptive', zhFlag.value)
          })
        },
        {
          key: 'scenario-quote',
          label: tr('rowScenarioQuote'),
          values: rows.map((row) => formatQuoteCell(row))
        },
        {
          key: 'scenario-breakdown',
          label: tr('rowScenarioBreakdown'),
          values: rows.map((row) => formatBreakdownDetailCell(row))
        }
      ]
    },
    {
      id: 'telemetry',
      title: tr('secTelemetry'),
      rows: [
        {
          key: 'metrics-window',
          label: tr('rowWindow'),
          values: rows.map(({ metrics }) => formatMetricsWindow(metrics, zhFlag.value))
        },
        {
          key: 'metrics-source',
          label: tr('rowSource'),
          values: rows.map(({ metrics }) => formatMetricsSource(metrics?.source, zhFlag.value))
        },
        {
          key: 'metrics-requests',
          label: tr('rowRequests'),
          values: rows.map(({ metrics }) => formatMetric(metrics?.requests))
        },
        {
          key: 'success-rate',
          label: tr('rowSuccessRate'),
          values: rows.map(({ metrics }) =>
            metrics?.success_rate == null
              ? tr('unobserved')
              : `${(metrics.success_rate * 100).toFixed(2)}%`
          )
        },
        {
          key: 'ttft',
          label: tr('rowTtft'),
          values: rows.map(({ metrics }) => formatMetric(metrics?.ttft_p50_ms, ' ms'))
        },
        {
          key: 'throughput',
          label: tr('rowThroughput'),
          values: rows.map(({ metrics }) =>
            metrics?.output_tokens_per_second == null
              ? tr('unobserved')
              : `${metrics.output_tokens_per_second.toFixed(1)} t/s`
          )
        }
      ]
    }
  ]
})

async function loadOptions(): Promise<void> {
  loadingOptions.value = true
  optionsError.value = null
  try {
    const page = await listPublicModels({ limit: 80, sort: 'name' })
    options.value = page.items ?? []
  } catch (err) {
    optionsError.value = err
    return
  } finally {
    loadingOptions.value = false
  }
  normalizeSelectedWithCatalog()
  if (canCompare.value) void run()
}

onMounted(() => {
  syncFromQuery()
  void loadOptions()
})
</script>

<template>
  <PublicShell>
    <header class="flex flex-wrap items-end justify-between gap-4 border-b border-oh-default pb-6">
      <div>
        <p class="font-mono text-xs uppercase tracking-[0.18em] text-oh-muted">
          {{ tr('eyebrow') }}
        </p>
        <h1 class="mt-3 text-3xl font-semibold tracking-tight text-oh-primary">
          {{ tr('title') }}
        </h1>
        <p class="mt-2 max-w-[65ch] text-sm text-oh-secondary">
          {{ tr('subtitle') }}
        </p>
      </div>
      <RouterLink
        to="/model-plaza"
        class="oh-focus-ring inline-flex h-9 items-center rounded-oh-md border border-oh-default bg-oh-surface px-3.5 text-xs font-medium text-oh-secondary hover:border-oh-strong hover:text-oh-primary"
      >
        {{ tr('backToPlaza') }}
      </RouterLink>
    </header>

    <Skeleton v-if="loadingOptions" class="py-4" :lines="6" :label="tr('loadingCatalog')" />
    <ErrorState
      v-else-if="optionsError"
      :title="tr('loadOptionsError')"
      :retry-label="tr('retry')"
      @retry="loadOptions"
    />
    <EmptyState
      v-else-if="!options.length"
      :title="tr('emptyCatalogTitle')"
      :description="tr('emptyCatalogDesc')"
    />
    <template v-else>
      <div class="pt-5">
        <Input
          v-model="filterQuery"
          type="search"
          clearable
          class="w-full sm:w-72"
          :placeholder="tr('filterPlaceholder')"
        />
      </div>

      <div class="flex flex-wrap gap-2 py-4">
        <button
          v-for="option in filteredOptions"
          :key="option.id"
          type="button"
          class="oh-focus-ring max-w-full break-all rounded-full border px-3 py-1 font-mono text-xs transition-colors disabled:opacity-40"
          :class="selected.includes(option.id)
            ? 'border-oh-accent bg-oh-accent-subtle font-medium text-oh-accent-text'
            : 'border-oh-default text-oh-secondary hover:border-oh-strong'"
          :aria-pressed="selected.includes(option.id)"
          :disabled="!selected.includes(option.id) && selected.length >= 3"
          @click="toggle(option.id)"
        >
          {{ option.display_name || option.model }}
        </button>
      </div>
      <div class="flex flex-wrap items-center gap-3 pb-5">
        <Button :disabled="!canCompare" :loading="loading" @click="run">
          {{ `${tr('compareBtnPrefix')}${selected.length}${tr('compareBtnSuffix')}` }}
        </Button>
        <Badge tone="neutral" variant="subtle" mono>
          {{ `${selected.length}${tr('selectedBadgeMid')}` }}
        </Badge>
        <button
          v-if="selected.length > 0"
          type="button"
          class="oh-focus-ring text-xs text-oh-muted hover:text-oh-primary hover:underline"
          @click="selected = []; result = null; router.replace({ query: {} })"
        >
          {{ tr('clearSelection') }}
        </button>
      </div>

      <section
        data-testid="compare-scenario-panel"
        class="mb-6 rounded-oh-md border border-oh-default bg-oh-surface p-4 sm:p-5"
      >
        <div class="flex flex-wrap items-start justify-between gap-3 border-b border-oh-subtle pb-3.5">
          <div>
            <h2 class="text-sm font-semibold text-oh-primary">
              {{ tr('scenarioTitle') }}
            </h2>
            <p class="mt-0.5 text-xs text-oh-secondary">
              {{ tr('scenarioSubtitle') }}
            </p>
          </div>
          <div class="flex flex-wrap items-center gap-1.5">
            <button
              v-for="preset in scenarioPresets"
              :key="preset.id"
              type="button"
              :data-testid="`scenario-preset-${preset.id}`"
              class="oh-focus-ring rounded-full border border-oh-default bg-oh-subtle px-2.5 py-1 font-mono text-[11px] text-oh-secondary transition-colors hover:border-oh-strong hover:text-oh-primary"
              @click="applyScenarioPreset(preset)"
            >
              {{ preset.label }}
            </button>
          </div>
        </div>

        <div class="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
          <label class="flex flex-col gap-1 text-xs text-oh-secondary">
            <span>{{ tr('inputTokensLabel') }}</span>
            <input
              v-model="scenarioInputTokens"
              data-testid="scenario-input-tokens"
              type="number"
              min="0"
              step="1000"
              class="oh-focus-ring h-9 rounded-oh-sm border border-oh-default bg-oh-canvas px-2.5 font-mono text-xs tabular-nums text-oh-primary"
              @change="handleScenarioInputChange"
            />
          </label>

          <label class="flex flex-col gap-1 text-xs text-oh-secondary">
            <span>{{ tr('cachedTokensLabel') }}</span>
            <input
              v-model="scenarioCachedTokens"
              data-testid="scenario-cached-tokens"
              type="number"
              min="0"
              step="500"
              class="oh-focus-ring h-9 rounded-oh-sm border border-oh-default bg-oh-canvas px-2.5 font-mono text-xs tabular-nums text-oh-primary"
              @change="handleScenarioInputChange"
            />
          </label>

          <label class="flex flex-col gap-1 text-xs text-oh-secondary">
            <span>{{ tr('outputTokensLabel') }}</span>
            <input
              v-model="scenarioOutputTokens"
              data-testid="scenario-output-tokens"
              type="number"
              min="0"
              step="500"
              class="oh-focus-ring h-9 rounded-oh-sm border border-oh-default bg-oh-canvas px-2.5 font-mono text-xs tabular-nums text-oh-primary"
              @change="handleScenarioInputChange"
            />
          </label>

          <label class="flex flex-col gap-1 text-xs text-oh-secondary">
            <span>{{ tr('requestCountLabel') }}</span>
            <input
              v-model="scenarioRequestCount"
              data-testid="scenario-request-count"
              type="number"
              min="1"
              step="10"
              class="oh-focus-ring h-9 rounded-oh-sm border border-oh-default bg-oh-canvas px-2.5 font-mono text-xs tabular-nums text-oh-primary"
              @change="handleScenarioInputChange"
            />
          </label>

          <div class="flex items-end">
            <Button
              variant="secondary"
              class="w-full"
              data-testid="scenario-recalculate"
              :disabled="!canCompare"
              :loading="loading"
              @click="run"
            >
              {{ tr('recalculateBtn') }}
            </Button>
          </div>
        </div>

        <div
          data-testid="scenario-formula-summary"
          class="mt-3 flex flex-wrap items-center justify-between gap-2 border-t border-oh-subtle pt-2.5 font-mono text-[11px] text-oh-muted"
        >
          <span>
            {{ tr('formulaPrefix') }}<code>{{ tr('formulaExpr') }}</code>
          </span>
          <span v-if="cachedWasClamped" class="text-[var(--oh-warn-fg)]">
            {{ tr('cachedClampedHint') }}
          </span>
        </div>
      </section>

      <Skeleton v-if="loading" class="py-4" :lines="6" :label="tr('loadingComparison')" />
      <ErrorState
        v-else-if="error"
        :title="tr('comparisonFailed')"
        :message="extractApiErrorMessage(error, '')"
        :retry-label="tr('retry')"
        @retry="run"
      />

      <div
        v-else-if="result?.rows.length"
        data-testid="compare-results-table"
        class="oh-focus-ring overflow-x-auto rounded-oh-md border border-oh-default bg-oh-surface"
        tabindex="0"
        role="region"
        :aria-label="tr('tableAria')"
      >
        <table class="w-full min-w-[680px] table-fixed border-collapse text-sm">
          <thead>
            <tr class="border-b border-oh-default bg-oh-subtle text-left font-mono text-[11px] uppercase tracking-wider text-oh-muted">
              <th class="w-56 px-4 py-2.5">{{ tr('colDimension') }}</th>
              <th v-for="(row, index) in result.rows" :key="row.model_id" class="break-all px-4 py-2.5 text-right">
                <RouterLink
                  :to="`/model-plaza/${encodeURIComponent(row.model_id)}`"
                  class="oh-focus-ring text-oh-primary hover:text-oh-accent-text"
                >
                  {{ findSummary(row.model_id)?.display_name || findSummary(row.model_id)?.model || row.model_id }}
                </RouterLink>
                <span class="ml-1 text-[10px] text-oh-muted">#{{ index + 1 }}</span>
              </th>
            </tr>
          </thead>
          <tbody v-for="section in comparisonSections" :key="section.id">
            <tr class="border-y border-oh-default bg-oh-subtle/70">
              <th
                :colspan="result.rows.length + 1"
                scope="colgroup"
                class="px-4 py-2 text-left font-mono text-[11px] font-semibold uppercase tracking-wider text-oh-accent-text"
              >
                {{ section.title }}
              </th>
            </tr>
            <tr
              v-for="metric in section.rows"
              :key="metric.key"
              :data-metric-key="metric.key"
              class="border-b border-oh-subtle last:border-0 hover:bg-oh-hover"
            >
              <td class="px-4 py-2.5 font-mono text-xs text-oh-secondary">{{ metric.label }}</td>
              <td
                v-for="(value, index) in metric.values"
                :key="index"
                class="break-words px-4 py-2.5 text-right font-mono text-xs tabular-nums text-oh-primary"
              >
                {{ value }}
              </td>
            </tr>
          </tbody>
        </table>
        <p v-if="result.as_of" class="border-t border-oh-default px-4 py-2 font-mono text-[11px] text-oh-muted">
          {{ `${tr('snapshotPrefix')}${result.catalog_version ?? '—'}${tr('snapshotMid')}${formatPublicTimestamp(result.as_of)}` }}
        </p>
      </div>

      <EmptyState
        v-else
        :title="result ? tr('emptyResultTitle') : tr('selectPromptTitle')"
        :description="result ? undefined : tr('selectPromptDesc')"
      />
    </template>
  </PublicShell>
</template>
