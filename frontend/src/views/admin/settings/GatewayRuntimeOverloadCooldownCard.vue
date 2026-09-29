<template>
  <div class="card">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
        {{ t("admin.settings.overloadCooldown.title") }}
      </h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
        {{ t("admin.settings.overloadCooldown.description") }}
      </p>
    </div>
    <div class="space-y-5 p-6">
      <div
        v-if="overloadCooldownLoading"
        class="flex items-center gap-2 text-gray-500"
      >
        <LoadingSpinner size="sm" color="primary" />
        {{ t("common.loading") }}
      </div>

      <template v-else>
        <div class="flex items-center justify-between">
          <div>
            <label class="font-medium text-gray-900 dark:text-white">{{
              t("admin.settings.overloadCooldown.enabled")
            }}</label>
            <p class="text-sm text-gray-500 dark:text-gray-400">
              {{ t("admin.settings.overloadCooldown.enabledHint") }}
            </p>
          </div>
          <Toggle v-model="overloadCooldownForm.enabled" />
        </div>

        <div
          v-if="overloadCooldownForm.enabled"
          class="space-y-4 border-t border-gray-100 pt-4 dark:border-dark-700"
        >
          <div>
            <label
              class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
            >
              {{ t("admin.settings.overloadCooldown.cooldownMinutes") }}
            </label>
            <input
              v-model.number="overloadCooldownForm.cooldown_minutes"
              type="number"
              min="1"
              max="120"
              class="input w-32"
            />
            <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t("admin.settings.overloadCooldown.cooldownMinutesHint") }}
            </p>
          </div>
        </div>

        <div
          class="flex justify-end border-t border-gray-100 pt-4 dark:border-dark-700"
        >
          <button
            type="button"
            @click="saveOverloadCooldownSettings"
            :disabled="overloadCooldownSaving"
            class="btn btn-primary btn-sm"
          >
            <LoadingSpinner v-if="overloadCooldownSaving" size="sm" color="white" />
            {{ overloadCooldownSaving ? t("common.saving") : t("common.save") }}
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
  overloadCooldownLoading,
  overloadCooldownSaving,
  overloadCooldownForm,
  saveOverloadCooldownSettings,
} = useSettingsContext();
</script>
