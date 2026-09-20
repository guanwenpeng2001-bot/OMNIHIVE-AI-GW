import { computed, watch } from "vue";
import {
  defaultWeChatConnectScopesForMode,
  deriveWeChatConnectStoredMode,
  resolveWeChatConnectModeCapabilities,
} from "@/api/admin/settings";
import type { WeChatConnectMode } from "@/api/admin/settings";
import { useSettingsServices } from "./useSettingsServices";
import type { SettingsForm } from "./settingsForm";

export function useOAuthSettings(form: SettingsForm) {
  const { t, localText, copyToClipboard } = useSettingsServices();
  const currentOrigin =
    typeof window !== "undefined" ? window.location.origin : "";

  function buildApiCallbackUrl(path: string): string {
    const base = (form.api_base_url || currentOrigin).replace(/\/+$/, "");
    const apiRoot = base.endsWith("/api/v1") ? base : `${base}/api/v1`;
    return `${apiRoot}${path.startsWith("/") ? path : `/${path}`}`;
  }

  // LinuxDo OAuth redirect URL suggestion
  const linuxdoRedirectUrlSuggestion = computed(() => {
    return buildApiCallbackUrl("/auth/oauth/linuxdo/callback");
  });

  async function setAndCopyLinuxdoRedirectUrl() {
    const url = linuxdoRedirectUrlSuggestion.value;
    if (!url) return;

    form.linuxdo_connect_redirect_url = url;
    await copyToClipboard(
      url,
      t("admin.settings.linuxdo.redirectUrlSetAndCopied"),
    );
  }

  type EmailOAuthProvider = "github" | "google";

  const githubOAuthRedirectUrlSuggestion = computed(() => {
    return buildApiCallbackUrl("/auth/oauth/github/callback");
  });

  const googleOAuthRedirectUrlSuggestion = computed(() => {
    return buildApiCallbackUrl("/auth/oauth/google/callback");
  });

  async function setAndCopyEmailOAuthRedirectUrl(provider: EmailOAuthProvider) {
    const url =
      provider === "github"
        ? githubOAuthRedirectUrlSuggestion.value
        : googleOAuthRedirectUrlSuggestion.value;
    if (!url) return;

    if (provider === "github") {
      form.github_oauth_redirect_url = url;
    } else {
      form.google_oauth_redirect_url = url;
    }
    await copyToClipboard(
      url,
      localText("回调地址已写入并复制。", "Callback URL set and copied."),
    );
  }

  const wechatRedirectUrlSuggestion = computed(() => {
    return buildApiCallbackUrl("/auth/oauth/wechat/callback");
  });

  function syncWeChatConnectMode(preferredMode?: WeChatConnectMode) {
    if (form.wechat_connect_mp_enabled && form.wechat_connect_mobile_enabled) {
      if (preferredMode === "mobile") {
        form.wechat_connect_mp_enabled = false;
      } else {
        form.wechat_connect_mobile_enabled = false;
      }
    }

    const capabilities = resolveWeChatConnectModeCapabilities(
      form.wechat_connect_open_enabled,
      form.wechat_connect_mp_enabled,
      form.wechat_connect_mobile_enabled,
      form.wechat_connect_mode,
    );
    form.wechat_connect_open_enabled = capabilities.openEnabled;
    form.wechat_connect_mp_enabled = capabilities.mpEnabled;
    form.wechat_connect_mobile_enabled = capabilities.mobileEnabled;
    form.wechat_connect_mode = deriveWeChatConnectStoredMode(
      capabilities.openEnabled,
      capabilities.mpEnabled,
      capabilities.mobileEnabled,
      form.wechat_connect_mode,
    );
    form.wechat_connect_scopes = defaultWeChatConnectScopesForMode(
      form.wechat_connect_mode,
    );
  }

  function handleWeChatOpenEnabledChange(value: boolean) {
    form.wechat_connect_open_enabled = value;
    syncWeChatConnectMode(value ? "open" : undefined);
  }

  function handleWeChatMPEnabledChange(value: boolean) {
    form.wechat_connect_mp_enabled = value;
    if (value) {
      form.wechat_connect_mobile_enabled = false;
    }
    syncWeChatConnectMode(value ? "mp" : undefined);
  }

  function handleWeChatMobileEnabledChange(value: boolean) {
    form.wechat_connect_mobile_enabled = value;
    if (value) {
      form.wechat_connect_mp_enabled = false;
    }
    syncWeChatConnectMode(value ? "mobile" : undefined);
  }

  async function setAndCopyWeChatRedirectUrl() {
    const url = wechatRedirectUrlSuggestion.value;
    if (!url) return;

    form.wechat_connect_redirect_url = url;
    await copyToClipboard(
      url,
      t("admin.settings.wechatConnect.redirectUrlSetAndCopied"),
    );
  }

  const oidcRedirectUrlSuggestion = computed(() => {
    return buildApiCallbackUrl("/auth/oauth/oidc/callback");
  });

  async function setAndCopyOIDCRedirectUrl() {
    const url = oidcRedirectUrlSuggestion.value;
    if (!url) return;

    form.oidc_connect_redirect_url = url;
    await copyToClipboard(
      url,
      t("admin.settings.oidc.redirectUrlSetAndCopied"),
    );
  }

  // bypass_registration 与身份同步三开关仅在 internal_only 模式下生效。切换 policy 到其它值时，
  // 立即把相关字段重置为 false，避免保存请求里残留旧值。后端 admin handler 与
  // 配置加载层都有 coerce 兜底，这里是 UX 层的同步而非安全防线。
  watch(
    () => form.dingtalk_connect_corp_restriction_policy,
    (policy) => {
      if (policy !== "internal_only") {
        if (form.dingtalk_connect_bypass_registration)
          form.dingtalk_connect_bypass_registration = false;
        if (form.dingtalk_connect_sync_corp_email)
          form.dingtalk_connect_sync_corp_email = false;
        if (form.dingtalk_connect_sync_display_name)
          form.dingtalk_connect_sync_display_name = false;
        if (form.dingtalk_connect_sync_dept)
          form.dingtalk_connect_sync_dept = false;
      }
    },
  );

  return {
    currentOrigin,
    buildApiCallbackUrl,
    linuxdoRedirectUrlSuggestion,
    setAndCopyLinuxdoRedirectUrl,
    githubOAuthRedirectUrlSuggestion,
    googleOAuthRedirectUrlSuggestion,
    setAndCopyEmailOAuthRedirectUrl,
    wechatRedirectUrlSuggestion,
    syncWeChatConnectMode,
    handleWeChatOpenEnabledChange,
    handleWeChatMPEnabledChange,
    handleWeChatMobileEnabledChange,
    setAndCopyWeChatRedirectUrl,
    oidcRedirectUrlSuggestion,
    setAndCopyOIDCRedirectUrl,
  };
}
