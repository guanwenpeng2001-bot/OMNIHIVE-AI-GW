<template>
  <details class="pricing-insights mt-3 rounded-lg border border-[var(--border)] bg-[var(--surface-2)] p-4 text-xs text-[var(--foreground)]" @toggle="onToggle">
    <summary class="cursor-pointer font-medium">{{ t('admin.channels.insights.title') }}</summary>
    <div v-if="opened" class="mt-3 space-y-3" data-testid="pricing-insights">
      <p class="text-[var(--muted-foreground)]">{{ t('admin.channels.insights.readOnly') }}</p>
      <p v-if="loading" role="status">{{ t('common.loading') }}</p>
      <p v-if="error" role="alert">{{ error }}</p>
      <button v-if="error" type="button" class="btn btn-secondary btn-outline" @click="loadAccounts">{{ t('admin.channels.insights.retry') }}</button>
      <p v-if="!groupIds.length">{{ t('admin.channels.insights.needGroup') }}</p>
      <label class="block">{{ t('admin.channels.insights.account') }}
        <select v-model="accountId" class="input tabular-nums mt-1" data-testid="insight-account">
          <option value="">{{ t('admin.channels.insights.chooseAccount') }}</option>
          <option v-for="account in accounts" :key="account.id" :value="String(account.id)">{{ account.name }} (#{{ account.id }})</option>
        </select>
      </label>
      <p v-if="catalog?.staleness === 'stale'" role="status">{{ t('capabilityCatalog.stale') }}</p>
      <label class="block">{{ t('admin.channels.insights.model') }}
        <select v-model="model" class="input tabular-nums mt-1" data-testid="insight-model">
          <option v-for="name in entry.models" :key="name" :value="name">{{ name }}</option>
        </select>
      </label>
      <p>{{ t('admin.channels.insights.profile') }}: {{ metadata?.provider_profile || unknown }} · {{ metadata?.task_type || unknown }} · {{ metadata?.metadata_status || unknown }}</p>
      <p v-if="metadata?.missing_fields?.length">{{ t('capabilityCatalog.missing') }}: {{ metadata.missing_fields.join(', ') }}</p>
      <p v-if="metadata?.conflicts?.length" role="status">{{ t('capabilityCatalog.conflict') }}: {{ metadata.conflicts.join(', ') }}</p>
      <div class="overflow-x-auto rounded-lg border border-[var(--border)] bg-[var(--card)] p-3">
        <table class="w-full text-left tabular-nums" data-testid="capability-pricing-table">
          <thead><tr><th>{{ t('admin.channels.insights.field') }}</th><th>{{ t('admin.channels.insights.value') }}</th><th>{{ t('capabilityCatalog.source') }}</th></tr></thead>
          <tbody><tr v-for="field in fields" :key="field" class="border-t border-[var(--border)] align-top">
            <th class="py-2 pr-3 font-normal">{{ t(`admin.channels.insights.fields.${field}`) }}</th>
            <td class="break-words py-2 pr-3">{{ fieldValue(field) }}</td>
            <td class="break-all py-2">{{ evidence(field) }}</td>
          </tr></tbody>
        </table>
      </div>
      <button type="button" class="btn btn-secondary btn-outline" :disabled="loading || !model" data-testid="compare-pricing" @click="preview(false)">{{ t('admin.channels.insights.compare') }}</button>
      <details class="rounded-lg border border-[var(--border)] bg-[var(--card)] p-3">
        <summary class="cursor-pointer">{{ t('admin.channels.insights.sourcePreview') }}</summary>
        <p class="my-2">{{ t('admin.channels.insights.sampleHint') }}</p>
        <label class="block">{{ t('admin.channels.insights.candidate') }}<textarea v-model="candidate" class="input tabular-nums mt-1" rows="4" data-testid="candidate-catalog" /></label>
        <label class="block">{{ t('admin.channels.insights.group') }}<select v-model="sampleGroupId" class="input tabular-nums"><option v-for="groupId in groupIds" :key="groupId" :value="groupId">#{{ groupId }}</option></select></label>
        <label class="block">{{ t('admin.channels.insights.inputTokens') }}<input v-model.number="inputTokens" class="input tabular-nums" type="number" min="0" max="10000000" /></label>
        <label class="block">{{ t('admin.channels.insights.outputTokens') }}<input v-model.number="outputTokens" class="input tabular-nums" type="number" min="0" max="10000000" /></label>
        <button type="button" class="btn btn-secondary btn-outline mt-2" :disabled="loading || !candidate.trim() || !sampleGroupId || !model" data-testid="preview-source" @click="preview(true)">{{ t('admin.channels.insights.sourcePreview') }}</button>
      </details>
      <div v-if="result" class="space-y-2 rounded-lg border border-[var(--border)] bg-[var(--card)] p-3 tabular-nums" data-testid="pricing-preview-result">
        <p>{{ t('admin.channels.insights.referenceHint') }}</p>
        <p v-if="result.source">{{ result.source.before_count }} → {{ result.source.after_count }} · {{ t('admin.channels.insights.added') }}: {{ result.source.added.length }} · {{ t('admin.channels.insights.removed') }}: {{ result.source.removed.length }}</p>
        <div v-for="difference in differences" :key="difference.id" :role="difference.warning ? 'alert' : undefined" :class="{ 'text-amber-700 dark:text-amber-400': difference.warning }">
          <p>{{ t(`admin.channels.insights.${difference.basis}`) }} · {{ difference.model }} · {{ difference.field }}: {{ t(`admin.channels.insights.${difference.status}`) }}<span v-if="difference.absolute_percent != null"> · {{ difference.absolute_percent.toFixed(2) }}%</span></p>
          <p>{{ t(difference.basis === 'channel_to_reference' ? 'admin.channels.insights.channelBaseline' : 'admin.channels.insights.baseline') }}: {{ quoteText(difference.baseline) }} → {{ t(difference.basis === 'channel_to_reference' ? 'admin.channels.insights.referenceCompared' : 'admin.channels.insights.compared') }}: {{ quoteText(difference.candidate) }}</p>
        </div>
        <p v-for="impact in result.impacts || []" :key="`${impact.group_id}:${impact.model}`">#{{ impact.group_id }} {{ impact.model }} · {{ t('admin.channels.insights.savedCharge') }} (USD): {{ impact.comparable ? `${impact.before?.ActualCost} → ${impact.after?.ActualCost}` : t('admin.channels.insights.incomparable') }}</p>
      </div>
    </div>
  </details>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getModelCatalog, list } from '@/api/admin/accounts'
