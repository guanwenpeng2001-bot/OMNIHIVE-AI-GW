<script setup lang="ts">
import { extractApiErrorMessage } from '@/utils/apiError'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import PublicShell from '@/components/layout/PublicShell.vue'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'
import CopyButton from '@/components/ui/CopyButton.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import ErrorState from '@/components/ui/ErrorState.vue'
import Input from '@/components/ui/Input.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import SegmentedControl from '@/components/ui/SegmentedControl.vue'
import Tooltip from '@/components/ui/Tooltip.vue'
import { useAuthStore } from '@/stores/auth'
import {
  formatContextTokens,
  formatPublicTimestamp,
  maturityLabel,
  publicTokenPrice,
  publicValue,
  routeStateLabel,
  routeStateTone
} from '@/features/public/format'
import { listPublicModels, type ListParams, type PublicModelSummary } from '@/features/public/api'

type ViewMode = 'table' | 'cards'

const { locale } = useI18n()
const isZh = computed(() => String(locale?.value ?? '').startsWith('zh'))

const MODELS_FILTER_MESSAGES = {
  en: {
    regionLabel: 'Region',
    regionUs: 'US',
    regionEu: 'EU',
    regionApac: 'APAC',
    regionGlobal: 'Global',
    retentionLabel: 'Retention',
    retentionZero: 'Zero Retention',
    retentionNone: 'No Retention',
    retentionLimited: 'Limited Retention',
    retentionStandard: 'Standard'
  },
  zh: {
    regionLabel: '部署区域',
    regionUs: '北美 US',
    regionEu: '欧洲 EU',
    regionApac: '亚太 APAC',
    regionGlobal: '全球 Global',
    retentionLabel: '留存合规',
    retentionZero: '零留存 Zero',
    retentionNone: '不落盘 None',
    retentionLimited: '受限审计留存',
    retentionStandard: '标准策略'
  }
} as const

type ModelsFilterMessageKey = keyof typeof MODELS_FILTER_MESSAGES.en

function toMessageFunctions<T extends Record<string, string>>(dict: T) {
  return Object.fromEntries(
    Object.entries(dict).map(([k, v]) => [k, () => v])
  ) as Record<keyof T, () => string>
}

const { t: tFilter } = useI18n({
  useScope: 'local',
  messages: {
    en: toMessageFunctions(MODELS_FILTER_MESSAGES.en),
    zh: toMessageFunctions(MODELS_FILTER_MESSAGES.zh),
    'zh-CN': toMessageFunctions(MODELS_FILTER_MESSAGES.zh)
  }
})

function tf(key: ModelsFilterMessageKey): string {
  const translated = tFilter(key)
  if (translated && translated !== key) return translated
  const active = String(locale?.value || 'en').toLowerCase()
  const dict = active.startsWith('zh') ? MODELS_FILTER_MESSAGES.zh : MODELS_FILTER_MESSAGES.en
  return dict[key]
}

const router = useRouter()
const authStore = useAuthStore()

const models = ref<PublicModelSummary[]>([])
const nextCursor = ref<string | null>(null)
const asOf = ref<string | null>(null)
const loading = ref(true)
const loadingMore = ref(false)
const error = ref<unknown>(null)
const paginationError = ref<unknown>(null)
let request = 0
const query = ref('')
const sort = ref<NonNullable<ListParams['sort']>>('name')
const viewMode = ref<ViewMode>('table')
const providers = ref<string[]>([])
const selectedActions = ref<string[]>([])
const selectedModalities = ref<string[]>([])
const selectedProtocols = ref<string[]>([])
const selectedRegions = ref<string[]>([])
const selectedRetentions = ref<string[]>([])
const searchContainerRef = ref<HTMLElement | null>(null)
const onlyFavorites = ref(false)
const FAVORITES_STORAGE_KEY = 'omnihive.public.favorite_models'
const favoriteIds = ref<string[]>([])
const compareIds = ref<string[]>([])

const providerOptions = ref<string[]>([])
const discoveredRegions = ref<string[]>([])
let appliedQuery = ''

const favoriteSet = computed(() => new Set(favoriteIds.value))
const compareSet = computed(() => new Set(compareIds.value))

function loadFavorites(): void {
  if (typeof window === 'undefined') return
  try {
    const raw = window.localStorage.getItem(FAVORITES_STORAGE_KEY)
    if (!raw) return
    const parsed = JSON.parse(raw)
    if (Array.isArray(parsed)) {
      favoriteIds.value = parsed.filter((entry): entry is string => typeof entry === 'string' && entry.length > 0)
    }
  } catch {
    favoriteIds.value = []
  }
}

function persistFavorites(): void {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(FAVORITES_STORAGE_KEY, JSON.stringify(favoriteIds.value))
  } catch {
    // ignore storage quota errors
  }
}

