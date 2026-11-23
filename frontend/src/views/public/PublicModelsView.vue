<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import PublicShell from '@/components/layout/PublicShell.vue'
import CatalogFilterBar from '@/components/catalog/CatalogFilterBar.vue'
import ModelOriginBadge from '@/components/catalog/ModelOriginBadge.vue'
import ModelPriceCell from '@/components/catalog/ModelPriceCell.vue'
import ModelStatusBadge from '@/components/catalog/ModelStatusBadge.vue'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'
import CopyButton from '@/components/ui/CopyButton.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import ErrorState from '@/components/ui/ErrorState.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import { useAuthStore } from '@/stores/auth'
import { useCatalogBrowser } from '@/composables/useCatalogBrowser'
import { getCatalogCopy } from '@/features/catalog/copy'
import {
  modalityLabel,
  modalityTone
} from '@/features/catalog/statusPresentation'
import { extractApiErrorMessage } from '@/utils/apiError'

const { locale } = useI18n()
const isZh = computed(() => String(locale?.value ?? '').startsWith('zh'))
const copy = computed(() => getCatalogCopy(isZh.value))
const router = useRouter()
const authStore = useAuthStore()
const filterBarRef = ref<InstanceType<typeof CatalogFilterBar> | null>(null)

const {
  MAX_SNAPSHOT_RESET_ATTEMPTS,
  models,
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
  selectedRegions,
  selectedRetentions,
  providers,
  providerOptions,
  discoveredRegions,
  favoriteIds,
  favoriteSet,
  onlyFavorites,
  compareIds,
  compareSet,
  hasClientSideFilter,
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
  toggleFavorite,
  toggleCompare,
  clearCompare,
  compareLabel
} = useCatalogBrowser({
  isZh,
  defaultSort: 'availability',
  defaultAvailability: 'live',
  pageLimit: 100
})

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
    selectedProtocols.value.length > 0 ||
    selectedRegions.value.length > 0 ||
    selectedRetentions.value.length > 0 ||
    onlyFavorites.value
  ) {
    resetFilters()
    return
  }
  clearAllFiltersIncludingAvailability()
}

function openConsoleEmptyAction(): void {
  void router.push(authStore.isAuthenticated ? '/dashboard' : '/login')
}

function handleGlobalShortcut(event: KeyboardEvent): void {
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
    window.addEventListener('keydown', handleGlobalShortcut)
  }
  void load(true)
})

onBeforeUnmount(() => {
  if (typeof window !== 'undefined') {
    window.removeEventListener('keydown', handleGlobalShortcut)
  }
})
</script>

