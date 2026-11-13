<template>
  <div class="card border-[var(--border)] bg-[var(--card)] shadow-none">
    <div class="border-b border-[var(--border)] px-4 py-4 sm:px-6">
      <h2 class="text-base font-semibold tracking-tight text-[var(--foreground)]">
        {{ t("admin.settings.betaPolicy.title") }}
      </h2>
      <p class="mt-1 text-sm text-[var(--muted-foreground)]">
        {{ t("admin.settings.betaPolicy.description") }}
      </p>
    </div>
    <div class="space-y-5 p-4 sm:p-6">
      <!-- Loading State -->
      <div
        v-if="betaPolicyLoading"
        class="flex items-center gap-2 text-[var(--muted-foreground)]"
      >
        <LoadingSpinner size="sm" color="primary" />
        {{ t("common.loading") }}
      </div>

      <template v-else>
        <!-- Rule Cards -->
        <div
          v-for="rule in betaPolicyForm.rules"
          :key="rule.beta_token"
          class="rounded-lg border border-[var(--border)] p-4"
        >
          <div class="mb-3 flex items-center gap-2">
            <span class="text-sm font-medium text-[var(--foreground)]">
              {{ getBetaDisplayName(rule.beta_token) }}
            </span>
            <span
              class="rounded bg-[var(--surface-2)] px-2 py-0.5 text-xs text-[var(--muted-foreground)]"
            >
              {{ rule.beta_token }}
            </span>
          </div>

          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <!-- Action -->
            <div>
              <label
                class="mb-1 block text-xs font-medium text-[var(--muted-foreground)]"
              >
                {{ t("admin.settings.betaPolicy.action") }}
              </label>
              <Select
                :modelValue="rule.action"
                @update:modelValue="rule.action = $event as typeof rule.action"
                :options="betaPolicyActionOptions"
              />
            </div>

            <!-- Scope -->
            <div>
              <label
                class="mb-1 block text-xs font-medium text-[var(--muted-foreground)]"
              >
                {{ t("admin.settings.betaPolicy.scope") }}
              </label>
              <Select
                :modelValue="rule.scope"
                @update:modelValue="rule.scope = $event as typeof rule.scope"
                :options="betaPolicyScopeOptions"
              />
            </div>
          </div>

          <!-- Error Message (only when action=block) -->
          <div v-if="rule.action === 'block'" class="mt-3">
            <label
              class="mb-1 block text-xs font-medium text-[var(--muted-foreground)]"
            >
              {{ t("admin.settings.betaPolicy.errorMessage") }}
            </label>
            <input
              v-model="rule.error_message"
              type="text"
              class="input"
              :placeholder="
                t('admin.settings.betaPolicy.errorMessagePlaceholder')
              "
            />
            <p class="mt-1 text-xs text-[var(--muted-foreground)]">
              {{ t("admin.settings.betaPolicy.errorMessageHint") }}
            </p>
          </div>

          <!-- Quick Presets (only for tokens with presets) -->
          <div v-if="betaPresets[rule.beta_token]?.length" class="mt-3">
            <label
              class="mb-1 block text-xs font-medium text-[var(--muted-foreground)]"
            >
              {{ t("admin.settings.betaPolicy.quickPresets") }}
            </label>
            <div class="flex flex-wrap gap-2">
              <button
                v-for="preset in betaPresets[rule.beta_token]"
                :key="preset.label"
                type="button"
                class="inline-flex items-center gap-1 rounded-md border border-[var(--border)] bg-[var(--surface-2)] px-2.5 py-1 text-xs font-medium text-[var(--brand-text)] transition-colors hover:bg-[var(--surface-2)]"
                @click="applyBetaPreset(rule, preset)"
                :title="preset.description"
              >
                {{ preset.label }}
              </button>
            </div>
          </div>

          <!-- Model Whitelist -->
          <div class="mt-3">
            <label
              class="mb-1 block text-xs font-medium text-[var(--muted-foreground)]"
            >
              {{ t("admin.settings.betaPolicy.modelWhitelist") }}
            </label>
            <p class="mb-2 text-xs text-[var(--muted-foreground)]">
              {{ t("admin.settings.betaPolicy.modelWhitelistHint") }}
            </p>
            <!-- Existing patterns -->
            <div
              v-for="(_, index) in rule.model_whitelist || []"
              :key="index"
              class="mb-1.5 flex items-center gap-2"
            >
              <input
                v-model="rule.model_whitelist![index]"
                type="text"
                class="input input-sm flex-1"
                :placeholder="
                  t('admin.settings.betaPolicy.modelPatternPlaceholder')
                "
              />
              <button
                type="button"
                @click="rule.model_whitelist!.splice(index, 1)"
                class="shrink-0 rounded p-1 text-[var(--bad-text)] transition-colors hover:bg-[color-mix(in_srgb,var(--bad)_8%,var(--card))] hover:text-[var(--bad-text)]"
              >
                <svg
                  class="h-4 w-4"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                  stroke-width="2"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M6 18L18 6M6 6l12 12"
                  />
                </svg>
              </button>
            </div>
            <!-- Add pattern button -->
            <button
              type="button"
              @click="
                if (!rule.model_whitelist) rule.model_whitelist = [];
                rule.model_whitelist.push('');
              "
              class="mb-2 inline-flex items-center gap-1 text-xs text-[var(--brand-text)] transition-colors hover:text-[var(--brand-text)]"
            >
              <svg
                class="h-3.5 w-3.5"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                stroke-width="2"
              >
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  d="M12 4v16m8-8H4"
                />
              </svg>
              {{ t("admin.settings.betaPolicy.addModelPattern") }}
            </button>
            <!-- Common pattern chips -->
            <div class="flex flex-wrap items-center gap-1.5">
              <span class="text-xs text-[var(--muted-foreground)]"
                >{{ t("admin.settings.betaPolicy.commonPatterns") }}:</span
              >
              <button
                v-for="pattern in commonModelPatterns"
                :key="pattern"
                type="button"
                class="rounded border border-[var(--border)] px-2 py-0.5 text-xs text-[var(--muted-foreground)] transition-colors hover:border-[var(--border)] hover:bg-[var(--surface-2)] hover:text-[var(--brand-text)]"
                @click="addQuickPattern(rule, pattern)"
              >
                {{ pattern }}
              </button>
            </div>
          </div>

          <!-- Fallback Action (only when model_whitelist is non-empty) -->
          <div
            v-if="rule.model_whitelist && rule.model_whitelist.length > 0"
            class="mt-3"
          >
            <label
              class="mb-1 block text-xs font-medium text-[var(--muted-foreground)]"
            >
              {{ t("admin.settings.betaPolicy.fallbackAction") }}
            </label>
            <Select
              :modelValue="rule.fallback_action || 'pass'"
              @update:modelValue="
                rule.fallback_action = $event as typeof rule.fallback_action
              "
              :options="betaPolicyActionOptions"
            />
            <p class="mt-1 text-xs text-[var(--muted-foreground)]">
              {{ t("admin.settings.betaPolicy.fallbackActionHint") }}
            </p>
            <!-- Fallback Error Message (only when fallback_action=block) -->
            <div v-if="rule.fallback_action === 'block'" class="mt-2">
              <input
                v-model="rule.fallback_error_message"
                type="text"
                class="input"
                :placeholder="
                  t('admin.settings.betaPolicy.fallbackErrorMessagePlaceholder')
                "
              />
              <p class="mt-1 text-xs text-[var(--muted-foreground)]">
                {{ t("admin.settings.betaPolicy.errorMessageHint") }}
              </p>
            </div>
          </div>
        </div>

        <!-- Save Button -->
        <div
          class="flex justify-end border-t border-[var(--border)] pt-4"
        >
          <button
            type="button"
            @click="saveBetaPolicySettings"
            :disabled="betaPolicySaving"
            class="btn btn-primary btn-sm"
          >
            <LoadingSpinner v-if="betaPolicySaving" size="sm" color="white" />
            {{ betaPolicySaving ? t("common.saving") : t("common.save") }}
          </button>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Select from "@/components/common/Select.vue";
import { useSettingsContext } from "./settingsContext";
const {
  t,
  betaPolicyLoading,
  betaPolicySaving,
  betaPolicyForm,
  betaPolicyActionOptions,
  betaPolicyScopeOptions,
  betaPresets,
  commonModelPatterns,
  getBetaDisplayName,
  applyBetaPreset,
  addQuickPattern,
  saveBetaPolicySettings,
} = useSettingsContext();
</script>
