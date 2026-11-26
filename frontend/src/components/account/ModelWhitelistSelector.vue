<template>
  <div @keydown.esc="handleEscape">
    <p v-if="presetError" role="alert" class="mb-3 text-sm text-red-600">
      {{ presetError }} <button type="button" class="underline" @click="reloadPresets">{{ t('common.refresh') }}</button>
    </p>
    <div
      v-if="!catalogOnly && !isSimplePicker"
      class="mb-4 space-y-2"
      data-testid="model-scope-mode"
      role="group"
      :aria-label="t('admin.accounts.modelSync.scopeModeLabel')"
    >
      <div class="flex flex-wrap gap-2">
        <button
          type="button"
          data-testid="mode-inherit"
          :aria-pressed="currentMode === 'inherit' && !denyAll"
          :disabled="!active || selectionBusy"
          :class="[
            'btn btn-sm',
            currentMode === 'inherit' && !denyAll ? 'btn-primary' : 'btn-secondary'
          ]"
          @click="selectMode('inherit')"
        >
          {{ t('admin.accounts.modelSync.modeInherit') }}
        </button>
        <button
          type="button"
          data-testid="mode-restricted"
          :aria-pressed="currentMode === 'restricted' && !denyAll"
          :disabled="!active || selectionBusy"
          :class="[
            'btn btn-sm',
            currentMode === 'restricted' && !denyAll ? 'btn-primary' : 'btn-secondary'
          ]"
          @click="selectMode('restricted')"
        >
          {{ t('admin.accounts.modelSync.modeRestricted') }}
        </button>
        <button
          v-if="allowDenyAll !== false"
          type="button"
          data-testid="mode-deny-all"
          :aria-pressed="denyAll"
          :disabled="!active || selectionBusy || (!!accountId && (configurationDirty || !catalogSummary))"
          :class="[
            'btn btn-sm',
            denyAll
              ? 'btn-danger'
              : 'btn-secondary border-red-200 text-red-600 dark:border-red-800 dark:text-red-400'
          ]"
          @click="selectMode('deny_all')"
        >
          {{ t('admin.accounts.modelSync.modeDenyAll') }}
        </button>
      </div>
      <p
        v-if="currentMode === 'inherit' && !denyAll"
        role="status"
        data-testid="mode-inherit-hint"
        class="text-xs text-secondary"
      >
        {{ t('admin.accounts.modelSync.modeInheritHint') }}
      </p>
      <p
        v-if="currentMode === 'restricted' && !denyAll"
        role="status"
        data-testid="mode-restricted-hint"
        class="text-xs text-secondary"
      >
        {{ t('admin.accounts.modelSync.modeRestrictedHint') }}
      </p>
      <p
        v-if="isLegacyFrozen"
        role="status"
        data-testid="legacy-frozen-notice"
        class="text-xs text-secondary"
      >
        {{ t('admin.accounts.modelSync.legacyFrozenNotice') }}
      </p>
    </div>
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
    <p v-if="!canSyncUpstream && normalizedPlatforms.length" role="status" class="mb-2 text-sm text-secondary">{{ t(`admin.accounts.${discoveryUnavailableKey}`) }}</p>
    <p v-if="catalogState === 'idle' && accountId" role="status" class="mb-2 text-sm text-secondary">{{ t('admin.accounts.modelSync.unknown') }}</p>
    <p v-if="catalogState !== 'idle' && !syncResult && !catalogSummary" role="status" class="mb-2 text-sm text-secondary">{{ t(`admin.accounts.modelCatalog.${catalogState}`) }}</p>
    <button v-if="catalogState === 'failed' || catalogState === 'stale'" type="button" class="btn btn-secondary btn-sm mb-3" @click="accountId ? loadCatalog() : syncUpstreamModels()">{{ t('common.tryAgain') }}</button>

    <p v-if="syncError" role="alert" class="mb-2 text-sm">{{ syncError }}</p>
    <p v-if="syncResult && needsPreview" class="mb-2 text-sm text-secondary">{{ t('admin.accounts.modelSync.previewCandidates') }}</p>
    <p v-if="previewDisabledReason && canSyncUpstream" :id="previewReasonId" class="mb-2 text-sm text-secondary">{{ previewDisabledReason }}</p>
    <p v-if="configurationDirty && accountId" :id="dirtyReasonId" class="mb-2 text-sm text-secondary">{{ t('admin.accounts.modelSync.saveBeforePreview') }}</p>

    <p v-if="denyAll" role="status" data-testid="deny-all">{{ t('admin.accounts.modelSync.denyAll') }}</p>
    <p v-if="autoAddedNotice" role="status" data-testid="auto-added-notice" class="mb-2 text-sm text-secondary">{{ autoAddedNotice }}</p>
    <p v-if="selectionWarning" role="status" data-testid="selection-warning">{{ selectionWarning }}</p>
    <BaseDialog
      :show="confirmClear"
      :title="t('admin.accounts.modelSync.confirmDenyAllTitle')"
      width="narrow"
      @close="cancelDenyAll"
    >
      <div class="space-y-3" data-testid="confirm-deny-all-dialog">
        <p class="text-sm leading-relaxed text-[var(--muted-foreground)]">
          {{ t('admin.accounts.modelSync.confirmDenyAllMessage') }}
        </p>
        <div class="space-y-2" data-testid="confirm-deny-all-body">
          <label class="input-label block text-xs" :for="denyAllReasonInputId">
            {{ t('admin.accounts.modelSync.denyAllReasonLabel') }}
          </label>
          <input
            :id="denyAllReasonInputId"
            ref="denyAllReasonInput"
            v-model="denyAllReasonDraft"
            type="text"
            class="input w-full text-sm"
            data-testid="deny-all-reason-input"
            :aria-invalid="Boolean(denyAllReasonValidationMessage)"
            :aria-describedby="denyAllReasonValidationMessage ? denyAllReasonErrorId : undefined"
            :placeholder="t('admin.accounts.modelSync.denyAllReasonPlaceholder')"
            @input="denyAllReasonError = ''"
            @keydown.enter.prevent="clearAll"
          />
          <p
            v-if="denyAllReasonValidationMessage"
            :id="denyAllReasonErrorId"
            role="alert"
            data-testid="deny-all-reason-error"
            class="text-xs text-red-600 dark:text-red-400"
          >
            {{ denyAllReasonValidationMessage }}
          </p>
        </div>
      </div>
      <template #footer>
        <div class="flex w-full flex-col-reverse gap-2 sm:w-auto sm:flex-row sm:justify-end">
          <button type="button" data-testid="confirm-deny-all-cancel" class="btn btn-secondary w-full sm:w-auto" @click="cancelDenyAll">
            {{ t('common.cancel') }}
          </button>
          <button
            type="button"
            data-testid="confirm-deny-all-submit"
            class="btn btn-danger w-full sm:w-auto"
            :disabled="isDenyAllConfirmDisabled"
            @click="clearAll"
          >
            {{ t('admin.accounts.modelSync.confirmDenyAllSubmit') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <details v-if="!catalogOnly" ref="selectionDetails" open class="mb-4 space-y-3" data-testid="model-selection">
      <summary class="cursor-pointer rounded-lg py-2 font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500">{{ t('admin.accounts.editSections.selection') }}</summary>
    <!-- Multi-select Dropdown -->
    <div v-if="!catalogOnly" class="mb-3">
      <div
        class="cursor-pointer rounded-xl border border-[var(--border)] bg-[var(--card)] px-3 py-2"
      >
        <div v-if="configuredModels.length" data-testid="selected-models" class="flex max-h-[min(12rem,30vh)] flex-wrap gap-1.5 overflow-y-auto">
          <section v-for="group in selectedGroups" :key="group.category" :data-model-category="group.category" class="w-full space-y-1">
            <h3 class="text-xs font-semibold">{{ t(`admin.accounts.modelCategories.${group.category}`) }}</h3>
          <span
            v-for="model in group.models"
            :key="model"
            class="inline-flex min-w-0 max-w-full items-center justify-between gap-1 rounded-md border border-[var(--border)] bg-[var(--surface-2)] px-2 py-1 text-xs text-[var(--foreground)]"
          >
            <span class="flex min-w-0 items-center gap-1">
              <ModelIcon :model="model" size="14px" />
              <span class="min-w-0 break-all">{{ mappingTargets.has(model) ? model : modelDisplayName(model) }}<small v-if="!mappingTargets.has(model) && modelDisplayName(model) !== model" class="block text-secondary">{{ model }}</small><small v-for="target in mappingTargets.get(model)" :key="target" class="block text-secondary">→ {{ target }}</small><small class="block">{{ modelSource(model) }}</small></span>
            </span>
            <button
              type="button"
              :aria-label="`${t('common.remove')} ${model}`"
              @click.stop="removeModel(model)"
              class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full hover:bg-[var(--surface-2)]"
            >
              <Icon name="x" size="xs" class="h-3.5 w-3.5" :stroke-width="2" />
            </button>
          </span>
          </section>
        </div>
        <button ref="trigger" type="button" :aria-expanded="showDropdown" :aria-controls="dropdownId" :aria-label="t('admin.accounts.selectModels')" @click="toggleDropdown" class="flex w-full items-center justify-between py-2" :class="{ 'mt-2 border-t border-[var(--border)]': configuredModels.length }">
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
        class="mt-1 rounded-lg border shadow-lg border-[var(--border)] bg-[var(--surface-2)]"
      >
        <div class="max-h-24 overflow-y-auto border-b p-2 border-[var(--border)] bg-[var(--surface-2)]">
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
            <h3 class="px-3 py-1.5 text-xs font-semibold text-secondary bg-[var(--card)]">{{ t(`admin.accounts.modelCategories.${group.category}`) }}</h3>
            <div
              v-for="model in group.models"
              :key="model.value"
              data-testid="model-option"
              class="group flex items-center hover:bg-[var(--surface-2)]"
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
                      : 'border-[var(--border)]'
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
                class="mr-2 rounded p-1.5 text-secondary opacity-70 transition-colors hover:text-primary-600 focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-focus group-hover:opacity-100 hover:bg-[var(--surface-2)] dark:hover:text-primary-400"
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
      <div class="flex flex-col gap-2 sm:flex-row">
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
          class="btn btn-secondary w-full shrink-0 sm:w-auto"
        >
          {{ t('admin.accounts.addModel') }}
        </button>
      </div>
    </div>
    </details>
    <details
      v-if="!catalogOnly && !isSimplePicker"
      ref="mappingDetails"
      class="mb-4 space-y-3 border-t border-[var(--border)] pt-3"
      data-testid="model-mappings"
    >
      <summary class="cursor-pointer rounded-lg py-1.5 text-sm font-medium text-[var(--foreground)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)]">
        {{ t('admin.accounts.editSections.mappings', { count: modelMappings?.length ?? 0 }) }}
      </summary>
      <p class="input-hint">{{ t('admin.accounts.mapRequestModels') }}</p>
      <div
        v-for="(mapping, index) in modelMappings ?? []"
        :key="index"
        class="grid min-w-0 grid-cols-1 gap-2 sm:grid-cols-[1fr_1fr_auto]"
      >
        <input
          :value="mapping.from"
          class="input min-w-0"
          :aria-label="t('admin.accounts.requestModel')"
          :placeholder="t('admin.accounts.requestModel')"
          @input="updateMapping(index, 'from', $event)"
        />
        <input
          :value="mapping.to"
          class="input min-w-0"
          :aria-label="t('admin.accounts.actualModel')"
          :placeholder="t('admin.accounts.actualModel')"
          @input="updateMapping(index, 'to', $event)"
        />
        <button type="button" class="btn btn-secondary" @click="removeMapping(index)">
          {{ t('common.delete') }}
        </button>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <button type="button" class="btn btn-secondary" @click="addMapping()">
          {{ t('admin.accounts.addMapping') }}
        </button>
        <button
          v-for="targetModel in selectedModels"
          :key="`target-alias-${targetModel}`"
          type="button"
          data-testid="add-target-alias"
          class="btn btn-secondary max-w-full break-all text-left text-xs"
          @click="addMapping({ from: '', to: targetModel })"
        >
          {{ t('admin.accounts.modelSync.addInlineAlias') }}: {{ targetModel }}
        </button>
      </div>
      <div v-if="presets?.length" class="space-y-2">
        <p class="input-label">{{ t('admin.accounts.editSections.mappingTemplates') }}</p>
        <div class="flex flex-wrap gap-2">
          <button
            v-for="preset in presets"
            :key="preset.from"
            type="button"
            class="btn btn-secondary max-w-full break-all text-xs"
            @click="addMapping(preset)"
          >
            {{ preset.label || preset.from }}
          </button>
        </div>
      </div>
    </details>
    <div
      v-if="!catalogOnly && !isSimplePicker && currentMode === 'restricted' && !denyAll && aliasTargets.length > 0"
      data-testid="alias-target-visibility"
      class="mb-4 space-y-2 rounded-lg border border-[var(--border)] bg-[var(--surface-2)]/40 p-3"
    >
      <div class="space-y-0.5">
        <p class="text-xs font-medium text-[var(--foreground)]">{{ t('admin.accounts.modelSync.aliasTargetsTitle') }}</p>
        <p class="text-xs text-secondary">{{ t('admin.accounts.modelSync.aliasTargetsHint') }}</p>
      </div>
      <div class="space-y-2">
        <div
          v-for="target in aliasTargets"
          :key="target"
          data-testid="alias-target-row"
          :data-target="target"
          class="flex flex-wrap items-center justify-between gap-2 rounded-md border border-[var(--border)] bg-[var(--card)] px-2.5 py-2 text-xs"
        >
          <div class="min-w-0 space-y-0.5">
            <p class="break-all font-mono font-medium text-[var(--foreground)]">{{ target }}</p>
            <p class="break-all text-secondary">{{ (aliasTargetsMap.get(target) ?? []).join(', ') }} → {{ target }}</p>
          </div>
          <label class="flex cursor-pointer items-center gap-2 text-xs text-[var(--foreground)]">
            <input
              type="checkbox"
              data-testid="toggle-alias-target-public"
              :data-target="target"
              :checked="effectivePublicTargetSet.has(target)"
              :disabled="!active || selectionBusy"
              class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              @change="toggleAliasTargetPublic(target, ($event.target as HTMLInputElement).checked)"
            />
            <span>{{ t('admin.accounts.modelSync.allowOriginalName') }}</span>
          </label>
        </div>
      </div>
    </div>
    <div
      v-if="!catalogOnly && !isSimplePicker"
      data-testid="available-request-names"
      class="mb-4 space-y-2 rounded-lg border border-[var(--border)] bg-[var(--card)] p-3"
    >
      <div class="flex flex-wrap items-center justify-between gap-2">
        <p class="text-xs font-medium text-[var(--foreground)]">{{ t('admin.accounts.modelSync.availableRequestNames') }}</p>
        <span v-if="!denyAll && (currentMode === 'restricted' || availableRequestNames.length > 0)" class="text-xs text-secondary">{{ availableRequestNames.length }}</span>
      </div>
      <p v-if="denyAll" class="text-xs text-secondary">{{ t('admin.accounts.modelSync.availableRequestNamesDenyAll') }}</p>
      <div v-else-if="availableRequestNames.length > 0" class="flex flex-wrap gap-1.5">
        <span
          v-for="name in availableRequestNames"
          :key="name"
          data-testid="available-request-name"
          class="inline-flex max-w-full break-all rounded-md border border-[var(--border)] bg-[var(--surface-2)] px-2 py-0.5 font-mono text-xs text-[var(--foreground)]"
        >
          {{ name }}
        </span>
      </div>
      <p v-else-if="currentMode === 'inherit'" class="text-xs text-secondary">{{ t('admin.accounts.modelSync.availableRequestNamesInheritAll') }}</p>
      <p v-else class="text-xs text-secondary">{{ t('admin.accounts.modelSync.availableRequestNamesEmpty') }}</p>
    </div>
    <div v-if="!catalogOnly" class="mb-4 space-y-2">
      <div class="flex flex-wrap items-center gap-3">
        <button v-if="accountId" type="button" data-testid="save-model-selection" class="btn btn-primary w-full sm:w-auto"
          :aria-describedby="configurationDirty ? dirtyReasonId : undefined"
          :disabled="!active || configurationDirty || selectionBusy || !catalogSummary || !canSubmitCurrentMode" @click="saveSelection(false)">
          {{ t(selectionBusy ? 'admin.accounts.modelSync.savingSelection' : denyAll ? 'admin.accounts.modelSync.reselect' : 'admin.accounts.modelSync.saveSelection') }}
        </button>
        <p v-if="accountId" role="status" data-testid="model-save-status" class="text-sm text-secondary">{{ t(`admin.accounts.modelSync.${selectionStatus}`) }}</p>
      </div>
      <p class="input-hint">{{ t(accountId ? 'admin.accounts.modelSync.selectionPendingSave' : 'admin.accounts.modelSync.selectionPendingCreate') }}</p>
    </div>
    <ModelCatalogDetails :display-metadata="displayMetadata" :mapping-targets="[...modelValue, ...(modelMappings ?? []).map(entry => entry.to)]" :creation-context="!accountId" :stale="catalogState === 'stale' || catalogState === 'invalidIdentity' || catalogSummary?.staleness === 'stale'" :status-override="catalogState === 'idle' ? undefined : catalogState" :result="syncResult" :summary="catalogSummary" :candidate-count="presetCount" />
    <div v-if="!catalogOnly && allowDenyAll !== false" class="mt-4 flex justify-end border-t pt-4 border-[var(--border)]">
      <button type="button" @click="openClearConfirmation"
        data-testid="clear-model-selection" :disabled="selectionBusy || !active || (!!accountId && (configurationDirty || !catalogSummary))"
        class="btn btn-secondary w-full border-red-200 text-red-600 sm:w-auto dark:border-red-800 dark:text-red-400">
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
import { compatibleProvider, compatibleDiscoveryDisabled } from '@/constants/compatibleProviders'
import { accountsAPI } from '@/api/admin/accounts'
import { usePlatformModelPresets } from '@/composables/usePlatformModelPresets'
import type { Account, ModelAllowlist, AccountModelCatalogDetails } from '@/types'
import ModelCatalogDetails from './ModelCatalogDetails.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import type { SyncUpstreamModelsResult, SyncUpstreamPreviewParams } from '@/api/admin/accounts'
import { useClipboard } from '@/composables/useClipboard'
import ModelIcon from '@/components/common/ModelIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import {
  accountModelAllowlist,
  buildUpdateModelAllowlistRequest,
  computePublicRequestNames,
  extractAliasTargets,
  extractPublicAliasTargets,
  isLegacySystemFreezeAllowlist,
  splitModelMappingObject,
  validateModelRestrictions,
  translateModelRestrictionErrors,
  type AccountModelSelectionMode,
  type ModelRestrictionError,
  type ModelMappingEntry,
  type ModelCandidateIdentity,
  modelCandidateKey,
  modelSyncErrorKey
} from '@/composables/useModelWhitelist'

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
  persistedSelection?: {
    mode?: AccountModelSelectionMode
    models: string[]
    mappings: ModelMappingEntry[]
    publicTargets?: string[]
    reason?: string
  }
  registerActions?: (actions: { saveSelection: () => Promise<boolean>; canSaveSelection: () => boolean } | null) => void
  configurationDirty?: boolean
  modelMappings?: ModelMappingEntry[]
  publicTargets?: string[]
  mode?: AccountModelSelectionMode
  reason?: string
  presets?: (ModelMappingEntry & { label?: string })[]
  allowDenyAll?: boolean
  simplePicker?: boolean
  active?: boolean
  modelValue: string[]
  platform?: string
  platforms?: string[]
  accountId?: number
  syncCredentials?: SyncUpstreamPreviewParams
}>(), {
  active: true,
  allowDenyAll: true,
  simplePicker: false,
  presets: () => []
})

