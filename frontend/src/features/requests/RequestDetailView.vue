<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { Badge, Button, Card, CopyButton, EmptyState, PageHeader, StatNumber } from '@/components/ui'
import { requestLocation, requestsApi, scopeQuery } from './api'
import { dateInZone, queryText, shiftCalendar, validTimezone } from './filters'
import { displayNumber, moneyText, statusTone, timestamp } from './presentation'
import { useProjectScope } from './useProjectScope'
import { useScopedRead } from './useScopedRead'
import ReadNotice from './components/ReadNotice.vue'
import RequestWaterfall from './components/RequestWaterfall.vue'
import RouteEvidence from './components/RouteEvidence.vue'
import ScopePicker from './components/ScopePicker.vue'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const { scope, scopeKey, admin, access, choose, can } = useProjectScope()
const id = computed(() => typeof route.params.id === 'string' ? route.params.id : '')
const timezone = computed(() => {
  const requested = queryText(route.query, 'timezone')
  return validTimezone(requested) ? requested : Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
})
const requestKey = computed(() => scope.value && id.value && can('requests.read') ? JSON.stringify([scopeKey.value, id.value]) : '')
const detail = useScopedRead(() => requestKey.value, async signal => {
  const target = scope.value!
  const requestId = id.value
  const result = await requestsApi.detail(target, requestId, signal)
  if (result.id !== requestId || result.workspace_id !== target.workspaceId || result.project_id !== target.projectId) {
    throw { status: 404, reason: 'NOT_FOUND' }
  }
  return result
})
const trace = useScopedRead(
  () => detail.data.value ? requestKey.value : '',
  signal => requestsApi.explanation(scope.value!, id.value, signal),
)
const record = detail.data
const finance = computed(() => can('billing.ledger.read') && !record.value?.redactions.includes('financial_details'))
const connectionAccess = computed(() => can('connections.read'))
const unit = (kind: 'input_token' | 'output_token') => record.value?.usage.units.find(item => item.kind === kind)?.quantity ?? null
const breadcrumbs = computed(() => [
  { label: t('wp6.requests.title'), to: scope.value ? router.resolve(requestLocation(scope.value, null, admin.value)).href : undefined },
  { label: t('wp6.requests.detail') },
])
const ledgerLink = computed(() => {
  const settledAt = record.value?.timeline.find(event => event.kind === 'settled')?.at ?? record.value?.started_at
  const instant = Date.parse(settledAt ?? '')
  const day = Number.isFinite(instant) ? dateInZone(instant, timezone.value) : undefined
  return {
    path: admin.value ? '/admin/billing' : '/billing',
    query: {
      ...(scope.value ? scopeQuery(scope.value) : {}), view: 'ledger', request_id: id.value,
      currency: record.value?.billing.currency ?? undefined, timezone: timezone.value,
      from: day, to: day ? shiftCalendar(day, 1) : undefined,
    },
  }
})
async function refresh() {
  await access.refresh()
  if (!can('requests.read')) return
  await Promise.all([detail.refresh(), trace.refresh()])
}
</script>

