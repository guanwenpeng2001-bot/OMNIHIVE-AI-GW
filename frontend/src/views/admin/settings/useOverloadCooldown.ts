import { ref, reactive } from "vue";
import { adminAPI } from "@/api";
import { extractApiErrorMessage } from "@/utils/apiError";
import { useSettingsServices } from "./useSettingsServices";

export function useOverloadCooldown() {
  const { t, appStore } = useSettingsServices();
  // Overload Cooldown (529) 状态
  const overloadCooldownLoading = ref(true);

  const overloadCooldownSaving = ref(false);

  const overloadCooldownForm = reactive({
    enabled: true,
    cooldown_minutes: 10,
  });

  // Overload Cooldown 方法
  async function loadOverloadCooldownSettings() {
    overloadCooldownLoading.value = true;
    try {
      const settings = await adminAPI.settings.getOverloadCooldownSettings();
      Object.assign(overloadCooldownForm, settings);
    } catch (_error: unknown) {
      // Silent fail - settings will use defaults
    } finally {
      overloadCooldownLoading.value = false;
    }
  }

  async function saveOverloadCooldownSettings() {
    overloadCooldownSaving.value = true;
    try {
      const updated = await adminAPI.settings.updateOverloadCooldownSettings({
        enabled: overloadCooldownForm.enabled,
        cooldown_minutes: overloadCooldownForm.cooldown_minutes,
      });
      Object.assign(overloadCooldownForm, updated);
      appStore.showSuccess(t("admin.settings.overloadCooldown.saved"));
    } catch (error: unknown) {
      appStore.showError(
        extractApiErrorMessage(
          error,
          t("admin.settings.overloadCooldown.saveFailed"),
        ),
      );
    } finally {
      overloadCooldownSaving.value = false;
    }
  }

  return {
    overloadCooldownLoading,
    overloadCooldownSaving,
    overloadCooldownForm,
    loadOverloadCooldownSettings,
    saveOverloadCooldownSettings,
  };
}
