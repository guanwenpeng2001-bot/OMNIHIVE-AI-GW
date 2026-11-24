<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import { catalogWorkspace, resolveCatalogWorkspace } from '@/features/catalog/adminWorkspace'
import { useAppStore } from '@/stores/app'
import { normalizeApiError } from '@/utils/apiError'
import {
  buildUnpricedModelsCsv,
  deduplicateAdminPublications,
  formatRatePairDisplay,
  getAdminCatalogModelDetail,
  isAdminCatalogSnapshotExpired,
  listAdminCatalogModels,
  matchesAdminCatalogQuickFilter,
  type AdminCatalogAvailabilityState,
  type AdminCatalogBillingState,
  type AdminCatalogDetail,
  type AdminCatalogModelSummary,
  type AdminCatalogPublicationState,
  type AdminCatalogQuickFilter,
  type AdminCatalogSourceKind
} from '@/api/admin/modelCatalog'
import AdminModelDetailDrawer from './models/AdminModelDetailDrawer.vue'

let i18nCtx: { t?: (k: string, p?: Record<string, unknown>) => string; locale?: { value?: string } } | null =
  null
try {
  i18nCtx = useI18n()
} catch {
  i18nCtx = { locale: { value: 'zh-CN' } }
}

const isZh = computed(() =>
  String(i18nCtx?.locale?.value ?? 'zh-CN')
    .toLowerCase()
    .startsWith('zh')
)

let routeQuery: Record<string, unknown> = {}
try {
  const route = useRoute()
  routeQuery = (route?.query as Record<string, unknown>) ?? {}
} catch {
  routeQuery = {}
}

const appStore = useAppStore()

function resolveInitialWorkspaceId(): string {
  const fromQuery = typeof routeQuery.workspace_id === 'string' ? routeQuery.workspace_id.trim() : ''
  if (/^[1-9]\d*$/.test(fromQuery)) return fromQuery
  return catalogWorkspace(appStore)
}

function resolveInitialProjectId(): string {
  const fromQuery = typeof routeQuery.project_id === 'string' ? routeQuery.project_id.trim() : ''
  if (/^[1-9]\d*$/.test(fromQuery)) return fromQuery
  return ''
}

const workspaceId = ref(resolveInitialWorkspaceId())
const projectId = ref(resolveInitialProjectId())

// Filters
const quickFilter = ref<AdminCatalogQuickFilter | ''>('')
const searchQuery = ref('')
const providerFilter = ref('')
const sourceFilter = ref('')
const availabilityFilter = ref('')
const billingFilter = ref('')
const publicationFilter = ref<AdminCatalogPublicationState | ''>('')

// List state
const items = ref<AdminCatalogModelSummary[]>([])
const catalogVersion = ref('')
const asOf = ref<string | null>(null)
const scopeVersion = ref<string | null>(null)
const nextCursor = ref<string | null>(null)
const loading = ref(false)
const loadingMore = ref(false)
const listError = ref('')
const listSnapshotExpired = ref(false)

const lastExportedCsv = ref('')

// Detail Drawer state
const drawerOpen = ref(false)
const drawerInitialTab = ref<'inspector' | 'verification'>('inspector')
const activeModelId = ref<string | null>(null)
const activeDetail = ref<AdminCatalogDetail | null>(null)
const detailLoading = ref(false)
const loadingMoreAccounts = ref(false)
const loadingMoreOffers = ref(false)
const detailError = ref('')
const detailSnapshotExpired = ref(false)

let listRequestGeneration = 0
let detailRequestGeneration = 0
let listAbortController: AbortController | null = null
let detailAbortController: AbortController | null = null

onUnmounted(() => {
  listAbortController?.abort()
  detailAbortController?.abort()
})

const QUICK_FILTER_OPTIONS: Array<{
  id: AdminCatalogQuickFilter | ''
  zh: string
  en: string
  isP0?: boolean
}> = [
  { id: '', zh: '全部模型', en: 'All Models' },
  {
    id: 'routable_but_unpriced',
    zh: '可路由但未定价 (P0)',
    en: 'Routable but Unpriced (P0)',
    isP0: true
  },
  {
    id: 'discovered_not_enabled',
    zh: '已发现但未启用',
    en: 'Discovered Not Enabled'
  },
  {
    id: 'listed_unrouted',
    zh: '已上架无路由',
    en: 'Listed Unrouted'
  },
  {
    id: 'group_price_conflict',
    zh: '分组价格冲突',
    en: 'Group Price Conflict'
  },
  {
    id: 'pi_only',
    zh: '仅社区目录（pi）',
    en: 'Community Catalog (pi) Only'
  },
  {
    id: 'stale',
    zh: '资料或发现过期',
    en: 'Stale Evidence'
  }
]

const quickFilterCounts = ref<Partial<Record<AdminCatalogQuickFilter, number>>>({})

