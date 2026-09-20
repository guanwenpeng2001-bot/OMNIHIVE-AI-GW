<template>
  <div class="card" data-testid="upstream-billing-probe-settings">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
        {{ t("admin.settings.upstreamBillingProbe.title") }}
      </h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
        {{ t("admin.settings.upstreamBillingProbe.description") }}
      </p>
    </div>
    <div class="space-y-5 p-6">
      <div
        v-if="upstreamBillingProbeLoading"
        class="flex items-center gap-2 text-gray-500"
      >
        <div
          class="h-4 w-4 animate-spin rounded-full border-b-2 border-primary-600"
        ></div>
        {{ t("common.loading") }}
      </div>

      <template v-else>
        <div class="flex items-center justify-between gap-4">
          <div>
            <label class="font-medium text-gray-900 dark:text-white">
              {{ t("admin.settings.upstreamBillingProbe.enabled") }}
            </label>
            <p class="text-sm text-gray-500 dark:text-gray-400">
              {{ t("admin.settings.upstreamBillingProbe.enabledHint") }}
            </p>
          </div>
          <Toggle
            v-model="upstreamBillingProbeForm.enabled"
            :aria-label="t('admin.settings.upstreamBillingProbe.enabled')"
            data-testid="upstream-billing-probe-enabled"
          />
        </div>

        <div
          v-if="upstreamBillingProbeForm.enabled"
          class="border-t border-gray-100 pt-4 dark:border-dark-700"
        >
          <label
            class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
            for="upstream-billing-probe-interval"
          >
            {{ t("admin.settings.upstreamBillingProbe.intervalMinutes") }}
          </label>
          <input
            id="upstream-billing-probe-interval"
            v-model.number="upstreamBillingProbeForm.interval_minutes"
            type="number"
            min="5"
            max="1440"
            class="input w-32"
            data-testid="upstream-billing-probe-interval"
            @keydown.enter.prevent="saveUpstreamBillingProbeSettings"
          />
          <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
            {{ t("admin.settings.upstreamBillingProbe.intervalHint") }}
          </p>
        </div>

        <div
          class="flex justify-end border-t border-gray-100 pt-4 dark:border-dark-700"
        >
          <button
            type="button"
            class="btn btn-primary btn-sm"
            :disabled="upstreamBillingProbeSaving"
            data-testid="upstream-billing-probe-save"
            @click="saveUpstreamBillingProbeSettings"
          >
            {{
              upstreamBillingProbeSaving ? t("common.saving") : t("common.save")
            }}
          </button>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import Toggle from "@/components/common/Toggle.vue";
import { useSettingsContext } from "./settingsContext";
const {
  t,
  upstreamBillingProbeLoading,
  upstreamBillingProbeSaving,
  upstreamBillingProbeForm,
  saveUpstreamBillingProbeSettings,
} = useSettingsContext();
</script>