function toggleFavorite(modelId: string): void {
  if (favoriteSet.value.has(modelId)) {
    favoriteIds.value = favoriteIds.value.filter((entry) => entry !== modelId)
  } else {
    favoriteIds.value = [...favoriteIds.value, modelId]
  }
  persistFavorites()
}

function toggleCompare(modelId: string): void {
  if (compareSet.value.has(modelId)) {
    compareIds.value = compareIds.value.filter((entry) => entry !== modelId)
    return
  }
  if (compareIds.value.length >= 3) return
  compareIds.value = [...compareIds.value, modelId]
}

function clearCompare(): void {
  compareIds.value = []
}

function compareLabel(modelId: string): string {
  const match = models.value.find((entry) => entry.id === modelId)
  return match?.display_name || match?.model || modelId
}

const displayedModels = computed(() => {
  if (!onlyFavorites.value) return models.value
  return models.value.filter((entry) => favoriteSet.value.has(entry.id))
})

const categoryChips = computed(() => [
  { value: 'text.chat', label: isZh.value ? '对话推理' : 'Chat & Reasoning' },
  { value: 'image.generate', label: isZh.value ? '图像生成' : 'Image Generation' },
  { value: 'text.embeddings', label: isZh.value ? '向量嵌入' : 'Embeddings' },
  { value: 'audio.stt', label: isZh.value ? '语音识别' : 'Speech-to-Text' },
  { value: 'audio.tts', label: isZh.value ? '语音合成' : 'Text-to-Speech' }
])

const modalityChips = computed(() => [
  { value: 'text', label: isZh.value ? '文本 Text' : 'Text' },
  { value: 'image', label: isZh.value ? '视觉 Image' : 'Image' },
  { value: 'audio', label: isZh.value ? '音频 Audio' : 'Audio' },
  { value: 'video', label: isZh.value ? '视频 Video' : 'Video' },
  { value: 'pdf', label: 'PDF' }
])

const protocolChips = computed(() => [
  { value: 'openai_chat', label: 'OpenAI Chat' },
  { value: 'openai_responses', label: 'OpenAI Responses' },
  { value: 'anthropic_messages', label: 'Anthropic Messages' },
  { value: 'gemini_generate', label: 'Gemini' }
])

const regionChips = computed(() => {
  const defaults = [
    { value: 'us', label: tf('regionUs') },
    { value: 'eu', label: tf('regionEu') },
    { value: 'apac', label: tf('regionApac') },
    { value: 'global', label: tf('regionGlobal') }
  ]
  const known = new Set(defaults.map((entry) => entry.value))
  const extras = discoveredRegions.value
    .filter((code) => !known.has(code.toLowerCase()))
    .map((code) => ({ value: code.toLowerCase(), label: code.toUpperCase() }))
  return [...defaults, ...extras]
})

const retentionChips = computed(() => [
  { value: 'zero', label: tf('retentionZero') },
  { value: 'none', label: tf('retentionNone') },
  { value: 'limited', label: tf('retentionLimited') },
  { value: 'standard', label: tf('retentionStandard') }
])

const sortOptions = computed(() => [
  { value: 'name', label: isZh.value ? '按模型名称' : 'Sort: Name' },
  { value: 'price_input', label: isZh.value ? '按输入价格' : 'Sort: Input Price' },
  { value: 'context', label: isZh.value ? '按上下文长度' : 'Sort: Context Window' },
  { value: 'ttft', label: isZh.value ? '按首字延迟 (TTFT)' : 'Sort: TTFT' }
])

const activeFilters = computed(
  () =>
    providers.value.length > 0 ||
    selectedActions.value.length > 0 ||
    selectedModalities.value.length > 0 ||
    selectedProtocols.value.length > 0 ||
    selectedRegions.value.length > 0 ||
    selectedRetentions.value.length > 0 ||
    onlyFavorites.value ||
    query.value.trim().length > 0
)

async function load(reset = true): Promise<void> {
  if (!reset && (loading.value || loadingMore.value || !nextCursor.value)) return
  const currentRequest = ++request
  paginationError.value = null
  if (reset) {
    appliedQuery = query.value.trim()
    loadingMore.value = false
    loading.value = true
    error.value = null
  } else {
    loadingMore.value = true
  }
  try {
    const page = await listPublicModels({
      q: appliedQuery || undefined,
      provider: providers.value.length ? providers.value : undefined,
      action: selectedActions.value.length ? selectedActions.value : undefined,
      modality: selectedModalities.value.length ? selectedModalities.value : undefined,
      protocol: selectedProtocols.value.length ? selectedProtocols.value : undefined,
      region: selectedRegions.value.length ? selectedRegions.value : undefined,
      retention: selectedRetentions.value.length ? selectedRetentions.value : undefined,
      sort: sort.value,
      cursor: reset ? undefined : nextCursor.value,
      limit: 20
    })
    if (currentRequest !== request) return
    if (
      !appliedQuery &&
      !providers.value.length &&
      !selectedActions.value.length &&
      !selectedModalities.value.length &&
      !selectedProtocols.value.length &&
      !selectedRegions.value.length &&
      !selectedRetentions.value.length
    ) {
      const seen = new Set(providerOptions.value)
      const seenRegions = new Set(discoveredRegions.value)
      for (const model of page.items ?? []) {
        const provider = model.provider?.trim()
        if (provider && provider !== 'unknown') seen.add(provider)
        for (const reg of model.regions ?? []) {
          const cleaned = reg?.trim().toLowerCase()
          if (cleaned && cleaned !== 'unknown') seenRegions.add(cleaned)
        }
      }
      providerOptions.value = [...seen].sort()
      discoveredRegions.value = [...seenRegions].sort()
    }
    models.value = reset ? page.items ?? [] : [...models.value, ...(page.items ?? [])]
    nextCursor.value = page.next_cursor ?? null
    asOf.value = page.as_of ?? null
  } catch (err) {
    if (currentRequest === request) {
      if (reset) error.value = err
      else paginationError.value = err
    }
  } finally {
    if (currentRequest === request) {
      loading.value = false
      loadingMore.value = false
    }
  }
}

