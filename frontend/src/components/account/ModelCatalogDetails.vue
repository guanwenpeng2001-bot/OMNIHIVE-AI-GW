<template>
  <section v-if="result || summary" class="mb-3 space-y-2 rounded-lg border border-gray-200 p-3 text-sm text-gray-700 dark:border-dark-500 dark:text-gray-200" :aria-label="t('admin.accounts.modelSync.title')">
    <p v-if="summary?.staleness === 'stale' && result?.applied !== true" role="alert" class="font-semibold text-amber-700 dark:text-amber-400">{{ t('admin.accounts.modelCatalog.stale') }}</p>
    <p v-if="!summary && !result">{{ t('admin.accounts.modelSync.unknown') }}</p>
    <p v-if="summary && !result">{{ summary.warnings.includes('identity_changed') ? reasonText('identity_changed') : t(`admin.accounts.modelSync.${summary.synced ? (summary.last_error ? 'failedPrevious' : summary.eligible_count ? 'synced' : 'syncedEmpty') : 'notSynced'}`) }}</p>
    <p v-if="result">{{ t(`admin.accounts.modelSync.${result.applied === true ? ((result.models?.length ?? 0) ? 'synced' : 'syncedEmpty') : 'previewOnly'}`) }}</p>
    <p v-if="result && result.complete !== true">{{ t('admin.accounts.modelSync.incomplete') }}</p>
    <p v-for="warning in result?.warnings ?? summary?.warnings ?? []" :key="modelSyncWarningCode(warning)">{{ reasonText(modelSyncWarningCode(warning)) }}</p>
    <p v-if="summary?.last_error">{{ reasonText(summary.last_error) }}</p>
    <p v-if="result && !result.applied">{{ t('admin.accounts.modelSync.previewCount', { count: (result.models?.length ?? 0) }) }}</p>
    <p v-if="!stale && result?.diff?.removed?.length" role="alert">{{ t('admin.accounts.modelSync.removalRisk', { count: (result.diff.removed?.length ?? 0) }) }}</p>
    <p v-if="result?.invalid_aliases?.length" role="alert">{{ t('admin.accounts.modelSync.invalidAliases') }}: {{ result.invalid_aliases.join(', ') }}</p>
    <div v-if="result" data-testid="capability-sets" class="space-y-1">
      <p>{{ labels.sets }}: D {{ discoveredCount }} · M {{ mappingCount }} · U {{ unionCount }} · E {{ stale ? '—' : (result.models?.length ?? 0) }}</p>
      <p>{{ labels.setsHint }}</p>
      <p role="status" data-testid="capability-state">{{ capabilityState }}</p>
      <p>{{ labels.coverage }}: {{ completeCount }} / {{ metadataRows.length }} · {{ labels.estimated }} {{ estimatedCount }} · {{ labels.missing }} {{ metadataRows.length - completeCount - estimatedCount }}</p>
    </div>
    <div v-else-if="summary?.synced" data-testid="capability-sets" class="space-y-1">
      <p>{{ labels.sets }}: D {{ summary.discovered_count }} · M {{ savedCapabilities?.mapping_targets ?? '—' }} · U {{ savedCapabilities?.total ?? '—' }} · E {{ stale ? '—' : summary.eligible_count }}</p>
      <p>{{ labels.setsHint }}</p>
      <p v-if="savedCapabilities">{{ labels.coverage }}: {{ savedCapabilities.complete }} / {{ savedCapabilities.total }} · {{ labels.estimated }} {{ savedCapabilities.estimated }} · {{ labels.missing }} {{ savedCapabilities.incomplete }}</p>
    </div>
    <section v-if="metadataRows.length" class="space-y-2" :aria-label="labels.title">
      <h3 class="font-semibold">{{ labels.title }}</h3>
      <p v-if="stale" role="status" class="text-amber-700 dark:text-amber-400">{{ labels.stale }}</p>
      <div v-if="metadataRows.length > 20" class="flex flex-wrap gap-2">
        <input v-model="query" type="search" class="input" :aria-label="labels.search" :placeholder="labels.search" data-testid="capability-search" />
        <select v-model="statusFilter" class="input" :aria-label="labels.filter" data-testid="capability-filter">
          <option value="all">{{ labels.all }}</option><option value="complete">{{ labels.complete }}</option>
          <option value="estimated">{{ labels.estimated }}</option><option value="incomplete">{{ labels.missing }}</option>
          <option value="conflict">{{ labels.conflict }}</option>
        </select>
        <p role="status">{{ visibleMetadata.length }} / {{ filteredMetadata.length }}</p>
        <button v-if="visibleMetadata.length < filteredMetadata.length" type="button" class="btn btn-secondary" data-testid="capability-more" @click="visibleLimit += 50">{{ labels.more }}</button>
      </div>
      <div class="max-h-[min(24rem,50vh)] space-y-2 overflow-y-auto">
        <details v-for="entry in visibleMetadata" :key="entry.id" :open="expanded.has(entry.id)" @toggle="toggleDetails(entry.id, $event)" data-testid="capability-row"
          class="rounded-lg border border-gray-200 p-2 dark:border-dark-600">
          <summary class="cursor-pointer break-all font-medium">{{ entry.id }} · {{ entry.metadata_status === 'complete' ? labels.complete : entry.metadata_status === 'estimated' ? labels.estimated : entry.metadata_status === 'incomplete' ? labels.missing : labels.unknown }}<span v-if="entry.conflicts?.length"> · {{ labels.conflict }}</span></summary>
          <div v-if="expanded.has(entry.id)" class="mt-2 space-y-1 break-words text-xs" data-testid="capability-fields">
            <p v-if="exclusions.get(entry.id)">{{ t('admin.accounts.modelSync.excluded') }}: {{ reasonText(exclusions.get(entry.id)!) }}</p>
            <p v-if="entry.missing_fields?.length" class="font-semibold text-amber-700 dark:text-amber-400">{{ labels.missing }}: {{ entry.missing_fields.join(', ') }}</p>
            <p v-if="entry.estimated_fields?.length" class="text-amber-700 dark:text-amber-400">{{ labels.estimated }}: {{ entry.estimated_fields.join(', ') }}</p>
            <p v-for="conflict in entry.conflicts ?? []" :key="conflict" class="text-red-600 dark:text-red-400">{{ labels.conflict }}: {{ conflict }}</p>
            <p v-if="entry.evidence_kind">{{ t('admin.accounts.modelSync.evidenceKind') }}: {{ entry.evidence_kind }}</p>
            <p v-if="!entry.metadata_status">{{ labels.unavailable }}</p>
            <dl class="space-y-2">
              <div v-for="(source, field) in entry.field_evidence" :key="field" class="border-t border-gray-100 pt-1 dark:border-dark-600">
                <dt class="font-medium">{{ field }}: {{ fieldValue(entry, field) }}</dt>
                <dd>{{ labels.source }}: {{ sourceTier(source.source) }}<template v-if="source.confidence"> · {{ source.confidence }}</template></dd>
                <dd v-if="source.source_version || source.content_hash">{{ labels.version }}: {{ source.source_version || source.content_hash }}</dd>
                <dd v-if="source.provider_profile || source.matched_model_id || source.match_rule">{{ labels.match }}: {{ [source.provider_profile, source.matched_model_id, source.match_rule].filter(Boolean).join(' · ') }}</dd>
                <dd v-if="source.observed_at">{{ formatDateTime(source.observed_at) }}</dd>
              </div>
            </dl>
            <p v-if="!Object.keys(entry.field_evidence ?? {}).length">{{ labels.noSources }}</p>
          </div>
        </details>
      </div>
    </section>
    <details>
      <summary class="cursor-pointer py-2 font-medium">{{ t('admin.accounts.modelSync.technicalDetails') }}</summary>
      <div class="max-h-[min(16rem,40vh)] space-y-2 overflow-y-auto break-all">
        <dl class="grid grid-cols-2 gap-2">
          <div><dt>{{ t('admin.accounts.modelSync.platformCandidates') }}</dt><dd>{{ candidateCount ?? '—' }}</dd></div>
          <div><dt>{{ t('admin.accounts.modelSync.nativeDiscovered') }}</dt><dd>{{ result?.discovered_models?.length ?? (summary?.synced ? summary.discovered_count : '—') }}</dd></div>
          <div><dt>{{ t('admin.accounts.modelSync.accountEffective') }}</dt><dd>{{ stale ? '—' : result?.models?.length ?? (summary?.synced ? summary.eligible_count : '—') }}</dd></div>
          <div><dt>{{ t('admin.accounts.modelSync.groupPublic') }}</dt><dd>{{ publicCount }}</dd></div>
        </dl>
        <p>{{ t('admin.accounts.modelSync.candidatesHint') }}</p>
        <p v-if="result?.observed_at || summary?.observed_at">{{ t('admin.accounts.modelSync.observedAt') }}: {{ formatDateTime(result?.observed_at || summary?.observed_at) }}</p>
        <p v-if="result?.catalog_source || summary?.snapshot">{{ t('admin.accounts.modelSync.source') }}: {{ result?.catalog_source ?? summary?.snapshot?.source_kind }} / {{ result?.discovery_scope ?? summary?.snapshot?.discovery_scope ?? '—' }}</p>
        <section v-for="group in (metadataRows.length > 50 ? [] : evidenceGroups)" :key="group.category" :data-model-category="group.category">
          <h3 class="font-semibold">{{ t(`admin.accounts.modelCategories.${group.category}`) }}</h3>
          <ul class="list-inside list-disc">
            <li v-for="entry in group.models" :key="entry.id" class="break-all">{{ entry.id }} — {{ t('admin.accounts.modelSync.evidenceKind') }}: {{ entry.evidence_kind || '—' }}<template v-if="entry.evidence_observed_at">; {{ t('admin.accounts.modelSync.evidenceObservedAt') }}: {{ formatDateTime(entry.evidence_observed_at) }}</template></li>
          </ul>
        </section>
        <template v-if="!stale && result?.diff">
          <p>{{ t('admin.accounts.modelSync.added') }}: {{ result.diff.added?.join(', ') || '—' }}</p>
          <p :class="{ 'font-semibold': (result.diff.removed?.length ?? 0) > 0 }">{{ t('admin.accounts.modelSync.removed') }}: {{ result.diff.removed?.join(', ') || '—' }}</p>
        </template>
        <div v-if="result?.excluded_models?.length && metadataRows.length <= 50">
          <p>{{ t('admin.accounts.modelSync.excluded') }}</p>
          <ul class="list-inside list-disc"><li v-for="entry in result.excluded_models" :key="entry.model" class="break-all">{{ entry.model }} — {{ reasonText(entry.reason) }}</li></ul>
        </div>
        <p v-if="result?.invalid_aliases?.length">{{ t('admin.accounts.modelSync.invalidAliases') }}: {{ result.invalid_aliases.join(', ') }}</p>
        <p v-if="!creationContext">{{ t('admin.accounts.modelSync.publicDiffUnavailable') }}</p>
        <p v-if="!creationContext">{{ t('admin.accounts.modelSync.savedConfigHint') }}</p>
      </div>
    </details>
  </section>
