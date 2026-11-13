<template>
  <div class="card border-[var(--border)] bg-[var(--card)] shadow-none">
    <div class="border-b border-[var(--border)] px-4 py-4 sm:px-6">
      <h2 class="text-base font-semibold tracking-tight text-[var(--foreground)]">
        {{ t("admin.settings.streamTimeout.title") }}
      </h2>
      <p class="mt-1 text-sm text-[var(--muted-foreground)]">
        {{ t("admin.settings.streamTimeout.description") }}
      </p>
    </div>
    <div class="space-y-5 p-4 sm:p-6">
      <!-- Loading State -->
      <div
        v-if="streamTimeoutLoading"
        class="flex items-center gap-2 text-[var(--muted-foreground)]"
      >
        <LoadingSpinner size="sm" color="primary" />
        {{ t("common.loading") }}
      </div>

      <template v-else>
        <!-- Enable Stream Timeout -->
        <div class="flex items-center justify-between">
          <div>
            <label class="font-medium text-[var(--foreground)]">{{
              t("admin.settings.streamTimeout.enabled")
            }}</label>
            <p class="text-sm text-[var(--muted-foreground)]">
              {{ t("admin.settings.streamTimeout.enabledHint") }}
            </p>
          </div>
          <Toggle v-model="streamTimeoutForm.enabled" />
        </div>

        <!-- Settings - Only show when enabled -->
        <div
          v-if="streamTimeoutForm.enabled"
          class="space-y-4 border-t border-[var(--border)] pt-4"
        >
          <!-- Action -->
          <div>
            <label
              class="mb-2 block text-sm font-medium text-[var(--foreground)]"
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
            <p class="mt-1.5 text-xs text-[var(--muted-foreground)]">
              {{ t("admin.settings.streamTimeout.actionHint") }}
            </p>
          </div>

          <!-- Temp Unsched Minutes (only show when action is temp_unsched) -->
          <div v-if="streamTimeoutForm.action === 'temp_unsched'">
            <label
              class="mb-2 block text-sm font-medium text-[var(--foreground)]"
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
            <p class="mt-1.5 text-xs text-[var(--muted-foreground)]">
              {{ t("admin.settings.streamTimeout.tempUnschedMinutesHint") }}
            </p>
          </div>

          <!-- Threshold Count -->
          <div>
            <label
              class="mb-2 block text-sm font-medium text-[var(--foreground)]"
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
            <p class="mt-1.5 text-xs text-[var(--muted-foreground)]">
              {{ t("admin.settings.streamTimeout.thresholdCountHint") }}
            </p>
          </div>

          <!-- Threshold Window Minutes -->
          <div>
            <label
              class="mb-2 block text-sm font-medium text-[var(--foreground)]"
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
            <p class="mt-1.5 text-xs text-[var(--muted-foreground)]">
              {{ t("admin.settings.streamTimeout.thresholdWindowMinutesHint") }}
            </p>
          </div>
        </div>

        <!-- Save Button -->
        <div
          class="flex justify-end border-t border-[var(--border)] pt-4"
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
