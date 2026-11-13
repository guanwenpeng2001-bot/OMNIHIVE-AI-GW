<template>
  <div class="card border-[var(--border)] bg-[var(--card)] shadow-none">
    <div class="border-b border-[var(--border)] px-4 py-4 sm:px-6">
      <h2 class="text-base font-semibold tracking-tight text-[var(--foreground)]">
        {{ t("admin.settings.rectifier.title") }}
      </h2>
      <p class="mt-1 text-sm text-[var(--muted-foreground)]">
        {{ t("admin.settings.rectifier.description") }}
      </p>
    </div>
    <div class="space-y-5 p-4 sm:p-6">
      <!-- Loading State -->
      <div
        v-if="rectifierLoading"
        class="flex items-center gap-2 text-[var(--muted-foreground)]"
      >
        <LoadingSpinner size="sm" color="primary" />
        {{ t("common.loading") }}
      </div>

      <template v-else>
        <!-- Master Toggle -->
        <div class="flex items-center justify-between">
          <div>
            <label class="font-medium text-[var(--foreground)]">{{
              t("admin.settings.rectifier.enabled")
            }}</label>
            <p class="text-sm text-[var(--muted-foreground)]">
              {{ t("admin.settings.rectifier.enabledHint") }}
            </p>
          </div>
          <Toggle v-model="rectifierForm.enabled" />
        </div>

        <!-- Sub-toggles (only show when master is enabled) -->
        <div
          v-if="rectifierForm.enabled"
          class="space-y-4 border-t border-[var(--border)] pt-4"
        >
          <!-- Thinking Signature Rectifier -->
          <div class="flex items-center justify-between">
            <div>
              <label
                class="text-sm font-medium text-[var(--foreground)]"
                >{{ t("admin.settings.rectifier.thinkingSignature") }}</label
              >
              <p class="text-xs text-[var(--muted-foreground)]">
                {{ t("admin.settings.rectifier.thinkingSignatureHint") }}
              </p>
            </div>
            <Toggle v-model="rectifierForm.thinking_signature_enabled" />
          </div>

          <!-- Thinking Budget Rectifier -->
          <div class="flex items-center justify-between">
            <div>
              <label
                class="text-sm font-medium text-[var(--foreground)]"
                >{{ t("admin.settings.rectifier.thinkingBudget") }}</label
              >
              <p class="text-xs text-[var(--muted-foreground)]">
                {{ t("admin.settings.rectifier.thinkingBudgetHint") }}
              </p>
            </div>
            <Toggle v-model="rectifierForm.thinking_budget_enabled" />
          </div>

          <!-- API Key Signature Rectifier -->
          <div class="flex items-center justify-between">
            <div>
              <label
                class="text-sm font-medium text-[var(--foreground)]"
                >{{ t("admin.settings.rectifier.apikeySignature") }}</label
              >
              <p class="text-xs text-[var(--muted-foreground)]">
                {{ t("admin.settings.rectifier.apikeySignatureHint") }}
              </p>
            </div>
            <Toggle v-model="rectifierForm.apikey_signature_enabled" />
          </div>

          <!-- Custom Patterns (only when apikey_signature_enabled) -->
          <div
            v-if="rectifierForm.apikey_signature_enabled"
            class="ml-4 space-y-3 border-l-2 border-[var(--border)] pl-4"
          >
            <div>
              <label
                class="text-sm font-medium text-[var(--foreground)]"
                >{{ t("admin.settings.rectifier.apikeyPatterns") }}</label
              >
              <p class="text-xs text-[var(--muted-foreground)]">
                {{ t("admin.settings.rectifier.apikeyPatternsHint") }}
              </p>
            </div>
            <div
              v-for="(_, index) in rectifierForm.apikey_signature_patterns"
              :key="index"
              class="flex items-center gap-2"
            >
              <input
                v-model="rectifierForm.apikey_signature_patterns[index]"
                type="text"
                class="input input-sm flex-1"
                :placeholder="
                  t('admin.settings.rectifier.apikeyPatternPlaceholder')
                "
              />
              <button
                type="button"
                @click="
                  rectifierForm.apikey_signature_patterns.splice(index, 1)
                "
                class="btn btn-ghost btn-xs text-[var(--bad-text)] hover:text-[var(--bad-text)]"
              >
                <svg
                  class="h-4 w-4"
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M6 18L18 6M6 6l12 12"
                  />
                </svg>
              </button>
            </div>
            <button
              type="button"
              @click="rectifierForm.apikey_signature_patterns.push('')"
              class="btn btn-ghost btn-xs text-[var(--brand-text)]"
            >
              + {{ t("admin.settings.rectifier.addPattern") }}
            </button>
          </div>
        </div>

        <!-- Save Button -->
        <div
          class="flex justify-end border-t border-[var(--border)] pt-4"
        >
          <button
            type="button"
            @click="saveRectifierSettings"
            :disabled="rectifierSaving"
            class="btn btn-primary btn-sm"
          >
            <LoadingSpinner v-if="rectifierSaving" size="sm" color="white" />
            {{ rectifierSaving ? t("common.saving") : t("common.save") }}
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
  rectifierLoading,
  rectifierSaving,
  rectifierForm,
  saveRectifierSettings,
} = useSettingsContext();
</script>