</template>

<script setup lang="ts">
import { groupModelsByCategory } from '@/utils/modelCategories'
import { computed, ref, watch } from 'vue'
import { formatDateTime } from '@/utils/format'
import { modelSyncWarningCode } from '@/composables/useModelWhitelist'
import { useI18n } from 'vue-i18n'
import type { UpstreamModelMetadata } from '@/api/admin/accounts'
import type { AccountModelCatalog, AccountModelCatalogDetails } from '@/types'
const props = defineProps<{ mappingTargets?: string[]; creationContext?: boolean; stale?: boolean; result?: AccountModelCatalog | null; summary?: AccountModelCatalogDetails | null; candidateCount: number | null }>()
const { t, te } = useI18n()
const labels = computed(() => ({
  sets: t('capabilityCatalog.sets'),
  setsHint: t('capabilityCatalog.setsHint'),
  coverage: t('capabilityCatalog.coverage'),
  noDiscovery: t('capabilityCatalog.noDiscovery'),
  noEligibility: t('capabilityCatalog.noEligibility'),
  partial: t('capabilityCatalog.partial'),
  complete: t('capabilityCatalog.complete'),
  unknownDiscovery: t('capabilityCatalog.unknownDiscovery'),
  search: t('capabilityCatalog.search'),
  filter: t('capabilityCatalog.filter'),
  all: t('capabilityCatalog.all'),
  more: t('capabilityCatalog.more'),
  title: t('capabilityCatalog.title'),
  missing: t('capabilityCatalog.missing'),
  estimated: t('capabilityCatalog.estimated'),
  conflict: t('capabilityCatalog.conflict'),
  unknown: t('capabilityCatalog.unknown'),
  unavailable: t('capabilityCatalog.unavailable'),
  source: t('capabilityCatalog.source'),
  version: t('capabilityCatalog.version'),
  match: t('capabilityCatalog.match'),
  noSources: t('capabilityCatalog.noSources'),
  stale: t('capabilityCatalog.stale')
}))
const savedCapabilities = computed(() => props.summary?.snapshot?.capability_summary)
const mappingTargets = computed(() => [...new Set((props.mappingTargets ?? []).map(id => id.trim()).filter(id => id && !/[*?]/.test(id)))])
const discoveredCount = computed(() => props.result?.discovered_models ? new Set(props.result.discovered_models).size : '—')
const mappingCount = computed(() => props.mappingTargets ? mappingTargets.value.length : '—')
const unionCount = computed(() => props.result?.discovered_models && props.mappingTargets
  ? new Set([...props.result.discovered_models, ...mappingTargets.value]).size : '—')
