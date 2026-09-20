import { reactive } from "vue";
import type { SettingsForm } from "./settingsForm";

export function useSecuritySettingsForm(
  localText: (zh: string, en: string) => string,
) {
  return reactive<
    Pick<
      SettingsForm,
      | "registration_enabled"
      | "email_verify_enabled"
      | "registration_email_domain_quota_enabled"
      | "promo_code_enabled"
      | "invitation_code_enabled"
      | "password_reset_enabled"
      | "totp_enabled"
      | "totp_encryption_key_configured"
      | "passkey_enabled"
      | "passkey_configured"
      | "passkey_rp_id"
      | "passkey_rp_origins"
      | "session_binding_enabled"
      | "step_up_enabled"
      | "audit_log_retention_days"
      | "frontend_url"
      | "turnstile_enabled"
      | "turnstile_site_key"
      | "turnstile_secret_key"
      | "turnstile_secret_key_configured"
      | "tencent_captcha_enabled"
      | "tencent_captcha_app_id"
      | "tencent_captcha_app_secret_key"
      | "tencent_captcha_app_secret_key_configured"
      | "tencent_captcha_cloud_secret_id"
      | "tencent_captcha_cloud_secret_id_configured"
      | "tencent_captcha_cloud_secret_key"
      | "tencent_captcha_cloud_secret_key_configured"
      | "tencent_captcha_region"
      | "aliyun_captcha_enabled"
      | "aliyun_captcha_access_key_id"
      | "aliyun_captcha_access_key_secret"
      | "aliyun_captcha_access_key_secret_configured"
      | "aliyun_captcha_scene_id"
      | "aliyun_captcha_prefix"
      | "aliyun_captcha_region"
      | "api_key_acl_trust_forwarded_ip"
      | "forwarded_client_ip_headers"
      | "linuxdo_connect_enabled"
      | "linuxdo_connect_client_id"
      | "linuxdo_connect_client_secret"
      | "linuxdo_connect_client_secret_configured"
      | "linuxdo_connect_redirect_url"
      | "dingtalk_connect_enabled"
      | "dingtalk_connect_client_id"
      | "dingtalk_connect_client_secret"
      | "dingtalk_connect_client_secret_configured"
      | "dingtalk_connect_redirect_url"
      | "dingtalk_connect_corp_restriction_policy"
      | "dingtalk_connect_internal_corp_id"
      | "dingtalk_connect_bypass_registration"
      | "dingtalk_connect_sync_corp_email"
      | "dingtalk_connect_sync_display_name"
      | "dingtalk_connect_sync_dept"
      | "dingtalk_connect_sync_corp_email_attr_key"
      | "dingtalk_connect_sync_display_name_attr_key"
      | "dingtalk_connect_sync_dept_attr_key"
      | "dingtalk_connect_sync_corp_email_attr_name"
      | "dingtalk_connect_sync_display_name_attr_name"
      | "dingtalk_connect_sync_dept_attr_name"
      | "wechat_connect_enabled"
      | "wechat_connect_app_id"
      | "wechat_connect_app_secret"
      | "wechat_connect_app_secret_configured"
      | "wechat_connect_open_app_id"
      | "wechat_connect_open_app_secret"
      | "wechat_connect_open_app_secret_configured"
      | "wechat_connect_mp_app_id"
      | "wechat_connect_mp_app_secret"
      | "wechat_connect_mp_app_secret_configured"
      | "wechat_connect_mobile_app_id"
      | "wechat_connect_mobile_app_secret"
      | "wechat_connect_mobile_app_secret_configured"
      | "wechat_connect_open_enabled"
      | "wechat_connect_mp_enabled"
      | "wechat_connect_mobile_enabled"
      | "wechat_connect_mode"
      | "wechat_connect_scopes"
      | "wechat_connect_redirect_url"
      | "wechat_connect_frontend_redirect_url"
      | "oidc_connect_enabled"
      | "oidc_connect_provider_name"
      | "oidc_connect_client_id"
      | "oidc_connect_client_secret"
      | "oidc_connect_client_secret_configured"
      | "oidc_connect_issuer_url"
      | "oidc_connect_discovery_url"
      | "oidc_connect_authorize_url"
      | "oidc_connect_token_url"
      | "oidc_connect_userinfo_url"
      | "oidc_connect_jwks_url"
      | "oidc_connect_scopes"
      | "oidc_connect_redirect_url"
      | "oidc_connect_frontend_redirect_url"
      | "oidc_connect_token_auth_method"
      | "oidc_connect_use_pkce"
      | "oidc_connect_validate_id_token"
      | "oidc_connect_allowed_signing_algs"
      | "oidc_connect_clock_skew_seconds"
      | "oidc_connect_require_email_verified"
      | "oidc_connect_userinfo_email_path"
      | "oidc_connect_userinfo_id_path"
      | "oidc_connect_userinfo_username_path"
      | "github_oauth_enabled"
      | "github_oauth_client_id"
      | "github_oauth_client_secret"
      | "github_oauth_client_secret_configured"
      | "github_oauth_redirect_url"
      | "github_oauth_frontend_redirect_url"
      | "google_oauth_enabled"
      | "google_oauth_client_id"
      | "google_oauth_client_secret"
      | "google_oauth_client_secret_configured"
      | "google_oauth_redirect_url"
      | "google_oauth_frontend_redirect_url"
    >
  >({
    registration_enabled: true,

    email_verify_enabled: false,

    registration_email_domain_quota_enabled: false,

    promo_code_enabled: true,

    invitation_code_enabled: false,

    password_reset_enabled: false,

    totp_enabled: false,

    totp_encryption_key_configured: false,

    passkey_enabled: false,

    passkey_configured: false,

    passkey_rp_id: "",

    passkey_rp_origins: [],

    session_binding_enabled: false,

    step_up_enabled: false,

    audit_log_retention_days: 180,

    frontend_url: "",

    // Cloudflare Turnstile
    turnstile_enabled: false,

    turnstile_site_key: "",

    turnstile_secret_key: "",

    turnstile_secret_key_configured: false,

    tencent_captcha_enabled: false,

    tencent_captcha_app_id: "",

    tencent_captcha_app_secret_key: "",

    tencent_captcha_app_secret_key_configured: false,

    tencent_captcha_cloud_secret_id: "",

    tencent_captcha_cloud_secret_id_configured: false,

    tencent_captcha_cloud_secret_key: "",

    tencent_captcha_cloud_secret_key_configured: false,

    tencent_captcha_region: "cn",

    aliyun_captcha_enabled: false,

    aliyun_captcha_access_key_id: "",

    aliyun_captcha_access_key_secret: "",

    aliyun_captcha_access_key_secret_configured: false,

    aliyun_captcha_scene_id: "",

    aliyun_captcha_prefix: "",

    aliyun_captcha_region: "cn",

    api_key_acl_trust_forwarded_ip: true,

    forwarded_client_ip_headers: [],

    // LinuxDo Connect OAuth 登录
    linuxdo_connect_enabled: false,

    linuxdo_connect_client_id: "",

    linuxdo_connect_client_secret: "",

    linuxdo_connect_client_secret_configured: false,

    linuxdo_connect_redirect_url: "",

    // DingTalk Connect OAuth 登录
    dingtalk_connect_enabled: false,

    dingtalk_connect_client_id: "",

    dingtalk_connect_client_secret: "",

    dingtalk_connect_client_secret_configured: false,

    dingtalk_connect_redirect_url: "",

    dingtalk_connect_corp_restriction_policy: "none",

    dingtalk_connect_internal_corp_id: "",

    dingtalk_connect_bypass_registration: false,

    dingtalk_connect_sync_corp_email: false,

    dingtalk_connect_sync_display_name: false,

    dingtalk_connect_sync_dept: false,

    dingtalk_connect_sync_corp_email_attr_key: "dingtalk_email",

    dingtalk_connect_sync_display_name_attr_key: "dingtalk_name",

    dingtalk_connect_sync_dept_attr_key: "dingtalk_department",

    dingtalk_connect_sync_corp_email_attr_name: localText(
      "钉钉企业邮箱",
      "DingTalk Corporate Email",
    ),

    dingtalk_connect_sync_display_name_attr_name: localText(
      "钉钉姓名",
      "DingTalk Name",
    ),

    dingtalk_connect_sync_dept_attr_name: localText(
      "钉钉部门",
      "DingTalk Department",
    ),

    wechat_connect_enabled: false,

    wechat_connect_app_id: "",

    wechat_connect_app_secret: "",

    wechat_connect_app_secret_configured: false,

    wechat_connect_open_app_id: "",

    wechat_connect_open_app_secret: "",

    wechat_connect_open_app_secret_configured: false,

    wechat_connect_mp_app_id: "",

    wechat_connect_mp_app_secret: "",

    wechat_connect_mp_app_secret_configured: false,

    wechat_connect_mobile_app_id: "",

    wechat_connect_mobile_app_secret: "",

    wechat_connect_mobile_app_secret_configured: false,

    wechat_connect_open_enabled: false,

    wechat_connect_mp_enabled: false,

    wechat_connect_mobile_enabled: false,

    wechat_connect_mode: "open",

    wechat_connect_scopes: "snsapi_login",

    wechat_connect_redirect_url: "",

    wechat_connect_frontend_redirect_url: "/auth/wechat/callback",

    // Generic OIDC OAuth 登录
    oidc_connect_enabled: false,

    oidc_connect_provider_name: "OIDC",

    oidc_connect_client_id: "",

    oidc_connect_client_secret: "",

    oidc_connect_client_secret_configured: false,

    oidc_connect_issuer_url: "",

    oidc_connect_discovery_url: "",

    oidc_connect_authorize_url: "",

    oidc_connect_token_url: "",

    oidc_connect_userinfo_url: "",

    oidc_connect_jwks_url: "",

    oidc_connect_scopes: "openid email profile",

    oidc_connect_redirect_url: "",

    oidc_connect_frontend_redirect_url: "/auth/oidc/callback",

    oidc_connect_token_auth_method: "client_secret_post",

    oidc_connect_use_pkce: false,

    oidc_connect_validate_id_token: false,

    oidc_connect_allowed_signing_algs: "RS256,ES256,PS256",

    oidc_connect_clock_skew_seconds: 120,

    oidc_connect_require_email_verified: false,

    oidc_connect_userinfo_email_path: "",

    oidc_connect_userinfo_id_path: "",

    oidc_connect_userinfo_username_path: "",

    // GitHub / Google 邮箱快捷登录
    github_oauth_enabled: false,

    github_oauth_client_id: "",

    github_oauth_client_secret: "",

    github_oauth_client_secret_configured: false,

    github_oauth_redirect_url: "",

    github_oauth_frontend_redirect_url: "/auth/oauth/callback",

    google_oauth_enabled: false,

    google_oauth_client_id: "",

    google_oauth_client_secret: "",

    google_oauth_client_secret_configured: false,

    google_oauth_redirect_url: "",

    google_oauth_frontend_redirect_url: "/auth/oauth/callback",
  });
}
