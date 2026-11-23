import { computed, onBeforeUnmount, ref, type ComputedRef } from 'vue'
import { useRoute, useRouter, type LocationQueryRaw } from 'vue-router'
import {
  isCatalogAbortError,
  isSnapshotExpiredError,
  listPublicModels
} from '@/api/catalog'
import type { CatalogModelSummary } from '@/features/catalog/contract'
import type { ModalityFilterKey } from '@/features/catalog/statusPresentation'
import { toCatalogModelVM, type CatalogModelVM } from '@/features/catalog/viewModel'

export type BrowserAvailabilityFilter = 'all' | 'live' | 'unavailable' | 'catalog_only'
export type BrowserBillingFilter = 'all' | 'priced' | 'free' | 'varies' | 'unpriced'
export type BrowserOriginFilter =
  | 'all'
  | 'channel_published'
  | 'reviewed_registry'
  | 'pi_supplement'
  | 'account_discovery'
export type CatalogSortKey = 'name' | 'price_input' | 'context' | 'ttft' | 'availability'
export type CatalogViewMode = 'table' | 'cards'

export interface UseCatalogBrowserOptions {
  isZh: ComputedRef<boolean>
  defaultSort?: CatalogSortKey
  defaultAvailability?: BrowserAvailabilityFilter
  pageLimit?: number
  syncQuery?: boolean
}

const FAVORITES_STORAGE_KEY = 'omnihive:public:favorite-models'
const COMPARE_STORAGE_KEY = 'omnihive:public:compare-models'
const MAX_SNAPSHOT_RESET_ATTEMPTS = 3

function parseQueryString(val: unknown): string {
  if (Array.isArray(val)) return String(val[0] ?? '').trim()
  return typeof val === 'string' ? val.trim() : ''
}

function parseQueryArray(val: unknown): string[] {
  if (Array.isArray(val)) {
    return val.map((item) => String(item ?? '').trim()).filter(Boolean)
  }
  if (typeof val === 'string' && val.trim()) {
    return val
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean)
  }
  return []
}

function modelSupportsInputModality(entry: CatalogModelSummary, wanted: string): boolean {
  const normalized = wanted.trim().toLowerCase()
  const mods = (entry.modalities ?? []).map((item) => item.trim().toLowerCase())
  if (normalized === 'embedding') {
    return (
      mods.includes('embedding') ||
      mods.includes('embeddings') ||
      (entry.actions ?? []).some((act) => act.toLowerCase().includes('embedding'))
    )
  }
  if (normalized === 'audio') {
    return mods.includes('audio') || mods.includes('speech')
  }
  return mods.includes(normalized)
}

function matchesBillingFilter(vm: CatalogModelVM, filter: BrowserBillingFilter): boolean {
  switch (filter) {
    case 'all':
      return true
    case 'priced':
      return (
        vm.billing.state === 'priced' ||
        vm.billing.state === 'conditional' ||
        vm.billing.state === 'partial'
      )
    case 'free':
      return vm.billing.state === 'free'
    case 'varies':
      return vm.billing.isGroupVarying
    case 'unpriced':
      return vm.billing.state === 'unpriced'
  }
}

function matchesAvailabilityFilter(
  vm: CatalogModelVM,
  filter: BrowserAvailabilityFilter
): boolean {
  switch (filter) {
    case 'all':
      return true
    case 'live':
      return vm.availability.state === 'live'
    case 'catalog_only':
      return vm.availability.state === 'catalog_only'
    case 'unavailable':
      return (
        vm.availability.state === 'unavailable' ||
        vm.availability.state === 'listed_unrouted'
      )
  }
}

function matchesOriginFilter(vm: CatalogModelVM, filter: BrowserOriginFilter): boolean {
  if (filter === 'all') return true
  return vm.origin.kinds.includes(filter)
}

