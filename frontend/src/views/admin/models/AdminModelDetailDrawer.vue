<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import CatalogVerificationWorkbench from '@/features/operations/components/CatalogVerificationWorkbench.vue'
import {
  formatRatePairDisplay,
  formatRatePerMillion,
  type AdminCatalogDetail,
  type AdminCatalogSourceKind
} from '@/api/admin/modelCatalog'
import AdminModelProvenanceTable from './AdminModelProvenanceTable.vue'

const props = withDefaults(
  defineProps<{
    open: boolean
    detail: AdminCatalogDetail | null
    loading?: boolean
    loadingMoreAccounts?: boolean
    loadingMoreOffers?: boolean
    error?: string
    snapshotExpired?: boolean
    initialTab?: 'inspector' | 'verification'
    isZh?: boolean
  }>(),
  {
    loading: false,
    loadingMoreAccounts: false,
    loadingMoreOffers: false,
    error: '',
    snapshotExpired: false,
    initialTab: 'inspector',
    isZh: true
  }
)

const emit = defineEmits<{
  close: []
  'load-more-accounts': []
  'load-more-offers': []
  'refresh-detail': []
  'open-batch-preview': [
    operation: 'enable_discovered' | 'draft_channel_pricing' | 'toggle_publication'
  ]
}>()

const activeTab = ref<'inspector' | 'verification'>(props.initialTab)

watch(
  () => [props.open, props.initialTab] as const,
  ([isOpen, tab]) => {
    if (isOpen) {
      activeTab.value = tab
    }
  },
  { immediate: true }
)

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

function sourceLabel(kind: AdminCatalogSourceKind): string {
  const entry = SOURCE_LABELS[kind] ?? SOURCE_LABELS.unknown
  return props.isZh ? entry.zh : entry.en
}

const referenceRateDisplay = computed(() => {
  const refPrice = props.detail?.model.billing.reference_price
  if (!refPrice) return '—'
  return formatRatePairDisplay(refPrice.input_rate, refPrice.output_rate)
})

const referenceSourceDisplay = computed(() => {
  const src = props.detail?.model.billing.reference_price?.source
  if (!src || src === 'pi-ai' || src === 'pi_ai_reference') {
    return props.isZh ? '社区目录（pi）' : 'Community Catalog (pi)'
  }
  return src
})

const saleRateDisplay = computed(() => {
  const salePrice = props.detail?.model.billing.sale_price
  if (!salePrice) return '—'
  return formatRatePairDisplay(salePrice.input_rate, salePrice.output_rate)
})

const sampleSettlementQuote = computed(() => {
  const billing = props.detail?.model.billing
  if (!billing || (billing.state !== 'priced' && billing.state !== 'partial')) {
    return null
  }
  const inRate = billing.sale_price?.input_rate
  const outRate = billing.sale_price?.output_rate
  const inText = formatRatePerMillion(inRate ?? null)
  const outText = formatRatePerMillion(outRate ?? null)
  if (!inText && !outText) return null
  return `${inText ?? '—'} (1M in) + ${outText ?? '—'} (1M out)`
})

const isRoutableUnpriced = computed(() => {
  const m = props.detail?.model
  if (!m) return false
  return m.availability.state === 'live' && m.billing.state === 'unpriced'
})
</script>

