import { computed } from "vue";
import { useSettingsServices } from "./useSettingsServices";
import type { SettingsForm } from "./settingsForm";

export function useOpenAIScheduler(form: SettingsForm) {
  const { t } = useSettingsServices();
  type OpenAIAdvancedSchedulerOverrideKey =
    | "openai_advanced_scheduler_lb_top_k"
    | "openai_advanced_scheduler_weight_priority"
    | "openai_advanced_scheduler_weight_load"
    | "openai_advanced_scheduler_weight_queue"
    | "openai_advanced_scheduler_weight_error_rate"
    | "openai_advanced_scheduler_weight_ttft"
    | "openai_advanced_scheduler_weight_reset"
    | "openai_advanced_scheduler_weight_quota_headroom"
    | "openai_advanced_scheduler_weight_upstream_cost"
    | "openai_advanced_scheduler_weight_previous_response"
    | "openai_advanced_scheduler_weight_session_sticky";

  type OpenAIAdvancedSchedulerEffectiveKey =
    | "openai_advanced_scheduler_effective_lb_top_k"
    | "openai_advanced_scheduler_effective_weight_priority"
    | "openai_advanced_scheduler_effective_weight_load"
    | "openai_advanced_scheduler_effective_weight_queue"
    | "openai_advanced_scheduler_effective_weight_error_rate"
    | "openai_advanced_scheduler_effective_weight_ttft"
    | "openai_advanced_scheduler_effective_weight_reset"
    | "openai_advanced_scheduler_effective_weight_quota_headroom"
    | "openai_advanced_scheduler_effective_weight_upstream_cost"
    | "openai_advanced_scheduler_effective_weight_previous_response"
    | "openai_advanced_scheduler_effective_weight_session_sticky";

  const openAIAdvancedSchedulerWeightFields = computed<
    Array<{
      key: OpenAIAdvancedSchedulerOverrideKey;
      label: string;
      placeholder: string;
    }>
  >(() => {
    const placeholder = (
      effectiveKey: OpenAIAdvancedSchedulerEffectiveKey,
      fallbackValue: string,
    ) => {
      const effectiveValue = String(
        (form as Record<string, unknown>)[effectiveKey] ?? "",
      ).trim();
      return t(
        "admin.settings.openaiExperimentalScheduler.defaultPlaceholder",
        {
          value: effectiveValue || fallbackValue,
        },
      );
    };

    return [
      {
        key: "openai_advanced_scheduler_lb_top_k",
        label: t("admin.settings.openaiExperimentalScheduler.topKLabel"),
        placeholder: placeholder(
          "openai_advanced_scheduler_effective_lb_top_k",
          "7",
        ),
      },
      {
        key: "openai_advanced_scheduler_weight_priority",
        label: t("admin.settings.openaiExperimentalScheduler.priorityWeight"),
        placeholder: placeholder(
          "openai_advanced_scheduler_effective_weight_priority",
          "1",
        ),
      },
      {
        key: "openai_advanced_scheduler_weight_load",
        label: t("admin.settings.openaiExperimentalScheduler.loadWeight"),
        placeholder: placeholder(
          "openai_advanced_scheduler_effective_weight_load",
          "1",
        ),
      },
      {
        key: "openai_advanced_scheduler_weight_queue",
        label: t("admin.settings.openaiExperimentalScheduler.queueWeight"),
        placeholder: placeholder(
          "openai_advanced_scheduler_effective_weight_queue",
          "0.7",
        ),
      },
      {
        key: "openai_advanced_scheduler_weight_error_rate",
        label: t("admin.settings.openaiExperimentalScheduler.errorRateWeight"),
        placeholder: placeholder(
          "openai_advanced_scheduler_effective_weight_error_rate",
          "0.8",
        ),
      },
      {
        key: "openai_advanced_scheduler_weight_ttft",
        label: t("admin.settings.openaiExperimentalScheduler.ttftWeight"),
        placeholder: placeholder(
          "openai_advanced_scheduler_effective_weight_ttft",
          "0.5",
        ),
      },
      {
        key: "openai_advanced_scheduler_weight_reset",
        label: t("admin.settings.openaiExperimentalScheduler.resetWeight"),
        placeholder: placeholder(
          "openai_advanced_scheduler_effective_weight_reset",
          "0",
        ),
      },
      {
        key: "openai_advanced_scheduler_weight_quota_headroom",
        label: t(
          "admin.settings.openaiExperimentalScheduler.quotaHeadroomWeight",
        ),
        placeholder: placeholder(
          "openai_advanced_scheduler_effective_weight_quota_headroom",
          "0",
        ),
      },
      {
        key: "openai_advanced_scheduler_weight_upstream_cost",
        label: t(
          "admin.settings.openaiExperimentalScheduler.upstreamCostWeight",
        ),
        placeholder: placeholder(
          "openai_advanced_scheduler_effective_weight_upstream_cost",
          "0",
        ),
      },
      {
        key: "openai_advanced_scheduler_weight_previous_response",
        label: t(
          "admin.settings.openaiExperimentalScheduler.previousResponseWeight",
        ),
        placeholder: placeholder(
          "openai_advanced_scheduler_effective_weight_previous_response",
          "5",
        ),
      },
      {
        key: "openai_advanced_scheduler_weight_session_sticky",
        label: t(
          "admin.settings.openaiExperimentalScheduler.sessionStickyWeight",
        ),
        placeholder: placeholder(
          "openai_advanced_scheduler_effective_weight_session_sticky",
          "3",
        ),
      },
    ];
  });

  return { openAIAdvancedSchedulerWeightFields };
}
