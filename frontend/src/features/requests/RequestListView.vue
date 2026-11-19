<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { Badge, Button, Card, EmptyState, ErrorState, Input, PageHeader, Select, type UiTableColumn, Table } from '@/components/ui'
import { opaqueNumericId, queryParams, requestLocation, requestsApi, scopeQuery, type Schema } from './api'
import { outcomes, requestFilters, timeWindow, windowQuery } from './filters'
import { displayNumber, moneyText, statusTone, timestamp } from './presentation'
import { useProjectScope } from './useProjectScope'
import { useScopedRead } from './useScopedRead'
import { useCursor } from './useCursor'
import DateRangeFields from './components/DateRangeFields.vue'
import ScopePicker from './components/ScopePicker.vue'
import ReadNotice from './components/ReadNotice.vue'
import CursorButtons from './components/CursorButtons.vue'

const { t, locale } = useI18n()
const router = useRouter()
const route = useRoute()
const { scope, scopeKey, admin, access, choose, can } = useProjectScope()
const applied = computed(() => requestFilters(route.query))
const draft = reactive({ ...applied.value })
watch(applied, (filters) => Object.assign(draft, filters))
const filterIssue = computed(() => {
  if (!timeWindow(draft)) return t('wp6.filters.invalidWindow')
  if (draft.key_id && !opaqueNumericId(draft.key_id)) return t('wp6.filters.invalidKey')
  return ''
})
const active = computed(() => Boolean(applied.value.from || applied.value.to || applied.value.model_id || applied.value.key_id || applied.value.outcome))
const outcomeOptions = computed(() => [
  { value: '', label: t('wp6.filters.allOutcomes') },
  ...outcomes.map(value => ({ value, label: t(`wp6.states.${value}`) })),
])
const window = computed(() => timeWindow(applied.value))
const filterKey = computed(() => JSON.stringify([scopeKey.value, applied.value]))
const pager = useCursor(() => filterKey.value)
const list = useScopedRead(
  () => scope.value && window.value && can('requests.read') ? JSON.stringify([filterKey.value, pager.cursor.value]) : '',
  signal => requestsApi.list(scope.value!, queryParams({
    ...window.value, outcome: applied.value.outcome, model_id: applied.value.model_id,
    key_id: applied.value.key_id, cursor: pager.cursor.value,
  }), signal),
)
const keys = useScopedRead(() => can('keys.read') ? scopeKey.value : '', signal => requestsApi.keys(scope.value!, signal))
const rows = computed(() => list.data.value?.items ?? [])
const attempts = computed(() => {
  const observed = rows.value.flatMap(row => row.attempt_count === null ? [] : [row.attempt_count])
  return observed.length ? observed.reduce((sum, count) => sum + BigInt(count), 0n).toString() : null
})
const incompleteAttempts = computed(() => rows.value.some(row => row.attempt_count === null))
const appliedChips = computed(() => [
  applied.value.from && `${t('wp6.filters.from')} ${applied.value.from}`,
  applied.value.to && `${t('wp6.filters.to')} ${applied.value.to}`,
  applied.value.model_id,
  applied.value.key_id && `${t('wp6.filters.key')} ${applied.value.key_id}`,
  applied.value.outcome && t(`wp6.states.${applied.value.outcome}`),
].filter(Boolean))
const columns = computed<UiTableColumn[]>(() => [
  { key: 'request', label: t('wp6.requests.request') },
  { key: 'state', label: t('wp6.requests.outcome') },
  { key: 'at', label: t('wp6.requests.started'), minWidth: '11rem' },
  { key: 'attempts', label: t('wp6.requests.attempts'), mono: true },
  { key: 'receipt', label: t('wp6.requests.settlement') },
  ...(can('billing.ledger.read') ? [{ key: 'charge', label: t('wp6.requests.charge'), mono: true }] : []),
])
function link(id: string) {
  return requestLocation(scope.value!, id, admin.value)
}
function retry() {
  if (list.error.value?.status === 410 && pager.cursor.value) pager.reset()
  else void list.refresh()
}
function filters(clear = false) {
  if (!clear && filterIssue.value) return
  void router.replace({
    query: {
      ...(scope.value ? scopeQuery(scope.value) : {}),
      ...(!clear ? {
        ...windowQuery(draft), outcome: draft.outcome || undefined,
        model_id: draft.model_id.trim() || undefined, key_id: draft.key_id.trim() || undefined,
      } : {}),
    },
  })
}
function rowOf(row: Record<string, unknown>) {
  return row as Schema['RequestsSummary']
}
</script>

