import { ref, reactive } from "vue";
import { adminAPI } from "@/api";
import { extractApiErrorMessage } from "@/utils/apiError";
import { useSettingsServices } from "./useSettingsServices";

export function useRectifier() {
  const { t, appStore } = useSettingsServices();
  // Rectifier 状态
  const rectifierLoading = ref(true);

  const rectifierSaving = ref(false);

  const rectifierForm = reactive({
    enabled: true,
    thinking_signature_enabled: true,
    thinking_budget_enabled: true,
    apikey_signature_enabled: false,
    apikey_signature_patterns: [] as string[],
  });

  // Rectifier 方法
  async function loadRectifierSettings() {
    rectifierLoading.value = true;
    try {
      const settings = await adminAPI.settings.getRectifierSettings();
      Object.assign(rectifierForm, settings);
      // 确保 patterns 是数组（旧数据可能为 null）
      if (!Array.isArray(rectifierForm.apikey_signature_patterns)) {
        rectifierForm.apikey_signature_patterns = [];
      }
    } catch (_error: unknown) {
      // Silent fail - settings will use defaults
    } finally {
      rectifierLoading.value = false;
    }
  }

  async function saveRectifierSettings() {
    rectifierSaving.value = true;
    try {
      const updated = await adminAPI.settings.updateRectifierSettings({
        enabled: rectifierForm.enabled,
        thinking_signature_enabled: rectifierForm.thinking_signature_enabled,
        thinking_budget_enabled: rectifierForm.thinking_budget_enabled,
        apikey_signature_enabled: rectifierForm.apikey_signature_enabled,
        apikey_signature_patterns:
          rectifierForm.apikey_signature_patterns.filter(
            (p) => p.trim() !== "",
          ),
      });
      Object.assign(rectifierForm, updated);
      if (!Array.isArray(rectifierForm.apikey_signature_patterns)) {
        rectifierForm.apikey_signature_patterns = [];
      }
      appStore.showSuccess(t("admin.settings.rectifier.saved"));
    } catch (error: unknown) {
      appStore.showError(
        extractApiErrorMessage(error, t("admin.settings.rectifier.saveFailed")),
      );
    } finally {
      rectifierSaving.value = false;
    }
  }

  return {
    rectifierLoading,
    rectifierSaving,
    rectifierForm,
    loadRectifierSettings,
    saveRectifierSettings,
  };
}
