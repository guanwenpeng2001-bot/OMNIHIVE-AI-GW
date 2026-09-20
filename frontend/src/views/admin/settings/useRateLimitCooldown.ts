import { ref, reactive } from "vue";
import { adminAPI } from "@/api";
import { extractApiErrorMessage } from "@/utils/apiError";
import { useSettingsServices } from "./useSettingsServices";

export function useRateLimitCooldown() {
  const { t, appStore } = useSettingsServices();
  // Rate Limit Cooldown (429) 状态
  const rateLimit429CooldownLoading = ref(true);

  const rateLimit429CooldownSaving = ref(false);

  const rateLimit429CooldownForm = reactive({
    enabled: true,
    cooldown_seconds: 5,
  });

  // Rate Limit Cooldown (429) 方法
  async function loadRateLimit429CooldownSettings() {
    rateLimit429CooldownLoading.value = true;
    try {
      const settings =
        await adminAPI.settings.getRateLimit429CooldownSettings();
      Object.assign(rateLimit429CooldownForm, settings);
    } catch (_error: unknown) {
      // Silent fail - settings will use defaults
    } finally {
      rateLimit429CooldownLoading.value = false;
    }
  }

  async function saveRateLimit429CooldownSettings() {
    rateLimit429CooldownSaving.value = true;
    try {
      const updated =
        await adminAPI.settings.updateRateLimit429CooldownSettings({
          enabled: rateLimit429CooldownForm.enabled,
          cooldown_seconds: rateLimit429CooldownForm.cooldown_seconds,
        });
      Object.assign(rateLimit429CooldownForm, updated);
      appStore.showSuccess(t("admin.settings.rateLimit429Cooldown.saved"));
    } catch (error: unknown) {
      appStore.showError(
        extractApiErrorMessage(
          error,
          t("admin.settings.rateLimit429Cooldown.saveFailed"),
        ),
      );
    } finally {
      rateLimit429CooldownSaving.value = false;
    }
  }

  return {
    rateLimit429CooldownLoading,
    rateLimit429CooldownSaving,
    rateLimit429CooldownForm,
    loadRateLimit429CooldownSettings,
    saveRateLimit429CooldownSettings,
  };
}
