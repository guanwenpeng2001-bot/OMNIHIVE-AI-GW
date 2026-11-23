<script setup lang="ts">
import { computed, onScopeDispose, reactive, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { Badge, Button, Card, EmptyState, Input, Modal, PageHeader, SegmentedControl, Select, StatNumber, Table, Tabs, type UiTableColumn } from '@/components/ui'
import { normalizeApiError, type NormalizedApiError } from '@/utils/apiError'
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
import { billingApi, newCommandKey } from './api'
import { costFilters, costWindow, fundingSources, settlementStates, validBudgetCeiling } from './presentation'
import { useCsvExport } from './useCsvExport'
import BillingNav from './components/BillingNav.vue'
import CsvExportNotice from './components/CsvExportNotice.vue'
import QuotaWaterfall from './components/QuotaWaterfall.vue'
import SpendTrend from './components/SpendTrend.vue'

const { t, locale } = useI18n()
const isZh = computed(() => String(locale?.value || '').startsWith('zh'))
const route = useRoute()
const router = useRouter()
const { scope, scopeKey, choose, access, can } = useProjectScope()
const activityScopeMode = ref<'project' | 'workspace'>('project')
const applied = computed(() => costFilters(route.query))
const draft = reactive({ ...applied.value })
watch(applied, v => Object.assign(draft, v))
const window = computed(() => costWindow(applied.value))
const filterIssue = computed(() => {
  if (!costWindow(draft)) return t('costLedger.filters.invalidCostWindow')
  if (draft.key_id && !opaqueNumericId(draft.key_id)) return t('costLedger.filters.invalidKey')
  return ''
})
const scopeFilter = computed(() => window.value && can('billing.read') ? JSON.stringify([scopeKey.value, applied.value]) : '')
const specificRequest = computed(() => queryText(route.query, 'request_id').slice(0, 255))
const view = computed(() => queryText(route.query, 'view') === 'ledger' ? 'ledger' : 'charges')
const groupsPager = useCursor(() => scopeFilter.value)
const dailyPager = useCursor(() => scopeFilter.value)
const workspaceKey = computed(() =>
  activityScopeMode.value === 'workspace' && scope.value && window.value && can('billing.read')
    ? JSON.stringify([scope.value.workspaceId, applied.value])
    : '',
)
const workspacePager = useCursor(() => workspaceKey.value)
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
const workspaceActivity = useScopedRead(
  () => workspaceKey.value ? JSON.stringify([workspaceKey.value, workspacePager.cursor.value]) : '',
  signal => billingApi.workspaceActivity(scope.value!.workspaceId, params({ group_by: 'project', limit: 25, cursor: workspacePager.cursor.value }), signal),
)
const quotaKey = computed(() => can('keys.read') ? applied.value.key_id : '')
const quota = useScopedRead(
  () => can('billing.read') ? JSON.stringify([scopeKey.value, quotaKey.value, applied.value.model_id]) : '',
  signal => billingApi.quotas(scope.value!, quotaKey.value, applied.value.model_id, signal),
)

const projectBudgetRule = computed(() => {
  const cur = applied.value.currency
  return (
    quota.data.value?.rules.find(
      rule =>
        rule.id === `project-${cur}` ||
        (rule.source === 'budget' && rule.scope.type === 'project' && rule.unit === cur),
    ) ?? null
  )
})
const canWriteBudget = computed(() => can('billing.budgets.write') || Boolean(projectBudgetRule.value?.can_adjust))

type BudgetMutationState = 'idle' | 'working' | 'failed' | 'unknown' | 'conflict'
const budgetModalOpen = ref(false)
const budgetState = ref<BudgetMutationState>('idle')
const budgetError = ref<NormalizedApiError | null>(null)
const budgetSavedRule = ref<Schema['CostRule'] | null>(null)
const budgetForm = reactive({
  mode: 'hard' as 'hard' | 'soft',
  amount: '',
  window: 'calendar_month' as 'calendar_day' | 'calendar_week' | 'calendar_month',
  timezone: 'UTC',
  includeByok: false,
})
let budgetRequest: AbortController | null = null
let budgetCommandKey: { fingerprint: string; key: string } | null = null

const budgetFormValid = computed(
  () => validBudgetCeiling(budgetForm.amount) && Boolean(budgetForm.timezone.trim()),
)

const budgetModeOptions = computed(() => [
  { value: 'hard', label: isZh.value ? '硬性阻断上限（超限拒绝准入）' : 'Hard Cap (Block on limit)' },
  { value: 'soft', label: isZh.value ? '软性预算提醒（超限仅记录预警）' : 'Soft Limit (Alert without blocking)' },
])
const budgetWindowOptions = computed(() => [
  { value: 'calendar_month', label: isZh.value ? '自然月' : 'Calendar Month' },
  { value: 'calendar_week', label: isZh.value ? '自然周' : 'Calendar Week' },
  { value: 'calendar_day', label: isZh.value ? '自然日' : 'Calendar Day' },
])

function formatBudgetWindowLabel(w: string | undefined): string {
  if (!w) return '—'
  if (isZh.value) {
    if (w === 'calendar_month') return '自然月'
    if (w === 'calendar_week') return '自然周'
    if (w === 'calendar_day') return '自然日'
  } else {
    if (w === 'calendar_month') return 'Monthly'
    if (w === 'calendar_week') return 'Weekly'
    if (w === 'calendar_day') return 'Daily'
  }
  return w
}

function openBudgetModal() {
  if (!canWriteBudget.value || budgetState.value === 'working') return
  const rule = projectBudgetRule.value
  budgetForm.mode = rule?.mode === 'soft' ? 'soft' : 'hard'
  budgetForm.amount = rule?.limit ?? '100.00'
  budgetForm.window =
    rule?.window === 'calendar_day' || rule?.window === 'calendar_week' || rule?.window === 'calendar_month'
      ? rule.window
      : 'calendar_month'
  budgetForm.timezone = rule?.timezone || applied.value.timezone || 'UTC'
  budgetForm.includeByok = Boolean(rule?.include_byok_external_estimates)
  budgetState.value = 'idle'
  budgetError.value = null
  budgetCommandKey = null
  budgetModalOpen.value = true
}

async function reloadBudgetRule() {
  await quota.refresh()
  const rule = projectBudgetRule.value
  if (rule) {
    budgetForm.mode = rule.mode === 'soft' ? 'soft' : 'hard'
    budgetForm.amount = rule.limit ?? budgetForm.amount
    if (rule.window === 'calendar_day' || rule.window === 'calendar_week' || rule.window === 'calendar_month') {
      budgetForm.window = rule.window
    }
    budgetForm.timezone = rule.timezone || budgetForm.timezone
    budgetForm.includeByok = Boolean(rule.include_byok_external_estimates)
  }
  budgetCommandKey = null
  budgetState.value = 'idle'
  budgetError.value = null
}

async function submitBudget() {
  const target = scope.value
  if (!target || !canWriteBudget.value || !budgetFormValid.value || budgetState.value === 'working' || budgetState.value === 'conflict') {
    return
  }
  const currency = applied.value.currency
  const budgetId = `project-${currency}`
  const expectedVersion = projectBudgetRule.value?.version ?? '0'
  const input: Schema['CostBudgetInput'] = {
    mode: budgetForm.mode,
    limit: { currency, amount: budgetForm.amount.trim() },
    timezone: budgetForm.timezone.trim(),
    window: budgetForm.window,
    include_byok_external_estimates: budgetForm.includeByok,
  }
  const fingerprint = JSON.stringify([expectedVersion, input])
  if (budgetState.value === 'unknown' && budgetCommandKey?.fingerprint !== fingerprint) return
  if (budgetCommandKey?.fingerprint !== fingerprint) {
    budgetCommandKey = { fingerprint, key: newCommandKey() }
  }
  const controller = new AbortController()
  budgetRequest = controller
  budgetState.value = 'working'
  budgetError.value = null
  try {
    const saved = await billingApi.updateBudget(
      target,
      budgetId,
      input,
      expectedVersion,
      budgetCommandKey.key,
      controller.signal,
    )
    if (controller.signal.aborted) return
    budgetSavedRule.value = saved
    budgetState.value = 'idle'
    budgetCommandKey = null
    budgetModalOpen.value = false
    await quota.refresh()
  } catch (cause) {
    if (controller.signal.aborted) return
    const issue = normalizeApiError(cause)
    budgetError.value = issue
    if (issue.status === 409 || issue.status === 412) budgetState.value = 'conflict'
    else if (issue.status === 0 || issue.status >= 500 || issue.metadata.retry === 'query_status') budgetState.value = 'unknown'
    else budgetState.value = 'failed'
  }
}

function clearBudgetState() {
  budgetRequest?.abort()
  budgetModalOpen.value = false
  budgetState.value = 'idle'
  budgetError.value = null
  budgetCommandKey = null
  budgetSavedRule.value = null
}
watch(() => [scopeKey.value, applied.value.currency], clearBudgetState)
onScopeDispose(clearBudgetState)

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
  { value: 'model', label: t('costLedger.filters.model') }, { value: 'key', label: t('costLedger.filters.key') },
])
const sources = computed(() => [{ value: '', label: t('costLedger.filters.allSources') }, ...fundingSources.map(value => ({
  value, label: t(`costLedger.funding.${value}`),
}))])
const settlements = computed(() => [{ value: '', label: t('costLedger.filters.allSettlements') }, ...settlementStates.map(value => ({
  value, label: t(`costLedger.states.${value}`),
}))])
const tabs = computed(() => [
  { value: 'charges', label: t('costLedger.billing.charges') },
  ...(can('billing.ledger.read') || view.value === 'ledger' ? [{ value: 'ledger', label: t('costLedger.billing.ledger') }] : []),
])
const chargeColumns = computed<UiTableColumn[]>(() => view.value === 'charges' ? [
  { key: 'request', label: t('costLedger.requests.request') }, { key: 'date', label: t('costLedger.requests.started') },
  { key: 'state', label: t('costLedger.requests.settlement') }, { key: 'money', label: t('costLedger.requests.charge'), mono: true },
  { key: 'price', label: t('costLedger.requests.priceVersion') },
] : [
  { key: 'request', label: t('costLedger.requests.request') }, { key: 'date', label: t('costLedger.billing.postedAt') },
  { key: 'state', label: t('costLedger.billing.entry') }, { key: 'money', label: t('costLedger.billing.signedAmount'), mono: true },
  { key: 'price', label: t('costLedger.billing.originalEntry') },
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
function inspectWorkspaceProject(projectId: string | null) {
  if (!scope.value || !projectId) return
  activityScopeMode.value = 'project'
  choose({ workspaceId: scope.value.workspaceId, projectId })
}
async function refresh() {
  await access.refresh()
  if (can('billing.read')) {
    const jobs: Array<Promise<unknown>> = [trend.refresh(), groups.refresh(), quota.refresh(), records.refresh()]
    if (activityScopeMode.value === 'workspace') jobs.push(workspaceActivity.refresh())
    await Promise.all(jobs)
  }
}
</script>

<template>
  <AppLayout contained>
    <div class="min-w-0 space-y-5">
      <PageHeader :title="t('costLedger.billing.title')" :description="t('costLedger.billing.description')">
        <template #actions>
          <div class="flex flex-wrap items-end gap-3">
            <SegmentedControl
              v-if="scope && can('billing.read')"
              v-model="activityScopeMode"
              size="sm"
              :aria-label="isZh ? '成本聚合视图切换' : 'Cost Aggregation Scope'"
              :options="[
                { value: 'project', label: isZh ? '当前项目明细' : 'Project View' },
                { value: 'workspace', label: isZh ? '工作空间跨项目汇总' : 'Workspace Rollup' },
              ]"
            />
            <ScopePicker :scope="scope" @change="choose" />
          </div>
        </template>
      </PageHeader>
      <BillingNav />
      <EmptyState v-if="!scope" :title="t('costLedger.scope.select')" :description="t('costLedger.scope.required')" />
      <template v-else>
        <ReadNotice :loading="access.loading.value" :error="access.error.value" :has-data="Boolean(access.data.value)" :offline="access.offline.value" :last-synced="access.lastSynced.value" @retry="access.refresh" />
        <p v-if="access.data.value && !can('billing.read') && !access.loading.value" role="status" class="rounded-oh-lg border border-oh-default bg-oh-surface p-4 text-sm text-oh-secondary">
          {{ t('costLedger.state.requiredGrant', { grant: 'billing.read' }) }}
        </p>
        <template v-if="can('billing.read')">
          <Card padding="md">
            <form class="grid grid-cols-1 items-end gap-3 sm:grid-cols-2 md:grid-cols-4 2xl:grid-cols-8" @submit.prevent="apply">
              <DateRangeFields v-model:from="draft.from" v-model:to="draft.to" v-model:timezone="draft.timezone" required />
              <Input v-model="draft.currency" :label="t('costLedger.billing.currency')" placeholder="USD" :maxlength="3" mono />
              <Input v-model="draft.model_id" :label="t('costLedger.filters.model')" :placeholder="t('costLedger.filters.anyModel')" :maxlength="255" mono />
              <label class="grid min-w-0 gap-1.5 text-xs font-medium text-oh-secondary">
                {{ t('costLedger.filters.key') }}
                <input v-model.trim="draft.key_id" list="cost-ledger-keys" inputmode="numeric" maxlength="19" :placeholder="t('costLedger.filters.anyKey')" class="oh-focus-ring h-9 w-full min-w-0 rounded-oh-md border border-oh-default bg-oh-surface px-2.5 font-mono text-base text-oh-primary sm:text-sm" />
                <datalist id="cost-ledger-keys"><option v-for="key in keys.data.value?.items ?? []" :key="key.id" :value="key.id">{{ key.name }}</option></datalist>
              </label>
              <Select v-model="draft.funding_source" :label="t('costLedger.billing.fundingSource')" :options="sources" />
              <Select v-model="draft.settlement_state" :label="t('costLedger.requests.settlement')" :options="settlements" />
              <div class="col-span-full flex flex-wrap items-center gap-2 border-t border-oh-subtle pt-3">
                <Button type="submit" variant="primary" :disabled="Boolean(filterIssue)">{{ t('costLedger.filters.apply') }}</Button>
                <Button type="button" variant="ghost" :disabled="trend.loading.value" @click="refresh">{{ t('costLedger.state.refresh') }}</Button>
                <Button
                  v-if="can('billing.export') && csv.state.value === 'idle'"
                  type="button" variant="secondary" :disabled="!window || trend.offline.value" @click="csv.create()"
                >
                  {{ t('costLedger.export.create') }}
                </Button>
                <p v-if="filterIssue" role="status" class="text-sm text-oh-status-danger-fg">{{ filterIssue }}</p>
              </div>
            </form>
          </Card>
          <CsvExportNotice :state="csv.state.value" :operation-id="csv.operationID.value" :error="csv.error.value" @check="csv.check" @retry="csv.create()" @renew="csv.create(true)" @download="csv.download" />

          <div
            v-if="budgetSavedRule"
            role="status"
            class="flex flex-wrap items-center justify-between gap-2 rounded-oh-md border border-oh-status-ok-border bg-oh-status-ok-bg px-3 py-2 text-sm"
          >
            <span>
              {{
                isZh
                  ? `项目预算已更新：${budgetSavedRule.unit} ${budgetSavedRule.limit ?? '—'}（${formatBudgetWindowLabel(budgetSavedRule.window)}）`
                  : `Project budget updated: ${budgetSavedRule.unit} ${budgetSavedRule.limit ?? '—'} (${formatBudgetWindowLabel(budgetSavedRule.window)})`
              }}
            </span>
            <Button size="xs" variant="ghost" @click="budgetSavedRule = null">
              {{ isZh ? '关闭' : 'Dismiss' }}
            </Button>
          </div>

          <Card v-if="activityScopeMode === 'workspace'" padding="md" data-testid="workspace-billing-rollup">
            <div class="space-y-3">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <div>
                  <h2 class="text-base font-semibold text-oh-primary">
                    {{ isZh ? `工作空间 #${scope.workspaceId} 跨项目成本汇总` : `Workspace #${scope.workspaceId} Cross-Project Activity` }}
                  </h2>
                  <p class="text-xs text-oh-secondary">
                    {{
                      isZh
                        ? '聚合当前工作空间下您具备账单查看权限的所有项目费用与请求量，点击任一项目可切换至该项目明细。'
                        : 'Aggregates spend and logical requests across projects in this workspace. Select a project row to drill down.'
                    }}
                  </p>
                </div>
                <Button size="sm" variant="ghost" :disabled="workspaceActivity.loading.value" @click="workspaceActivity.refresh">
                  {{ t('costLedger.state.refresh') }}
                </Button>
              </div>
              <ReadNotice
                :loading="workspaceActivity.loading.value"
                :error="workspaceActivity.error.value"
                :has-data="Boolean(workspaceActivity.data.value)"
                :offline="workspaceActivity.offline.value"
                :last-synced="workspaceActivity.lastSynced.value"
                grant="billing.read"
                @retry="workspacePager.cursor.value && workspaceActivity.error.value?.status === 410 ? workspacePager.reset() : workspaceActivity.refresh()"
              />
              <template v-if="workspaceActivity.data.value">
                <div class="grid min-w-0 grid-cols-2 gap-3 xl:grid-cols-4">
                  <Card padding="sm">
                    <StatNumber
                      :label="isZh ? '工作空间总支出' : 'Workspace Spend'"
                      :value="workspaceActivity.data.value.totals.platform_charge ? moneyText(workspaceActivity.data.value.totals.platform_charge, locale) : null"
                      :unknown-label="t('costLedger.requests.unobserved')"
                      accent
                    />
                  </Card>
                  <Card padding="sm">
                    <StatNumber
                      :label="t('costLedger.requests.logicalRequests')"
                      :value="displayNumber(workspaceActivity.data.value.totals.logical_requests, locale)"
                    />
                  </Card>
                  <Card padding="sm">
                    <StatNumber
                      :label="t('costLedger.requests.attempts')"
                      :value="workspaceActivity.data.value.totals.attempts === null ? null : displayNumber(workspaceActivity.data.value.totals.attempts, locale)"
                      :unknown-label="t('costLedger.requests.unobserved')"
                    />
                  </Card>
                  <Card padding="sm">
                    <StatNumber
                      :label="t('costLedger.requests.reserved')"
                      :value="workspaceActivity.data.value.totals.reserved ? moneyText(workspaceActivity.data.value.totals.reserved, locale) : null"
                      :unknown-label="t('costLedger.requests.unobserved')"
                    />
                  </Card>
                </div>
                <EmptyState
                  v-if="!workspaceActivity.data.value.items.length"
                  :title="t('costLedger.state.noMatches')"
                  :description="t('costLedger.billing.noTrend')"
                  compact
                />
                <ul v-else class="divide-y divide-oh-subtle">
                  <li
                    v-for="row in workspaceActivity.data.value.items"
                    :key="row.group ?? 'unknown'"
                    class="flex min-w-0 flex-wrap items-center justify-between gap-2 py-2.5 text-sm"
                  >
                    <button
                      v-if="row.group"
                      type="button"
                      class="oh-focus-ring rounded-oh-xs font-mono text-xs font-medium text-oh-accent-text hover:underline"
                      @click="inspectWorkspaceProject(row.group)"
                    >
                      {{ isZh ? `项目 #${row.group}` : `Project #${row.group}` }}
                    </button>
                    <span v-else class="text-oh-muted">{{ t('costLedger.states.unknown') }}</span>
                    <div class="flex flex-wrap items-center gap-x-3 gap-y-1 font-mono text-xs tabular-nums">
                      <span class="text-oh-muted">{{ t('costLedger.billing.groupRequests', { count: displayNumber(row.logical_requests, locale) }) }}</span>
                      <strong class="font-medium text-oh-primary">{{ moneyText(row.platform_charge, locale) }}</strong>
                    </div>
                  </li>
                </ul>
                <CursorButtons
                  :next="Boolean(workspaceActivity.data.value.next_cursor)"
                  :previous="workspacePager.hasPrevious.value"
                  :loading="workspaceActivity.loading.value"
                  @next="workspacePager.next(workspaceActivity.data.value.next_cursor)"
                  @previous="workspacePager.previous"
                />
              </template>
            </div>
          </Card>

          <div v-if="totals" class="grid min-w-0 grid-cols-2 gap-3 xl:grid-cols-4">
            <Card padding="sm"><StatNumber class="max-w-full [&_.oh-tabular]:min-w-0 [&_.oh-tabular]:break-all max-sm:[&_.oh-tabular]:text-sm" :label="t('costLedger.billing.spend')" :value="totals.platform_charge ? moneyText(totals.platform_charge, locale) : null" :unknown-label="t('costLedger.requests.unobserved')" accent /></Card>
            <Card padding="sm"><StatNumber class="max-w-full [&_.oh-tabular]:min-w-0 [&_.oh-tabular]:break-all max-sm:[&_.oh-tabular]:text-sm" :label="t('costLedger.requests.logicalRequests')" :value="displayNumber(totals.logical_requests, locale)" /></Card>
            <Card padding="sm"><StatNumber class="max-w-full [&_.oh-tabular]:min-w-0 [&_.oh-tabular]:break-all max-sm:[&_.oh-tabular]:text-sm" :label="t('costLedger.requests.attempts')" :value="totals.attempts === null ? null : displayNumber(totals.attempts, locale)" :unknown-label="t('costLedger.requests.unobserved')" /></Card>
            <Card padding="sm"><StatNumber class="max-w-full [&_.oh-tabular]:min-w-0 [&_.oh-tabular]:break-all max-sm:[&_.oh-tabular]:text-sm" :label="t('costLedger.requests.reserved')" :value="totals.reserved ? moneyText(totals.reserved, locale) : null" :unknown-label="t('costLedger.requests.unobserved')" /></Card>
          </div>

          <div class="grid min-w-0 gap-4 xl:grid-cols-[minmax(0,1.25fr)_minmax(0,1fr)]">
            <Card padding="md" class="min-w-0">
              <div class="space-y-3">
                <h2 class="text-base font-semibold text-oh-primary">{{ t('costLedger.billing.trend') }}</h2>
                <ReadNotice :loading="trend.loading.value" :error="trend.error.value" :has-data="Boolean(trend.data.value)" :offline="trend.offline.value" :last-synced="trend.lastSynced.value" grant="billing.read" @retry="resetOrRetry(trend, dailyPager)" />
                <SpendTrend v-if="trend.data.value" :activity="trend.data.value" @day="inspectDay" />
                <p v-if="trend.data.value?.data_completeness.state === 'partial'" class="text-xs text-oh-status-warn-fg">{{ t('costLedger.billing.partial') }}</p>
                <CursorButtons :next="Boolean(trend.data.value?.next_cursor)" :previous="dailyPager.hasPrevious.value" :loading="trend.loading.value" @next="dailyPager.next(trend.data.value?.next_cursor)" @previous="dailyPager.previous" />
              </div>
            </Card>
            <Card padding="md" class="min-w-0">
              <div class="space-y-3">
                <div class="flex flex-wrap items-center justify-between gap-2">
                  <h2 class="text-base font-semibold text-oh-primary">{{ t('costLedger.billing.quota') }}</h2>
                  <Button
                    v-if="canWriteBudget"
                    data-testid="configure-project-budget-btn"
                    size="xs"
                    variant="secondary"
                    :disabled="quota.offline.value"
                    @click="openBudgetModal"
                  >
                    {{
                      projectBudgetRule
                        ? isZh
                          ? '调整项目预算'
                          : 'Edit Project Budget'
                        : isZh
                          ? '设置项目预算'
                          : 'Set Project Budget'
                    }}
                  </Button>
                </div>
                <div
                  v-if="quota.data.value"
                  data-testid="project-budget-summary"
                  class="flex flex-wrap items-center justify-between gap-2 rounded-oh-md border border-oh-subtle bg-oh-subtle/50 px-3 py-2 text-xs"
                >
                  <template v-if="projectBudgetRule">
                    <span class="text-oh-secondary">
                      {{ isZh ? '当前项目预算' : 'Project Budget' }} ({{ applied.currency }} · {{ formatBudgetWindowLabel(projectBudgetRule.window) }})
                    </span>
                    <span class="font-mono font-medium tabular-nums text-oh-primary">
                      {{ displayNumber(projectBudgetRule.used, locale) }} / {{ displayNumber(projectBudgetRule.limit, locale) }} {{ projectBudgetRule.unit }}
                      · {{ projectBudgetRule.mode === 'hard' ? t('costLedger.billing.hardLimit') : t('costLedger.billing.softLimit') }}
                    </span>
                  </template>
                  <span v-else class="text-oh-muted">
                    {{
                      isZh
                        ? `当前项目尚未配置 ${applied.currency} 币种预算上限`
                        : `No ${applied.currency} project budget ceiling configured yet`
                    }}
                  </span>
                </div>
                <ReadNotice :loading="quota.loading.value" :error="quota.error.value" :has-data="Boolean(quota.data.value)" :offline="quota.offline.value" :last-synced="quota.lastSynced.value" grant="billing.read" @retry="quota.refresh" />
                <QuotaWaterfall v-if="quota.data.value" :quota="quota.data.value" />
              </div>
            </Card>
          </div>

          <Card padding="md">
            <div class="space-y-3">
              <div class="flex flex-wrap items-center justify-between gap-3">
                <h2 class="text-base font-semibold text-oh-primary">{{ t('costLedger.billing.groupBreakdown') }}</h2>
                <Select :model-value="applied.group_by" :label="t('costLedger.billing.groupBy')" :options="groupValues" @update:model-value="pickGroup" />
              </div>
              <ReadNotice :loading="groups.loading.value" :error="groups.error.value" :has-data="Boolean(groups.data.value)" :offline="groups.offline.value" :last-synced="groups.lastSynced.value" grant="billing.read" @retry="resetOrRetry(groups, groupsPager)" />
              <EmptyState v-if="groups.data.value && !groups.data.value.items.length" :title="t('costLedger.state.noMatches')" :description="t('costLedger.billing.noTrend')" compact />
              <ul v-else-if="groups.data.value" class="divide-y divide-oh-subtle">
                <li v-for="row in groups.data.value.items" :key="row.group ?? 'unknown'" class="flex min-w-0 flex-wrap items-center justify-between gap-2 py-2.5 text-sm">
                  <button v-if="row.group" type="button" class="oh-focus-ring min-w-0 max-w-full break-all rounded-oh-xs text-left font-mono text-xs font-medium text-oh-accent-text hover:underline" @click="inspectGroup(row)">
                    {{ applied.group_by === 'key' ? keys.data.value?.items.find(key => key.id === row.group)?.name || row.group : row.group }}
                  </button>
                  <span v-else class="text-oh-muted">{{ t('costLedger.states.unknown') }}</span>
                  <div class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 font-mono text-xs tabular-nums">
                    <span class="text-oh-muted">{{ t('costLedger.billing.groupRequests', { count: displayNumber(row.logical_requests, locale) }) }}</span>
                    <strong class="break-all font-medium text-oh-primary">{{ moneyText(row.platform_charge, locale) }}</strong>
                  </div>
                </li>
              </ul>
              <CursorButtons :next="Boolean(groups.data.value?.next_cursor)" :previous="groupsPager.hasPrevious.value" :loading="groups.loading.value" @next="groupsPager.next(groups.data.value?.next_cursor)" @previous="groupsPager.previous" />
            </div>
          </Card>

          <section class="min-w-0 space-y-3">
            <Tabs :model-value="view" :items="tabs" :aria-label="t('costLedger.billing.records')" @update:model-value="pickView" />
            <p v-if="view === 'ledger' && !can('billing.ledger.read')" role="status" class="rounded-oh-lg border border-oh-default bg-oh-surface p-4 text-sm text-oh-secondary">{{ t('costLedger.state.requiredGrant', { grant: 'billing.ledger.read' }) }}</p>
            <ReadNotice :loading="records.loading.value" :error="records.error.value" :has-data="Boolean(records.data.value)" :offline="records.offline.value" :last-synced="records.lastSynced.value" :grant="view === 'ledger' ? 'billing.ledger.read' : 'billing.read'" @retry="resetOrRetry(records, ledgerPager)" />
            <Table v-if="records.data.value" :columns="chargeColumns" :rows="records.data.value.items" :row-key="recordId" :empty-title="t('costLedger.billing.noRecords')">
              <template #cell-request="{ row }">
                <RouterLink :to="requestLocation(scope, String(row.request_id), false)" class="oh-focus-ring block min-w-40 rounded-oh-xs break-all font-mono text-xs font-medium text-oh-accent-text hover:underline">{{ view === 'charges' ? asCharge(row).model_id || asCharge(row).request_id : asEntry(row).request_id }}</RouterLink>
              </template>
              <template #cell-date="{ row }"><span class="whitespace-nowrap font-mono text-xs tabular-nums">{{ timestamp(view === 'charges' ? asCharge(row).at : asEntry(row).posted_at, locale, applied.timezone) }}</span></template>
              <template #cell-state="{ row }">
                <Badge v-if="view === 'charges'" :tone="statusTone(asCharge(row).state)" dot>{{ t(`costLedger.states.${asCharge(row).state}`) }}</Badge>
                <span v-else>{{ t(`costLedger.entry.${asEntry(row).kind}`) }} <code>{{ asEntry(row).id }}</code></span>
              </template>
              <template #cell-money="{ row }">
                <span class="whitespace-nowrap">{{ moneyText(view === 'charges' ? asCharge(row).platform_charge : asEntry(row).signed_amount, locale) }}</span>
                <small v-if="view === 'charges' && !asCharge(row).platform_charge && asCharge(row).estimate" class="block whitespace-nowrap text-oh-muted">{{ t('costLedger.billing.estimateValue', { amount: moneyText(asCharge(row).estimate, locale) }) }}</small>
              </template>
              <template #cell-price="{ row }"><code class="text-xs">{{ (view === 'charges' ? asCharge(row).price_version : asEntry(row).reverses_id) ?? '—' }}</code></template>
            </Table>
            <CursorButtons :next="Boolean(records.data.value?.next_cursor)" :previous="ledgerPager.hasPrevious.value" :loading="records.loading.value" @next="ledgerPager.next(records.data.value?.next_cursor)" @previous="ledgerPager.previous" />
          </section>
        </template>
      </template>

      <Modal
        v-model:open="budgetModalOpen"
        :title="isZh ? `配置项目预算上限 (${applied.currency})` : `Configure Project Budget (${applied.currency})`"
        :description="
          isZh
            ? '项目预算对当前项目下所有密钥共享生效，支持硬性准入阻断或软性超支提醒。'
            : 'Project budgets apply across all credentials in this project with either hard admission blocking or soft alerting.'
        "
        :close-on-backdrop="budgetState !== 'working'"
        :close-on-escape="budgetState !== 'working'"
        :hide-close-button="budgetState === 'working'"
      >
        <form class="space-y-3" @submit.prevent="submitBudget">
          <p
            v-if="budgetState === 'conflict'"
            role="alert"
            class="rounded-oh-md border border-oh-status-warn-border bg-oh-status-warn-bg/55 p-3 text-sm"
          >
            {{
              isZh
                ? '预算配置已被其他会话更新，请先同步最新预算规则后再提交。'
                : 'Project budget was updated elsewhere. Reload the latest rule before saving.'
            }}
          </p>
          <p
            v-else-if="budgetState === 'unknown'"
            role="alert"
            class="text-sm text-oh-status-warn-fg"
          >
            {{
              isZh
                ? '预算更新结果暂未确认，可使用相同幂等请求安全重试或刷新配额确认。'
                : 'Budget update outcome is uncertain. Retry safely with the same request or reload quotas.'
            }}
          </p>
          <p
            v-else-if="budgetError"
            role="alert"
            class="text-sm text-oh-status-danger-fg"
          >
            {{ budgetError.message || t('costLedger.state.loadFailed') }}
          </p>

          <Select
            v-model="budgetForm.mode"
            :label="isZh ? '控制模式' : 'Enforcement Mode'"
            :options="budgetModeOptions"
            :disabled="budgetState === 'working' || budgetState === 'unknown'"
          />
          <Select
            v-model="budgetForm.window"
            :label="isZh ? '统计周期' : 'Budget Window'"
            :options="budgetWindowOptions"
            :disabled="budgetState === 'working' || budgetState === 'unknown'"
          />
          <Input
            v-model="budgetForm.amount"
            :label="isZh ? `预算上限 (${applied.currency})` : `Budget Limit (${applied.currency})`"
            placeholder="100.00"
            mono
            required
            :readonly="budgetState === 'working' || budgetState === 'unknown'"
          />
          <Input
            v-model="budgetForm.timezone"
            :label="t('costLedger.filters.timezone')"
            placeholder="UTC"
            mono
            required
            :readonly="budgetState === 'working' || budgetState === 'unknown'"
          />
          <label class="flex items-center gap-2 text-xs text-oh-primary">
            <input
              v-model="budgetForm.includeByok"
              type="checkbox"
              class="h-4 w-4"
              :disabled="budgetState === 'working' || budgetState === 'unknown'"
            />
            <span>
              {{
                isZh
                  ? '将自带上游凭据 (BYOK) 的外部参考估算计入项目预算消耗'
                  : 'Include BYOK external cost estimates in project budget usage'
              }}
            </span>
          </label>

          <div class="flex flex-wrap items-center justify-end gap-2 pt-2">
            <Button
              v-if="budgetState === 'conflict'"
              type="button"
              variant="secondary"
              @click="reloadBudgetRule"
            >
              {{ isZh ? '同步最新预算' : 'Reload Budget' }}
            </Button>
            <Button
              v-else
              type="button"
              variant="ghost"
              :disabled="budgetState === 'working'"
              @click="budgetModalOpen = false"
            >
              {{ t('common.cancel') }}
            </Button>
            <Button
              type="submit"
              variant="primary"
              :loading="budgetState === 'working'"
              :disabled="!budgetFormValid || budgetState === 'conflict'"
            >
              {{
                budgetState === 'unknown'
                  ? isZh
                    ? '安全重试保存'
                    : 'Retry Save'
                  : isZh
                    ? '保存项目预算'
                    : 'Save Budget'
              }}
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  </AppLayout>
</template>
