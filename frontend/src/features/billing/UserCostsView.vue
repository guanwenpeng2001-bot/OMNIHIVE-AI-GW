<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { Badge, Button, Card, EmptyState, Input, PageHeader, Select, StatNumber, Table, Tabs, type UiTableColumn } from '@/components/ui'
import { opaqueNumericId, queryParams, requestLocation, requestsApi, scopeQuery, type Schema } from '@/features/requests/api'
import { queryText, shiftCalendar, windowQuery } from '@/features/requests/filters'
import { displayNumber, moneyText, statusTone, timestamp } from '@/features/requests/presentation'
import { useCursor } from '@/features/requests/useCursor'
import { useProjectScope } from '@/features/requests/useProjectScope'
import { useScopedRead } from '@/features/requests/useScopedRead'
import CursorButtons from '@/features/requests/components/CursorButtons.vue'
import DateRangeFields from '@/features/requests/components/DateRangeFields.vue'
import ReadNotice from '@/features/requests/components/ReadNotice.vue'
import ScopePicker from '@/features/requests/components/ScopePicker.vue'
import { billingApi } from './api'
import { costFilters, costWindow, fundingSources, settlementStates } from './presentation'
import { useCsvExport } from './useCsvExport'
import BillingNav from './components/BillingNav.vue'
import CsvExportNotice from './components/CsvExportNotice.vue'
import QuotaWaterfall from './components/QuotaWaterfall.vue'
import SpendTrend from './components/SpendTrend.vue'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const { scope, scopeKey, choose, access, can } = useProjectScope()
const applied = computed(() => costFilters(route.query))
const draft = reactive({ ...applied.value })
watch(applied, v => Object.assign(draft, v))
const window = computed(() => costWindow(applied.value))
const filterIssue = computed(() => {
  if (!costWindow(draft)) return t('wp6.filters.invalidCostWindow')
  if (draft.key_id && !opaqueNumericId(draft.key_id)) return t('wp6.filters.invalidKey')
  return ''
})
const scopeFilter = computed(() => window.value && can('billing.read') ? JSON.stringify([scopeKey.value, applied.value]) : '')
const specificRequest = computed(() => queryText(route.query, 'request_id').slice(0, 255))
const view = computed(() => queryText(route.query, 'view') === 'ledger' ? 'ledger' : 'charges')
const groupsPager = useCursor(() => scopeFilter.value)
const dailyPager = useCursor(() => scopeFilter.value)
const ledgerKey = computed(() => JSON.stringify([scopeFilter.value, view.value, specificRequest.value]))
const ledgerPager = useCursor(() => ledgerKey.value)