import { previewPricing, type ChannelPricingPreview, type PricingQuote } from '@/api/admin/channels'
import type { AccountModelCatalogDetails } from '@/types'
import { extractApiErrorMessage } from '@/utils/apiError'
import { copyPricingFacts, formIntervalsToAPI, formTimePricingToAPI, mTokToPerToken, type PricingFormEntry } from './types'

const props = defineProps<{ entry: PricingFormEntry; platform: string; groupIds: number[] }>()
const { t } = useI18n()
const opened = ref(false)
const loadingAccounts = ref(false)
const loadingCatalog = ref(false)
const previewing = ref(false)
const loading = computed(() => loadingAccounts.value || loadingCatalog.value || previewing.value)
const error = ref('')
const accounts = ref<Array<{ id: number; name: string }>>([])
const accountId = ref('')
const catalog = ref<AccountModelCatalogDetails | null>(null)
const model = ref(props.entry.models[0] || '')
const result = ref<ChannelPricingPreview | null>(null)
const candidate = ref('')
const sampleGroupId = ref(props.groupIds[0])
const inputTokens = ref(1000)
const outputTokens = ref(1000)
let accountRequest = 0
let catalogRequest = 0
let previewRequest = 0
function clearPreview() { previewRequest++; previewing.value = false; result.value = null }
const unknown = computed(() => t('capabilityCatalog.unknown'))
const metadata = computed(() => catalog.value?.display_metadata?.[model.value])
const fields = computed(() => [
  'context_window', 'max_context_window', 'max_output_tokens', 'input_modalities', 'output_modalities',
  'supports_function_calling', 'reasoning', 'reasoning_control', 'supported_reasoning_levels',
  ...(metadata.value?.task_type === 'embedding' ? ['embedding_dimensions'] : []),
  ...(metadata.value?.task_type && !['text_generation', 'embedding'].includes(metadata.value.task_type) ? ['task_limits'] : [])
])
const differences = computed(() => [...new Map([...(result.value?.differences || []), ...(result.value?.source?.differences || [])].filter(item => item.model === model.value).map(item => [item.id, item])).values()])
function fieldValue(field: string) {
  const value = (metadata.value as Record<string, unknown> | undefined)?.[field]
  if (value == null) return unknown.value
  if (typeof value === 'boolean') return t(`admin.channels.insights.${value ? 'supported' : 'unsupported'}`)
  if (Array.isArray(value)) {
    const text = value.length ? value.join(', ') : t('admin.channels.insights.knownEmpty')
    return field === 'input_modalities' && metadata.value?.input_modalities_status === 'partial' ? `${text} (${t('admin.channels.insights.partial')})` : text
  }
  if (typeof value === 'object') return JSON.stringify(value)
  return `${value}${field.includes('tokens') || field.endsWith('context_window') ? ' tokens' : ''}${field === 'context_window' ? ` (${metadata.value?.context_limit_kind || unknown.value})` : ''}`
}
function evidence(field: string) {
  const source = metadata.value?.field_evidence?.[field] as ({ source: string; confidence?: string; provider_profile?: string; matched_model_id?: string; match_rule?: string; source_version?: string; content_hash?: string; observed_at?: string; stale?: boolean } | undefined)
  return source ? [source.source, source.confidence, source.provider_profile, source.matched_model_id, source.match_rule, source.source_version || source.content_hash, source.observed_at, source.stale ? t('capabilityCatalog.stale') : ''].filter(Boolean).join(' · ') : unknown.value
}
function quoteText(quote: PricingQuote) {
  return `${quote.value ?? unknown.value} ${quote.currency || unknown.value}/${quote.unit || unknown.value} · ${quote.scope || unknown.value} · ${quote.conditions || unknown.value}`
}
async function loadAccounts() {
  accountId.value = ''
  const current = ++accountRequest
  catalogRequest++; clearPreview(); loadingCatalog.value = false
  loadingAccounts.value = true; error.value = ''; catalog.value = null; accounts.value = []
  try {
    const found = new Map<number, { id: number; name: string }>()
    for (const groupId of props.groupIds) {
      let page = 1
      let total = 0
      do {
        const response = await list(page, 100, { group: String(groupId), platform: props.platform, lite: 'true' })
        if (current !== accountRequest) return
        for (const account of response.items) found.set(account.id, { id: account.id, name: account.name })
        total = response.total
        if (!response.items.length) break
        page++
      } while ((page - 1) * 100 < total)
    }
    accounts.value = [...found.values()]
  } catch (cause) {
    if (current === accountRequest) error.value = extractApiErrorMessage(cause, t('admin.channels.insights.failed'))
  } finally { if (current === accountRequest) loadingAccounts.value = false }
}
function onToggle(event: Event) {
  opened.value = (event.target as HTMLDetailsElement).open
  if (opened.value) void loadAccounts()
  else { accountRequest++; catalogRequest++; clearPreview(); loadingAccounts.value = false; loadingCatalog.value = false; catalog.value = null }
}
watch(accountId, async id => {
  const current = ++catalogRequest
  clearPreview(); catalog.value = null; error.value = ''; loadingCatalog.value = false
  if (!id) return
  loadingCatalog.value = true
  try {
    const response = await getModelCatalog(Number(id))
    if (current === catalogRequest) catalog.value = response
  } catch (cause) {
    if (current === catalogRequest) error.value = extractApiErrorMessage(cause, t('admin.channels.insights.failed'))
  } finally { if (current === catalogRequest) loadingCatalog.value = false }
}, { flush: 'sync' })
watch(() => [props.platform, props.groupIds], () => { sampleGroupId.value = props.groupIds[0]; if (opened.value) void loadAccounts() }, { deep: true })
watch(() => props.entry, () => { clearPreview(); if (!props.entry.models.includes(model.value)) model.value = props.entry.models[0] || '' }, { deep: true })
watch([model, candidate, inputTokens, outputTokens, sampleGroupId], clearPreview)
onBeforeUnmount(() => { accountRequest++; catalogRequest++; previewRequest++ })
async function preview(withSource: boolean) {
  const current = ++previewRequest
  previewing.value = true; error.value = ''; result.value = null
  const entry = props.entry
  try {
    const response = await previewPricing({
      model: model.value, profile: metadata.value?.provider_profile || '', task_type: metadata.value?.task_type || '',
      pricing: {
        ...copyPricingFacts(entry),
        platform: props.platform, models: [...entry.models], billing_mode: entry.billing_mode,
        input_price: mTokToPerToken(entry.input_price), output_price: mTokToPerToken(entry.output_price),
        cache_write_price: mTokToPerToken(entry.cache_write_price), cache_read_price: mTokToPerToken(entry.cache_read_price),
        cache_write_1h_price: mTokToPerToken(entry.cache_write_1h_price ?? null),
        image_input_price: mTokToPerToken(entry.image_input_price), image_output_price: mTokToPerToken(entry.image_output_price),
        per_request_price: entry.per_request_price === '' || entry.per_request_price == null ? null : Number(entry.per_request_price),
        intervals: formIntervalsToAPI(entry.intervals), time_pricing: formTimePricingToAPI(entry.time_pricing)
      },
      ...(withSource ? { candidate: JSON.parse(candidate.value), group_id: sampleGroupId.value, input_tokens: inputTokens.value, output_tokens: outputTokens.value } : {})
    })
    if (current === previewRequest) result.value = response
  } catch (cause) {
    if (current === previewRequest) error.value = extractApiErrorMessage(cause, t('admin.channels.insights.failed'))
  } finally { if (current === previewRequest) previewing.value = false }
}
</script>

