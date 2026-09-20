<template>
  <div @keydown.esc="handleEscape">
    <p v-if="presetError" role="alert" class="mb-3 text-sm text-red-600">
      {{ presetError }} <button type="button" class="underline" @click="reloadPresets">{{ t('common.refresh') }}</button>
    </p>
    <!-- Quick Actions -->
    <div class="mb-4 flex flex-wrap gap-2">
      <button
        type="button"
        v-if="!catalogOnly"
        @click="fillRelated"
        class="rounded-lg border border-blue-200 px-3 py-1.5 text-sm text-blue-600 hover:bg-blue-50 dark:border-blue-800 dark:text-blue-400 dark:hover:bg-blue-900/30"
      >
        {{ t('admin.accounts.fillRelatedModels') }}
      </button>
      <button
        v-if="canSyncUpstream"
        type="button"
        @click="syncUpstreamModels(false)"
        :disabled="isSyncingUpstream || !!previewDisabledReason || !active"
        :aria-describedby="previewDisabledReason ? previewReasonId : undefined"
        data-testid="preview-upstream-models"
        class="rounded-lg border border-emerald-200 px-3 py-1.5 text-sm text-emerald-600 hover:bg-emerald-50 disabled:cursor-not-allowed disabled:opacity-60 dark:border-emerald-800 dark:text-emerald-400 dark:hover:bg-emerald-900/30"
      >
        {{ isSyncingUpstream ? t('admin.accounts.syncUpstreamModelsLoading') : t('admin.accounts.syncUpstreamModels') }}
      </button>
      <button v-if="(!catalogOnly || catalogSelectionEnabled) && syncResult" type="button" data-testid="add-preview-models"
        :disabled="!canAddPreview || !previewModels.length" @click="addPreviewModels"
        class="rounded-lg border border-emerald-200 px-3 py-1.5 text-sm text-emerald-700 disabled:opacity-60 dark:text-emerald-300">
        {{ t(previewModels.length && !previewAdditions.length ? 'admin.accounts.modelSync.previewAlreadyAdded' : 'admin.accounts.modelSync.addPreviewModels', { count: previewAdditions.length }) }}
      </button>
      <button
        type="button"
        v-if="!catalogOnly"
        @click="clearAll"
        class="rounded-lg border border-red-200 px-3 py-1.5 text-sm text-red-600 hover:bg-red-50 dark:border-red-800 dark:text-red-400 dark:hover:bg-red-900/30"
      >
        {{ t('admin.accounts.clearAllModels') }}
      </button>
    </div>

    <div v-if="!catalogOnly" class="mb-3 space-y-1">
      <p class="text-sm font-medium text-gray-700 dark:text-gray-200">{{ t('admin.accounts.selectedModels', { count: modelValue.length }) }}</p>
      <p v-if="!modelValue.length" class="text-xs text-secondary">{{ t('admin.accounts.supportsAllModels') }}</p>
      <p class="text-xs text-secondary">{{ t(accountId ? 'admin.accounts.modelSync.selectionPendingSave' : 'admin.accounts.modelSync.selectionPendingCreate') }}</p>
    </div>
    <!-- Multi-select Dropdown -->
    <div v-if="!catalogOnly" class="mb-3">
      <div
        class="cursor-pointer rounded-lg border border-gray-300 bg-white px-3 py-2 dark:border-dark-500 dark:bg-dark-700"
      >
        <div v-if="modelValue.length" data-testid="selected-models" class="flex max-h-[min(12rem,30vh)] flex-wrap gap-1.5 overflow-y-auto">
          <span
            v-for="model in modelValue"
            :key="model"
            class="inline-flex min-w-0 max-w-full items-center justify-between gap-1 rounded bg-gray-100 px-2 py-1 text-xs text-gray-700 dark:bg-dark-600 dark:text-gray-300"
          >
            <span class="flex min-w-0 items-center gap-1">
              <ModelIcon :model="model" size="14px" />
              <span class="break-all">{{ model }}</span>
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
        </div>
        <button ref="trigger" type="button" :aria-expanded="showDropdown" :aria-controls="dropdownId" :aria-label="t('admin.accounts.selectModels')" @click="toggleDropdown" class="flex w-full items-center justify-between py-2" :class="{ 'mt-2 border-t border-gray-200 dark:border-dark-600': modelValue.length }">
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
          <span role="status">{{ Math.min(optionLimit, filteredModels.length) }} / {{ filteredModels.length }}</span>
          <button v-if="optionLimit < filteredModels.length" type="button" data-testid="model-options-more" class="btn btn-secondary" @click="optionLimit += 100">{{ (locale?.startsWith('zh') ?? true) ? '显示更多' : 'Show more' }}</button>
        </div>
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
                :aria-pressed="modelValue.includes(model.value)"
                class="flex min-w-0 flex-1 items-center gap-2 px-3 py-2 text-left text-sm"
                @click="toggleModel(model.value)"
              >
                <span
                  :class="[
                    'flex h-4 w-4 shrink-0 items-center justify-center rounded border',
                    modelValue.includes(model.value)
                      ? 'border-primary-500 bg-primary-500 text-white'
                      : 'border-gray-300 dark:border-dark-500'
                  ]"
                >
                  <svg v-if="modelValue.includes(model.value)" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M5 13l4 4L19 7" />
                  </svg>
                </span>
                <ModelIcon :model="model.value" size="18px" />
                <span class="min-w-0 break-all text-gray-900 dark:text-white">{{ model.value }}</span>
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

    <p v-if="catalogState !== 'idle'" role="status" class="mb-2 text-sm text-secondary">{{ t(`admin.accounts.modelCatalog.${catalogState}`) }}</p>
    <button v-if="catalogState === 'failed' || catalogState === 'stale'" type="button" @click="accountId ? loadCatalog() : syncUpstreamModels()">{{ t('common.tryAgain') }}</button>

    <p v-if="syncError" role="alert" class="mb-2 text-sm">{{ syncError }}</p>
    <p v-if="syncResult && needsPreview" class="mb-2 text-sm text-secondary">{{ t('admin.accounts.modelSync.previewCandidates') }}</p>
    <p v-if="previewDisabledReason && canSyncUpstream" :id="previewReasonId" class="mb-2 text-sm text-secondary">{{ previewDisabledReason }}</p>
    <p v-if="configurationDirty && accountId" :id="dirtyReasonId" class="mb-2 text-sm text-secondary">{{ t('admin.accounts.modelSync.saveBeforePreview') }}</p>

    <p v-if="accountId && capabilityDefaultsDirty" class="mb-2 text-sm text-secondary">{{ t('admin.accounts.modelSync.selectionPendingSave') }}</p>

    <!-- Custom Model Input -->
    <div v-if="!catalogOnly" class="mb-3">
      <label :for="customModelId" class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.accounts.customModelName') }}</label>
      <div class="flex gap-2">
        <input
          :id="customModelId"
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
          class="rounded-lg bg-primary-50 px-4 py-2 text-sm font-medium text-primary-600 hover:bg-primary-100 dark:bg-primary-900/30 dark:text-primary-400 dark:hover:bg-primary-900/50"
        >
          {{ t('admin.accounts.addModel') }}
        </button>
      </div>
    </div>
    <details v-if="accountId && catalogSummary?.synced" class="mb-3 rounded border p-3" data-testid="catalog-allowlist">
      <summary>{{ t('admin.accounts.modelSync.allowlistTitle') }}</summary>
      <p class="input-hint">{{ t('admin.accounts.modelSync.allowlistHint') }}</p>
      <label class="flex gap-2"><input v-model="allowlistEnabled" type="checkbox" />{{ t('admin.accounts.modelSync.allowlistEnabled') }}</label>
      <label class="input-label">{{ t('admin.accounts.modelSync.allowlistModels') }}<textarea v-model="allowlistModels" class="input" data-testid="allowlist-models" /></label>
      <label class="input-label">{{ t('admin.accounts.modelSync.allowlistReason') }}<input v-model="allowlistReason" class="input" data-testid="allowlist-reason" /></label>
      <button type="button" class="btn btn-secondary" data-testid="save-allowlist" :disabled="!active || configurationDirty || allowlistBusy || !allowlistReason.trim() || (allowlistEnabled && !allowlistModels.trim())" @click="saveAllowlist">{{ t('common.save') }}</button>
    </details>
    <section v-if="syncResult" class="mb-3 space-y-2" data-testid="snapshot-actions">
      <p role="status">{{ t('admin.accounts.modelSync.previewCount', { count: previewModels.length }) }}</p>
      <button v-if="accountId && syncResult && !syncResult.applied" type="button" data-testid="apply-model-snapshot"
        :disabled="!canApplySnapshot" :aria-describedby="configurationDirty ? dirtyReasonId : undefined" @click="confirmSnapshot = true"
        class="rounded-lg border border-emerald-200 px-3 py-1.5 text-sm text-emerald-700 dark:text-emerald-300">
        {{ t('admin.accounts.modelSync.apply') }}
      </button>
    </section>
    <ConfirmDialog :show="confirmSnapshot" :title="t('admin.accounts.modelSync.apply')"
      :message="t('admin.accounts.modelSync.applyConfirmation')" @confirm="applyModelSnapshot" @cancel="confirmSnapshot = false" />
    <ModelCatalogDetails :mapping-targets="[...modelValue, ...(modelMappings ?? []).map(entry => entry.to)]" :creation-context="!accountId" :stale="needsPreview" :result="syncResult" :summary="catalogSummary" :candidate-count="presetCount" />
  </div>
