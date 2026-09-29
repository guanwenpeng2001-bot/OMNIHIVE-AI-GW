import { ref, reactive } from "vue";
import { adminAPI } from "@/api";
import { extractApiErrorMessage } from "@/utils/apiError";
import { useSettingsServices } from "./useSettingsServices";

export function useStreamTimeout() {
  const { t, appStore } = useSettingsServices();
  // Stream Timeout 状态
  const streamTimeoutLoading = ref(true);

  const streamTimeoutSaving = ref(false);

  const streamTimeoutForm = reactive({
    enabled: true,
    action: "temp_unsched" as "temp_unsched" | "error" | "none",
    temp_unsched_minutes: 5,
    threshold_count: 3,
    threshold_window_minutes: 10,
  });

  // Stream Timeout 方法
  async function loadStreamTimeoutSettings() {
    streamTimeoutLoading.value = true;
    try {
      const settings = await adminAPI.settings.getStreamTimeoutSettings();
      Object.assign(streamTimeoutForm, settings);
    } catch (_error: unknown) {
      // Silent fail - settings will use defaults
    } finally {
      streamTimeoutLoading.value = false;
    }
  }

  async function saveStreamTimeoutSettings() {
    streamTimeoutSaving.value = true;
    try {
      const updated = await adminAPI.settings.updateStreamTimeoutSettings({
        enabled: streamTimeoutForm.enabled,
        action: streamTimeoutForm.action,
        temp_unsched_minutes: streamTimeoutForm.temp_unsched_minutes,
        threshold_count: streamTimeoutForm.threshold_count,
        threshold_window_minutes: streamTimeoutForm.threshold_window_minutes,
      });
      Object.assign(streamTimeoutForm, updated);
      appStore.showSuccess(t("admin.settings.streamTimeout.saved"));
    } catch (error: unknown) {
      appStore.showError(
        extractApiErrorMessage(
          error,
          t("admin.settings.streamTimeout.saveFailed"),
        ),
      );
    } finally {
      streamTimeoutSaving.value = false;
    }
  }

  return {
    streamTimeoutLoading,
    streamTimeoutSaving,
    streamTimeoutForm,
    loadStreamTimeoutSettings,
    saveStreamTimeoutSettings,
  };
}
