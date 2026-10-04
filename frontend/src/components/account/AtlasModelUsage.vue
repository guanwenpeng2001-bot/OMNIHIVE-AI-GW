<template>
  <div v-if="eligible" class="space-y-1 text-xs" :aria-busy="loading">
    <div>{{ t('admin.accounts.atlasUsage.title') }}</div>
    <div class="text-[10px] text-gray-500">{{ t('admin.accounts.atlasUsage.scope') }}</div>
    <fieldset :disabled="loading" class="space-y-1">
      <legend class="text-[10px]">{{ t('admin.accounts.compatibleUsage.window') }} · UTC</legend>
      <div class="flex flex-wrap gap-2">
        <button v-for="days in [1, 7, 30]" :key="days" type="button" class="text-blue-600" :data-test="`atlas-preset-${days}`" @click="preset(days)">{{ t(`admin.accounts.compatibleUsage.preset${days}`) }}</button>
      </div>
      <label class="block">{{ t('admin.accounts.compatibleUsage.start') }} <input v-model="startDate" data-test="atlas-start" type="date" class="rounded border bg-transparent" /></label>
      <label class="block">{{ t('admin.accounts.atlasUsage.endExclusive') }} <input v-model="endDate" data-test="atlas-end" type="date" class="rounded border bg-transparent" /></label>
      <button type="button" data-test="atlas-query" class="rounded px-1.5 py-0.5 text-blue-600 disabled:opacity-50" :disabled="!window" @click="query">{{ t('admin.accounts.cnProviders.probe') }}</button>
    </fieldset>
    <div v-if="!window" role="alert">{{ t('admin.accounts.atlasUsage.invalidWindow') }}</div>
    <div v-if="failure" role="alert">{{ t(`admin.accounts.compatibleUsage.reasons.${failure}`) }}</div>
    <div v-if="result?.usage" aria-live="polite" class="space-y-1">
      <div v-if="stale" class="text-amber-600">{{ t('admin.accounts.compatibleUsage.stale') }}</div>
      <div>{{ result.usage.start_date }} – {{ result.usage.end_date }} · UTC · {{ t('admin.accounts.atlasUsage.endExclusive') }}</div>
      <div v-if="result.usage.partial || result.usage.has_more" class="text-amber-600">{{ t('admin.accounts.atlasUsage.partial') }}</div>
      <div class="text-[10px] text-gray-500">{{ t('admin.accounts.atlasUsage.pages') }}: {{ result.usage.pages_fetched }}</div>
      <div v-if="!result.usage.data.length">{{ t('admin.accounts.compatibleUsage.noItems') }}</div>
      <div v-for="bucket in result.usage.data" :key="bucket.date" class="space-y-1">
        <div class="font-medium">{{ bucket.date }} · UTC</div>
        <div v-if="bucket.partial" class="text-amber-600">{{ t('admin.accounts.atlasUsage.partial') }}</div>
        <div class="text-[10px] text-gray-500">{{ t('admin.accounts.atlasUsage.coveredUntil') }}: {{ formatReportDateTime(bucket.covered_until, 'UTC') }} UTC</div>
        <div v-if="!bucket.results.length">{{ t('admin.accounts.compatibleUsage.noItems') }}</div>
        <div v-for="(row, index) in bucket.results" :key="index">
          <div>{{ row.usage.requests }} {{ t('admin.accounts.atlasUsage.requests') }}</div>
          <template v-if="row.usage.tokens">
            <div v-for="field in atlasTokenFields" :key="field">{{ t(`admin.accounts.atlasUsage.tokens.${field}`) }}: {{ row.usage.tokens[field] }} tokens</div>
          </template>
          <div v-else>tokens: {{ t('admin.accounts.atlasUsage.unknown') }}</div>
          <div>{{ t('admin.accounts.atlasUsage.images') }}: {{ row.usage.images?.count ?? t('admin.accounts.atlasUsage.unknown') }}</div>
          <div>{{ t('admin.accounts.atlasUsage.video') }}: {{ row.usage.video ? `${row.usage.video.seconds} ${t('admin.accounts.atlasUsage.seconds')}` : t('admin.accounts.atlasUsage.unknown') }}</div>
        </div>
      </div>
      <div class="text-[10px] text-gray-500">{{ t('admin.accounts.compatibleUsage.delayed') }}</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { atlasTokenFields, queryAtlasUsage, type AtlasUsageResult, type AtlasUsageWindow } from '@/api/admin/atlasUsage'
