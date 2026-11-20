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
  formatPublicTimestamp,
  publicTokenPrice,
  publicValue
} from '@/features/public/format'
import { comparePublicModels, listPublicModels, type PublicCompareResponse, type PublicModelSummary } from '@/features/public/api'

const { locale } = useI18n()
const isZh = computed(() => String(locale?.value ?? '').startsWith('zh'))
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
  const raw = String(route.query.ids ?? '')
  selected.value = raw ? [...new Set(raw.split(',').filter(Boolean))].slice(0, 3) : []
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
  if (!canCompare.value || loading.value) return
  const currentRequest = ++request
  loading.value = true
  error.value = null
  try {
    const response = await comparePublicModels(selected.value)
    if (currentRequest === request) result.value = response
  } catch (err) {
    if (currentRequest === request) error.value = err
  } finally {
    if (currentRequest === request) loading.value = false
  }
}

function findSummary(modelId: string): PublicModelSummary | undefined {
  return options.value.find((item) => item.id === modelId || item.model === modelId)
}

function formatMetric(value?: number | string | null, suffix = ''): string {
  if (value === null || value === undefined || value === '') {
    return isZh.value ? '未观测' : 'Unobserved'
  }
  return `${value.toLocaleString()}${suffix}`
}

function supportText(val?: string | null): string {
  if (val === 'supported') return isZh.value ? '支持' : 'Supported'
  if (val === 'unsupported') return isZh.value ? '不支持' : 'Unsupported'
  return isZh.value ? '未声明' : 'Undeclared'
}

