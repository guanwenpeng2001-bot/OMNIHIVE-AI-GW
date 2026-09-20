import { adminAPI } from "@/api";
import {
  buildAuthSourceDefaultsState,
  normalizeAccountSchedulingThresholdsMap,
  normalizePlatformQuotasMap,
  defaultWeChatConnectScopesForMode,
  deriveWeChatConnectStoredMode,
  normalizeDefaultSubscriptionSettings,
  resolveWeChatConnectModeCapabilities,
} from "@/api/admin/settings";
import { extractApiErrorMessage } from "@/utils/apiError";
import { normalizeRegistrationEmailSuffixDomains } from "@/utils/registrationEmailPolicy";
import {
  parseFingerprintSignalsToRows,
  defaultFingerprintSignalRows,
} from "../codexFingerprintSignals";
import type { useSettingsEditor } from "./useSettingsEditor";

type SettingsEditor = Pick<
  ReturnType<typeof useSettingsEditor>,
  | "t"
  | "appStore"
  | "defaultLoginAgreementDocuments"
  | "parseClaudeOAuthSystemPromptBlocks"
  | "defaultClaudeOAuthSystemPromptBlocks"
  | "claudeOAuthSystemPromptBlocks"
  | "syncClaudeOAuthSystemPromptBlocksFormField"
  | "openaiFastPolicyForm"
  | "openaiFastPolicyLoaded"
  | "loadWebSearchConfig"
  | "registrationEmailSuffixWhitelistTags"
  | "registrationEmailSuffixWhitelistDraft"
  | "tablePageSizeOptionsInput"
  | "formatTablePageSizeOptions"
  | "form"
  | "syncCaptchaProviderSelection"
  | "authSourceDefaults"
  | "forwardedClientIpHeaderDraft"
  | "normalizeForwardedClientIpHeaders"
  | "smtpPasswordManuallyEdited"
  | "codexBlacklistRows"
  | "codexWhitelistRows"
  | "codexFingerprintRows"
  | "parseCodexEntriesToRows"
  | "loading"
  | "loadFailed"
>;

