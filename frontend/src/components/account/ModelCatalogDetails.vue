<template>
  <section v-if="result || summary" class="mb-3 space-y-2 rounded-lg border p-3 text-sm" :aria-label="t('admin.accounts.modelSync.title')">
    <p role="status">{{ t(`admin.accounts.modelCatalog.${status}`) }}</p>
    <p v-if="result">{{ t('admin.accounts.modelSync.previewOnly') }}</p>
    <p v-if="result && !result.discovery_complete">{{ t('admin.accounts.modelSync.incomplete') }}</p>
    <dl class="grid grid-cols-2 gap-2" data-testid="catalog-counts">
      <div><dt>{{ t('admin.accounts.modelSync.platformCandidates') }}</dt><dd>{{ candidateCount ?? '—' }}</dd></div>
      <div><dt>{{ t('admin.accounts.modelSync.nativeDiscovered') }}</dt><dd>{{ result?.discovered_models?.length ?? summary?.discovered_count ?? '—' }}</dd></div>
      <div><dt>{{ t('admin.accounts.modelSync.accountEffective') }}</dt><dd>{{ stale ? '—' : summary?.eligible_count ?? result?.eligible_models?.length ?? '—' }}</dd></div>
    </dl>
    <p>{{ t('admin.accounts.modelSync.candidatesHint') }}</p>
    <details v-if="capabilities.length" data-testid="capability-details">
      <summary>{{ t('admin.accounts.modelSync.optionalCapabilities') }}</summary>
      <p>{{ t('admin.accounts.modelSync.capabilitiesHint') }}</p>
      <input v-model="query" type="search" class="input" :aria-label="t('admin.accounts.searchModels')" />
      <div class="max-h-64 overflow-y-auto">
        <p v-for="entry in visibleCapabilities" :key="entry.id" class="break-all">{{ entry.id }} — {{ entry.description }}</p>
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
  result?: AccountModelCatalog | null
  summary?: AccountModelCatalogDetails | null
  candidateCount: number | null
}>()
const { t } = useI18n()
const status = computed(() => {
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