export function useCatalogBrowser(options: UseCatalogBrowserOptions) {
  const route = useRoute()
  const router = useRouter()
  const defaultSort = options.defaultSort ?? 'availability'
  const defaultAvailability = options.defaultAvailability ?? 'live'
  const pageLimit = options.pageLimit ?? 100
  const syncQuery = options.syncQuery ?? true

  const initialAvailabilityRaw = parseQueryString(route.query.availability_state || route.query.availability)
  const initialAvailability: BrowserAvailabilityFilter =
    initialAvailabilityRaw === 'all' ||
    initialAvailabilityRaw === 'live' ||
    initialAvailabilityRaw === 'unavailable' ||
    initialAvailabilityRaw === 'catalog_only'
      ? initialAvailabilityRaw
      : defaultAvailability

  const initialBillingRaw = parseQueryString(route.query.billing)
  const initialBilling: BrowserBillingFilter =
    initialBillingRaw === 'priced' ||
    initialBillingRaw === 'free' ||
    initialBillingRaw === 'varies' ||
    initialBillingRaw === 'unpriced'
      ? initialBillingRaw
      : 'all'

  const initialOriginRaw = parseQueryString(route.query.origin)
  const initialOrigin: BrowserOriginFilter =
    initialOriginRaw === 'channel_published' ||
    initialOriginRaw === 'reviewed_registry' ||
    initialOriginRaw === 'pi_supplement' ||
    initialOriginRaw === 'account_discovery'
      ? initialOriginRaw
      : 'all'

  const initialModalityRaw = parseQueryString(route.query.channel || route.query.modality_pill)
  const initialModalityPill: ModalityFilterKey =
    initialModalityRaw === 'text' ||
    initialModalityRaw === 'image' ||
    initialModalityRaw === 'video' ||
    initialModalityRaw === 'audio' ||
    initialModalityRaw === 'embedding' ||
    initialModalityRaw === 'realtime'
      ? initialModalityRaw
      : 'all'

  const initialSortRaw = parseQueryString(route.query.sort)
  const initialSort: CatalogSortKey =
    initialSortRaw === 'name' ||
    initialSortRaw === 'price_input' ||
    initialSortRaw === 'context' ||
    initialSortRaw === 'ttft' ||
    initialSortRaw === 'availability'
      ? initialSortRaw
      : defaultSort

  const initialViewRaw = parseQueryString(route.query.view)
  const viewMode = ref<CatalogViewMode>(initialViewRaw === 'cards' ? 'cards' : 'table')

  const models = ref<CatalogModelSummary[]>([])
  const nextCursor = ref<string | null>(null)
  const asOf = ref<string | null>(null)
  const loading = ref(true)
  const loadingMore = ref(false)
  const error = ref<unknown>(null)
  const paginationError = ref<unknown>(null)
  const paginationSnapshotExpired = ref(false)
  const snapshotResetAttempts = ref(0)

  const query = ref(parseQueryString(route.query.q))
  const sort = ref<CatalogSortKey>(initialSort)
  const availabilityState = ref<BrowserAvailabilityFilter>(initialAvailability)
  const billingFilter = ref<BrowserBillingFilter>(initialBilling)
  const originFilter = ref<BrowserOriginFilter>(initialOrigin)
  const activeModalityPill = ref<ModalityFilterKey>(initialModalityPill)
  const selectedActions = ref<string[]>(parseQueryArray(route.query.action))
  const selectedModalities = ref<string[]>(parseQueryArray(route.query.modality))
  const selectedProtocols = ref<string[]>(parseQueryArray(route.query.protocol))
  const selectedRegions = ref<string[]>(parseQueryArray(route.query.region))
  const selectedRetentions = ref<string[]>(parseQueryArray(route.query.retention))
  const providers = ref<string[]>(parseQueryArray(route.query.provider))
  const providerOptions = ref<string[]>([])
  const discoveredRegions = ref<string[]>([])

  const favoriteIds = ref<string[]>([])
  const onlyFavorites = ref(false)
  const compareIds = ref<string[]>([])
  const compareLabelCache = ref<Record<string, string>>({})

  let requestSeq = 0
  let appliedQuery = query.value.trim()
  let activeAbortController: AbortController | null = null

  function loadPersistedBrowserState(): void {
    if (typeof window === 'undefined') return
    try {
      const rawFavs = window.localStorage.getItem(FAVORITES_STORAGE_KEY)
      if (rawFavs) {
        const parsed = JSON.parse(rawFavs)
        if (Array.isArray(parsed)) {
          favoriteIds.value = parsed.map(String).filter(Boolean)
        }
      }
    } catch {
      // Ignore storage read errors in sandboxed contexts
    }
    try {
      const rawCompare = window.sessionStorage.getItem(COMPARE_STORAGE_KEY)
      if (rawCompare) {
        const parsed = JSON.parse(rawCompare)
        if (Array.isArray(parsed)) {
          compareIds.value = parsed.map(String).filter(Boolean).slice(0, 3)
        }
      }
    } catch {
      // Ignore storage read errors
    }
  }

  function persistFavorites(): void {
    if (typeof window === 'undefined') return
    try {
      window.localStorage.setItem(FAVORITES_STORAGE_KEY, JSON.stringify(favoriteIds.value))
    } catch {
      // Ignore storage write errors
    }
  }

  function persistCompareIds(): void {
    if (typeof window === 'undefined') return
    try {
      window.sessionStorage.setItem(COMPARE_STORAGE_KEY, JSON.stringify(compareIds.value))
    } catch {
      // Ignore storage write errors
    }
  }

  function syncUrlQuery(): void {
    if (!syncQuery || typeof router?.replace !== 'function') return
    const nextQuery: LocationQueryRaw = {}
    if (query.value.trim()) nextQuery.q = query.value.trim()
    if (availabilityState.value !== defaultAvailability) {
      nextQuery.availability_state = availabilityState.value
    }
    if (billingFilter.value !== 'all') nextQuery.billing = billingFilter.value
    if (originFilter.value !== 'all') nextQuery.origin = originFilter.value
    if (activeModalityPill.value !== 'all') nextQuery.channel = activeModalityPill.value
    if (selectedModalities.value.length) nextQuery.modality = selectedModalities.value
    if (selectedProtocols.value.length) nextQuery.protocol = selectedProtocols.value
    if (selectedRegions.value.length) nextQuery.region = selectedRegions.value
    if (selectedRetentions.value.length) nextQuery.retention = selectedRetentions.value
    if (providers.value.length) nextQuery.provider = providers.value
    if (sort.value !== defaultSort) nextQuery.sort = sort.value
    if (viewMode.value !== 'table') nextQuery.view = viewMode.value
    void Promise.resolve(router.replace({ query: nextQuery })).catch(() => {})
  }

  const favoriteSet = computed(() => new Set(favoriteIds.value))
  const compareSet = computed(() => new Set(compareIds.value))

  function toggleFavorite(id: string): void {
    if (favoriteSet.value.has(id)) {
      favoriteIds.value = favoriteIds.value.filter((item) => item !== id)
    } else {
      favoriteIds.value = [...favoriteIds.value, id]
    }
    persistFavorites()
  }

  function toggleCompare(id: string): void {
    if (compareSet.value.has(id)) {
      compareIds.value = compareIds.value.filter((item) => item !== id)
    } else if (compareIds.value.length < 3) {
      const match = models.value.find((item) => item.id === id)
      if (match) {
        compareLabelCache.value = {
          ...compareLabelCache.value,
          [id]: match.display_name || match.model || id
        }
      }
      compareIds.value = [...compareIds.value, id]
    }
    persistCompareIds()
  }

  function clearCompare(): void {
    compareIds.value = []
    persistCompareIds()
  }

  function compareLabel(id: string): string {
    const cached = compareLabelCache.value[id]
    if (cached) return cached
    const match = models.value.find((item) => item.id === id)
    return match?.display_name || match?.model || id
  }

  const viewModels = computed<CatalogModelVM[]>(() =>
    models.value.map((item) =>
      toCatalogModelVM(item, null, { isZh: options.isZh.value, perspective: 'public' })
    )
  )

  const displayedViewModels = computed<CatalogModelVM[]>(() => {
    let list = viewModels.value
    if (availabilityState.value !== 'all') {
      const filteredByAvail = list.filter((vm) =>
        matchesAvailabilityFilter(vm, availabilityState.value)
      )
      // When talking to a legacy fixture/mock that already filtered server-side
      // without returning availability fields on items, keep items only if none had availability.
      const anyHasExplicitAvailability = list.some((vm) => vm.availability.state !== 'unknown')
      if (anyHasExplicitAvailability || availabilityState.value === 'unavailable') {
        list = filteredByAvail
      }
    }
    if (billingFilter.value !== 'all') {
      list = list.filter((vm) => matchesBillingFilter(vm, billingFilter.value))
    }
    if (originFilter.value !== 'all') {
      list = list.filter((vm) => matchesOriginFilter(vm, originFilter.value))
    }
    if (onlyFavorites.value) {
      list = list.filter((vm) => favoriteSet.value.has(vm.id))
    }
    if (activeModalityPill.value !== 'all' && selectedModalities.value.length > 0) {
      list = list.filter((vm) =>
        selectedModalities.value.some((mod) => modelSupportsInputModality(vm.raw, mod))
      )
    }
    return list
  })

  const hasClientSideFilter = computed(
    () =>
      onlyFavorites.value ||
      billingFilter.value !== 'all' ||
      originFilter.value !== 'all' ||
      availabilityState.value === 'unavailable' ||
      (activeModalityPill.value !== 'all' && selectedModalities.value.length > 0)
  )

  const activeFilters = computed(
    () =>
      availabilityState.value !== defaultAvailability ||
      billingFilter.value !== 'all' ||
      originFilter.value !== 'all' ||
      activeModalityPill.value !== 'all' ||
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
    const currentRequest = ++requestSeq
    if (reset) {
      activeAbortController?.abort()
    }
    const controller = new AbortController()
    activeAbortController = controller

    paginationError.value = null
    paginationSnapshotExpired.value = false
    if (reset) {
      appliedQuery = query.value.trim()
      loadingMore.value = false
      loading.value = true
      error.value = null
    } else {
      loadingMore.value = true
    }
    try {
      const backendModalities =
        activeModalityPill.value !== 'all'
          ? [activeModalityPill.value]
          : selectedModalities.value.length
            ? [...selectedModalities.value]
            : undefined
      const page = await listPublicModels(
        {
          q: appliedQuery || undefined,
          provider: providers.value.length ? providers.value : undefined,
          action: selectedActions.value.length ? selectedActions.value : undefined,
          modality: backendModalities,
          protocol: selectedProtocols.value.length ? selectedProtocols.value : undefined,
          region: selectedRegions.value.length ? selectedRegions.value : undefined,
          retention: selectedRetentions.value.length ? selectedRetentions.value : undefined,
          availability_state:
            availabilityState.value === 'live' || availabilityState.value === 'catalog_only'
              ? availabilityState.value
              : undefined,
          availability: availabilityState.value === 'unavailable' ? 'unavailable' : undefined,
          billing:
            billingFilter.value === 'all'
              ? undefined
              : billingFilter.value === 'varies'
                ? 'conflict'
                : billingFilter.value,

          sort: sort.value,
          cursor: reset ? undefined : nextCursor.value ?? undefined,
          limit: pageLimit
        },
        { signal: controller.signal }
      )
      if (currentRequest !== requestSeq) return

      const incoming = page.items ?? []
      if (
        !appliedQuery &&
        activeModalityPill.value === 'all' &&
        !providers.value.length &&
        !selectedActions.value.length &&
        !selectedModalities.value.length &&
        !selectedProtocols.value.length &&
        !selectedRegions.value.length &&
        !selectedRetentions.value.length
      ) {
        const seen = new Set(providerOptions.value)
        const seenRegions = new Set(discoveredRegions.value)
        for (const model of incoming) {
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

      if (incoming.length) {
        const nextCache = { ...compareLabelCache.value }
        for (const item of incoming) {
          nextCache[item.id] = item.display_name || item.model || item.id
        }
        compareLabelCache.value = nextCache
      }
      models.value = reset ? incoming : [...models.value, ...incoming]
      nextCursor.value = page.next_cursor ?? null
      asOf.value = page.as_of ?? null
      if (reset) {
        snapshotResetAttempts.value = 0
      }
    } catch (err) {
      if (currentRequest === requestSeq && !isCatalogAbortError(err)) {
        if (reset) {
          error.value = err
        } else {
          paginationError.value = err
          if (isSnapshotExpiredError(err)) {
            paginationSnapshotExpired.value = true
            nextCursor.value = null
          }
        }
      }
    } finally {
      if (currentRequest === requestSeq) {
        loading.value = false
        loadingMore.value = false
      }
    }
  }

  function retryPagination(): void {
    if (paginationSnapshotExpired.value) {
      if (snapshotResetAttempts.value >= MAX_SNAPSHOT_RESET_ATTEMPTS) return
      snapshotResetAttempts.value += 1
      void load(true)
      return
    }
    void load(false)
  }

  function toggleFilterByKey(
    kind: 'action' | 'modality' | 'protocol' | 'provider' | 'region' | 'retention',
    item: string
  ): void {
    snapshotResetAttempts.value = 0
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
    syncUrlQuery()
    void load(true)
  }

  function selectAvailabilityState(next: BrowserAvailabilityFilter): void {
    snapshotResetAttempts.value = 0
    availabilityState.value = next
    syncUrlQuery()
    void load(true)
  }

  function selectBillingFilter(next: BrowserBillingFilter): void {
    snapshotResetAttempts.value = 0
    billingFilter.value = next
    syncUrlQuery()
    void load(true)
  }

  function selectOriginFilter(next: BrowserOriginFilter): void {
    snapshotResetAttempts.value = 0
    originFilter.value = next
    syncUrlQuery()
    void load(true)
  }

  function selectModalityPill(next: ModalityFilterKey): void {
    snapshotResetAttempts.value = 0
    activeModalityPill.value = next
    syncUrlQuery()
    void load(true)
  }

  function setSort(next: CatalogSortKey): void {
    sort.value = next
    syncUrlQuery()
    void load(true)
  }

  function setViewMode(next: CatalogViewMode): void {
    viewMode.value = next
    syncUrlQuery()
  }

  function submitSearch(): void {
    syncUrlQuery()
    void load(true)
  }

  function resetFilters(): void {
    snapshotResetAttempts.value = 0
    query.value = ''
    availabilityState.value = defaultAvailability
    billingFilter.value = 'all'
    originFilter.value = 'all'
    activeModalityPill.value = 'all'
    providers.value = []
    selectedActions.value = []
    selectedModalities.value = []
    selectedProtocols.value = []
    selectedRegions.value = []
    selectedRetentions.value = []
    onlyFavorites.value = false
    sort.value = defaultSort
    syncUrlQuery()
    void load(true)
  }

  function clearAllFiltersIncludingAvailability(): void {
    snapshotResetAttempts.value = 0
    query.value = ''
    availabilityState.value = 'all'
    billingFilter.value = 'all'
    originFilter.value = 'all'
    activeModalityPill.value = 'all'
    providers.value = []
    selectedActions.value = []
    selectedModalities.value = []
    selectedProtocols.value = []
    selectedRegions.value = []
    selectedRetentions.value = []
    onlyFavorites.value = false
    sort.value = defaultSort
    syncUrlQuery()
    void load(true)
  }

  onBeforeUnmount(() => {
    requestSeq++
    activeAbortController?.abort()
    activeAbortController = null
  })

  return {
    MAX_SNAPSHOT_RESET_ATTEMPTS,
    models,
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
    selectedActions,
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
  }
}
