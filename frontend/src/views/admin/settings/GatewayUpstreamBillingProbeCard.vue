<template>
  <div class="card border-[var(--border)] bg-[var(--card)] shadow-none" data-testid="upstream-billing-probe-settings">
    <div class="border-b border-[var(--border)] px-4 py-4 sm:px-6">
      <h2 class="text-base font-semibold tracking-tight text-[var(--foreground)]">
        {{ t("admin.settings.upstreamBillingProbe.title") }}
      </h2>
      <p class="mt-1 text-sm text-[var(--muted-foreground)]">
        {{ t("admin.settings.upstreamBillingProbe.description") }}
      </p>
    </div>
    <div class="space-y-5 p-4 sm:p-6">
      <div
        v-if="upstreamBillingProbeLoading"
        class="flex items-center gap-2 text-[var(--muted-foreground)]"
      >
        <LoadingSpinner size="sm" color="primary" />
        {{ t("common.loading") }}
      </div>

      <template v-else>
        <div class="flex items-center justify-between gap-4">
          <div>
            <label class="font-medium text-[var(--foreground)]">
              {{ t("admin.settings.upstreamBillingProbe.enabled") }}
            </label>
            <p class="text-sm text-[var(--muted-foreground)]">
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
          class="border-t border-[var(--border)] pt-4"
        >
          <label
            class="mb-2 block text-sm font-medium text-[var(--foreground)]"
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
          <p class="mt-1.5 text-xs text-[var(--muted-foreground)]">
            {{ t("admin.settings.upstreamBillingProbe.intervalHint") }}
          </p>
        </div>

        <div
          class="flex justify-end border-t border-[var(--border)] pt-4"
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
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
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