function params(values: Record<string, string | number | null | undefined> = {}) {
  return queryParams({
    ...window.value, key_id: applied.value.key_id, model_id: applied.value.model_id,
    funding_source: applied.value.funding_source, settlement_state: applied.value.settlement_state, ...values,
  })
}
const trend = useScopedRead(
  () => scopeFilter.value ? JSON.stringify([scopeFilter.value, dailyPager.cursor.value]) : '',
  signal => billingApi.activity(scope.value!, params({ group_by: 'day', limit: 100, cursor: dailyPager.cursor.value }), signal),
)
const groups = useScopedRead(
  () => scopeFilter.value ? JSON.stringify([scopeFilter.value, groupsPager.cursor.value]) : '',
  signal => billingApi.activity(scope.value!, params({ group_by: applied.value.group_by, limit: 8, cursor: groupsPager.cursor.value }), signal),
)
const quotaKey = computed(() => can('keys.read') ? applied.value.key_id : '')
const quota = useScopedRead(
  () => can('billing.read') ? JSON.stringify([scopeKey.value, quotaKey.value, applied.value.model_id]) : '',
  signal => billingApi.quotas(scope.value!, quotaKey.value, applied.value.model_id, signal),
)
const records = useScopedRead<Schema['CostChargePage'] | Schema['CostLedgerPage']>(
  () => scopeFilter.value && (view.value !== 'ledger' || can('billing.ledger.read')) ? JSON.stringify([ledgerKey.value, ledgerPager.cursor.value]) : '',
  signal => view.value === 'ledger'
    ? billingApi.ledger(scope.value!, params({ request_id: specificRequest.value, cursor: ledgerPager.cursor.value }), signal)
    : billingApi.charges(scope.value!, params({ cursor: ledgerPager.cursor.value }), signal),
)
const keys = useScopedRead(() => can('keys.read') ? scopeKey.value : '', signal => requestsApi.keys(scope.value!, signal))
const totals = computed(() => trend.data.value?.totals ?? groups.data.value?.totals)
const csv = useCsvExport(
  () => scopeFilter.value && can('billing.export') && !trend.offline.value ? JSON.stringify([scopeKey.value, window.value]) : '',
  () => scope.value, () => window.value,
)
const groupValues = computed(() => [
  { value: 'model', label: t('wp6.filters.model') }, { value: 'key', label: t('wp6.filters.key') },
])
const sources = computed(() => [{ value: '', label: t('wp6.filters.allSources') }, ...fundingSources.map(value => ({
  value, label: t(`wp6.funding.${value}`),
}))])
const settlements = computed(() => [{ value: '', label: t('wp6.filters.allSettlements') }, ...settlementStates.map(value => ({
  value, label: t(`wp6.states.${value}`),
}))])
const tabs = computed(() => [
  { value: 'charges', label: t('wp6.billing.charges') },
  ...(can('billing.ledger.read') || view.value === 'ledger' ? [{ value: 'ledger', label: t('wp6.billing.ledger') }] : []),
])
const chargeColumns = computed<UiTableColumn[]>(() => view.value === 'charges' ? [
  { key: 'request', label: t('wp6.requests.request') }, { key: 'date', label: t('wp6.requests.started') },
  { key: 'state', label: t('wp6.requests.settlement') }, { key: 'money', label: t('wp6.requests.charge'), mono: true },
  { key: 'price', label: t('wp6.requests.priceVersion') },
] : [
  { key: 'request', label: t('wp6.requests.request') }, { key: 'date', label: t('wp6.billing.postedAt') },
  { key: 'state', label: t('wp6.billing.entry') }, { key: 'money', label: t('wp6.billing.signedAmount'), mono: true },
  { key: 'price', label: t('wp6.billing.originalEntry') },
])
type Charge = Schema['CostCharge']
type Entry = Schema['CostEntry']
const asCharge = (row: Record<string, unknown>) => row as Charge
const asEntry = (row: Record<string, unknown>) => row as Entry
const recordId = (row: Record<string, unknown>) => view.value === 'charges' ? asCharge(row).request_id : asEntry(row).id

function apply() {
  if (!scope.value || filterIssue.value) return
  void router.replace({ query: {
    ...route.query, ...scopeQuery(scope.value), ...windowQuery(draft), currency: draft.currency.trim().toUpperCase(),
    key_id: draft.key_id || undefined, model_id: draft.model_id.trim() || undefined, group_by: draft.group_by,
    funding_source: draft.funding_source || undefined, settlement_state: draft.settlement_state || undefined,
  } })
}
function pickView(value: string) {
  void router.replace({ query: { ...route.query, view: value === 'ledger' ? value : undefined, request_id: undefined } })
}
function pickGroup(value: string | number) {
  void router.replace({ query: { ...route.query, group_by: String(value) } })
}
function inspectGroup(row: Schema['CostGroup']) {
  const model = row.filter.model_id
  const key = row.filter.key_id
  if (!model && !key) return
  void router.replace({ query: { ...route.query, model_id: model ?? (applied.value.model_id || undefined), key_id: key ?? (applied.value.key_id || undefined) } })
}
function inspectDay(day: string) {
  void router.replace({ query: { ...route.query, from: day, to: shiftCalendar(day, 1), timezone: applied.value.timezone } })
}
function resetOrRetry(resource: typeof trend | typeof groups | typeof records, pager: ReturnType<typeof useCursor>) {
  if (resource.error.value?.status === 410 && pager.cursor.value) pager.reset()
  else void resource.refresh()
}
async function refresh() {
  await access.refresh()
  if (can('billing.read')) await Promise.all([trend.refresh(), groups.refresh(), quota.refresh(), records.refresh()])
}
</script>

