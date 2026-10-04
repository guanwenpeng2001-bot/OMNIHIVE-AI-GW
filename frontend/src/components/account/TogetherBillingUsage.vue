<template>
  <div v-if="eligible" class="space-y-2 text-xs" :aria-busy="loading">
    <div class="font-medium">{{ t('admin.accounts.togetherUsage.title') }}</div>
    <p class="text-gray-500">{{ t('admin.accounts.togetherUsage.scope') }}</p>
    <div class="flex flex-wrap items-center gap-2">
      <label>{{ t('admin.accounts.togetherUsage.month') }} <input v-model="month" data-test="together-month" type="month" class="rounded border bg-transparent" /></label>
      <button type="button" data-test="together-query" :disabled="loading || !validMonth" class="rounded px-1.5 py-0.5 text-blue-600 disabled:opacity-50" @click="query">{{ t('admin.accounts.cnProviders.probe') }}</button>
    </div>
    <p v-if="!validMonth" role="alert">{{ t('admin.accounts.togetherUsage.invalidMonth') }}</p>
    <p v-if="loading" role="status">{{ t('admin.accounts.togetherUsage.loading') }}</p>
    <p v-if="failure" role="alert">{{ t(`admin.accounts.togetherUsage.reasons.${failure}`) }}</p>
    <div v-if="result?.usage" aria-live="polite" class="space-y-1">
      <p>{{ result.usage.organization_id }} · {{ result.usage.billing_period }} · UTC · USD</p>
      <p v-if="result.usage.partial" class="text-amber-600">{{ t('admin.accounts.togetherUsage.partial') }}</p>
      <p v-if="!rows.length">{{ t('admin.accounts.togetherUsage.empty') }}</p>
      <div v-else class="max-w-xl overflow-x-auto">
        <table class="w-full text-left">
          <caption class="sr-only">{{ t('admin.accounts.togetherUsage.title') }}</caption>
          <thead><tr><th v-for="field in ['date', 'product', 'unit', 'quantity', 'unitPrice', 'cost']" :key="field" scope="col" class="p-1">{{ t(`admin.accounts.togetherUsage.${field}`) }}</th></tr></thead>
          <tbody><tr v-for="(row, index) in rows" :key="index">
            <td class="whitespace-nowrap p-1">{{ row.date }}</td><td class="p-1">{{ row.product_name }}</td>
            <td class="p-1">{{ typeof row.pricing_dimensions.unit === 'string' ? row.pricing_dimensions.unit : t('admin.accounts.togetherUsage.unknown') }}</td>
            <td class="p-1">{{ row.quantity }}</td><td class="p-1">{{ row.unit_price }} USD</td><td class="p-1">{{ row.cost }} USD</td>
          </tr></tbody>
        </table>
      </div>
      <p class="text-gray-500">{{ t('admin.accounts.togetherUsage.delayed') }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Account } from '@/types'
import { queryTogetherUsage, togetherBillingVisible, validTogetherBillingResult, type TogetherBillingResult } from '@/api/admin/togetherUsage'

const props = defineProps<{ account: Account }>()
const { t } = useI18n()
const month = ref(new Date().toISOString().slice(0, 7))
const validMonth = computed(() => /^\d{4}-(0[1-9]|1[0-2])$/.test(month.value))
const eligible = computed(() => togetherBillingVisible(props.account))
const loading = ref(false)
const failure = ref<string | null>(null)
const result = ref<TogetherBillingResult | null>(null)
const rows = computed(() => result.value?.usage?.data.flatMap(window => window.line_items.map(line => ({ ...line, date: window.date }))) ?? [])
let generation = 0
const reasons = ['not_enabled', 'invalid_credentials', 'permission_denied', 'rate_limited', 'invalid_response', 'upstream_error', 'custom_origin', 'mode_unsupported', 'proxy_unavailable']
function failureReason(response: { reason?: string; status_code?: number }) {
  if (response.reason && reasons.includes(response.reason)) return response.reason
  return ({ 401: 'invalid_credentials', 403: 'permission_denied', 404: 'not_enabled', 429: 'rate_limited' } as Record<number, string>)[response.status_code ?? 0] ?? 'upstream_error'
}
async function query() {
  if (!eligible.value || !validMonth.value || loading.value) return
  const request = ++generation
  const requestedMonth = month.value
  loading.value = true
  failure.value = null
  result.value = null
  try {
    const response = await queryTogetherUsage(props.account.id, requestedMonth)
    if (request !== generation) return
    if (response.success && validTogetherBillingResult(response, requestedMonth)) result.value = response
    else failure.value = response.success ? 'invalid_response' : failureReason(response)
  } catch (error) {
    if (request !== generation) return
    const status = (error as { response?: { status?: number } })?.response?.status
    // A local missing account/route is distinct from a supplier beta 404.
    failure.value = status === 404 ? 'upstream_error' : failureReason({ status_code: status })
  } finally {
    if (request === generation) loading.value = false
  }
}
// Financial updated_at changes are deliberately not query identity changes.
watch(() => JSON.stringify([month.value, props.account.id, props.account.platform, props.account.type, props.account.credentials, props.account.credentials_status?.has_api_key, props.account.extra?.custom_base_url_enabled, props.account.proxy_id, props.account.proxy_group_id]), () => {
  generation++
  result.value = null
  failure.value = null
  loading.value = false
}, { flush: 'sync' })
onBeforeUnmount(() => { generation++ })
</script>
