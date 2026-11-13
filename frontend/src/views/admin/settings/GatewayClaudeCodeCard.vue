<template>
  <div class="card border-[var(--border)] bg-[var(--card)] shadow-none">
    <div class="border-b border-[var(--border)] px-4 py-4 sm:px-6">
      <h2 class="text-base font-semibold tracking-tight text-[var(--foreground)]">
        {{ t("admin.settings.claudeCode.title") }}
      </h2>
      <p class="mt-1 text-sm text-[var(--muted-foreground)]">
        {{ t("admin.settings.claudeCode.description") }}
      </p>
    </div>
    <div class="p-6">
      <div>
        <label
          class="mb-2 block text-sm font-medium text-[var(--foreground)]"
        >
          {{ t("admin.settings.claudeCode.minVersion") }}
        </label>
        <input
          v-model="form.min_claude_code_version"
          type="text"
          class="input max-w-xs font-mono text-sm"
          :placeholder="t('admin.settings.claudeCode.minVersionPlaceholder')"
        />
        <p class="mt-1.5 text-xs text-[var(--muted-foreground)]">
          {{ t("admin.settings.claudeCode.minVersionHint") }}
        </p>
      </div>
      <div class="mt-4">
        <label
          class="mb-2 block text-sm font-medium text-[var(--foreground)]"
        >
          {{ t("admin.settings.claudeCode.maxVersion") }}
        </label>
        <input
          v-model="form.max_claude_code_version"
          type="text"
          class="input max-w-xs font-mono text-sm"
          :placeholder="t('admin.settings.claudeCode.maxVersionPlaceholder')"
        />
        <p class="mt-1.5 text-xs text-[var(--muted-foreground)]">
          {{ t("admin.settings.claudeCode.maxVersionHint") }}
        </p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useSettingsContext } from "./settingsContext";
const { t, form } = useSettingsContext();
</script>
