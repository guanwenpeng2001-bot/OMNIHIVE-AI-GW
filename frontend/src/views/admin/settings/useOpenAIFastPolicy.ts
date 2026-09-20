import { ref, reactive, computed } from "vue";
import type { OpenAIFastPolicyRule } from "@/api/admin/settings";
import { useSettingsServices } from "./useSettingsServices";

export function useOpenAIFastPolicy() {
  const { t } = useSettingsServices();
  // OpenAI Fast/Flex Policy 状态
  const openaiFastPolicyForm = reactive({
    rules: [] as OpenAIFastPolicyRule[],
  });

  // 标记 openai_fast_policy_settings 是否已成功从后端加载，
  // 避免后端 GET 出错或字段缺失时，保存把默认规则覆盖成空数组。
  const openaiFastPolicyLoaded = ref(false);

  // ==================== OpenAI Fast/Flex Policy ====================

  const openaiFastPolicyTierOptions = computed(() => [
    { value: "all", label: t("admin.settings.openaiFastPolicy.tierAll") },
    {
      value: "priority",
      label: t("admin.settings.openaiFastPolicy.tierPriority"),
    },
    {
      value: "ultrafast",
      label: t("admin.settings.openaiFastPolicy.tierUltrafast"),
    },
    { value: "flex", label: t("admin.settings.openaiFastPolicy.tierFlex") },
    {
      value: "missing",
      label: t("admin.settings.openaiFastPolicy.tierMissing"),
    },
  ]);

  const openaiFastPolicyActionOptions = computed(() => [
    { value: "pass", label: t("admin.settings.openaiFastPolicy.actionPass") },
    {
      value: "filter",
      label: t("admin.settings.openaiFastPolicy.actionFilter"),
    },
    {
      value: "force_priority",
      label: t("admin.settings.openaiFastPolicy.actionForcePriority"),
    },
    { value: "block", label: t("admin.settings.openaiFastPolicy.actionBlock") },
  ]);

  function openaiFastPolicyActionSummary(
    action: OpenAIFastPolicyRule["action"],
  ) {
    return t(`admin.settings.openaiFastPolicy.summaryAction.${action}`);
  }

  function hasOpenAIFastPolicyTargetModels(rule: OpenAIFastPolicyRule) {
    return Boolean(
      rule.model_whitelist?.some((pattern) => pattern.trim() !== ""),
    );
  }

  const openaiFastPolicyScopeOptions = computed(() => [
    { value: "all", label: t("admin.settings.openaiFastPolicy.scopeAll") },
    { value: "oauth", label: t("admin.settings.openaiFastPolicy.scopeOAuth") },
    {
      value: "apikey",
      label: t("admin.settings.openaiFastPolicy.scopeAPIKey"),
    },
    {
      value: "bedrock",
      label: t("admin.settings.openaiFastPolicy.scopeBedrock"),
    },
  ]);

  function addOpenAIFastPolicyRule() {
    openaiFastPolicyForm.rules.push({
      service_tier: "priority",
      action: "filter",
      scope: "all",
      user_ids: [],
      error_message: "",
      model_whitelist: [],
      fallback_action: "pass",
      fallback_error_message: "",
    });
  }

  function removeOpenAIFastPolicyRule(index: number) {
    openaiFastPolicyForm.rules.splice(index, 1);
  }

  function addOpenAIFastPolicyModelPattern(rule: OpenAIFastPolicyRule) {
    if (!rule.model_whitelist) rule.model_whitelist = [];
    rule.model_whitelist.push("");
  }

  function removeOpenAIFastPolicyModelPattern(
    rule: OpenAIFastPolicyRule,
    idx: number,
  ) {
    rule.model_whitelist?.splice(idx, 1);
  }

  return {
    openaiFastPolicyForm,
    openaiFastPolicyLoaded,
    openaiFastPolicyTierOptions,
    openaiFastPolicyActionOptions,
    openaiFastPolicyActionSummary,
    hasOpenAIFastPolicyTargetModels,
    openaiFastPolicyScopeOptions,
    addOpenAIFastPolicyRule,
    removeOpenAIFastPolicyRule,
    addOpenAIFastPolicyModelPattern,
    removeOpenAIFastPolicyModelPattern,
  };
}
