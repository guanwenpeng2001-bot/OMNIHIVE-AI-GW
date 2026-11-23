import { computed, reactive, ref } from 'vue'
import {
  DEFAULT_EFFECTIVE_CONFIG_LIMIT,
  getEffectiveConfig,
  resetConfigOverrides,
  sanitizeEffectiveConfigItem,
  type ConfigSource,
  type ConfigState,
  type EffectiveConfigItem,
  type ResetConfigResponse
} from '@/api/admin/config'
import type { SystemSettings } from '@/api/admin/settings'
import {
  isStepUpBlocked,
  isStepUpCancelled,
  stepUpBlockReason,
  useStepUp,
  type StepUpController
} from '@/composables/useStepUp'
import {
  classifyAsyncError,
  redactSensitiveText,
  type AsyncErrorInfo
} from '@/features/shared/asyncState'

export type DiagnosticsSyncState = 'loading' | 'refreshing' | 'synced' | 'stale' | 'error'

export interface DiagnosticsSyncSnapshot {
  syncState: DiagnosticsSyncState
  refreshing: boolean
  unhealthyCount: number
  generatedAt: string
}

export interface FormattedDiagnosticToken {
  kind: 'secret' | 'seed-provenance' | 'unavailable' | 'empty-string' | 'null' | 'bool' | 'mono'
  text?: string
  configured?: boolean | null
  seedSource?: string
}

export const KNOWN_CONFIG_GROUPS = [
  'system',
  'security',
  'onboarding',
  'gateway',
  'email',
  'plaza',
  'ops',
  'catalog',
  'logging',
  'image_storage',
  'backup',
  'linuxdo_connect',
  'dingtalk_connect',
  'wechat_connect',
  'oidc_connect',
  'github_oauth',
  'google_oauth'
] as const

export const ATOMIC_RESET_GROUPS = new Set<string>([
  'image_storage',
  'linuxdo_connect',
  'dingtalk_connect',
  'wechat_connect',
  'oidc_connect',
  'github_oauth',
  'google_oauth'
])

export function isSeedProvenanceKey(key: string): boolean {
  return key.endsWith('_install_seed_source')
}

export function isNewUserSeededKey(key: string): boolean {
  return key === 'default_balance' || key === 'default_concurrency'
}

export function canResetConfigItem(item: EffectiveConfigItem): boolean {
  return Boolean(item.resettable && item.authority === 'database_override')
}

export function isAtomicResetGroupItem(item: EffectiveConfigItem): boolean {
  return canResetConfigItem(item) && ATOMIC_RESET_GROUPS.has(item.group)
}

export function resolveEffectiveSeedSource(
  item: EffectiveConfigItem,
  itemsByKey: ReadonlyMap<string, EffectiveConfigItem>
): string {
  if (isSeedProvenanceKey(item.key)) {
    return item.install_seed_source || ''
  }
  const companion = itemsByKey.get(`${item.key}_install_seed_source`)
  if (companion?.install_seed_source) {
    return companion.install_seed_source
  }
  return item.install_seed_source || ''
}

export function formatDiagnosticValueToken(item: EffectiveConfigItem): FormattedDiagnosticToken {
  if (item.sensitive) {
    return {
      kind: 'secret',
      configured: typeof item.configured === 'boolean' ? item.configured : null
    }
  }
  if (isSeedProvenanceKey(item.key) && item.install_seed_source) {
    return {
      kind: 'seed-provenance',
      seedSource: item.install_seed_source
    }
  }
  if (item.state === 'unavailable' && (item.value === null || item.value === undefined)) {
    return { kind: 'unavailable' }
  }
  return formatNonSecretScalarOrStructured(item.value)
}

export function formatBaselinePreviewToken(item: EffectiveConfigItem): FormattedDiagnosticToken {
  if (item.sensitive) {
    return {
      kind: 'secret',
      configured: null
    }
  }
  if (!Object.prototype.hasOwnProperty.call(item, 'baseline') || item.baseline === undefined) {
    return { kind: 'null' }
  }
  return formatNonSecretScalarOrStructured(item.baseline)
}

function formatNonSecretScalarOrStructured(raw: unknown): FormattedDiagnosticToken {
  if (raw === null || raw === undefined) {
    return { kind: 'null' }
  }
  if (typeof raw === 'boolean') {
    return { kind: 'bool', text: raw ? 'true' : 'false' }
  }
  if (typeof raw === 'number') {
    return { kind: 'mono', text: String(raw) }
  }
  if (typeof raw === 'string') {
    if (raw === '') {
      return { kind: 'empty-string' }
    }
    return { kind: 'mono', text: redactSensitiveText(raw) }
  }
  try {
    return { kind: 'mono', text: redactSensitiveText(JSON.stringify(raw)) }
  } catch {
    return { kind: 'mono', text: redactSensitiveText(String(raw)) }
  }
}

