<template>
  <div v-if="eligible" class="consumed-usage min-w-0 max-w-[260px] space-y-1.5 text-[10px] text-[var(--foreground)]" :aria-busy="loading">
    <div class="font-medium">{{ t('admin.accounts.compatibleUsage.keyConsumption') }}</div>
    <div class="max-w-[230px] truncate text-[var(--muted-foreground)]" :title="t('admin.accounts.compatibleUsage.qiniuHint')">{{ t('admin.accounts.compatibleUsage.qiniuHint') }}</div>
    <fieldset :disabled="loading" class="min-w-0 space-y-1.5 rounded-md border border-[var(--border)] bg-[color-mix(in_srgb,var(--surface-2)_70%,transparent)] p-2">
      <legend class="px-1 font-mono text-[10px] text-[var(--muted-foreground)]">{{ t('admin.accounts.compatibleUsage.window') }} · {{ timezone }}</legend>
      <div class="flex flex-wrap gap-1">
        <button v-for="days in [1, 7, 30]" :key="days" type="button" class="usage-action" @click="preset(days)">{{ t(`admin.accounts.compatibleUsage.preset${days}`) }}</button>
      </div>
      <label class="flex flex-wrap items-center justify-between gap-1 text-[var(--muted-foreground)]">{{ t('admin.accounts.compatibleUsage.start') }} <input v-model="startDate" data-test="usage-start" type="date" class="min-w-0 rounded-md border border-[var(--border)] bg-[var(--card)] px-1.5 py-0.5 font-mono tabular-nums text-[var(--foreground)]" /></label>
      <label class="flex flex-wrap items-center justify-between gap-1 text-[var(--muted-foreground)]">{{ t('admin.accounts.compatibleUsage.end') }} <input v-model="endDate" data-test="usage-end" type="date" class="min-w-0 rounded-md border border-[var(--border)] bg-[var(--card)] px-1.5 py-0.5 font-mono tabular-nums text-[var(--foreground)]" /></label>
      <button type="button" data-test="usage-query" class="usage-action" :disabled="!window" @click="query">{{ t('admin.accounts.cnProviders.probe') }}</button>
    </fieldset>
    <div v-if="!window" role="alert">{{ t('admin.accounts.compatibleUsage.invalidWindow') }}</div>
    <div v-if="failure" role="alert">{{ t(`admin.accounts.compatibleUsage.reasons.${failure}`) }}</div>
    <div v-if="result?.usage" aria-live="polite" class="max-h-48 space-y-1 overflow-auto rounded-md border border-[var(--border)] p-2 font-mono tabular-nums">
      <div v-if="stale" class="text-[var(--warn-text)]">{{ t('admin.accounts.compatibleUsage.stale') }}</div>
      <div>{{ formatReportDateTime(result.usage.start, result.usage.timezone) }} – {{ formatReportDateTime(result.usage.end, result.usage.timezone) }} · {{ result.usage.timezone }}</div>
      <div v-if="!result.usage.models.length">{{ t('admin.accounts.compatibleUsage.noItems') }}</div>
      <div v-for="(model, modelIndex) in result.usage.models" :key="`${model.id}-${modelIndex}`">
        <div class="font-medium">{{ model.name }} ({{ model.id }})</div>
        <div v-for="(item, itemIndex) in model.items" :key="itemIndex">{{ item.name }}: {{ item.total }} {{ item.unit }}</div>
      </div>
      <div class="text-[10px] text-[var(--muted-foreground)]">{{ t('admin.accounts.compatibleUsage.delayed') }}</div>
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

<style scoped>
.usage-action {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  border: 1px solid var(--border);
  border-radius: 0.375rem;
  background: var(--card);
  padding: 0.125rem 0.5rem;
  font-size: 10px;
  font-weight: 500;
  color: var(--foreground);
  transition: color 150ms, border-color 150ms;
}

.usage-action:hover {
  border-color: var(--brand);
  color: var(--brand-text);
}

.usage-action:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.usage-action:focus-visible,
input:focus-visible {
  outline: 2px solid var(--brand);
  outline-offset: 2px;
}

.consumed-usage [role='alert'] {
  border: 1px solid color-mix(in srgb, var(--bad) 30%, transparent);
  border-radius: 0.25rem;
  background: color-mix(in srgb, var(--bad) 10%, transparent);
  padding: 0.125rem 0.375rem;
  color: var(--bad-text);
}
</style>