const emit = defineEmits<{
  'update:modelValue': [value: string[]]
  'update:modelMappings': [mappings: ModelMappingEntry[]]
  'update:publicTargets': [targets: string[]]
  'update:mode': [mode: AccountModelSelectionMode]
  'update:reason': [reason: string]
  'upstream-synced': []
  'catalog-applied': []
  'selection-saved': [account: Account]
  'catalog-hydrated': [selection: { mode?: AccountModelSelectionMode; models: string[]; mappings: ModelMappingEntry[]; publicTargets: string[] }]
  'validation-failed': [errors: ModelRestrictionError[]]
}>()

const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

const dropdownId = useId()
const customModelId = useId()
const previewReasonId = useId()
const dirtyReasonId = useId()
const denyAllReasonInputId = useId()
const denyAllReasonErrorId = useId()
const trigger = ref<HTMLButtonElement>()
const selectionDetails = ref<HTMLDetailsElement>()
const mappingDetails = ref<HTMLDetailsElement>()
const customModelInput = ref<HTMLInputElement>()
const denyAllReasonInput = ref<HTMLInputElement>()
function revealSelection() {
  if (selectionDetails.value) selectionDetails.value.open = true
  void nextTick(() => customModelInput.value?.focus())
}
function revealValidation(errors: ModelRestrictionError[]) {
  const error = errors[0]
  if (!error) return
  if (error.code === 'exactIdRequired' || error.code === 'emptyRestrictedSelection') {
    revealSelection()
    return
  }
  if (error.code === 'denyAllReasonRequired') {
    void nextTick(() => denyAllReasonInput.value?.focus())
    return
  }
  if (mappingDetails.value) mappingDetails.value.open = true
  void nextTick(() => mappingDetails.value?.querySelectorAll('input')[(error.row - 1) * 2]?.focus())
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
onUnmounted(() => {
  disposed = true
  props.registerActions?.(null)
})
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
  normalizedPlatforms.value.some(platform => {
    const provider = compatibleProvider(platform.toLowerCase())
    if (!provider) return platformSupportsModelCatalog(platform)
    return !compatibleDiscoveryDisabled(provider, {
      account_mode: props.syncCredentials?.credentials?.account_mode ?? props.candidateIdentity?.mode
    })
  }) ||
  Boolean(props.accountId && !normalizedPlatforms.value.length)
))
const discoveryUnavailableKey = computed(() => props.discoveryDisabled || normalizedPlatforms.value.some(platform => compatibleProvider(platform.toLowerCase()))
  ? 'syncUpstreamModelsDiscoveryUnconfirmed' : 'syncUpstreamModelsNotAdapted')
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
const denyAllReasonDraft = ref('')
const denyAllReasonError = ref('')
const selectionBusy = ref(false)
const selectionWarning = ref('')
const autoAddedNotice = ref('')
const savedAllowlist = ref(props.modelAllowlist)
const localModeOverride = ref<AccountModelSelectionMode | null>(null)
const localReason = ref(props.reason ?? props.modelAllowlist?.reason ?? '')
const localPublicTargets = ref<string[]>([])
const stashedRestrictedDraft = ref<{ models: string[]; mappings: ModelMappingEntry[]; publicTargets: string[] } | null>(null)
const effectivePublicTargets = computed(() =>
  props.publicTargets !== undefined ? props.publicTargets : localPublicTargets.value
)
const setPublicTargets = (next: string[]) => {
  const normalized = [...new Set(next.map(target => target.trim()).filter(Boolean))]
  if (props.publicTargets !== undefined) {
    emit('update:publicTargets', normalized)
  } else {
    localPublicTargets.value = normalized
  }
}
const aliasTargets = computed(() => extractAliasTargets(props.modelMappings ?? []))
const aliasTargetsMap = computed(() => {
  const map = new Map<string, string[]>()
  for (const entry of props.modelMappings ?? []) {
    const from = entry.from.trim()
    const to = entry.to.trim()
    if (!from || !to || from === to || to.includes('*')) continue
    const list = map.get(to) ?? []
    list.push(from)
    map.set(to, list)
  }
  return map
})
const effectivePublicTargetSet = computed(() => {
  const validTargets = new Set(aliasTargets.value)
  return new Set(effectivePublicTargets.value.map(target => target.trim()).filter(target => validTargets.has(target)))
})
watch(aliasTargets, (targets) => {
  const targetSet = new Set(targets)
  const current = effectivePublicTargets.value
  const pruned = current.filter(target => targetSet.has(target.trim()))
  if (pruned.length !== current.length) {
    setPublicTargets(pruned)
  }
})
const isSimplePicker = computed(
  () =>
    props.simplePicker ||
    (!props.accountId &&
      !props.platform &&
      !(props.platforms?.length) &&
      props.modelMappings === undefined &&
      props.mode === undefined &&
      !props.modelAllowlist)
)
const isLegacyFrozen = computed(() => isLegacySystemFreezeAllowlist(savedAllowlist.value))
const isPersistedDenyAll = computed(
  () =>
    savedAllowlist.value?.enabled === true &&
    !savedAllowlist.value.models?.length &&
    !isLegacyFrozen.value
)
const hasConfiguredModelsOrMappings = computed(
  () => props.modelValue.length > 0 || (props.modelMappings?.length ?? 0) > 0
)
const currentMode = computed<AccountModelSelectionMode>(() => {
  if (localModeOverride.value !== null) return localModeOverride.value
  if (props.mode !== undefined) {
    if (props.mode === 'inherit' && hasConfiguredModelsOrMappings.value) return 'restricted'
    return props.mode
  }
  if (isPersistedDenyAll.value) return 'deny_all'
  if (hasConfiguredModelsOrMappings.value) return 'restricted'
  if (savedAllowlist.value?.enabled === true && (savedAllowlist.value.models?.length ?? 0) > 0) {
    return 'restricted'
  }
  if (catalogSummary.value?.mode) return catalogSummary.value.mode
  return 'inherit'
})
const denyAll = computed(
  () =>
    (isPersistedDenyAll.value && localModeOverride.value !== 'inherit') ||
    currentMode.value === 'deny_all'
)
const canSubmitCurrentMode = computed(() => {
  if (currentMode.value === 'inherit') return true
  return hasConfiguredModelsOrMappings.value
})
const isDenyAllConfirmDisabled = computed(
  () => !denyAllReasonDraft.value.trim() || selectionBusy.value
)
const denyAllReasonValidationMessage = computed(
  () => denyAllReasonError.value || (!denyAllReasonDraft.value.trim() ? t('admin.accounts.modelValidation.denyAllReasonRequired') : '')
)
const currentRulesRevision = computed(
  () => catalogSummary.value?.rules_revision ?? catalogSummary.value?.state?.rules_revision
)
watch(() => props.modelAllowlist, value => { savedAllowlist.value = value }, { deep: true })
watch(() => props.mode, value => {
  if (value !== undefined) localModeOverride.value = null
})
watch(() => props.reason, value => {
  if (value !== undefined) localReason.value = value
})
watch(() => [props.accountId, props.active], () => {
  selectionWarning.value = ''
  autoAddedNotice.value = ''
  savedAllowlist.value = props.modelAllowlist
  localModeOverride.value = null
  localReason.value = props.reason ?? props.modelAllowlist?.reason ?? ''
  localPublicTargets.value = []
  stashedRestrictedDraft.value = null
})
const catalogState = ref<'idle' | 'loading' | 'failed' | 'empty' | 'stale' | 'ready' | 'unsynced' | 'invalidIdentity'>('idle')
let generation = 0
let selectionGeneration = 0
const context = computed(() => {
  const credentials = props.syncCredentials ? { ...props.syncCredentials } : undefined
  if (credentials) delete credentials.model_mapping
  return JSON.stringify([props.active !== false, props.accountId, normalizedPlatforms.value.map(platform => modelCandidateKey(platform, props.candidateIdentity)), credentials, props.connectionContext, props.savedRevision, props.discoveryDisabled])
})
const selectionContext = computed(() => JSON.stringify([props.modelValue, props.modelMappings, effectivePublicTargets.value, props.syncCredentials?.model_mapping, props.configurationDirty]))
function selectionKey(
  models: string[],
  mappings: ModelMappingEntry[],
  mode?: AccountModelSelectionMode,
  publicTargets: readonly string[] = []
) {
  const hasEntries = models.length > 0 || mappings.length > 0
  const resolvedMode: AccountModelSelectionMode =
    mode === 'deny_all'
      ? 'deny_all'
      : mode === 'inherit' && !hasEntries
        ? 'inherit'
        : hasEntries || mode === 'restricted'
          ? 'restricted'
          : 'inherit'
  const validAliasTargets = new Set(extractAliasTargets(mappings))
  const normalizedPublicTargets = [...new Set(publicTargets.map(target => target.trim()).filter(target => validAliasTargets.has(target)))].sort()
  return JSON.stringify({
    mode: resolvedMode,
    entries: [...models.map(model => [model, model]), ...mappings.map(mapping => [mapping.from, mapping.to])]
      .sort((left, right) => JSON.stringify(left).localeCompare(JSON.stringify(right))),
    publicTargets: resolvedMode === 'restricted' ? normalizedPublicTargets : []
  })
}
const selectionFingerprint = computed(() => selectionKey(props.modelValue, props.modelMappings ?? [], currentMode.value, effectivePublicTargets.value))
const availableRequestNames = computed(() =>
  computePublicRequestNames({
    mode: currentMode.value,
    models: props.modelValue,
    mappings: props.modelMappings ?? [],
    publicTargets: effectivePublicTargets.value
  })
)
const savedSelectionFingerprint = ref(selectionFingerprint.value)
const selectionSaved = ref(false)
const selectionSaveFailed = ref(false)
const selectionDirty = computed(() => selectionFingerprint.value !== savedSelectionFingerprint.value)
const selectionStatus = computed(() => selectionBusy.value ? 'savingSelection' : selectionSaveFailed.value ? 'selectionSaveFailed' : selectionDirty.value ? 'unsavedSelection' : selectionSaved.value ? 'savedSelection' : 'unchangedSelection')
const wildcardCount = computed(() => props.modelMappings?.filter(mapping => mapping.from.includes('*')).length ?? 0)
watch(
  () => props.persistedSelection,
  (value) => {
    if (!value) return
    savedSelectionFingerprint.value = selectionKey(value.models, value.mappings, value.mode, value.publicTargets ?? [])
  },
  { immediate: true, deep: true }
)
watch(() => [props.accountId, props.active], async () => {
  selectionSaved.value = false
  selectionSaveFailed.value = false
  if (props.persistedSelection) {
    savedSelectionFingerprint.value = selectionKey(props.persistedSelection.models, props.persistedSelection.mappings, props.persistedSelection.mode, props.persistedSelection.publicTargets ?? [])
    return
  }
  await nextTick()
  savedSelectionFingerprint.value = selectionFingerprint.value
}, { immediate: true })
watch(selectionFingerprint, () => { selectionSaveFailed.value = false })
const previewModels = computed(() => [...new Set((syncResult.value?.candidate_models ?? syncResult.value?.discovered_models ?? []).map(model => model.trim()).filter(Boolean))])
const previewAdditions = computed(() => previewModels.value.filter(model => !configuredModelKeys.value.has(model)))
const canAddPreview = computed(() => props.active && (!props.catalogOnly || props.catalogSelectionEnabled) && !!syncResult.value &&
  (catalogState.value === 'ready' || catalogState.value === 'unsynced') && previewAdditions.value.length > 0 && !isSyncingUpstream.value)