const visibleItems = computed(() => {
  return items.value.filter((item) => {
    if (providerFilter.value && item.provider !== providerFilter.value) {
      return false
    }
    if (
      sourceFilter.value &&
      !item.source.kinds.includes(sourceFilter.value as AdminCatalogSourceKind)
    ) {
      return false
    }
    if (availabilityFilter.value && item.availability.state !== availabilityFilter.value) {
      return false
    }
    if (billingFilter.value && item.billing.state !== billingFilter.value) {
      return false
    }
    if (publicationFilter.value && item.availability.publication !== publicationFilter.value) {
      return false
    }
    if (searchQuery.value.trim()) {
      const q = searchQuery.value.trim().toLowerCase()
      const haystack = `${item.id} ${item.canonical_id} ${item.provider} ${item.model} ${item.display_name}`.toLowerCase()
      if (!haystack.includes(q)) return false
    }
    return true
  })
})

const SOURCE_LABELS: Record<AdminCatalogSourceKind, { zh: string; en: string }> = {
  builtin_reviewed: { zh: '内置审阅', en: 'Approved Registry' },
  platform_preset: { zh: '平台候选', en: 'Platform Candidates' },
  builtin_supplement: { zh: '内置补充', en: 'Custom Supplement' },
  pi_ai_reference: { zh: '社区目录（pi）', en: 'Community Catalog (pi)' },
  account_discovery: { zh: '账号原生发现', en: 'Native Discovered' },
  account_declaration: { zh: '连接声明', en: 'Connection Declared' },
  external_reference: { zh: '外部参考', en: 'External Reference' },
  heuristic: { zh: '启发式估算', en: 'Heuristic' },
  unknown: { zh: '未知来源', en: 'Unknown Source' }
}

const AVAILABILITY_LABELS: Record<AdminCatalogAvailabilityState, { zh: string; en: string }> = {
  live: { zh: '可调用', en: 'Live (Routable)' },
  listed_unrouted: { zh: '已上架·无路由', en: 'Listed · Unrouted' },
  catalog_only: { zh: '仅目录收录', en: 'Catalog Only' },
  requires_scope: { zh: '需项目作用域', en: 'Requires Scope' },
  unavailable: { zh: '不可用', en: 'Unavailable' },
  unknown: { zh: '状态未知', en: 'Unknown State' }
}

const BILLING_LABELS: Record<AdminCatalogBillingState, { zh: string; en: string }> = {
  priced: { zh: '销售价已配置', en: 'Channel Priced (Sale Rate)' },
  free: { zh: '免费调用', en: 'Free' },
  conditional: { zh: '按条件定价', en: 'Conditional Rate' },
  partial: { zh: '部分维度定价', en: 'Partial Dimensions' },
  conflict: { zh: '分组价格冲突', en: 'Group Price Conflict' },
  varies: { zh: '按分组定价', en: 'Group-Specific Pricing' },
  unpriced: { zh: '未配置销售价', en: 'Unpriced (No Sale Rate)' },
  error: { zh: '价格加载失败', en: 'Pricing Unavailable' },
  unknown: { zh: '价格状态未知', en: 'Billing Unknown' }
}

function sourceBadgeLabel(item: AdminCatalogModelSummary, kind: AdminCatalogSourceKind): string {
  const base = SOURCE_LABELS[kind] ?? SOURCE_LABELS.unknown
  const label = isZh.value ? base.zh : base.en
  if (kind === 'account_discovery' && item.discovered_accounts_count !== null) {
    return `${label}: ${item.discovered_accounts_count}`
  }
  return label
}

function availabilityLabel(state: AdminCatalogAvailabilityState): string {
  const entry = AVAILABILITY_LABELS[state] ?? AVAILABILITY_LABELS.unknown
  return isZh.value ? entry.zh : entry.en
}

function billingStateLabel(state: AdminCatalogBillingState): string {
  const entry = BILLING_LABELS[state] ?? BILLING_LABELS.unknown
  return isZh.value ? entry.zh : entry.en
}

function formatSaleRate(item: AdminCatalogModelSummary): string {
  if (!item.billing.sale_price) return '—'
  return formatRatePairDisplay(
    item.billing.sale_price.input_rate,
    item.billing.sale_price.output_rate
  )
}

function formatReferenceRate(item: AdminCatalogModelSummary): string | null {
  if (!item.billing.reference_price) return null
  const pair = formatRatePairDisplay(
    item.billing.reference_price.input_rate,
    item.billing.reference_price.output_rate
  )
  return pair !== '—' ? pair : null
}

function isRowRoutableUnpriced(item: AdminCatalogModelSummary): boolean {
  return matchesAdminCatalogQuickFilter(item, 'routable_but_unpriced')
}