<template>
  <AppLayout contained>
    <div class="min-w-0 space-y-5">
      <PageHeader :title="t('wp6.requests.detail')" :breadcrumbs="breadcrumbs">
        <template v-if="record" #badges>
          <Badge :tone="statusTone(record.outcome)" dot>{{ t(`wp6.states.${record.outcome}`) }}</Badge>
          <Badge :tone="statusTone(record.settlement_state)">{{ t(`wp6.states.${record.settlement_state}`) }}</Badge>
        </template>
        <template #actions>
          <ScopePicker :scope="scope" @change="choose" />
          <Button v-if="scope" size="sm" variant="secondary" :disabled="detail.loading.value || access.loading.value" @click="refresh">{{ t('wp6.state.refresh') }}</Button>
        </template>
      </PageHeader>

      <EmptyState v-if="!scope" :title="t('wp6.scope.select')" :description="t('wp6.scope.required')" />
      <template v-else>
        <ReadNotice :loading="access.loading.value" :error="access.error.value" :has-data="Boolean(access.data.value)" :offline="access.offline.value" :last-synced="access.lastSynced.value" @retry="access.refresh" />
        <p v-if="access.data.value && !can('requests.read') && !access.loading.value" class="rounded-oh-lg border border-oh-default bg-oh-surface p-4 text-sm text-oh-secondary" role="status">
          {{ t('wp6.state.requiredGrant', { grant: 'requests.read' }) }}
        </p>
        <ReadNotice :loading="detail.loading.value" :error="detail.error.value" :has-data="Boolean(record)" :offline="detail.offline.value" :last-synced="detail.lastSynced.value" grant="requests.read" @retry="refresh" />

        <template v-if="record">
          <div class="flex min-w-0 flex-wrap items-start justify-between gap-3">
            <div class="min-w-0 space-y-1.5">
              <div class="flex min-w-0 flex-wrap items-center gap-2">
                <code class="break-all rounded-oh-sm border border-oh-default bg-oh-surface px-2 py-1 font-mono text-xs text-oh-primary">{{ record.id }}</code>
                <CopyButton :value="record.id" :label="t('wp6.copy.label')" :copied-label="t('wp6.copy.copied')" :failed-label="t('wp6.copy.failed')" :aria-label="t('wp6.copy.requestId')" variant="secondary" />
              </div>
              <div class="flex flex-wrap items-center gap-x-2.5 gap-y-1 text-sm text-oh-muted">
                <strong class="break-all font-mono text-sm font-medium text-oh-primary">{{ record.requested_model || '—' }}</strong>
                <span v-if="record.protocol" class="font-mono text-xs">{{ record.protocol }}</span>
                <time class="font-mono text-xs tabular-nums" :datetime="record.started_at">{{ timestamp(record.started_at, locale, timezone) }}</time>
              </div>
            </div>
            <Badge v-if="record.data_completeness.state === 'partial'" tone="warning" dot>{{ t('wp6.requests.partialEvidence') }}</Badge>
          </div>

          <div class="grid min-w-0 grid-cols-2 gap-3 xl:grid-cols-4">
            <Card padding="sm">
              <StatNumber class="max-w-full [&_.oh-tabular]:min-w-0 [&_.oh-tabular]:break-all max-sm:[&_.oh-tabular]:text-sm" :label="t('wp6.requests.totalLatency')" :value="record.timings_ms.total" unit="ms" :unknown-label="t('wp6.requests.unobserved')" />
            </Card>
            <Card padding="sm">
              <StatNumber class="max-w-full [&_.oh-tabular]:min-w-0 [&_.oh-tabular]:break-all max-sm:[&_.oh-tabular]:text-sm" :label="t('wp6.requests.ttft')" :value="record.timings_ms.upstream_ttft" unit="ms" :unknown-label="t('wp6.requests.unobserved')" />
            </Card>
            <Card padding="sm">
              <StatNumber class="max-w-full [&_.oh-tabular]:min-w-0 [&_.oh-tabular]:break-all max-sm:[&_.oh-tabular]:text-sm" :label="t('wp6.requests.inputTokens')" :value="unit('input_token') === null ? null : displayNumber(unit('input_token'), locale)" :unknown-label="t('wp6.requests.unobserved')" />
              <p class="mt-1 text-xs text-oh-muted">{{ t('wp6.requests.outputTokens') }} <span class="font-mono tabular-nums">{{ displayNumber(unit('output_token'), locale) }}</span></p>
            </Card>
            <Card padding="sm">
              <StatNumber
                class="max-w-full [&_.oh-tabular]:min-w-0 [&_.oh-tabular]:break-all max-sm:[&_.oh-tabular]:text-sm"
                v-if="finance" :label="t('wp6.requests.charge')" :value="record.billing.platform_charge ? moneyText(record.billing.platform_charge, locale) : null"
                :unknown-label="t('wp6.requests.unobserved')" accent
              />
              <template v-else>
                <p class="text-xs font-medium text-oh-secondary">{{ t('wp6.requests.receipt') }}</p>
                <Badge class="mt-2" :tone="statusTone(record.billing.receipt_state)" dot>{{ t(`wp6.states.${record.billing.receipt_state}`) }}</Badge>
              </template>
            </Card>
          </div>

          <div class="grid min-w-0 gap-4 xl:grid-cols-[minmax(0,1.75fr)_minmax(0,1fr)]">
            <RequestWaterfall :detail="record" :connection-access="connectionAccess" :timezone="timezone" />
            <Card padding="md" class="min-w-0">
              <section class="space-y-3" aria-labelledby="wp6-route-evidence">
                <h2 id="wp6-route-evidence" class="text-base font-semibold text-oh-primary">{{ t('wp6.requests.routing') }}</h2>
                <ReadNotice :loading="trace.loading.value" :error="trace.error.value" :has-data="Boolean(trace.data.value)" :offline="trace.offline.value" :last-synced="trace.lastSynced.value" grant="requests.read" :rows="2" @retry="trace.refresh" />
                <RouteEvidence v-if="trace.data.value" :explanation="trace.data.value" :financial-access="finance" />
              </section>
            </Card>
          </div>

          <div class="grid min-w-0 gap-4 lg:grid-cols-2">
            <Card padding="md" class="min-w-0">
              <section class="space-y-3" aria-labelledby="wp6-receipt-evidence">
                <div class="flex flex-wrap items-center justify-between gap-2">
                  <h2 id="wp6-receipt-evidence" class="text-base font-semibold text-oh-primary">{{ t('wp6.requests.receiptProof') }}</h2>
                  <Badge :tone="statusTone(record.billing.receipt_state)" dot>{{ t(`wp6.states.${record.billing.receipt_state}`) }}</Badge>
                </div>
                <p v-if="!finance" class="text-sm text-oh-secondary">{{ t('wp6.state.requiredGrant', { grant: 'billing.ledger.read' }) }}</p>
                <dl v-else class="grid grid-cols-[minmax(0,0.85fr)_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">
                  <dt class="text-oh-secondary">{{ t('wp6.billing.fundingSource') }}</dt><dd class="break-all font-mono">{{ record.billing.billing_source ?? '—' }}</dd>
                  <dt class="text-oh-secondary">{{ t('wp6.requests.priceVersion') }}</dt><dd class="break-all font-mono">{{ record.billing.price_version ?? '—' }}</dd>
                  <dt class="text-oh-secondary">{{ t('wp6.requests.reserved') }}</dt><dd class="break-all font-mono tabular-nums">{{ moneyText(record.billing.reserved, locale) }}</dd>
                  <dt class="text-oh-secondary">{{ t('wp6.requests.charge') }}</dt><dd class="break-all font-mono tabular-nums">{{ moneyText(record.billing.platform_charge, locale) }}</dd>
                  <dt class="text-oh-secondary">{{ t('wp6.requests.settlementId') }}</dt><dd class="break-all font-mono">{{ record.billing.settlement_id ?? '—' }}</dd>
                  <dt class="text-oh-secondary">{{ t('wp6.requests.ledgerEntries') }}</dt><dd class="break-all font-mono">{{ record.billing.ledger_entry_ids.join(', ') || '—' }}</dd>
                </dl>
                <RouterLink v-if="finance && record.billing.ledger_entry_ids.length" :to="ledgerLink" class="oh-focus-ring inline-flex rounded-oh-xs text-sm font-medium text-oh-accent-text hover:underline">
                  {{ t('wp6.billing.openLedger') }}
                </RouterLink>
              </section>
            </Card>

            <Card v-if="record.conversion || record.diagnostics.length || record.data_completeness.missing.length" padding="md" class="min-w-0">
              <section class="space-y-2.5" aria-labelledby="wp6-diagnostics">
                <h2 id="wp6-diagnostics" class="text-base font-semibold text-oh-primary">{{ t('wp6.requests.diagnostics') }}</h2>
                <p v-if="record.conversion" class="break-all font-mono text-xs text-oh-secondary">
                  {{ record.conversion.source_protocol }} → {{ record.conversion.target_protocol }} · {{ record.conversion.mode }}
                </p>
                <ul class="space-y-2 text-xs">
                  <li v-for="(item, index) in record.conversion?.diagnostics ?? []" :key="`conversion-${index}`" class="break-all font-mono text-oh-secondary">{{ item.field }} · {{ item.reason }}</li>
                  <li v-for="(item, index) in record.diagnostics" :key="`diag-${index}`" class="break-all font-mono text-oh-secondary">{{ item.reason }}</li>
                  <li v-for="item in record.data_completeness.missing" :key="`${item.section}:${item.reason}`" class="flex flex-wrap items-center gap-2">
                    <Badge tone="unknown" size="xs">{{ item.section }}</Badge>
                    <code class="break-all text-oh-muted">{{ item.reason }}</code>
                  </li>
                </ul>
              </section>
            </Card>
          </div>
        </template>
      </template>
    </div>
  </AppLayout>
</template>