function toggleFilterByKey(
  kind: 'action' | 'modality' | 'protocol' | 'provider' | 'region' | 'retention',
  item: string
): void {
  const map = {
    action: selectedActions,
    modality: selectedModalities,
    protocol: selectedProtocols,
    provider: providers,
    region: selectedRegions,
    retention: selectedRetentions
  }
  const target = map[kind]
  target.value = target.value.includes(item)
    ? target.value.filter((entry) => entry !== item)
    : [...target.value, item]
  void load()
}

function resetFilters(): void {
  query.value = ''
  providers.value = []
  selectedActions.value = []
  selectedModalities.value = []
  selectedProtocols.value = []
  selectedRegions.value = []
  selectedRetentions.value = []
  onlyFavorites.value = false
  sort.value = 'name'
  void load()
}

function displayRate(model: PublicModelSummary, kind: 'input' | 'output'): string {
  if (model.price_state === 'free') return isZh.value ? '免费' : 'Free'
  const rate = kind === 'input' ? model.input_rate : model.output_rate
  return publicTokenPrice(rate, 'adaptive', isZh.value)
}

function modelTtftMs(model: PublicModelSummary): number | null {
  const ms = model.metrics?.ttft_p50_ms
  return typeof ms === 'number' && Number.isFinite(ms) ? ms : null
}

function ttftTooltip(model: PublicModelSummary): string {
  if (model.observed_at) {
    return isZh.value
      ? `目录验证于 ${formatPublicTimestamp(model.observed_at)}；点击进入模型详情查看实时分位延迟与吞吐样本`
      : `Catalog verified ${formatPublicTimestamp(model.observed_at)}; open model details for live P50 TTFT and throughput telemetry`
  }
  return isZh.value
    ? '当前周期尚无足够真实调用样本，点击查看指标披露状态；未观测数据绝不伪造为 0'
    : 'No live sample threshold met yet. Open model details for telemetry status; unobserved metrics are never reported as zero'
}

function openConsoleEmptyAction(): void {
  void router.push(authStore.isAuthenticated ? '/dashboard' : '/login')
}

function handleGlobalShortcut(event: KeyboardEvent): void {
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
    const activeTag = (document.activeElement as HTMLElement | null)?.tagName
    if (activeTag === 'INPUT' || activeTag === 'TEXTAREA') return
    event.preventDefault()
    searchContainerRef.value?.querySelector('input')?.focus()
  }
}

onMounted(() => {
  loadFavorites()
  window.addEventListener('keydown', handleGlobalShortcut)
  void load()
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', handleGlobalShortcut)
})
</script>