async function fetchCatalogModels(append = false) {
  const ws = workspaceId.value.trim()
  if (!/^[1-9]\d*$/.test(ws)) {
    listError.value = isZh.value
      ? '请输入有效的正整数工作空间 ID（如 1）。'
      : 'Please enter a valid positive integer Workspace ID (e.g. 1).'
    return
  }
  const proj = projectId.value.trim()
  if (proj && !/^[1-9]\d*$/.test(proj)) {
    listError.value = isZh.value
      ? '项目 ID 必须为正整数，或留空以查询整个工作空间。'
      : 'Project ID must be a positive integer or empty for workspace-wide query.'
    return
  }

  listAbortController?.abort()
  const controller = new AbortController()
  listAbortController = controller
  const currentGen = ++listRequestGeneration

  if (append) {
    loadingMore.value = true
  } else {
    loading.value = true
    listSnapshotExpired.value = false
  }
  listError.value = ''

  try {
    const page = await listAdminCatalogModels(
      {
        workspace_id: ws,
        project_id: proj || undefined,
        provider: providerFilter.value || undefined,
        q: searchQuery.value.trim() || undefined,
        source: sourceFilter.value || undefined,
        publication: publicationFilter.value || undefined,
        availability: availabilityFilter.value || undefined,
        billing: billingFilter.value || undefined,
        quick_filter: quickFilter.value || undefined,
        cursor: append ? nextCursor.value : undefined,
        limit: 50
      },
      controller.signal
    )
    if (currentGen !== listRequestGeneration) return

    quickFilterCounts.value = page.quick_filter_counts
    catalogVersion.value = page.catalog_version
    asOf.value = page.as_of
    scopeVersion.value = page.scope_version
    nextCursor.value = page.next_cursor

    if (append) {
      const existingIds = new Set(items.value.map((i) => i.id))
      items.value = [
        ...items.value,
        ...page.items.filter((i) => !existingIds.has(i.id))
      ]
    } else {
      items.value = page.items
    }
  } catch (cause) {
    if (currentGen !== listRequestGeneration) return
    if (isAdminCatalogSnapshotExpired(cause)) {
      listSnapshotExpired.value = true
      nextCursor.value = null
      listError.value = isZh.value
        ? '目录快照游标已过期（410 SNAPSHOT_EXPIRED）。请点击「刷新目录快照」重新加载。'
        : 'Catalog snapshot cursor expired (410 SNAPSHOT_EXPIRED). Please refresh the catalog snapshot.'
    } else {
      const normalized = normalizeApiError(cause)
      listError.value =
        normalized.message ||
        (isZh.value ? '读取模型目录快照失败。' : 'Failed to load model catalog snapshot.')
    }
  } finally {
    if (currentGen === listRequestGeneration) {
      loading.value = false
      loadingMore.value = false
    }
  }
}

function selectQuickFilter(filter: AdminCatalogQuickFilter | '') {
  quickFilter.value = filter
  void fetchCatalogModels(false)
}

function clearAllFilters() {
  quickFilter.value = ''
  searchQuery.value = ''
  providerFilter.value = ''
  sourceFilter.value = ''
  availabilityFilter.value = ''
  billingFilter.value = ''
  publicationFilter.value = ''
  void fetchCatalogModels(false)
}

async function openModelDetail(item: AdminCatalogModelSummary) {
  activeModelId.value = item.id
  drawerInitialTab.value = 'inspector'
  drawerOpen.value = true
  await fetchModelDetail(item.id, catalogVersion.value || undefined)
}

function openVerificationWorkbenchDrawer() {
  detailAbortController?.abort()
  detailRequestGeneration++
  activeDetail.value = null
  detailError.value = ''
  detailLoading.value = false
  const first = visibleItems.value[0]
  activeModelId.value = first?.id ?? null
  drawerInitialTab.value = 'verification'
  drawerOpen.value = true
  if (first) void fetchModelDetail(first.id, catalogVersion.value)
}