<style scoped>
:is(input, select, textarea).input {
  min-height: 40px;
  min-width: 0;
  border-radius: 8px;
  border-color: var(--field-border, var(--input));
  background: var(--card);
  color: var(--foreground);
  box-shadow: none;
  transition: border-color 150ms ease, background-color 150ms ease;
}

:is(input, select, textarea).input::placeholder { color: var(--muted-foreground); }
:is(input, select, textarea).input:disabled { background: var(--surface-2); }
:is(input, select, textarea).input:focus {
  border-color: var(--field-border, var(--brand));
  outline: 2px solid color-mix(in srgb, var(--field-border, var(--brand)) 22%, transparent);
  outline-offset: 2px;
  box-shadow: none;
}

.input-error { --field-border: var(--bad); }
label.input-label { color: var(--foreground); }
p.input-hint { color: var(--muted-foreground); }

:is(button, a).btn {
  min-height: 40px;
  border-radius: 8px;
  background-image: none;
  box-shadow: none;
  transform: none;
  transition: background-color 150ms ease, border-color 150ms ease;
}

.btn.btn-primary { background: var(--primary); color: var(--primary-foreground); }
.btn.btn-primary:hover:not(:disabled) { background: color-mix(in srgb, var(--primary) 90%, var(--card)); }
.btn.btn-outline { border-color: var(--border); background: var(--card); color: var(--foreground); }
.btn.btn-outline:hover:not(:disabled) { background: var(--surface-2); }

div.card {
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--card);
  color: var(--foreground);
  box-shadow: none;
}

button:focus-visible, a:focus-visible, summary:focus-visible {
  outline: 2px solid var(--brand);
  outline-offset: 3px;
}

input[type='checkbox'] { accent-color: var(--brand); }

@media (prefers-reduced-motion: reduce) {
  :is(button, a).btn, :is(input, select, textarea).input, .fade-enter-active, .fade-leave-active,
  .agreement-fade-enter-active, .agreement-fade-leave-active,
  .agreement-fade-enter-active > div, .agreement-fade-leave-active > div {
    transition: none;
  }
}
</style>
