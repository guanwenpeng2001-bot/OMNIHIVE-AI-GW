<template>
  <div class="card border-[var(--border)] bg-[var(--card)] shadow-none">
    <div class="border-b border-[var(--border)] px-4 py-4 sm:px-6">
      <div class="flex items-center gap-2">
        <Icon name="shield" size="md" class="text-[var(--brand-text)]" />
        <h2 class="text-base font-semibold tracking-tight text-[var(--foreground)]">
          {{ t("admin.settings.panelRateLimit.title") }}
        </h2>
      </div>
      <p class="mt-1 text-sm text-[var(--muted-foreground)]">
        {{ t("admin.settings.panelRateLimit.description") }}
      </p>
    </div>
    <div class="space-y-5 p-4 sm:p-6">
      <div
        v-if="panelRateLimitLoading"
        class="flex items-center gap-2 text-[var(--muted-foreground)]"
      >
        <LoadingSpinner size="sm" color="primary" />
        {{ t("common.loading") }}
      </div>

      <template v-else>
        <!-- 计数维度说明：按账号计数，反代部署无误伤 -->
        <div
          class="rounded-lg border border-[var(--border)] bg-[var(--surface-2)] p-4"
        >
          <div class="flex items-start">
            <Icon
              name="infoCircle"
              size="md"
              class="mt-0.5 flex-shrink-0 text-sky-500"
            />
            <p class="ml-3 text-sm text-[var(--muted-foreground)]">
              {{ t("admin.settings.panelRateLimit.proxySafeNote") }}
            </p>
          </div>
        </div>

        <div class="flex items-center justify-between">
          <div>
            <label class="font-medium text-[var(--foreground)]">{{
              t("admin.settings.panelRateLimit.enabled")
            }}</label>
            <p class="text-sm text-[var(--muted-foreground)]">
              {{ t("admin.settings.panelRateLimit.enabledHint") }}
            </p>
          </div>
          <Toggle v-model="panelRateLimitForm.enabled" />
        </div>

        <div
          v-if="panelRateLimitForm.enabled"
          class="space-y-5 border-t border-[var(--border)] pt-4"
        >
          <div class="grid grid-cols-1 gap-6 sm:grid-cols-2">
            <div>
              <label
                class="mb-2 block text-sm font-medium text-[var(--foreground)]"
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
                <span class="text-sm text-[var(--muted-foreground)]">
                  {{ t("admin.settings.panelRateLimit.perMinute") }}
                </span>
              </div>
              <p class="mt-1.5 text-xs text-[var(--muted-foreground)]">
                {{ t("admin.settings.panelRateLimit.userRpmHint") }}
              </p>
            </div>

            <div>
              <label
                class="mb-2 block text-sm font-medium text-[var(--foreground)]"
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
                <span class="text-sm text-[var(--muted-foreground)]">
                  {{ t("admin.settings.panelRateLimit.perMinute") }}
                </span>
              </div>
              <p class="mt-1.5 text-xs text-[var(--muted-foreground)]">
                {{ t("admin.settings.panelRateLimit.heavyRpmHint") }}
              </p>
            </div>

            <div>
              <label
                class="mb-2 block text-sm font-medium text-[var(--foreground)]"
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
                <span class="text-sm text-[var(--muted-foreground)]">
                  {{ t("admin.settings.panelRateLimit.perMinute") }}
                </span>
              </div>
              <p class="mt-1.5 text-xs text-[var(--muted-foreground)]">
                {{ t("admin.settings.panelRateLimit.publicIpRpmHint") }}
              </p>
            </div>
          </div>

          <div
            class="flex items-center justify-between border-t border-[var(--border)] pt-4"
          >
            <div>
              <label class="font-medium text-[var(--foreground)]">{{
                t("admin.settings.panelRateLimit.exemptAdmin")
              }}</label>
              <p class="text-sm text-[var(--muted-foreground)]">
                {{ t("admin.settings.panelRateLimit.exemptAdminHint") }}
              </p>
            </div>
            <Toggle v-model="panelRateLimitForm.exempt_admin" />
          </div>
        </div>

        <div
          class="flex justify-end border-t border-[var(--border)] pt-4"
        >
          <button
            type="button"
            data-testid="panel-rate-limit-save"
            @click="savePanelRateLimitSettings"
            :disabled="panelRateLimitSaving"
            class="btn btn-primary btn-sm"
          >
            <LoadingSpinner v-if="panelRateLimitSaving" size="sm" color="white" />
            {{ panelRateLimitSaving ? t("common.saving") : t("common.save") }}
          </button>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
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