async function fetchModelDetail(
  modelId: string,
  version?: string,
  cursors?: { accounts_cursor?: string; offers_cursor?: string }
) {
  const ws = workspaceId.value.trim()
  if (!/^[1-9]\d*$/.test(ws)) {
    detailError.value = isZh.value ? '请选择工作空间。' : 'Select a workspace.'
    return
  }
  const proj = projectId.value.trim() || undefined

  detailAbortController?.abort()
  const controller = new AbortController()
  detailAbortController = controller
  const currentGen = ++detailRequestGeneration

  const isAppendingAccounts = Boolean(cursors?.accounts_cursor)
  const isAppendingOffers = Boolean(cursors?.offers_cursor)

  if (isAppendingAccounts) loadingMoreAccounts.value = true
  else if (isAppendingOffers) loadingMoreOffers.value = true
  else {
    detailLoading.value = true
    detailSnapshotExpired.value = false
  }
  detailError.value = ''

  try {
    const response = await getAdminCatalogModelDetail(
      modelId,
      {
        workspace_id: ws,
        project_id: proj,
        catalog_version:
          version && version !== 'unknown' ? version : undefined,
        accounts_cursor: cursors?.accounts_cursor,
        offers_cursor: cursors?.offers_cursor
      },
      controller.signal
    )
    if (currentGen !== detailRequestGeneration) return

    if ((isAppendingAccounts || isAppendingOffers) && activeDetail.value) {
      const prev = activeDetail.value
      const mergedAccounts = isAppendingAccounts
        ? [...prev.accounts, ...response.accounts]
        : prev.accounts
      const mergedOffers = isAppendingOffers
        ? [...prev.offers, ...response.offers]
        : prev.offers
      const mergedPublications = deduplicateAdminPublications([
        ...prev.publications,
        ...response.publications
      ])
      activeDetail.value = {
        ...response,
        accounts: mergedAccounts,
        offers: mergedOffers,
        publications: mergedPublications,
        next_accounts_cursor: isAppendingAccounts
          ? response.next_accounts_cursor
          : prev.next_accounts_cursor,
        next_offers_cursor: isAppendingOffers
          ? response.next_offers_cursor
          : prev.next_offers_cursor
      }
    } else {
      activeDetail.value = response
    }
  } catch (cause) {
    if (currentGen !== detailRequestGeneration) return
    if (isAdminCatalogSnapshotExpired(cause)) {
      detailSnapshotExpired.value = true
      detailError.value = isZh.value
        ? '模型详情游标或版本已过期（410 SNAPSHOT_EXPIRED），请刷新详情快照。'
        : 'Model detail cursor or version expired (410 SNAPSHOT_EXPIRED). Refresh detail snapshot.'
    } else {
      const normalized = normalizeApiError(cause)
      detailError.value =
        normalized.message ||
        (isZh.value ? '读取模型详情证据链失败。' : 'Failed to load model detail.')
    }
  } finally {
    if (currentGen === detailRequestGeneration) {
      detailLoading.value = false
      loadingMoreAccounts.value = false
      loadingMoreOffers.value = false
    }
  }
}

function handleLoadMoreAccounts() {
  if (!activeModelId.value || !activeDetail.value?.next_accounts_cursor) return
  void fetchModelDetail(activeModelId.value, activeDetail.value.catalog_version, {
    accounts_cursor: activeDetail.value.next_accounts_cursor
  })
}

function handleLoadMoreOffers() {
  if (!activeModelId.value || !activeDetail.value?.next_offers_cursor) return
  void fetchModelDetail(activeModelId.value, activeDetail.value.catalog_version, {
    offers_cursor: activeDetail.value.next_offers_cursor
  })
}

function handleRefreshDetail() {
  if (!activeModelId.value) return
  // Refresh without pinning a stale catalog_version
  void fetchModelDetail(activeModelId.value)
}

function handleExportUnpricedCsv() {
  const csv = buildUnpricedModelsCsv(visibleItems.value)
  lastExportedCsv.value = csv
  try {
    if (typeof Blob !== 'undefined' && typeof URL !== 'undefined' && URL.createObjectURL) {
      const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' })
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.setAttribute('download', `omnihive-unpriced-models-${workspaceId.value}.csv`)
      document.body.appendChild(link)
      link.click()
      document.body.removeChild(link)
      URL.revokeObjectURL(url)
    }
  } catch {
    // In headless/unit test environments without Blob URL download, lastExportedCsv holds the output
  }
}

onMounted(async () => {
  if (!workspaceId.value) {
    const resolved = await resolveCatalogWorkspace(appStore)
    if (!workspaceId.value) workspaceId.value = resolved
  }
  void fetchCatalogModels(false)
})
</script>

