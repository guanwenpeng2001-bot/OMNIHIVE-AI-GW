<script setup lang="ts">
import { extractApiErrorMessage } from '@/utils/apiError'
import { computed, onMounted, ref, watch } from 'vue'
import { applyRouteSeo } from '@/features/public/seo'
import { useRoute, RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import PublicShell from '@/components/layout/PublicShell.vue'
import Badge from '@/components/ui/Badge.vue'
import CopyButton from '@/components/ui/CopyButton.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import ErrorState from '@/components/ui/ErrorState.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import Tooltip from '@/components/ui/Tooltip.vue'
import type { UiTone } from '@/components/ui/types'
import {
  availabilityStateLabel,
  availabilityStateTone,
  evidenceStateLabel,
  extractDisplayModalities,
  formatContextTokens,
  formatPriceSourceLabel,
  formatPublicTimestamp,
  formatRateBandKind,
  formatRateBandPrice,
  formatShortVersion,
  maturityLabel,
  maturityTone,
  metricsStateReason,
  modalityLabel,
  modalityTone,
  publicTokenPrice,
  publicValue,
  resolveAvailabilityState,
  routeStateLabel,
  routeStateTone
} from '@/features/public/format'
import { getPublicModel, type PublicModelDetail } from '@/features/public/api'

interface FactRow {
  key: string
  label: string
  value?: string
  badge?: string
  badgeTone?: UiTone
  sourceNote?: string
  undeclared?: boolean
}

const { locale } = useI18n()
const isZh = computed(() => String(locale?.value ?? '').startsWith('zh'))
const route = useRoute()
const detail = ref<PublicModelDetail | null>(null)
const model = computed(() => detail.value?.model)
let request = 0
const loading = ref(true)
const error = ref<unknown>(null)
const snippetTab = ref<'curl' | 'ts' | 'python'>('curl')
const showUndeclaredDetails = ref(false)

const modelId = computed(() => {
  const rawParam = route.params.id
  const raw = Array.isArray(rawParam) ? rawParam.join('/') : String(rawParam ?? '')
  try {
    return decodeURIComponent(raw)
  } catch {
    return raw
  }
})

const compareRouteTarget = computed(() => ({
  path: '/model-plaza/compare',
  query: { ids: model.value?.id || modelId.value }
}))

const primaryFact = computed(() => {
  const list = detail.value?.model_facts ?? []
  return list.find((entry) => entry.state === 'fresh') ?? list[0] ?? null
})

const gatewayOrigin = computed(() => {
  if (typeof window !== 'undefined' && window.location?.origin) {
    return window.location.origin
  }
  return 'https://api.omnihive.dev'
})

const invocationModelName = computed(() => model.value?.model || model.value?.id || modelId.value)

const integrationSnippet = computed(() => {
  const m = invocationModelName.value
  if (snippetTab.value === 'ts') {
    return `import OpenAI from 'openai'

const client = new OpenAI({
  baseURL: '${gatewayOrigin.value}/v1',
  apiKey: process.env.OMNIHIVE_API_KEY
})

const completion = await client.chat.completions.create({
  model: '${m}',
  messages: [{ role: 'user', content: 'Explain quantum routing in two sentences.' }]
})
console.log(completion.choices[0]?.message?.content)`
  }
  if (snippetTab.value === 'python') {
    return `from openai import OpenAI
import os

client = OpenAI(
    base_url="${gatewayOrigin.value}/v1",
    api_key=os.environ["OMNIHIVE_API_KEY"],
)

resp = client.chat.completions.create(
    model="${m}",
    messages=[{"role": "user", "content": "Explain quantum routing in two sentences."}],
)
print(resp.choices[0].message.content)`
  }
  return `curl ${gatewayOrigin.value}/v1/chat/completions \\
  -H "Authorization: Bearer $OMNIHIVE_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${m}",
    "messages": [{"role": "user", "content": "Explain quantum routing in two sentences."}]
  }'`
})

function boolFactRow(key: string, label: string, val?: boolean | null, source?: string): FactRow {
  if (val === true) {
    return {
      key,
      label,
      badge: isZh.value ? '支持' : 'Supported',
      badgeTone: 'success',
      sourceNote: source
    }
  }
  if (val === false) {
    return {
      key,
      label,
      badge: isZh.value ? '不支持' : 'Unsupported',
      badgeTone: 'neutral',
      sourceNote: source
    }
  }
  return {
    key,
    label,
    badge: isZh.value ? '未声明' : 'Undeclared',
    badgeTone: 'unknown',
    undeclared: true,
    sourceNote: isZh.value ? '模型注册表与上游观测尚未声明此能力' : 'Not declared by model registry or upstream verification'
  }
}

const trainingGovernance = computed<FactRow>(() => {
  const endpoints = detail.value?.endpoints ?? []
  const declared = endpoints.find((ep) => ep.training && ep.training !== 'unknown')
  if (declared) {
    const normalized = declared.training.toLowerCase()
    const noTrain = normalized.includes('never') || normalized.includes('none') || normalized.includes('false') || normalized.includes('opt_out') || normalized.includes('no')
    return {
      key: 'training',
      label: isZh.value ? '请求数据用于训练' : 'Training on data',
      badge: noTrain
        ? (isZh.value ? `不用于训练 (${declared.training})` : `Not trained (${declared.training})`)
        : declared.training,
      badgeTone: noTrain ? 'success' : 'warning',
      sourceNote: declared.privacy_evidence?.source
        ? (isZh.value ? `证据来源：${declared.privacy_evidence.source}` : `Source: ${declared.privacy_evidence.source}`)
        : (isZh.value ? `来自上游端点 (${declared.provider})` : `Declared on endpoint (${declared.provider})`)
    }
  }
  return {
    key: 'training',
    label: isZh.value ? '请求数据用于训练' : 'Training on data',
    badge: isZh.value ? '未声明' : 'Undeclared',
    badgeTone: 'unknown',
    undeclared: true,
    sourceNote: isZh.value
      ? '上游供应商尚未发布经核验的数据留存与训练隐私声明'
      : 'No verified training/privacy declaration published for active endpoints'
  }
})

const stabilityGovernance = computed<FactRow>(() => {
  const rawMaturity =
    (model.value?.maturity && model.value.maturity !== 'unknown' ? model.value.maturity : null) ||
    (primaryFact.value?.maturity && primaryFact.value.maturity !== 'unknown' ? primaryFact.value.maturity : null) ||
    detail.value?.compatibility?.find((c) => c.maturity && c.maturity !== 'unknown')?.maturity ||
    null

  if (rawMaturity) {
    const src = primaryFact.value?.source || detail.value?.compatibility?.[0]?.adapter_version
    return {
      key: 'stability',
      label: isZh.value ? '稳定性与生命周期' : 'Stability & Maturity',
      badge: maturityLabel(rawMaturity, isZh.value),
      badgeTone: maturityTone(rawMaturity),
      sourceNote: src
        ? (isZh.value ? `核验来源：${src}` : `Verified via ${src}`)
        : undefined
    }
  }

  const metricsState = detail.value?.metrics?.state
  return {
    key: 'stability',
    label: isZh.value ? '稳定性与生命周期' : 'Stability & Maturity',
    badge: isZh.value ? '未声明 / 未观测' : 'Undeclared / Unobserved',
    badgeTone: 'unknown',
    undeclared: true,
    sourceNote: metricsStateReason(metricsState, isZh.value)
  }
})

const facts = computed<FactRow[]>(() => {
  const fact = primaryFact.value
  const ctxTokens = fact?.context_tokens || model.value?.context_tokens
  const modalities = fact?.modalities?.length ? fact.modalities : (model.value?.modalities ?? [])
  const sourceHint = fact?.source
    ? (isZh.value ? `来源：${fact.source} (${evidenceStateLabel(fact.state, true)})` : `Source: ${fact.source} (${evidenceStateLabel(fact.state, false)})`)
    : undefined

  return [
    {
      key: 'context',
      label: isZh.value ? '上下文窗口' : 'Context window',
      value: ctxTokens ? `${Number(ctxTokens).toLocaleString()} tokens (${formatContextTokens(ctxTokens)})` : undefined,
      badge: ctxTokens ? undefined : (isZh.value ? '未声明' : 'Undeclared'),
      badgeTone: 'unknown',
      undeclared: !ctxTokens,
      sourceNote: detail.value?.context_basis || (fact?.context_kind ? `basis: ${fact.context_kind}` : undefined)
    },
    {
      key: 'max_output',
      label: isZh.value ? '最大输出长度' : 'Max output tokens',
      value: fact?.max_output_tokens ? `${Number(fact.max_output_tokens).toLocaleString()} tokens` : undefined,
      badge: fact?.max_output_tokens ? undefined : (isZh.value ? '未声明' : 'Undeclared'),
      badgeTone: 'unknown',
      undeclared: !fact?.max_output_tokens
    },
    {
      key: 'modalities',
      label: isZh.value ? '支持模态' : 'Modalities',
      value: modalities.length ? modalities.join(', ') : undefined,
      badge: modalities.length ? undefined : (isZh.value ? '未声明' : 'Undeclared'),
      badgeTone: 'unknown',
      undeclared: modalities.length === 0
    },
    boolFactRow('tools', isZh.value ? '工具 / 函数调用 (Tools)' : 'Tool / Function Calling', fact?.tools, sourceHint),
    boolFactRow('reasoning', isZh.value ? '深度推理 (Reasoning)' : 'Reasoning / Thinking', fact?.reasoning, sourceHint),
    boolFactRow('vision', isZh.value ? '视觉输入 (Vision)' : 'Vision Input', fact?.vision, sourceHint),
    boolFactRow('caching', isZh.value ? '提示词缓存 (Prompt Caching)' : 'Prompt Caching', fact?.prompt_caching, sourceHint),
    trainingGovernance.value,
    stabilityGovernance.value
  ]
})

const declaredFacts = computed(() => facts.value.filter((entry) => !entry.undeclared))
const undeclaredFacts = computed(() => facts.value.filter((entry) => entry.undeclared))
const shouldCompactUndeclared = computed(() => undeclaredFacts.value.length >= 2)
const visibleFacts = computed(() => (shouldCompactUndeclared.value ? declaredFacts.value : facts.value))

const detailModalities = computed(() => {
  const factMods = primaryFact.value?.modalities?.length ? primaryFact.value.modalities : null
  return extractDisplayModalities({
    modalities: factMods ?? model.value?.modalities,
    actions: model.value?.actions
  })
})

const detailAvailability = computed(() => {
  const state = resolveAvailabilityState(model.value)
  if (state === 'unknown') return null
  return {
    state,
    label: availabilityStateLabel(state, isZh.value),
    tone: availabilityStateTone(state)
  }
})

const activeReferencePrice = computed(() => detail.value?.reference_price ?? model.value?.reference_price ?? null)

function displayPrice(kind: 'input' | 'output'): string {
  if (model.value?.price_state === 'free') return isZh.value ? '免费' : 'Free'
  const rate = kind === 'input' ? model.value?.input_rate : model.value?.output_rate
  if (rate) return publicTokenPrice(rate, 'adaptive', isZh.value)
  const refRate = kind === 'input' ? activeReferencePrice.value?.input_rate : activeReferencePrice.value?.output_rate
  if (refRate) {
    const formatted = publicTokenPrice(refRate, 'adaptive', isZh.value)
    return isZh.value ? `参考 ${formatted}` : `Ref ${formatted}`
  }
  if (detailAvailability.value?.state === 'catalog_only') {
    return isZh.value ? '按需接入' : 'On demand'
  }
  return publicTokenPrice(rate, 'adaptive', isZh.value)
}

async function load(): Promise<void> {
  const currentRequest = ++request
  loading.value = true
  error.value = null
  showUndeclaredDetails.value = false
  try {
    const response = await getPublicModel(modelId.value)
    if (currentRequest === request) {
      detail.value = response
      const loaded = response?.model
      if (loaded) {
        const modelTitle = loaded.display_name || loaded.model
        const siteName = 'OmniHive'
        applyRouteSeo({
          title: `${modelTitle} - ${siteName}`,
          description: isZh.value
            ? `${modelTitle} 的上下文窗口、支持能力、定价与实时延迟指标。`
            : `Context window, capabilities, pricing, and live latency telemetry for ${modelTitle}.`,
          canonicalPath: `/model-plaza/${encodeURIComponent(loaded.model || loaded.id)}`,
          siteName
        })
      }
    }
  } catch (err) {
    if (currentRequest === request) error.value = err
  } finally {
    if (currentRequest === request) loading.value = false
  }
}

watch(modelId, () => void load())
onMounted(() => void load())
</script>

<template>
  <PublicShell>
    <div class="mb-6 flex items-center gap-2 text-xs text-oh-muted">
      <RouterLink to="/model-plaza" class="oh-focus-ring shrink-0 rounded-oh-xs hover:text-oh-primary">
        {{ isZh ? '模型广场' : 'Models' }}
      </RouterLink>
      <span>/</span>
      <span class="min-w-0 break-all font-mono text-oh-secondary">{{ model?.model || modelId }}</span>
    </div>

    <Skeleton v-if="loading" class="py-4" :lines="10" :label="isZh ? '正在加载模型详情' : 'Loading model details'" />
    <ErrorState
      v-else-if="error"
      :title="isZh ? '无法加载该模型详情' : 'Model unavailable'"
      :message="extractApiErrorMessage(error, '')"
      :retry-label="isZh ? '重试加载' : 'Retry'"
      @retry="load()"
    />

    <template v-else-if="model">
      <header class="flex flex-wrap items-start justify-between gap-4 border-b border-oh-default pb-6">
        <div class="min-w-0 space-y-2">
          <div class="flex flex-wrap items-center gap-2">
            <span class="font-mono text-xs uppercase tracking-[0.18em] text-oh-muted">
              {{ publicValue(model.provider, isZh ? '未声明供应商' : 'Undeclared Provider') }}
            </span>
            <Badge
              v-if="detailAvailability"
              :data-testid="`detail-availability-badge-${detailAvailability.state}`"
              :tone="detailAvailability.tone"
              size="xs"
              variant="subtle"
              dot
            >
              {{ detailAvailability.label }}
            </Badge>
            <Badge
              v-else
              :tone="routeStateTone(model.route_state)"
              size="xs"
              variant="subtle"
              dot
            >
              {{ routeStateLabel(model.route_state, isZh) }}
            </Badge>
            <Badge :tone="maturityTone(model.maturity)" size="xs" variant="outline">
              {{ maturityLabel(model.maturity, isZh) }}
            </Badge>
            <Badge
              v-for="mod in detailModalities"
              :key="mod"
              :tone="modalityTone(mod)"
              size="xs"
              variant="subtle"
              :data-testid="`detail-modality-badge-${mod}`"
            >
              {{ modalityLabel(mod, isZh) }}
            </Badge>
          </div>
          <h1 class="break-words text-3xl font-semibold tracking-tight text-oh-primary">
            {{ model.display_name || model.model }}
          </h1>
          <div class="flex flex-wrap items-center gap-2">
            <code
              v-if="model.display_name && model.display_name !== model.model"
              class="rounded-oh-xs border border-oh-subtle bg-oh-subtle px-2 py-0.5 font-mono text-xs text-oh-primary"
            >
              {{ model.model }}
            </code>
            <span v-else class="font-mono text-xs text-oh-muted">
              {{ isZh ? '调用模型标识' : 'Model Slug' }}
            </span>
            <CopyButton
              :value="model.model"
              variant="secondary"
              :aria-label="isZh ? '复制调用模型名' : 'Copy model identifier'"
            />
          </div>
          <div v-if="detail?.aliases?.length" class="flex flex-wrap items-center gap-1.5 pt-1">
            <span class="font-mono text-[11px] text-oh-muted">{{ isZh ? '支持别名：' : 'Aliases:' }}</span>
            <span
              v-for="alias in detail.aliases"
              :key="alias"
              class="rounded-oh-xs border border-oh-subtle px-1.5 py-0.5 font-mono text-[11px] text-oh-secondary"
            >
              {{ alias }}
            </span>
          </div>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <a
            v-if="detail?.documentation_url"
            :href="detail.documentation_url"
            target="_blank"
            rel="noopener noreferrer"
            class="oh-focus-ring inline-flex h-9 items-center rounded-oh-md border border-oh-default bg-oh-surface px-3.5 text-xs font-medium text-oh-secondary hover:border-oh-strong hover:text-oh-primary"
          >
            {{ isZh ? '官方文档 ↗' : 'Provider Docs ↗' }}
          </a>
          <RouterLink
            :to="compareRouteTarget"
            class="oh-focus-ring inline-flex h-9 items-center rounded-oh-md border border-oh-default bg-oh-surface px-3.5 text-[13px] font-medium text-oh-primary hover:border-oh-strong hover:bg-oh-hover"
          >
            {{ isZh ? '加入对比' : 'Compare' }}
          </RouterLink>
        </div>
      </header>

      <section class="grid gap-4 py-6 sm:grid-cols-2 lg:grid-cols-4">
        <div class="rounded-oh-md border border-oh-default bg-oh-surface p-4">
          <p class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">
            {{ isZh ? '输入单价 / 1M Tokens' : 'Input / 1M' }}
          </p>
          <p class="mt-2 font-mono text-lg font-semibold tabular-nums text-oh-primary">
            {{ displayPrice('input') }}
          </p>
          <p v-if="activeReferencePrice?.input_rate" class="mt-1 text-xs text-oh-muted">
            {{ isZh ? '参考定价' : 'Reference price' }} · {{ publicTokenPrice(activeReferencePrice.input_rate, 'adaptive', isZh) }}
          </p>
          <p v-if="detail?.price_source" class="mt-1 font-mono text-[11px] text-oh-muted">
            {{ isZh ? `定价源：${formatPriceSourceLabel(detail.price_source, true)}` : `Source: ${formatPriceSourceLabel(detail.price_source, false)}` }}
          </p>
        </div>

        <div class="rounded-oh-md border border-oh-default bg-oh-surface p-4">
          <p class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">
            {{ isZh ? '输出单价 / 1M Tokens' : 'Output / 1M' }}
          </p>
          <p class="mt-2 font-mono text-lg font-semibold tabular-nums text-oh-primary">
            {{ displayPrice('output') }}
          </p>
          <p v-if="activeReferencePrice?.output_rate" class="mt-1 text-xs text-oh-muted">
            {{ isZh ? '参考定价' : 'Reference price' }} · {{ publicTokenPrice(activeReferencePrice.output_rate, 'adaptive', isZh) }}
          </p>
          <p v-if="detail?.price_version" class="mt-1 truncate font-mono text-[11px] text-oh-muted" :title="detail.price_version">
            {{ isZh ? `费率快照 ${formatShortVersion(detail.price_version)}` : `Snapshot ${formatShortVersion(detail.price_version)}` }}
          </p>
        </div>

        <div class="rounded-oh-md border border-oh-default bg-oh-surface p-4">
          <p class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">
            {{ isZh ? '实测中位首字延迟 (P50 TTFT)' : 'Observed TTFT (P50)' }}
          </p>
          <div class="mt-2 flex items-center gap-2">
            <span
              v-if="detail?.metrics?.ttft_p50_ms != null"
              class="font-mono text-lg font-semibold tabular-nums text-oh-primary"
            >
              {{ detail.metrics.ttft_p50_ms }} ms
            </span>
            <Tooltip v-else :content="metricsStateReason(detail?.metrics?.state, isZh)">
              <Badge tone="unknown" variant="subtle">
                {{ isZh ? '未观测' : 'Unobserved' }}
              </Badge>
            </Tooltip>
          </div>
          <p class="mt-1 font-mono text-[11px] text-oh-muted">
            {{
              detail?.metrics?.ttft_samples
                ? (isZh ? `${detail.metrics.ttft_samples} 次有效采样` : `${detail.metrics.ttft_samples} samples`)
                : metricsStateReason(detail?.metrics?.state, isZh)
            }}
          </p>
        </div>

        <div class="rounded-oh-md border border-oh-default bg-oh-surface p-4">
          <p class="font-mono text-[11px] uppercase tracking-wider text-oh-muted">
            {{ isZh ? '输出吞吐与成功率' : 'Throughput & Success' }}
          </p>
          <div class="mt-2 flex flex-wrap items-baseline gap-2 font-mono tabular-nums">
            <span
              v-if="detail?.metrics?.output_tokens_per_second != null"
              class="text-lg font-semibold text-oh-primary"
            >
              {{ detail.metrics.output_tokens_per_second.toFixed(1) }} t/s
            </span>
            <Badge v-else tone="unknown" variant="subtle">
              {{ isZh ? '吞吐未观测' : 'Unobserved' }}
            </Badge>
            <span v-if="detail?.metrics?.success_rate != null" class="text-xs text-oh-secondary">
              · {{ (detail.metrics.success_rate * 100).toFixed(2) }}%
            </span>
          </div>
          <p class="mt-1 font-mono text-[11px] text-oh-muted">
            {{
              detail?.metrics?.requests
                ? (isZh ? `统计窗口请求数：${detail.metrics.requests}` : `Window requests: ${detail.metrics.requests}`)
                : (isZh ? '基于网关真实流量被动观测' : 'Passive gateway traffic observation')
            }}
          </p>
        </div>
      </section>

      <section class="mb-6 min-w-0 max-w-full overflow-x-auto rounded-oh-md border border-oh-default bg-oh-surface">
        <div class="flex min-w-0 flex-wrap items-center justify-between gap-2 border-b border-oh-default px-4 py-2.5">
          <div class="flex min-w-0 flex-wrap items-center gap-2">
            <h2 class="text-xs font-semibold uppercase tracking-wider text-oh-secondary">
              {{ isZh ? '快速调用示例' : 'Quickstart Integration' }}
            </h2>
            <div class="flex min-w-0 flex-wrap items-center gap-1" role="tablist" :aria-label="isZh ? 'SDK 语言切换' : 'SDK language tabs'">
              <button
                v-for="tab in [
                  { id: 'curl', label: 'cURL' },
                  { id: 'ts', label: 'OpenAI SDK (TS)' },
                  { id: 'python', label: 'OpenAI SDK (Python)' }
                ]"
                :key="tab.id"
                type="button"
                role="tab"
                :aria-selected="snippetTab === tab.id"
                :class="[
                  'oh-focus-ring rounded-oh-xs px-2 py-0.5 font-mono text-[11px] transition-colors',
                  snippetTab === tab.id
                    ? 'bg-oh-accent-subtle font-semibold text-oh-accent-text'
                    : 'text-oh-muted hover:text-oh-primary'
                ]"
                @click="snippetTab = tab.id as 'curl' | 'ts' | 'python'"
              >
                {{ tab.label }}
              </button>
            </div>
          </div>
          <CopyButton
            :value="integrationSnippet"
            variant="secondary"
            :aria-label="isZh ? '复制接入代码' : 'Copy snippet'"
          />
        </div>
        <pre
          class="oh-focus-ring min-w-0 max-w-full overflow-x-auto p-4 font-mono text-xs leading-relaxed text-oh-primary"
          tabindex="0"
          role="region"
          :aria-label="isZh ? '模型调用代码示例' : 'Model API code snippet'"
        ><code>{{ integrationSnippet }}</code></pre>
      </section>

      <section class="grid gap-6 py-2 lg:grid-cols-2">
        <div class="min-w-0">
          <div class="flex items-center justify-between gap-2">
            <h2 class="text-sm font-semibold text-oh-primary">
              {{ isZh ? '模型规格与数据治理声明' : 'Capabilities & Data Governance' }}
            </h2>
            <span v-if="primaryFact?.observed_at" class="font-mono text-[11px] text-oh-muted">
              {{ isZh ? `核验于 ${formatPublicTimestamp(primaryFact.observed_at)}` : `Verified ${formatPublicTimestamp(primaryFact.observed_at)}` }}
            </span>
          </div>
          <dl class="mt-3 divide-y divide-oh-subtle rounded-oh-md border border-oh-default bg-oh-surface">
            <div v-for="fact in visibleFacts" :key="fact.key" class="px-4 py-3">
              <div class="flex flex-wrap items-center justify-between gap-3">
                <dt class="text-xs font-medium text-oh-secondary">{{ fact.label }}</dt>
                <dd class="min-w-0 break-words text-right font-mono text-xs tabular-nums text-oh-primary">
                  <Badge v-if="fact.badge" :tone="fact.badgeTone ?? 'neutral'" size="xs" variant="subtle">
                    {{ fact.badge }}
                  </Badge>
                  <span v-else>{{ fact.value }}</span>
                </dd>
              </div>
              <p v-if="fact.sourceNote" class="mt-1 text-[11px] text-oh-muted">
                {{ fact.sourceNote }}
              </p>
            </div>
            <div v-if="shouldCompactUndeclared" class="bg-oh-subtle/40 px-4 py-3">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <span class="font-mono text-[11px] font-medium text-oh-muted">
                  {{
                    isZh
                      ? `其余 ${undeclaredFacts.length} 项能力/治理字段暂未声明`
                      : `${undeclaredFacts.length} additional fields not yet declared`
                  }}
                </span>
                <button
                  type="button"
                  class="oh-focus-ring rounded-oh-xs font-mono text-[11px] text-oh-accent-text hover:underline"
                  :aria-expanded="showUndeclaredDetails"
                  @click="showUndeclaredDetails = !showUndeclaredDetails"
                >
                  {{
                    showUndeclaredDetails
                      ? (isZh ? '收起说明' : 'Hide details')
                      : (isZh ? '查看说明' : 'Show details')
                  }}
                </button>
              </div>
              <div class="mt-2 flex flex-wrap gap-1.5">
                <span
                  v-for="item in undeclaredFacts"
                  :key="item.key"
                  class="inline-flex items-center gap-1 rounded-full border border-oh-subtle bg-oh-surface px-2.5 py-0.5 text-[11px] text-oh-muted"
                >
                  <span>{{ item.label }}</span>
                  <span class="font-mono text-[10px] opacity-75">· {{ isZh ? '未声明' : 'N/A' }}</span>
                </span>
              </div>
              <div v-if="showUndeclaredDetails" class="mt-3 space-y-2 border-t border-oh-subtle pt-2.5">
                <div v-for="item in undeclaredFacts" :key="`${item.key}-note`" class="text-[11px] text-oh-muted">
                  <span class="font-medium text-oh-secondary">{{ item.label }}:</span>
                  {{
                    item.sourceNote ||
                    (isZh ? '模型注册表与上游探测尚未提供该字段的核验声明。' : 'Not yet declared by registry or upstream verification.')
                  }}
                </div>
              </div>
            </div>
          </dl>
        </div>

        <div class="min-w-0 space-y-6">
          <div>
            <h2 class="text-sm font-semibold text-oh-primary">
              {{ isZh ? '已验证协议与能力矩阵' : 'Verified Protocols & Endpoints' }}
            </h2>
            <div
              v-if="detail?.capabilities?.length"
              class="mt-3 divide-y divide-oh-subtle rounded-oh-md border border-oh-default bg-oh-surface"
            >
              <div
                v-for="(cap, index) in detail.capabilities"
                :key="index"
                class="space-y-1.5 px-4 py-3"
              >
                <div class="flex flex-wrap items-center justify-between gap-2">
                  <div class="flex items-center gap-2 font-mono text-xs">
                    <span class="font-semibold text-oh-primary">{{ publicValue(cap.action) }}</span>
                    <span class="text-oh-muted">·</span>
                    <span class="text-oh-secondary">{{ publicValue(cap.protocol) }}</span>
                  </div>
                  <div class="flex items-center gap-1.5">
                    <Badge v-if="cap.mode && cap.mode !== 'unknown'" tone="info" size="xs" variant="subtle">
                      {{ cap.mode }}
                    </Badge>
                    <Badge :tone="cap.state === 'fresh' ? 'success' : 'neutral'" size="xs" variant="outline">
                      {{ evidenceStateLabel(cap.state, isZh) }}
                    </Badge>
                  </div>
                </div>
                <div class="flex flex-wrap gap-3 font-mono text-[11px] text-oh-muted">
                  <span>Tools: {{ cap.tools || 'unknown' }}</span>
                  <span>Stream: {{ cap.stream || 'unknown' }}</span>
                  <span v-if="cap.verified_at">
                    {{ isZh ? `验证时间 ${formatPublicTimestamp(cap.verified_at)}` : `Verified ${formatPublicTimestamp(cap.verified_at)}` }}
                  </span>
                </div>
                <p v-if="cap.limitations?.length" class="text-[11px] text-oh-secondary">
                  {{ isZh ? '已知限制：' : 'Limitations: ' }}{{ cap.limitations.join('; ') }}
                </p>
              </div>
            </div>
            <div
              v-else
              class="mt-3 rounded-oh-md border border-dashed border-oh-default bg-oh-subtle/40 px-4 py-6 text-sm text-oh-secondary"
            >
              {{
                isZh
                  ? '当前模型尚未发布经网关探测核验的协议矩阵快照；仍可按标准兼容协议调用。'
                  : 'No verified protocol compatibility evidence published yet; standard compatible calls remain available.'
              }}
            </div>
          </div>

          <div v-if="detail?.endpoints?.length">
            <h2 class="text-sm font-semibold text-oh-primary">
              {{ isZh ? '上游区域与留存策略' : 'Provider Regions & Retention' }}
            </h2>
            <div class="mt-3 divide-y divide-oh-subtle rounded-oh-md border border-oh-default bg-oh-surface">
              <div v-for="ep in detail.endpoints" :key="ep.id" class="space-y-1 px-4 py-3 text-xs">
                <div class="flex flex-wrap items-center justify-between gap-2">
                  <span class="font-mono font-semibold text-oh-primary">{{ ep.provider }}</span>
                  <span v-if="ep.regions?.length" class="font-mono text-[11px] text-oh-muted">
                    {{ ep.regions.join(', ') }}
                  </span>
                </div>
                <div class="flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-oh-secondary">
                  <span>
                    {{ isZh ? '训练策略：' : 'Training: ' }}
                    <code class="font-mono text-oh-primary">{{ publicValue(ep.training, isZh ? '未声明' : 'Undeclared') }}</code>
                  </span>
                  <span>
                    {{ isZh ? '内容留存：' : 'Retention: ' }}
                    <code class="font-mono text-oh-primary">{{ publicValue(ep.content_retention, isZh ? '未声明' : 'Undeclared') }}</code>
                  </span>
                  <span v-if="ep.safety_retention_days">
                    {{ isZh ? `安全审计留存 ${ep.safety_retention_days} 天` : `Safety retention: ${ep.safety_retention_days}d` }}
                  </span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>

      <section
        v-if="detail?.additional_rates?.length || detail?.time_bands?.length || detail?.benchmarks?.length"
        class="mt-6 grid gap-6 lg:grid-cols-2"
      >
        <div v-if="detail?.additional_rates?.length || detail?.time_bands?.length">
          <h2 class="text-sm font-semibold text-oh-primary">
            {{ isZh ? '阶梯费率与分时折扣' : 'Additional Rate Bands & Time Multipliers' }}
          </h2>
          <div class="mt-3 divide-y divide-oh-subtle rounded-oh-md border border-oh-default bg-oh-surface">
            <div
              v-for="(band, idx) in detail.additional_rates ?? []"
              :key="`rate-${idx}`"
              class="flex flex-wrap items-center justify-between gap-2 px-4 py-2.5 text-xs"
            >
              <div>
                <span class="font-medium text-oh-primary">{{ formatRateBandKind(band.label || band.kind, isZh) }}</span>
                <span v-if="band.min_tokens || band.max_tokens" class="ml-2 font-mono text-[11px] text-oh-muted">
                  [{{ formatContextTokens(band.min_tokens) }} – {{ band.max_tokens ? formatContextTokens(band.max_tokens) : '∞' }}]
                </span>
              </div>
              <span class="font-mono tabular-nums text-oh-primary">{{ formatRateBandPrice(band.rate, isZh) }}</span>
            </div>
            <div
              v-for="(tb, idx) in detail.time_bands ?? []"
              :key="`tb-${idx}`"
              class="flex flex-wrap items-center justify-between gap-2 px-4 py-2.5 text-xs"
            >
              <span class="font-mono text-oh-secondary">
                {{ tb.start_time }}–{{ tb.end_time }} ({{ tb.timezone }})
                <template v-if="tb.weekdays_only"> · {{ isZh ? '仅工作日' : 'Weekdays' }}</template>
              </span>
              <Badge tone="accent" size="xs" variant="subtle" mono>×{{ tb.multiplier }}</Badge>
            </div>
          </div>
        </div>

        <div v-if="detail?.benchmarks?.length">
          <h2 class="text-sm font-semibold text-oh-primary">
            {{ isZh ? '公开基准评测 (Benchmarks)' : 'Published Benchmarks' }}
          </h2>
          <div class="mt-3 divide-y divide-oh-subtle rounded-oh-md border border-oh-default bg-oh-surface">
            <div
              v-for="bm in detail.benchmarks"
              :key="bm.benchmark_id"
              class="flex flex-wrap items-center justify-between gap-2 px-4 py-2.5 text-xs"
            >
              <div>
                <span class="font-mono font-medium text-oh-primary">{{ bm.benchmark_id }}</span>
                <span class="ml-2 text-[11px] text-oh-muted">{{ bm.source }}</span>
              </div>
              <span class="font-mono font-semibold tabular-nums text-oh-primary">
                {{ bm.value }} {{ bm.unit }}
              </span>
            </div>
          </div>
        </div>
      </section>
    </template>
    <EmptyState
      v-else
      :title="isZh ? '未找到该模型的公开详情' : 'No model details published'"
      :description="isZh ? '该模型可能尚未上架或已从公开目录移除。' : 'This model has not been published to the public catalog.'"
    />
  </PublicShell>
</template>