export function useSettingsLoad(editor: SettingsEditor) {
  const {
    t,
    appStore,
    defaultLoginAgreementDocuments,
    parseClaudeOAuthSystemPromptBlocks,
    defaultClaudeOAuthSystemPromptBlocks,
    claudeOAuthSystemPromptBlocks,
    syncClaudeOAuthSystemPromptBlocksFormField,
    openaiFastPolicyForm,
    openaiFastPolicyLoaded,
    loadWebSearchConfig,
    registrationEmailSuffixWhitelistTags,
    registrationEmailSuffixWhitelistDraft,
    tablePageSizeOptionsInput,
    formatTablePageSizeOptions,
    form,
    syncCaptchaProviderSelection,
    authSourceDefaults,
    forwardedClientIpHeaderDraft,
    normalizeForwardedClientIpHeaders,
    smtpPasswordManuallyEdited,
    codexBlacklistRows,
    codexWhitelistRows,
    codexFingerprintRows,
    parseCodexEntriesToRows,
    loading,
    loadFailed,
  } = editor;

  async function loadSettings() {
    loading.value = true;
    loadFailed.value = false;
    try {
      const settings = await adminAPI.settings.getSettings();
      settings.payment_load_balance_strategy =
        settings.payment_load_balance_strategy || "round-robin";
      // Only assign non-null values from backend (null means unconfigured, keep defaults)
      for (const [key, value] of Object.entries(settings)) {
        if (value !== null && value !== undefined) {
          (form as Record<string, unknown>)[key] = value;
        }
      }
      syncCaptchaProviderSelection();
      if (!form.claude_oauth_system_prompt_blocks?.trim()) {
        form.claude_oauth_system_prompt_blocks =
          defaultClaudeOAuthSystemPromptBlocks;
      }
      claudeOAuthSystemPromptBlocks.value = parseClaudeOAuthSystemPromptBlocks(
        form.claude_oauth_system_prompt_blocks,
        form.claude_oauth_system_prompt,
      );
      syncClaudeOAuthSystemPromptBlocksFormField();
      codexBlacklistRows.value = parseCodexEntriesToRows(
        form.codex_cli_only_blacklist,
      );
      codexWhitelistRows.value = parseCodexEntriesToRows(
        form.codex_cli_only_whitelist,
      );
      codexFingerprintRows.value =
        form.codex_cli_only_engine_fingerprint_signals
          ? parseFingerprintSignalsToRows(
              form.codex_cli_only_engine_fingerprint_signals,
            )
          : defaultFingerprintSignalRows();
      form.login_agreement_mode =
        settings.login_agreement_mode === "checkbox" ? "checkbox" : "modal";
      form.channel_monitor_mode =
        settings.channel_monitor_mode === "v2" ? "v2" : "v1";
      form.channel_monitor_hide_throughput = Boolean(
        settings.channel_monitor_hide_throughput,
      );
      form.channel_monitor_show_quota = Boolean(
        settings.channel_monitor_show_quota,
      );
      form.channel_monitor_hide_user_ranking = Boolean(
        settings.channel_monitor_hide_user_ranking,
      );
      form.login_agreement_updated_at =
        settings.login_agreement_updated_at || "2026-03-31";
      form.login_agreement_documents =
        Array.isArray(settings.login_agreement_documents) &&
        settings.login_agreement_documents.length > 0
          ? settings.login_agreement_documents.map((doc) => ({
              id: doc.id || "",
              title: doc.title || "",
              content_md: doc.content_md || "",
            }))
          : defaultLoginAgreementDocuments();
      Object.assign(authSourceDefaults, buildAuthSourceDefaultsState(settings));
      form.default_platform_quotas = normalizePlatformQuotasMap(
        settings.default_platform_quotas,
      );
      form.account_scheduling_thresholds =
        normalizeAccountSchedulingThresholdsMap(
          settings.account_scheduling_thresholds,
        );
      form.backend_mode_enabled = settings.backend_mode_enabled;
      form.default_subscriptions = normalizeDefaultSubscriptionSettings(
        settings.default_subscriptions,
      );
      registrationEmailSuffixWhitelistTags.value =
        normalizeRegistrationEmailSuffixDomains(
          settings.registration_email_suffix_whitelist,
        );
      form.forwarded_client_ip_headers = normalizeForwardedClientIpHeaders(
        settings.forwarded_client_ip_headers,
      );
      forwardedClientIpHeaderDraft.value = "";
      tablePageSizeOptionsInput.value = formatTablePageSizeOptions(
        Array.isArray(settings.table_page_size_options)
          ? settings.table_page_size_options
          : [10, 20, 50, 100],
      );
      registrationEmailSuffixWhitelistDraft.value = "";
      form.smtp_password = "";
      smtpPasswordManuallyEdited.value = false;
      form.turnstile_secret_key = "";
      form.tencent_captcha_app_secret_key = "";
      form.tencent_captcha_cloud_secret_id = "";
      form.tencent_captcha_cloud_secret_key = "";
      form.aliyun_captcha_access_key_secret = "";
      form.linuxdo_connect_client_secret = "";
      form.dingtalk_connect_client_secret = "";
      form.github_oauth_client_secret = "";
      form.google_oauth_client_secret = "";
      form.wechat_connect_app_secret = "";
      form.wechat_connect_open_app_secret = "";
      form.wechat_connect_mp_app_secret = "";
      form.wechat_connect_mobile_app_secret = "";
      const wechatCapabilities = resolveWeChatConnectModeCapabilities(
        settings.wechat_connect_open_enabled,
        settings.wechat_connect_mp_enabled,
        settings.wechat_connect_mobile_enabled,
        settings.wechat_connect_mode,
      );
      form.wechat_connect_open_enabled = wechatCapabilities.openEnabled;
      form.wechat_connect_mp_enabled = wechatCapabilities.mpEnabled;
      form.wechat_connect_mobile_enabled = wechatCapabilities.mobileEnabled;
      form.wechat_connect_mode = deriveWeChatConnectStoredMode(
        wechatCapabilities.openEnabled,
        wechatCapabilities.mpEnabled,
        wechatCapabilities.mobileEnabled,
        settings.wechat_connect_mode,
      );
      const legacyWeChatAppID = String(
        settings.wechat_connect_app_id || "",
      ).trim();
      const legacyWeChatSecretConfigured = Boolean(
        settings.wechat_connect_app_secret_configured,
      );
      if (!form.wechat_connect_open_app_id && wechatCapabilities.openEnabled) {
        form.wechat_connect_open_app_id = legacyWeChatAppID;
      }
      if (!form.wechat_connect_mp_app_id && wechatCapabilities.mpEnabled) {
        form.wechat_connect_mp_app_id = legacyWeChatAppID;
      }
      if (
        !form.wechat_connect_mobile_app_id &&
        wechatCapabilities.mobileEnabled
      ) {
        form.wechat_connect_mobile_app_id = legacyWeChatAppID;
      }
      if (
        !form.wechat_connect_open_app_secret_configured &&
        wechatCapabilities.openEnabled
      ) {
        form.wechat_connect_open_app_secret_configured =
          legacyWeChatSecretConfigured;
      }
      if (
        !form.wechat_connect_mp_app_secret_configured &&
        wechatCapabilities.mpEnabled
      ) {
        form.wechat_connect_mp_app_secret_configured =
          legacyWeChatSecretConfigured;
      }
      if (
        !form.wechat_connect_mobile_app_secret_configured &&
        wechatCapabilities.mobileEnabled
      ) {
        form.wechat_connect_mobile_app_secret_configured =
          legacyWeChatSecretConfigured;
      }
      form.wechat_connect_scopes = defaultWeChatConnectScopesForMode(
        form.wechat_connect_mode,
      );
      form.oidc_connect_client_secret = "";

      // Load OpenAI fast/flex policy rules from bulk settings.
      // 仅当 payload 真的包含该字段时填充并标记为已加载；否则保持表单空值，
      // 让 saveSettings 在未加载时跳过该字段，防止覆盖后端默认规则。
      if (
        settings.openai_fast_policy_settings &&
        Array.isArray(settings.openai_fast_policy_settings.rules)
      ) {
        openaiFastPolicyForm.rules =
          settings.openai_fast_policy_settings.rules.map((rule) => ({
            ...rule,
            user_ids: rule.user_ids ? [...rule.user_ids] : [],
            model_whitelist: rule.model_whitelist
              ? [...rule.model_whitelist]
              : [],
          }));
        openaiFastPolicyLoaded.value = true;
      }

      // Load web search emulation config separately
      await loadWebSearchConfig();
    } catch (error: unknown) {
      loadFailed.value = true;
      appStore.showError(
        extractApiErrorMessage(error, t("admin.settings.failedToLoad")),
      );
    } finally {
      loading.value = false;
    }
  }
  return { loadSettings };
}