</template>

<script setup lang="ts">
import { groupModelsByCategory, type ModelCategoryMetadata } from '@/utils/modelCategories'
import { ref, computed, watch, onUnmounted, useId, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { platformSupportsModelCatalog } from '@/constants/platforms'
import { accountsAPI } from '@/api/admin/accounts'
import { parseModelCapabilityDefaults, modelCapabilityRequestError } from './modelCapabilityDefaults'
import { usePlatformModelPresets } from '@/composables/usePlatformModelPresets'
import type { AccountModelCatalogDetails } from '@/types'
import ModelCatalogDetails from './ModelCatalogDetails.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import type { SyncUpstreamModelsResult, SyncUpstreamPreviewParams } from '@/api/admin/accounts'
import { useClipboard } from '@/composables/useClipboard'
import ModelIcon from '@/components/common/ModelIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import { validateModelRestrictions, translateModelRestrictionErrors, type ModelMappingEntry, type ModelCandidateIdentity, modelCandidateKey, modelSyncErrorKey } from '@/composables/useModelWhitelist'

const { t, locale } = useI18n()

const props = withDefaults(defineProps<{
  capabilityDefaultsDirty?: boolean
  capabilityDefaultsJSON?: string
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
}>()

const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

const dropdownId = useId()
const customModelId = useId()
const previewReasonId = useId()
const dirtyReasonId = useId()
const trigger = ref<HTMLButtonElement>()
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
const confirmSnapshot = ref(false)
const canApplySnapshot = computed(() => props.active && !isSyncingUpstream.value && syncResult.value?.discovery_complete === true &&
  !!syncResult.value.review && !needsPreview.value && !props.configurationDirty && !props.capabilityDefaultsDirty)
watch(canApplySnapshot, valid => { if (!valid) confirmSnapshot.value = false }, { flush: 'sync' })
const catalogState = ref<'idle' | 'loading' | 'failed' | 'empty' | 'stale' | 'ready' | 'unsynced' | 'invalidIdentity'>('idle')
let generation = 0
const context = computed(() => {
  const credentials = props.syncCredentials ? { ...props.syncCredentials } : undefined
  if (credentials) delete credentials.model_mapping
  return JSON.stringify([props.active !== false, props.accountId, normalizedPlatforms.value.map(platform => modelCandidateKey(platform, props.candidateIdentity)), credentials, props.connectionContext, props.savedRevision])
})
const selectionContext = computed(() => JSON.stringify([props.modelValue, props.modelMappings, props.syncCredentials?.model_mapping, props.configurationDirty]))
const previewModels = computed(() => [...new Set((syncResult.value?.candidate_models ?? syncResult.value?.discovered_models ?? []).map(model => model.trim()).filter(Boolean))])
const previewAdditions = computed(() => previewModels.value.filter(model => !props.modelValue.includes(model)))
const canAddPreview = computed(() => props.active && (!props.catalogOnly || props.catalogSelectionEnabled) && !!syncResult.value &&
  catalogState.value === 'ready' && previewAdditions.value.length > 0 && !isSyncingUpstream.value)
watch(selectionContext, () => {
  needsPreview.value = true
  confirmSnapshot.value = false
}, { flush: 'sync' })
// Editing declaration text invalidates the preview without fetching on each keystroke.
watch(() => props.capabilityDefaultsJSON, () => {
  generation++
  isSyncingUpstream.value = false
  needsPreview.value = true
  if (syncResult.value || catalogState.value === 'loading') catalogState.value = 'stale'
}, { flush: 'sync' })
const allowlistReason = ref('')
const allowlistModels = ref('')
const allowlistEnabled = ref(true)
const allowlistBusy = ref(false)
const saveAllowlist = async () => {
  const revision = catalogSummary.value?.state.rules_revision
  const models = [...new Set(allowlistModels.value.split(/[\s,]+/).filter(Boolean))]
  const discovered = catalogSummary.value?.effective_snapshot?.discovered_models ?? []
  if (!props.accountId || !props.active || props.configurationDirty || allowlistBusy.value || revision === undefined ||
      !allowlistReason.value.trim()) return
  if (allowlistEnabled.value && (!models.length || models.some(model => !discovered.includes(model)))) {
    syncError.value = t('admin.accounts.modelSync.allowlistInvalid')
    return
  }
  syncError.value = ''
  const request = ++generation
  allowlistBusy.value = true
  syncResult.value = null
  needsPreview.value = true
  try {
    await accountsAPI.updateModelAllowlist(props.accountId, { enabled: allowlistEnabled.value, models,
      reason: allowlistReason.value.trim(), expected_rules_revision: revision })
    if (disposed || request !== generation) return
    await loadCatalog()
    emit('catalog-applied')
  } catch (error) {
    if (!disposed && request === generation) syncError.value = t(`admin.accounts.${modelSyncErrorKey(error)}`)
  } finally { allowlistBusy.value = false }
}
const loadCatalog = async () => {
  if (disposed || !props.accountId || props.active === false || props.configurationDirty) return
  const request = ++generation
  const accountId = props.accountId
  catalogState.value = 'loading'
  try {
    const models = props.catalogOnly ? [] : await accountsAPI.getAvailableModels(accountId)
    if (disposed || request !== generation) return
    catalogMetadata.value = Object.fromEntries(models.map(model => [model.id, model]))
    catalog.value = models.map(model => model.id)
    catalogState.value = catalog.value.length ? 'ready' : 'empty'
    try {
      const summary = await accountsAPI.getModelCatalog(accountId)
      if (disposed || request !== generation) return
      catalogSummary.value = summary
      catalogMetadata.value = { ...catalogMetadata.value, ...summary.display_metadata }
      if (props.catalogOnly) {
        catalog.value = summary.effective_snapshot?.eligible_models?.map(model => model.id) ?? []
        catalogState.value = catalog.value.length ? 'ready' : 'empty'
      }
      needsPreview.value = summary.warnings?.includes('identity_changed') || !!summary.last_error || summary.staleness === 'stale'
      if (summary.warnings?.includes('identity_changed')) catalogState.value = 'invalidIdentity'
      else if (summary.last_error) catalogState.value = 'stale'
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
  confirmSnapshot.value = false
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
  allowlistModels.value = ''
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
  return [...new Set([...props.modelValue, ...presetModels.value, ...catalog.value])].map(value => ({ value, label: value }))
})

const filteredModels = computed(() => {
  const query = searchQuery.value.toLowerCase().trim()
  if (!query) return availableOptions.value
  return availableOptions.value.filter(
    m => m.value.toLowerCase().includes(query) || m.label.toLowerCase().includes(query)
  )
})

const groupedModels = computed(() => {
  const metadata: Record<string, ModelCategoryMetadata> = Object.fromEntries(
    selectedPresets.value.flatMap(preset => (preset.models ?? []).map(model => [model.id, model]))
  )
  for (const model of catalogSummary.value?.snapshot?.eligible_models ?? []) metadata[model.id] = { ...metadata[model.id], ...model }
  for (const [id, model] of Object.entries(catalogMetadata.value)) metadata[id] = { ...metadata[id], ...model }
  for (const [id, model] of Object.entries(syncResult.value?.metadata ?? {})) metadata[id] = { ...metadata[id], ...model }
  return groupModelsByCategory(filteredModels.value.slice(0, optionLimit.value), model => metadata[model.value])
})

const toggleDropdown = () => {
  showDropdown.value = !showDropdown.value
  if (!showDropdown.value) searchQuery.value = ''
}

const removeModel = (model: string) => {
  emit('update:modelValue', props.modelValue.filter(m => m !== model))
}

const toggleModel = (model: string) => {
  if (props.modelValue.includes(model)) {
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
  if (props.modelValue.includes(model)) {
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
    if (!newModels.includes(model)) newModels.push(model)
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

const syncUpstreamModels = async (apply = false) => {
  if (isSyncingUpstream.value || !props.active || !canSyncUpstream.value || previewDisabledReason.value) return
  if (!props.accountId && !props.syncCredentials) return

  const request = ++generation
  const reviewedSelection = selectionContext.value
  isSyncingUpstream.value = true
  syncError.value = ''
  catalogState.value = 'loading'
  try {
    let result
    if (props.accountId) {
      result = apply
        ? await accountsAPI.syncUpstreamModels(props.accountId, true, syncResult.value?.rules_revision, undefined, syncResult.value?.review)
        : props.capabilityDefaultsDirty
          ? await accountsAPI.syncUpstreamModels(props.accountId, false, undefined, parseModelCapabilityDefaults(props.capabilityDefaultsJSON ?? ''))
          : await accountsAPI.syncUpstreamModels(props.accountId, false)
    } else if (props.syncCredentials) {
      result = await accountsAPI.syncUpstreamModelsPreview({ ...props.syncCredentials, ...(props.capabilityDefaultsJSON?.trim() ? { model_capability_defaults: parseModelCapabilityDefaults(props.capabilityDefaultsJSON) } : {}) } as SyncUpstreamPreviewParams)
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
    if (apply) {
      isSyncingUpstream.value = false
      await loadCatalog()
      if (disposed || !props.active) return
      emit('catalog-applied')
    }

    if (result.applied === true) appStore.showInfo(t('admin.accounts.modelSync.synced'))
  } catch (error) {
    if (disposed || request !== generation) return
    catalogState.value = catalog.value.length ? 'stale' : 'failed'
    needsPreview.value = true
    syncError.value = modelCapabilityRequestError(error, t) || t(`admin.accounts.${modelSyncErrorKey(error)}`)
    appStore.showError(syncError.value)
  } finally {
    if (!disposed && request === generation) isSyncingUpstream.value = false
  }
}

const applyModelSnapshot = () => {
  if (!confirmSnapshot.value || !canApplySnapshot.value) return
  confirmSnapshot.value = false
  return syncUpstreamModels(true)
}

const clearAll = () => {
  emit('update:modelValue', [])
}

</script>
