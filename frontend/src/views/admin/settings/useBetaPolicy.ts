import { ref, reactive, computed } from "vue";
import { adminAPI } from "@/api";
import { extractApiErrorMessage } from "@/utils/apiError";
import { useSettingsServices } from "./useSettingsServices";

export function useBetaPolicy() {
  const { t, appStore } = useSettingsServices();
  // Beta Policy 状态
  const betaPolicyLoading = ref(true);

  const betaPolicySaving = ref(false);

  const betaPolicyForm = reactive({
    rules: [] as Array<{
      beta_token: string;
      action: "pass" | "filter" | "block";
      scope: "all" | "oauth" | "apikey" | "bedrock";
      error_message?: string;
      model_whitelist?: string[];
      fallback_action?: "pass" | "filter" | "block";
      fallback_error_message?: string;
    }>,
  });

  const betaPolicyActionOptions = computed(() => [
    { value: "pass", label: t("admin.settings.betaPolicy.actionPass") },
    { value: "filter", label: t("admin.settings.betaPolicy.actionFilter") },
    { value: "block", label: t("admin.settings.betaPolicy.actionBlock") },
  ]);

  const betaPolicyScopeOptions = computed(() => [
    { value: "all", label: t("admin.settings.betaPolicy.scopeAll") },
    { value: "oauth", label: t("admin.settings.betaPolicy.scopeOAuth") },
    { value: "apikey", label: t("admin.settings.betaPolicy.scopeAPIKey") },
    { value: "bedrock", label: t("admin.settings.betaPolicy.scopeBedrock") },
  ]);

  // Beta Policy 方法
  const betaDisplayNames: Record<string, string> = {
    "fast-mode-2026-02-01": "Fast Mode",
    "context-1m-2025-08-07": "Context 1M",
  };

  // 快捷预设：按 beta_token 定义预设方案
  const betaPresets: Record<
    string,
    Array<{
      label: string;
      description: string;
      action: "pass" | "filter" | "block";
      model_whitelist: string[];
      fallback_action: "pass" | "filter" | "block";
    }>
  > = {
    "context-1m-2025-08-07": [
      {
        label: t("admin.settings.betaPolicy.presetOpusOnly"),
        description: t("admin.settings.betaPolicy.presetOpusOnlyDesc"),
        action: "pass",
        model_whitelist: ["claude-opus-4-6"],
        fallback_action: "filter",
      },
    ],
  };

  // 常用模型模式（具体 ID + 通配符示例）
  const commonModelPatterns = [
    "claude-opus-4-6",
    "claude-sonnet-4-6",
    "claude-opus-*",
    "claude-sonnet-*",
  ];

  function getBetaDisplayName(token: string): string {
    return betaDisplayNames[token] || token;
  }

  function applyBetaPreset(
    rule: (typeof betaPolicyForm.rules)[number],
    preset: {
      action: "pass" | "filter" | "block";
      model_whitelist: string[];
      fallback_action: "pass" | "filter" | "block";
    },
  ) {
    rule.action = preset.action;
    rule.model_whitelist = [...preset.model_whitelist];
    rule.fallback_action = preset.fallback_action;
  }

  function addQuickPattern(
    rule: (typeof betaPolicyForm.rules)[number],
    pattern: string,
  ) {
    if (!rule.model_whitelist) rule.model_whitelist = [];
    if (!rule.model_whitelist.includes(pattern)) {
      rule.model_whitelist.push(pattern);
    }
  }

  async function loadBetaPolicySettings() {
    betaPolicyLoading.value = true;
    try {
      const settings = await adminAPI.settings.getBetaPolicySettings();
      betaPolicyForm.rules = settings.rules;
    } catch (_error: unknown) {
      // Silent fail - settings will use defaults
    } finally {
      betaPolicyLoading.value = false;
    }
  }

  async function saveBetaPolicySettings() {
    betaPolicySaving.value = true;
    try {
      // Clean up empty patterns before saving
      const cleanedRules = betaPolicyForm.rules.map((rule) => {
        const whitelist = rule.model_whitelist?.filter((p) => p.trim() !== "");
        const hasWhitelist = whitelist && whitelist.length > 0;
        return {
          beta_token: rule.beta_token,
          action: rule.action,
          scope: rule.scope,
          error_message: rule.error_message,
          model_whitelist: hasWhitelist ? whitelist : undefined,
          fallback_action: hasWhitelist
            ? rule.fallback_action || "pass"
            : undefined,
          fallback_error_message:
            hasWhitelist && rule.fallback_action === "block"
              ? rule.fallback_error_message
              : undefined,
        };
      });
      const updated = await adminAPI.settings.updateBetaPolicySettings({
        rules: cleanedRules,
      });
      betaPolicyForm.rules = updated.rules;
      appStore.showSuccess(t("admin.settings.betaPolicy.saved"));
    } catch (error: unknown) {
      appStore.showError(
        extractApiErrorMessage(
          error,
          t("admin.settings.betaPolicy.saveFailed"),
        ),
      );
    } finally {
      betaPolicySaving.value = false;
    }
  }

  return {
    betaPolicyLoading,
    betaPolicySaving,
    betaPolicyForm,
    betaPolicyActionOptions,
    betaPolicyScopeOptions,
    betaDisplayNames,
    betaPresets,
    commonModelPatterns,
    getBetaDisplayName,
    applyBetaPreset,
    addQuickPattern,
    loadBetaPolicySettings,
    saveBetaPolicySettings,
  };
}