watch(selectionContext, () => {
  needsPreview.value = true
  confirmClear.value = false
}, { flush: 'sync' })
// Read the persisted account after every mutation: legacy filter writes can be
// normalized to deny-all by the server and must never be shown as unrestricted.
const persistSelection = async (requestBody: Parameters<typeof accountsAPI.updateModelAllowlist>[1]): Promise<boolean> => {
  if (!props.accountId || !props.active || props.configurationDirty || selectionBusy.value) return false
  const accountId = props.accountId
  const request = ++generation
  const selectionRequest = ++selectionGeneration
  let savedDraftContext = selectionContext.value
  let draftChanged = false
  selectionBusy.value = true
  selectionSaveFailed.value = false
  syncError.value = ''
  selectionWarning.value = ''
  autoAddedNotice.value = ''
  try {
    const result = await accountsAPI.updateModelAllowlist(accountId, requestBody)
    if (disposed || request !== generation) return false
    draftChanged = selectionContext.value !== savedDraftContext
    if (!draftChanged && requestBody.mode === 'restricted') {
      const selection = splitModelMappingObject(requestBody.model_mapping)
      const immediateModels = [
        ...new Set([
          ...requestBody.models,
          ...(result.models ?? []),
          ...selection.allowedModels,
          ...(result.auto_added ?? [])
        ])
      ]
      emit('update:modelValue', immediateModels)
      emit('update:modelMappings', selection.modelMappings)
      setPublicTargets(extractPublicAliasTargets(requestBody.model_mapping))
    }
    await nextTick()
    savedDraftContext = selectionContext.value
    const account = await accountsAPI.getById(accountId)
    if (disposed || request !== generation) return false
    const accountAllowlist = accountModelAllowlist(account)
    const effectiveMode: AccountModelSelectionMode =
      result.mode ??
      requestBody.mode ??
      (accountAllowlist?.enabled
        ? accountAllowlist.models.length > 0
          ? 'restricted'
          : 'deny_all'
        : 'restricted')
    const persistedMappingRaw = (account?.credentials?.model_mapping ??
      result.model_mapping ??
      requestBody.model_mapping) as Record<string, unknown> | undefined
    const persisted = splitModelMappingObject(persistedMappingRaw)
    const persistedPublicTargets = extractPublicAliasTargets(persistedMappingRaw)
    let persistedModels: string[] = []
    let persistedMappings: ModelMappingEntry[] = []
    if (effectiveMode === 'inherit') {
      persistedModels = []
      persistedMappings = persisted.modelMappings
      savedAllowlist.value = accountAllowlist ?? { enabled: false, models: [] }
    } else if (effectiveMode === 'deny_all') {
      persistedModels = []
      persistedMappings = persisted.modelMappings
      savedAllowlist.value = accountAllowlist ?? {
        enabled: true,
        models: [],
        ...(requestBody.reason ? { reason: requestBody.reason } : {})
      }
    } else {
      persistedMappings = persisted.modelMappings
      persistedModels = [
        ...new Set([
          ...(accountAllowlist?.enabled ? accountAllowlist.models : []),
          ...(result.models ?? []),
          ...persisted.allowedModels,
          ...(result.auto_added ?? []),
          ...requestBody.models
        ])
      ]
      savedAllowlist.value =
        accountAllowlist && (accountAllowlist.enabled || persisted.allowedModels.length > 0 || persisted.modelMappings.length > 0)
          ? accountAllowlist
          : {
              enabled: true,
              models: persistedModels,
              ...(requestBody.reason ? { reason: requestBody.reason } : {})
            }
    }
    localModeOverride.value = effectiveMode
    emit('update:mode', effectiveMode)
    const resolvedPublicTargets = effectiveMode === 'inherit' ? [] : persistedPublicTargets
    savedSelectionFingerprint.value = selectionKey(persistedModels, persistedMappings, effectiveMode, resolvedPublicTargets)
    selectionSaved.value = true
    if (result.not_discovered?.length) selectionWarning.value = t('admin.accounts.modelSync.notDiscoveredWarning', { models: result.not_discovered.join(', ') })
    if (result.auto_added?.length) autoAddedNotice.value = t('admin.accounts.modelSync.autoAddedNotice', { models: result.auto_added.join(', ') })
    if (!draftChanged && selectionContext.value === savedDraftContext) {
      emit('update:modelValue', persistedModels)
      emit('update:modelMappings', persistedMappings)
      setPublicTargets(resolvedPublicTargets)
      if (effectiveMode === 'deny_all') {
        localReason.value = accountAllowlist?.reason ?? requestBody.reason ?? ''
        emit('update:reason', localReason.value)
      }
      emit('selection-saved', account)
    }
    emit('catalog-applied')
    await loadCatalog()
    return !draftChanged && selectionContext.value === savedDraftContext
  } catch (error) {
    if (disposed || request !== generation) return false
    selectionSaveFailed.value = true
    const errorKey = modelSyncErrorKey(error)
    syncError.value = t(
      `admin.accounts.${
        extractApiErrorStatus(error) === 400 && errorKey === 'syncUpstreamModelsFailed'
          ? 'modelSync.invalidSelection'
          : errorKey
      }`
    )
    // Refresh the CAS token; retain the user's draft and require an explicit retry.
    await loadCatalog()
    return false
  } finally { if (selectionRequest === selectionGeneration) selectionBusy.value = false }
}
const saveSelection = async (clear: boolean, explicitReason?: string): Promise<boolean> => {
  const revision = currentRulesRevision.value
  if (revision === undefined) return false
  if (clear) {
    const reasonText =
      (explicitReason ?? localReason.value).trim() ||
      t('admin.accounts.modelSync.defaultDenyAllReason')
    return await persistSelection(
      buildUpdateModelAllowlistRequest({
        mode: 'deny_all',
        models: [],
        mappings: [],
        reason: reasonText,
        expectedRulesRevision: revision
      })
    )
  }
  const targetMode: AccountModelSelectionMode =
    localModeOverride.value === 'inherit' ||
    (currentMode.value === 'inherit' && !hasConfiguredModelsOrMappings.value)
      ? 'inherit'
      : 'restricted'
  if (targetMode === 'restricted' && !hasConfiguredModelsOrMappings.value) {
    const emptyErrors = validateModelRestrictions('restricted', props.modelValue, props.modelMappings ?? [], {
      requireNonEmptyRestricted: true
    })
    syncError.value = translateModelRestrictionErrors(emptyErrors, t)
    revealValidation(emptyErrors)
    emit('validation-failed', emptyErrors)
    return false
  }
  const errors = validateModelRestrictions(targetMode, props.modelValue, props.modelMappings ?? [])
  if (errors.length) {
    syncError.value = translateModelRestrictionErrors(errors, t)
    revealValidation(errors)
    emit('validation-failed', errors)
    return false
  }
  if (targetMode === 'inherit') {
    return await persistSelection(
      buildUpdateModelAllowlistRequest({
        mode: 'inherit',
        models: [],
        mappings: props.modelMappings ?? [],
        reason: localReason.value,
        expectedRulesRevision: revision
      })
    )
  }
  return await persistSelection(
    buildUpdateModelAllowlistRequest({
      mode: 'restricted',
      models: props.modelValue,
      mappings: props.modelMappings ?? [],
      publicTargets: effectivePublicTargets.value,
      reason: localReason.value,
      expectedRulesRevision: revision
    })
  )
}
const canSaveSelection = () =>
  Boolean(
    props.accountId &&
    props.active &&
    !props.configurationDirty &&
    !selectionBusy.value &&
    currentRulesRevision.value !== undefined &&
    canSubmitCurrentMode.value
  )
