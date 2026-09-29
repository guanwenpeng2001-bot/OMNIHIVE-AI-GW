<template>
  <section class="rounded-lg border border-gray-200 p-4 dark:border-dark-600" data-testid="qianwen-pricing" @keydown.enter.stop.prevent>
    <button type="button" class="btn btn-secondary" @click="expanded = !expanded">{{ t('admin.accounts.qianwenPricing.title') }}</button>
    <div v-if="expanded" class="mt-4 space-y-3">
      <p class="input-hint">{{ t('admin.accounts.qianwenPricing.hint') }}</p>
      <fieldset :disabled="busy" class="space-y-3">
        <label class="block input-label">{{ t('admin.accounts.qianwenPricing.rate') }}
          <input v-model="rate" data-testid="pricing-rate" type="number" min="0" step="any" class="input" />
        </label>
        <label class="block input-label">{{ t('admin.accounts.qianwenPricing.source') }}
          <input v-model="source" data-testid="pricing-source" class="input" />
        </label>
        <label class="block input-label">{{ t('admin.accounts.qianwenPricing.effective') }}
          <input v-model="effective" data-testid="pricing-effective" type="datetime-local" class="input" />
        </label>
        <label class="block input-label">{{ t('admin.accounts.qianwenPricing.group') }}
          <input v-model="group" data-testid="pricing-group" type="number" min="1" step="1" class="input" />
        </label>
        <div class="flex gap-2">
          <button type="button" data-testid="pricing-preview" class="btn btn-secondary" :disabled="!valid" @click="sync(false)">{{ t('admin.accounts.qianwenPricing.preview') }}</button>
          <button type="button" data-testid="pricing-apply" class="btn btn-primary" :disabled="!canApply" @click="sync(true)">{{ t('admin.accounts.qianwenPricing.apply') }}</button>
        </div>
      </fieldset>
      <p v-if="busy" role="status">{{ t('common.loading') }}</p>
      <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
      <div v-for="(entry, index) in sessions" :key="index" class="space-y-2 rounded border p-3" data-testid="pricing-result">
        <p class="font-medium" role="status">{{ t(`admin.accounts.qianwenPricing.${entry.result.status === 'ready' ? 'ready' : 'unsupported'}`) }}</p>
        <p>{{ t('admin.accounts.qianwenPricing.counts', { imported: entry.result.imported, written: entry.written, group: entry.group_id ?? '—' }) }}</p>
        <p v-if="entry.result.reason">{{ entry.result.reason }}</p>
        <details><summary>{{ t('admin.accounts.qianwenPricing.details') }}</summary>
          <pre class="max-h-80 overflow-auto whitespace-pre-wrap break-all text-xs">{{ JSON.stringify(entry, null, 2) }}</pre>
        </details>
      </div>
    </div>
  </section>
</template>
<script setup lang="ts">
import { extractApiErrorMessage } from '@/utils/apiError'

import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { syncQianwenPricing, type QianwenPricingResponse } from '@/api/admin/qianwenPricing'
const props = defineProps<{ accountId: number }>()
const { t } = useI18n()
const expanded = ref(false)
const busy = ref(false)
const rate = ref('')
const source = ref('')
const effective = ref('')
const group = ref('')
const error = ref('')
const sessions = ref<QianwenPricingResponse[]>([])
const previewSignature = ref('')
const signature = computed(() => JSON.stringify([rate.value, source.value, effective.value]))
const valid = computed(() => Number.isFinite(Number(rate.value)) && Number(rate.value) > 0 && !!source.value.trim() && !!effective.value && Number.isFinite(Date.parse(effective.value)))
const canApply = computed(() => valid.value && previewSignature.value === signature.value && Number.isSafeInteger(Number(group.value)) && Number(group.value) > 0)
async function sync(apply: boolean) {
  if (busy.value || !valid.value || (apply && !canApply.value)) return
  busy.value = true
  error.value = ''
  previewSignature.value = ''
  try {
    const result = await syncQianwenPricing(props.accountId, {
      region: 'asia-pacific-china', usd_per_cny: Number(rate.value), fx_source: source.value.trim(),
      fx_effective_at: new Date(effective.value).toISOString(), ...(apply ? { group_id: Number(group.value) } : {})
    })
    sessions.value.unshift(result)
    if (result.result.status === 'ready') previewSignature.value = signature.value
  } catch (err: unknown) {
    const detail = err as { message?: string; response?: { data?: { message?: string } } }
    error.value = `${t('admin.accounts.qianwenPricing.failed')}: ${extractApiErrorMessage(detail, '') || ''}`
  } finally { busy.value = false }
}
</script>
