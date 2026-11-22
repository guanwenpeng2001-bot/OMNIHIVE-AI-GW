package config

import "sort"

const (
	DefaultGitHubOAuthAuthorize = "https://github.com/login/oauth/authorize"
	DefaultGitHubOAuthToken     = "https://github.com/login/oauth/access_token"
	DefaultGitHubOAuthUserInfo  = "https://api.github.com/user"
	DefaultGitHubOAuthEmails    = "https://api.github.com/user/emails"
	DefaultGitHubOAuthScopes    = "read:user user:email"
	DefaultGitHubOAuthFrontend  = "/auth/oauth/callback"
	DefaultGoogleOAuthAuthorize = "https://accounts.google.com/o/oauth2/v2/auth"
	DefaultGoogleOAuthToken     = "https://oauth2.googleapis.com/token"
	DefaultGoogleOAuthUserInfo  = "https://openidconnect.googleapis.com/v1/userinfo"
	DefaultGoogleOAuthScopes    = "openid email profile"
	DefaultGoogleOAuthFrontend  = "/auth/oauth/callback"
)

// operationalConfigDefaults is the single declaration consumed by loading,
// diagnostics and new-install settings inheritance. Never mutate this map.
var operationalConfigDefaults = map[string]any{
	"default.user_balance":                             DefaultUserBalance,
	"default.user_concurrency":                         DefaultUserConcurrency,
	"dingtalk_connect.app_type":                        "public",
	"dingtalk_connect.attribute_sync_fields":           []string{},
	"dingtalk_connect.attribute_sync_overwrite_policy": "",
	"dingtalk_connect.authorize_url":                   "https://login.dingtalk.com/oauth2/auth",
	"dingtalk_connect.bypass_registration":             false,
	"dingtalk_connect.client_id":                       "",
	"dingtalk_connect.client_secret":                   "",
	"dingtalk_connect.corp_restriction_policy":         "none",
	"dingtalk_connect.dingtalk_app_kind":               "internal_app",
	"dingtalk_connect.enable_attribute_matching":       false,
	"dingtalk_connect.enable_attribute_sync":           false,
	"dingtalk_connect.enabled":                         false,
	"dingtalk_connect.frontend_redirect_url":           "/auth/dingtalk/callback",
	"dingtalk_connect.internal_corp_id":                "",
	"dingtalk_connect.redirect_url":                    "",
	"dingtalk_connect.require_email":                   true,
	"dingtalk_connect.scopes":                          "openid",
	"dingtalk_connect.sync_corp_email":                 false,
	"dingtalk_connect.sync_corp_email_attr_key":        "",
	"dingtalk_connect.sync_corp_email_attr_name":       "",
	"dingtalk_connect.sync_dept":                       false,
	"dingtalk_connect.sync_dept_attr_key":              "",
	"dingtalk_connect.sync_dept_attr_name":             "",
	"dingtalk_connect.sync_display_name":               false,
	"dingtalk_connect.sync_display_name_attr_key":      "",
	"dingtalk_connect.sync_display_name_attr_name":     "",
	"dingtalk_connect.token_url":                       "https://api.dingtalk.com/v1.0/oauth2/userAccessToken",
	"dingtalk_connect.userinfo_url":                    "https://api.dingtalk.com/v1.0/contact/users/me",
	"dingtalk_connect.username_attribute_key":          "",
	"dingtalk_connect.username_overwrite_policy":       "if_empty",
	"github_oauth.enabled":                             false,
	"github_oauth.client_id":                           "",
	"github_oauth.client_secret":                       "",
	"github_oauth.authorize_url":                       DefaultGitHubOAuthAuthorize,
	"github_oauth.token_url":                           DefaultGitHubOAuthToken,
	"github_oauth.userinfo_url":                        DefaultGitHubOAuthUserInfo,
	"github_oauth.emails_url":                          DefaultGitHubOAuthEmails,
	"github_oauth.scopes":                              DefaultGitHubOAuthScopes,
	"github_oauth.redirect_url":                        "",
	"github_oauth.frontend_redirect_url":               DefaultGitHubOAuthFrontend,
	"google_oauth.enabled":                             false,
	"google_oauth.client_id":                           "",
	"google_oauth.client_secret":                       "",
	"google_oauth.authorize_url":                       DefaultGoogleOAuthAuthorize,
	"google_oauth.token_url":                           DefaultGoogleOAuthToken,
	"google_oauth.userinfo_url":                        DefaultGoogleOAuthUserInfo,
	"google_oauth.emails_url":                          "",
	"google_oauth.scopes":                              DefaultGoogleOAuthScopes,
	"google_oauth.redirect_url":                        "",
	"google_oauth.frontend_redirect_url":               DefaultGoogleOAuthFrontend,
	"image_storage.access_key_id":                      "",
	"image_storage.bucket":                             "",
	"image_storage.enabled":                            false,
	"image_storage.endpoint":                           "",
	"image_storage.force_path_style":                   false,
	"image_storage.max_download_bytes":                 33554432,
	"image_storage.prefix":                             "images/",
	"image_storage.presign_expiry_hours":               24,
	"image_storage.public_base_url":                    "",
	"image_storage.region":                             "auto",
	"image_storage.secret_access_key":                  "",
	"linuxdo_connect.authorize_url":                    "https://connect.linux.do/oauth2/authorize",
	"linuxdo_connect.client_id":                        "",
	"linuxdo_connect.client_secret":                    "",
	"linuxdo_connect.enabled":                          false,
	"linuxdo_connect.frontend_redirect_url":            "/auth/linuxdo/callback",
	"linuxdo_connect.redirect_url":                     "",
	"linuxdo_connect.scopes":                           "user",
	"linuxdo_connect.token_auth_method":                "client_secret_post",
	"linuxdo_connect.token_url":                        "https://connect.linux.do/oauth2/token",
	"linuxdo_connect.use_pkce":                         false,
	"linuxdo_connect.userinfo_email_path":              "",
	"linuxdo_connect.userinfo_id_path":                 "",
	"linuxdo_connect.userinfo_url":                     "https://connect.linux.do/api/user",
	"linuxdo_connect.userinfo_username_path":           "",
	"log.format":                                       DefaultLogFormat,
	"model_catalog_refresh_enabled":                    DefaultModelCatalogRefreshEnabled,
	"oidc_connect.allowed_signing_algs":                "RS256,ES256,PS256",
	"oidc_connect.authorize_url":                       "",
	"oidc_connect.client_id":                           "",
	"oidc_connect.client_secret":                       "",
	"oidc_connect.clock_skew_seconds":                  120,
	"oidc_connect.discovery_url":                       "",
	"oidc_connect.enabled":                             false,
	"oidc_connect.frontend_redirect_url":               "/auth/oidc/callback",
	"oidc_connect.issuer_url":                          "",
	"oidc_connect.jwks_url":                            "",
	"oidc_connect.provider_name":                       "OIDC",
	"oidc_connect.redirect_url":                        "",
	"oidc_connect.require_email_verified":              false,
	"oidc_connect.scopes":                              "openid email profile",
	"oidc_connect.token_auth_method":                   "client_secret_post",
	"oidc_connect.token_url":                           "",
	"oidc_connect.use_pkce":                            true,
	"oidc_connect.userinfo_email_path":                 "",
	"oidc_connect.userinfo_id_path":                    "",
	"oidc_connect.userinfo_url":                        "",
	"oidc_connect.userinfo_username_path":              "",
	"oidc_connect.validate_id_token":                   true,
	"ops.enabled":                                      true,
	"secrets.encryption_key":                           "",
	"totp.encryption_key":                              "",
	"turnstile.required":                               false,
	"webauthn.enabled":                                 false,
	"wechat_connect.app_id":                            "",
	"wechat_connect.app_secret":                        "",
	"wechat_connect.enabled":                           false,
	"wechat_connect.frontend_redirect_url":             defaultWeChatConnectFrontendRedirect,
	"wechat_connect.mobile_app_id":                     "",
	"wechat_connect.mobile_app_secret":                 "",
	"wechat_connect.mobile_enabled":                    false,
	"wechat_connect.mode":                              defaultWeChatConnectMode,
	"wechat_connect.mp_app_id":                         "",
	"wechat_connect.mp_app_secret":                     "",
	"wechat_connect.mp_enabled":                        false,
	"wechat_connect.open_app_id":                       "",
	"wechat_connect.open_app_secret":                   "",
	"wechat_connect.open_enabled":                      false,
	"wechat_connect.redirect_url":                      "",
	"wechat_connect.scopes":                            defaultWeChatConnectScopes,
}

// OperationalDefault returns a declared value, not the running effective value.
func OperationalDefault(key string) (any, bool) {
	value, ok := operationalConfigDefaults[key]
	return value, ok
}

func OperationalDefaultKeys() []string {
	keys := make([]string, 0, len(operationalConfigDefaults))
	for key := range operationalConfigDefaults {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
