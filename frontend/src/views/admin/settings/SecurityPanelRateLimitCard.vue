<template>
  <div class="card">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <div class="flex items-center gap-2">
        <Icon name="shield" size="md" class="text-primary-500" />
        <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
          {{ t("admin.settings.panelRateLimit.title") }}
        </h2>
      </div>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
        {{ t("admin.settings.panelRateLimit.description") }}
      </p>
    </div>
    <div class="space-y-5 p-6">
      <div
        v-if="panelRateLimitLoading"
        class="flex items-center gap-2 text-gray-500"
      >
        <div
          class="h-4 w-4 animate-spin rounded-full border-b-2 border-primary-600"
        ></div>
        {{ t("common.loading") }}
      </div>

      <template v-else>
        <!-- 计数维度说明：按账号计数，反代部署无误伤 -->
        <div
          class="rounded-lg border border-sky-200 bg-sky-50 p-4 dark:border-sky-800 dark:bg-sky-900/20"
        >
          <div class="flex items-start">
            <Icon
              name="infoCircle"
              size="md"
              class="mt-0.5 flex-shrink-0 text-sky-500"
            />
            <p class="ml-3 text-sm text-sky-700 dark:text-sky-300">
              {{ t("admin.settings.panelRateLimit.proxySafeNote") }}
            </p>
          </div>
        </div>

        <div class="flex items-center justify-between">
          <div>
            <label class="font-medium text-gray-900 dark:text-white">{{
              t("admin.settings.panelRateLimit.enabled")
            }}</label>
            <p class="text-sm text-gray-500 dark:text-gray-400">
              {{ t("admin.settings.panelRateLimit.enabledHint") }}
            </p>
          </div>
          <Toggle v-model="panelRateLimitForm.enabled" />
        </div>

        <div
          v-if="panelRateLimitForm.enabled"
          class="space-y-5 border-t border-gray-100 pt-4 dark:border-dark-700"
        >
          <div class="grid grid-cols-1 gap-6 sm:grid-cols-2">
            <div>
              <label
                class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
              >
                {{ t("admin.settings.panelRateLimit.userRpm") }}
              </label>
              <div class="flex items-center gap-2">
                <input
                  v-model.number="panelRateLimitForm.user_rpm"
                  data-testid="panel-rate-limit-user-rpm"
                  type="number"
                  min="0"
                  max="100000"
                  class="input w-32"
                />
                <span class="text-sm text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.panelRateLimit.perMinute") }}
                </span>
              </div>
              <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.panelRateLimit.userRpmHint") }}
              </p>
            </div>

            <div>
              <label
                class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
              >
                {{ t("admin.settings.panelRateLimit.heavyRpm") }}
              </label>
              <div class="flex items-center gap-2">
                <input
                  v-model.number="panelRateLimitForm.heavy_rpm"
                  type="number"
                  min="0"
                  max="100000"
                  class="input w-32"
                />
                <span class="text-sm text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.panelRateLimit.perMinute") }}
                </span>
              </div>
              <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.panelRateLimit.heavyRpmHint") }}
              </p>
            </div>

            <div>
              <label
                class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300"
              >
                {{ t("admin.settings.panelRateLimit.publicIpRpm") }}
              </label>
              <div class="flex items-center gap-2">
                <input
                  v-model.number="panelRateLimitForm.public_ip_rpm"
                  type="number"
                  min="0"
                  max="100000"
                  class="input w-32"
                />
                <span class="text-sm text-gray-500 dark:text-gray-400">
                  {{ t("admin.settings.panelRateLimit.perMinute") }}
                </span>
              </div>
              <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.panelRateLimit.publicIpRpmHint") }}
              </p>
            </div>
          </div>

          <div
            class="flex items-center justify-between border-t border-gray-100 pt-4 dark:border-dark-700"
          >
            <div>
              <label class="font-medium text-gray-900 dark:text-white">{{
                t("admin.settings.panelRateLimit.exemptAdmin")
              }}</label>
              <p class="text-sm text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.panelRateLimit.exemptAdminHint") }}
              </p>
            </div>
            <Toggle v-model="panelRateLimitForm.exempt_admin" />
          </div>
        </div>

        <div
          class="flex justify-end border-t border-gray-100 pt-4 dark:border-dark-700"
        >
          <button
            type="button"
            data-testid="panel-rate-limit-save"
            @click="savePanelRateLimitSettings"
            :disabled="panelRateLimitSaving"
            class="btn btn-primary btn-sm"
          >
            <svg
              v-if="panelRateLimitSaving"
              class="mr-1 h-4 w-4 animate-spin"
              fill="none"
              viewBox="0 0 24 24"
            >
              <circle
                class="opacity-25"
                cx="12"
                cy="12"
                r="10"
                stroke="currentColor"
                stroke-width="4"
              ></circle>
              <path
                class="opacity-75"
                fill="currentColor"
                d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
              ></path>
            </svg>
            {{ panelRateLimitSaving ? t("common.saving") : t("common.save") }}
          </button>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import Icon from "@/components/icons/Icon.vue";
import Toggle from "@/components/common/Toggle.vue";
import { useSettingsContext } from "./settingsContext";
const {
  t,
  panelRateLimitLoading,
  panelRateLimitSaving,
  panelRateLimitForm,
  savePanelRateLimitSettings,
} = useSettingsContext();
</script>
