<template>
  <div class="card border-[var(--border)] bg-[var(--card)] shadow-none">
    <div class="border-b border-[var(--border)] px-4 py-4 sm:px-6">
      <h2 class="text-base font-semibold tracking-tight text-[var(--foreground)]">
        {{ t("admin.settings.openaiFastPolicy.title") }}
      </h2>
      <p class="mt-1 text-sm text-[var(--muted-foreground)]">
        {{ t("admin.settings.openaiFastPolicy.description") }}
      </p>
    </div>
    <div class="space-y-5 p-4 sm:p-6">
      <!-- Empty state -->
      <div
        v-if="openaiFastPolicyForm.rules.length === 0"
        class="rounded-lg border border-dashed border-[var(--border)] p-6 text-center text-sm text-[var(--muted-foreground)]"
      >
        {{ t("admin.settings.openaiFastPolicy.empty") }}
      </div>

      <!-- Rule Cards -->
      <div
        v-for="(rule, ruleIndex) in openaiFastPolicyForm.rules"
        :key="ruleIndex"
        class="rounded-lg border border-[var(--border)] p-4"
      >
        <div class="mb-3 flex items-center justify-between">
          <span class="text-sm font-medium text-[var(--foreground)]">
            {{
              t("admin.settings.openaiFastPolicy.ruleHeader", {
                index: ruleIndex + 1,
              })
            }}
          </span>
          <button
            type="button"
            @click="removeOpenAIFastPolicyRule(ruleIndex)"
            class="rounded p-1 text-[var(--bad-text)] transition-colors hover:bg-[color-mix(in_srgb,var(--bad)_8%,var(--card))] hover:text-[var(--bad-text)]"
            :title="t('admin.settings.openaiFastPolicy.removeRule')"
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

        <div
          class="mb-4 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-[var(--muted-foreground)]"
          :data-testid="`openai-fast-policy-summary-${ruleIndex}`"
        >
          <span class="font-medium text-[var(--foreground)]">
            {{
              t(
                hasOpenAIFastPolicyTargetModels(rule)
                  ? "admin.settings.openaiFastPolicy.summaryTargetModels"
                  : "admin.settings.openaiFastPolicy.summaryAllModels",
              )
            }}
          </span>
          <span aria-hidden="true">→</span>
          <span
            class="inline-flex items-center rounded bg-[var(--surface-2)] px-2 py-0.5 font-medium text-[var(--brand-text)]"
          >
            {{ openaiFastPolicyActionSummary(rule.action) }}
          </span>
          <template v-if="hasOpenAIFastPolicyTargetModels(rule)">
            <span aria-hidden="true">·</span>
            <span class="font-medium text-[var(--foreground)]">
              {{ t("admin.settings.openaiFastPolicy.summaryOtherModels") }}
            </span>
            <span aria-hidden="true">→</span>
            <span
              class="inline-flex items-center rounded bg-[var(--surface-2)] px-2 py-0.5 font-medium text-[var(--foreground)]"
            >
              {{
                openaiFastPolicyActionSummary(rule.fallback_action || "pass")
              }}
            </span>
          </template>
        </div>

        <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
          <!-- Service Tier -->
          <div>
            <label
              class="mb-1 block text-xs font-medium text-[var(--muted-foreground)]"
            >
              {{ t("admin.settings.openaiFastPolicy.serviceTier") }}
            </label>
            <Select
              :modelValue="rule.service_tier"
              @update:modelValue="
                rule.service_tier = $event as
                  | 'all'
                  | 'priority'
                  | 'flex'
                  | 'missing'
              "
              :options="openaiFastPolicyTierOptions"
            />
          </div>

          <!-- Action -->
          <div>
            <label
              class="mb-1 block text-xs font-medium text-[var(--muted-foreground)]"
            >
              {{ t("admin.settings.openaiFastPolicy.action") }}
            </label>
            <Select
              :modelValue="rule.action"
              @update:modelValue="
                rule.action = $event as
                  | 'pass'
                  | 'filter'
                  | 'block'
                  | 'force_priority'
              "
              :options="openaiFastPolicyActionOptions"
            />
          </div>

          <!-- Scope -->
          <div>
            <label
              class="mb-1 block text-xs font-medium text-[var(--muted-foreground)]"
            >
              {{ t("admin.settings.openaiFastPolicy.scope") }}
            </label>
            <Select
              :modelValue="rule.scope"
              @update:modelValue="
                rule.scope = $event as 'all' | 'oauth' | 'apikey' | 'bedrock'
              "
              :options="openaiFastPolicyScopeOptions"
            />
          </div>
        </div>

        <!-- User Scope -->
        <div class="mt-3">
          <label
            class="mb-1 block text-xs font-medium text-[var(--muted-foreground)]"
          >
            {{ t("admin.settings.openaiFastPolicy.userIds") }}
          </label>
          <p class="mb-2 text-xs text-[var(--muted-foreground)]">
            {{ t("admin.settings.openaiFastPolicy.userIdsHint") }}
          </p>
          <OpenAIFastPolicyUserSelector
            :model-value="rule.user_ids || []"
            @update:model-value="rule.user_ids = $event"
          />
        </div>

        <!-- Error Message (only when action=block) -->
        <div v-if="rule.action === 'block'" class="mt-3">
          <label
            class="mb-1 block text-xs font-medium text-[var(--muted-foreground)]"
          >
            {{ t("admin.settings.openaiFastPolicy.errorMessage") }}
          </label>
          <input
            v-model="rule.error_message"
            type="text"
            class="input"
            :placeholder="
              t('admin.settings.openaiFastPolicy.errorMessagePlaceholder')
            "
          />
          <p class="mt-1 text-xs text-[var(--muted-foreground)]">
            {{ t("admin.settings.openaiFastPolicy.errorMessageHint") }}
          </p>
        </div>

        <!-- Target Models -->
        <div
          class="mt-3"
          role="group"
          :aria-labelledby="`openai-fast-policy-models-label-${ruleIndex}`"
          :aria-describedby="`openai-fast-policy-models-hint-${ruleIndex}`"
        >
          <label
            :id="`openai-fast-policy-models-label-${ruleIndex}`"
            class="mb-1 block text-xs font-medium text-[var(--muted-foreground)]"
          >
            {{ t("admin.settings.openaiFastPolicy.modelWhitelist") }}
          </label>
          <p
            :id="`openai-fast-policy-models-hint-${ruleIndex}`"
            class="mb-2 text-xs text-[var(--muted-foreground)]"
          >
            {{ t("admin.settings.openaiFastPolicy.modelWhitelistHint") }}
          </p>
          <div
            v-for="(_, patternIdx) in rule.model_whitelist || []"
            :key="patternIdx"
            class="mb-1.5 flex items-center gap-2"
          >
            <input
              v-model="rule.model_whitelist![patternIdx]"
              type="text"
              class="input input-sm flex-1"
              :placeholder="
                t('admin.settings.openaiFastPolicy.modelPatternPlaceholder')
              "
            />
            <button
              type="button"
              @click="removeOpenAIFastPolicyModelPattern(rule, patternIdx)"
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
          <button
            type="button"
            @click="addOpenAIFastPolicyModelPattern(rule)"
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
            {{ t("admin.settings.openaiFastPolicy.addModelPattern") }}
          </button>
        </div>

        <!-- Other Models Action (only when target models are non-empty) -->
        <div v-if="hasOpenAIFastPolicyTargetModels(rule)" class="mt-3">
          <label
            class="mb-1 block text-xs font-medium text-[var(--muted-foreground)]"
          >
            {{ t("admin.settings.openaiFastPolicy.fallbackAction") }}
          </label>
          <Select
            :modelValue="rule.fallback_action || 'pass'"
            @update:modelValue="
              rule.fallback_action = $event as
                | 'pass'
                | 'filter'
                | 'block'
                | 'force_priority'
            "
            :options="openaiFastPolicyActionOptions"
          />
          <p class="mt-1 text-xs text-[var(--muted-foreground)]">
            {{ t("admin.settings.openaiFastPolicy.fallbackActionHint") }}
          </p>
          <div v-if="rule.fallback_action === 'block'" class="mt-2">
            <input
              v-model="rule.fallback_error_message"
              type="text"
              class="input"
              :placeholder="
                t(
                  'admin.settings.openaiFastPolicy.fallbackErrorMessagePlaceholder',
                )
              "
            />
          </div>
        </div>
      </div>

      <!-- Add Rule Button -->
      <div>
        <button
          type="button"
          @click="addOpenAIFastPolicyRule"
          class="btn btn-secondary btn-sm inline-flex items-center gap-1"
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
              d="M12 4v16m8-8H4"
            />
          </svg>
          {{ t("admin.settings.openaiFastPolicy.addRule") }}
        </button>
        <p class="mt-2 text-xs text-[var(--muted-foreground)]">
          {{ t("admin.settings.openaiFastPolicy.saveHint") }}
        </p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import Select from "@/components/common/Select.vue";
import OpenAIFastPolicyUserSelector from "@/views/admin/settings/OpenAIFastPolicyUserSelector.vue";
import { useSettingsContext } from "./settingsContext";
const {
  t,
  openaiFastPolicyForm,
  openaiFastPolicyTierOptions,
  openaiFastPolicyActionOptions,
  openaiFastPolicyActionSummary,
  hasOpenAIFastPolicyTargetModels,
  openaiFastPolicyScopeOptions,
  addOpenAIFastPolicyRule,
  removeOpenAIFastPolicyRule,
  addOpenAIFastPolicyModelPattern,
  removeOpenAIFastPolicyModelPattern,
} = useSettingsContext();
</script>