<template>
  <AppLayout contained>
    <div class="mx-auto min-w-0 space-y-5">
      <PageHeader :title="t('wp6.requests.title')" :description="t('wp6.requests.description')">
        <template #actions><ScopePicker :scope="scope" @change="choose" /></template>
      </PageHeader>

      <EmptyState v-if="!scope" :title="t('wp6.scope.select')" :description="t('wp6.scope.required')" />
      <template v-else>
        <ReadNotice
          :loading="access.loading.value" :error="access.error.value" :has-data="Boolean(access.data.value)"
          :offline="access.offline.value" :last-synced="access.lastSynced.value" @retry="access.refresh"
        />
        <p v-if="access.data.value && !can('requests.read') && !access.loading.value" class="rounded-oh-lg border border-oh-default bg-oh-surface p-4 text-sm text-oh-secondary" role="status">
          {{ t('wp6.state.requiredGrant', { grant: 'requests.read' }) }}
        </p>
        <template v-if="can('requests.read')">
          <Card padding="md">
            <form class="grid grid-cols-2 items-end gap-3 md:grid-cols-3 2xl:grid-cols-7" @submit.prevent="filters()">
              <DateRangeFields v-model:from="draft.from" v-model:to="draft.to" v-model:timezone="draft.timezone" />
              <Select v-model="draft.outcome" :label="t('wp6.requests.outcome')" :options="outcomeOptions" />
              <Input v-model="draft.model_id" :label="t('wp6.filters.model')" :placeholder="t('wp6.filters.anyModel')" :maxlength="255" mono />
              <label class="grid min-w-0 gap-1.5 text-xs font-medium text-oh-secondary">
                {{ t('wp6.filters.key') }}
                <input
                  v-model.trim="draft.key_id" list="wp6-request-keys" inputmode="numeric" maxlength="19"
                  :placeholder="t('wp6.filters.anyKey')"
                  class="oh-focus-ring h-9 w-full min-w-0 rounded-oh-md border border-oh-default bg-oh-surface px-2.5 font-mono text-base text-oh-primary sm:text-sm"
                />
                <datalist id="wp6-request-keys">
                  <option v-for="key in keys.data.value?.items ?? []" :key="key.id" :value="key.id">{{ key.name }}</option>
                </datalist>
              </label>
              <div class="col-span-2 flex flex-wrap items-center gap-2 md:col-span-3 2xl:col-span-1">
                <Button type="submit" variant="primary" :disabled="Boolean(filterIssue)">{{ t('wp6.filters.apply') }}</Button>
                <Button v-if="active" type="button" variant="ghost" @click="filters(true)">{{ t('wp6.filters.clear') }}</Button>
              </div>
              <p v-if="filterIssue" role="status" class="col-span-full text-sm text-oh-status-danger-fg">{{ filterIssue }}</p>
            </form>
          </Card>
          <div class="flex flex-wrap items-center justify-between gap-2 text-xs">
            <div v-if="list.data.value" class="flex flex-wrap gap-x-4 gap-y-1 text-oh-secondary">
              <span>{{ t('wp6.requests.pageRequests') }} <strong class="font-mono tabular-nums text-oh-primary">{{ displayNumber(rows.length, locale) }}</strong></span>
              <span>{{ t(incompleteAttempts ? 'wp6.requests.observedPageAttempts' : 'wp6.requests.pageAttempts') }} <strong class="font-mono tabular-nums text-oh-primary">{{ displayNumber(attempts, locale) }}</strong></span>
            </div>
            <Button variant="ghost" size="sm" :disabled="list.loading.value" @click="retry">{{ t('wp6.state.refresh') }}</Button>
          </div>
          <div v-if="appliedChips.length" class="flex min-w-0 flex-wrap items-center gap-1.5">
            <Badge v-for="(chip, index) in appliedChips" :key="index" size="xs">{{ chip }}</Badge>
          </div>
          <ReadNotice
            :loading="list.loading.value" :error="list.error.value" :has-data="Boolean(list.data.value)"
            :offline="list.offline.value" :last-synced="list.lastSynced.value" grant="requests.read" @retry="retry"
          />
          <ErrorState v-if="!window" :title="t('wp6.filters.invalidWindow')" :retryable="false" />
          <Table
            v-if="list.data.value"
            :columns="columns" :rows="rows" :caption="t('wp6.requests.title')"
            :filtered="active" :empty-title="t('wp6.requests.empty')"
            :empty-description="t('wp6.requests.emptyDescription')"
            :filtered-empty-title="t('wp6.state.noMatches')" :filtered-empty-description="t('wp6.state.noMatchesDescription')"
            @clear-filters="filters(true)"
          >
            <template #cell-request="{ row }">
              <div class="grid min-w-36 gap-1">
                <RouterLink :to="link(rowOf(row).id)" class="oh-focus-ring rounded-oh-xs break-all font-mono text-xs font-medium text-oh-accent-text hover:underline">
                  {{ rowOf(row).requested_model || rowOf(row).id }}
                </RouterLink>
                <span class="font-mono text-[11px] text-oh-muted">{{ rowOf(row).protocol || '—' }}</span>
              </div>
            </template>
            <template #cell-state="{ row }">
              <Badge :tone="statusTone(rowOf(row).outcome)" dot>{{ t(`wp6.states.${rowOf(row).outcome}`) }}</Badge>
            </template>
            <template #cell-at="{ row }">
              <time :datetime="rowOf(row).started_at" class="font-mono text-xs tabular-nums">{{ timestamp(rowOf(row).started_at, locale, applied.timezone) }}</time>
            </template>
            <template #cell-attempts="{ row }">{{ displayNumber(rowOf(row).attempt_count, locale) }}</template>
            <template #cell-receipt="{ row }"><Badge :tone="statusTone(rowOf(row).settlement_state)">{{ t(`wp6.states.${rowOf(row).settlement_state}`) }}</Badge></template>
            <template #cell-charge="{ row }">{{ moneyText(rowOf(row).charged_amount, locale) }}</template>
          </Table>
          <CursorButtons
            :next="Boolean(list.data.value?.next_cursor)" :previous="pager.hasPrevious.value" :loading="list.loading.value"
            @next="pager.next(list.data.value?.next_cursor)" @previous="pager.previous"
          />
        </template>
      </template>
    </div>
  </AppLayout>
</template>