<template>
  <AppLayout>
    <div
      data-testid="admin-models-catalog-view"
      class="w-full min-w-0 max-w-full space-y-4 pb-12 text-[var(--foreground)] tabular-nums"
    >
      <!-- Top Header & Canonical Terminology Strip -->
      <header
        class="rounded-xl border border-[var(--border)] bg-[var(--card)] p-4 sm:p-5 space-y-3"
      >
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div class="min-w-0 max-w-full space-y-1">
            <div class="flex min-w-0 flex-wrap items-center gap-2">
              <h1 class="text-lg font-bold tracking-tight">
                {{ isZh ? '模型目录' : 'Model Catalog' }}
              </h1>
              <span
                v-if="catalogVersion && catalogVersion !== 'unknown'"
                data-testid="catalog-version-pill"
                class="min-w-0 max-w-full break-all rounded bg-[var(--surface-2)] px-2 py-0.5 font-mono text-[11px] text-[var(--muted-foreground)]"
              >
                {{ catalogVersion }}
              </span>
            </div>
            <p class="text-xs text-[var(--muted-foreground)]">
              {{
                isZh
                  ? '按规范模型聚合核验四段漏斗与四层价格体系，排查可路由但未定价 (P0)、已发现未启用及规格来源冲突。'
                  : 'Per-model inspection workbench for the 4-stage funnel and 4 price layers; diagnose routable-but-unpriced (P0), discovered-not-enabled, and provenance conflicts.'
              }}
            </p>
            <p data-testid="catalog-read-only-note" class="text-xs text-[var(--muted-foreground)]">
              {{ isZh ? '目录为只读视图；模型启用与定价请在' : 'This catalog is read-only. Manage model availability and prices in' }}
              <a href="/admin/accounts" class="underline">{{ isZh ? '上游连接' : 'Upstream Connections' }}</a>
              {{ isZh ? '/' : 'and' }}
              <a href="/admin/channels/pricing" class="underline">{{ isZh ? '渠道与定价' : 'Channels & Pricing' }}</a>{{ isZh ? ' 中调整。' : '.' }}
            </p>
          </div>

          <div class="flex flex-wrap items-center gap-2">
            <button
              type="button"
              data-testid="open-catalog-verification-workbench"
              class="btn btn-secondary btn-sm text-xs font-semibold"
              @click="openVerificationWorkbenchDrawer"
            >
              {{ isZh ? '目录核验工作台' : 'Verification Workbench' }}
            </button>
            <button
              type="button"
              data-testid="export-unpriced-csv"
              class="btn btn-secondary btn-sm text-xs"
              @click="handleExportUnpricedCsv"
            >
              {{ isZh ? '导出当前列表缺价 CSV' : 'Export Unpriced CSV (Loaded Rows)' }}
            </button>
            <button
              type="button"
              data-testid="refresh-catalog-snapshot"
              class="btn btn-primary btn-sm text-xs"
              :disabled="loading"
              @click="fetchCatalogModels(false)"
            >
              {{ isZh ? '刷新目录快照' : 'Refresh Snapshot' }}
            </button>
          </div>
        </div>

        <!-- Canonical 4-Stage Funnel & 4 Price Layers Legend Strip -->
        <div
          class="grid grid-cols-1 gap-2 rounded-lg border border-[var(--border)] bg-[var(--surface-2)]/50 p-3 text-xs lg:grid-cols-2"
        >
          <div data-testid="admin-models-funnel-legend" class="flex flex-wrap items-center gap-1.5">
            <span class="font-semibold text-[var(--foreground)]">
              {{ isZh ? '四段漏斗：' : '4-Stage Funnel:' }}
            </span>
            <span class="font-mono text-[11px] text-[var(--muted-foreground)]">
              {{
                isZh
                  ? '1. 平台候选 -> 2. 账号原生发现 -> 3. 账号有效模型 -> 4. 分组对外可见'
                  : '1. Platform Candidates -> 2. Native Discovered -> 3. Account Effective -> 4. Group Visible'
              }}
            </span>
          </div>
          <div
            data-testid="admin-models-price-layer-legend"
            class="flex flex-wrap items-center gap-1.5"
          >
            <span class="font-semibold text-[var(--foreground)]">
              {{ isZh ? '价格分层：' : '4 Price Layers:' }}
            </span>
            <span class="font-mono text-[11px] text-[var(--muted-foreground)]">
              {{
                isZh
                  ? '1. 参考价（非结算价） / 2. 销售价 / 3. 内部成本 / 4. 实收结算价'
                  : '1. Reference Price (Non-settlement) / 2. Sale Rate / 3. Internal Cost / 4. Settlement Amount'
              }}
            </span>
          </div>
        </div>
      </header>

      <!-- Diagnostic Quick Filters & Scope Controls -->
      <section
        aria-label="模型目录筛选栏"
        class="rounded-xl border border-[var(--border)] bg-[var(--card)] p-4 space-y-3"
      >
        <!-- Quick Filters Row -->
        <div class="flex flex-wrap items-center gap-2" role="group" aria-label="诊断快筛">
          <button
            v-for="qf in QUICK_FILTER_OPTIONS"
            :key="qf.id || 'all'"
            type="button"
            :data-testid="`quick-filter-${qf.id || 'all'}`"
            :aria-pressed="quickFilter === qf.id"
            class="inline-flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-xs font-semibold transition-colors"
            :class="
              quickFilter === qf.id
                ? qf.isP0
                  ? 'border-red-500 bg-red-500/15 text-red-700 dark:text-red-300'
                  : 'border-[var(--brand,#f59e0b)] bg-[var(--brand,#f59e0b)] text-[#141008]'
                : qf.isP0
                  ? 'border-red-500/30 bg-red-500/5 text-red-600 dark:text-red-400 hover:bg-red-500/10'
                  : 'border-[var(--border)] bg-[var(--surface-2)] text-[var(--muted-foreground)] hover:text-[var(--foreground)]'
            "
            @click="selectQuickFilter(qf.id)"
          >
            <span>{{ isZh ? qf.zh : qf.en }}</span>
            <span
              v-if="qf.id && quickFilterCounts[qf.id] !== undefined"
              class="rounded bg-black/10 px-1.5 py-0.5 font-mono text-[10px] dark:bg-white/10"
            >
              {{ quickFilterCounts[qf.id] }}
            </span>
          </button>
        </div>

        <!-- Search, Scope & Dimension Filters Row -->
        <div class="grid grid-cols-1 gap-2.5 sm:grid-cols-2 lg:grid-cols-6">
          <label class="space-y-1 text-xs">
            <span class="text-[var(--muted-foreground)]">
              {{ isZh ? '工作空间 ID (必填)' : 'Workspace ID' }}
            </span>
            <input
              v-model="workspaceId"
              type="text"
              data-testid="admin-models-workspace-input"
              class="input w-full font-mono text-xs"
              @change="fetchCatalogModels(false)"
            />
          </label>

          <label class="space-y-1 text-xs">
            <span class="text-[var(--muted-foreground)]">
              {{ isZh ? '项目 ID (可选)' : 'Project ID (Optional)' }}
            </span>
            <input
              v-model="projectId"
              type="text"
              data-testid="admin-models-project-input"
              :placeholder="isZh ? '全部项目' : 'All projects'"
              class="input w-full font-mono text-xs"
              @change="fetchCatalogModels(false)"
            />
          </label>

          <label class="space-y-1 text-xs lg:col-span-2">
            <span class="text-[var(--muted-foreground)]">
              {{ isZh ? '搜索规范 ID / 模型名 / 别名' : 'Search Canonical ID / Model / Alias' }}
            </span>
            <input
              v-model="searchQuery"
              type="search"
              data-testid="admin-models-search-input"
              :placeholder="isZh ? '输入关键字筛选或按回车查询…' : 'Filter or press Enter to query…'"
              class="input w-full text-xs"
              @keydown.enter.prevent="fetchCatalogModels(false)"
            />
          </label>

          <label class="space-y-1 text-xs">
            <span class="text-[var(--muted-foreground)]">
              {{ isZh ? '来源 (Axis 1)' : 'Source (Axis 1)' }}
            </span>
            <select
              v-model="sourceFilter"
              data-testid="admin-models-source-filter"
              class="input w-full text-xs"
              @change="fetchCatalogModels(false)"
            >
              <option value="">{{ isZh ? '全部来源' : 'All Sources' }}</option>
              <option value="builtin_reviewed">{{ isZh ? '内置审阅' : 'Approved Registry' }}</option>
              <option value="builtin_supplement">{{ isZh ? '内置补充' : 'Custom Supplement' }}</option>
              <option value="pi_ai_reference">{{ isZh ? '社区目录（pi）' : 'Community Catalog (pi)' }}</option>
              <option value="account_discovery">{{ isZh ? '账号原生发现' : 'Native Discovered' }}</option>
            </select>
          </label>

          <label class="space-y-1 text-xs">
            <span class="text-[var(--muted-foreground)]">
              {{ isZh ? '可用性 (Axis 2)' : 'Availability (Axis 2)' }}
            </span>
            <select
              v-model="availabilityFilter"
              data-testid="admin-models-availability-filter"
              class="input w-full text-xs"
              @change="fetchCatalogModels(false)"
            >
              <option value="">{{ isZh ? '全部可用性' : 'All Availability' }}</option>
              <option value="live">{{ isZh ? '可调用 (live)' : 'Live (Routable)' }}</option>
              <option value="listed_unrouted">{{ isZh ? '已上架·无路由' : 'Listed · Unrouted' }}</option>
              <option value="catalog_only">{{ isZh ? '仅目录收录' : 'Catalog Only' }}</option>
              <option value="unavailable">{{ isZh ? '不可用' : 'Unavailable' }}</option>
            </select>
          </label>
        </div>
      </section>

      <!-- 410 Snapshot Expired Alert -->
      <div
        v-if="listSnapshotExpired"
        role="alert"
        data-testid="list-snapshot-expired"
        class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-amber-500/40 bg-amber-500/10 p-4 text-xs text-amber-800 dark:text-amber-200"
      >
        <span>{{ listError }}</span>
        <button
          type="button"
          data-testid="list-expired-refresh-button"
          class="btn btn-secondary btn-sm text-xs font-semibold"
          @click="fetchCatalogModels(false)"
        >
          {{ isZh ? '刷新目录快照' : 'Refresh Snapshot' }}
        </button>
      </div>

      <!-- General Error Banner -->
      <div
        v-else-if="listError"
        role="alert"
        data-testid="list-error-banner"
        class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-red-500/40 bg-red-500/10 p-4 text-xs text-red-700 dark:text-red-300"
      >
        <span>{{ listError }}</span>
        <button
          type="button"
          data-testid="list-retry-button"
          class="btn btn-secondary btn-sm text-xs"
          @click="fetchCatalogModels(false)"
        >
          {{ isZh ? '立即重试' : 'Retry' }}
        </button>
      </div>

      <!-- Exported CSV feedback (for visual confirmation & headless tests) -->
      <div
        v-if="lastExportedCsv"
        data-testid="exported-csv-preview"
        class="rounded-xl border border-emerald-500/30 bg-emerald-500/5 p-3 text-xs"
      >
        <div class="flex items-center justify-between">
          <span class="font-semibold text-emerald-700 dark:text-emerald-300">
            {{ isZh ? '已生成缺价模型 CSV 清单' : 'Unpriced Models CSV Checklist Generated' }}
          </span>
          <button
            type="button"
            class="text-[11px] underline"
            @click="lastExportedCsv = ''"
          >
            {{ isZh ? '收起' : 'Dismiss' }}
          </button>
        </div>
        <pre class="mt-2 max-h-28 overflow-auto rounded bg-[var(--card)] p-2 font-mono text-[11px]">{{ lastExportedCsv }}</pre>
      </div>

      <!-- Main Catalog Table -->
      <section class="overflow-hidden rounded-xl border border-[var(--border)] bg-[var(--card)]">
        <div
          v-if="loading && items.length === 0"
          data-testid="admin-models-loading"
          class="p-8 text-center text-sm text-[var(--muted-foreground)]"
        >
          {{ isZh ? '正在读取模型目录快照…' : 'Loading model catalog snapshot…' }}
        </div>

        <div
          v-else-if="visibleItems.length === 0"
          data-testid="admin-models-empty"
          class="p-8 text-center space-y-3"
        >
          <p class="text-sm text-[var(--muted-foreground)]">
            {{ isZh ? '当前筛选条件下没有匹配的模型记录。' : 'No models match the active filters.' }}
          </p>
          <button
            type="button"
            data-testid="clear-filters-button"
            class="btn btn-secondary btn-sm text-xs"
            @click="clearAllFilters"
          >
            {{ isZh ? '清除筛选条件' : 'Clear Filters' }}
          </button>
        </div>

        <div v-else class="w-full min-w-0 max-w-full overflow-x-auto">
          <table
            data-testid="admin-models-table"
            class="w-full text-left text-xs tabular-nums"
          >
            <thead
              class="border-b border-[var(--border)] bg-[var(--surface-2)] text-[var(--muted-foreground)]"
            >
              <tr>
                <th class="px-3 py-2.5 font-semibold">
                  {{ isZh ? '规范模型 / 厂商' : 'Canonical Model / Provider' }}
                </th>
                <th class="px-3 py-2.5 font-semibold">
                  {{ isZh ? '来源徽标组 (Axis 1)' : 'Source Badges (Axis 1)' }}
                </th>
                <th class="px-3 py-2.5 font-semibold">
                  {{ isZh ? '可用性 (Axis 2)' : 'Availability (Axis 2)' }}
                </th>
                <th class="px-3 py-2.5 font-semibold">
                  {{ isZh ? '四段漏斗绑定' : 'Funnel Bindings' }}
                </th>
                <th class="px-3 py-2.5 font-semibold">
                  {{ isZh ? '价格层级 / 告警 (Axis 3)' : 'Price Layer / Alerts (Axis 3)' }}
                </th>
                <th class="px-3 py-2.5 text-right font-semibold">
                  {{ isZh ? '操作' : 'Actions' }}
                </th>
              </tr>
            </thead>
            <tbody class="divide-y divide-[var(--border)]">
              <tr
                v-for="item in visibleItems"
                :key="item.id"
                data-testid="admin-model-row"
                :data-model-id="item.id"
                class="cursor-pointer transition-colors hover:bg-[var(--surface-2)]/60"
                @click="openModelDetail(item)"
              >

                <!-- Column 1: Canonical Model & Provider -->
                <td class="px-3 py-3">
                  <div class="font-mono font-semibold text-[var(--foreground)]">
                    {{ item.provider }}/{{ item.model }}
                  </div>
                  <div class="mt-0.5 flex flex-wrap items-center gap-1.5 text-[11px] text-[var(--muted-foreground)]">
                    <span>{{ item.display_name }}</span>
                    <span class="font-mono">({{ item.id }})</span>
                    <span v-if="item.aliases_count" class="rounded bg-[var(--surface-2)] px-1.5 py-0.2">
                      {{ isZh ? `${item.aliases_count} 别名` : `${item.aliases_count} alias(es)` }}
                    </span>
                  </div>
                </td>

                <!-- Column 2: Source Badges (Axis 1) -->
                <td class="px-3 py-3">
                  <div class="flex flex-wrap items-center gap-1.5">
                    <span
                      v-for="kind in item.source.kinds"
                      :key="kind"
                      data-testid="model-source-badge"
                      class="rounded border border-[var(--border)] bg-[var(--surface-2)] px-2 py-0.5 text-[11px]"
                    >
                      {{ sourceBadgeLabel(item, kind) }}
                    </span>
                    <span
                      v-if="item.has_conflict"
                      data-testid="row-provenance-conflict-badge"
                      class="rounded border border-amber-500/50 bg-amber-500/15 px-2 py-0.5 text-[11px] font-semibold text-amber-700 dark:text-amber-300"
                    >
                      {{ isZh ? '规格来源冲突' : 'Provenance Conflict' }}
                    </span>
                  </div>
                </td>

                <!-- Column 3: Availability (Axis 2) -->
                <td class="px-3 py-3">
                  <div class="flex flex-wrap items-center gap-1.5">
                    <span
                      data-testid="model-availability-badge"
                      class="inline-flex items-center gap-1 rounded px-2 py-0.5 text-[11px] font-semibold"
                      :class="
                        item.availability.state === 'live'
                          ? 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-300'
                          : item.availability.state === 'listed_unrouted'
                            ? 'bg-amber-500/15 text-amber-700 dark:text-amber-300'
                            : 'bg-[var(--surface-2)] text-[var(--muted-foreground)]'
                      "
                    >
                      {{ availabilityLabel(item.availability.state) }}
                    </span>
                    <span
                      class="rounded border border-[var(--border)] px-1.5 py-0.5 text-[10px] text-[var(--muted-foreground)]"
                    >
                      {{
                        item.availability.publication === 'published'
                          ? isZh
                            ? '分组对外可见'
                            : 'Group Visible'
                          : item.availability.publication
                      }}
                    </span>
                  </div>
                </td>

                <!-- Column 4: Bindings (Connections / Groups) -->
                <td class="px-3 py-3 font-mono text-[11px]">
                  <div>
                    {{
                      isZh
                        ? `有效连接: ${item.accounts_count ?? '—'} (发现: ${item.discovered_accounts_count ?? '—'})`
                        : `Effective: ${item.accounts_count ?? '—'} (Discovered: ${item.discovered_accounts_count ?? '—'})`
                    }}
                  </div>
                  <div class="text-[var(--muted-foreground)]">
                    {{
                      isZh
                        ? `对外分组: ${item.groups_count ?? '—'} · 渠道: ${item.channels_count ?? '—'}`
                        : `Groups: ${item.groups_count ?? '—'} · Channels: ${item.channels_count ?? '—'}`
                    }}
                  </div>
                </td>

                <!-- Column 5: Effective Price Layer (Axis 3) + Secondary Reference Price -->
                <td class="px-3 py-3">
                  <div class="flex flex-wrap items-center gap-1.5">
                    <span
                      v-if="isRowRoutableUnpriced(item)"
                      data-testid="row-p0-unpriced-badge"
                      class="rounded border border-red-500/50 bg-red-500/15 px-2 py-0.5 text-[11px] font-bold text-red-600 dark:text-red-400"
                    >
                      {{ isZh ? '缺销售价 P0' : 'Missing Sale Rate P0' }}
                    </span>
                    <span
                      v-else-if="item.billing.state === 'conflict'"
                      data-testid="row-price-conflict-badge"
                      class="rounded border border-amber-500/50 bg-amber-500/15 px-2 py-0.5 text-[11px] font-semibold text-amber-700 dark:text-amber-300"
                    >
                      {{ isZh ? '分组价格冲突' : 'Group Price Conflict' }}
                    </span>
                    <span class="font-mono font-semibold">
                      <template v-if="item.billing.sale_price">
                        {{ isZh ? '销售价' : 'Sale Rate' }}: {{ formatSaleRate(item) }}
                      </template>
                      <template v-else>
                        {{ billingStateLabel(item.billing.state) }}
                      </template>
                    </span>
                  </div>
                  <!-- Strictly secondary muted Reference Price (Non-settlement) -->
                  <div
                    v-if="formatReferenceRate(item)"
                    data-testid="row-reference-price"
                    class="mt-0.5 text-[11px] text-[var(--muted-foreground)]"
                  >
                    {{
                      isZh
                        ? `参考价（非结算价）：${formatReferenceRate(item)} · 社区目录（pi）`
                        : `Reference Price (Non-settlement): ${formatReferenceRate(item)} · Community Catalog (pi)`
                    }}
                  </div>
                </td>

                <!-- Column 6: Actions -->
                <td class="px-3 py-3 text-right" @click.stop>
                  <button
                    type="button"
                    data-testid="inspect-model-button"
                    class="btn btn-secondary btn-sm text-xs"
                    @click="openModelDetail(item)"
                  >
                    {{ isZh ? '查看详情' : 'Inspect' }}
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- Cursor Pagination Footer -->
        <div
          v-if="nextCursor"
          class="flex items-center justify-center border-t border-[var(--border)] bg-[var(--surface-2)]/40 p-3"
        >
          <button
            type="button"
            data-testid="load-more-models"
            class="btn btn-secondary btn-sm text-xs"
            :disabled="loadingMore"
            @click="fetchCatalogModels(true)"
          >
            {{
              loadingMore
                ? isZh
                  ? '正在加载更多模型…'
                  : 'Loading more models…'
                : isZh
                  ? '加载更多模型'
                  : 'Load More Models'
            }}
          </button>
        </div>
      </section>

      <!-- Detail & Verification Workbench Drawer -->
      <AdminModelDetailDrawer
        :open="drawerOpen"
        :detail="activeDetail"
        :loading="detailLoading"
        :loading-more-accounts="loadingMoreAccounts"
        :loading-more-offers="loadingMoreOffers"
        :error="detailError"
        :snapshot-expired="detailSnapshotExpired"
        :initial-tab="drawerInitialTab"
        :is-zh="isZh"
        @close="drawerOpen = false"
        @load-more-accounts="handleLoadMoreAccounts"
        @load-more-offers="handleLoadMoreOffers"
        @refresh-detail="handleRefreshDetail"
      />

    </div>
  </AppLayout>
</template>