import type { CompatibleUsageReason } from '@/api/admin/cnProviders'
import type { Account } from '@/types'
import { accountQueryIdentity } from '@/utils/accountQueryIdentity'
import { formatReportDateInput, formatReportDateTime } from '@/utils/format'
import { compatibleBalanceVisible, compatibleFailureReason } from './compatibleBalance'

const props = defineProps<{ account: Account }>()
const { t } = useI18n()
const startDate = ref('')
const endDate = ref('')
const loading = ref(false)
const failure = ref<CompatibleUsageReason | null>(null)
const result = ref<AtlasUsageResult | null>(null)
let generation = 0
const eligible = computed(() => props.account.platform === 'atlascloud' && compatibleBalanceVisible(props.account))
function preset(days: number) {
  const today = Date.parse(`${formatReportDateInput(new Date(), 'UTC')}T00:00:00Z`)
  startDate.value = formatReportDateInput(new Date(today - (days - 1) * 86400000), 'UTC')
  endDate.value = formatReportDateInput(new Date(today + 86400000), 'UTC')
}
preset(1)
const window = computed<AtlasUsageWindow | null>(() => {
  const validDate = (value: string) => /^\d{4}-\d{2}-\d{2}$/.test(value) && Number.isFinite(Date.parse(value)) && new Date(value).toISOString().slice(0, 10) === value
  if (!validDate(startDate.value) || !validDate(endDate.value)) return null
  const span = Date.parse(endDate.value) - Date.parse(startDate.value)
  return span > 0 && span <= 180 * 86400000 ? { start_date: startDate.value, end_date: endDate.value } : null
})
const stale = computed(() => !!result.value && (loading.value || !!failure.value || result.value.usage?.start_date !== window.value?.start_date || result.value.usage?.end_date !== window.value?.end_date))

function validResult(response: AtlasUsageResult, requested: AtlasUsageWindow) {
  const usage = response.usage
  const counter = (value: unknown) => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0
  return response.provider === 'atlascloud' && response.kind === 'consumed_usage' && usage?.scope === 'self' &&
    usage.start_date === requested.start_date && usage.end_date === requested.end_date &&
    typeof usage.partial === 'boolean' && typeof usage.has_more === 'boolean' && counter(usage.pages_fetched) &&
    Array.isArray(usage.data) && usage.data.every(bucket => typeof bucket.partial === 'boolean' &&
      Number.isFinite(Date.parse(bucket.covered_until)) && Array.isArray(bucket.results) && bucket.results.every(row =>
        counter(row.usage.requests) && (row.usage.tokens === null || atlasTokenFields.every(field => counter(row.usage.tokens?.[field]))) &&
        (row.usage.images === null || counter(row.usage.images?.count)) && (row.usage.video === null || counter(row.usage.video?.seconds))))
}
async function query() {
  if (loading.value || !eligible.value || !window.value) return
  const request = ++generation
  const requested = { ...window.value }
  loading.value = true
  failure.value = null
  try {
    const response = await queryAtlasUsage(props.account.id, requested)
    if (request !== generation) return
    if (response.success && validResult(response, requested)) result.value = response
    else failure.value = response.success ? 'invalid_response' : compatibleFailureReason(response)
  } catch (error) {
    if (request !== generation) return
    failure.value = compatibleFailureReason({ status_code: (error as { response?: { status?: number } })?.response?.status })
  } finally {
    if (request === generation) loading.value = false
  }
}
watch(() => accountQueryIdentity(props.account), () => {
  generation++
  loading.value = false
  result.value = null
  failure.value = null
}, { flush: 'sync' })
onBeforeUnmount(() => { generation++ })
</script>
