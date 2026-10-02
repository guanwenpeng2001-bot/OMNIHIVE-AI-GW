<template>
  <div @keydown.esc="handleEscape">
    <p v-if="presetError" role="alert" class="mb-3 text-sm text-red-600">
      {{ presetError }} <button type="button" class="underline" @click="reloadPresets">{{ t('common.refresh') }}</button>
    </p>
    <div v-if="!catalogOnly" class="mb-4 space-y-1">
      <p data-testid="configured-model-summary" class="text-sm font-medium text-gray-700 dark:text-gray-200">{{ t('admin.accounts.modelSync.configuredSelection', { direct: selectedModels.length, mappings: modelMappings?.length ?? 0, wildcards: wildcardCount }) }}</p>
      <p v-if="!configuredModels.length" class="input-hint">{{ t('admin.accounts.modelSync.emptySelection') }}</p>
    </div>
    <!-- Candidate sources never replace the current configuration. -->
    <div class="mb-4 flex flex-wrap gap-2">
      <button
        type="button"
        v-if="!catalogOnly"
        @click="fillRelated"
        class="btn btn-secondary btn-sm"
      >
        {{ t('admin.accounts.fillRelatedModels') }}
      </button>
      <button
        v-if="canSyncUpstream"
        type="button"
        @click="syncUpstreamModels()"
        :disabled="isSyncingUpstream || selectionBusy || !!previewDisabledReason || !active"
        :aria-describedby="previewDisabledReason ? previewReasonId : undefined"
        data-testid="preview-upstream-models"
        class="btn btn-secondary btn-sm"
      >
        {{ isSyncingUpstream ? t('admin.accounts.syncUpstreamModelsLoading') : t('admin.accounts.syncUpstreamModels') }}
      </button>
      <button v-if="(!catalogOnly || catalogSelectionEnabled) && syncResult" type="button" data-testid="add-preview-models"
        :disabled="!canAddPreview || !previewModels.length" @click="addPreviewModels"
        class="btn btn-secondary btn-sm">
        {{ t(previewModels.length && !previewAdditions.length ? 'admin.accounts.modelSync.previewAlreadyAdded' : 'admin.accounts.modelSync.addPreviewModels', { count: previewAdditions.length }) }}
      </button>
    </div>
    <p v-if="catalogState === 'idle' && accountId" role="status" class="mb-2 text-sm text-secondary">{{ t('admin.accounts.modelSync.unknown') }}</p>
    <p v-if="catalogState !== 'idle' && !syncResult && !catalogSummary" role="status" class="mb-2 text-sm text-secondary">{{ t(`admin.accounts.modelCatalog.${catalogState}`) }}</p>
    <button v-if="catalogState === 'failed' || catalogState === 'stale'" type="button" class="btn btn-secondary btn-sm mb-3" @click="accountId ? loadCatalog() : syncUpstreamModels()">{{ t('common.tryAgain') }}</button>

    <p v-if="syncError" role="alert" class="mb-2 text-sm">{{ syncError }}</p>
    <p v-if="syncResult && needsPreview" class="mb-2 text-sm text-secondary">{{ t('admin.accounts.modelSync.previewCandidates') }}</p>
    <p v-if="previewDisabledReason && canSyncUpstream" :id="previewReasonId" class="mb-2 text-sm text-secondary">{{ previewDisabledReason }}</p>
    <p v-if="configurationDirty && accountId" :id="dirtyReasonId" class="mb-2 text-sm text-secondary">{{ t('admin.accounts.modelSync.saveBeforePreview') }}</p>

    <p v-if="denyAll" role="status" data-testid="deny-all">{{ t('admin.accounts.modelSync.denyAll') }}</p>
    <p v-if="selectionWarning" role="status" data-testid="selection-warning">{{ selectionWarning }}</p>
    <ConfirmDialog :show="confirmClear" :title="t('admin.accounts.clearAllModels')" :message="t('admin.accounts.modelSync.clearConfirmation')"
      @confirm="clearAll" @cancel="confirmClear = false" />

    <details v-if="!catalogOnly" ref="selectionDetails" open class="mb-4 space-y-3" data-testid="model-selection">
      <summary class="cursor-pointer rounded-lg py-2 font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500">{{ t('admin.accounts.editSections.selection') }}</summary>
    <!-- Multi-select Dropdown -->
    <div v-if="!catalogOnly" class="mb-3">
      <div
        class="cursor-pointer rounded-lg border border-gray-300 bg-white px-3 py-2 dark:border-dark-500 dark:bg-dark-700"
      >
        <div v-if="configuredModels.length" data-testid="selected-models" class="flex max-h-[min(12rem,30vh)] flex-wrap gap-1.5 overflow-y-auto">
          <section v-for="group in selectedGroups" :key="group.category" :data-model-category="group.category" class="w-full space-y-1">
            <h3 class="text-xs font-semibold">{{ t(`admin.accounts.modelCategories.${group.category}`) }}</h3>
          <span
            v-for="model in group.models"
            :key="model"
            class="inline-flex min-w-0 max-w-full items-center justify-between gap-1 rounded bg-gray-100 px-2 py-1 text-xs text-gray-700 dark:bg-dark-600 dark:text-gray-300"
          >
            <span class="flex min-w-0 items-center gap-1">
              <ModelIcon :model="model" size="14px" />
              <span class="min-w-0 break-all">{{ mappingTargets.has(model) ? model : modelDisplayName(model) }}<small v-if="!mappingTargets.has(model) && modelDisplayName(model) !== model" class="block text-secondary">{{ model }}</small><small v-for="target in mappingTargets.get(model)" :key="target" class="block text-secondary">→ {{ target }}</small><small class="block">{{ modelSource(model) }}</small></span>
            </span>
            <button
              type="button"
              :aria-label="`${t('common.remove')} ${model}`"
              @click.stop="removeModel(model)"
              class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full hover:bg-gray-200 dark:hover:bg-dark-500"
            >
              <Icon name="x" size="xs" class="h-3.5 w-3.5" :stroke-width="2" />
            </button>
          </span>
          </section>
        </div>
        <button ref="trigger" type="button" :aria-expanded="showDropdown" :aria-controls="dropdownId" :aria-label="t('admin.accounts.selectModels')" @click="toggleDropdown" class="flex w-full items-center justify-between py-2" :class="{ 'mt-2 border-t border-gray-200 dark:border-dark-600': configuredModels.length }">
          <span class="text-xs text-secondary">{{ t('admin.accounts.modelSync.expandModels') }}</span>
          <svg class="h-5 w-5 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
          </svg>
        </button>
      </div>
      <!-- Dropdown List -->
      <div
        v-if="showDropdown"
        :id="dropdownId"
        class="mt-1 rounded-lg border border-gray-200 bg-white shadow-lg dark:border-dark-600 dark:bg-dark-700"
      >
        <div class="max-h-24 overflow-y-auto border-b border-gray-200 bg-white p-2 dark:border-dark-600 dark:bg-dark-700">
          <input
            v-model="searchQuery"
            type="text"
            class="input w-full text-sm"
            :aria-label="t('admin.accounts.searchModels')"
            :placeholder="t('admin.accounts.searchModels')"
            @click.stop
          />
        </div>
        <div v-if="filteredModels.length > 100" class="flex items-center gap-2 p-2">
          <span role="status">{{ t('capabilityCatalog.candidateProgress', { shown: Math.min(optionLimit, filteredModels.length), matched: filteredModels.length }) }}</span>
          <button v-if="optionLimit < filteredModels.length" type="button" data-testid="model-options-more" class="btn btn-secondary" @click="optionLimit += 100">{{ t('capabilityCatalog.more') }}</button>
        </div>
        <button type="button" data-testid="select-filtered-models" class="btn btn-secondary m-2" :disabled="!filteredModels.length" @click="selectFilteredModels">{{ t('capabilityCatalog.selectFiltered') }}</button>
        <div data-testid="model-options" class="max-h-[min(18rem,40vh)] overflow-y-auto">
          <section v-for="group in groupedModels" :key="group.category" :data-model-category="group.category">
            <h3 class="bg-gray-50 px-3 py-1.5 text-xs font-semibold text-secondary dark:bg-dark-800">{{ t(`admin.accounts.modelCategories.${group.category}`) }}</h3>
            <div
              v-for="model in group.models"
              :key="model.value"
              data-testid="model-option"
              class="group flex items-center hover:bg-gray-100 dark:hover:bg-dark-600"
            >
              <button
                type="button"
                data-testid="select-model"
                :aria-pressed="configuredModelKeys.has(model.value)"
                class="flex min-w-0 flex-1 items-center gap-2 px-3 py-2 text-left text-sm"
                @click="toggleModel(model.value)"
              >
                <span
                  :class="[
                    'flex h-4 w-4 shrink-0 items-center justify-center rounded border',
                    configuredModelKeys.has(model.value)
                      ? 'border-primary-500 bg-primary-500 text-white'
                      : 'border-gray-300 dark:border-dark-500'
                  ]"
                >
                  <svg v-if="configuredModelKeys.has(model.value)" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M5 13l4 4L19 7" />
                  </svg>
                </span>
                <ModelIcon :model="model.value" size="18px" />
                <span class="min-w-0 break-all text-gray-900 dark:text-white">{{ modelDisplayName(model.value) }}<small v-if="modelDisplayName(model.value) !== model.value" class="block text-xs text-secondary">{{ model.value }}</small><small class="block text-xs text-secondary">{{ modelSource(model.value) }}</small></span>
              </button>
              <button
                type="button"
                data-testid="copy-model-id"
                class="mr-2 rounded p-1.5 text-secondary opacity-70 transition-colors hover:bg-gray-200 hover:text-primary-600 focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-focus group-hover:opacity-100 dark:hover:bg-dark-500 dark:hover:text-primary-400"
                :title="`${t('common.copy')} ${model.value}`"
                :aria-label="`${t('common.copy')} ${model.value}`"
                @click="copyModelId(model.value)"
              >
                <Icon name="copy" size="sm" />
              </button>
            </div>
          </section>
          <div v-if="filteredModels.length === 0" class="px-3 py-4 text-center text-sm text-secondary">
            {{ t('admin.accounts.noMatchingModels') }}
          </div>
        </div>
      </div>
    </div>

    <!-- Custom Model Input -->
    <div v-if="!catalogOnly" class="mb-3">
      <label :for="customModelId" class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.accounts.customModelName') }}</label>
      <div class="flex gap-2">
        <input
          :id="customModelId"
          ref="customModelInput"
          v-model="customModel"
          :aria-label="t('admin.accounts.customModelName')"
          type="text"
          class="input min-w-0 flex-1"
          :placeholder="t('admin.accounts.enterCustomModelName')"
          @keydown.enter.prevent="handleEnter"
          @compositionstart="isComposing = true"
          @compositionend="isComposing = false"
        />
        <button
          type="button"
          @click="addCustom"
          class="btn btn-secondary"
        >
          {{ t('admin.accounts.addModel') }}
        </button>
      </div>
    </div>
    </details>
    <div v-if="!catalogOnly" class="mb-4 space-y-2">
      <div class="flex flex-wrap items-center gap-3">
        <button v-if="accountId" type="button" data-testid="save-model-selection" class="btn btn-primary w-full sm:w-auto"
          :aria-describedby="configurationDirty ? dirtyReasonId : undefined"
          :disabled="!active || configurationDirty || selectionBusy || !catalogSummary || (!modelValue.length && !modelMappings?.length)" @click="saveSelection(false)">
          {{ t(selectionBusy && !allowlistBusy ? 'admin.accounts.modelSync.savingSelection' : denyAll ? 'admin.accounts.modelSync.reselect' : 'admin.accounts.modelSync.saveSelection') }}
        </button>
        <p v-if="accountId" role="status" data-testid="model-save-status" class="text-sm text-secondary">{{ t(`admin.accounts.modelSync.${selectionStatus}`) }}</p>
      </div>
      <p class="input-hint">{{ t(accountId ? 'admin.accounts.modelSync.selectionPendingSave' : 'admin.accounts.modelSync.selectionPendingCreate') }}</p>
    </div>
    <details v-if="accountId && catalogSummary" class="mb-4 space-y-3 border-t border-gray-200 pt-3 dark:border-dark-600" data-testid="catalog-allowlist">
      <summary class="cursor-pointer rounded-lg py-2 font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500">{{ t('admin.accounts.modelSync.allowlistTitle') }}</summary>
      <p class="input-hint">{{ t('admin.accounts.modelSync.allowlistHint') }}</p>
      <label class="flex gap-2"><input v-model="allowlistEnabled" type="checkbox" />{{ t('admin.accounts.modelSync.allowlistEnabled') }}</label>
      <label class="input-label">{{ t('admin.accounts.modelSync.allowlistModels') }}<textarea v-model="allowlistModels" class="input" data-testid="allowlist-models" /></label>
      <label class="input-label">{{ t('admin.accounts.modelSync.allowlistReason') }}<input v-model="allowlistReason" class="input" data-testid="allowlist-reason" /></label>
      <p v-if="allowlistEnabled && !allowlistModels.trim()" class="input-hint">{{ t('admin.accounts.modelSync.denyAll') }}</p>
      <button type="button" class="btn btn-secondary" data-testid="save-allowlist" :aria-describedby="configurationDirty ? dirtyReasonId : undefined" :disabled="!active || configurationDirty || selectionBusy || allowlistBusy" @click="saveAllowlist">{{ t(allowlistBusy ? 'common.saving' : 'common.save') }}</button>
    </details>
    <ModelCatalogDetails :display-metadata="displayMetadata" :mapping-targets="[...modelValue, ...(modelMappings ?? []).map(entry => entry.to)]" :creation-context="!accountId" :stale="catalogState === 'stale' || catalogState === 'invalidIdentity' || catalogSummary?.staleness === 'stale'" :status-override="catalogState === 'idle' ? undefined : catalogState" :result="syncResult" :summary="catalogSummary" :candidate-count="presetCount" />
    <div v-if="!catalogOnly" class="mt-4 flex justify-end border-t border-gray-200 pt-4 dark:border-dark-600">
      <button type="button" @click="confirmClear = true"
        data-testid="clear-model-selection" :disabled="selectionBusy || !active || (!!accountId && (configurationDirty || !catalogSummary))"
        class="btn btn-secondary border-red-200 text-red-600 dark:border-red-800 dark:text-red-400">
        {{ t('admin.accounts.clearAllModels') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { groupModelsByCategory, filterModelDisplay, limitModelGroups, type ModelCategoryMetadata } from '@/utils/modelCategories'
import { ref, computed, watch, onUnmounted, useId, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractApiErrorStatus } from '@/utils/apiError'
import { platformSupportsModelCatalog } from '@/constants/platforms'
import { accountsAPI } from '@/api/admin/accounts'
import { usePlatformModelPresets } from '@/composables/usePlatformModelPresets'
import type { Account, ModelAllowlist, AccountModelCatalogDetails } from '@/types'
import ModelCatalogDetails from './ModelCatalogDetails.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import type { SyncUpstreamModelsResult, SyncUpstreamPreviewParams } from '@/api/admin/accounts'
import { useClipboard } from '@/composables/useClipboard'
import ModelIcon from '@/components/common/ModelIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import { accountModelAllowlist, buildModelMappingObject, splitModelMappingObject, validateModelRestrictions, translateModelRestrictionErrors, type ModelRestrictionError, type ModelMappingEntry, type ModelCandidateIdentity, modelCandidateKey, modelSyncErrorKey } from '@/composables/useModelWhitelist'

const { t } = useI18n()

const props = withDefaults(defineProps<{
  modelAllowlist?: ModelAllowlist
  catalogOnly?: boolean
  catalogSelectionEnabled?: boolean
  discoveryDisabled?: boolean
  previewUnavailableReason?: string
  candidateIdentity?: ModelCandidateIdentity
  connectionContext?: unknown
  savedRevision?: unknown
  configurationDirty?: boolean
  modelMappings?: ModelMappingEntry[]
  active?: boolean
  modelValue: string[]
  platform?: string
  platforms?: string[]
  accountId?: number
  syncCredentials?: SyncUpstreamPreviewParams
}>(), { active: true })

const emit = defineEmits<{
  'update:modelValue': [value: string[]]
  'upstream-synced': []
  'catalog-applied': []
  'selection-saved': [account: Account]
  'update:modelMappings': [mappings: ModelMappingEntry[]]
  'validation-failed': [errors: ModelRestrictionError[]]
}>()

const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

const dropdownId = useId()
const customModelId = useId()
const previewReasonId = useId()
const dirtyReasonId = useId()
const trigger = ref<HTMLButtonElement>()
const selectionDetails = ref<HTMLDetailsElement>()
const customModelInput = ref<HTMLInputElement>()
function revealSelection() {
  if (selectionDetails.value) selectionDetails.value.open = true
  void nextTick(() => customModelInput.value?.focus())
}
const handleEscape = (event: KeyboardEvent) => {
  if (!showDropdown.value) return
  event.preventDefault()
  event.stopPropagation()
  showDropdown.value = false
  trigger.value?.focus()
}
const showDropdown = ref(false)
const searchQuery = ref('')
const optionLimit = ref(100)
watch(searchQuery, () => { optionLimit.value = 100 })
const customModel = ref('')
const isComposing = ref(false)
const isSyncingUpstream = ref(false)
let disposed = false
onUnmounted(() => { disposed = true })
const normalizedPlatforms = computed(() => {
  const rawPlatforms =
    props.platforms && props.platforms.length > 0
      ? props.platforms
      : props.platform
        ? [props.platform]
        : []

  return Array.from(
    new Set(
      rawPlatforms
        .map(platform => platform?.trim())
        .filter((platform): platform is string => Boolean(platform))
    )
  )
})

const canSyncUpstream = computed(() => !props.discoveryDisabled && (
  normalizedPlatforms.value.some(platformSupportsModelCatalog) ||
  Boolean(props.accountId && !normalizedPlatforms.value.length)
))
const previewDisabledReason = computed(() => {
  if (props.configurationDirty && props.accountId) return t('admin.accounts.modelSync.saveBeforePreview')
  if (!props.accountId && !props.syncCredentials) return props.previewUnavailableReason || t('admin.accounts.modelSync.credentialsRequired')
  return ''
})

// A synchronous generation fence also invalidates A→B→A changes and close/reopen.
const catalogMetadata = ref<Record<string, ModelCategoryMetadata>>({})
const catalog = ref<string[]>([])
const syncResult = ref<SyncUpstreamModelsResult | null>(null)
const catalogSummary = ref<AccountModelCatalogDetails | null>(null)
const syncError = ref('')
const needsPreview = ref(false)
const confirmClear = ref(false)
const selectionBusy = ref(false)
const selectionWarning = ref('')
const savedAllowlist = ref(props.modelAllowlist)
watch(() => props.modelAllowlist, value => { savedAllowlist.value = value }, { deep: true })
watch(() => [props.accountId, props.active], () => {
  selectionWarning.value = ''
  savedAllowlist.value = props.modelAllowlist
})
const denyAll = computed(() => savedAllowlist.value?.enabled === true && !savedAllowlist.value.models?.length)
const catalogState = ref<'idle' | 'loading' | 'failed' | 'empty' | 'stale' | 'ready' | 'unsynced' | 'invalidIdentity'>('idle')
let generation = 0
let selectionGeneration = 0
const context = computed(() => {
  const credentials = props.syncCredentials ? { ...props.syncCredentials } : undefined
  if (credentials) delete credentials.model_mapping
  return JSON.stringify([props.active !== false, props.accountId, normalizedPlatforms.value.map(platform => modelCandidateKey(platform, props.candidateIdentity)), credentials, props.connectionContext, props.savedRevision])
})
const selectionContext = computed(() => JSON.stringify([props.modelValue, props.modelMappings, props.syncCredentials?.model_mapping, props.configurationDirty]))
function selectionKey(models: string[], mappings: ModelMappingEntry[]) {
  return JSON.stringify([...models.map(model => [model, model]), ...mappings.map(mapping => [mapping.from, mapping.to])]
    .sort((left, right) => JSON.stringify(left).localeCompare(JSON.stringify(right))))
}
const selectionFingerprint = computed(() => selectionKey(props.modelValue, props.modelMappings ?? []))
const savedSelectionFingerprint = ref(selectionFingerprint.value)
const selectionSaved = ref(false)
const selectionSaveFailed = ref(false)
const selectionDirty = computed(() => selectionFingerprint.value !== savedSelectionFingerprint.value)
const selectionStatus = computed(() => selectionBusy.value && !allowlistBusy.value ? 'savingSelection' : selectionSaveFailed.value ? 'selectionSaveFailed' : selectionDirty.value ? 'unsavedSelection' : selectionSaved.value ? 'savedSelection' : 'unchangedSelection')
const wildcardCount = computed(() => props.modelMappings?.filter(mapping => mapping.from.includes('*')).length ?? 0)
watch(() => [props.accountId, props.active], async () => {
  selectionSaved.value = false
  selectionSaveFailed.value = false
  await nextTick()
  savedSelectionFingerprint.value = selectionFingerprint.value
}, { immediate: true })
watch(selectionFingerprint, () => { selectionSaveFailed.value = false })
const previewModels = computed(() => [...new Set((syncResult.value?.candidate_models ?? syncResult.value?.discovered_models ?? []).map(model => model.trim()).filter(Boolean))])
const previewAdditions = computed(() => previewModels.value.filter(model => !configuredModelKeys.value.has(model)))
const canAddPreview = computed(() => props.active && (!props.catalogOnly || props.catalogSelectionEnabled) && !!syncResult.value &&
  catalogState.value === 'ready' && previewAdditions.value.length > 0 && !isSyncingUpstream.value)
watch(selectionContext, () => {
  needsPreview.value = true
  confirmClear.value = false
}, { flush: 'sync' })
const allowlistReason = ref('')
const allowlistModels = ref('')
const allowlistEnabled = ref(true)
const allowlistBusy = ref(false)
// Read the persisted account after every mutation: legacy filter writes can be
// normalized to deny-all by the server and must never be shown as unrestricted.
const persistSelection = async (requestBody: Parameters<typeof accountsAPI.updateModelAllowlist>[1]) => {
  if (!props.accountId || !props.active || props.configurationDirty || selectionBusy.value) return
  const accountId = props.accountId
  const request = ++generation
  const selectionRequest = ++selectionGeneration
  let savedDraftContext = selectionContext.value
  let draftChanged = false
  selectionBusy.value = true
  selectionSaveFailed.value = false
  syncError.value = ''
  selectionWarning.value = ''
  try {
    const result = await accountsAPI.updateModelAllowlist(accountId, requestBody)
    if (disposed || request !== generation) return
    draftChanged = selectionContext.value !== savedDraftContext
    if (requestBody.model_mapping !== undefined && !draftChanged) {
      const selection = splitModelMappingObject(requestBody.model_mapping)
      emit('update:modelValue', selection.allowedModels)
      emit('update:modelMappings', selection.modelMappings)
    }
    await nextTick()
    savedDraftContext = selectionContext.value
    const account = await accountsAPI.getById(accountId)
    if (disposed || request !== generation) return
    if (requestBody.model_mapping !== undefined) {
      const persisted = splitModelMappingObject(account.credentials?.model_mapping as Record<string, unknown>)
      savedSelectionFingerprint.value = selectionKey(persisted.allowedModels, persisted.modelMappings)
      selectionSaved.value = true
    }
    savedAllowlist.value = accountModelAllowlist(account)
    allowlistEnabled.value = savedAllowlist.value?.enabled ?? false
    allowlistModels.value = savedAllowlist.value?.models?.join('\n') ?? ''
    if (result.not_discovered?.length) selectionWarning.value = t('admin.accounts.modelSync.notDiscoveredWarning', { models: result.not_discovered.join(', ') })
    if (requestBody.model_mapping !== undefined && !draftChanged && selectionContext.value === savedDraftContext) {
      const selection = splitModelMappingObject(account.credentials?.model_mapping as Record<string, unknown>)
      emit('update:modelValue', selection.allowedModels)
      emit('update:modelMappings', selection.modelMappings)
      emit('selection-saved', account)
    }
    emit('catalog-applied')
    await loadCatalog()
  } catch (error) {
    if (disposed || request !== generation) return
    selectionSaveFailed.value = requestBody.model_mapping !== undefined
    syncError.value = t(`admin.accounts.${extractApiErrorStatus(error) === 400 ? 'modelSync.invalidSelection' : modelSyncErrorKey(error)}`)
    // Refresh the CAS token; retain the user's draft and require an explicit retry.
    await loadCatalog()
  } finally { if (selectionRequest === selectionGeneration) selectionBusy.value = false }
}
const saveAllowlist = async () => {
  const revision = catalogSummary.value?.state?.rules_revision
  if (revision === undefined) return
  allowlistBusy.value = true
  const saveContext = context.value
  try {
    await persistSelection({ enabled: allowlistEnabled.value, models: [...new Set(allowlistModels.value.split(/[\s,]+/).filter(Boolean))],
      reason: allowlistReason.value.trim(), expected_rules_revision: revision })
  } finally { if (saveContext === context.value) allowlistBusy.value = false }
}
const saveSelection = async (clear: boolean) => {
  const revision = catalogSummary.value?.state?.rules_revision
  if (revision === undefined) return
  const errors = clear ? [] : validateModelRestrictions('combined', props.modelValue, props.modelMappings ?? [])
  if (errors.length) {
    syncError.value = translateModelRestrictionErrors(errors, t)
    if (errors[0].code === 'exactIdRequired') revealSelection()
    emit('validation-failed', errors)
    return
  }
  const mapping = clear ? {} : buildModelMappingObject('combined', props.modelValue, props.modelMappings ?? []) ?? {}
  // A normal selection save preserves a configured filter. Reselecting from
  // deny-all is the sole explicit operation that releases that state.
  const enabled = clear || (!denyAll.value && savedAllowlist.value?.enabled === true)
  await persistSelection({ enabled, models: clear || denyAll.value ? [] : savedAllowlist.value?.models ?? [],
    reason: '', expected_rules_revision: revision, model_mapping: mapping })
}
const loadCatalog = async () => {
  if (disposed || !props.accountId || props.active === false || props.configurationDirty) return
  const request = ++generation
  const accountId = props.accountId
  catalogState.value = 'loading'
  try {
    let modelListFailed = false
    const models = props.catalogOnly ? [] : await accountsAPI.getAvailableModels(accountId).catch(() => { modelListFailed = true; return [] })
    if (disposed || request !== generation) return
    catalogMetadata.value = Object.fromEntries(models.map(model => [model.id, model]))
    catalog.value = models.map(model => model.id)
    catalogState.value = catalog.value.length ? 'ready' : 'empty'
    try {
      const summary = await accountsAPI.getModelCatalog(accountId)
      if (disposed || request !== generation) return
      catalogSummary.value = summary
      catalog.value = [...new Set([...catalog.value, ...(summary.snapshot?.discovered_models ?? [])])]
      catalogState.value = catalog.value.length ? 'ready' : 'empty'
      catalogMetadata.value = { ...catalogMetadata.value, ...summary.display_metadata }
      if (props.catalogOnly) {
        catalog.value = summary.effective_snapshot?.eligible_models?.map(model => model.id) ?? []
        catalogState.value = catalog.value.length ? 'ready' : 'empty'
      }
      needsPreview.value = summary.warnings?.includes('identity_changed') || !!summary.last_error || summary.staleness === 'stale'
      if (summary.warnings?.includes('identity_changed')) catalogState.value = 'invalidIdentity'
      else if (summary.last_error) catalogState.value = 'stale'
      else if (modelListFailed) catalogState.value = 'failed'
      else if (!summary.synced) catalogState.value = 'unsynced'
    } catch {
      // Catalog errors are real failures, including 404; never probe another capabilities API.
      if (disposed || request !== generation) return
      catalogSummary.value = null
      catalogState.value = 'failed'
    }
  } catch {
    if (disposed || request !== generation) return
    catalogState.value = catalog.value.length ? 'stale' : 'failed'
  }
}
let catalogLoadScheduled = false
watch(context, () => {
  generation++
  selectionGeneration++
  selectionBusy.value = false
  allowlistBusy.value = false
  confirmClear.value = false
  catalog.value = []
  catalogMetadata.value = {}
  syncResult.value = null
  catalogSummary.value = null
  syncError.value = ''
  needsPreview.value = false
  catalogState.value = 'idle'
  isSyncingUpstream.value = false
  showDropdown.value = false
  optionLimit.value = 100
  allowlistReason.value = ''
  allowlistModels.value = props.modelAllowlist?.models?.join('\n') ?? ''
  allowlistEnabled.value = props.modelAllowlist?.enabled ?? false
  // Invalidate synchronously, but read only after all parent props (including
  // configurationDirty) settle. Vue updates those props one at a time.
  if (!catalogLoadScheduled) {
    catalogLoadScheduled = true
    void nextTick(() => {
      catalogLoadScheduled = false
      if (!isSyncingUpstream.value && !syncResult.value) void loadCatalog()
    })
  }
}, { immediate: true, flush: 'sync' })

const { presets: selectedPresets, models: presetModels, loaded: presetsLoaded, error: presetError, reload: reloadPresets } = usePlatformModelPresets(
  normalizedPlatforms, () => props.candidateIdentity ?? {}, () => props.active && !props.catalogOnly
)
const presetCount = computed(() => presetsLoaded.value && selectedPresets.value.length
  && !selectedPresets.value.some(preset => preset.coverage_status === 'unconfigured')
  && normalizedPlatforms.value.every(platform => selectedPresets.value.some(preset => preset.platform === platform))
  ? presetModels.value.length : null)
const availableOptions = computed(() => {
  return [...new Set([...configuredModels.value, ...presetModels.value, ...catalog.value])].map(value => ({ value, label: value }))
})

const displayMetadata = computed(() => {
  const metadata: Record<string, ModelCategoryMetadata> = Object.fromEntries(
    selectedPresets.value.flatMap(preset => (preset.models ?? []).map(model => [model.id, model]))
  )
  for (const model of catalogSummary.value?.snapshot?.eligible_models ?? []) metadata[model.id] = { ...metadata[model.id], ...model }
  for (const [id, model] of Object.entries(catalogMetadata.value)) metadata[id] = { ...metadata[id], ...model }
  for (const [id, model] of Object.entries(syncResult.value?.metadata ?? {})) metadata[id] = { ...metadata[id], ...model }
  for (const model of syncResult.value?.eligible_models ?? []) metadata[model.id] = { ...metadata[model.id], ...model }
  return metadata
})
const modelDisplayName = (id: string) => displayMetadata.value[id]?.display_name?.trim() || id
const selectedModels = computed(() => [...new Set(props.modelValue)])
// Request keys are selected independently of their targets. Keep mapping rows in
// the draft so showing a chip never converts an alias into an identity mapping.
const configuredModelKeys = computed(() => new Set([
  ...props.modelValue.map(model => model.trim()),
  ...(props.modelMappings ?? []).map(mapping => mapping.from.trim())
].filter(Boolean)))
const configuredModels = computed(() => [...configuredModelKeys.value])
const mappingTargets = computed(() => {
  const targets = new Map<string, string[]>()
  for (const mapping of props.modelMappings ?? []) {
    const from = mapping.from.trim()
    const to = mapping.to.trim()
    if (!from || from === to) continue
    targets.set(from, [...new Set([...(targets.get(from) ?? []), to])])
  }
  return targets
})
const selectedGroups = computed(() => groupModelsByCategory(
  filterModelDisplay(configuredModels.value, '', model => model, model => displayMetadata.value[model]), model => displayMetadata.value[model]))
const filteredModels = computed(() => filterModelDisplay(availableOptions.value, searchQuery.value, model => model.value, model => displayMetadata.value[model.value]))
const groupedModels = computed(() => limitModelGroups(groupModelsByCategory(filteredModels.value, model => displayMetadata.value[model.value]), optionLimit.value))
const modelSource = (id: string) => {
  const sources: string[] = []
  if (presetModels.value.includes(id)) sources.push(t('admin.accounts.modelSync.platformCandidates'))
  const discovered = syncResult.value?.discovered_models ?? catalogSummary.value?.snapshot?.discovered_models ?? []
  if (discovered.includes(id)) sources.push(t('admin.accounts.modelSync.nativeDiscovered'))
  if (!sources.length) sources.push(t('admin.accounts.modelSync.manualSelection'))
  return sources.join(' · ')
}
const selectFilteredModels = () => emit('update:modelValue', [...new Set([...props.modelValue, ...filteredModels.value.map(model => model.value).filter(model => !configuredModelKeys.value.has(model))])])

const toggleDropdown = () => {
  showDropdown.value = !showDropdown.value
  if (!showDropdown.value) searchQuery.value = ''
}

const removeModel = (model: string) => {
  if (configuredModelKeys.value.size === 1) { confirmClear.value = true; return }
  if (props.modelValue.some(value => value.trim() === model)) {
    emit('update:modelValue', props.modelValue.filter(value => value.trim() !== model))
  }
  if (props.modelMappings?.some(mapping => mapping.from.trim() === model)) {
    emit('update:modelMappings', props.modelMappings.filter(mapping => mapping.from.trim() !== model))
  }
}

const toggleModel = (model: string) => {
  if (configuredModelKeys.value.has(model)) {
    removeModel(model)
  } else {
    emit('update:modelValue', [...props.modelValue, model])
  }
}

const copyModelId = async (model: string) => {
  await copyToClipboard(model)
}

const addCustom = () => {
  const model = customModel.value.trim()
  if (!model) return
  if (configuredModelKeys.value.has(model)) {
    appStore.showInfo(t('admin.accounts.modelExists'))
    return
  }
  emit('update:modelValue', [...props.modelValue, model])
  customModel.value = ''
}

const handleEnter = () => {
  if (!isComposing.value) addCustom()
}

const fillRelated = () => {
  const newModels = [...props.modelValue]
  for (const model of presetModels.value) {
    if (!configuredModelKeys.value.has(model) && !newModels.includes(model)) newModels.push(model)
  }
  emit('update:modelValue', newModels)
}

const addPreviewModels = () => {
  if (!canAddPreview.value) return
  if (!previewAdditions.value.length) {
    appStore.showInfo(t('admin.accounts.modelSync.previewAlreadyAdded'))
    return
  }
  const nextModels = [...new Set([...props.modelValue, ...previewAdditions.value])]
  const errors = validateModelRestrictions('combined', nextModels, props.modelMappings ?? [])
  if (errors.length) {
    syncError.value = translateModelRestrictionErrors(errors, t)
    appStore.showError(syncError.value)
    return
  }
  const count = previewAdditions.value.length
  emit('update:modelValue', nextModels)
  appStore.showInfo(t('admin.accounts.modelSync.previewAdded', { count }))
}

const syncUpstreamModels = async () => {
  if (isSyncingUpstream.value || selectionBusy.value || !props.active || !canSyncUpstream.value || previewDisabledReason.value) return
  if (!props.accountId && !props.syncCredentials) return

  const request = ++generation
  const reviewedSelection = selectionContext.value
  isSyncingUpstream.value = true
  syncError.value = ''
  catalogState.value = 'loading'
  try {
    let result
    if (props.accountId) {
      result = await accountsAPI.syncUpstreamModels(props.accountId, false)
    } else if (props.syncCredentials) {
      result = await accountsAPI.syncUpstreamModelsPreview(props.syncCredentials)
    } else {
      return
    }

    if (disposed || request !== generation) return
    syncResult.value = result
    needsPreview.value = reviewedSelection !== selectionContext.value
    const upstreamModels = (result.candidate_models ?? result.discovered_models ?? []).map(model => model.trim()).filter(Boolean)
    catalog.value = upstreamModels
    catalogState.value = upstreamModels.length ? 'ready' : 'empty'
    if (!props.accountId) emit('upstream-synced')

  } catch (error) {
    if (disposed || request !== generation) return
    catalogState.value = catalog.value.length ? 'stale' : 'failed'
    needsPreview.value = true
    syncError.value = t(`admin.accounts.${modelSyncErrorKey(error)}`)
    appStore.showError(syncError.value)
  } finally {
    if (!disposed && request === generation) isSyncingUpstream.value = false
  }
}

defineExpose({ requestClear: () => { confirmClear.value = true }, revealSelection })

const clearAll = async () => {
  if (!confirmClear.value || !props.active || selectionBusy.value) return
  confirmClear.value = false
  if (props.accountId) await saveSelection(true)
  else {
    emit('update:modelValue', [])
    emit('update:modelMappings', [])
  }
}

</script>
