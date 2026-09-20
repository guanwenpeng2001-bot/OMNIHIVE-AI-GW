<template>
  <div
    v-if="wsTestDialogOpen"
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
    @click.self="wsTestDialogOpen = false"
  >
    <div
      class="mx-4 w-full max-w-lg rounded-xl bg-white p-6 shadow-xl dark:bg-dark-800"
    >
      <h3 class="mb-4 text-lg font-semibold text-gray-900 dark:text-white">
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
        class="mt-4 max-h-80 overflow-y-auto rounded-lg bg-gray-50 p-4 dark:bg-dark-700"
      >
        <p class="mb-2 text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t("admin.settings.webSearchEmulation.testResultProvider") }}:
          {{ wsTestResult.provider }}
        </p>
        <div
          v-if="wsTestResult.results.length === 0"
          class="text-sm text-gray-400"
        >
          {{ t("admin.settings.webSearchEmulation.testNoResults") }}
        </div>
        <div
          v-for="(r, rIdx) in wsTestResult.results"
          :key="rIdx"
          class="mt-2 border-t border-gray-200 pt-2 first:mt-0 first:border-0 first:pt-0 dark:border-dark-600"
        >
          <a
            :href="r.url"
            target="_blank"
            class="text-sm font-medium text-blue-600 hover:underline dark:text-blue-400"
            >{{ r.title }}</a
          >
          <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
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
const {
  t,
  wsTestQuery,
  wsTestLoading,
  wsTestResult,
  wsTestDialogOpen,
  testWebSearchProvider,
} = useSettingsContext();
</script>
