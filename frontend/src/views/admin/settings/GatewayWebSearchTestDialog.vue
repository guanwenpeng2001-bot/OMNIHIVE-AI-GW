<template>
  <div
    v-if="wsTestDialogOpen"
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
    @click.self="wsTestDialogOpen = false"
  >
    <div
      class="mx-4 w-full max-w-lg rounded-xl bg-[var(--card)] p-6 shadow-xl"
    >
      <h3 class="mb-4 text-base font-semibold tracking-tight text-[var(--foreground)]">
        {{ t("admin.settings.webSearchEmulation.testResultTitle") }}
      </h3>
      <div class="flex items-center gap-2">
        <input
          v-model="wsTestQuery"
          type="text"
          class="input flex-1 text-sm"
          :placeholder="t('admin.settings.webSearchEmulation.testDefaultQuery')"
          @keyup.enter="testWebSearchProvider()"
        />
        <button
          type="button"
          class="btn btn-primary btn-sm"
          :disabled="wsTestLoading"
          @click="testWebSearchProvider()"
        >
          {{
            wsTestLoading
              ? t("admin.settings.webSearchEmulation.testing")
              : t("admin.settings.webSearchEmulation.test")
          }}
        </button>
      </div>
      <!-- Test results -->
      <div
        v-if="wsTestResult"
        class="mt-4 max-h-80 overflow-y-auto rounded-lg bg-[var(--surface-2)] p-4"
      >
        <p class="mb-2 text-sm font-medium text-[var(--foreground)]">
          {{ t("admin.settings.webSearchEmulation.testResultProvider") }}:
          {{ wsTestResult.provider }}
        </p>
        <div
          v-if="wsTestResult.results.length === 0"
          class="text-sm text-[var(--muted-foreground)]"
        >
          {{ t("admin.settings.webSearchEmulation.testNoResults") }}
        </div>
        <div
          v-for="(r, rIdx) in wsTestResult.results"
          :key="rIdx"
          class="mt-2 border-t border-[var(--border)] pt-2 first:mt-0 first:border-0 first:pt-0"
        >
          <a
            v-if="sanitizeUrl(r.url)"
            :href="sanitizeUrl(r.url)"
            target="_blank"
            rel="noopener noreferrer"
            class="text-sm font-medium text-[var(--info-text)] hover:underline"
            >{{ r.title }}</a
          >
          <span
            v-else
            class="text-sm font-medium text-[var(--foreground)]"
            >{{ r.title }}</span
          >
          <p class="mt-0.5 text-xs text-[var(--muted-foreground)]">
            {{ r.snippet }}
          </p>
        </div>
      </div>
      <div class="mt-4 flex justify-end">
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          @click="wsTestDialogOpen = false"
        >
          {{ t("common.close") }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useSettingsContext } from "./settingsContext";
import { sanitizeUrl } from "@/utils/url";
const {
  t,
  wsTestQuery,
  wsTestLoading,
  wsTestResult,
  wsTestDialogOpen,
  testWebSearchProvider,
} = useSettingsContext();
</script>
