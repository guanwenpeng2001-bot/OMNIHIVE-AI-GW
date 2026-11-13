<template>
  <div class="card border-[var(--border)] bg-[var(--card)] shadow-none">
    <div class="border-b border-[var(--border)] px-4 py-4 sm:px-6">
      <h2 class="text-base font-semibold tracking-tight text-[var(--foreground)]">
        {{ t("admin.settings.overloadCooldown.title") }}
      </h2>
      <p class="mt-1 text-sm text-[var(--muted-foreground)]">
        {{ t("admin.settings.overloadCooldown.description") }}
      </p>
    </div>
    <div class="space-y-5 p-4 sm:p-6">
      <div
        v-if="overloadCooldownLoading"
        class="flex items-center gap-2 text-[var(--muted-foreground)]"
      >
        <LoadingSpinner size="sm" color="primary" />
        {{ t("common.loading") }}
      </div>

      <template v-else>
        <div class="flex items-center justify-between">
          <div>
            <label class="font-medium text-[var(--foreground)]">{{
              t("admin.settings.overloadCooldown.enabled")
            }}</label>
            <p class="text-sm text-[var(--muted-foreground)]">
              {{ t("admin.settings.overloadCooldown.enabledHint") }}
            </p>
          </div>
          <Toggle v-model="overloadCooldownForm.enabled" />
        </div>

        <div
          v-if="overloadCooldownForm.enabled"
          class="space-y-4 border-t border-[var(--border)] pt-4"
        >
          <div>
            <label
              class="mb-2 block text-sm font-medium text-[var(--foreground)]"
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
            <p class="mt-1.5 text-xs text-[var(--muted-foreground)]">
              {{ t("admin.settings.overloadCooldown.cooldownMinutesHint") }}
            </p>
          </div>
        </div>

        <div
          class="flex justify-end border-t border-[var(--border)] pt-4"
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