<template>
  <PublicShell>
    <section class="flex flex-wrap items-end justify-between gap-4 border-b border-oh-default pb-6">
      <div>
        <p class="font-mono text-xs uppercase tracking-[0.18em] text-oh-muted">
          {{ isZh ? '模型广场 · MODEL PLAZA' : 'MODEL PLAZA' }}
        </p>
        <h1 class="mt-3 text-3xl font-semibold tracking-tight text-oh-primary sm:text-4xl">
          {{ isZh ? '模型广场' : 'Model Plaza' }}
        </h1>
        <p class="mt-2 max-w-[68ch] text-sm text-oh-secondary">
          {{
            isZh
              ? '统一检索并对比已接入可调用与目录收录模型。销售价为主展示位，社区目录（pi）参考价仅作辅助参考（非网关结算价），未观测指标均诚实标注。'
              : 'Search and compare live routable and catalog models. Sale rates appear primary while Community Catalog (pi) reference rates remain secondary and non-settlement.'
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
              : (isZh ? '多模型横向对比' : 'Compare models')
          }}
        </RouterLink>
        <RouterLink
          to="/pricing"
          class="oh-focus-ring inline-flex h-9 items-center rounded-oh-md border border-oh-default bg-oh-surface px-3.5 text-xs font-medium text-oh-secondary hover:border-oh-strong hover:text-oh-primary"
        >
          {{ isZh ? '定价总表' : 'Pricing' }}
        </RouterLink>
      </div>
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
      :selected-regions="selectedRegions"
      :selected-retentions="selectedRetentions"
      :selected-providers="providers"
      :provider-options="providerOptions"
      :discovered-regions="discoveredRegions"
      :sort="sort"
      :view-mode="viewMode"
      :only-favorites="onlyFavorites"
      :show-favorites-toggle="true"
      :favorite-count="favoriteIds.length"
      :show-extended-filters="true"
      :show-governance-filters="true"
      :as-of="asOf"
      :active-filters="activeFilters"
      @update:query="query = $event"
      @update:sort="setSort"
      @update:view-mode="setViewMode"
      @update:only-favorites="onlyFavorites = $event"
      @select-availability="selectAvailabilityState"
      @select-billing="selectBillingFilter"
      @select-origin="selectOriginFilter"
      @select-modality-pill="selectModalityPill"
      @toggle-filter="handleToggleFilter"
      @search="submitSearch"
      @reset="resetFilters"
    />

    <Skeleton v-if="loading" class="py-4" :lines="8" :label="isZh ? '正在加载模型广场' : 'Loading model plaza'" />
    <ErrorState
      v-else-if="error"
      :title="isZh ? '模型广场暂时无法访问' : 'Model plaza unavailable'"
      :message="extractApiErrorMessage(error, '')"
      :retry-label="isZh ? '重新加载' : 'Retry'"
      @retry="load(true)"
    />
    <template v-else-if="displayedViewModels.length === 0">
      <EmptyState
        :variant="activeFilters || availabilityState === 'live' ? 'filtered' : 'empty'"
        :title="
          onlyFavorites
            ? (isZh ? '当前已加载列表中暂无已收藏模型' : 'No favorited models in loaded pages')
            : availabilityState === 'live'
              ? copy.empty.liveNoMatchesTitle
              : activeFilters
                ? (isZh ? '没有符合筛选条件的模型' : 'No models match these filters')
                : (isZh ? '暂未发布模型目录' : 'No public models published yet')
        "
        :description="
          nextCursor && hasClientSideFilter
            ? (isZh
                ? `已扫描 ${models.length} 个模型，仍有更多目录分页未加载；可继续加载下一页以查找匹配项或清空筛选。`
                : `Scanned ${models.length} loaded models with more catalog pages available. Load the next page to find matches or clear filters.`)
            : availabilityState === 'live' && !onlyFavorites
              ? copy.empty.liveNoMatchesDesc
              : activeFilters
                ? (isZh ? '请尝试清空筛选条件，或更换模型关键词与协议类别。' : 'Clear active filters or try a broader search query.')
                : (isZh
                    ? '当管理员在控制台完成上游连接验证、配置渠道销售价并确认模型上架后，模型规格与实时费率将自动在此展示。'
                    : 'Models and rates appear here automatically once an administrator configures upstream connections, pricing, and publication.')
        "
        :action-label="
          activeFilters || availabilityState === 'live'
            ? undefined
            : (authStore.isAuthenticated ? (isZh ? '前往控制台发布模型' : 'Open Console') : (isZh ? '管理员登录配置' : 'Sign in to Console'))
        "
        :clear-filter-label="isZh ? '清除筛选' : 'Clear filters'"
        @action="openConsoleEmptyAction"
        @clear-filters="handleEmptyClearFilters"
      >
        <template v-if="availabilityState === 'live' && !onlyFavorites" #actions>
          <div class="flex flex-wrap items-center justify-center gap-2 pt-1">
            <Button variant="secondary" @click="handleEmptyClearFilters">
              {{ copy.filters.resetFilters }}
            </Button>
            <Button
              data-testid="empty-state-view-catalog-only"
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

      <ErrorState
        v-if="paginationError"
        compact
        class="mt-4"
        :title="
          paginationSnapshotExpired
            ? (isZh ? '目录快照已更新' : 'Catalog snapshot expired')
            : (isZh ? '加载更多模型失败' : 'Could not load more models')
        "
        :message="
          paginationSnapshotExpired
            ? (isZh
                ? '模型目录已发布新快照，请刷新最新目录以继续浏览。'
                : 'A newer model catalog snapshot was published. Reload the latest catalog to continue.')
            : extractApiErrorMessage(paginationError, '')
        "
        :retryable="!paginationSnapshotExpired || snapshotResetAttempts < MAX_SNAPSHOT_RESET_ATTEMPTS"
        :retry-label="
          paginationSnapshotExpired
            ? (isZh ? '刷新最新目录' : 'Reload latest catalog')
            : (isZh ? '重试加载' : 'Retry')
        "
        @retry="retryPagination()"
      />
      <div v-if="nextCursor && !paginationError" class="flex flex-col items-center gap-2 py-4">
        <p role="status" class="text-xs text-oh-muted">
          {{
            isZh
              ? `当前已展示 0 个匹配模型（已加载 ${models.length} 个，仍有后续分页）`
              : `Showing 0 matching models (${models.length} loaded, more pages available)`
          }}
        </p>
        <Button
          data-testid="empty-state-load-more"
          variant="secondary"
          :loading="loadingMore"
          @click="load(false)"
        >
          {{ isZh ? '继续加载下一页' : 'Load next page' }}
        </Button>
      </div>
    </template>

    <template v-else>
      <div
        v-if="viewMode === 'table'"
        class="oh-focus-ring overflow-x-auto rounded-oh-md border border-oh-default bg-oh-surface"
        tabindex="0"
        role="region"
        :aria-label="isZh ? '模型广场目录表格' : 'Model plaza table'"
      >
        <table class="w-full min-w-[860px] border-collapse text-sm">
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
            <tr v-for="vm in displayedViewModels" :key="vm.id" class="border-b border-oh-subtle last:border-0 hover:bg-oh-hover">
              <td class="max-w-[18rem] break-all px-4 py-3 xl:max-w-[24rem]">
                <div class="flex flex-wrap items-center gap-2">
                  <button
                    type="button"
                    class="oh-focus-ring shrink-0 rounded-oh-xs px-0.5 text-sm leading-none transition-colors"
                    :class="favoriteSet.has(vm.id) ? 'text-amber-500' : 'text-oh-muted hover:text-amber-500'"
                    :aria-pressed="favoriteSet.has(vm.id)"
                    :aria-label="
                      favoriteSet.has(vm.id)
                        ? (isZh ? `取消收藏 ${vm.displayName}` : `Remove ${vm.displayName} from favorites`)
                        : (isZh ? `收藏 ${vm.displayName}` : `Favorite ${vm.displayName}`)
                    "
                    @click="toggleFavorite(vm.id)"
                  >
                    {{ favoriteSet.has(vm.id) ? '★' : '☆' }}
                  </button>
                  <RouterLink
                    :to="`/model-plaza/${encodeURIComponent(vm.id)}`"
                    class="oh-focus-ring rounded-oh-xs font-medium text-oh-primary hover:text-oh-accent-text"
                  >
                    {{ vm.displayName }}
                  </RouterLink>
                  <ModelStatusBadge :badge="vm.availability.badge" />
                  <ModelOriginBadge v-if="authStore.isAuthenticated" :badge="vm.origin.primaryBadge" />
                </div>
                <div class="mt-0.5 flex items-center gap-1.5 font-mono text-[11px] text-oh-muted">
                  <span class="truncate">{{ vm.model }}</span>
                  <CopyButton :value="vm.model" size="xs" :aria-label="isZh ? '复制模型名称' : 'Copy model name'" />
                </div>
              </td>
              <td class="px-4 py-3">
                <div class="font-mono text-xs text-oh-primary">{{ vm.provider }}</div>
                <div v-if="vm.displayModalities.length || vm.protocols.length" class="mt-1 flex flex-wrap gap-1">
                  <Badge
                    v-for="mod in vm.displayModalities.slice(0, 4)"
                    :key="mod"
                    :tone="modalityTone(mod)"
                    size="xs"
                    variant="subtle"
                    :data-testid="`modality-badge-${mod}`"
                  >
                    {{ modalityLabel(mod, isZh) }}
                  </Badge>
                  <Badge
                    v-for="proto in vm.protocols.slice(0, 3)"
                    :key="proto"
                    tone="neutral"
                    size="xs"
                    variant="outline"
                    mono
                  >
                    {{ proto }}
                  </Badge>
                </div>
              </td>
              <td class="px-4 py-3 text-right font-mono tabular-nums text-oh-primary">
                {{ vm.contextDisplay }}
              </td>
              <td class="px-4 py-3 text-right">
                <ModelPriceCell
                  :billing="vm.billing"
                  kind="input"
                  :is-zh="isZh"
                  :detail-to="`/model-plaza/${encodeURIComponent(vm.id)}`"
                  @retry="load(true)"
                />
              </td>
              <td class="px-4 py-3 text-right">
                <ModelPriceCell
                  :billing="vm.billing"
                  kind="output"
                  :is-zh="isZh"
                  :detail-to="`/model-plaza/${encodeURIComponent(vm.id)}`"
                  @retry="load(true)"
                />
              </td>
              <td class="px-4 py-3 text-right font-mono tabular-nums text-oh-secondary">
                {{ vm.metrics?.ttft_p50_ms != null ? `${vm.metrics.ttft_p50_ms} ms` : (isZh ? '未观测' : 'Unobserved') }}
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

      <div v-else class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <div
          v-for="vm in displayedViewModels"
          :key="vm.id"
          class="flex flex-col justify-between rounded-oh-md border border-oh-default bg-oh-surface p-4 transition-colors hover:border-oh-strong"
        >
          <div>
            <div class="flex items-start justify-between gap-2">
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
              <button
                type="button"
                class="oh-focus-ring shrink-0 rounded-oh-xs px-1 text-sm leading-none transition-colors"
                :class="favoriteSet.has(vm.id) ? 'text-amber-500' : 'text-oh-muted hover:text-amber-500'"
                :aria-pressed="favoriteSet.has(vm.id)"
                :aria-label="
                  favoriteSet.has(vm.id)
                    ? (isZh ? `取消收藏 ${vm.displayName}` : `Remove ${vm.displayName} from favorites`)
                    : (isZh ? `收藏 ${vm.displayName}` : `Favorite ${vm.displayName}`)
                "
                @click="toggleFavorite(vm.id)"
              >
                {{ favoriteSet.has(vm.id) ? '★' : '☆' }}
              </button>
            </div>
            <div class="mt-2 flex flex-wrap gap-1.5">
              <ModelStatusBadge
                :badge="vm.availability.badge"
                test-id-prefix="card-availability-badge"
              />
              <ModelOriginBadge v-if="authStore.isAuthenticated" :badge="vm.origin.primaryBadge" />
              <Badge
                v-for="mod in vm.displayModalities.slice(0, 3)"
                :key="mod"
                :tone="modalityTone(mod)"
                size="xs"
                variant="subtle"
              >
                {{ modalityLabel(mod, isZh) }}
              </Badge>
            </div>
          </div>
          <div class="mt-4 flex items-end justify-between gap-2 border-t border-oh-subtle pt-3">
            <ModelPriceCell
              :billing="vm.billing"
              kind="pair"
              :is-zh="isZh"
              :detail-to="`/model-plaza/${encodeURIComponent(vm.id)}`"
              compact
              @retry="load(true)"
            />
            <button
              type="button"
              class="oh-focus-ring shrink-0 rounded-oh-sm border px-2.5 py-1 text-xs transition-colors disabled:opacity-40"
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
          </div>
        </div>
      </div>

      <ErrorState
        v-if="paginationError"
        compact
        class="mt-4"
        :title="
          paginationSnapshotExpired
            ? (isZh ? '目录快照已更新' : 'Catalog snapshot expired')
            : (isZh ? '加载更多模型失败' : 'Could not load more models')
        "
        :message="
          paginationSnapshotExpired
            ? (isZh
                ? '模型目录已发布新快照，请刷新最新目录以继续浏览。'
                : 'A newer model catalog snapshot was published. Reload the latest catalog to continue.')
            : extractApiErrorMessage(paginationError, '')
        "
        :retryable="!paginationSnapshotExpired || snapshotResetAttempts < MAX_SNAPSHOT_RESET_ATTEMPTS"
        :retry-label="
          paginationSnapshotExpired
            ? (isZh ? '刷新最新目录' : 'Reload latest catalog')
            : (isZh ? '重试加载' : 'Retry')
        "
        @retry="retryPagination()"
      />
      <div class="flex flex-col items-center gap-2 py-8">
        <p role="status" class="text-xs text-oh-muted">
          {{
            onlyFavorites
              ? (isZh ? `当前已展示 ${displayedViewModels.length} 个收藏模型（已加载 ${models.length} 个）` : `Showing ${displayedViewModels.length} favorited of ${models.length} loaded`)
              : displayedViewModels.length !== models.length
                ? (isZh ? `当前已展示 ${displayedViewModels.length} 个匹配模型（已加载 ${models.length} 个）` : `Showing ${displayedViewModels.length} matching of ${models.length} loaded`)
                : (isZh ? `已加载 ${displayedViewModels.length} 个模型` : `${displayedViewModels.length} models loaded`)
          }}
        </p>
        <Button v-if="nextCursor && !paginationError" variant="secondary" :loading="loadingMore" @click="load(false)">
          {{ isZh ? '加载更多' : 'Load more' }}
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
