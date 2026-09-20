import { ref } from "vue";
import { adminAPI } from "@/api";
import { extractApiErrorMessage } from "@/utils/apiError";
import { useSettingsServices } from "./useSettingsServices";

export function useAdminApiKey() {
  const { t, appStore } = useSettingsServices();
  // Admin API Key 状态
  const adminApiKeyLoading = ref(true);

  const adminApiKeyExists = ref(false);

  const adminApiKeyMasked = ref("");

  const adminApiKeyOperating = ref(false);

  const newAdminApiKey = ref("");

  // Admin API Key 方法
  async function loadAdminApiKey() {
    adminApiKeyLoading.value = true;
    try {
      const status = await adminAPI.settings.getAdminApiKey();
      adminApiKeyExists.value = status.exists;
      adminApiKeyMasked.value = status.masked_key;
    } catch (_error: unknown) {
      // Silent fail - admin API key status is non-critical
    } finally {
      adminApiKeyLoading.value = false;
    }
  }

  async function createAdminApiKey() {
    adminApiKeyOperating.value = true;
    try {
      const result = await adminAPI.settings.regenerateAdminApiKey();
      newAdminApiKey.value = result.key;
      adminApiKeyExists.value = true;
      adminApiKeyMasked.value =
        result.key.substring(0, 10) + "..." + result.key.slice(-4);
      appStore.showSuccess(t("admin.settings.adminApiKey.keyGenerated"));
    } catch (error: unknown) {
      appStore.showError(extractApiErrorMessage(error, t("common.error")));
    } finally {
      adminApiKeyOperating.value = false;
    }
  }

  async function regenerateAdminApiKey() {
    if (!confirm(t("admin.settings.adminApiKey.regenerateConfirm"))) return;
    await createAdminApiKey();
  }

  async function deleteAdminApiKey() {
    if (!confirm(t("admin.settings.adminApiKey.deleteConfirm"))) return;
    adminApiKeyOperating.value = true;
    try {
      await adminAPI.settings.deleteAdminApiKey();
      adminApiKeyExists.value = false;
      adminApiKeyMasked.value = "";
      newAdminApiKey.value = "";
      appStore.showSuccess(t("admin.settings.adminApiKey.keyDeleted"));
    } catch (error: unknown) {
      appStore.showError(extractApiErrorMessage(error, t("common.error")));
    } finally {
      adminApiKeyOperating.value = false;
    }
  }

  function copyNewKey() {
    navigator.clipboard
      .writeText(newAdminApiKey.value)
      .then(() => {
        appStore.showSuccess(t("admin.settings.adminApiKey.keyCopied"));
      })
      .catch(() => {
        appStore.showError(t("common.copyFailed"));
      });
  }

  return {
    adminApiKeyLoading,
    adminApiKeyExists,
    adminApiKeyMasked,
    adminApiKeyOperating,
    newAdminApiKey,
    loadAdminApiKey,
    createAdminApiKey,
    regenerateAdminApiKey,
    deleteAdminApiKey,
    copyNewKey,
  };
}
