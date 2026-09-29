<template>
  <div class="card" data-testid="ollama-cloud-usage-global-settings">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
        {{ t("admin.settings.ollamaCloudUsage.title") }}
      </h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
        {{ t("admin.settings.ollamaCloudUsage.description") }}
      </p>
    </div>
    <div class="space-y-5 p-6">
      <div
        v-if="ollamaCloudUsageLoading"
        class="flex items-center gap-2 text-gray-500"
      >
        <LoadingSpinner size="sm" color="primary" />
        {{ t("common.loading") }}
      </div>
      <template v-else>
        <div class="flex items-center justify-between gap-4">
          <div>
            <label class="font-medium text-gray-900 dark:text-white">
              {{ t("admin.settings.ollamaCloudUsage.enabled") }}
            </label>
            <p class="text-sm text-gray-500 dark:text-gray-400">
              {{ t("admin.settings.ollamaCloudUsage.enabledHint") }}
            </p>
          </div>
          <Toggle
            v-model="ollamaCloudUsageForm.enabled"
            :aria-label="t('admin.settings.ollamaCloudUsage.enabled')"
            data-testid="ollama-cloud-usage-global-enabled"
          />
        </div>
        <div
          v-if="ollamaCloudUsageForm.enabled"
          class="space-y-4 border-t border-gray-100 pt-4 dark:border-dark-700"
        >
          <div>
            <label
              class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
              for="ollama-cloud-usage-debounce"
            >
              {{ t("admin.settings.ollamaCloudUsage.debounceMinutes") }}
            </label>
            <input
              id="ollama-cloud-usage-debounce"
              v-model.number="ollamaCloudUsageForm.debounce_minutes"
              type="number"
              min="1"
              max="60"
              class="input w-32"
              data-testid="ollama-cloud-usage-global-debounce"
              @keydown.enter.prevent="saveOllamaCloudUsageSettings"
            />
            <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t("admin.settings.ollamaCloudUsage.debounceHint") }}
            </p>
          </div>
          <div>
            <label
              class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
              for="ollama-cloud-usage-interval"
            >
              {{ t("admin.settings.ollamaCloudUsage.intervalMinutes") }}
            </label>
            <input
              id="ollama-cloud-usage-interval"
              v-model.number="ollamaCloudUsageForm.interval_minutes"
              type="number"
              min="15"
              max="1440"
              class="input w-32"
              data-testid="ollama-cloud-usage-global-interval"
              @keydown.enter.prevent="saveOllamaCloudUsageSettings"
            />
            <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t("admin.settings.ollamaCloudUsage.intervalHint") }}
            </p>
          </div>
        </div>
        <div
          class="flex justify-end border-t border-gray-100 pt-4 dark:border-dark-700"
        >
          <button
            type="button"
            class="btn btn-primary btn-sm"
            :disabled="ollamaCloudUsageSaving"
            data-testid="ollama-cloud-usage-global-save"
            @click="saveOllamaCloudUsageSettings"
          >
            {{ ollamaCloudUsageSaving ? t("common.saving") : t("common.save") }}
          </button>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Toggle from "@/components/common/Toggle.vue";
import { useSettingsContext } from "./settingsContext";
const {
  t,
  ollamaCloudUsageLoading,
  ollamaCloudUsageSaving,
  ollamaCloudUsageForm,
  saveOllamaCloudUsageSettings,
} = useSettingsContext();
</script>
