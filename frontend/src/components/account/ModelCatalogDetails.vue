<template>
  <section v-if="result || summary" class="mb-4 space-y-3 rounded-lg border border-[var(--border)] p-3 text-xs bg-[var(--card)]" :aria-label="t('admin.accounts.modelSync.title')">
    <p role="status">{{ t(`admin.accounts.modelCatalog.${status}`) }}</p>
    <p v-if="result">{{ t('admin.accounts.modelSync.previewOnly') }}</p>
    <p v-if="result && !result.discovery_complete">{{ t('admin.accounts.modelSync.incomplete') }}</p>
    <dl class="grid grid-cols-1 gap-3 text-secondary sm:grid-cols-3 [&_dd]:mt-1 [&_dd]:text-base [&_dd]:font-semibold [&_dd]:text-gray-900 dark:[&_dd]:text-gray-100" data-testid="catalog-counts">
      <div><dt>{{ t('admin.accounts.modelSync.platformCandidates') }}</dt><dd>{{ candidateCount ?? '—' }}</dd></div>
      <div><dt>{{ t('admin.accounts.modelSync.nativeDiscovered') }}</dt><dd>{{ result?.discovered_models?.length ?? summary?.discovered_count ?? '—' }}</dd></div>
      <div><dt>{{ t('admin.accounts.modelSync.accountEffective') }}</dt><dd>{{ stale ? '—' : summary?.eligible_count ?? result?.eligible_models?.length ?? '—' }}</dd></div>
    </dl>
    <p>{{ t('admin.accounts.modelSync.candidatesHint') }}</p>
    <details v-if="capabilities.length" data-testid="capability-details">
      <summary class="cursor-pointer rounded py-2 font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500">{{ t('admin.accounts.modelSync.optionalCapabilities') }}</summary>
      <p>{{ t('admin.accounts.modelSync.capabilitiesHint') }}</p>
      <input v-model="query" type="search" class="input" :aria-label="t('admin.accounts.searchModels')" />
      <div class="mt-2 max-h-64 overflow-y-auto rounded-md border border-[var(--border)] bg-[var(--surface-2)]">
        <p v-for="entry in visibleCapabilities" :key="entry.id" class="break-all border-b border-[var(--border)] px-2 py-1.5 font-mono last:border-0">{{ entry.id }} — {{ entry.description }}</p>
      </div>
      <button v-if="filteredCapabilities.length > limit" type="button" class="btn btn-secondary" @click="limit += 50">{{ t('capabilityCatalog.more') }}</button>
    </details>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { modelCapabilitySummary, type ModelCategoryMetadata } from '@/utils/modelCategories'
import type { AccountModelCatalog, AccountModelCatalogDetails } from '@/types'

const props = defineProps<{
  displayMetadata?: Record<string, ModelCategoryMetadata>
  mappingTargets?: string[]
  creationContext?: boolean
  stale?: boolean
  statusOverride?: 'loading' | 'failed' | 'empty' | 'stale' | 'ready' | 'unsynced' | 'invalidIdentity'
  result?: AccountModelCatalog | null
  summary?: AccountModelCatalogDetails | null
  candidateCount: number | null
}>()
const { t } = useI18n()
const status = computed(() => {
  if (props.statusOverride && ['loading', 'failed', 'stale', 'invalidIdentity'].includes(props.statusOverride)) return props.statusOverride
  if (props.summary?.warnings?.includes('identity_changed')) return 'invalidIdentity'
  if (props.summary?.last_error) return 'failed'
  if (props.stale || props.summary?.staleness === 'stale') return 'stale'
  if (props.result) return (props.result.candidate_models?.length ?? 0) ? 'ready' : 'empty'
  if (!props.summary?.synced) return 'unsynced'
  return props.summary.eligible_count ? 'ready' : 'empty'
})
const capabilities = computed(() => Object.entries({ ...props.displayMetadata, ...props.summary?.display_metadata, ...props.result?.metadata })
  .map(([id, metadata]) => ({ id, description: modelCapabilitySummary(metadata, t) }))
  .filter(entry => entry.description))
const query = ref('')
const limit = ref(50)
watch(query, () => { limit.value = 50 })
const filteredCapabilities = computed(() => capabilities.value.filter(entry => entry.id.toLowerCase().includes(query.value.toLowerCase())))
const visibleCapabilities = computed(() => filteredCapabilities.value.slice(0, limit.value))
</script>
