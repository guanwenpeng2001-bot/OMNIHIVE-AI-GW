<template>
  <div class="card">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
        {{ t("admin.settings.streamTimeout.title") }}
      </h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
        {{ t("admin.settings.streamTimeout.description") }}
      </p>
    </div>
    <div class="space-y-5 p-6">
      <!-- Loading State -->
      <div
        v-if="streamTimeoutLoading"
        class="flex items-center gap-2 text-gray-500"
      >
        <LoadingSpinner size="sm" color="primary" />
        {{ t("common.loading") }}
      </div>

      <template v-else>
        <!-- Enable Stream Timeout -->
        <div class="flex items-center justify-between">
          <div>
            <label class="font-medium text-gray-900 dark:text-white">{{
              t("admin.settings.streamTimeout.enabled")
            }}</label>
            <p class="text-sm text-gray-500 dark:text-gray-400">
              {{ t("admin.settings.streamTimeout.enabledHint") }}
            </p>
          </div>
          <Toggle v-model="streamTimeoutForm.enabled" />
        </div>

        <!-- Settings - Only show when enabled -->
        <div
          v-if="streamTimeoutForm.enabled"
          class="space-y-4 border-t border-gray-100 pt-4 dark:border-dark-700"
        >
          <!-- Action -->
          <div>
            <label
              class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
            >
              {{ t("admin.settings.streamTimeout.action") }}
            </label>
            <select v-model="streamTimeoutForm.action" class="input w-64">
              <option value="temp_unsched">
                {{ t("admin.settings.streamTimeout.actionTempUnsched") }}
              </option>
              <option value="error">
                {{ t("admin.settings.streamTimeout.actionError") }}
              </option>
              <option value="none">
                {{ t("admin.settings.streamTimeout.actionNone") }}
              </option>
            </select>
            <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t("admin.settings.streamTimeout.actionHint") }}
            </p>
          </div>

          <!-- Temp Unsched Minutes (only show when action is temp_unsched) -->
          <div v-if="streamTimeoutForm.action === 'temp_unsched'">
            <label
              class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
            >
              {{ t("admin.settings.streamTimeout.tempUnschedMinutes") }}
            </label>
            <input
              v-model.number="streamTimeoutForm.temp_unsched_minutes"
              type="number"
              min="1"
              max="60"
              class="input w-32"
            />
            <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t("admin.settings.streamTimeout.tempUnschedMinutesHint") }}
            </p>
          </div>

          <!-- Threshold Count -->
          <div>
            <label
              class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
            >
              {{ t("admin.settings.streamTimeout.thresholdCount") }}
            </label>
            <input
              v-model.number="streamTimeoutForm.threshold_count"
              type="number"
              min="1"
              max="10"
              class="input w-32"
            />
            <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t("admin.settings.streamTimeout.thresholdCountHint") }}
            </p>
          </div>

          <!-- Threshold Window Minutes -->
          <div>
            <label
              class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
            >
              {{ t("admin.settings.streamTimeout.thresholdWindowMinutes") }}
            </label>
            <input
              v-model.number="streamTimeoutForm.threshold_window_minutes"
              type="number"
              min="1"
              max="60"
              class="input w-32"
            />
            <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t("admin.settings.streamTimeout.thresholdWindowMinutesHint") }}
            </p>
          </div>
        </div>

        <!-- Save Button -->
        <div
          class="flex justify-end border-t border-gray-100 pt-4 dark:border-dark-700"
        >
          <button
            type="button"
            @click="saveStreamTimeoutSettings"
            :disabled="streamTimeoutSaving"
            class="btn btn-primary btn-sm"
          >
            <LoadingSpinner v-if="streamTimeoutSaving" size="sm" color="white" />
            {{ streamTimeoutSaving ? t("common.saving") : t("common.save") }}
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
  streamTimeoutLoading,
  streamTimeoutSaving,
  streamTimeoutForm,
  saveStreamTimeoutSettings,
} = useSettingsContext();
</script>
