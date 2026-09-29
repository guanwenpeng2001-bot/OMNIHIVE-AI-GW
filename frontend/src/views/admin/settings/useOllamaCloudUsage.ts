import { ref, reactive } from "vue";
import { adminAPI } from "@/api";
import { extractApiErrorMessage } from "@/utils/apiError";
import { useSettingsServices } from "./useSettingsServices";

export function useOllamaCloudUsage() {
  const { t, appStore } = useSettingsServices();
  const ollamaCloudUsageLoading = ref(true);

  const ollamaCloudUsageSaving = ref(false);

  const ollamaCloudUsageForm = reactive({
    enabled: false,
    interval_minutes: 60,
    debounce_minutes: 1,
  });

  async function loadOllamaCloudUsageSettings() {
    ollamaCloudUsageLoading.value = true;
    try {
      Object.assign(
        ollamaCloudUsageForm,
        await adminAPI.accounts.getOllamaCloudUsageSettings(),
      );
    } catch (_error: unknown) {
      // Keep the fail-safe disabled defaults when this optional setting cannot be loaded.
    } finally {
      ollamaCloudUsageLoading.value = false;
    }
  }

  async function saveOllamaCloudUsageSettings() {
    ollamaCloudUsageSaving.value = true;
    try {
      const updated = await adminAPI.accounts.updateOllamaCloudUsageSettings({
        ...ollamaCloudUsageForm,
      });
      Object.assign(ollamaCloudUsageForm, updated);
      appStore.showSuccess(t("admin.settings.ollamaCloudUsage.saved"));
    } catch (error: unknown) {
      appStore.showError(
        extractApiErrorMessage(
          error,
          t("admin.settings.ollamaCloudUsage.saveFailed"),
        ),
      );
    } finally {
      ollamaCloudUsageSaving.value = false;
    }
  }

  return {
    ollamaCloudUsageLoading,
    ollamaCloudUsageSaving,
    ollamaCloudUsageForm,
    loadOllamaCloudUsageSettings,
    saveOllamaCloudUsageSettings,
  };
}
