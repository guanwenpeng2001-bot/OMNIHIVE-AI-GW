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
const isZh = computed(() => String(locale?.value || '').startsWith('zh'))
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
  ...(can('billing.reconcile.read') || view.value === 'reconciliation' ? [{ value: 'reconciliation', label: t('costLedger.reconciliation.cases') }] : []),
  ...(can('billing.ledger.read') || view.value === 'ledger' ? [{ value: 'ledger', label: t('costLedger.billing.ledger') }] : []),
])
const caseStates = computed(() => [
  { value: '', label: t('costLedger.filters.allSettlements') },
  { value: 'needs_review', label: t('costLedger.states.needs_review') },
  { value: 'resolved', label: t('costLedger.states.resolved') },
])
const caseColumns = computed<UiTableColumn[]>(() => [
  { key: 'case', label: t('costLedger.reconciliation.case') },
  { key: 'reason', label: t('costLedger.reconciliation.reason') },
  { key: 'state', label: t('costLedger.requests.settlement') },
  { key: 'difference', label: t('costLedger.reconciliation.difference'), mono: true },
  { key: 'actions', label: t('costLedger.reconciliation.actions') },
])
const ledgerColumns = computed<UiTableColumn[]>(() => [
  { key: 'entry', label: t('costLedger.billing.entry') },
  { key: 'request', label: t('costLedger.requests.request') },
  { key: 'posted', label: t('costLedger.billing.postedAt') },
  { key: 'amount', label: t('costLedger.billing.signedAmount'), mono: true },
  ...(can('billing.adjust') ? [{ key: 'actions', label: t('costLedger.reconciliation.actions') }] : []),
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

const paymentEvidenceDialog = ref(false)
const paymentAdjustmentId = ref('')
const paymentEventId = ref('')
const paymentEvidenceItems = ref<Schema['PaymentAdjustmentEvidence'][]>([])
const paymentEvidenceLoading = ref(false)
const paymentEvidenceLinkState = ref<Mutation>('idle')
const paymentEvidenceError = ref<NormalizedApiError | null>(null)
const paymentEvidenceLinkedNotice = ref('')
let paymentEvidenceController: AbortController | null = null

const validUuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
const canQueryPaymentEvidence = computed(
  () => can('billing.read') && validUuidPattern.test(paymentAdjustmentId.value.trim()),
)
const canLinkPaymentEvidence = computed(
  () =>
    can('billing.adjust') &&
    validUuidPattern.test(paymentAdjustmentId.value.trim()) &&
    /^[1-9]\d*$/.test(paymentEventId.value.trim()),
)

function openPaymentEvidenceModal(initialAdjustmentId = '') {
  paymentAdjustmentId.value = initialAdjustmentId
  paymentEventId.value = ''
  paymentEvidenceItems.value = []
  paymentEvidenceError.value = null
  paymentEvidenceLinkedNotice.value = ''
  paymentEvidenceLinkState.value = 'idle'
  paymentEvidenceDialog.value = true
  if (initialAdjustmentId && validUuidPattern.test(initialAdjustmentId)) {
    void fetchPaymentEvidence()
  }
}

async function fetchPaymentEvidence() {
  const target = scope.value
  const adjId = paymentAdjustmentId.value.trim()
  if (!target || !can('billing.read') || !validUuidPattern.test(adjId) || paymentEvidenceLoading.value) {
    return
  }
  paymentEvidenceController?.abort()
  const controller = new AbortController()
  paymentEvidenceController = controller
  paymentEvidenceLoading.value = true
  paymentEvidenceError.value = null
  try {
    const res = await billingApi.listPaymentEvidence(target, adjId, controller.signal)
    if (controller.signal.aborted) return
    paymentEvidenceItems.value = res.items ?? []
  } catch (cause) {
    if (controller.signal.aborted) return
    paymentEvidenceError.value = normalizeApiError(cause)
  } finally {
    if (paymentEvidenceController === controller) {
      paymentEvidenceLoading.value = false
    }
  }
}

async function submitPaymentEvidenceLink() {
  const target = scope.value
  const adjId = paymentAdjustmentId.value.trim()
  const evId = paymentEventId.value.trim()
  if (!target || !canLinkPaymentEvidence.value || paymentEvidenceLinkState.value === 'working') {
    return
  }
  paymentEvidenceController?.abort()
  const controller = new AbortController()
  paymentEvidenceController = controller
  paymentEvidenceLinkState.value = 'working'
  paymentEvidenceError.value = null
  paymentEvidenceLinkedNotice.value = ''
  try {
    await stepUp.run(() => {
      if (controller.signal.aborted || !can('billing.adjust')) {
        throw { status: 403, reason: 'ACTION_DENIED' }
      }
      return billingApi.linkPaymentEvidence(target, adjId, evId, controller.signal)
    })
    if (controller.signal.aborted) return
    paymentEvidenceLinkState.value = 'idle'
    paymentEventId.value = ''
    paymentEvidenceLinkedNotice.value = isZh.value
      ? `已将钱包付款事件 #${evId} 关联至调账单 ${adjId}`
      : `Linked wallet payment event #${evId} to adjustment ${adjId}`
    await fetchPaymentEvidence()
  } catch (cause) {
    if (controller.signal.aborted) return
    if (isStepUpCancelled(cause)) {
      paymentEvidenceLinkState.value = 'idle'
      return
    }
    const issue = classify(cause)
    paymentEvidenceLinkState.value = issue.state
    paymentEvidenceError.value = issue.error
  }
}

function clearCommands() {
  adjustmentRequest?.abort()
  evidenceRequest?.abort()
  paymentEvidenceController?.abort()
  stepUp.onCancel()
  activeEntry.value = null
  activeCase.value = null
  entryDialog.value = false
  caseDialog.value = false
  paymentEvidenceDialog.value = false
  adjustmentKey = null
  evidenceKey = null
  Object.assign(adjustment, { amount: '', note: '' })
  Object.assign(evidence, { id: '', note: '' })
  paymentAdjustmentId.value = ''
  paymentEventId.value = ''
  paymentEvidenceItems.value = []
  paymentEvidenceError.value = null
  paymentEvidenceLinkedNotice.value = ''
  paymentEvidenceLinkState.value = 'idle'
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
      <PageHeader :title="t('costLedger.reconciliation.title')" :description="t('costLedger.reconciliation.description')">
        <template #actions><ScopePicker :scope="scope" @change="choose" /></template>
      </PageHeader>
      <EmptyState v-if="!scope" :title="t('costLedger.scope.select')" :description="t('costLedger.scope.required')" />
      <template v-else>
        <ReadNotice :loading="access.loading.value" :error="access.error.value" :has-data="Boolean(access.data.value)" :offline="access.offline.value" :last-synced="access.lastSynced.value" @retry="access.refresh" />
        <Tabs v-if="access.data.value" :model-value="view" :items="tabs" :aria-label="t('costLedger.reconciliation.title')" @update:model-value="pickView" />

        <p v-if="adjustState === 'unknown' && !entryDialog" role="status" class="rounded-oh-md border border-oh-status-warn-border bg-oh-status-warn-bg/55 p-3 text-sm">
          {{ t('costLedger.reconciliation.adjustUnknown') }}
          <Button size="sm" variant="secondary" class="ml-1" @click="entryDialog = true">{{ t('costLedger.reconciliation.openPending') }}</Button>
        </p>
        <div v-if="posted" role="status" class="flex flex-wrap items-center justify-between gap-2 rounded-oh-md border border-oh-status-ok-border bg-oh-status-ok-bg px-3 py-2.5 text-sm">
          <span>{{ t('costLedger.reconciliation.posted', { entry: posted.ledger_entry_id, project: scope.projectId }) }}</span>
          <div class="flex flex-wrap items-center gap-1">
            <CopyButton :value="posted.ledger_entry_id" :label="t('costLedger.copy.label')" :copied-label="t('costLedger.copy.copied')" :failed-label="t('costLedger.copy.failed')" size="xs" />
            <Button
              v-if="can('billing.read')"
              size="sm"
              variant="secondary"
              @click="openPaymentEvidenceModal(posted.id)"
            >
              {{ isZh ? '核验/关联付款凭证' : 'Payment Evidence' }}
            </Button>
            <Button size="sm" variant="secondary" @click="openPostedLedger">{{ t('costLedger.billing.openLedger') }}</Button>
          </div>
        </div>
        <div v-if="resolvedOperation" role="status" class="flex flex-wrap items-center gap-2 rounded-oh-md border border-oh-status-ok-border bg-oh-status-ok-bg px-3 py-2.5 text-sm">
          {{ t('costLedger.reconciliation.evidenceSaved') }} <code class="break-all">{{ resolvedOperation }}</code>
          <CopyButton :value="resolvedOperation" :label="t('costLedger.copy.label')" :copied-label="t('costLedger.copy.copied')" :failed-label="t('costLedger.copy.failed')" size="xs" />
        </div>

        <template v-if="view === 'reconciliation'">
          <p v-if="access.data.value && !can('billing.reconcile.read') && !access.loading.value" role="status" class="rounded-oh-lg border border-oh-default bg-oh-surface p-4 text-sm text-oh-secondary">{{ t('costLedger.state.requiredGrant', { grant: 'billing.reconcile.read' }) }}</p>
          <template v-if="can('billing.reconcile.read')">
            <Card padding="md">
              <form class="flex flex-wrap items-end gap-3" @submit.prevent="applyCases">
                <Select v-model="caseDraft.state" :label="t('costLedger.requests.settlement')" :options="caseStates" />
                <Input v-model="caseDraft.currency" :label="t('costLedger.billing.currency')" :placeholder="t('costLedger.filters.allCurrencies')" :maxlength="3" mono />
                <Button type="submit" variant="primary" :disabled="Boolean(caseDraft.currency && !currencyCode(caseDraft.currency))">{{ t('costLedger.filters.apply') }}</Button>
                <Button type="button" variant="ghost" :disabled="cases.loading.value" @click="resetOrRetry(cases, casePager)">{{ t('costLedger.state.refresh') }}</Button>
              </form>
            </Card>
            <p class="text-xs text-oh-muted">{{ t('costLedger.reconciliation.separateCurrencies') }}</p>
            <p v-if="evidenceState === 'unknown' && !caseDialog" role="status" class="rounded-oh-md border border-oh-status-warn-border bg-oh-status-warn-bg/55 p-3 text-sm">
              {{ t('costLedger.reconciliation.evidenceUnknown') }}
              <Button size="sm" variant="secondary" class="ml-1" @click="caseDialog = true">{{ t('costLedger.reconciliation.openPending') }}</Button>
            </p>
            <ReadNotice :loading="cases.loading.value" :error="cases.error.value" :has-data="Boolean(cases.data.value)" :offline="cases.offline.value" :last-synced="cases.lastSynced.value" grant="billing.reconcile.read" @retry="resetOrRetry(cases, casePager)" />
            <Table
              v-if="cases.data.value" :columns="caseColumns" :rows="cases.data.value.items" :caption="t('costLedger.reconciliation.cases')"
              :filtered="Boolean(state || caseCurrency)" :empty-title="t('costLedger.reconciliation.empty')" :filtered-empty-title="t('costLedger.state.noMatches')" :filtered-empty-description="t('costLedger.state.noMatchesDescription')"
              @clear-filters="caseDraft.state = ''; caseDraft.currency = ''; applyCases()"
            >
              <template #cell-case="{ row }">
                <div class="grid min-w-32 gap-1 font-mono text-xs">
                  <span>{{ asCase(row).id }}</span>
                  <RouterLink :to="requestLocation(scope, asCase(row).request_id, true)" class="oh-focus-ring rounded-oh-xs break-all text-oh-accent-text hover:underline">{{ asCase(row).request_id }}</RouterLink>
                </div>
              </template>
              <template #cell-reason="{ row }"><code class="text-xs text-oh-secondary">{{ asCase(row).reason }}</code></template>
              <template #cell-state="{ row }"><Badge :tone="statusTone(asCase(row).state)" dot>{{ t(`costLedger.states.${asCase(row).state}`) }}</Badge></template>
              <template #cell-difference="{ row }">
                <span class="whitespace-nowrap">{{ moneyText(asCase(row).difference, locale) }}</span>
                <span v-if="!asCase(row).difference" class="ml-1 text-xs text-oh-muted">{{ asCase(row).currency }}</span>
              </template>
              <template #cell-actions="{ row }">
                <div class="flex flex-wrap items-center gap-1">
                  <Button v-if="can('billing.ledger.read')" size="xs" variant="ghost" @click="openCaseLedger(asCase(row))">{{ t('costLedger.billing.ledger') }}</Button>
                  <Button v-if="can('billing.reconcile.write') && asCase(row).state === 'needs_review'" size="xs" variant="secondary" :disabled="cases.stale.value || cases.offline.value || evidenceState === 'unknown'" @click="openEvidence(asCase(row))">{{ t('costLedger.reconciliation.attach') }}</Button>
                </div>
              </template>
            </Table>
            <CursorButtons :next="Boolean(cases.data.value?.next_cursor)" :previous="casePager.hasPrevious.value" :loading="cases.loading.value" @next="casePager.next(cases.data.value?.next_cursor)" @previous="casePager.previous" />
          </template>
        </template>

        <template v-else>
          <p v-if="access.data.value && !can('billing.ledger.read') && !access.loading.value" role="status" class="rounded-oh-lg border border-oh-default bg-oh-surface p-4 text-sm text-oh-secondary">{{ t('costLedger.state.requiredGrant', { grant: 'billing.ledger.read' }) }}</p>
          <template v-if="can('billing.ledger.read')">
            <Card padding="md">
              <form class="grid grid-cols-1 items-end gap-3 sm:grid-cols-2 md:grid-cols-5" @submit.prevent="applyLedger">
                <DateRangeFields v-model:from="ledgerDraft.from" v-model:to="ledgerDraft.to" v-model:timezone="ledgerDraft.timezone" required />
                <Input v-model="ledgerDraft.currency" :label="t('costLedger.billing.currency')" placeholder="USD" :maxlength="3" mono />
                <Button type="submit" variant="primary" :disabled="!draftLedgerWindow">{{ t('costLedger.filters.apply') }}</Button>
                <p v-if="!draftLedgerWindow" class="col-span-full text-sm text-oh-status-danger-fg" role="status">{{ t('costLedger.filters.invalidCostWindow') }}</p>
              </form>
            </Card>
            <div class="flex flex-wrap items-center justify-between gap-2">
              <p class="text-xs text-oh-secondary">{{ t('costLedger.reconciliation.immutable') }}</p>
              <div class="flex flex-wrap items-center gap-2">
                <Button
                  v-if="can('billing.read')"
                  data-testid="open-payment-evidence-btn"
                  size="sm"
                  variant="secondary"
                  @click="openPaymentEvidenceModal(posted?.id || '')"
                >
                  {{ isZh ? '调账付款凭证核验' : 'Payment Evidence' }}
                </Button>
                <Button size="sm" variant="ghost" :disabled="ledger.loading.value" @click="resetOrRetry(ledger, ledgerPager)">{{ t('costLedger.state.refresh') }}</Button>
              </div>
            </div>
            <div v-if="requestId" class="flex flex-wrap items-center gap-2">
              <Badge mono>{{ requestId }}</Badge>
              <Button size="xs" variant="ghost" @click="pickView('ledger')">{{ t('costLedger.filters.clear') }}</Button>
            </div>
            <ReadNotice :loading="ledger.loading.value" :error="ledger.error.value" :has-data="Boolean(ledger.data.value)" :offline="ledger.offline.value" :last-synced="ledger.lastSynced.value" grant="billing.ledger.read" @retry="resetOrRetry(ledger, ledgerPager)" />
            <Table v-if="ledger.data.value" :columns="ledgerColumns" :rows="ledger.data.value.items" :empty-title="t('costLedger.billing.noRecords')" :caption="t('costLedger.billing.ledger')">
              <template #cell-entry="{ row }">
                <div class="grid gap-1">
                  <code class="text-xs">{{ asEntry(row).id }}</code>
                  <span class="whitespace-nowrap">{{ t(`costLedger.entry.${asEntry(row).kind}`) }}</span>
                  <span v-if="asEntry(row).reverses_id" class="font-mono text-[11px] text-oh-muted">{{ t('costLedger.billing.originalEntry') }} {{ asEntry(row).reverses_id }}</span>
                </div>
              </template>
              <template #cell-request="{ row }">
                <RouterLink :to="requestLocation(scope, asEntry(row).request_id, true)" class="oh-focus-ring block min-w-32 rounded-oh-xs break-all font-mono text-xs text-oh-accent-text hover:underline">{{ asEntry(row).request_id }}</RouterLink>
              </template>
              <template #cell-posted="{ row }"><span class="whitespace-nowrap font-mono text-xs tabular-nums">{{ timestamp(asEntry(row).posted_at, locale, ledgerFilters.timezone) }}</span></template>
              <template #cell-amount="{ row }"><span class="whitespace-nowrap">{{ moneyText(asEntry(row).signed_amount, locale) }}</span></template>
              <template #cell-actions="{ row }">
                <Button v-if="asEntry(row).kind === 'charge' && asEntry(row).signed_amount.currency === 'USD'" size="xs" variant="secondary" :disabled="ledger.stale.value || ledger.offline.value || adjustState === 'unknown'" @click="openAdjustment(asEntry(row))">{{ t('costLedger.reconciliation.adjust') }}</Button>
              </template>
            </Table>
            <CursorButtons :next="Boolean(ledger.data.value?.next_cursor)" :previous="ledgerPager.hasPrevious.value" :loading="ledger.loading.value" @next="ledgerPager.next(ledger.data.value?.next_cursor)" @previous="ledgerPager.previous" />
          </template>
        </template>
      </template>

      <Modal
        :open="entryDialog && !stepUp.visible.value" :title="t('costLedger.reconciliation.adjustTitle')" :description="t('costLedger.reconciliation.immutable')"
        :close-on-backdrop="adjustState !== 'working'" :close-on-escape="adjustState !== 'working'" :hide-close-button="adjustState === 'working'"
        @update:open="entryDialog = $event"
      >
        <form v-if="activeEntry" class="space-y-3" @submit.prevent="submitAdjustment">
          <p class="rounded-oh-md border border-oh-default bg-oh-subtle px-3 py-2 text-sm">
            {{ t('costLedger.billing.originalEntry') }} <code>{{ activeEntry.id }}</code>
            <span class="ml-2 font-mono tabular-nums">{{ moneyText(activeEntry.signed_amount, locale) }}</span>
          </p>
          <p v-if="adjustState === 'unknown'" role="alert" class="text-sm text-oh-status-warn-fg">{{ t('costLedger.reconciliation.adjustUnknown') }}</p>
          <p v-else-if="adjustState === 'conflict'" role="alert" class="text-sm text-oh-status-warn-fg">{{ t('costLedger.reconciliation.adjustConflict') }}</p>
          <p v-else-if="adjustError" role="alert" class="text-sm text-oh-status-danger-fg">{{ adjustError.message || t('costLedger.state.loadFailed') }}</p>
          <Input v-model="adjustment.amount" :label="t('costLedger.reconciliation.adjustAmount')" :hint="t('costLedger.reconciliation.adjustHint')" placeholder="0.00000000" mono required :readonly="adjustState === 'working' || adjustState === 'unknown'" />
          <Input v-model="adjustment.note" :label="t('costLedger.reconciliation.adjustNote')" :maxlength="1024" required :readonly="adjustState === 'working' || adjustState === 'unknown'" />
          <div class="flex flex-wrap items-center justify-end gap-2">
            <Button v-if="adjustState === 'unknown' || adjustState === 'conflict'" type="button" variant="secondary" @click="checkLedger">{{ t('costLedger.reconciliation.verifyLedger') }}</Button>
            <Button v-else type="button" variant="ghost" :disabled="adjustState === 'working'" @click="entryDialog = false">{{ t('common.cancel') }}</Button>
            <Button type="submit" variant="primary" :loading="adjustState === 'working'" :disabled="!adjustmentValid || adjustState === 'conflict' || ledger.stale.value || ledger.offline.value">
              {{ adjustState === 'unknown' ? t('costLedger.reconciliation.retrySame') : t('costLedger.reconciliation.verifyAndPost') }}
            </Button>
          </div>
        </form>
      </Modal>
      <TotpStepUpDialog :controller="stepUp" />
      <Modal
        v-model:open="caseDialog" :title="t('costLedger.reconciliation.attachTitle')" :close-on-backdrop="evidenceState !== 'working'"
        :close-on-escape="evidenceState !== 'working'" :hide-close-button="evidenceState === 'working'"
      >
        <form v-if="activeCase" class="space-y-3" @submit.prevent="submitEvidence">
          <p class="font-mono text-xs text-oh-secondary">{{ activeCase.id }}</p>
          <div v-if="evidenceState === 'conflict'" role="alert" class="space-y-1 rounded-oh-md border border-oh-status-warn-border bg-oh-status-warn-bg/55 p-3 text-sm">
            <p>{{ t('costLedger.reconciliation.caseConflict') }}</p>
            <dl class="grid grid-cols-2 gap-1 text-xs">
              <dt>{{ t('costLedger.reconciliation.formVersion') }}</dt><dd class="font-mono">{{ activeCase.version }}</dd>
              <dt>{{ t('costLedger.reconciliation.currentVersion') }}</dt><dd class="font-mono">{{ evidenceError?.metadata.current_version ?? '—' }}</dd>
            </dl>
          </div>
          <p v-else-if="evidenceState === 'unknown'" role="alert" class="text-sm text-oh-status-warn-fg">{{ t('costLedger.reconciliation.evidenceUnknown') }}</p>
          <p v-else-if="evidenceError" role="alert" class="text-sm text-oh-status-danger-fg">{{ evidenceError.message || t('costLedger.state.loadFailed') }}</p>
          <Input v-model="evidence.id" :label="t('costLedger.reconciliation.evidenceId')" :maxlength="255" mono required :readonly="evidenceState === 'working' || evidenceState === 'unknown'" />
          <Input v-model="evidence.note" :label="t('costLedger.reconciliation.evidenceNote')" :maxlength="1024" :readonly="evidenceState === 'working' || evidenceState === 'unknown'" />
          <div class="flex flex-wrap justify-end gap-2">
            <Button v-if="evidenceState === 'conflict'" type="button" variant="secondary" @click="reloadCase">{{ t('costLedger.reconciliation.reloadCase') }}</Button>
            <Button v-else type="button" variant="ghost" :disabled="evidenceState === 'working'" @click="caseDialog = false">{{ t('common.cancel') }}</Button>
            <Button type="submit" variant="primary" :loading="evidenceState === 'working'" :disabled="!evidenceValid || evidenceState === 'conflict' || cases.stale.value || cases.offline.value">
              {{ evidenceState === 'unknown' ? t('costLedger.reconciliation.retrySame') : t('costLedger.reconciliation.attach') }}
            </Button>
          </div>
        </form>
      </Modal>
      <Modal
        :open="paymentEvidenceDialog && !stepUp.visible.value"
        :title="isZh ? '调账单付款凭证核验与关联' : 'Adjustment Payment Evidence'"
        :description="
          isZh
            ? '查看项目调账单已关联的同付款人权益与扣款证据，或在二次身份验证后绑定付款流水事件。'
            : 'Inspect reviewed entitlement links for a project adjustment or attach same-payer wallet evidence with step-up verification.'
        "
        :close-on-backdrop="paymentEvidenceLinkState !== 'working'"
        :close-on-escape="paymentEvidenceLinkState !== 'working'"
        :hide-close-button="paymentEvidenceLinkState === 'working'"
        @update:open="paymentEvidenceDialog = $event"
      >
        <div class="space-y-4">
          <form class="flex flex-wrap items-end gap-2" @submit.prevent="fetchPaymentEvidence">
            <div class="min-w-0 flex-1">
              <Input
                v-model="paymentAdjustmentId"
                :label="isZh ? '调账单 UUID' : 'Adjustment UUID'"
                placeholder="00000000-0000-0000-0000-000000000000"
                mono
                required
              />
            </div>
            <Button
              type="submit"
              variant="secondary"
              :loading="paymentEvidenceLoading"
              :disabled="!canQueryPaymentEvidence"
            >
              {{ isZh ? '查询凭证' : 'Load Evidence' }}
            </Button>
          </form>

          <p
            v-if="paymentEvidenceLinkedNotice"
            role="status"
            class="rounded-oh-md border border-oh-status-ok-border bg-oh-status-ok-bg px-3 py-2 text-xs text-oh-primary"
          >
            {{ paymentEvidenceLinkedNotice }}
          </p>
          <p
            v-if="paymentEvidenceError"
            role="alert"
            class="text-sm text-oh-status-danger-fg"
          >
            {{ paymentEvidenceError.message || t('costLedger.state.loadFailed') }}
          </p>

          <div v-if="paymentEvidenceItems.length > 0" class="space-y-2">
            <p class="text-xs font-semibold text-oh-secondary">
              {{ isZh ? `已关联凭证记录 (${paymentEvidenceItems.length})` : `Linked Evidence (${paymentEvidenceItems.length})` }}
            </p>
            <ul class="max-h-52 divide-y divide-oh-subtle overflow-auto rounded-oh-md border border-oh-default px-3">
              <li
                v-for="ev in paymentEvidenceItems"
                :key="ev.id"
                class="flex flex-wrap items-center justify-between gap-2 py-2 text-xs"
              >
                <div class="space-y-0.5 font-mono">
                  <p class="font-semibold text-oh-primary">#{{ ev.id }} · {{ ev.kind }}</p>
                  <p class="text-oh-muted">{{ t(`costLedger.funding.${ev.billing_source}`) }}</p>
                </div>
                <div class="text-right font-mono tabular-nums">
                  <p class="text-oh-primary">{{ moneyText(ev.platform_charge ?? ev.entitlement_consumption, locale) }}</p>
                </div>
              </li>
            </ul>
          </div>
          <p
            v-else-if="!paymentEvidenceLoading && validUuidPattern.test(paymentAdjustmentId.trim())"
            class="text-xs text-oh-muted"
          >
            {{ isZh ? '当前调账单暂无已关联的付款凭证记录。' : 'No linked payment evidence found for this adjustment yet.' }}
          </p>

          <form
            v-if="can('billing.adjust')"
            class="space-y-3 border-t border-oh-subtle pt-3"
            @submit.prevent="submitPaymentEvidenceLink"
          >
            <Input
              v-model="paymentEventId"
              :label="isZh ? '关联同付款人钱包事件 ID' : 'Attach Same-Payer Wallet Event ID'"
              :placeholder="isZh ? '输入正整数事件 ID' : 'Positive integer event ID'"
              mono
              required
              :readonly="paymentEvidenceLinkState === 'working'"
            />
            <div class="flex flex-wrap justify-end gap-2">
              <Button
                type="button"
                variant="ghost"
                :disabled="paymentEvidenceLinkState === 'working'"
                @click="paymentEvidenceDialog = false"
              >
                {{ t('common.cancel') }}
              </Button>
              <Button
                type="submit"
                variant="primary"
                :loading="paymentEvidenceLinkState === 'working'"
                :disabled="!canLinkPaymentEvidence"
              >
                {{ isZh ? '验证会话并绑定凭证' : 'Verify & Link Evidence' }}
              </Button>
            </div>
          </form>
        </div>
      </Modal>
    </div>
  </AppLayout>
</template>
