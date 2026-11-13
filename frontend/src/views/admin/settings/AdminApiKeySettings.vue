<template>
  <div v-show="activeTab === 'security'" class="space-y-6">
    <!-- Admin API Key Settings -->
    <div class="card border-[var(--border)] bg-[var(--card)] shadow-none">
      <div class="border-b border-[var(--border)] px-4 py-4 sm:px-6">
        <h2 class="text-base font-semibold tracking-tight text-[var(--foreground)]">
          {{ t("admin.settings.adminApiKey.title") }}
        </h2>
        <p class="mt-1 text-sm text-[var(--muted-foreground)]">
          {{ t("admin.settings.adminApiKey.description") }}
        </p>
      </div>
      <div class="space-y-4 p-4 sm:p-6">
        <!-- Security Warning -->
        <div
          class="rounded-lg border border-[var(--border)] bg-[color-mix(in_srgb,var(--warn)_8%,var(--card))] p-4"
        >
          <div class="flex items-start">
            <Icon
              name="exclamationTriangle"
              size="md"
              class="mt-0.5 flex-shrink-0 text-[var(--warn-text)]"
            />
            <p class="ml-3 text-sm text-[var(--warn-text)]">
              {{ t("admin.settings.adminApiKey.securityWarning") }}
            </p>
          </div>
        </div>

        <!-- Loading State -->
        <div
          v-if="adminApiKeyLoading"
          class="flex items-center gap-2 text-[var(--muted-foreground)]"
        >
          <LoadingSpinner size="sm" color="primary" />
          {{ t("common.loading") }}
        </div>

        <!-- No Key Configured -->
        <div
          v-else-if="!adminApiKeyExists"
          class="flex items-center justify-between"
        >
          <span class="text-[var(--muted-foreground)]">
            {{ t("admin.settings.adminApiKey.notConfigured") }}
          </span>
          <button
            type="button"
            @click="createAdminApiKey"
            :disabled="adminApiKeyOperating"
            class="btn btn-primary btn-sm"
          >
            <LoadingSpinner v-if="adminApiKeyOperating" size="sm" color="primary" />
            {{
              adminApiKeyOperating
                ? t("admin.settings.adminApiKey.creating")
                : t("admin.settings.adminApiKey.create")
            }}
          </button>
        </div>

        <!-- Key Exists -->
        <div v-else class="space-y-4">
          <div class="flex items-center justify-between">
            <div>
              <label
                class="mb-1 block text-sm font-medium text-[var(--foreground)]"
              >
                {{ t("admin.settings.adminApiKey.currentKey") }}
              </label>
              <code
                class="rounded bg-[var(--surface-2)] px-2 py-1 font-mono text-sm text-[var(--foreground)]"
              >
                {{ adminApiKeyMasked }}
              </code>
            </div>
            <div class="flex gap-2">
              <button
                type="button"
                @click="regenerateAdminApiKey"
                :disabled="adminApiKeyOperating"
                class="btn btn-secondary btn-sm"
              >
                {{
                  adminApiKeyOperating
                    ? t("admin.settings.adminApiKey.regenerating")
                    : t("admin.settings.adminApiKey.regenerate")
                }}
              </button>
              <button
                type="button"
                @click="deleteAdminApiKey"
                :disabled="adminApiKeyOperating"
                class="btn btn-secondary btn-sm text-[var(--bad-text)] hover:text-[var(--bad-text)]"
              >
                {{ t("admin.settings.adminApiKey.delete") }}
              </button>
            </div>
          </div>

          <!-- Newly Generated Key Display -->
          <div
            v-if="newAdminApiKey"
            class="space-y-3 rounded-lg border border-[var(--border)] bg-[color-mix(in_srgb,var(--ok)_8%,var(--card))] p-4"
          >
            <p class="text-sm font-medium text-[var(--ok-text)]">
              {{ t("admin.settings.adminApiKey.keyWarning") }}
            </p>
            <div class="flex items-center gap-2">
              <code
                class="flex-1 select-all break-all rounded border border-[var(--border)] bg-[var(--card)] px-3 py-2 font-mono text-sm"
              >
                {{ newAdminApiKey }}
              </code>
              <button
                type="button"
                @click="copyNewKey"
                class="btn btn-primary btn-sm flex-shrink-0"
              >
                {{ t("admin.settings.adminApiKey.copyKey") }}
              </button>
            </div>
            <p class="text-xs text-[var(--ok-text)]">
              {{ t("admin.settings.adminApiKey.usage") }}
            </p>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from "@/components/icons/Icon.vue";
import { useSettingsContext } from "./settingsContext";
const {
  t,
  activeTab,
  adminApiKeyLoading,
  adminApiKeyExists,
  adminApiKeyMasked,
  adminApiKeyOperating,
  newAdminApiKey,
  createAdminApiKey,
  regenerateAdminApiKey,
  deleteAdminApiKey,
  copyNewKey,
} = useSettingsContext();
</script>
