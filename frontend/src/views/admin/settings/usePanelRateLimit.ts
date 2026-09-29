import { ref, reactive } from "vue";
import { adminAPI } from "@/api";
import { extractApiErrorMessage } from "@/utils/apiError";
import { useSettingsServices } from "./useSettingsServices";

export function usePanelRateLimit() {
  const { t, appStore } = useSettingsServices();
  // Panel API Rate Limit 状态
  const panelRateLimitLoading = ref(true);

  const panelRateLimitSaving = ref(false);

  const panelRateLimitForm = reactive({
    enabled: true,
    user_rpm: 240,
    heavy_rpm: 60,
    exempt_admin: true,
    public_ip_rpm: 300,
  });

  // Panel API Rate Limit 方法
  async function loadPanelRateLimitSettings() {
    panelRateLimitLoading.value = true;
    try {
      const settings = await adminAPI.settings.getPanelRateLimitSettings();
      Object.assign(panelRateLimitForm, settings);
    } catch (_error: unknown) {
      // Silent fail - settings will use defaults
    } finally {
      panelRateLimitLoading.value = false;
    }
  }

  async function savePanelRateLimitSettings() {
    panelRateLimitSaving.value = true;
    try {
      const updated = await adminAPI.settings.updatePanelRateLimitSettings({
        enabled: panelRateLimitForm.enabled,
        user_rpm: panelRateLimitForm.user_rpm,
        heavy_rpm: panelRateLimitForm.heavy_rpm,
        exempt_admin: panelRateLimitForm.exempt_admin,
        public_ip_rpm: panelRateLimitForm.public_ip_rpm,
      });
      Object.assign(panelRateLimitForm, updated);
      appStore.showSuccess(t("admin.settings.panelRateLimit.saved"));
    } catch (error: unknown) {
      appStore.showError(
        extractApiErrorMessage(
          error,
          t("admin.settings.panelRateLimit.saveFailed"),
        ),
      );
    } finally {
      panelRateLimitSaving.value = false;
    }
  }

  return {
    panelRateLimitLoading,
    panelRateLimitSaving,
    panelRateLimitForm,
    loadPanelRateLimitSettings,
    savePanelRateLimitSettings,
  };
}
