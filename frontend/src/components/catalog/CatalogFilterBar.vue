<script setup lang="ts">
import { computed, ref } from 'vue'
import Input from '@/components/ui/Input.vue'
import SegmentedControl from '@/components/ui/SegmentedControl.vue'
import { getCatalogCopy } from '@/features/catalog/copy'
import {
  MODALITY_FILTER_PILLS,
  formatPublicTimestamp,
  type ModalityFilterKey
} from '@/features/catalog/statusPresentation'
import type {
  BrowserAvailabilityFilter,
  BrowserBillingFilter,
  BrowserOriginFilter,
  CatalogSortKey
} from '@/composables/useCatalogBrowser'

const props = withDefaults(
  defineProps<{
    isZh: boolean
    query: string
    availabilityState: BrowserAvailabilityFilter
    billingFilter: BrowserBillingFilter
    originFilter?: BrowserOriginFilter
    showOriginFilter?: boolean
    activeModalityPill: ModalityFilterKey
    selectedModalities?: string[]
    selectedProtocols?: string[]
    selectedRegions?: string[]
    selectedRetentions?: string[]
    selectedProviders: string[]
    providerOptions: string[]
    discoveredRegions?: string[]
    sort: CatalogSortKey
    viewMode: 'table' | 'cards'
    onlyFavorites?: boolean
    showFavoritesToggle?: boolean
    favoriteCount?: number
    showExtendedFilters?: boolean
    showGovernanceFilters?: boolean
    availabilityRowTestId?: string
    availabilityTestIdPrefix?: string
    asOf?: string | null
    activeFilters: boolean
  }>(),
  {
    originFilter: 'all',
    showOriginFilter: false,
    selectedModalities: () => [],
    selectedProtocols: () => [],
    selectedRegions: () => [],
    selectedRetentions: () => [],
    discoveredRegions: () => [],
    onlyFavorites: false,
    showFavoritesToggle: false,
    favoriteCount: 0,
    showExtendedFilters: true,
    showGovernanceFilters: true,
    availabilityRowTestId: 'availability-filter-pills-row',
    availabilityTestIdPrefix: 'availability-pill',
    asOf: null
  }
)

const emit = defineEmits<{
  (e: 'update:query', val: string): void
  (e: 'update:sort', val: CatalogSortKey): void
  (e: 'update:viewMode', val: 'table' | 'cards'): void
  (e: 'update:onlyFavorites', val: boolean): void
  (e: 'selectAvailability', val: BrowserAvailabilityFilter): void
  (e: 'selectBilling', val: BrowserBillingFilter): void
  (e: 'selectOrigin', val: BrowserOriginFilter): void
  (e: 'selectModalityPill', val: ModalityFilterKey): void
  (e: 'toggleFilter', kind: 'modality' | 'protocol' | 'provider' | 'region' | 'retention', value: string): void
  (e: 'search'): void
  (e: 'reset'): void
}>()

const searchContainerRef = ref<HTMLElement | null>(null)
const copy = computed(() => getCatalogCopy(props.isZh))

defineExpose({
  focusSearch: () => {
    searchContainerRef.value?.querySelector('input')?.focus()
  }
})

const availabilityPills = computed(() => [
  { key: 'live' as const, label: copy.value.filters.availabilityLive },
  { key: 'unavailable' as const, label: copy.value.filters.availabilityUnavailable },
  { key: 'catalog_only' as const, label: copy.value.filters.availabilityCatalogOnly },
  { key: 'all' as const, label: copy.value.filters.availabilityAll }
])

const billingPills = computed(() => [
  { key: 'all' as const, label: copy.value.filters.billingAll },
  { key: 'priced' as const, label: copy.value.filters.billingPriced },
  { key: 'free' as const, label: copy.value.filters.billingFree },
  { key: 'varies' as const, label: copy.value.filters.billingVaries },
  { key: 'unpriced' as const, label: copy.value.filters.billingUnpriced }
])

