<template>
  <div v-if="eligible" class="space-y-1 text-xs" :aria-busy="loading">
    <div>{{ t('admin.accounts.compatibleUsage.keyConsumption') }}</div>
    <div class="text-[10px] text-gray-500">{{ t('admin.accounts.compatibleUsage.qiniuHint') }}</div>
    <fieldset :disabled="loading" class="space-y-1">
      <legend class="text-[10px]">{{ t('admin.accounts.compatibleUsage.window') }} · {{ timezone }}</legend>
      <div class="flex flex-wrap gap-2">
        <button v-for="days in [1, 7, 30]" :key="days" type="button" class="text-blue-600" @click="preset(days)">{{ t(`admin.accounts.compatibleUsage.preset${days}`) }}</button>
      </div>
      <label class="block">{{ t('admin.accounts.compatibleUsage.start') }} <input v-model="startDate" data-test="usage-start" type="date" class="rounded border bg-transparent" /></label>
      <label class="block">{{ t('admin.accounts.compatibleUsage.end') }} <input v-model="endDate" data-test="usage-end" type="date" class="rounded border bg-transparent" /></label>
      <button type="button" data-test="usage-query" class="rounded px-1.5 py-0.5 text-blue-600 disabled:opacity-50" :disabled="!window" @click="query">{{ t('admin.accounts.cnProviders.probe') }}</button>
    </fieldset>
    <div v-if="!window" role="alert">{{ t('admin.accounts.compatibleUsage.invalidWindow') }}</div>
    <div v-if="failure" role="alert">{{ t(`admin.accounts.compatibleUsage.reasons.${failure}`) }}</div>
    <div v-if="result?.usage" aria-live="polite" class="space-y-1">
      <div v-if="stale" class="text-amber-600">{{ t('admin.accounts.compatibleUsage.stale') }}</div>
      <div>{{ formatReportDateTime(result.usage.start, result.usage.timezone) }} – {{ formatReportDateTime(result.usage.end, result.usage.timezone) }} · {{ result.usage.timezone }}</div>
      <div v-if="!result.usage.models.length">{{ t('admin.accounts.compatibleUsage.noItems') }}</div>
      <div v-for="(model, modelIndex) in result.usage.models" :key="`${model.id}-${modelIndex}`">
        <div class="font-medium">{{ model.name }} ({{ model.id }})</div>
        <div v-for="(item, itemIndex) in model.items" :key="itemIndex">{{ item.name }}: {{ item.total }} {{ item.unit }}</div>
      </div>
      <div class="text-[10px] text-gray-500">{{ t('admin.accounts.compatibleUsage.delayed') }}</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { CompatibleConsumedUsageResult, CompatibleUsageReason, CompatibleUsageWindow } from '@/api/admin/cnProviders'
import type { Account } from '@/types'
import { formatReportDateInput, formatReportDateTime, resolveReportTimeZone } from '@/utils/format'
import { compatibleFailureReason, compatibleUsageCapability } from './compatibleBalance'

const props = defineProps<{ account: Account }>()
const { t } = useI18n()
const timezone = resolveReportTimeZone('Asia/Shanghai')
const startDate = ref('')
const endDate = ref('')
const loading = ref(false)
const failure = ref<CompatibleUsageReason | null>(null)
const result = ref<CompatibleConsumedUsageResult | null>(null)
let generation = 0
const eligible = computed(() => compatibleUsageCapability(props.account).kind === 'consumed_usage')
function preset(days: number) {
  const now = new Date()
  endDate.value = formatReportDateInput(now, timezone)
  startDate.value = formatReportDateInput(new Date(now.getTime() - (days - 1) * 86400000), timezone)
}
preset(1)
const window = computed<CompatibleUsageWindow | null>(() => {
  const validDate = (value: string) => /^\d{4}-\d{2}-\d{2}$/.test(value) && Number.isFinite(Date.parse(value)) && new Date(value).toISOString().slice(0, 10) === value
  if (!validDate(startDate.value) || !validDate(endDate.value)) return null
  const start = `${startDate.value}T00:00:00+08:00`
  const endMillis = Date.parse(`${endDate.value}T00:00:00+08:00`) + 86400000
  const span = endMillis - Date.parse(start)
  if (span <= 0 || span > 31 * 86400000) return null
  const end = `${formatReportDateInput(new Date(endMillis), timezone)}T00:00:00+08:00`
  return { granularity: 'day', start, end, timezone }
})
const stale = computed(() => !!result.value && (loading.value || !!failure.value || result.value.usage?.start !== window.value?.start || result.value.usage?.end !== window.value?.end))
async function query() {
  if (loading.value || !eligible.value || !window.value) return
  const request = ++generation
  loading.value = true
  failure.value = null
  try {
    const response = await adminAPI.cnProviders.queryUsage(props.account.id, { ...window.value })
    if (request !== generation) return
    if (response.success && response.kind === 'consumed_usage' && response.usage && response.usage.models.every(model => model.items.every(item => typeof item.total === 'number' && Number.isFinite(item.total)))) result.value = response
    else failure.value = response.success ? 'invalid_response' : compatibleFailureReason(response)
  } catch (error) {
    if (request !== generation) return
    failure.value = compatibleFailureReason({ status_code: (error as { response?: { status?: number } })?.response?.status })
  } finally {
    if (request === generation) loading.value = false
  }
}
watch(() => [props.account.id, props.account.platform, props.account.type, JSON.stringify(props.account.credentials), props.account.credentials_status?.has_api_key, props.account.extra?.custom_base_url_enabled], () => {
  generation++
  loading.value = false
  result.value = null
  failure.value = null
})
onBeforeUnmount(() => { generation++ })
</script>
