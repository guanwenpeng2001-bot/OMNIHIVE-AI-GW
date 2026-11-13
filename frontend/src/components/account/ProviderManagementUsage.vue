<template>
  <section v-if="eligible" class="management-usage min-w-0 max-w-[260px] space-y-1.5 text-[10px] text-[var(--foreground)]" :aria-busy="loading" :aria-label="t(`${prefix}.title`)">
    <div class="font-medium">{{ t(`${prefix}.${isAzure ? 'azureTitle' : 'githubTitle'}`) }}</div>
    <p class="max-w-[230px] truncate text-[var(--muted-foreground)]" :title="t(`${prefix}.${isAzure ? 'azureScope' : 'githubScope'}`)">{{ t(`${prefix}.${isAzure ? 'azureScope' : 'githubScope'}`) }}</p>
    <p v-if="isAzure" class="max-w-[230px] truncate text-[var(--muted-foreground)]" :title="t(`${prefix}.azureUnits`)">{{ t(`${prefix}.azureUnits`) }}</p>
    <p v-else class="max-w-[230px] truncate text-[var(--muted-foreground)]" :title="t(`${prefix}.githubUnits`)">{{ t(`${prefix}.githubUnits`) }}</p>
    <div class="flex flex-wrap items-center gap-2">
      <label v-if="!isAzure" class="flex flex-wrap items-center gap-1 text-[var(--muted-foreground)]">{{ t(`${prefix}.month`) }}
        <input v-model="selectedMonth" data-test="management-month" type="month" :min="oldestMonth" :max="currentMonth" class="min-w-0 rounded-md border border-[var(--border)] bg-[var(--card)] px-1.5 py-0.5 font-mono tabular-nums text-[var(--foreground)]" />
      </label>
      <button type="button" data-test="management-query" class="inline-flex items-center gap-1 rounded-md border border-[var(--border)] bg-[var(--card)] px-2 py-0.5 text-[10px] font-medium text-[var(--foreground)] shadow-sm transition-colors hover:border-[var(--brand)] hover:text-[var(--brand-text)] disabled:cursor-not-allowed disabled:opacity-50" :disabled="loading || (!isAzure && !billingMonth)" @click="query">{{ t(`${prefix}.query`) }}</button>
    </div>
    <div v-if="!isAzure && !billingMonth" role="alert">{{ t(`${prefix}.invalidMonth`) }}</div>
    <div v-if="failure" role="alert">{{ t(`${prefix}.reasons.${failure}`) }}</div>
    <p v-if="result && (loading || failure)" role="status" class="text-[var(--warn-text)]">{{ t('admin.accounts.managementUsage.previousReading') }}</p>
    <div v-if="result?.partial" role="status" class="text-[var(--warn-text)]">{{ t(`${prefix}.partial`) }}</div>
    <div v-if="result?.quota" class="space-y-1" aria-live="polite">
      <div>{{ t(`${prefix}.subscription`) }}: {{ result.quota.subscription_id }} · {{ t(`${prefix}.region`) }}: {{ result.quota.region }}</div>
      <div>{{ t(`${prefix}.pages`) }}: {{ result.quota.pages_fetched }}</div>
      <div v-if="!result.quota.value.length">{{ t(`${prefix}.noItems`) }}</div>
      <div v-else class="max-h-48 overflow-auto rounded-md border border-[var(--border)]" tabindex="0" :aria-label="t(`${prefix}.azureTitle`)">
        <table class="w-full text-left">
          <caption class="sr-only">{{ t(`${prefix}.azureTitle`) }}</caption>
          <thead><tr><th v-for="field in ['name', 'allocated', 'limit', 'unit']" :key="field" scope="col" class="px-2 py-1">{{ t(`${prefix}.${field}`) }}</th></tr></thead>
          <tbody><tr v-for="(row, index) in result.quota.value" :key="index">
            <th scope="row" class="px-2 py-1 font-normal">{{ row.name.value }}<div class="text-[var(--muted-foreground)]">{{ display(row.name.localizedValue) }}</div></th>
            <td class="px-2 py-1">{{ display(row.currentValue) }}</td><td class="px-2 py-1">{{ display(row.limit) }}</td><td class="px-2 py-1">{{ display(row.unit) }}</td>
          </tr></tbody>
        </table>
      </div>
    </div>
    <div v-if="result?.billing" class="space-y-3" aria-live="polite">
      <div>{{ result.billing.username }} · {{ result.billing.year }}-{{ String(result.billing.month).padStart(2, '0') }}</div>
      <section v-for="report in result.billing.reports" :key="report.report" class="space-y-1">
        <h4 class="font-medium">{{ t(`${prefix}.${report.report}`) }}</h4>
        <div v-if="!report.success" role="alert">{{ t(`${prefix}.reasons.${managementFailureReason(report)}`) }}</div>
        <template v-else>
          <div>{{ t(`${prefix}.reportedPeriod`) }}: {{ report.timePeriod?.year }} / {{ display(report.timePeriod?.month) }} · {{ report.user }}</div>
          <div v-if="!report.usageItems?.length">{{ t(`${prefix}.noItems`) }}</div>
          <div v-else class="max-h-48 overflow-auto rounded-md border border-[var(--border)]" tabindex="0" :aria-label="t(`${prefix}.${report.report}`)">
            <table class="w-full text-left">
              <caption class="sr-only">{{ t(`${prefix}.${report.report}`) }}</caption>
              <thead><tr><th v-for="field in billingFields" :key="field" scope="col" class="whitespace-nowrap px-2 py-1">{{ t(`${prefix}.fields.${field}`) }}</th></tr></thead>
              <tbody><tr v-for="(row, index) in report.usageItems" :key="index"><td v-for="field in billingFields" :key="field" class="px-2 py-1">{{ display(row[field]) }}</td></tr></tbody>
            </table>
          </div>
        </template>
      </section>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Account } from '@/types'
