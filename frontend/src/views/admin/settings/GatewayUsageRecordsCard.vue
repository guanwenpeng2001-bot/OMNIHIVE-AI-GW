<template>
  <div class="card border-[var(--border)] bg-[var(--card)] shadow-none">
    <div class="border-b border-[var(--border)] px-4 py-4 sm:px-6">
      <h2 class="text-base font-semibold tracking-tight text-[var(--foreground)]">
        {{ t("admin.settings.usageRecords.title") }}
      </h2>
      <p class="mt-1 text-sm text-[var(--muted-foreground)]">
        {{ t("admin.settings.usageRecords.description") }}
      </p>
    </div>
    <div class="space-y-4 p-4 sm:p-6">
      <!-- User error requests visibility -->
      <div class="flex items-center justify-between">
        <div>
          <label class="text-sm font-medium text-[var(--foreground)]">
            {{ t("admin.settings.user_error_view.label") }}
          </label>
          <p class="text-xs text-[var(--muted-foreground)]">
            {{ t("admin.settings.user_error_view.description") }}
          </p>
        </div>
        <label class="toggle">
          <input
            v-model="form.allow_user_view_error_requests"
            type="checkbox"
          />
          <span class="toggle-slider"></span>
        </label>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useSettingsContext } from "./settingsContext";
const { t, form } = useSettingsContext();
</script>
