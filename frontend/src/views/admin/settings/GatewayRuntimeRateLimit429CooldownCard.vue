<template>
  <div class="card">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
        {{ t("admin.settings.rateLimit429Cooldown.title") }}
      </h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
        {{ t("admin.settings.rateLimit429Cooldown.description") }}
      </p>
    </div>
    <div class="space-y-5 p-6">
      <div
        v-if="rateLimit429CooldownLoading"
        class="flex items-center gap-2 text-gray-500"
      >
        <LoadingSpinner size="sm" color="primary" />
        {{ t("common.loading") }}
      </div>

      <template v-else>
        <div class="flex items-center justify-between">
          <div>
            <label class="font-medium text-gray-900 dark:text-white">{{
              t("admin.settings.rateLimit429Cooldown.enabled")
            }}</label>
            <p class="text-sm text-gray-500 dark:text-gray-400">
              {{ t("admin.settings.rateLimit429Cooldown.enabledHint") }}
            </p>
          </div>
          <Toggle v-model="rateLimit429CooldownForm.enabled" />
        </div>

        <div
          v-if="rateLimit429CooldownForm.enabled"
          class="space-y-4 border-t border-gray-100 pt-4 dark:border-dark-700"
        >
          <div>
            <label
              class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
            >
              {{ t("admin.settings.rateLimit429Cooldown.cooldownSeconds") }}
            </label>
            <input
              v-model.number="rateLimit429CooldownForm.cooldown_seconds"
              type="number"
              min="1"
              max="7200"
              class="input w-32"
            />
            <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t("admin.settings.rateLimit429Cooldown.cooldownSecondsHint") }}
            </p>
          </div>
        </div>

        <div
          class="flex justify-end border-t border-gray-100 pt-4 dark:border-dark-700"
        >
          <button
            type="button"
            @click="saveRateLimit429CooldownSettings"
            :disabled="rateLimit429CooldownSaving"
            class="btn btn-primary btn-sm"
          >
            <LoadingSpinner v-if="rateLimit429CooldownSaving" size="sm" color="white" />
            {{
              rateLimit429CooldownSaving ? t("common.saving") : t("common.save")
            }}
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
  rateLimit429CooldownLoading,
  rateLimit429CooldownSaving,
  rateLimit429CooldownForm,
  saveRateLimit429CooldownSettings,
} = useSettingsContext();
</script>