import { accountQueryIdentity } from '@/utils/accountQueryIdentity'
import { managementUsageVisible, githubBillingTextFields, githubBillingNumberFields, managementFailureReason, queryManagementUsage, validManagementUsage, type ManagementBillingMonth, type ManagementUsageReason, type ManagementUsageResult } from '@/api/admin/managementUsage'

const props = defineProps<{ account: Account }>()
const { t } = useI18n()
const prefix = 'admin.accounts.managementUsage'
const billingFields = [...githubBillingTextFields, ...githubBillingNumberFields]
const isAzure = computed(() => props.account.platform === 'azure_openai')
const eligible = computed(() => managementUsageVisible(props.account))
const today = new Date()
const currentMonth = today.toISOString().slice(0, 7)
const oldestMonth = new Date(Date.UTC(today.getUTCFullYear(), today.getUTCMonth() - 23, 1)).toISOString().slice(0, 7)
const selectedMonth = ref(currentMonth)
const billingMonth = computed<ManagementBillingMonth | undefined>(() => {
  if (!/^\d{4}-(0[1-9]|1[0-2])$/.test(selectedMonth.value) || selectedMonth.value < oldestMonth || selectedMonth.value > currentMonth) return undefined
  return { year: Number(selectedMonth.value.slice(0, 4)), month: Number(selectedMonth.value.slice(5)) }
})
const loading = ref(false)
const failure = ref<ManagementUsageReason | null>(null)
const result = ref<ManagementUsageResult | null>(null)
let generation = 0
function display(value: unknown) { return value == null || value === '' ? t(`${prefix}.unknown`) : value }
function invalidate() { generation++; loading.value = false; result.value = null; failure.value = null }
async function query() {
  if (loading.value || !eligible.value || (!isAzure.value && !billingMonth.value)) return
  const request = ++generation
  const requested = isAzure.value ? undefined : billingMonth.value
  const provider = props.account.platform
  loading.value = true; failure.value = null
  try {
    const response = await queryManagementUsage(props.account.id, requested)
    if (request !== generation) return
    if (!validManagementUsage(response, provider, requested)) { failure.value = 'invalid_response'; return }
    if (response.success || response.partial) result.value = response
    if (!response.success) failure.value = managementFailureReason(response)
  } catch (error) {
    if (request !== generation) return
    failure.value = managementFailureReason({ status_code: (error as { response?: { status?: number } })?.response?.status })
  } finally { if (request === generation) loading.value = false }
}
watch(() => JSON.stringify([accountQueryIdentity(props.account), selectedMonth.value]), invalidate, { flush: 'sync' })
onBeforeUnmount(() => { generation++ })
</script>

<style scoped>
.management-usage button:focus-visible,
.management-usage input:focus-visible,
.management-usage [tabindex]:focus-visible {
  outline: 2px solid var(--brand);
  outline-offset: 2px;
}

.management-usage [role='alert'] {
  border: 1px solid color-mix(in srgb, var(--bad) 30%, transparent);
  border-radius: 0.25rem;
  background: color-mix(in srgb, var(--bad) 10%, transparent);
  padding: 0.125rem 0.375rem;
  color: var(--bad-text);
}

thead {
  background: var(--surface-2);
  color: var(--muted-foreground);
}

tbody tr + tr {
  border-top: 1px solid var(--border);
}

td {
  font-family: 'JetBrains Mono', ui-monospace, monospace;
  font-variant-numeric: tabular-nums;
}
</style>