watch(
  () => props.registerActions,
  (register, previous) => {
    if (previous && previous !== register) previous(null)
    register?.({
      saveSelection: () => saveSelection(false),
      canSaveSelection
    })
  },
  { immediate: true }
)
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
      if (summary.mode === 'deny_all' && !savedAllowlist.value) {
        savedAllowlist.value = { enabled: true, models: [] }
      } else if (
        summary.mode === 'restricted' &&
        Array.isArray(summary.models) &&
        summary.models.length > 0 &&
        !savedAllowlist.value
      ) {
        savedAllowlist.value = { enabled: true, models: [...summary.models] }
      }
      if (summary.model_mapping && typeof summary.model_mapping === 'object') {
        const catalogPublicTargets = extractPublicAliasTargets(summary.model_mapping)
        if (!selectionDirty.value) {
          if (props.publicTargets === undefined) {
            localPublicTargets.value = catalogPublicTargets
            savedSelectionFingerprint.value = selectionKey(
              props.modelValue,
              props.modelMappings ?? [],
              currentMode.value,
              catalogPublicTargets
            )
          } else {
            const currentSorted = [...effectivePublicTargetSet.value].sort()
            const nextSorted = [...catalogPublicTargets].sort()
            if (JSON.stringify(currentSorted) !== JSON.stringify(nextSorted)) {
              emit('update:publicTargets', catalogPublicTargets)
              emit('catalog-hydrated', {
                mode: summary.mode,
                models: summary.models ?? [],
                mappings: splitModelMappingObject(summary.model_mapping).modelMappings,
                publicTargets: catalogPublicTargets
              })
              savedSelectionFingerprint.value = selectionKey(
                props.modelValue,
                props.modelMappings ?? [],
                currentMode.value,
                catalogPublicTargets
              )
            }
          }
        }
      }
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
  confirmClear.value = false
  denyAllReasonError.value = ''
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
  if (presetModels.value.includes(id) || (syncResult.value?.candidate_models?.includes(id) && !syncResult.value.discovered_models?.includes(id))) sources.push(t('admin.accounts.modelSync.platformCandidates'))
  const discovered = syncResult.value?.discovered_models ?? catalogSummary.value?.snapshot?.discovered_models ?? []
  if (discovered.includes(id)) sources.push(t('admin.accounts.modelSync.nativeDiscovered'))
  if (!sources.length) sources.push(t('admin.accounts.modelSync.manualSelection'))
  return sources.join(' · ')
}
const ensureRestrictedModeOnEdit = () => {
  if (currentMode.value !== 'restricted') {
    localModeOverride.value = 'restricted'
    emit('update:mode', 'restricted')
  }
}