const originPills = computed(() => [
  { key: 'all' as const, label: copy.value.filters.originAll },
  { key: 'channel_published' as const, label: copy.value.origins.channelPublished },
  { key: 'reviewed_registry' as const, label: copy.value.origins.reviewedRegistry },
  { key: 'pi_supplement' as const, label: copy.value.origins.communityPi },
  { key: 'account_discovery' as const, label: copy.value.origins.accountDiscovery }
])

const modalityChips = computed(() => [
  { value: 'text', label: props.isZh ? '文本 Text' : 'Text' },
  { value: 'image', label: props.isZh ? '图像 Image' : 'Image' },
  { value: 'audio', label: props.isZh ? '音频 Audio' : 'Audio' },
  { value: 'video', label: props.isZh ? '视频 Video' : 'Video' },
  { value: 'embedding', label: props.isZh ? '向量 Embedding' : 'Embeddings' },
  { value: 'realtime', label: props.isZh ? '实时 Realtime' : 'Realtime' },
  { value: 'pdf', label: 'PDF' }
])

const protocolChips = [
  { value: 'openai_chat', label: 'OpenAI Chat' },
  { value: 'openai_responses', label: 'Responses' },
  { value: 'anthropic_messages', label: 'Anthropic' },
  { value: 'gemini_generate', label: 'Gemini' }
]

const regionChips = computed(() => {
  const defaults = [
    { value: 'us', label: copy.value.filters.regionUs },
    { value: 'eu', label: copy.value.filters.regionEu },
    { value: 'apac', label: copy.value.filters.regionApac },
    { value: 'global', label: copy.value.filters.regionGlobal }
  ]
  const known = new Set(defaults.map((entry) => entry.value))
  const extras = props.discoveredRegions
    .filter((reg) => !known.has(reg))
    .map((reg) => ({ value: reg, label: reg.toUpperCase() }))
  return [...defaults, ...extras]
})

const retentionChips = computed(() => [
  { value: 'zero', label: copy.value.filters.retentionZero },
  { value: 'none', label: copy.value.filters.retentionNone },
  { value: 'limited', label: copy.value.filters.retentionLimited },
  { value: 'standard', label: copy.value.filters.retentionStandard }
])
</script>