<template>
  <PublicShell>
    <section class="flex flex-wrap items-end justify-between gap-4 border-b border-oh-default pb-6">
      <div>
        <p class="font-mono text-xs uppercase tracking-[0.18em] text-oh-muted">
          {{ isZh ? '公开模型目录 · MODEL DIRECTORY' : 'MODEL DIRECTORY' }}
        </p>
        <h1 class="mt-3 text-3xl font-semibold tracking-tight text-oh-primary sm:text-4xl">
          {{ isZh ? '模型广场' : 'Models' }}
        </h1>
        <p class="mt-2 max-w-[68ch] text-sm text-oh-secondary">
          {{
            isZh
              ? '统一检索并对比 OmniHive 已发布上游模型的上下文规格、精确 Token 费率、协议兼容性与实测延迟。未观测或未声明指标均诚实标注，绝不伪造为零。'
              : 'Compare capabilities, exact per-token pricing, protocol support, and observed latency across every published provider. Unknown values stay explicitly labeled, never faked as zero.'
          }}
        </p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <RouterLink
          to="/model-plaza/compare"
          class="oh-focus-ring inline-flex h-9 items-center rounded-oh-md border border-oh-default bg-oh-surface px-3.5 text-xs font-medium text-oh-primary hover:border-oh-strong hover:bg-oh-hover"
        >
          {{ isZh ? '多模型横向对比' : 'Compare models' }}
        </RouterLink>
        <RouterLink
          to="/pricing"
          class="oh-focus-ring inline-flex h-9 items-center rounded-oh-md border border-oh-default bg-oh-surface px-3.5 text-xs font-medium text-oh-secondary hover:border-oh-strong hover:text-oh-primary"
        >
          {{ isZh ? '费率总表' : 'Pricing sheet' }}
        </RouterLink>
      </div>
    </section>

    <section class="flex flex-col gap-3.5 py-6">
      <div class="flex flex-wrap items-center gap-3">
        <div ref="searchContainerRef" class="relative w-full sm:w-80">
          <Input
            v-model="query"
            type="search"
            clearable
            :placeholder="isZh ? '搜索模型 ID、显示名称或供应商… (⌘K)' : 'Search model ID, name, or provider... (⌘K)'"
            @keydown.enter="load()"
            @clear="load()"
          />
        </div>
        <Button variant="secondary" @click="load()">{{ isZh ? '搜索' : 'Search' }}</Button>

        <div class="ml-auto flex flex-wrap items-center gap-2">
          <button
            type="button"
            class="oh-focus-ring inline-flex h-9 items-center gap-1.5 rounded-oh-md border px-3 text-xs font-medium transition-colors"
            :class="
              onlyFavorites
                ? 'border-oh-accent bg-oh-accent-subtle text-oh-accent-text'
                : 'border-oh-default bg-oh-surface text-oh-secondary hover:border-oh-strong hover:text-oh-primary'
            "
            :aria-pressed="onlyFavorites"
            @click="onlyFavorites = !onlyFavorites"
          >
            <span aria-hidden="true">{{ onlyFavorites ? '★' : '☆' }}</span>
            <span>{{ isZh ? '仅看收藏' : 'Favorites' }}</span>
            <span class="font-mono text-[11px] tabular-nums">({{ favoriteIds.length }})</span>
          </button>

          <label class="sr-only" for="public-models-sort">{{ isZh ? '排序方式' : 'Sort models' }}</label>
          <select
            id="public-models-sort"
            v-model="sort"
            class="oh-focus-ring h-9 rounded-oh-md border border-oh-default bg-oh-surface px-3 font-mono text-xs text-oh-primary hover:border-oh-strong"
            @change="load()"
          >
            <option v-for="opt in sortOptions" :key="opt.value" :value="opt.value">
              {{ opt.label }}
            </option>
          </select>

          <SegmentedControl
            :model-value="viewMode"
            :options="[
              { value: 'table', label: isZh ? '表格' : 'Table' },
              { value: 'cards', label: isZh ? '卡片' : 'Cards' }
            ]"
            @update:model-value="viewMode = $event as ViewMode"
          />
        </div>
      </div>

      <div class="flex flex-wrap items-center gap-1.5">
        <span class="mr-1 font-mono text-[11px] uppercase tracking-wider text-oh-muted">
          {{ isZh ? '能力类别' : 'Category' }}
        </span>
        <button
          v-for="chip in categoryChips"
          :key="chip.value"
          type="button"
          class="oh-focus-ring rounded-full border px-2.5 py-1 text-xs transition-colors"
          :class="
            selectedActions.includes(chip.value)
              ? 'border-oh-accent bg-oh-accent-subtle font-medium text-oh-accent-text'
              : 'border-oh-default text-oh-secondary hover:border-oh-strong hover:text-oh-primary'
          "
          :aria-pressed="selectedActions.includes(chip.value)"
          @click="toggleFilterByKey('action', chip.value)"
        >
          {{ chip.label }}
        </button>
      </div>

      <div class="flex flex-wrap items-center gap-1.5">
        <span class="mr-1 font-mono text-[11px] uppercase tracking-wider text-oh-muted">
          {{ isZh ? '输入模态' : 'Modality' }}
        </span>
        <button
          v-for="chip in modalityChips"
          :key="chip.value"
          type="button"
          class="oh-focus-ring rounded-full border px-2.5 py-1 font-mono text-xs transition-colors"
          :class="
            selectedModalities.includes(chip.value)
              ? 'border-oh-accent bg-oh-accent-subtle font-medium text-oh-accent-text'
              : 'border-oh-default text-oh-secondary hover:border-oh-strong hover:text-oh-primary'
          "
          :aria-pressed="selectedModalities.includes(chip.value)"
          @click="toggleFilterByKey('modality', chip.value)"
        >
          {{ chip.label }}
        </button>

        <span class="ml-2 mr-1 font-mono text-[11px] uppercase tracking-wider text-oh-muted">
          {{ isZh ? '兼容协议' : 'Protocol' }}
        </span>
        <button
          v-for="chip in protocolChips"
          :key="chip.value"
          type="button"
          class="oh-focus-ring rounded-full border px-2.5 py-1 font-mono text-xs transition-colors"
          :class="
            selectedProtocols.includes(chip.value)
              ? 'border-oh-accent bg-oh-accent-subtle font-medium text-oh-accent-text'
              : 'border-oh-default text-oh-secondary hover:border-oh-strong hover:text-oh-primary'
          "
          :aria-pressed="selectedProtocols.includes(chip.value)"
          @click="toggleFilterByKey('protocol', chip.value)"
        >
          {{ chip.label }}
        </button>
      </div>

      <div class="flex flex-wrap items-center gap-1.5" data-testid="filter-governance-row">
        <span class="mr-1 font-mono text-[11px] uppercase tracking-wider text-oh-muted">
          {{ tf('regionLabel') }}
        </span>
        <div class="inline-flex flex-wrap items-center gap-1.5" data-testid="filter-region-group">
          <button
            v-for="chip in regionChips"
            :key="chip.value"
            type="button"
            :data-testid="`filter-region-${chip.value}`"
            class="oh-focus-ring rounded-full border px-2.5 py-1 font-mono text-xs transition-colors"
            :class="
              selectedRegions.includes(chip.value)
                ? 'border-oh-accent bg-oh-accent-subtle font-medium text-oh-accent-text'
                : 'border-oh-default text-oh-secondary hover:border-oh-strong hover:text-oh-primary'
            "
            :aria-pressed="selectedRegions.includes(chip.value)"
            @click="toggleFilterByKey('region', chip.value)"
          >
            {{ chip.label }}
          </button>
        </div>

        <span class="ml-2 mr-1 font-mono text-[11px] uppercase tracking-wider text-oh-muted">
          {{ tf('retentionLabel') }}
        </span>
        <div class="inline-flex flex-wrap items-center gap-1.5" data-testid="filter-retention-group">
          <button
            v-for="chip in retentionChips"
            :key="chip.value"
            type="button"
            :data-testid="`filter-retention-${chip.value}`"
            class="oh-focus-ring rounded-full border px-2.5 py-1 font-mono text-xs transition-colors"
            :class="
              selectedRetentions.includes(chip.value)
                ? 'border-oh-accent bg-oh-accent-subtle font-medium text-oh-accent-text'
                : 'border-oh-default text-oh-secondary hover:border-oh-strong hover:text-oh-primary'
            "
            :aria-pressed="selectedRetentions.includes(chip.value)"
            @click="toggleFilterByKey('retention', chip.value)"
          >
            {{ chip.label }}
          </button>
        </div>
      </div>

      <div v-if="providerOptions.length > 0 || activeFilters || asOf" class="flex flex-wrap items-center gap-1.5">
        <span v-if="providerOptions.length > 0" class="mr-1 font-mono text-[11px] uppercase tracking-wider text-oh-muted">
          {{ isZh ? '供应商' : 'Provider' }}
        </span>
        <button
          v-for="provider in providerOptions"
          :key="provider"
          type="button"
          class="oh-focus-ring max-w-full break-all rounded-full border px-2.5 py-1 font-mono text-xs transition-colors"
          :class="providers.includes(provider)
            ? 'border-oh-accent bg-oh-accent-subtle font-medium text-oh-accent-text'
            : 'border-oh-default text-oh-secondary hover:border-oh-strong'"
          :aria-pressed="providers.includes(provider)"
          @click="toggleFilterByKey('provider', provider)"
        >
          {{ provider }}
        </button>
        <button
          v-if="activeFilters"
          type="button"
          class="oh-focus-ring ml-1 rounded-oh-sm text-xs font-medium text-oh-accent-text underline-offset-2 hover:underline"
          @click="resetFilters"
        >
          {{ isZh ? '重置筛选条件' : 'Clear filters' }}
        </button>
        <span v-if="asOf" class="ml-auto font-mono text-[11px] text-oh-muted">
          {{ isZh ? `目录快照 ${formatPublicTimestamp(asOf)}` : `Snapshot ${formatPublicTimestamp(asOf)}` }}
        </span>
      </div>
    </section>

    <section
      v-if="compareIds.length > 0"
      class="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-oh-md border border-oh-accent/40 bg-oh-accent-subtle/50 px-4 py-3"
      role="region"
      :aria-label="isZh ? '快捷模型对比栏' : 'Quick model comparison bar'"
    >
      <div class="flex min-w-0 flex-wrap items-center gap-2">
        <span class="font-mono text-xs font-semibold text-oh-accent-text">
          {{ isZh ? `已选对比 (${compareIds.length}/3)` : `Compare tray (${compareIds.length}/3)` }}
        </span>
        <span
          v-for="selectedId in compareIds"
          :key="selectedId"
          class="inline-flex max-w-full items-center gap-1.5 rounded-full border border-oh-default bg-oh-surface px-2.5 py-0.5 text-xs text-oh-primary"
        >
          <span class="truncate max-w-[12rem]">{{ compareLabel(selectedId) }}</span>
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

    <Skeleton v-if="loading" class="py-4" :lines="8" :label="isZh ? '正在加载公开模型目录' : 'Loading model catalog'" />
    <ErrorState
      v-else-if="error"
      :title="isZh ? '公开模型目录暂时无法访问' : 'Model catalog unavailable'"
      :message="extractApiErrorMessage(error, '')"
      :retry-label="isZh ? '重新加载' : 'Retry'"
      @retry="load()"
    />
    <EmptyState
      v-else-if="displayedModels.length === 0"
      :variant="activeFilters ? 'filtered' : 'empty'"
      :title="
        onlyFavorites
          ? (isZh ? '当前列表下暂无已收藏模型' : 'No favorited models in this view')
          : activeFilters
            ? (isZh ? '没有符合筛选条件的模型' : 'No models match these filters')
            : (isZh ? '暂未发布公开模型目录' : 'No public models published yet')
      "
      :description="
        activeFilters
          ? (isZh ? '请尝试清空筛选条件，或更换模型关键词与协议类别。' : 'Clear active filters or try a broader search query.')
          : (isZh
              ? '当管理员在控制台完成上游连接验证、配置分组定价并确认模型上架后，模型规格与实时费率将自动在此公开展示。'
              : 'Models and per-token rates appear here automatically once an administrator verifies upstream connections, configures pricing, and publishes the catalog.')
      "
      :action-label="
        activeFilters
          ? undefined
          : (authStore.isAuthenticated ? (isZh ? '前往控制台发布模型' : 'Open Console') : (isZh ? '管理员登录配置' : 'Sign in to Console'))
      "
      :clear-filter-label="isZh ? '清空全部筛选' : 'Reset filters'"
      @action="openConsoleEmptyAction"
      @clear-filters="resetFilters"
    />

    <template v-else>
      <div
        v-if="viewMode === 'table'"
        class="oh-focus-ring overflow-x-auto rounded-oh-md border border-oh-default bg-oh-surface"
        tabindex="0"
        role="region"
        :aria-label="isZh ? '公开模型目录表格' : 'Model catalog table'"
      >
        <table class="w-full min-w-[820px] border-collapse text-sm">
          <thead>
            <tr class="border-b border-oh-default bg-oh-subtle text-left font-mono text-[11px] uppercase tracking-wider text-oh-muted">
              <th class="px-4 py-2.5">{{ isZh ? '模型 / ID' : 'Model' }}</th>
              <th class="px-4 py-2.5">{{ isZh ? '供应商与协议' : 'Provider & Protocols' }}</th>
              <th class="px-4 py-2.5 text-right">{{ isZh ? '上下文' : 'Context' }}</th>
              <th class="px-4 py-2.5 text-right">{{ isZh ? '输入 / 1M' : 'Input / 1M' }}</th>
              <th class="px-4 py-2.5 text-right">{{ isZh ? '输出 / 1M' : 'Output / 1M' }}</th>
              <th class="px-4 py-2.5 text-right">{{ isZh ? '首字延迟 (TTFT)' : 'TTFT' }}</th>
              <th class="px-4 py-2.5 text-right">{{ isZh ? '操作' : 'Actions' }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="model in displayedModels" :key="model.id" class="border-b border-oh-subtle last:border-0 hover:bg-oh-hover">
              <td class="max-w-[18rem] break-all px-4 py-3 xl:max-w-[24rem]">
                <div class="flex items-center gap-2">
                  <button
                    type="button"
                    class="oh-focus-ring shrink-0 rounded-oh-xs px-0.5 text-sm leading-none transition-colors"
                    :class="favoriteSet.has(model.id) ? 'text-amber-500' : 'text-oh-muted hover:text-amber-500'"
                    :aria-pressed="favoriteSet.has(model.id)"
                    :aria-label="
                      favoriteSet.has(model.id)
                        ? (isZh ? `取消收藏 ${model.display_name || model.model}` : `Remove ${model.display_name || model.model} from favorites`)
                        : (isZh ? `收藏 ${model.display_name || model.model}` : `Favorite ${model.display_name || model.model}`)
                    "
                    @click="toggleFavorite(model.id)"
                  >
                    {{ favoriteSet.has(model.id) ? '★' : '☆' }}
                  </button>
                  <RouterLink :to="`/model-plaza/${encodeURIComponent(model.id)}`" class="oh-focus-ring rounded-oh-xs font-medium text-oh-primary hover:text-oh-accent-text">
                    {{ model.display_name || model.model }}
                  </RouterLink>
                  <Badge :tone="routeStateTone(model.route_state)" size="xs" variant="subtle">
                    {{ routeStateLabel(model.route_state, isZh) }}
                  </Badge>
                </div>
                <div class="mt-0.5 flex items-center gap-1.5 font-mono text-[11px] text-oh-muted">
                  <span class="truncate">{{ model.model }}</span>
                  <CopyButton :value="model.model" size="xs" :aria-label="isZh ? '复制模型名称' : 'Copy model name'" />
                </div>
              </td>
              <td class="px-4 py-3">
                <div class="font-mono text-xs text-oh-primary">{{ publicValue(model.provider, isZh ? '未声明' : 'Undeclared') }}</div>
                <div v-if="model.modalities?.length || model.protocols?.length" class="mt-1 flex flex-wrap gap-1">
                  <span
                    v-for="mod in (model.modalities ?? []).slice(0, 3)"
                    :key="mod"
                    class="rounded-oh-xs bg-oh-subtle px-1.5 py-0.5 font-mono text-[10px] text-oh-secondary"
                  >
                    {{ mod }}
                  </span>
                  <span
                    v-for="proto in (model.protocols ?? []).slice(0, 2)"
                    :key="proto"
                    class="rounded-oh-xs border border-oh-subtle px-1.5 py-0.5 font-mono text-[10px] text-oh-muted"
                  >
                    {{ proto }}
                  </span>
                </div>
              </td>
              <td class="px-4 py-3 text-right font-mono tabular-nums text-oh-primary">
                {{ formatContextTokens(model.context_tokens) }}
              </td>
              <td class="px-4 py-3 text-right font-mono tabular-nums text-oh-primary">
                {{ displayRate(model, 'input') }}
              </td>
              <td class="px-4 py-3 text-right font-mono tabular-nums text-oh-primary">
                {{ displayRate(model, 'output') }}
              </td>
              <td class="px-4 py-3 text-right font-mono tabular-nums">
                <span v-if="modelTtftMs(model) !== null" class="text-oh-primary">
                  {{ modelTtftMs(model) }} ms
                </span>
                <Tooltip v-else :content="ttftTooltip(model)" placement="left">
                  <RouterLink :to="`/model-plaza/${encodeURIComponent(model.id)}`" class="oh-focus-ring inline-flex rounded-oh-xs">
                    <Badge tone="unknown" size="xs" variant="subtle">
                      {{ isZh ? '未观测' : 'Unobserved' }}
                    </Badge>
                  </RouterLink>
                </Tooltip>
              </td>
              <td class="px-4 py-3 text-right">
                <button
                  type="button"
                  class="oh-focus-ring inline-flex h-7 items-center rounded-oh-xs border px-2 font-mono text-[11px] transition-colors disabled:cursor-not-allowed disabled:opacity-40"
                  :class="
                    compareSet.has(model.id)
                      ? 'border-oh-accent bg-oh-accent-subtle font-medium text-oh-accent-text'
                      : 'border-oh-default text-oh-secondary hover:border-oh-strong hover:text-oh-primary'
                  "
                  :disabled="!compareSet.has(model.id) && compareIds.length >= 3"
                  :aria-pressed="compareSet.has(model.id)"
                  @click="toggleCompare(model.id)"
                >
                  {{ compareSet.has(model.id) ? (isZh ? '已加入' : 'Added') : (isZh ? '+ 对比' : '+ Compare') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-else class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
        <div
          v-for="model in displayedModels"
          :key="model.id"
          class="group flex min-w-0 flex-col justify-between rounded-oh-md border border-oh-default bg-oh-surface p-4 transition-colors hover:border-oh-strong"
        >
          <div class="flex items-start justify-between gap-2">
            <div class="min-w-0">
              <RouterLink
                :to="`/model-plaza/${encodeURIComponent(model.id)}`"
                class="oh-focus-ring block truncate font-medium text-oh-primary group-hover:text-oh-accent-text"
              >
                {{ model.display_name || model.model }}
              </RouterLink>
              <p class="break-all font-mono text-[11px] text-oh-muted">
                {{ publicValue(model.provider, isZh ? '未声明' : 'Undeclared') }} · {{ model.model }}
              </p>
            </div>
            <div class="flex shrink-0 items-center gap-1.5">
              <button
                type="button"
                class="oh-focus-ring rounded-oh-xs px-1 text-sm leading-none transition-colors"
                :class="favoriteSet.has(model.id) ? 'text-amber-500' : 'text-oh-muted hover:text-amber-500'"
                :aria-pressed="favoriteSet.has(model.id)"
                :aria-label="
                  favoriteSet.has(model.id)
                    ? (isZh ? `取消收藏 ${model.display_name || model.model}` : `Remove ${model.display_name || model.model} from favorites`)
                    : (isZh ? `收藏 ${model.display_name || model.model}` : `Favorite ${model.display_name || model.model}`)
                "
                @click="toggleFavorite(model.id)"
              >
                {{ favoriteSet.has(model.id) ? '★' : '☆' }}
              </button>
              <Badge tone="neutral" size="xs" variant="subtle">{{ formatContextTokens(model.context_tokens) }}</Badge>
            </div>
          </div>
          <dl class="mt-4 grid grid-cols-2 gap-2 border-t border-oh-subtle pt-3 font-mono text-xs tabular-nums">
            <div>
              <dt class="text-[10px] uppercase text-oh-muted">{{ isZh ? '输入 / 1M' : 'Input / 1M' }}</dt>
              <dd class="mt-0.5 text-oh-primary">{{ displayRate(model, 'input') }}</dd>
            </div>
            <div>
              <dt class="text-[10px] uppercase text-oh-muted">{{ isZh ? '输出 / 1M' : 'Output / 1M' }}</dt>
              <dd class="mt-0.5 text-oh-primary">{{ displayRate(model, 'output') }}</dd>
            </div>
            <div>
              <dt class="text-[10px] uppercase text-oh-muted">TTFT</dt>
              <dd class="mt-0.5 text-oh-secondary">
                {{ modelTtftMs(model) !== null ? `${modelTtftMs(model)} ms` : (isZh ? '未观测' : 'Unobserved') }}
              </dd>
            </div>
            <div>
              <dt class="text-[10px] uppercase text-oh-muted">{{ isZh ? '成熟度' : 'Stability' }}</dt>
              <dd class="mt-0.5 text-oh-secondary">{{ maturityLabel(model.maturity, isZh) }}</dd>
            </div>
          </dl>
          <div class="mt-3 flex items-center justify-between gap-2 border-t border-oh-subtle pt-2.5">
            <RouterLink
              :to="`/model-plaza/${encodeURIComponent(model.id)}`"
              class="oh-focus-ring rounded-oh-xs text-xs font-medium text-oh-accent-text hover:underline"
            >
              {{ isZh ? '查看详情 →' : 'View details →' }}
            </RouterLink>
            <button
              type="button"
              class="oh-focus-ring inline-flex h-7 items-center rounded-oh-xs border px-2 font-mono text-[11px] transition-colors disabled:cursor-not-allowed disabled:opacity-40"
              :class="
                compareSet.has(model.id)
                  ? 'border-oh-accent bg-oh-accent-subtle font-medium text-oh-accent-text'
                  : 'border-oh-default text-oh-secondary hover:border-oh-strong hover:text-oh-primary'
              "
              :disabled="!compareSet.has(model.id) && compareIds.length >= 3"
              :aria-pressed="compareSet.has(model.id)"
              @click="toggleCompare(model.id)"
            >
              {{ compareSet.has(model.id) ? (isZh ? '已加入对比' : 'In compare') : (isZh ? '+ 加入对比' : '+ Compare') }}
            </button>
          </div>
        </div>
      </div>

      <ErrorState
        v-if="paginationError"
        compact
        class="mt-4"
        :title="isZh ? '加载更多模型失败' : 'Could not load more models'"
        :retry-label="isZh ? '重试加载' : 'Retry'"
        @retry="load(false)"
      />
      <div class="flex flex-col items-center gap-2 py-8">
        <p role="status" class="text-xs text-oh-muted">
          {{
            onlyFavorites
              ? (isZh ? `当前已展示 ${displayedModels.length} 个收藏模型（共加载 ${models.length} 个）` : `Showing ${displayedModels.length} favorited of ${models.length} loaded`)
              : (isZh ? `已加载 ${models.length} 个模型` : `${models.length} models loaded`)
          }}
        </p>
        <Button v-if="nextCursor && !paginationError" variant="secondary" :loading="loadingMore" @click="load(false)">
          {{ isZh ? '加载更多' : 'Load more' }}
        </Button>
      </div>

      <div
        v-if="compareIds.length > 0"
        class="sticky bottom-4 z-20 mt-2 flex flex-wrap items-center justify-between gap-3 rounded-oh-md border border-oh-accent/50 bg-oh-surface/95 px-4 py-2.5 shadow-lg backdrop-blur"
      >
        <div class="flex min-w-0 flex-wrap items-center gap-2 text-xs">
          <span class="font-mono font-semibold text-oh-accent-text">
            {{ isZh ? `对比托盘 ${compareIds.length}/3` : `Compare ${compareIds.length}/3` }}
          </span>
          <span class="truncate text-oh-secondary">
            {{ compareIds.map((selectedId) => compareLabel(selectedId)).join(' · ') }}
          </span>
        </div>
        <div class="flex items-center gap-2">
          <button
            type="button"
            class="oh-focus-ring rounded-oh-xs text-xs text-oh-muted hover:text-oh-primary"
            @click="clearCompare"
          >
            {{ isZh ? '清空' : 'Clear' }}
          </button>
          <RouterLink
            :to="{ path: '/model-plaza/compare', query: { ids: compareIds.join(',') } }"
            class="oh-focus-ring inline-flex h-8 items-center rounded-oh-md bg-[var(--oh-accent,#f59e0b)] px-3 text-xs font-semibold text-slate-950 transition-opacity hover:opacity-90"
          >
            {{ isZh ? '前往对比 →' : 'Compare now →' }}
          </RouterLink>
        </div>
      </div>
    </template>
  </PublicShell>
</template>