const toggleAliasTargetPublic = (target: string, checked: boolean) => {
  ensureRestrictedModeOnEdit()
  const validTargets = new Set(aliasTargets.value)
  const next = new Set(effectivePublicTargets.value.map(item => item.trim()).filter(item => validTargets.has(item)))
  if (checked) {
    next.add(target)
  } else {
    next.delete(target)
  }
  setPublicTargets([...next])
}

const selectMode = (nextMode: AccountModelSelectionMode) => {
  if (!props.active || selectionBusy.value) return
  syncError.value = ''
  if (nextMode === 'deny_all') {
    denyAllReasonDraft.value = localReason.value.trim()
    denyAllReasonError.value = ''
    confirmClear.value = true
    void nextTick(() => denyAllReasonInput.value?.focus())
    return
  }
  if (nextMode === 'inherit') {
    if (props.modelValue.length > 0 || (props.modelMappings?.length ?? 0) > 0 || effectivePublicTargets.value.length > 0) {
      stashedRestrictedDraft.value = {
        models: [...props.modelValue],
        mappings: (props.modelMappings ?? []).map(entry => ({ ...entry })),
        publicTargets: [...effectivePublicTargets.value]
      }
      emit('update:modelValue', [])
      emit('update:modelMappings', [])
      setPublicTargets([])
    }
    localModeOverride.value = 'inherit'
    emit('update:mode', 'inherit')
    return
  }
  localModeOverride.value = 'restricted'
  emit('update:mode', 'restricted')
  if (!props.modelValue.length && !(props.modelMappings?.length) && stashedRestrictedDraft.value) {
    emit('update:modelValue', [...stashedRestrictedDraft.value.models])
    emit('update:modelMappings', stashedRestrictedDraft.value.mappings.map(entry => ({ ...entry })))
    setPublicTargets([...stashedRestrictedDraft.value.publicTargets])
  }
}