export function resolveResetPreviewItems(
  target: EffectiveConfigItem,
  loadedItems: EffectiveConfigItem[]
): EffectiveConfigItem[] {
  const safeTarget = sanitizeEffectiveConfigItem(target)
  if (!isAtomicResetGroupItem(safeTarget)) {
    return [safeTarget]
  }
  const byKey = new Map<string, EffectiveConfigItem>()
  byKey.set(safeTarget.key, safeTarget)
  for (const candidate of loadedItems) {
    if (candidate.group === safeTarget.group && canResetConfigItem(candidate)) {
      byKey.set(candidate.key, sanitizeEffectiveConfigItem(candidate))
    }
  }
  return Array.from(byKey.values()).sort((a, b) => a.key.localeCompare(b.key))
}

export interface UseConfigDiagnosticsOptions {
  pageSize?: number
  stepUp?: StepUpController
  onSettingsUpdated?: (settings: SystemSettings) => void
  onSyncChange?: (snapshot: DiagnosticsSyncSnapshot) => void
}

export function useConfigDiagnostics(options: UseConfigDiagnosticsOptions = {}) {
  const pageSize = options.pageSize ?? DEFAULT_EFFECTIVE_CONFIG_LIMIT
  const stepUp = options.stepUp ?? useStepUp()

  const filters = reactive<{
    q: string
    group: string
    source: ConfigSource | ''
    state: ConfigState | ''
  }>({
    q: '',
    group: '',
    source: '',
    state: ''
  })

  const items = ref<EffectiveConfigItem[]>([])
  const configFile = ref('')
  const generatedAt = ref('')
  const nextCursor = ref<string | undefined>(undefined)

  const hasLoadedOnce = ref(false)
  const loading = ref(false)
  const refreshing = ref(false)
  const loadingMore = ref(false)
  const resetting = ref(false)

  const loadError = ref<AsyncErrorInfo | null>(null)
  const refreshError = ref<AsyncErrorInfo | null>(null)
  const resetError = ref<(AsyncErrorInfo & { stepUpBlockedReason?: string }) | null>(null)
  const pendingResetTarget = ref<EffectiveConfigItem | null>(null)

  let requestSeq = 0

  const itemsByKey = computed(() => {
    const map = new Map<string, EffectiveConfigItem>()
    for (const item of items.value) {
      map.set(item.key, item)
    }
    return map
  })

  const hasActiveFilters = computed(() =>
    Boolean(filters.q.trim() || filters.group || filters.source || filters.state)
  )

  const unhealthyItems = computed(() =>
    items.value.filter((item) => item.state === 'invalid' || item.state === 'unavailable')
  )

  const availableGroups = computed(() => {
    const known = new Set<string>(KNOWN_CONFIG_GROUPS)
    for (const item of items.value) {
      if (item.group) {
        known.add(item.group)
      }
    }
    return Array.from(known)
  })

  const syncState = computed<DiagnosticsSyncState>(() => {
    if (!hasLoadedOnce.value) {
      if (loadError.value) return 'error'
      return 'loading'
    }
    if (refreshError.value) {
      return 'stale'
    }
    if (refreshing.value || loading.value) {
      return 'refreshing'
    }
    return 'synced'
  })

  const resetPreviewItems = computed(() => {
    if (!pendingResetTarget.value) return []
    return resolveResetPreviewItems(pendingResetTarget.value, items.value)
  })

  const resetRequiresRestart = computed(() =>
    resetPreviewItems.value.some((item) => item.restart_required)
  )

  function notifySyncChange(): void {
    options.onSyncChange?.({
      syncState: syncState.value,
      refreshing: refreshing.value || loading.value,
      unhealthyCount: unhealthyItems.value.length,
      generatedAt: generatedAt.value
    })
  }

  async function loadFirstPage(isManualRefresh = false): Promise<void> {
    const seq = ++requestSeq
    if (!hasLoadedOnce.value) {
      loading.value = true
    } else {
      refreshing.value = true
      if (!isManualRefresh) {
        loading.value = true
      }
    }
    loadError.value = null
    refreshError.value = null
    notifySyncChange()

    try {
      const response = await getEffectiveConfig({
        q: filters.q,
        group: filters.group,
        source: filters.source,
        state: filters.state,
        limit: pageSize
      })
      if (seq !== requestSeq) return

      items.value = response.items
      configFile.value = response.config_file
      generatedAt.value = response.generated_at
      nextCursor.value = response.next_cursor
      hasLoadedOnce.value = true
      loadError.value = null
      refreshError.value = null
    } catch (error) {
      if (seq !== requestSeq) return
      const classified = classifyAsyncError(error, 'Failed to load effective configuration')
      if (!hasLoadedOnce.value) {
        items.value = []
        nextCursor.value = undefined
        loadError.value = classified
      } else {
        refreshError.value = classified
      }
    } finally {
      if (seq === requestSeq) {
        loading.value = false
        refreshing.value = false
        notifySyncChange()
      }
    }
  }

  async function loadMore(): Promise<void> {
    if (!nextCursor.value || loadingMore.value || loading.value) return
    const cursor = nextCursor.value
    const seq = requestSeq
    loadingMore.value = true
    refreshError.value = null
    notifySyncChange()

    try {
      const response = await getEffectiveConfig({
        q: filters.q,
        group: filters.group,
        source: filters.source,
        state: filters.state,
        cursor,
        limit: pageSize
      })
      if (seq !== requestSeq) return

      const merged = new Map<string, EffectiveConfigItem>()
      for (const existing of items.value) {
        merged.set(existing.key, existing)
      }
      for (const incoming of response.items) {
        merged.set(incoming.key, incoming)
      }
      items.value = Array.from(merged.values()).sort((a, b) => a.key.localeCompare(b.key))
      configFile.value = response.config_file || configFile.value
      generatedAt.value = response.generated_at || generatedAt.value
      nextCursor.value = response.next_cursor
    } catch (error) {
      if (seq !== requestSeq) return
      refreshError.value = classifyAsyncError(error, 'Failed to load next page')
    } finally {
      if (seq === requestSeq) {
        loadingMore.value = false
        notifySyncChange()
      }
    }
  }

  async function clearFiltersAndReload(): Promise<void> {
    filters.q = ''
    filters.group = ''
    filters.source = ''
    filters.state = ''
    await loadFirstPage(false)
  }

  function openResetConfirm(item: EffectiveConfigItem): void {
    if (!canResetConfigItem(item)) return
    resetError.value = null
    pendingResetTarget.value = sanitizeEffectiveConfigItem(item)
  }

  function closeResetConfirm(): void {
    if (resetting.value) return
    pendingResetTarget.value = null
    resetError.value = null
  }

  function matchesActiveClientFilters(item: EffectiveConfigItem): boolean {
    if (filters.group && item.group !== filters.group) return false
    if (filters.source && item.source !== filters.source) return false
    if (filters.state && item.state !== filters.state) return false
    const query = filters.q.trim().toLowerCase()
    if (
      query &&
      !item.key.toLowerCase().includes(query) &&
      !item.group.toLowerCase().includes(query)
    ) {
      return false
    }
    return true
  }

  async function confirmReset(): Promise<ResetConfigResponse | null> {
    const target = pendingResetTarget.value
    if (!target || !canResetConfigItem(target) || resetting.value) {
      return null
    }

    resetting.value = true
    resetError.value = null

    try {
      const response = await stepUp.run(() => resetConfigOverrides([target.key]))
      const updatedMap = new Map<string, EffectiveConfigItem>()
      for (const existing of items.value) {
        updatedMap.set(existing.key, existing)
      }
      for (const updated of response.items) {
        const sanitized = sanitizeEffectiveConfigItem(updated)
        if (matchesActiveClientFilters(sanitized)) {
          updatedMap.set(sanitized.key, sanitized)
        } else {
          updatedMap.delete(sanitized.key)
        }
      }
      items.value = Array.from(updatedMap.values()).sort((a, b) => a.key.localeCompare(b.key))
      if (response.generated_at) {
        generatedAt.value = response.generated_at
      }
      refreshError.value = null
      pendingResetTarget.value = null
      options.onSettingsUpdated?.(response.settings)
      notifySyncChange()
      return response
    } catch (error) {
      if (isStepUpCancelled(error)) {
        return null
      }
      if (isStepUpBlocked(error)) {
        const classified = classifyAsyncError(error, 'Two-factor step-up verification blocked')
        resetError.value = {
          ...classified,
          stepUpBlockedReason: stepUpBlockReason(error)
        }
        return null
      }
      const classified = classifyAsyncError(error, 'Failed to reset configuration override')
      resetError.value = classified
      if (classified.state === 'unknown') {
        refreshError.value = classified
        notifySyncChange()
      }
      return null
    } finally {
      resetting.value = false
    }
  }

  return {
    filters,
    items,
    itemsByKey,
    configFile,
    generatedAt,
    nextCursor,
    hasLoadedOnce,
    loading,
    refreshing,
    loadingMore,
    resetting,
    loadError,
    refreshError,
    resetError,
    pendingResetTarget,
    resetPreviewItems,
    resetRequiresRestart,
    hasActiveFilters,
    unhealthyItems,
    availableGroups,
    syncState,
    stepUp,
    loadFirstPage,
    loadMore,
    clearFiltersAndReload,
    openResetConfirm,
    closeResetConfirm,
    confirmReset
  }
}
