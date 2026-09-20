<template>
  <div class="card">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
        {{ t("admin.settings.scheduling.title") }}
      </h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
        {{ t("admin.settings.scheduling.description") }}
      </p>
    </div>
    <div class="space-y-5 p-6">
      <div class="flex items-center justify-between">
        <div>
          <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
            {{ t("admin.settings.scheduling.allowUngroupedKey") }}
          </label>
          <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
            {{ t("admin.settings.scheduling.allowUngroupedKeyHint") }}
          </p>
        </div>
        <Toggle v-model="form.allow_ungrouped_key_scheduling" />
      </div>

      <div class="border-t border-gray-100 pt-4 dark:border-dark-700">
        <div class="mb-3">
          <label class="font-medium text-gray-900 dark:text-white">
            {{
              t("admin.settings.scheduling.accountSchedulingThresholdsTitle")
            }}
          </label>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            {{
              t(
                "admin.settings.scheduling.accountSchedulingThresholdsDescription",
              )
            }}
          </p>
          <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
            {{
              t(
                "admin.settings.scheduling.accountSchedulingThresholdsGlobalHint",
              )
            }}
          </p>
          <p class="mt-0.5 text-xs text-amber-600 dark:text-amber-400">
            {{
              t(
                "admin.settings.scheduling.accountSchedulingThresholdsDisabledHint",
              )
            }}
          </p>
        </div>
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
          <div
            v-for="platform in schedulingThresholdPlatforms"
            :key="platform"
            class="rounded-lg border border-gray-200 p-4 dark:border-dark-700"
          >
            <div class="flex items-start justify-between gap-3">
              <div>
                <label
                  class="font-mono text-sm font-medium text-gray-900 dark:text-white"
                >
                  {{ platform }}
                </label>
                <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                  {{
                    t(
                      "admin.settings.scheduling.accountSchedulingThresholdsRangeHint",
                    )
                  }}
                </p>
              </div>
              <span
                class="rounded bg-gray-100 px-2 py-0.5 text-[11px] font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300"
              >
                %
              </span>
            </div>
            <input
              v-model.number="form.account_scheduling_thresholds[platform]"
              type="number"
              min="1"
              max="100"
              step="1"
              class="input mt-3"
              :data-testid="`account-scheduling-threshold-${platform}`"
              placeholder="100"
            />
          </div>
        </div>
      </div>

      <div
        v-if="!form.openai_advanced_scheduler_enabled"
        class="flex items-center justify-between border-t border-gray-100 pt-5 dark:border-dark-700"
      >
        <div>
          <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
            {{
              t(
                "admin.settings.openaiExperimentalScheduler.lowRatePriorityTitle",
              )
            }}
          </label>
          <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
            {{
              t(
                "admin.settings.openaiExperimentalScheduler.lowRatePriorityDescription",
              )
            }}
          </p>
        </div>
        <Toggle
          v-model="form.openai_low_upstream_rate_priority_enabled"
          data-testid="openai-low-rate-priority-toggle"
        />
      </div>

      <div
        v-if="
          !form.openai_advanced_scheduler_enabled &&
          form.openai_low_upstream_rate_priority_enabled
        "
        class="flex flex-col items-stretch gap-3 border-t border-gray-100 pt-5 sm:flex-row sm:items-start sm:justify-between sm:gap-6 dark:border-dark-700"
      >
        <div class="min-w-0">
          <label
            class="text-sm font-medium text-gray-700 dark:text-gray-300"
            for="openai-oauth-scheduling-rate-multiplier"
          >
            {{ t("admin.settings.openaiExperimentalScheduler.oauthRateTitle") }}
          </label>
          <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
            {{
              t(
                "admin.settings.openaiExperimentalScheduler.oauthRatePriorityDescription",
              )
            }}
          </p>
        </div>
        <div class="relative w-full shrink-0 sm:w-32">
          <input
            id="openai-oauth-scheduling-rate-multiplier"
            v-model.number="form.openai_oauth_scheduling_rate_multiplier"
            class="input pr-8"
            data-testid="openai-oauth-scheduling-rate-multiplier"
            min="0"
            required
            step="0.01"
            type="number"
          />
          <span
            class="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-sm text-gray-400"
            >x</span
          >
        </div>
      </div>

      <div
        class="flex items-center justify-between border-t border-gray-100 pt-5 dark:border-dark-700"
      >
        <div>
          <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
            {{ t("admin.settings.openaiExperimentalScheduler.title") }}
          </label>
          <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
            {{ t("admin.settings.openaiExperimentalScheduler.description") }}
          </p>
        </div>
        <Toggle
          v-model="form.openai_advanced_scheduler_enabled"
          data-testid="openai-advanced-scheduler-toggle"
        />
      </div>

      <div
        v-if="form.openai_advanced_scheduler_enabled"
        class="flex items-center justify-between border-t border-gray-100 pt-5 dark:border-dark-700"
      >
        <div>
          <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
            {{
              t(
                "admin.settings.openaiExperimentalScheduler.stickyWeightedTitle",
              )
            }}
          </label>
          <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
            {{
              t(
                "admin.settings.openaiExperimentalScheduler.stickyWeightedDescription",
              )
            }}
          </p>
        </div>
        <Toggle
          v-model="form.openai_advanced_scheduler_sticky_weighted_enabled"
        />
      </div>

      <div
        v-if="form.openai_advanced_scheduler_enabled"
        class="flex items-center justify-between border-t border-gray-100 pt-5 dark:border-dark-700"
      >
        <div>
          <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
            {{
              t(
                "admin.settings.openaiExperimentalScheduler.subscriptionPriorityTitle",
              )
            }}
          </label>
          <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
            {{
              t(
                "admin.settings.openaiExperimentalScheduler.subscriptionPriorityDescription",
              )
            }}
          </p>
        </div>
        <Toggle
          v-model="form.openai_advanced_scheduler_subscription_priority_enabled"
        />
      </div>

      <div
        v-if="form.openai_advanced_scheduler_enabled"
        class="flex flex-col items-stretch gap-3 border-t border-gray-100 pt-5 sm:flex-row sm:items-start sm:justify-between sm:gap-6 dark:border-dark-700"
      >
        <div class="min-w-0">
          <label
            class="text-sm font-medium text-gray-700 dark:text-gray-300"
            for="openai-oauth-scheduling-rate-multiplier"
          >
            {{ t("admin.settings.openaiExperimentalScheduler.oauthRateTitle") }}
          </label>
          <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
            {{
              t(
                "admin.settings.openaiExperimentalScheduler.oauthRateWeightedDescription",
              )
            }}
          </p>
        </div>
        <div class="relative w-full shrink-0 sm:w-32">
          <input
            id="openai-oauth-scheduling-rate-multiplier"
            v-model.number="form.openai_oauth_scheduling_rate_multiplier"
            class="input pr-8"
            data-testid="openai-oauth-scheduling-rate-multiplier"
            min="0"
            required
            step="0.01"
            type="number"
          />
          <span
            class="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-sm text-gray-400"
            >x</span
          >
        </div>
      </div>

      <div
        v-if="form.openai_advanced_scheduler_enabled"
        class="border-t border-gray-100 pt-5 dark:border-dark-700"
      >
        <div>
          <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
            {{ t("admin.settings.openaiExperimentalScheduler.weightsTitle") }}
          </label>
          <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
            {{
              t("admin.settings.openaiExperimentalScheduler.weightsDescription")
            }}
          </p>
        </div>

        <div class="mt-4 grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-5">
          <label
            v-for="field in openAIAdvancedSchedulerWeightFields"
            :key="field.key"
            class="block"
          >
            <span class="text-xs font-medium text-gray-600 dark:text-gray-400">
              {{ field.label }}
            </span>
            <input
              v-model="form[field.key]"
              class="input mt-1"
              inputmode="decimal"
              :placeholder="field.placeholder"
              type="text"
            />
          </label>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import Toggle from "@/components/common/Toggle.vue";
import { useSettingsContext } from "./settingsContext";
const {
  t,
  openAIAdvancedSchedulerWeightFields,
  schedulingThresholdPlatforms,
  form,
} = useSettingsContext();
</script>
