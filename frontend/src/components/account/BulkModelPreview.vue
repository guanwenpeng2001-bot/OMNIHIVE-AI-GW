<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { accountsAPI, type SyncUpstreamModelsResult } from '@/api/admin/accounts'
import { modelSyncErrorKey, translateModelRestrictionErrors, validateModelRestrictions, type ModelMappingEntry } from '@/composables/useModelWhitelist'
import ModelCatalogDetails from './ModelCatalogDetails.vue'

const props = defineProps<{
  connectionContext?: unknown
  active: boolean
  accountIds: number[]
  filtered: boolean
  modelValue: string[]
  modelMappings: ModelMappingEntry[]
}>()
const emit = defineEmits<{ 'update:modelValue': [models: string[]] }>()
const { t } = useI18n()
type PreviewRow = { accountId: number; state: 'idle' | 'loading' | 'ready' | 'failed'; result?: SyncUpstreamModelsResult; errorKey?: string }
const rows = ref<PreviewRow[]>([])
const running = ref(false)
const selectionError = ref('')
let generation = 0
let stopRequested = false
watch(() => JSON.stringify([props.active, props.filtered, props.accountIds, props.connectionContext]), () => {
  generation++
  running.value = false
  stopRequested = false
  selectionError.value = ''
  rows.value = props.active && !props.filtered
    ? [...new Set(props.accountIds)].map(accountId => ({ accountId, state: 'idle' })) : []
}, { immediate: true, flush: 'sync' })
onUnmounted(() => { generation++ })
const complete = computed(() => rows.value.length > 0 && rows.value.every(row => row.state === 'ready' && !!row.result))
const commonModels = computed(() => {
  if (!complete.value) return []
  const modelSets = rows.value.map(row => new Set((row.result!.candidate_models ?? row.result!.discovered_models ?? []).map(model => model.trim()).filter(Boolean)))
  return [...modelSets[0]].filter(model => modelSets.every(models => models.has(model)))
})
const additions = computed(() => commonModels.value.filter(model => !props.modelValue.includes(model)))
const preview = async (accountId?: number) => {
  if (running.value || !props.active || props.filtered) return
  const requestGeneration = generation
  stopRequested = false
  running.value = true
  if (accountId === undefined) {
    for (const row of rows.value) { row.state = 'idle'; row.errorKey = undefined }
  }
  try {
    for (const row of rows.value) {
      if (stopRequested || requestGeneration !== generation) break
      if (accountId !== undefined && row.accountId !== accountId) continue
      row.state = 'loading'
      row.errorKey = undefined
      try {
        const result = await accountsAPI.syncUpstreamModels(row.accountId, false)
        if (requestGeneration !== generation) return
        row.result = result
        row.state = 'ready'
      } catch (error) {
        if (requestGeneration !== generation) return
        row.state = 'failed'
        row.errorKey = modelSyncErrorKey(error)
      }
    }
  } finally {
    if (requestGeneration === generation) running.value = false
  }
}
const addCommonModels = () => {
  if (!complete.value || running.value || !props.active) return
  const models = [...new Set([...props.modelValue, ...commonModels.value])]
  selectionError.value = translateModelRestrictionErrors(validateModelRestrictions('combined', models, props.modelMappings), t)
  if (!selectionError.value && models.length !== props.modelValue.length) emit('update:modelValue', models)
}
</script>

<template>
  <section class="mb-3 space-y-2" data-testid="bulk-model-preview">
    <p class="text-sm text-secondary">{{ t(filtered ? 'admin.accounts.modelSync.bulkSelectIds' : 'admin.accounts.modelSync.bulkSavedScope') }}</p>
    <div class="flex flex-wrap gap-2">
      <button type="button" class="btn btn-secondary" data-testid="preview-each-account" :disabled="!active || filtered || running || !rows.length" @click="preview()">{{ t('admin.accounts.modelSync.previewEach') }}</button>
      <button v-if="running" type="button" class="btn btn-secondary" data-testid="stop-account-previews" @click="stopRequested = true">{{ t('admin.accounts.modelSync.stopPreviews') }}</button>
      <button type="button" class="btn btn-secondary" data-testid="add-common-models" :disabled="!active || running || !complete || !additions.length" @click="addCommonModels">{{ t('admin.accounts.modelSync.addCommon', { count: commonModels.length }) }}</button>
    </div>
    <p v-if="selectionError" role="alert">{{ selectionError }}</p>
    <p v-if="complete && !commonModels.length" role="status">{{ t('admin.accounts.modelSync.noCommonModels') }}</p>
    <p v-else-if="rows.length && !complete" class="text-sm text-secondary">{{ t('admin.accounts.modelSync.commonRequiresComplete') }}</p>
    <div class="max-h-80 space-y-2 overflow-y-auto">
      <div v-for="row in rows" :key="row.accountId" :data-account-id="row.accountId" class="rounded border p-2">
        <div class="flex items-center justify-between gap-2">
          <span>#{{ row.accountId }} · {{ t(`admin.accounts.modelSync.batchState.${row.state}`) }}</span>
          <button type="button" class="btn btn-secondary" :disabled="!active || running" @click="preview(row.accountId)">{{ t(row.state === 'failed' ? 'common.tryAgain' : 'admin.accounts.syncUpstreamModels') }}</button>
        </div>
        <p v-if="row.errorKey" role="alert">{{ t(`admin.accounts.${row.errorKey}`) }}</p>
        <p v-if="row.result" class="break-all text-sm">{{ (row.result.candidate_models ?? row.result.discovered_models ?? []).join(', ') }}</p>
        <p v-if="row.result && row.state !== 'ready'" role="status">{{ t('admin.accounts.modelSync.previousPreview') }}</p>
        <ModelCatalogDetails v-if="row.result" :stale="row.state !== 'ready'" :result="row.result" :candidate-count="null" />
      </div>
    </div>
  </section>
</template>