const metadataRows = computed(() => [...new Set([
  ...(props.result?.discovered_models ?? props.summary?.snapshot?.discovered_models ?? []),
  ...mappingTargets.value,
  ...(props.result?.models ?? props.summary?.snapshot?.eligible_models?.map(model => model.id) ?? []),
  ...Object.keys(props.result?.metadata ?? {})
])].sort().map(id => ({ ...props.result?.metadata?.[id], id })))
const query = ref('')
const statusFilter = ref('all')
const visibleLimit = ref(50)
const expanded = ref(new Set<string>())
watch(() => props.result, () => { expanded.value = new Set(); visibleLimit.value = 50 })
watch([query, statusFilter], () => { visibleLimit.value = 50 })
const toggleDetails = (id: string, event: Event) => {
  const next = new Set(expanded.value)
  if ((event.target as HTMLDetailsElement).open) next.add(id)
  else next.delete(id)
  expanded.value = next
}
const completeCount = computed(() => metadataRows.value.filter(row => row.metadata_status === 'complete').length)
const estimatedCount = computed(() => metadataRows.value.filter(row => row.metadata_status === 'estimated').length)
const capabilityState = computed(() => {
  if (!props.result?.discovered_models) return labels.value.unknownDiscovery
  if (!props.result.discovered_models.length) return labels.value.noDiscovery
  if (!(props.result.models?.length ?? 0)) return labels.value.noEligibility
  return metadataRows.value.length === completeCount.value ? labels.value.complete : labels.value.partial
})
const filteredMetadata = computed(() => metadataRows.value.filter(row => {
  const matchesQuery = `${row.id} ${row.display_name ?? ''}`.toLowerCase().includes(query.value.trim().toLowerCase())
  const matchesStatus = statusFilter.value === 'all' || (statusFilter.value === 'conflict' ? !!row.conflicts?.length
    : statusFilter.value === 'incomplete' ? !row.metadata_status || row.metadata_status === 'incomplete'
    : row.metadata_status === statusFilter.value)
  return matchesQuery && matchesStatus
}))
const visibleMetadata = computed(() => filteredMetadata.value.slice(0, visibleLimit.value))
const exclusions = computed(() => new Map(props.result?.excluded_models?.map(entry => [entry.model, entry.reason]) ?? []))
const fieldValue = (entry: object, field: string) => {
  const value = (entry as Record<string, unknown>)[field]
  return value === undefined ? '—' : typeof value === 'object' ? JSON.stringify(value) : String(value)
}
const sourceTier = (source: string) => {
  if (source === 'upstream') return `T1 · ${source}`
  if (/^(litellm|supplement|account|cursor-builtin)/.test(source)) return `T2 · ${source}`
  if (source === 'models.dev') return `T3 · ${source}`
  if (source.startsWith('heuristic')) return `T4 · ${source}`
  return source
}
const reasonText = (reason: string) => {
  const key = `admin.accounts.modelSync.reasons.${reason}`
  return te?.(key) ? t(key) : reason
}
const evidence = computed<Array<Partial<UpstreamModelMetadata> & { id: string }>>(() => props.result
  ? [...new Set([...(props.result.models ?? []), ...Object.keys(props.result.metadata ?? {})])].map(id => ({ id, ...props.result?.metadata?.[id] }))
  : props.summary?.snapshot?.eligible_models ?? [])
const evidenceGroups = computed(() => groupModelsByCategory(evidence.value, model => model))
const publicCount = '—'
</script>