const openClearConfirmation = () => {
  denyAllReasonDraft.value = localReason.value.trim() || t('admin.accounts.modelSync.defaultDenyAllReason')
  denyAllReasonError.value = ''
  confirmClear.value = true
  void nextTick(() => denyAllReasonInput.value?.focus())
}

const cancelDenyAll = () => {
  confirmClear.value = false
  denyAllReasonError.value = ''
}

const removeMapping = (index: number) => {
  const current = props.modelMappings ?? []
  if (current.length === 1 && !props.modelValue.length && props.allowDenyAll !== false) {
    openClearConfirmation()
    return
  }
  emit('update:modelMappings', current.filter((_, position) => position !== index))
}

const updateMapping = (index: number, field: keyof ModelMappingEntry, event: Event) => {
  const value = (event.target as HTMLInputElement).value
  const current = props.modelMappings ?? []
  ensureRestrictedModeOnEdit()
  emit(
    'update:modelMappings',
    current.map((mapping, position) => (position === index ? { ...mapping, [field]: value } : mapping))
  )
}

const addMapping = (mapping: ModelMappingEntry = { from: '', to: '' }) => {
  const current = props.modelMappings ?? []
  if (mapping.from && current.some(existing => existing.from === mapping.from)) return
  ensureRestrictedModeOnEdit()
  if (mappingDetails.value) mappingDetails.value.open = true
  emit('update:modelMappings', [...current, { from: mapping.from, to: mapping.to }])
}