<template>
  <aside
    v-if="open"
    data-testid="admin-model-detail-drawer"
    role="dialog"
    aria-modal="true"
    :aria-label="isZh ? '模型目录详情与核验抽屉' : 'Model Catalog Detail & Verification Drawer'"
    class="fixed inset-0 z-50 flex justify-end bg-black/40 backdrop-blur-xs"
    @click.self="emit('close')"
  >
    <div
      class="flex h-full w-full max-w-4xl flex-col overflow-hidden border-l border-[var(--border)] bg-[var(--background)] text-[var(--foreground)] shadow-2xl"
    >
      <!-- Drawer Header -->
      <header
        class="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--border)] bg-[var(--card)] px-5 py-4"
      >
        <div class="min-w-0 space-y-1">
          <div class="flex flex-wrap items-center gap-2">
            <h2 class="truncate text-base font-bold">
              <template v-if="detail">
                {{ detail.model.provider }} / {{ detail.model.model }}
              </template>
              <template v-else>
                {{ isZh ? '模型目录详情与目录核验工作台' : 'Model Catalog Inspector & Verification Workbench' }}
              </template>
            </h2>
            <span
              v-if="detail"
              class="rounded bg-[var(--surface-2)] px-2 py-0.5 font-mono text-xs text-[var(--muted-foreground)]"
            >
              {{ detail.model.id }}
            </span>
            <span
              v-if="isRoutableUnpriced"
              data-testid="drawer-p0-unpriced-badge"
              class="rounded border border-red-500/40 bg-red-500/15 px-2 py-0.5 text-xs font-semibold text-red-600 dark:text-red-400"
            >
              {{ isZh ? '缺销售价 P0（可路由但未定价）' : 'Missing Sale Rate P0 (Routable but Unpriced)' }}
            </span>
          </div>
          <p v-if="detail" class="text-xs text-[var(--muted-foreground)]">
            <span>{{ detail.model.display_name }}</span>
            <span v-if="detail.aliases.length > 0" class="ml-2">
              · {{ isZh ? '公开别名' : 'Aliases' }}: {{ detail.aliases.join(', ') }}
            </span>
            <span class="ml-2 font-mono">
              · {{ isZh ? '版本' : 'Version' }}: {{ detail.catalog_version }}
            </span>
          </p>
        </div>

        <div class="flex items-center gap-2">
          <button
            v-if="detail"
            type="button"
            data-testid="drawer-refresh-button"
            class="btn btn-secondary btn-sm text-xs"
            :disabled="loading"
            @click="emit('refresh-detail')"
          >
            {{ isZh ? '刷新详情快照' : 'Refresh Detail' }}
          </button>
          <button
            type="button"
            data-testid="drawer-close-button"
            class="btn btn-secondary btn-sm text-xs"
            @click="emit('close')"
          >
            {{ isZh ? '关闭' : 'Close' }}
          </button>
        </div>
      </header>

      <!-- Drawer Tabs -->
      <div
        role="tablist"
        class="flex flex-wrap items-center gap-2 border-b border-[var(--border)] bg-[var(--surface-2)] px-5 py-2.5"
      >
        <button
          type="button"
          role="tab"
          data-testid="drawer-tab-inspector"
          :aria-selected="activeTab === 'inspector'"
          class="rounded-lg px-3 py-1.5 text-xs font-semibold transition-colors"
          :class="
            activeTab === 'inspector'
              ? 'bg-[var(--brand,#f59e0b)] text-[#141008]'
              : 'bg-[var(--card)] text-[var(--muted-foreground)] hover:text-[var(--foreground)]'
          "
          @click="activeTab = 'inspector'"
        >
          {{ isZh ? '规格来源 · 四段漏斗 · 四层价格瀑布' : 'Provenance · 4-Stage Funnel · Price Waterfall' }}
        </button>
        <button
          type="button"
          role="tab"
          data-testid="drawer-tab-verification"
          :aria-selected="activeTab === 'verification'"
          class="rounded-lg px-3 py-1.5 text-xs font-semibold transition-colors"
          :class="
            activeTab === 'verification'
              ? 'bg-[var(--brand,#f59e0b)] text-[#141008]'
              : 'bg-[var(--card)] text-[var(--muted-foreground)] hover:text-[var(--foreground)]'
          "
          @click="activeTab = 'verification'"
        >
          {{ isZh ? '目录核验与发布工作台' : 'Verification & Publication Workbench' }}
        </button>
      </div>

      <!-- Drawer Body -->
      <div class="flex-1 overflow-y-auto p-5 space-y-5">
        <!-- Tab 1: Model Inspector -->
        <template v-if="activeTab === 'inspector'">
          <div
            v-if="snapshotExpired"
            role="alert"
            data-testid="drawer-snapshot-expired"
            class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-amber-500/40 bg-amber-500/10 p-4 text-xs text-amber-800 dark:text-amber-200"
          >
            <span>
              {{
                isZh
                  ? '目录快照游标已过期（410 SNAPSHOT_EXPIRED）。请刷新详情快照以继续加载连接或路由绑定。'
                  : 'Catalog snapshot cursor expired (410 SNAPSHOT_EXPIRED). Refresh detail snapshot to continue.'
              }}
            </span>
            <button
              type="button"
              data-testid="drawer-expired-refresh"
              class="btn btn-secondary btn-sm text-xs"
              @click="emit('refresh-detail')"
            >
              {{ isZh ? '立即刷新快照' : 'Refresh Snapshot' }}
            </button>
          </div>

          <div
            v-if="error && !snapshotExpired"
            role="alert"
            data-testid="drawer-error-banner"
            class="rounded-xl border border-red-500/40 bg-red-500/10 p-4 text-xs text-red-700 dark:text-red-300"
          >
            {{ error }}
          </div>

          <div
            v-if="loading && !detail"
            data-testid="drawer-loading"
            class="rounded-xl border border-[var(--border)] bg-[var(--card)] p-6 text-center text-sm text-[var(--muted-foreground)]"
          >
            {{ isZh ? '正在读取模型详情证据链…' : 'Loading model detail evidence chain…' }}
          </div>

          <template v-else-if="detail">
            <!-- Quick Batch Action Triggers for current model -->
            <div
              class="flex flex-wrap items-center justify-between gap-2 rounded-xl border border-[var(--border)] bg-[var(--card)] p-3.5 text-xs"
            >
              <span class="text-[var(--muted-foreground)]">
                {{
                  isZh
                    ? '需要变更该模型的有效绑定、销售价草稿或分组可见性？所有写操作均经过差异预览与 CAS 校验：'
                    : 'Need to update effective bindings, sale rate drafts, or group visibility? All writes require Preview + CAS:'
                }}
              </span>
              <div class="flex flex-wrap items-center gap-2">
                <button
                  type="button"
                  data-testid="drawer-action-enable-discovered"
                  class="btn btn-secondary btn-sm text-xs"
                  @click="emit('open-batch-preview', 'enable_discovered')"
                >
                  {{ isZh ? '加入账号有效模型 (Preview+CAS)' : 'Add to Account Effective (Preview+CAS)' }}
                </button>
                <button
                  type="button"
                  data-testid="drawer-action-draft-pricing"
                  class="btn btn-secondary btn-sm text-xs"
                  @click="emit('open-batch-preview', 'draft_channel_pricing')"
                >
                  {{ isZh ? '以参考价起草销售价 (Preview+CAS)' : 'Draft Sale Rate from Ref (Preview+CAS)' }}
                </button>
                <button
                  type="button"
                  data-testid="drawer-action-toggle-publication"
                  class="btn btn-secondary btn-sm text-xs"
                  @click="emit('open-batch-preview', 'toggle_publication')"
                >
                  {{ isZh ? '变更分组对外可见 (Preview+CAS)' : 'Update Group Visibility (Preview+CAS)' }}
                </button>
              </div>
            </div>

            <!-- 1. Provenance Merge Table -->
            <AdminModelProvenanceTable
              :common-facts="detail.common_facts"
              :provenance="detail.provenance"
              :billing="detail.model.billing"
              :is-zh="isZh"
            />

            <!-- 2. 4-Stage Funnel Binding Chain -->
            <section
              data-testid="admin-model-funnel-section"
              class="space-y-4 rounded-xl border border-[var(--border)] bg-[var(--card)] p-4"
            >
              <div>
                <h3 class="text-sm font-semibold">
                  {{
                    isZh
                      ? '2. 四段漏斗绑定链（1. 平台候选 -> 2. 账号原生发现 -> 3. 账号有效模型 -> 4. 分组对外可见）'
                      : '2. 4-Stage Funnel Binding Chain (1. Platform Candidates -> 2. Native Discovered -> 3. Account Effective -> 4. Group Visible)'
                  }}
                </h3>
                <p class="mt-0.5 text-xs text-[var(--muted-foreground)]">
                  {{
                    isZh
                      ? `公开标识：${detail.model.model} · 规范 ID：${detail.model.canonical_id}`
                      : `Public identity: ${detail.model.model} · Canonical ID: ${detail.model.canonical_id}`
                  }}
                </p>
              </div>

              <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
                <!-- Stage 1: Platform Candidates -->
                <div
                  data-testid="funnel-stage-1-candidates"
                  class="rounded-lg border border-[var(--border)] bg-[var(--surface-2)]/60 p-3 space-y-2 text-xs"
                >
                  <div class="flex items-center justify-between font-semibold">
                    <span>{{ isZh ? '1. 平台候选' : '1. Platform Candidates' }}</span>
                    <span class="font-mono">
                      {{
                        detail.model.source.candidate === true
                          ? isZh
                            ? '已收录候选'
                            : 'Candidate'
                          : detail.model.source.candidate === false
                            ? isZh
                              ? '非平台候选'
                              : 'No'
                            : '—'
                      }}
                    </span>
                  </div>
                  <div class="flex flex-wrap gap-1.5">
                    <span
                      v-for="kind in detail.model.source.kinds"
                      :key="kind"
                      class="rounded border border-[var(--border)] bg-[var(--card)] px-2 py-0.5 text-[11px]"
                    >
                      {{ sourceLabel(kind) }}
                    </span>
                    <span v-if="detail.model.source.kinds.length === 0" class="text-[var(--muted-foreground)]">
                      —
                    </span>
                  </div>
                </div>

                <!-- Stage 2: Native Discovered -->
                <div
                  data-testid="funnel-stage-2-discovered"
                  class="rounded-lg border border-[var(--border)] bg-[var(--surface-2)]/60 p-3 space-y-2 text-xs"
                >
                  <div class="flex items-center justify-between font-semibold">
                    <span>{{ isZh ? '2. 账号原生发现' : '2. Native Discovered' }}</span>
                    <span class="font-mono">
                      {{ detail.model.discovered_accounts_count ?? detail.accounts.length }}
                    </span>
                  </div>
                  <ul v-if="detail.accounts.length > 0" class="space-y-1.5">
                    <li
                      v-for="acc in detail.accounts"
                      :key="`${acc.account_id}:${acc.snapshot_revision}`"
                      data-testid="funnel-account-row"
                      class="flex flex-wrap items-center justify-between gap-1 rounded border border-[var(--border)] bg-[var(--card)] px-2.5 py-1.5"
                    >
                      <span class="font-medium">
                        #{{ acc.account_id }} · {{ acc.label }}
                        <span class="text-[var(--muted-foreground)]">({{ acc.account_type }})</span>
                      </span>
                      <div class="flex items-center gap-1.5 font-mono text-[11px]">
                        <span
                          :class="
                            acc.discovery_state === 'current'
                              ? 'text-emerald-600 dark:text-emerald-400'
                              : 'text-amber-600 dark:text-amber-400'
                          "
                        >
                          {{ acc.discovery_state }}
                        </span>
                        <span class="text-[var(--muted-foreground)]">
                          rev:{{ acc.rules_revision ?? '—' }}
                        </span>
                      </div>
                    </li>
                  </ul>
                  <p v-else class="text-[var(--muted-foreground)]">
                    {{ isZh ? '暂无上游账号原生发现记录' : 'No native discovery observations' }}
                  </p>
                  <button
                    v-if="detail.next_accounts_cursor"
                    type="button"
                    data-testid="drawer-load-more-accounts"
                    class="btn btn-secondary btn-sm w-full text-xs"
                    :disabled="loadingMoreAccounts"
                    @click="emit('load-more-accounts')"
                  >
                    {{
                      loadingMoreAccounts
                        ? isZh
                          ? '正在加载发现账号…'
                          : 'Loading accounts…'
                        : isZh
                          ? '加载更多发现账号'
                          : 'Load More Discovered Accounts'
                    }}
                  </button>
                </div>

                <!-- Stage 3: Account Effective -->
                <div
                  data-testid="funnel-stage-3-effective"
                  class="rounded-lg border border-[var(--border)] bg-[var(--surface-2)]/60 p-3 space-y-2 text-xs"
                >
                  <div class="flex items-center justify-between font-semibold">
                    <span>{{ isZh ? '3. 账号有效模型' : '3. Account Effective' }}</span>
                    <span class="font-mono">
                      {{ detail.model.accounts_count ?? detail.offers.filter((o) => o.eligible).length }}
                    </span>
                  </div>
                  <ul v-if="detail.offers.length > 0" class="space-y-1.5">
                    <li
                      v-for="(offer, idx) in detail.offers"
                      :key="`${offer.group_id}:${offer.channel_id}:${offer.account_id ?? 'null'}:${offer.action}:${offer.protocol}:${idx}`"
                      data-testid="funnel-offer-row"
                      class="rounded border border-[var(--border)] bg-[var(--card)] px-2.5 py-1.5 space-y-1"
                    >
                      <div class="flex flex-wrap items-center justify-between gap-1">
                        <span class="font-medium">
                          <template v-if="offer.account_id">
                            {{ isZh ? `连接 #${offer.account_id}` : `Connection #${offer.account_id}` }}
                          </template>
                          <template v-else>
                            {{ isZh ? '无具体连接绑定 (group/channel 级)' : 'No concrete account binding' }}
                          </template>
                          →
                          {{ isZh ? `分组 #${offer.group_id}` : `Group #${offer.group_id}` }}
                          /
                          <template v-if="offer.channel_id === '0'">
                            {{ isZh ? '仅分组证据 (channel #0)' : 'Group-only evidence (channel #0)' }}
                          </template>
                          <template v-else>
                            {{ isZh ? `渠道 #${offer.channel_id}` : `Channel #${offer.channel_id}` }}
                          </template>
                        </span>
                        <span
                          class="rounded px-1.5 py-0.5 font-mono text-[10px] font-semibold"
                          :class="
                            offer.eligible
                              ? 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-300'
                              : 'bg-amber-500/15 text-amber-700 dark:text-amber-300'
                          "
                        >
                          {{ offer.eligible ? (isZh ? '已生效 (eligible)' : 'eligible') : offer.reason || 'ineligible' }}
                        </span>
                      </div>
                      <div class="flex flex-wrap items-center justify-between text-[11px] text-[var(--muted-foreground)] font-mono">
                        <span>{{ offer.action }} · {{ offer.protocol }}</span>
                        <span>
                          {{
                            offer.billing.sale_price
                              ? formatRatePairDisplay(
                                  offer.billing.sale_price.input_rate,
                                  offer.billing.sale_price.output_rate
                                )
                              : offer.billing.state
                          }}
                        </span>
                      </div>
                    </li>
                  </ul>
                  <p v-else class="text-[var(--muted-foreground)]">
                    {{ isZh ? '当前作用域暂无有效模型路由绑定' : 'No eligible account bindings in scope' }}
                  </p>
                  <button
                    v-if="detail.next_offers_cursor"
                    type="button"
                    data-testid="drawer-load-more-offers"
                    class="btn btn-secondary btn-sm w-full text-xs"
                    :disabled="loadingMoreOffers"
                    @click="emit('load-more-offers')"
                  >
                    {{
                      loadingMoreOffers
                        ? isZh
                          ? '正在加载路由绑定…'
                          : 'Loading offers…'
                        : isZh
                          ? '加载更多路由绑定'
                          : 'Load More Route Offers'
                    }}
                  </button>
                </div>

                <!-- Stage 4: Group Visible -->
                <div
                  data-testid="funnel-stage-4-visible"
                  class="rounded-lg border border-[var(--border)] bg-[var(--surface-2)]/60 p-3 space-y-2 text-xs"
                >
                  <div class="flex items-center justify-between font-semibold">
                    <span>{{ isZh ? '4. 分组对外可见' : '4. Group Visible' }}</span>
                    <span class="font-mono">
                      {{ detail.model.groups_count ?? detail.publications.length }}
                    </span>
                  </div>
                  <ul v-if="detail.publications.length > 0" class="space-y-1.5">
                    <li
                      v-for="pub in detail.publications"
                      :key="`${pub.group_id}:${pub.channel_id}`"
                      data-testid="funnel-publication-row"
                      class="flex flex-wrap items-center justify-between gap-1 rounded border border-[var(--border)] bg-[var(--card)] px-2.5 py-1.5"
                    >
                      <span>
                        {{ isZh ? `分组 #${pub.group_id}` : `Group #${pub.group_id}` }}
                        ·
                        <template v-if="pub.channel_id === '0'">
                          {{ isZh ? '仅分组证据 (channel #0)' : 'Group-only (channel #0)' }}
                        </template>
                        <template v-else>
                          {{ isZh ? `渠道 #${pub.channel_id}` : `Channel #${pub.channel_id}` }}
                        </template>
                      </span>
                      <span class="font-mono text-[11px]">
                        {{ pub.state }}
                        <span v-if="pub.basis" class="text-[var(--muted-foreground)]">
                          ({{ pub.basis }})
                        </span>
                      </span>
                    </li>
                  </ul>
                  <p v-else class="text-[var(--muted-foreground)]">
                    {{ isZh ? '尚无分组对外可见发布记录' : 'No group visibility publication records' }}
                  </p>
                </div>
              </div>
            </section>

            <!-- 3. 4-Layer Price Waterfall -->
            <section
              data-testid="admin-model-price-waterfall"
              class="space-y-3 rounded-xl border border-[var(--border)] bg-[var(--card)] p-4"
            >
              <div>
                <h3 class="text-sm font-semibold">
                  {{
                    isZh
                      ? '3. 四层价格瀑布链（1. 参考价（非结算价） -> 2. 销售价 -> 3. 内部成本 -> 4. 实收结算价）'
                      : '3. 4-Layer Price Waterfall (1. Reference Price -> 2. Sale Rate -> 3. Internal Cost -> 4. Settlement Amount)'
                  }}
                </h3>
                <p class="mt-0.5 text-xs text-[var(--muted-foreground)]">
                  {{
                    isZh
                      ? '参考价（社区目录 pi）严格置于次级说明且绝不参与网关结算；销售价冲突或缺失将在此显式告警。'
                      : 'Reference Price (Community Catalog pi) is strictly secondary and non-settlement; Sale Rate conflicts or missing tariffs alert explicitly.'
                  }}
                </p>
              </div>

              <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4 text-xs">
                <!-- Layer 1: Reference Price (Non-settlement) -->
                <div
                  data-testid="price-layer-1-reference"
                  class="rounded-lg border border-dashed border-[var(--border)] bg-[var(--surface-2)]/40 p-3 space-y-1"
                >
                  <div class="font-semibold text-[var(--muted-foreground)]">
                    {{ isZh ? '1. 参考价（非结算价）' : '1. Reference Price (Non-settlement)' }}
                  </div>
                  <div class="font-mono text-sm text-[var(--muted-foreground)]">
                    {{ referenceRateDisplay }}
                    <span v-if="referenceRateDisplay !== '—'" class="text-[10px]">/ 1M tokens</span>
                  </div>
                  <p class="text-[11px] text-[var(--muted-foreground)]">
                    {{
                      isZh
                        ? `来源：${referenceSourceDisplay} · 非网关结算价`
                        : `Source: ${referenceSourceDisplay} · Non-settlement`
                    }}
                  </p>
                </div>

                <!-- Layer 2: Sale Rate -->
                <div
                  data-testid="price-layer-2-sale"
                  class="rounded-lg border p-3 space-y-1"
                  :class="
                    isRoutableUnpriced
                      ? 'border-red-500/50 bg-red-500/10'
                      : detail.model.billing.state === 'conflict'
                        ? 'border-amber-500/50 bg-amber-500/10'
                        : 'border-[var(--border)] bg-[var(--surface-2)]/60'
                  "
                >
                  <div class="flex items-center justify-between font-semibold">
                    <span>{{ isZh ? '2. 销售价' : '2. Sale Rate' }}</span>
                    <span class="font-mono text-[10px] uppercase">{{ detail.model.billing.state }}</span>
                  </div>
                  <div class="font-mono text-sm font-bold">
                    {{ saleRateDisplay }}
                    <span v-if="saleRateDisplay !== '—'" class="text-[10px] font-normal">/ 1M tokens</span>
                  </div>
                  <p class="text-[11px] text-[var(--muted-foreground)]">
                    <template v-if="isRoutableUnpriced">
                      {{ isZh ? '告警：模型可路由但未配置销售价 (P0)' : 'Alert: Routable model has no Sale Rate (P0)' }}
                    </template>
                    <template v-else-if="detail.model.billing.state === 'conflict'">
                      {{ isZh ? '告警：关联分组/渠道销售价存在冲突' : 'Alert: Conflicting group/channel Sale Rates' }}
                    </template>
                    <template v-else>
                      {{ isZh ? '渠道与路由分组基准销售费率' : 'Channel & routing group Sale Rate' }}
                    </template>
                  </p>
                </div>

                <!-- Layer 3: Internal Cost -->
                <div
                  data-testid="price-layer-3-internal-cost"
                  class="rounded-lg border border-[var(--border)] bg-[var(--surface-2)]/60 p-3 space-y-1"
                >
                  <div class="font-semibold">
                    {{ isZh ? '3. 内部成本' : '3. Internal Cost' }}
                  </div>
                  <div class="font-mono text-sm">
                    {{ isZh ? '按上游连接核算' : 'Per Upstream Connection' }}
                  </div>
                  <p class="text-[11px] text-[var(--muted-foreground)]">
                    {{
                      isZh
                        ? '上游连接凭据账本内部成本，与对外销售价严格隔离'
                        : 'Upstream credential cost accounting, isolated from public Sale Rate'
                    }}
                  </p>
                </div>

                <!-- Layer 4: Settlement Amount -->
                <div
                  data-testid="price-layer-4-settlement"
                  class="rounded-lg border border-[var(--border)] bg-[var(--surface-2)]/60 p-3 space-y-1"
                >
                  <div class="font-semibold">
                    {{ isZh ? '4. 实收结算价' : '4. Settlement Amount' }}
                  </div>
                  <div class="font-mono text-sm font-semibold">
                    {{ sampleSettlementQuote ?? '—' }}
                  </div>
                  <p class="text-[11px] text-[var(--muted-foreground)]">
                    {{
                      sampleSettlementQuote
                        ? isZh
                          ? '基于销售价 × 分组默认倍率（1M 输入 + 1M 输出样本）'
                          : 'Based on Sale Rate × group default multiplier (1M in + 1M out)'
                        : isZh
                          ? '未配置有效销售价，禁止使用参考价代替实收结算价'
                          : 'No active Sale Rate; Reference Price is never used for settlement'
                    }}
                  </p>
                </div>
              </div>
            </section>
          </template>
        </template>

        <!-- Tab 2: Relocated CatalogVerificationWorkbench -->
        <div
          v-show="activeTab === 'verification'"
          data-testid="drawer-verification-workbench-panel"
        >
          <CatalogVerificationWorkbench :is-zh="isZh" />
        </div>
      </div>
    </div>
  </aside>
</template>