const metricRows = computed(() => [
  {
    label: isZh.value ? '供应商' : 'Provider',
    values: result.value?.rows.map((row) =>
      publicValue(findSummary(row.model_id)?.provider, isZh.value ? '未声明' : 'Undeclared')
    )
  },
  {
    label: isZh.value ? '上下文窗口' : 'Context window',
    values: result.value?.rows.map((row) => formatContextTokens(findSummary(row.model_id)?.context_tokens))
  },
  {
    label: isZh.value ? '输入单价 / 1M' : 'Input / 1M',
    values: result.value?.rows.map((row) => {
      const summary = findSummary(row.model_id)
      if (summary?.price_state === 'free') return isZh.value ? '免费' : 'Free'
      return publicTokenPrice(summary?.input_rate, 4, isZh.value)
    })
  },
  {
    label: isZh.value ? '输出单价 / 1M' : 'Output / 1M',
    values: result.value?.rows.map((row) => {
      const summary = findSummary(row.model_id)
      if (summary?.price_state === 'free') return isZh.value ? '免费' : 'Free'
      return publicTokenPrice(summary?.output_rate, 4, isZh.value)
    })
  },
  {
    label: isZh.value ? '标准场景估算费用' : 'Scenario quote',
    values: result.value?.rows.map((row) => {
      const charge = row.quote?.estimated_charge
      if (!charge) return isZh.value ? '未定价' : 'Unpriced'
      return charge.currency === 'USD' ? `$${charge.amount}` : `${charge.currency} ${charge.amount}`
    })
  },
  {
    label: isZh.value ? '工具调用 (Tools)' : 'Tool calling',
    values: result.value?.rows.map((row) => supportText(row.capabilities?.tools))
  },
  {
    label: isZh.value ? '流式输出 (Stream)' : 'Streaming',
    values: result.value?.rows.map((row) => supportText(row.capabilities?.stream))
  },
  {
    label: isZh.value ? '实测成功率' : 'Success rate',
    values: result.value?.rows.map(({ metrics }) =>
      metrics?.success_rate == null
        ? (isZh.value ? '未观测' : 'Unobserved')
        : `${(metrics.success_rate * 100).toFixed(2)}%`
    )
  },
  {
    label: isZh.value ? '中位首字延迟 (P50 TTFT)' : 'Median TTFT (P50)',
    values: result.value?.rows.map(({ metrics }) => formatMetric(metrics?.ttft_p50_ms, ' ms'))
  },
  {
    label: isZh.value ? '输出吞吐速率' : 'Output throughput',
    values: result.value?.rows.map(({ metrics }) =>
      metrics?.output_tokens_per_second == null
        ? (isZh.value ? '未观测' : 'Unobserved')
        : `${metrics.output_tokens_per_second.toFixed(1)} t/s`
    )
  },
  {
    label: isZh.value ? '窗口观测请求数' : 'Observed requests',
    values: result.value?.rows.map(({ metrics }) => formatMetric(metrics?.requests))
  }
])

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
          {{ isZh ? '多模型对比 · COMPARE' : 'COMPARE' }}
        </p>
        <h1 class="mt-3 text-3xl font-semibold tracking-tight text-oh-primary">
          {{ isZh ? '横向对比至多 3 个模型' : 'Compare up to three models' }}
        </h1>
        <p class="mt-2 max-w-[65ch] text-sm text-oh-secondary">
          {{
            isZh
              ? '选择 2 至 3 个模型进行并排对比，严格保持选择顺序、目录快照版本及真实观测状态。'
              : 'Pick two or three models below; the comparison preserves your selection order, catalog snapshot, and live telemetry state.'
          }}
        </p>
      </div>
      <RouterLink
        to="/model-plaza"
        class="oh-focus-ring inline-flex h-9 items-center rounded-oh-md border border-oh-default bg-oh-surface px-3.5 text-xs font-medium text-oh-secondary hover:border-oh-strong hover:text-oh-primary"
      >
        {{ isZh ? '← 返回模型广场' : '← Back to Models' }}
      </RouterLink>
    </header>

    <Skeleton v-if="loadingOptions" class="py-4" :lines="6" :label="isZh ? '正在加载候选模型' : 'Loading candidate models'" />
    <ErrorState
      v-else-if="optionsError"
      :title="isZh ? '无法加载可选模型列表' : 'Model choices unavailable'"
      :retry-label="isZh ? '重新加载' : 'Retry'"
      @retry="loadOptions"
    />
    <EmptyState
      v-else-if="!options.length"
      :title="isZh ? '暂无可对比的已发布模型' : 'No models available to compare'"
      :description="isZh ? '待公开目录发布至少两个模型后即可使用并排对比。' : 'Publish at least two models to the catalog to run side-by-side comparisons.'"
    />
    <template v-else>
      <div class="pt-5">
        <Input
          v-model="filterQuery"
          type="search"
          clearable
          class="w-full sm:w-72"
          :placeholder="isZh ? '快速过滤候选模型…' : 'Filter candidate models...'"
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
          {{ isZh ? `开始对比 (${selected.length})` : `Compare ${selected.length} models` }}
        </Button>
        <Badge tone="neutral" variant="subtle" mono>
          {{ isZh ? `已选 ${selected.length} / 3` : `${selected.length}/3 selected` }}
        </Badge>
        <button
          v-if="selected.length > 0"
          type="button"
          class="oh-focus-ring text-xs text-oh-muted hover:text-oh-primary hover:underline"
          @click="selected = []; result = null; router.replace({ query: {} })"
        >
          {{ isZh ? '清空已选' : 'Clear selection' }}
        </button>
      </div>

      <Skeleton v-if="loading" class="py-4" :lines="6" :label="isZh ? '正在生成对比矩阵' : 'Loading comparison'" />
      <ErrorState
        v-else-if="error"
        :title="isZh ? '对比请求失败' : 'Comparison failed'"
        :message="extractApiErrorMessage(error, '')"
        :retry-label="isZh ? '重试对比' : 'Retry'"
        @retry="run"
      />

      <div
        v-else-if="result?.rows.length"
        class="oh-focus-ring overflow-x-auto rounded-oh-md border border-oh-default bg-oh-surface"
        tabindex="0"
        role="region"
        :aria-label="isZh ? '多模型对比结果表' : 'Model comparison table'"
      >
        <table class="w-full min-w-[600px] table-fixed border-collapse text-sm">
          <thead>
            <tr class="border-b border-oh-default bg-oh-subtle text-left font-mono text-[11px] uppercase tracking-wider text-oh-muted">
              <th class="w-48 px-4 py-2.5">{{ isZh ? '对比维度' : 'Dimension' }}</th>
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
          <tbody>
            <tr v-for="metric in metricRows" :key="metric.label" class="border-b border-oh-subtle last:border-0 hover:bg-oh-hover">
              <td class="px-4 py-2.5 font-mono text-xs text-oh-secondary">{{ metric.label }}</td>
              <td v-for="(value, index) in metric.values" :key="index" class="px-4 py-2.5 text-right font-mono tabular-nums text-oh-primary">
                {{ value }}
              </td>
            </tr>
          </tbody>
        </table>
        <p v-if="result.as_of" class="border-t border-oh-default px-4 py-2 font-mono text-[11px] text-oh-muted">
          {{
            isZh
              ? `目录版本 ${result.catalog_version ?? '—'} · 快照时间 ${formatPublicTimestamp(result.as_of)}`
              : `Catalog ${result.catalog_version ?? '—'} · Snapshot ${formatPublicTimestamp(result.as_of)}`
          }}
        </p>
      </div>

      <EmptyState
        v-else
        :title="
          result
            ? (isZh ? '暂无对比结果数据' : 'No comparison data published')
            : (isZh ? '请选择至少 2 个模型开始对比' : 'Select models to compare')
        "
        :description="
          result
            ? undefined
            : (isZh ? '点击上方胶囊按钮选中 2 至 3 个模型，然后点击“开始对比”。' : 'Select two or three models above, then run the comparison.')
        "
      />
    </template>
  </PublicShell>
</template>
