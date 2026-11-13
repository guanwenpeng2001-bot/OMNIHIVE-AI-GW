<template>
  <div class="card border-[var(--border)] bg-[var(--card)] shadow-none">
    <div class="border-b border-[var(--border)] px-4 py-4 sm:px-6">
      <h2 class="text-base font-semibold tracking-tight text-[var(--foreground)]">
        {{ t("admin.settings.scheduling.title") }}
      </h2>
      <p class="mt-1 text-sm text-[var(--muted-foreground)]">
        {{ t("admin.settings.scheduling.description") }}
      </p>
    </div>
    <div class="space-y-5 p-4 sm:p-6">
      <div class="flex items-center justify-between">
        <div>
          <label class="text-sm font-medium text-[var(--foreground)]">
            {{ t("admin.settings.scheduling.allowUngroupedKey") }}
          </label>
          <p class="mt-0.5 text-xs text-[var(--muted-foreground)]">
            {{ t("admin.settings.scheduling.allowUngroupedKeyHint") }}
          </p>
        </div>
        <Toggle v-model="form.allow_ungrouped_key_scheduling" />
      </div>

      <div class="border-t border-[var(--border)] pt-4">
        <div class="mb-3">
          <label class="font-medium text-[var(--foreground)]">
            {{
              t("admin.settings.scheduling.accountSchedulingThresholdsTitle")
            }}
          </label>
          <p class="mt-1 text-sm text-[var(--muted-foreground)]">
            {{
              t(
                "admin.settings.scheduling.accountSchedulingThresholdsDescription",
              )
            }}
          </p>
          <p class="mt-0.5 text-xs text-[var(--muted-foreground)]">
            {{
              t(
                "admin.settings.scheduling.accountSchedulingThresholdsGlobalHint",
              )
            }}
          </p>
          <p class="mt-0.5 text-xs text-[var(--warn-text)]">
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
            class="rounded-lg border border-[var(--border)] p-4"
          >
            <div class="flex items-start justify-between gap-3">
              <div>
                <label
                  class="font-mono text-sm font-medium text-[var(--foreground)]"
                >
                  {{ platform }}
                </label>
                <p class="mt-0.5 text-xs text-[var(--muted-foreground)]">
                  {{
                    t(
                      "admin.settings.scheduling.accountSchedulingThresholdsRangeHint",
                    )
                  }}
                </p>
              </div>
              <span
                class="rounded bg-[var(--surface-2)] px-2 py-0.5 text-[11px] font-medium text-[var(--muted-foreground)]"
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
        class="flex items-center justify-between border-t border-[var(--border)] pt-5"
      >
        <div>
          <label class="text-sm font-medium text-[var(--foreground)]">
            {{
              t(
                "admin.settings.openaiExperimentalScheduler.lowRatePriorityTitle",
              )
            }}
          </label>
          <p class="mt-0.5 text-xs text-[var(--muted-foreground)]">
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
        class="flex flex-col items-stretch gap-3 border-t border-[var(--border)] pt-5 sm:flex-row sm:items-start sm:justify-between sm:gap-6"
      >
        <div class="min-w-0">
          <label
            class="text-sm font-medium text-[var(--foreground)]"
            for="openai-oauth-scheduling-rate-multiplier"
          >
            {{ t("admin.settings.openaiExperimentalScheduler.oauthRateTitle") }}
          </label>
          <p class="mt-0.5 text-xs text-[var(--muted-foreground)]">
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
            class="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-sm text-[var(--muted-foreground)]"
            >x</span
          >
        </div>
      </div>

      <div
        class="flex items-center justify-between border-t border-[var(--border)] pt-5"
      >
        <div>
          <label class="text-sm font-medium text-[var(--foreground)]">
            {{ t("admin.settings.openaiExperimentalScheduler.title") }}
          </label>
          <p class="mt-0.5 text-xs text-[var(--muted-foreground)]">
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
        class="flex items-center justify-between border-t border-[var(--border)] pt-5"
      >
        <div>
          <label class="text-sm font-medium text-[var(--foreground)]">
            {{
              t(
                "admin.settings.openaiExperimentalScheduler.stickyWeightedTitle",
              )
            }}
          </label>
          <p class="mt-0.5 text-xs text-[var(--muted-foreground)]">
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
        class="flex items-center justify-between border-t border-[var(--border)] pt-5"
      >
        <div>
          <label class="text-sm font-medium text-[var(--foreground)]">
            {{
              t(
                "admin.settings.openaiExperimentalScheduler.subscriptionPriorityTitle",
              )
            }}
          </label>
          <p class="mt-0.5 text-xs text-[var(--muted-foreground)]">
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
        class="flex flex-col items-stretch gap-3 border-t border-[var(--border)] pt-5 sm:flex-row sm:items-start sm:justify-between sm:gap-6"
      >
        <div class="min-w-0">
          <label
            class="text-sm font-medium text-[var(--foreground)]"
            for="openai-oauth-scheduling-rate-multiplier"
          >
            {{ t("admin.settings.openaiExperimentalScheduler.oauthRateTitle") }}
          </label>
          <p class="mt-0.5 text-xs text-[var(--muted-foreground)]">
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
            class="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-sm text-[var(--muted-foreground)]"
            >x</span
          >
        </div>
      </div>

      <div
        v-if="form.openai_advanced_scheduler_enabled"
        class="border-t border-[var(--border)] pt-5"
      >
        <div>
          <label class="text-sm font-medium text-[var(--foreground)]">
            {{ t("admin.settings.openaiExperimentalScheduler.weightsTitle") }}
          </label>
          <p class="mt-0.5 text-xs text-[var(--muted-foreground)]">
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
            <span class="text-xs font-medium text-[var(--muted-foreground)]">
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
