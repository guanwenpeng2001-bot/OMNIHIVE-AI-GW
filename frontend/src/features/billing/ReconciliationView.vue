<script setup lang="ts">
import { computed, onScopeDispose, reactive, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { Badge, Button, Card, CopyButton, EmptyState, Input, Modal, PageHeader, Select, Table, Tabs, type UiTableColumn } from '@/components/ui'
import { isStepUpCancelled, useStepUp } from '@/composables/useStepUp'
import { normalizeApiError, type NormalizedApiError } from '@/utils/apiError'
import { queryParams, requestLocation, type Schema } from '@/features/requests/api'
import { dateFields, dateInZone, queryText, shiftCalendar, windowQuery } from '@/features/requests/filters'
import { moneyText, statusTone, timestamp } from '@/features/requests/presentation'
import { useCursor } from '@/features/requests/useCursor'
import { useProjectScope } from '@/features/requests/useProjectScope'
import { useScopedRead } from '@/features/requests/useScopedRead'
import CursorButtons from '@/features/requests/components/CursorButtons.vue'
import DateRangeFields from '@/features/requests/components/DateRangeFields.vue'
import ReadNotice from '@/features/requests/components/ReadNotice.vue'
import ScopePicker from '@/features/requests/components/ScopePicker.vue'
import { billingApi, newCommandKey } from './api'
import { costFilters, costWindow, currencyCode, validAdjustment } from './presentation'

type Case = Schema['CostCase']
type Entry = Schema['CostEntry']
type Mutation = 'idle' | 'working' | 'failed' | 'unknown' | 'conflict'
const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const { scope, scopeKey, access, choose, can } = useProjectScope()
const stepUp = useStepUp()
const view = computed(() => queryText(route.query, 'view') === 'ledger' ? 'ledger' : 'reconciliation')
const state = computed(() => {
  const value = queryText(route.query, 'state')
  return value === 'needs_review' || value === 'resolved' ? value : ''
})
const caseCurrency = computed(() => currencyCode(queryText(route.query, 'case_currency')) ?? '')
const requestId = computed(() => queryText(route.query, 'request_id').slice(0, 255))
const ledgerFilters = computed(() => ({ ...costFilters(route.query), ...dateFields(route.query, 30) }))
const ledgerDraft = reactive({ ...ledgerFilters.value })
const caseDraft = reactive({ state: state.value, currency: caseCurrency.value })
watch(ledgerFilters, filters => Object.assign(ledgerDraft, filters))
watch([state, caseCurrency], ([next, currency]) => Object.assign(caseDraft, { state: next, currency }))
const ledgerWindow = computed(() => costWindow(ledgerFilters.value))
const draftLedgerWindow = computed(() => costWindow(ledgerDraft))
const caseKey = computed(() => can('billing.reconcile.read') && view.value === 'reconciliation'
  ? JSON.stringify([scopeKey.value, state.value, caseCurrency.value]) : '')
const ledgerKey = computed(() => can('billing.ledger.read') && view.value === 'ledger' && ledgerWindow.value
  ? JSON.stringify([scopeKey.value, ledgerWindow.value, requestId.value]) : '')
const casePager = useCursor(() => caseKey.value)
const ledgerPager = useCursor(() => ledgerKey.value)
const cases = useScopedRead(() => caseKey.value ? JSON.stringify([caseKey.value, casePager.cursor.value]) : '',
  signal => billingApi.cases(scope.value!, state.value, caseCurrency.value, casePager.cursor.value, signal))
const ledger = useScopedRead(() => ledgerKey.value ? JSON.stringify([ledgerKey.value, ledgerPager.cursor.value]) : '',
  signal => billingApi.ledger(scope.value!, queryParams({
    ...ledgerWindow.value, request_id: requestId.value, cursor: ledgerPager.cursor.value,
  }), signal))
const tabs = computed(() => [
  ...(can('billing.reconcile.read') || view.value === 'reconciliation' ? [{ value: 'reconciliation', label: t('wp6.reconciliation.cases') }] : []),
  ...(can('billing.ledger.read') || view.value === 'ledger' ? [{ value: 'ledger', label: t('wp6.billing.ledger') }] : []),
])
const caseStates = computed(() => [
  { value: '', label: t('wp6.filters.allSettlements') },
  { value: 'needs_review', label: t('wp6.states.needs_review') },
  { value: 'resolved', label: t('wp6.states.resolved') },
])
const caseColumns = computed<UiTableColumn[]>(() => [
  { key: 'case', label: t('wp6.reconciliation.case') },
  { key: 'reason', label: t('wp6.reconciliation.reason') },
  { key: 'state', label: t('wp6.requests.settlement') },
  { key: 'difference', label: t('wp6.reconciliation.difference'), mono: true },
  { key: 'actions', label: t('wp6.reconciliation.actions') },
])
const ledgerColumns = computed<UiTableColumn[]>(() => [
  { key: 'entry', label: t('wp6.billing.entry') },
  { key: 'request', label: t('wp6.requests.request') },
  { key: 'posted', label: t('wp6.billing.postedAt') },
  { key: 'amount', label: t('wp6.billing.signedAmount'), mono: true },
  ...(can('billing.adjust') ? [{ key: 'actions', label: t('wp6.reconciliation.actions') }] : []),
])
const asCase = (row: Record<string, unknown>) => row as Case
const asEntry = (row: Record<string, unknown>) => row as Entry

function pickView(value: string) {
  void router.replace({ query: { ...route.query, view: value === 'ledger' ? 'ledger' : undefined, request_id: undefined } })
}
function applyCases() {
  if (caseDraft.currency && !currencyCode(caseDraft.currency)) return
  void router.replace({ query: { ...route.query, state: caseDraft.state || undefined, case_currency: currencyCode(caseDraft.currency) || undefined } })
}
function applyLedger() {
  if (!draftLedgerWindow.value) return
  void router.replace({ query: {
    ...route.query, ...windowQuery(ledgerDraft), currency: draftLedgerWindow.value.currency,
  } })
}
function openCaseLedger(record: Case) {
  const currency = currencyCode(record.currency)
  void router.replace({ query: { ...route.query, view: 'ledger', request_id: record.request_id, currency: currency ?? undefined } })
}
function resetOrRetry(resource: typeof cases | typeof ledger, pager: ReturnType<typeof useCursor>) {
  if (resource.error.value?.status === 410 && pager.cursor.value) pager.reset()
  else void resource.refresh()
}

const activeEntry = ref<Entry | null>(null)
const entryDialog = ref(false)
const adjustment = reactive({ amount: '', note: '' })
const adjustState = ref<Mutation>('idle')
const adjustError = ref<NormalizedApiError | null>(null)
const posted = ref<Schema['CostAdjustment'] | null>(null)
const adjustmentValid = computed(() => validAdjustment(adjustment.amount, adjustment.note))
let adjustmentRequest: AbortController | null = null
let adjustmentKey: { input: string; value: string } | null = null

function openAdjustment(entry: Entry) {
  if (adjustState.value === 'unknown' || !can('billing.adjust') || ledger.stale.value || ledger.offline.value) return
  if (entry.kind !== 'charge' || entry.signed_amount.currency !== 'USD') return
  activeEntry.value = entry
  adjustmentKey = null
  Object.assign(adjustment, { amount: '', note: '' })
  adjustState.value = 'idle'
  adjustError.value = null
  entryDialog.value = true
}
function checkLedger() {
  entryDialog.value = false
  void ledger.refresh()
}
function classify(cause: unknown): { state: Mutation; error: NormalizedApiError } {
  const error = normalizeApiError(cause)
  const state = error.status === 409 || error.status === 412 ? 'conflict'
    : error.status === 0 || error.status >= 500 || error.metadata.retry === 'query_status' ? 'unknown' : 'failed'
  return { state, error }
}
async function submitAdjustment() {
  const target = scope.value
  const entry = activeEntry.value
  if (!entry || !target || !can('billing.adjust') || !can('billing.ledger.read') || !adjustmentValid.value ||
      ledger.stale.value || ledger.offline.value || adjustState.value === 'working' || adjustState.value === 'conflict') return
  const input = {
    ledger_entry_id: entry.id,
    amount: { currency: entry.signed_amount.currency, amount: adjustment.amount.trim() },
    reason: 'billing_correction' as const, note: adjustment.note.trim(),
  }
  const fingerprint = JSON.stringify(input)
  if (adjustState.value === 'unknown' && adjustmentKey?.input !== fingerprint) return
  if (adjustmentKey?.input !== fingerprint) adjustmentKey = { input: fingerprint, value: newCommandKey() }
  const key = adjustmentKey.value
  const previousState = adjustState.value
  const controller = new AbortController()
  adjustmentRequest = controller
  adjustState.value = 'working'
  adjustError.value = null

  try {
    const result = await stepUp.run(() => {
      if (controller.signal.aborted || !can('billing.adjust') || !can('billing.ledger.read')) {
        throw { status: 403, reason: 'ACTION_DENIED' }
      }
      return billingApi.postAdjustment(target, input, key, controller.signal)
    })
    if (controller.signal.aborted) return
    posted.value = result
    entryDialog.value = false
    activeEntry.value = null
    adjustmentKey = null
    adjustState.value = 'idle'
    if (ledgerPager.cursor.value) ledgerPager.reset()
    else void ledger.refresh()
  } catch (cause) {
    if (controller.signal.aborted) return
    if (isStepUpCancelled(cause)) {
      adjustState.value = previousState
      return
    }
    const issue = classify(cause)
    adjustState.value = issue.state
    adjustError.value = issue.error
  }
}

const caseDialog = ref(false)
const activeCase = ref<Case | null>(null)
const evidence = reactive({ id: '', note: '' })
const evidenceValid = computed(() => Boolean(evidence.id.trim()) &&
  new TextEncoder().encode(evidence.id.trim()).byteLength <= 255 && new TextEncoder().encode(evidence.note.trim()).byteLength <= 1024)
const evidenceState = ref<Mutation>('idle')
const evidenceError = ref<NormalizedApiError | null>(null)
const resolvedOperation = ref<string | null>(null)
let evidenceRequest: AbortController | null = null
let evidenceKey: { input: string; value: string } | null = null
function openEvidence(record: Case) {
  if (!can('billing.reconcile.write') || cases.stale.value || cases.offline.value || evidenceState.value === 'unknown') return
  activeCase.value = record
  evidenceState.value = 'idle'
  evidenceError.value = null
  evidenceKey = null
  Object.assign(evidence, { id: '', note: '' })
  caseDialog.value = true
}
async function submitEvidence() {
  const target = scope.value
  const record = activeCase.value
  if (!target || !record || !can('billing.reconcile.write') || !evidenceValid.value ||
      cases.stale.value || cases.offline.value || evidenceState.value === 'working' || evidenceState.value === 'conflict') return
  const input = { action: 'attach_evidence' as const, evidence_id: evidence.id.trim(), note: evidence.note.trim() }
  const fingerprint = JSON.stringify([record.version, input])
  if (evidenceState.value === 'unknown' && evidenceKey?.input !== fingerprint) return
  if (evidenceKey?.input !== fingerprint) evidenceKey = { input: fingerprint, value: newCommandKey() }
  const controller = new AbortController()
  evidenceRequest = controller
  evidenceState.value = 'working'
  evidenceError.value = null
  try {
    const result = await billingApi.resolveCase(target, record, input, evidenceKey.value, controller.signal)
    if (controller.signal.aborted) return
    resolvedOperation.value = result.operation_id
    evidenceState.value = 'idle'
    caseDialog.value = false
    void cases.refresh()
  } catch (cause) {
    if (controller.signal.aborted) return
    const issue = classify(cause)
    evidenceState.value = issue.state
    evidenceError.value = issue.error
  }
}
async function reloadCase() {
  await cases.refresh()
  const fresh = cases.data.value?.items.find(record => record.id === activeCase.value?.id)
  if (fresh?.state !== 'needs_review') {
    caseDialog.value = false
    return
  }
  activeCase.value = fresh
  evidenceKey = null
  evidenceState.value = 'idle'
  evidenceError.value = null
}

function clearCommands() {
  adjustmentRequest?.abort()
  evidenceRequest?.abort()
  stepUp.onCancel()
  activeEntry.value = null
  activeCase.value = null
  entryDialog.value = false
  caseDialog.value = false
  adjustmentKey = null
  evidenceKey = null
  Object.assign(adjustment, { amount: '', note: '' })
  Object.assign(evidence, { id: '', note: '' })
  adjustError.value = null
  evidenceError.value = null
  adjustState.value = 'idle'
  evidenceState.value = 'idle'
  posted.value = null
  resolvedOperation.value = null
}
watch(() => [scopeKey.value, can('billing.ledger.read'), can('billing.adjust'), can('billing.reconcile.write')], clearCommands)
onScopeDispose(clearCommands)

function openPostedLedger() {
  const to = dateInZone(Date.now(), ledgerFilters.value.timezone)
  void router.replace({ query: { ...route.query, request_id: undefined, to: shiftCalendar(to, 1) } })
  void ledger.refresh()
}
</script>

<template>
  <AppLayout tier="admin" contained>
    <div class="min-w-0 space-y-5">
      <PageHeader :title="t('wp6.reconciliation.title')" :description="t('wp6.reconciliation.description')">
        <template #actions><ScopePicker :scope="scope" @change="choose" /></template>
      </PageHeader>
      <EmptyState v-if="!scope" :title="t('wp6.scope.select')" :description="t('wp6.scope.required')" />
      <template v-else>
        <ReadNotice :loading="access.loading.value" :error="access.error.value" :has-data="Boolean(access.data.value)" :offline="access.offline.value" :last-synced="access.lastSynced.value" @retry="access.refresh" />
        <Tabs v-if="access.data.value" :model-value="view" :items="tabs" :aria-label="t('wp6.reconciliation.title')" @update:model-value="pickView" />

        <p v-if="adjustState === 'unknown' && !entryDialog" role="status" class="rounded-oh-md border border-oh-status-warn-border bg-oh-status-warn-bg/55 p-3 text-sm">
          {{ t('wp6.reconciliation.adjustUnknown') }}
          <Button size="sm" variant="secondary" class="ml-1" @click="entryDialog = true">{{ t('wp6.reconciliation.openPending') }}</Button>
        </p>
        <div v-if="posted" role="status" class="flex flex-wrap items-center justify-between gap-2 rounded-oh-md border border-oh-status-ok-border bg-oh-status-ok-bg px-3 py-2.5 text-sm">
          <span>{{ t('wp6.reconciliation.posted', { entry: posted.ledger_entry_id, project: scope.projectId }) }}</span>
          <div class="flex flex-wrap items-center gap-1">
            <CopyButton :value="posted.ledger_entry_id" :label="t('wp6.copy.label')" :copied-label="t('wp6.copy.copied')" :failed-label="t('wp6.copy.failed')" size="xs" />
            <Button size="sm" variant="secondary" @click="openPostedLedger">{{ t('wp6.billing.openLedger') }}</Button>
          </div>
        </div>
        <div v-if="resolvedOperation" role="status" class="flex flex-wrap items-center gap-2 rounded-oh-md border border-oh-status-ok-border bg-oh-status-ok-bg px-3 py-2.5 text-sm">
          {{ t('wp6.reconciliation.evidenceSaved') }} <code class="break-all">{{ resolvedOperation }}</code>
          <CopyButton :value="resolvedOperation" :label="t('wp6.copy.label')" :copied-label="t('wp6.copy.copied')" :failed-label="t('wp6.copy.failed')" size="xs" />
        </div>

        <template v-if="view === 'reconciliation'">
          <p v-if="access.data.value && !can('billing.reconcile.read') && !access.loading.value" role="status" class="rounded-oh-lg border border-oh-default bg-oh-surface p-4 text-sm text-oh-secondary">{{ t('wp6.state.requiredGrant', { grant: 'billing.reconcile.read' }) }}</p>
          <template v-if="can('billing.reconcile.read')">
            <Card padding="md">
              <form class="flex flex-wrap items-end gap-3" @submit.prevent="applyCases">
                <Select v-model="caseDraft.state" :label="t('wp6.requests.settlement')" :options="caseStates" />
                <Input v-model="caseDraft.currency" :label="t('wp6.billing.currency')" :placeholder="t('wp6.filters.allCurrencies')" :maxlength="3" mono />
                <Button type="submit" variant="primary" :disabled="Boolean(caseDraft.currency && !currencyCode(caseDraft.currency))">{{ t('wp6.filters.apply') }}</Button>
                <Button type="button" variant="ghost" :disabled="cases.loading.value" @click="resetOrRetry(cases, casePager)">{{ t('wp6.state.refresh') }}</Button>
              </form>
            </Card>
            <p class="text-xs text-oh-muted">{{ t('wp6.reconciliation.separateCurrencies') }}</p>
            <p v-if="evidenceState === 'unknown' && !caseDialog" role="status" class="rounded-oh-md border border-oh-status-warn-border bg-oh-status-warn-bg/55 p-3 text-sm">
              {{ t('wp6.reconciliation.evidenceUnknown') }}
              <Button size="sm" variant="secondary" class="ml-1" @click="caseDialog = true">{{ t('wp6.reconciliation.openPending') }}</Button>
            </p>
            <ReadNotice :loading="cases.loading.value" :error="cases.error.value" :has-data="Boolean(cases.data.value)" :offline="cases.offline.value" :last-synced="cases.lastSynced.value" grant="billing.reconcile.read" @retry="resetOrRetry(cases, casePager)" />
            <Table
              v-if="cases.data.value" :columns="caseColumns" :rows="cases.data.value.items" :caption="t('wp6.reconciliation.cases')"
              :filtered="Boolean(state || caseCurrency)" :empty-title="t('wp6.reconciliation.empty')" :filtered-empty-title="t('wp6.state.noMatches')" :filtered-empty-description="t('wp6.state.noMatchesDescription')"
              @clear-filters="caseDraft.state = ''; caseDraft.currency = ''; applyCases()"
            >
              <template #cell-case="{ row }">
                <div class="grid min-w-32 gap-1 font-mono text-xs">
                  <span>{{ asCase(row).id }}</span>
                  <RouterLink :to="requestLocation(scope, asCase(row).request_id, true)" class="oh-focus-ring rounded-oh-xs break-all text-oh-accent-text hover:underline">{{ asCase(row).request_id }}</RouterLink>
                </div>
              </template>
              <template #cell-reason="{ row }"><code class="text-xs text-oh-secondary">{{ asCase(row).reason }}</code></template>
              <template #cell-state="{ row }"><Badge :tone="statusTone(asCase(row).state)" dot>{{ t(`wp6.states.${asCase(row).state}`) }}</Badge></template>
              <template #cell-difference="{ row }">
                <span class="whitespace-nowrap">{{ moneyText(asCase(row).difference, locale) }}</span>
                <span v-if="!asCase(row).difference" class="ml-1 text-xs text-oh-muted">{{ asCase(row).currency }}</span>
              </template>
              <template #cell-actions="{ row }">
                <div class="flex flex-wrap items-center gap-1">
                  <Button v-if="can('billing.ledger.read')" size="xs" variant="ghost" @click="openCaseLedger(asCase(row))">{{ t('wp6.billing.ledger') }}</Button>
                  <Button v-if="can('billing.reconcile.write') && asCase(row).state === 'needs_review'" size="xs" variant="secondary" :disabled="cases.stale.value || cases.offline.value || evidenceState === 'unknown'" @click="openEvidence(asCase(row))">{{ t('wp6.reconciliation.attach') }}</Button>
                </div>
              </template>
            </Table>
            <CursorButtons :next="Boolean(cases.data.value?.next_cursor)" :previous="casePager.hasPrevious.value" :loading="cases.loading.value" @next="casePager.next(cases.data.value?.next_cursor)" @previous="casePager.previous" />
          </template>
        </template>

        <template v-else>
          <p v-if="access.data.value && !can('billing.ledger.read') && !access.loading.value" role="status" class="rounded-oh-lg border border-oh-default bg-oh-surface p-4 text-sm text-oh-secondary">{{ t('wp6.state.requiredGrant', { grant: 'billing.ledger.read' }) }}</p>
          <template v-if="can('billing.ledger.read')">
            <Card padding="md">
              <form class="grid grid-cols-2 items-end gap-3 md:grid-cols-5" @submit.prevent="applyLedger">
                <DateRangeFields v-model:from="ledgerDraft.from" v-model:to="ledgerDraft.to" v-model:timezone="ledgerDraft.timezone" required />
                <Input v-model="ledgerDraft.currency" :label="t('wp6.billing.currency')" placeholder="USD" :maxlength="3" mono />
                <Button type="submit" variant="primary" :disabled="!draftLedgerWindow">{{ t('wp6.filters.apply') }}</Button>
                <p v-if="!draftLedgerWindow" class="col-span-full text-sm text-oh-status-danger-fg" role="status">{{ t('wp6.filters.invalidCostWindow') }}</p>
              </form>
            </Card>
            <div class="flex flex-wrap items-center justify-between gap-2">
              <p class="text-xs text-oh-secondary">{{ t('wp6.reconciliation.immutable') }}</p>
              <Button size="sm" variant="ghost" :disabled="ledger.loading.value" @click="resetOrRetry(ledger, ledgerPager)">{{ t('wp6.state.refresh') }}</Button>
            </div>
            <div v-if="requestId" class="flex flex-wrap items-center gap-2">
              <Badge mono>{{ requestId }}</Badge>
              <Button size="xs" variant="ghost" @click="pickView('ledger')">{{ t('wp6.filters.clear') }}</Button>
            </div>
            <ReadNotice :loading="ledger.loading.value" :error="ledger.error.value" :has-data="Boolean(ledger.data.value)" :offline="ledger.offline.value" :last-synced="ledger.lastSynced.value" grant="billing.ledger.read" @retry="resetOrRetry(ledger, ledgerPager)" />
            <Table v-if="ledger.data.value" :columns="ledgerColumns" :rows="ledger.data.value.items" :empty-title="t('wp6.billing.noRecords')" :caption="t('wp6.billing.ledger')">
              <template #cell-entry="{ row }">
                <div class="grid gap-1">
                  <code class="text-xs">{{ asEntry(row).id }}</code>
                  <span class="whitespace-nowrap">{{ t(`wp6.entry.${asEntry(row).kind}`) }}</span>
                  <span v-if="asEntry(row).reverses_id" class="font-mono text-[11px] text-oh-muted">{{ t('wp6.billing.originalEntry') }} {{ asEntry(row).reverses_id }}</span>
                </div>
              </template>
              <template #cell-request="{ row }">
                <RouterLink :to="requestLocation(scope, asEntry(row).request_id, true)" class="oh-focus-ring block min-w-32 rounded-oh-xs break-all font-mono text-xs text-oh-accent-text hover:underline">{{ asEntry(row).request_id }}</RouterLink>
              </template>
              <template #cell-posted="{ row }"><span class="whitespace-nowrap font-mono text-xs tabular-nums">{{ timestamp(asEntry(row).posted_at, locale, ledgerFilters.timezone) }}</span></template>
              <template #cell-amount="{ row }"><span class="whitespace-nowrap">{{ moneyText(asEntry(row).signed_amount, locale) }}</span></template>
              <template #cell-actions="{ row }">
                <Button v-if="asEntry(row).kind === 'charge' && asEntry(row).signed_amount.currency === 'USD'" size="xs" variant="secondary" :disabled="ledger.stale.value || ledger.offline.value || adjustState === 'unknown'" @click="openAdjustment(asEntry(row))">{{ t('wp6.reconciliation.adjust') }}</Button>
              </template>
            </Table>
            <CursorButtons :next="Boolean(ledger.data.value?.next_cursor)" :previous="ledgerPager.hasPrevious.value" :loading="ledger.loading.value" @next="ledgerPager.next(ledger.data.value?.next_cursor)" @previous="ledgerPager.previous" />
          </template>
        </template>
      </template>

      <Modal
        :open="entryDialog && !stepUp.visible.value" :title="t('wp6.reconciliation.adjustTitle')" :description="t('wp6.reconciliation.immutable')"
        :close-on-backdrop="adjustState !== 'working'" :close-on-escape="adjustState !== 'working'" :hide-close-button="adjustState === 'working'"
        @update:open="entryDialog = $event"
      >
        <form v-if="activeEntry" class="space-y-3" @submit.prevent="submitAdjustment">
          <p class="rounded-oh-md border border-oh-default bg-oh-subtle px-3 py-2 text-sm">
            {{ t('wp6.billing.originalEntry') }} <code>{{ activeEntry.id }}</code>
            <span class="ml-2 font-mono tabular-nums">{{ moneyText(activeEntry.signed_amount, locale) }}</span>
          </p>
          <p v-if="adjustState === 'unknown'" role="alert" class="text-sm text-oh-status-warn-fg">{{ t('wp6.reconciliation.adjustUnknown') }}</p>
          <p v-else-if="adjustState === 'conflict'" role="alert" class="text-sm text-oh-status-warn-fg">{{ t('wp6.reconciliation.adjustConflict') }}</p>
          <p v-else-if="adjustError" role="alert" class="text-sm text-oh-status-danger-fg">{{ adjustError.message || t('wp6.state.loadFailed') }}</p>
          <Input v-model="adjustment.amount" :label="t('wp6.reconciliation.adjustAmount')" :hint="t('wp6.reconciliation.adjustHint')" placeholder="0.00000000" mono required :readonly="adjustState === 'working' || adjustState === 'unknown'" />
          <Input v-model="adjustment.note" :label="t('wp6.reconciliation.adjustNote')" :maxlength="1024" required :readonly="adjustState === 'working' || adjustState === 'unknown'" />
          <div class="flex flex-wrap items-center justify-end gap-2">
            <Button v-if="adjustState === 'unknown' || adjustState === 'conflict'" type="button" variant="secondary" @click="checkLedger">{{ t('wp6.reconciliation.verifyLedger') }}</Button>
            <Button v-else type="button" variant="ghost" :disabled="adjustState === 'working'" @click="entryDialog = false">{{ t('common.cancel') }}</Button>
            <Button type="submit" variant="primary" :loading="adjustState === 'working'" :disabled="!adjustmentValid || adjustState === 'conflict' || ledger.stale.value || ledger.offline.value">
              {{ adjustState === 'unknown' ? t('wp6.reconciliation.retrySame') : t('wp6.reconciliation.verifyAndPost') }}
            </Button>
          </div>
        </form>
      </Modal>
      <TotpStepUpDialog :controller="stepUp" />
      <Modal
        v-model:open="caseDialog" :title="t('wp6.reconciliation.attachTitle')" :close-on-backdrop="evidenceState !== 'working'"
        :close-on-escape="evidenceState !== 'working'" :hide-close-button="evidenceState === 'working'"
      >
        <form v-if="activeCase" class="space-y-3" @submit.prevent="submitEvidence">
          <p class="font-mono text-xs text-oh-secondary">{{ activeCase.id }}</p>
          <div v-if="evidenceState === 'conflict'" role="alert" class="space-y-1 rounded-oh-md border border-oh-status-warn-border bg-oh-status-warn-bg/55 p-3 text-sm">
            <p>{{ t('wp6.reconciliation.caseConflict') }}</p>
            <dl class="grid grid-cols-2 gap-1 text-xs">
              <dt>{{ t('wp6.reconciliation.formVersion') }}</dt><dd class="font-mono">{{ activeCase.version }}</dd>
              <dt>{{ t('wp6.reconciliation.currentVersion') }}</dt><dd class="font-mono">{{ evidenceError?.metadata.current_version ?? '—' }}</dd>
            </dl>
          </div>
          <p v-else-if="evidenceState === 'unknown'" role="alert" class="text-sm text-oh-status-warn-fg">{{ t('wp6.reconciliation.evidenceUnknown') }}</p>
          <p v-else-if="evidenceError" role="alert" class="text-sm text-oh-status-danger-fg">{{ evidenceError.message || t('wp6.state.loadFailed') }}</p>
          <Input v-model="evidence.id" :label="t('wp6.reconciliation.evidenceId')" :maxlength="255" mono required :readonly="evidenceState === 'working' || evidenceState === 'unknown'" />
          <Input v-model="evidence.note" :label="t('wp6.reconciliation.evidenceNote')" :maxlength="1024" :readonly="evidenceState === 'working' || evidenceState === 'unknown'" />
          <div class="flex flex-wrap justify-end gap-2">
            <Button v-if="evidenceState === 'conflict'" type="button" variant="secondary" @click="reloadCase">{{ t('wp6.reconciliation.reloadCase') }}</Button>
            <Button v-else type="button" variant="ghost" :disabled="evidenceState === 'working'" @click="caseDialog = false">{{ t('common.cancel') }}</Button>
            <Button type="submit" variant="primary" :loading="evidenceState === 'working'" :disabled="!evidenceValid || evidenceState === 'conflict' || cases.stale.value || cases.offline.value">
              {{ evidenceState === 'unknown' ? t('wp6.reconciliation.retrySame') : t('wp6.reconciliation.attach') }}
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  </AppLayout>
</template>
