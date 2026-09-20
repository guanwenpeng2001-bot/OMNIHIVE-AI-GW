import { ref, reactive } from "vue";
import { adminAPI } from "@/api";
import { extractApiErrorMessage } from "@/utils/apiError";
import { useSettingsServices } from "./useSettingsServices";

export function useUpstreamBillingProbe() {
  const { t, appStore } = useSettingsServices();
  // Upstream billing probe state
  const upstreamBillingProbeLoading = ref(true);

  const upstreamBillingProbeSaving = ref(false);

  const upstreamBillingProbeForm = reactive({
    enabled: true,
    interval_minutes: 30,
  });

  async function loadUpstreamBillingProbeSettings() {
    upstreamBillingProbeLoading.value = true;
    try {
      Object.assign(
        upstreamBillingProbeForm,
        await adminAPI.accounts.getUpstreamBillingProbeSettings(),
      );
    } catch (_error: unknown) {
      // Keep defaults when this optional setting cannot be loaded.
    } finally {
      upstreamBillingProbeLoading.value = false;
    }
  }

  async function saveUpstreamBillingProbeSettings() {
    upstreamBillingProbeSaving.value = true;
    try {
      const updated =
        await adminAPI.accounts.updateUpstreamBillingProbeSettings({
          ...upstreamBillingProbeForm,
        });
      Object.assign(upstreamBillingProbeForm, updated);
      appStore.showSuccess(t("admin.settings.upstreamBillingProbe.saved"));
    } catch (error: unknown) {
      appStore.showError(
        extractApiErrorMessage(
          error,
          t("admin.settings.upstreamBillingProbe.saveFailed"),
        ),
      );
    } finally {
      upstreamBillingProbeSaving.value = false;
    }
  }

  return {
    upstreamBillingProbeLoading,
    upstreamBillingProbeSaving,
    upstreamBillingProbeForm,
    loadUpstreamBillingProbeSettings,
    saveUpstreamBillingProbeSettings,
  };
}