const selectFilteredModels = () => {
  ensureRestrictedModeOnEdit()
  emit('update:modelValue', [...new Set([...props.modelValue, ...filteredModels.value.map(model => model.value).filter(model => !configuredModelKeys.value.has(model))])])
}

const toggleDropdown = () => {
  showDropdown.value = !showDropdown.value
  if (!showDropdown.value) searchQuery.value = ''
}

const removeModel = (model: string) => {
  if (configuredModelKeys.value.size === 1 && props.allowDenyAll !== false) { openClearConfirmation(); return }
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
    ensureRestrictedModeOnEdit()
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
  ensureRestrictedModeOnEdit()
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
  if (newModels.length > 0) ensureRestrictedModeOnEdit()
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
  ensureRestrictedModeOnEdit()
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
    catalogState.value = result.discovered_models?.length ? 'ready' : result.discovery_complete ? 'empty' : 'unsynced'
    if (!props.accountId && result.discovery_complete) emit('upstream-synced')

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

defineExpose({ requestClear: openClearConfirmation, revealSelection, revealValidation })

const clearAll = async () => {
  if (!confirmClear.value || !props.active || selectionBusy.value) return
  const trimmedReason = denyAllReasonDraft.value.trim()
  if (!trimmedReason) {
    denyAllReasonError.value = t('admin.accounts.modelValidation.denyAllReasonRequired')
    syncError.value = denyAllReasonError.value
    void nextTick(() => denyAllReasonInput.value?.focus())
    return
  }
  confirmClear.value = false
  denyAllReasonError.value = ''
  if (props.accountId) {
    await saveSelection(true, trimmedReason)
    return
  }
  localReason.value = trimmedReason
  localModeOverride.value = 'deny_all'
  emit('update:reason', trimmedReason)
  emit('update:mode', 'deny_all')
  emit('update:modelValue', [])
}

</script>
