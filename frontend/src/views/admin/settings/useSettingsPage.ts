import { onMounted } from "vue";
import { useSettingsEditor } from "./useSettingsEditor";
import { useSettingsLoad } from "./useSettingsLoad";
import { useSettingsSave } from "./useSettingsSave";

/** Preserve the original load order and one bulk-save path across all tabs. */
export function useSettingsPage() {
  const editor = useSettingsEditor(saveSettings);
  const { loadSettings } = useSettingsLoad(editor);
  const settingsSave = useSettingsSave(editor);
  const {
    loadSubscriptionGroups,
    loadAdminApiKey,
    loadUpstreamBillingProbeSettings,
    loadOllamaCloudUsageSettings,
    loadOverloadCooldownSettings,
    loadRateLimit429CooldownSettings,
    loadPanelRateLimitSettings,
    loadStreamTimeoutSettings,
    loadRectifierSettings,
    loadBetaPolicySettings,
    loadProviders,
  } = editor;

  async function saveSettings() {
    await settingsSave.saveSettings();
  }

  onMounted(() => {
    loadSettings();
    loadSubscriptionGroups();
    loadAdminApiKey();
    loadUpstreamBillingProbeSettings();
    loadOllamaCloudUsageSettings();
    loadOverloadCooldownSettings();
    loadRateLimit429CooldownSettings();
    loadPanelRateLimitSettings();
    loadStreamTimeoutSettings();
    loadRectifierSettings();
    loadBetaPolicySettings();
    loadProviders();
  });
  return editor;
}