<template>
  <section class="min-w-0 max-w-full space-y-3 py-5">
    <div class="flex flex-wrap items-center gap-3">
      <div ref="searchContainerRef" class="min-w-0 basis-full sm:min-w-[16rem] sm:basis-auto sm:flex-1">
        <Input
          :model-value="query"
          :placeholder="copy.filters.searchPlaceholder"
          type="search"
          clearable
          @update:model-value="emit('update:query', String($event))"
          @keydown.enter="emit('search')"
        />
      </div>
      <select
        :value="sort"
        :aria-label="isZh ? '排序方式' : 'Sort models'"
        class="oh-focus-ring h-9 min-w-0 max-w-full truncate rounded-oh-md border border-oh-default bg-oh-surface px-3 text-sm text-oh-primary hover:border-oh-strong"
        @change="emit('update:sort', ($event.target as HTMLSelectElement).value as CatalogSortKey)"
      >
        <option value="availability">{{ isZh ? '按可用性优先' : 'Sort: Availability' }}</option>
        <option value="price_input">{{ isZh ? '按输入价格升序' : 'Sort: Lowest input price' }}</option>
        <option value="context">{{ isZh ? '按上下文窗口降序' : 'Sort: Largest context' }}</option>
        <option value="ttft">{{ isZh ? '按最低首字延迟' : 'Sort: Fastest TTFT' }}</option>
        <option value="name">{{ isZh ? '按模型名称排序' : 'Sort: Name' }}</option>
      </select>
      <SegmentedControl
        :model-value="viewMode"
        :options="[
          { value: 'table', label: isZh ? '表格视图' : 'Table' },
          { value: 'cards', label: isZh ? '卡片视图' : 'Cards' }
        ]"
        @update:model-value="emit('update:viewMode', $event as 'table' | 'cards')"
      />
      <button
        v-if="showFavoritesToggle"
        type="button"
        class="oh-focus-ring inline-flex h-9 items-center gap-1.5 rounded-oh-md border px-3 text-xs font-medium transition-colors"
        :class="
          onlyFavorites
            ? 'border-oh-accent bg-oh-accent-subtle text-oh-accent-text'
            : 'border-oh-default bg-oh-surface text-oh-secondary hover:border-oh-strong hover:text-oh-primary'
        "
        :aria-pressed="onlyFavorites"
        @click="emit('update:onlyFavorites', !onlyFavorites)"
      >
        <span>★</span>
        <span>{{ copy.filters.favoritesOnly }}</span>
        <span v-if="favoriteCount > 0" class="font-mono text-[11px] opacity-80">({{ favoriteCount }})</span>
      </button>
    </div>

    <div class="flex flex-wrap items-center gap-1.5" :data-testid="availabilityRowTestId">
      <span class="mr-1 font-mono text-[11px] uppercase tracking-wider text-oh-muted">
        {{ copy.filters.availabilityLabel }}
      </span>
      <button
        v-for="pill in availabilityPills"
        :key="pill.key"
        type="button"
        :data-testid="`${availabilityTestIdPrefix}-${pill.key}`"
        class="oh-focus-ring rounded-full border px-3 py-1 text-xs font-medium transition-colors"
        :class="
          availabilityState === pill.key
            ? 'border-oh-accent bg-oh-accent-subtle text-oh-accent-text'
            : 'border-oh-default text-oh-secondary hover:border-oh-strong hover:text-oh-primary'
        "
        :aria-pressed="availabilityState === pill.key"
        @click="emit('selectAvailability', pill.key)"
      >
        {{ pill.label }}
      </button>

      <span class="ml-3 mr-1 font-mono text-[11px] uppercase tracking-wider text-oh-muted">
        {{ copy.filters.billingLabel }}
      </span>
      <div class="inline-flex min-w-0 max-w-full flex-wrap items-center gap-1.5" data-testid="billing-filter-pills-row">
        <button
          v-for="pill in billingPills"
          :key="pill.key"
          type="button"
          :data-testid="`billing-pill-${pill.key}`"
          class="oh-focus-ring rounded-full border px-2.5 py-1 text-xs font-medium transition-colors"
          :class="
            billingFilter === pill.key
              ? 'border-oh-accent bg-oh-accent-subtle text-oh-accent-text'
              : 'border-oh-default text-oh-secondary hover:border-oh-strong hover:text-oh-primary'
          "
          :aria-pressed="billingFilter === pill.key"
          @click="emit('selectBilling', pill.key)"
        >
          {{ pill.label }}
        </button>
      </div>
    </div>

    <div
      v-if="showOriginFilter"
      class="flex flex-wrap items-center gap-1.5"
      data-testid="origin-filter-pills-row"
    >
      <span class="mr-1 font-mono text-[11px] uppercase tracking-wider text-oh-muted">
        {{ copy.filters.originLabel }}
      </span>
      <button
        v-for="pill in originPills"
        :key="pill.key"
        type="button"
        :data-testid="`origin-pill-${pill.key}`"
        class="oh-focus-ring rounded-full border px-2.5 py-1 text-xs font-medium transition-colors"
        :class="
          originFilter === pill.key
            ? 'border-oh-accent bg-oh-accent-subtle text-oh-accent-text'
            : 'border-oh-default text-oh-secondary hover:border-oh-strong hover:text-oh-primary'
        "
        :aria-pressed="originFilter === pill.key"
        @click="emit('selectOrigin', pill.key)"
      >
        {{ pill.label }}
      </button>
    </div>

    <div class="flex flex-wrap items-center gap-1.5" data-testid="modality-filter-pills-row">
      <span class="mr-1 font-mono text-[11px] uppercase tracking-wider text-oh-muted">
        {{ isZh ? '能力通道' : 'Channel' }}
      </span>
      <button
        v-for="pill in MODALITY_FILTER_PILLS"
        :key="pill.key"
        type="button"
        :data-testid="`modality-pill-${pill.key}`"
        class="oh-focus-ring rounded-full border px-3 py-1 text-xs font-medium transition-colors"
        :class="
          activeModalityPill === pill.key
            ? 'border-oh-accent bg-oh-accent-subtle text-oh-accent-text'
            : 'border-oh-default text-oh-secondary hover:border-oh-strong hover:text-oh-primary'
        "
        :aria-pressed="activeModalityPill === pill.key"
        @click="emit('selectModalityPill', pill.key)"
      >
        {{ isZh ? pill.labelZh : pill.labelEn }}
      </button>
    </div>

    <div v-if="showExtendedFilters" class="flex flex-wrap items-center gap-1.5">
      <span class="mr-1 font-mono text-[11px] uppercase tracking-wider text-oh-muted">
        {{ copy.filters.modalityInputLabel }}
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
        @click="emit('toggleFilter', 'modality', chip.value)"
      >
        {{ chip.label }}
      </button>

      <span class="ml-2 mr-1 font-mono text-[11px] uppercase tracking-wider text-oh-muted">
        {{ copy.filters.protocolLabel }}
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
        @click="emit('toggleFilter', 'protocol', chip.value)"
      >
        {{ chip.label }}
      </button>
    </div>

    <div v-if="showGovernanceFilters" class="flex flex-wrap items-center gap-1.5" data-testid="filter-governance-row">
      <span class="mr-1 font-mono text-[11px] uppercase tracking-wider text-oh-muted">
        {{ copy.filters.regionLabel }}
      </span>
      <div class="inline-flex min-w-0 max-w-full flex-wrap items-center gap-1.5" data-testid="filter-region-group">
        <button
          v-for="chip in regionChips"
          :key="chip.value"
          type="button"
          :data-testid="`filter-region-${chip.value}`"
          class="oh-focus-ring max-w-full break-all rounded-full border px-2.5 py-1 font-mono text-xs transition-colors"
          :class="
            selectedRegions.includes(chip.value)
              ? 'border-oh-accent bg-oh-accent-subtle font-medium text-oh-accent-text'
              : 'border-oh-default text-oh-secondary hover:border-oh-strong hover:text-oh-primary'
          "
          :aria-pressed="selectedRegions.includes(chip.value)"
          @click="emit('toggleFilter', 'region', chip.value)"
        >
          {{ chip.label }}
        </button>
      </div>

      <span class="ml-2 mr-1 font-mono text-[11px] uppercase tracking-wider text-oh-muted">
        {{ copy.filters.retentionLabel }}
      </span>
      <div class="inline-flex min-w-0 max-w-full flex-wrap items-center gap-1.5" data-testid="filter-retention-group">
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
          @click="emit('toggleFilter', 'retention', chip.value)"
        >
          {{ chip.label }}
        </button>
      </div>
    </div>

    <div v-if="providerOptions.length > 0 || activeFilters || asOf" class="flex flex-wrap items-center gap-1.5">
      <span v-if="providerOptions.length > 0" class="mr-1 font-mono text-[11px] uppercase tracking-wider text-oh-muted">
        {{ copy.filters.providerLabel }}
      </span>
      <button
        v-for="provider in providerOptions"
        :key="provider"
        type="button"
        class="oh-focus-ring max-w-full break-all rounded-full border px-2.5 py-1 font-mono text-xs transition-colors"
        :class="
          selectedProviders.includes(provider)
            ? 'border-oh-accent bg-oh-accent-subtle font-medium text-oh-accent-text'
            : 'border-oh-default text-oh-secondary hover:border-oh-strong'
        "
        :aria-pressed="selectedProviders.includes(provider)"
        @click="emit('toggleFilter', 'provider', provider)"
      >
        {{ provider }}
      </button>
      <button
        v-if="activeFilters"
        type="button"
        class="oh-focus-ring ml-1 rounded-oh-sm text-xs font-medium text-oh-accent-text underline-offset-2 hover:underline"
        @click="emit('reset')"
      >
        {{ copy.filters.resetFilters }}
      </button>
      <span v-if="asOf" class="ml-auto font-mono text-[11px] text-oh-muted">
        {{ isZh ? `目录快照 ${formatPublicTimestamp(asOf)}` : `Snapshot ${formatPublicTimestamp(asOf)}` }}
      </span>
    </div>
  </section>
</template>