<template>
  <AppLayout contained>
    <div class="min-w-0 space-y-5">
      <PageHeader :title="t('wp6.billing.title')" :description="t('wp6.billing.description')">
        <template #actions><ScopePicker :scope="scope" @change="choose" /></template>
      </PageHeader>
      <BillingNav />
      <EmptyState v-if="!scope" :title="t('wp6.scope.select')" :description="t('wp6.scope.required')" />
      <template v-else>
        <ReadNotice :loading="access.loading.value" :error="access.error.value" :has-data="Boolean(access.data.value)" :offline="access.offline.value" :last-synced="access.lastSynced.value" @retry="access.refresh" />
        <p v-if="access.data.value && !can('billing.read') && !access.loading.value" role="status" class="rounded-oh-lg border border-oh-default bg-oh-surface p-4 text-sm text-oh-secondary">
          {{ t('wp6.state.requiredGrant', { grant: 'billing.read' }) }}
        </p>
        <template v-if="can('billing.read')">
          <Card padding="md">
            <form class="grid grid-cols-1 items-end gap-3 sm:grid-cols-2 md:grid-cols-4 2xl:grid-cols-8" @submit.prevent="apply">
              <DateRangeFields v-model:from="draft.from" v-model:to="draft.to" v-model:timezone="draft.timezone" required />
              <Input v-model="draft.currency" :label="t('wp6.billing.currency')" placeholder="USD" :maxlength="3" mono />
              <Input v-model="draft.model_id" :label="t('wp6.filters.model')" :placeholder="t('wp6.filters.anyModel')" :maxlength="255" mono />
              <label class="grid min-w-0 gap-1.5 text-xs font-medium text-oh-secondary">
                {{ t('wp6.filters.key') }}
                <input v-model.trim="draft.key_id" list="wp6-cost-keys" inputmode="numeric" maxlength="19" :placeholder="t('wp6.filters.anyKey')" class="oh-focus-ring h-9 w-full min-w-0 rounded-oh-md border border-oh-default bg-oh-surface px-2.5 font-mono text-base text-oh-primary sm:text-sm" />
                <datalist id="wp6-cost-keys"><option v-for="key in keys.data.value?.items ?? []" :key="key.id" :value="key.id">{{ key.name }}</option></datalist>
              </label>
              <Select v-model="draft.funding_source" :label="t('wp6.billing.fundingSource')" :options="sources" />
              <Select v-model="draft.settlement_state" :label="t('wp6.requests.settlement')" :options="settlements" />
              <div class="col-span-full flex flex-wrap items-center gap-2 border-t border-oh-subtle pt-3">
                <Button type="submit" variant="primary" :disabled="Boolean(filterIssue)">{{ t('wp6.filters.apply') }}</Button>
                <Button type="button" variant="ghost" :disabled="trend.loading.value" @click="refresh">{{ t('wp6.state.refresh') }}</Button>
                <Button
                  v-if="can('billing.export') && csv.state.value === 'idle'"
                  type="button" variant="secondary" :disabled="!window || trend.offline.value" @click="csv.create()"
                >
                  {{ t('wp6.export.create') }}
                </Button>
                <p v-if="filterIssue" role="status" class="text-sm text-oh-status-danger-fg">{{ filterIssue }}</p>
              </div>
            </form>
          </Card>
          <CsvExportNotice :state="csv.state.value" :operation-id="csv.operationID.value" :error="csv.error.value" @check="csv.check" @retry="csv.create()" @renew="csv.create(true)" @download="csv.download" />

          <div v-if="totals" class="grid min-w-0 grid-cols-2 gap-3 xl:grid-cols-4">
            <Card padding="sm"><StatNumber class="max-w-full [&_.oh-tabular]:min-w-0 [&_.oh-tabular]:break-all max-sm:[&_.oh-tabular]:text-sm" :label="t('wp6.billing.spend')" :value="totals.platform_charge ? moneyText(totals.platform_charge, locale) : null" :unknown-label="t('wp6.requests.unobserved')" accent /></Card>
            <Card padding="sm"><StatNumber class="max-w-full [&_.oh-tabular]:min-w-0 [&_.oh-tabular]:break-all max-sm:[&_.oh-tabular]:text-sm" :label="t('wp6.requests.logicalRequests')" :value="displayNumber(totals.logical_requests, locale)" /></Card>
            <Card padding="sm"><StatNumber class="max-w-full [&_.oh-tabular]:min-w-0 [&_.oh-tabular]:break-all max-sm:[&_.oh-tabular]:text-sm" :label="t('wp6.requests.attempts')" :value="totals.attempts === null ? null : displayNumber(totals.attempts, locale)" :unknown-label="t('wp6.requests.unobserved')" /></Card>
            <Card padding="sm"><StatNumber class="max-w-full [&_.oh-tabular]:min-w-0 [&_.oh-tabular]:break-all max-sm:[&_.oh-tabular]:text-sm" :label="t('wp6.requests.reserved')" :value="totals.reserved ? moneyText(totals.reserved, locale) : null" :unknown-label="t('wp6.requests.unobserved')" /></Card>
          </div>

          <div class="grid min-w-0 gap-4 xl:grid-cols-[minmax(0,1.25fr)_minmax(0,1fr)]">
            <Card padding="md" class="min-w-0">
              <div class="space-y-3">
                <h2 class="text-base font-semibold text-oh-primary">{{ t('wp6.billing.trend') }}</h2>
                <ReadNotice :loading="trend.loading.value" :error="trend.error.value" :has-data="Boolean(trend.data.value)" :offline="trend.offline.value" :last-synced="trend.lastSynced.value" grant="billing.read" @retry="resetOrRetry(trend, dailyPager)" />
                <SpendTrend v-if="trend.data.value" :activity="trend.data.value" @day="inspectDay" />
                <p v-if="trend.data.value?.data_completeness.state === 'partial'" class="text-xs text-oh-status-warn-fg">{{ t('wp6.billing.partial') }}</p>
                <CursorButtons :next="Boolean(trend.data.value?.next_cursor)" :previous="dailyPager.hasPrevious.value" :loading="trend.loading.value" @next="dailyPager.next(trend.data.value?.next_cursor)" @previous="dailyPager.previous" />
              </div>
            </Card>
            <Card padding="md" class="min-w-0">
              <div class="space-y-3">
                <h2 class="text-base font-semibold text-oh-primary">{{ t('wp6.billing.quota') }}</h2>
                <ReadNotice :loading="quota.loading.value" :error="quota.error.value" :has-data="Boolean(quota.data.value)" :offline="quota.offline.value" :last-synced="quota.lastSynced.value" grant="billing.read" @retry="quota.refresh" />
                <QuotaWaterfall v-if="quota.data.value" :quota="quota.data.value" />
              </div>
            </Card>
          </div>

          <Card padding="md">
            <div class="space-y-3">
              <div class="flex flex-wrap items-center justify-between gap-3">
                <h2 class="text-base font-semibold text-oh-primary">{{ t('wp6.billing.groupBreakdown') }}</h2>
                <Select :model-value="applied.group_by" :label="t('wp6.billing.groupBy')" :options="groupValues" @update:model-value="pickGroup" />
              </div>
              <ReadNotice :loading="groups.loading.value" :error="groups.error.value" :has-data="Boolean(groups.data.value)" :offline="groups.offline.value" :last-synced="groups.lastSynced.value" grant="billing.read" @retry="resetOrRetry(groups, groupsPager)" />
              <EmptyState v-if="groups.data.value && !groups.data.value.items.length" :title="t('wp6.state.noMatches')" :description="t('wp6.billing.noTrend')" compact />
              <ul v-else-if="groups.data.value" class="divide-y divide-oh-subtle">
                <li v-for="row in groups.data.value.items" :key="row.group ?? 'unknown'" class="flex min-w-0 flex-wrap items-center justify-between gap-2 py-2.5 text-sm">
                  <button v-if="row.group" type="button" class="oh-focus-ring min-w-0 max-w-full break-all rounded-oh-xs text-left font-mono text-xs font-medium text-oh-accent-text hover:underline" @click="inspectGroup(row)">
                    {{ applied.group_by === 'key' ? keys.data.value?.items.find(key => key.id === row.group)?.name || row.group : row.group }}
                  </button>
                  <span v-else class="text-oh-muted">{{ t('wp6.states.unknown') }}</span>
                  <div class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 font-mono text-xs tabular-nums">
                    <span class="text-oh-muted">{{ t('wp6.billing.groupRequests', { count: displayNumber(row.logical_requests, locale) }) }}</span>
                    <strong class="break-all font-medium text-oh-primary">{{ moneyText(row.platform_charge, locale) }}</strong>
                  </div>
                </li>
              </ul>
              <CursorButtons :next="Boolean(groups.data.value?.next_cursor)" :previous="groupsPager.hasPrevious.value" :loading="groups.loading.value" @next="groupsPager.next(groups.data.value?.next_cursor)" @previous="groupsPager.previous" />
            </div>
          </Card>

          <section class="min-w-0 space-y-3">
            <Tabs :model-value="view" :items="tabs" :aria-label="t('wp6.billing.records')" @update:model-value="pickView" />
            <p v-if="view === 'ledger' && !can('billing.ledger.read')" role="status" class="rounded-oh-lg border border-oh-default bg-oh-surface p-4 text-sm text-oh-secondary">{{ t('wp6.state.requiredGrant', { grant: 'billing.ledger.read' }) }}</p>
            <ReadNotice :loading="records.loading.value" :error="records.error.value" :has-data="Boolean(records.data.value)" :offline="records.offline.value" :last-synced="records.lastSynced.value" :grant="view === 'ledger' ? 'billing.ledger.read' : 'billing.read'" @retry="resetOrRetry(records, ledgerPager)" />
            <Table v-if="records.data.value" :columns="chargeColumns" :rows="records.data.value.items" :row-key="recordId" :empty-title="t('wp6.billing.noRecords')">
              <template #cell-request="{ row }">
                <RouterLink :to="requestLocation(scope, String(row.request_id), false)" class="oh-focus-ring block min-w-40 rounded-oh-xs break-all font-mono text-xs font-medium text-oh-accent-text hover:underline">{{ view === 'charges' ? asCharge(row).model_id || asCharge(row).request_id : asEntry(row).request_id }}</RouterLink>
              </template>
              <template #cell-date="{ row }"><span class="whitespace-nowrap font-mono text-xs tabular-nums">{{ timestamp(view === 'charges' ? asCharge(row).at : asEntry(row).posted_at, locale, applied.timezone) }}</span></template>
              <template #cell-state="{ row }">
                <Badge v-if="view === 'charges'" :tone="statusTone(asCharge(row).state)" dot>{{ t(`wp6.states.${asCharge(row).state}`) }}</Badge>
                <span v-else>{{ t(`wp6.entry.${asEntry(row).kind}`) }} <code>{{ asEntry(row).id }}</code></span>
              </template>
              <template #cell-money="{ row }">
                <span class="whitespace-nowrap">{{ moneyText(view === 'charges' ? asCharge(row).platform_charge : asEntry(row).signed_amount, locale) }}</span>
                <small v-if="view === 'charges' && !asCharge(row).platform_charge && asCharge(row).estimate" class="block whitespace-nowrap text-oh-muted">{{ t('wp6.billing.estimateValue', { amount: moneyText(asCharge(row).estimate, locale) }) }}</small>
              </template>
              <template #cell-price="{ row }"><code class="text-xs">{{ (view === 'charges' ? asCharge(row).price_version : asEntry(row).reverses_id) ?? '—' }}</code></template>
            </Table>
            <CursorButtons :next="Boolean(records.data.value?.next_cursor)" :previous="ledgerPager.hasPrevious.value" :loading="records.loading.value" @next="ledgerPager.next(records.data.value?.next_cursor)" @previous="ledgerPager.previous" />
          </section>
        </template>
      </template>
    </div>
  </AppLayout>
</template>
