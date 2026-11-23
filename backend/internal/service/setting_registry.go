package service

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
)

type SettingAuthority string

const (
	SettingAuthorityDeployment        SettingAuthority = "deployment"
	SettingAuthorityDatabaseOverride  SettingAuthority = "database_override"
	SettingAuthorityDatabaseOnly      SettingAuthority = "database_only"
	SettingAuthorityVersionedArtifact SettingAuthority = "versioned_artifact"
)

type SettingValueType string

const (
	SettingValueBool       SettingValueType = "bool"
	SettingValueInt        SettingValueType = "int"
	SettingValueFloat      SettingValueType = "float"
	SettingValueString     SettingValueType = "string"
	SettingValueStringList SettingValueType = "string_list"
	SettingValueJSON       SettingValueType = "json"
	SettingValueObject     SettingValueType = "object"
)

type SettingErrorPolicy string

const (
	SettingErrorFailClosed SettingErrorPolicy = "fail_closed"
	SettingErrorFailOpen   SettingErrorPolicy = "fail_open"
	SettingErrorDefault    SettingErrorPolicy = "default"
)

type SettingRegistration struct {
	Key             string
	Group           string
	ValueType       SettingValueType
	Authority       SettingAuthority
	Default         any
	ConfigField     string
	EnvironmentKey  string
	Projection      string
	ResetGroup      string
	Sensitive       bool
	RestartRequired bool
	Resettable      bool
	ErrorPolicy     SettingErrorPolicy
	Validation      string
}

const (
	defaultRegistryBalance     = 0.0
	defaultRegistryConcurrency = 5
)

var sensitiveSettingKeys = map[string]struct{}{
	SettingKeyAdminAPIKey: {}, SettingKeySMTPPassword: {}, SettingKeyTurnstileSecretKey: {},
	SettingKeyTencentCaptchaAppSecretKey: {}, SettingKeyTencentCaptchaCloudSecretKey: {},
	SettingKeyAliyunCaptchaAccessKeySecret: {}, SettingKeyLinuxDoConnectClientSecret: {},
	SettingKeyDingTalkConnectClientSecret: {}, SettingKeyWeChatConnectAppSecret: {},
	SettingKeyWeChatConnectOpenAppSecret: {}, SettingKeyWeChatConnectMPAppSecret: {},
	SettingKeyWeChatConnectMobileAppSecret: {}, SettingKeyOIDCConnectClientSecret: {},
	SettingKeyGitHubOAuthClientSecret: {}, SettingKeyGoogleOAuthClientSecret: {},
	SettingKeyWebSearchEmulationConfig: {},
	settingKeyImageStorageConfig:       {},
}

var installSeedSettingKeys = map[string]string{
	SettingKeyDefaultBalance: "DEFAULT_USER_BALANCE", SettingKeyDefaultConcurrency: "DEFAULT_USER_CONCURRENCY",
}

var securitySettingKeys = map[string]struct{}{
	SettingKeyRegistrationEnabled: {}, SettingKeyPasswordResetEnabled: {},
	SettingKeyInvitationCodeEnabled: {}, SettingKeyTotpEnabled: {},
	SettingKeyPasskeyEnabled: {}, SettingKeySessionBindingEnabled: {},
	SettingKeyStepUpEnabled: {}, SettingKeyTurnstileEnabled: {},
	SettingKeyAPIKeyACLTrustForwardedIP: {}, SettingKeyForwardedClientIPHeaders: {},
	SettingKeyBackendModeEnabled: {}, SettingKeyAllowUngroupedKeyScheduling: {},
	SettingKeyOpsMonitoringEnabled: {},
}

var deploymentSettingRegistrations = []SettingRegistration{
	{Key: "model_catalog_refresh_enabled", Group: "catalog", ValueType: SettingValueBool, Authority: SettingAuthorityDeployment, Default: false, ConfigField: "ModelCatalogRefreshEnabled", EnvironmentKey: "MODEL_CATALOG_REFRESH_ENABLED", RestartRequired: true, ErrorPolicy: SettingErrorFailClosed, Validation: "boolean"},
	{Key: "log.format", Group: "logging", ValueType: SettingValueString, Authority: SettingAuthorityDeployment, Default: "json", ConfigField: "Log.Format", EnvironmentKey: "LOG_FORMAT", RestartRequired: true, ErrorPolicy: SettingErrorFailClosed, Validation: "console|json"},
	{Key: "turnstile.required", Group: "security", ValueType: SettingValueBool, Authority: SettingAuthorityDeployment, Default: false, ConfigField: "Turnstile.Required", EnvironmentKey: "TURNSTILE_REQUIRED", RestartRequired: true, ErrorPolicy: SettingErrorFailClosed, Validation: "boolean; enforced only in release mode"},
	{Key: "webauthn.enabled", Group: "security", ValueType: SettingValueBool, Authority: SettingAuthorityDeployment, Default: false, ConfigField: "WebAuthn.Enabled", EnvironmentKey: "WEBAUTHN_ENABLED", RestartRequired: true, ErrorPolicy: SettingErrorFailClosed, Validation: "valid relying-party configuration"},
	{Key: "webauthn.rp_id", Group: "security", ValueType: SettingValueString, Authority: SettingAuthorityDeployment, Default: "", ConfigField: "WebAuthn.RPID", EnvironmentKey: "WEBAUTHN_RP_ID", RestartRequired: true, ErrorPolicy: SettingErrorFailClosed, Validation: "relying-party hostname"},
	{Key: "webauthn.rp_origins", Group: "security", ValueType: SettingValueStringList, Authority: SettingAuthorityDeployment, Default: []string{}, ConfigField: "WebAuthn.RPOrigins", EnvironmentKey: "WEBAUTHN_RP_ORIGINS", RestartRequired: true, ErrorPolicy: SettingErrorFailClosed, Validation: "trusted HTTPS origins"},
	{Key: "ops.enabled", Group: "ops", ValueType: SettingValueBool, Authority: SettingAuthorityDeployment, Default: true, ConfigField: "Ops.Enabled", EnvironmentKey: "OPS_ENABLED", RestartRequired: true, ErrorPolicy: SettingErrorFailClosed, Validation: "boolean"},
	{Key: "image_storage.enabled", Group: "image_storage", ValueType: SettingValueBool, Authority: SettingAuthorityDatabaseOverride, Default: false, ConfigField: "ImageStorage.Enabled", EnvironmentKey: "IMAGE_STORAGE_ENABLED", Resettable: true, ResetGroup: "image_storage", ErrorPolicy: SettingErrorFailClosed, Validation: "boolean"},
	{Key: "image_storage.presign_expiry_hours", Group: "image_storage", ValueType: SettingValueInt, Authority: SettingAuthorityDatabaseOverride, Default: 1, ConfigField: "ImageStorage.PresignExpiry", EnvironmentKey: "IMAGE_STORAGE_PRESIGN_EXPIRY_HOURS", Resettable: true, ResetGroup: "image_storage", ErrorPolicy: SettingErrorFailClosed, Validation: "positive"},
	{Key: "image_storage.max_download_bytes", Group: "image_storage", ValueType: SettingValueInt, Authority: SettingAuthorityDatabaseOverride, Default: int64(0), ConfigField: "ImageStorage.MaxDownloadByte", EnvironmentKey: "IMAGE_STORAGE_MAX_DOWNLOAD_BYTES", Resettable: true, ResetGroup: "image_storage", ErrorPolicy: SettingErrorFailClosed, Validation: "non-negative"},
	{Key: "image_storage.secret_access_key", Group: "image_storage", ValueType: SettingValueString, Authority: SettingAuthorityDatabaseOverride, Default: "", ConfigField: "ImageStorage.SecretAccessKey", EnvironmentKey: "IMAGE_STORAGE_SECRET_ACCESS_KEY", Resettable: true, ResetGroup: "image_storage", Sensitive: true, ErrorPolicy: SettingErrorFailClosed, Validation: "encrypted at rest"},
	{Key: "image_storage.access_key_id", Group: "image_storage", ValueType: SettingValueString, Authority: SettingAuthorityDatabaseOverride, Default: "", ConfigField: "ImageStorage.AccessKeyID", EnvironmentKey: "IMAGE_STORAGE_ACCESS_KEY_ID", Resettable: true, ResetGroup: "image_storage", ErrorPolicy: SettingErrorFailClosed, Validation: "S3 access key identifier"},
	{Key: "image_storage.reuse_backup_s3", Group: "image_storage", ValueType: SettingValueBool, Authority: SettingAuthorityDatabaseOverride, Default: false, EnvironmentKey: "IMAGE_STORAGE_REUSE_BACKUP_S3", Resettable: true, ResetGroup: "image_storage", ErrorPolicy: SettingErrorFailClosed, Validation: "reference BackupService S3 credentials"},
}

var oauthSettingRegistrations = []SettingRegistration{
	{Key: SettingKeyLinuxDoConnectEnabled, ConfigField: "LinuxDo.Enabled", ValueType: SettingValueBool},
	{Key: SettingKeyLinuxDoConnectClientID, ConfigField: "LinuxDo.ClientID", ValueType: SettingValueString},
	{Key: SettingKeyLinuxDoConnectClientSecret, ConfigField: "LinuxDo.ClientSecret", ValueType: SettingValueString},
	{Key: SettingKeyLinuxDoConnectRedirectURL, ConfigField: "LinuxDo.RedirectURL", ValueType: SettingValueString},
	{Key: SettingKeyDingTalkConnectEnabled, ConfigField: "DingTalk.Enabled", ValueType: SettingValueBool},
	{Key: SettingKeyDingTalkConnectClientID, ConfigField: "DingTalk.ClientID", ValueType: SettingValueString},
	{Key: SettingKeyDingTalkConnectClientSecret, ConfigField: "DingTalk.ClientSecret", ValueType: SettingValueString},
	{Key: SettingKeyDingTalkConnectRedirectURL, ConfigField: "DingTalk.RedirectURL", ValueType: SettingValueString},
	{Key: SettingKeyDingTalkConnectCorpRestrictionPolicy, ConfigField: "DingTalk.CorpRestrictionPolicy", ValueType: SettingValueString},
	{Key: SettingKeyDingTalkConnectInternalCorpID, ConfigField: "DingTalk.InternalCorpID", ValueType: SettingValueString},
	{Key: SettingKeyDingTalkConnectBypassRegistration, ConfigField: "DingTalk.BypassRegistration", ValueType: SettingValueBool},
	{Key: SettingKeyDingTalkConnectSyncCorpEmail, ConfigField: "DingTalk.SyncCorpEmail", ValueType: SettingValueBool},
	{Key: SettingKeyDingTalkConnectSyncDisplayName, ConfigField: "DingTalk.SyncDisplayName", ValueType: SettingValueBool},
	{Key: SettingKeyDingTalkConnectSyncDept, ConfigField: "DingTalk.SyncDept", ValueType: SettingValueBool},
	{Key: SettingKeyDingTalkConnectSyncCorpEmailAttrKey, ConfigField: "DingTalk.SyncCorpEmailAttrKey", ValueType: SettingValueString},
	{Key: SettingKeyDingTalkConnectSyncDisplayNameAttrKey, ConfigField: "DingTalk.SyncDisplayNameAttrKey", ValueType: SettingValueString},
	{Key: SettingKeyDingTalkConnectSyncDeptAttrKey, ConfigField: "DingTalk.SyncDeptAttrKey", ValueType: SettingValueString},
	{Key: SettingKeyDingTalkConnectSyncCorpEmailAttrName, ConfigField: "DingTalk.SyncCorpEmailAttrName", ValueType: SettingValueString},
	{Key: SettingKeyDingTalkConnectSyncDisplayNameAttrName, ConfigField: "DingTalk.SyncDisplayNameAttrName", ValueType: SettingValueString},
	{Key: SettingKeyDingTalkConnectSyncDeptAttrName, ConfigField: "DingTalk.SyncDeptAttrName", ValueType: SettingValueString},
	{Key: SettingKeyWeChatConnectEnabled, ConfigField: "WeChat.Enabled", ValueType: SettingValueBool},
	{Key: SettingKeyWeChatConnectAppID, ConfigField: "WeChat.AppID", ValueType: SettingValueString},
	{Key: SettingKeyWeChatConnectAppSecret, ConfigField: "WeChat.AppSecret", ValueType: SettingValueString},
	{Key: SettingKeyWeChatConnectOpenAppID, ConfigField: "WeChat.OpenAppID", ValueType: SettingValueString},
	{Key: SettingKeyWeChatConnectOpenAppSecret, ConfigField: "WeChat.OpenAppSecret", ValueType: SettingValueString},
	{Key: SettingKeyWeChatConnectMPAppID, ConfigField: "WeChat.MPAppID", ValueType: SettingValueString},
	{Key: SettingKeyWeChatConnectMPAppSecret, ConfigField: "WeChat.MPAppSecret", ValueType: SettingValueString},
	{Key: SettingKeyWeChatConnectMobileAppID, ConfigField: "WeChat.MobileAppID", ValueType: SettingValueString},
	{Key: SettingKeyWeChatConnectMobileAppSecret, ConfigField: "WeChat.MobileAppSecret", ValueType: SettingValueString},
	{Key: SettingKeyWeChatConnectOpenEnabled, ConfigField: "WeChat.OpenEnabled", ValueType: SettingValueBool},
	{Key: SettingKeyWeChatConnectMPEnabled, ConfigField: "WeChat.MPEnabled", ValueType: SettingValueBool},
	{Key: SettingKeyWeChatConnectMobileEnabled, ConfigField: "WeChat.MobileEnabled", ValueType: SettingValueBool},
	{Key: SettingKeyWeChatConnectMode, ConfigField: "WeChat.Mode", ValueType: SettingValueString},
	{Key: SettingKeyWeChatConnectScopes, ConfigField: "WeChat.Scopes", ValueType: SettingValueString},
	{Key: SettingKeyWeChatConnectRedirectURL, ConfigField: "WeChat.RedirectURL", ValueType: SettingValueString},
	{Key: SettingKeyWeChatConnectFrontendRedirectURL, ConfigField: "WeChat.FrontendRedirectURL", ValueType: SettingValueString},
	{Key: SettingKeyOIDCConnectEnabled, ConfigField: "OIDC.Enabled", ValueType: SettingValueBool},
	{Key: SettingKeyOIDCConnectProviderName, ConfigField: "OIDC.ProviderName", ValueType: SettingValueString},
	{Key: SettingKeyOIDCConnectClientID, ConfigField: "OIDC.ClientID", ValueType: SettingValueString},
	{Key: SettingKeyOIDCConnectClientSecret, ConfigField: "OIDC.ClientSecret", ValueType: SettingValueString},
	{Key: SettingKeyOIDCConnectIssuerURL, ConfigField: "OIDC.IssuerURL", ValueType: SettingValueString},
	{Key: SettingKeyOIDCConnectDiscoveryURL, ConfigField: "OIDC.DiscoveryURL", ValueType: SettingValueString},
	{Key: SettingKeyOIDCConnectAuthorizeURL, ConfigField: "OIDC.AuthorizeURL", ValueType: SettingValueString},
	{Key: SettingKeyOIDCConnectTokenURL, ConfigField: "OIDC.TokenURL", ValueType: SettingValueString},
	{Key: SettingKeyOIDCConnectUserInfoURL, ConfigField: "OIDC.UserInfoURL", ValueType: SettingValueString},
	{Key: SettingKeyOIDCConnectJWKSURL, ConfigField: "OIDC.JWKSURL", ValueType: SettingValueString},
	{Key: SettingKeyOIDCConnectScopes, ConfigField: "OIDC.Scopes", ValueType: SettingValueString},
	{Key: SettingKeyOIDCConnectRedirectURL, ConfigField: "OIDC.RedirectURL", ValueType: SettingValueString},
	{Key: SettingKeyOIDCConnectFrontendRedirectURL, ConfigField: "OIDC.FrontendRedirectURL", ValueType: SettingValueString},
	{Key: SettingKeyOIDCConnectTokenAuthMethod, ConfigField: "OIDC.TokenAuthMethod", ValueType: SettingValueString},
	{Key: SettingKeyOIDCConnectUsePKCE, ConfigField: "OIDC.UsePKCE", ValueType: SettingValueBool},
	{Key: SettingKeyOIDCConnectValidateIDToken, ConfigField: "OIDC.ValidateIDToken", ValueType: SettingValueBool},
	{Key: SettingKeyOIDCConnectAllowedSigningAlgs, ConfigField: "OIDC.AllowedSigningAlgs", ValueType: SettingValueString},
	{Key: SettingKeyOIDCConnectClockSkewSeconds, ConfigField: "OIDC.ClockSkewSeconds", ValueType: SettingValueInt},
	{Key: SettingKeyOIDCConnectRequireEmailVerified, ConfigField: "OIDC.RequireEmailVerified", ValueType: SettingValueBool},
	{Key: SettingKeyOIDCConnectUserInfoEmailPath, ConfigField: "OIDC.UserInfoEmailPath", ValueType: SettingValueString},
	{Key: SettingKeyOIDCConnectUserInfoIDPath, ConfigField: "OIDC.UserInfoIDPath", ValueType: SettingValueString},
	{Key: SettingKeyOIDCConnectUserInfoUsernamePath, ConfigField: "OIDC.UserInfoUsernamePath", ValueType: SettingValueString},
	{Key: SettingKeyGitHubOAuthEnabled, ConfigField: "GitHubOAuth.Enabled", ValueType: SettingValueBool},
	{Key: SettingKeyGitHubOAuthClientID, ConfigField: "GitHubOAuth.ClientID", ValueType: SettingValueString},
	{Key: SettingKeyGitHubOAuthClientSecret, ConfigField: "GitHubOAuth.ClientSecret", ValueType: SettingValueString},
	{Key: SettingKeyGitHubOAuthRedirectURL, ConfigField: "GitHubOAuth.RedirectURL", ValueType: SettingValueString},
	{Key: SettingKeyGitHubOAuthFrontendRedirectURL, ConfigField: "GitHubOAuth.FrontendRedirectURL", ValueType: SettingValueString},
	{Key: SettingKeyGoogleOAuthEnabled, ConfigField: "GoogleOAuth.Enabled", ValueType: SettingValueBool},
	{Key: SettingKeyGoogleOAuthClientID, ConfigField: "GoogleOAuth.ClientID", ValueType: SettingValueString},
	{Key: SettingKeyGoogleOAuthClientSecret, ConfigField: "GoogleOAuth.ClientSecret", ValueType: SettingValueString},
	{Key: SettingKeyGoogleOAuthRedirectURL, ConfigField: "GoogleOAuth.RedirectURL", ValueType: SettingValueString},
	{Key: SettingKeyGoogleOAuthFrontendRedirectURL, ConfigField: "GoogleOAuth.FrontendRedirectURL", ValueType: SettingValueString},
}

func (s *SettingService) databaseSettingDefaults() map[string]string {
	defaults := map[string]string{}
	for key, value := range coreDatabaseSettingDefaults {
		defaults[key] = value
	}
	for _, registration := range oauthSettingRegistrations {
		defaults[registration.Key] = fmt.Sprint(zeroSettingValue(registration.ValueType))
	}
	return defaults
}

func (s *SettingService) installSettingDefaults(newInstallation bool) map[string]string {
	defaults := s.databaseSettingDefaults()
	for _, registration := range oauthSettingRegistrations {
		delete(defaults, registration.Key)
	}
	defaults[SettingKeyModelPlazaEnabled] = strconv.FormatBool(newInstallation)
	return defaults
}

func (s *SettingService) SettingRegistry() []SettingRegistration {
	defaults := s.databaseSettingDefaults()
	registrations := make([]SettingRegistration, 0, len(defaults)+len(deploymentSettingRegistrations)+len(oauthSettingRegistrations))
	for key, raw := range defaults {
		registrations = append(registrations, databaseRegistration(key, raw))
	}
	for _, registration := range deploymentSettingRegistrations {
		registration.Projection = registration.Key
		registrations = append(registrations, registration)
	}
	for _, registration := range oauthSettingRegistrations {
		registration.Group = oauthGroup(registration.Key)
		registration.Authority = SettingAuthorityDatabaseOverride
		registration.Projection = registration.Key
		registration.Resettable = true
		registration.ResetGroup = registration.Group
		registration.ErrorPolicy = SettingErrorFailClosed
		registration.Validation = "validated by the provider login service"
		_, registration.Sensitive = sensitiveSettingKeys[registration.Key]
		if field, ok := configField(s.cfg, registration.ConfigField); ok {
			registration.Default = field
		} else {
			registration.Default = zeroSettingValue(registration.ValueType)
		}
		registrations = append(registrations, registration)
	}
	registrations = append(registrations,
		SettingRegistration{Key: "default_balance_install_seed_source", Group: "onboarding", ValueType: SettingValueString, Authority: SettingAuthorityDatabaseOnly, Default: "unknown", Projection: "default_balance.install_seed_source", ErrorPolicy: SettingErrorFailClosed, Validation: "migration provenance"},
		SettingRegistration{Key: "default_concurrency_install_seed_source", Group: "onboarding", ValueType: SettingValueString, Authority: SettingAuthorityDatabaseOnly, Default: "unknown", Projection: "default_concurrency.install_seed_source", ErrorPolicy: SettingErrorFailClosed, Validation: "migration provenance"},
		SettingRegistration{Key: "image_storage_config", Group: "image_storage", ValueType: SettingValueObject, Authority: SettingAuthorityDatabaseOverride, Default: map[string]any{}, Projection: "image_storage", Resettable: true, ResetGroup: "image_storage", ErrorPolicy: SettingErrorFailClosed, Validation: "validated ImageStorageSettings JSON"},
		SettingRegistration{Key: SettingKeyOpsAdvancedSettings, Group: "ops", ValueType: SettingValueObject, Authority: SettingAuthorityDatabaseOverride, Default: map[string]any{}, Projection: "ops_advanced_settings", Resettable: true, ErrorPolicy: SettingErrorFailClosed, Validation: "validated OpsAdvancedSettings JSON"},
		SettingRegistration{Key: SettingKeyOpsRuntimeLogConfig, Group: "ops", ValueType: SettingValueObject, Authority: SettingAuthorityDatabaseOverride, Default: map[string]any{}, Projection: "ops_runtime_log_config", Resettable: true, ErrorPolicy: SettingErrorFailClosed, Validation: "validated OpsRuntimeLogConfig JSON"},
		SettingRegistration{Key: "backup_s3_config", Group: "backup", ValueType: SettingValueObject, Authority: SettingAuthorityDatabaseOnly, Default: map[string]any{}, Sensitive: true, ErrorPolicy: SettingErrorFailClosed, Validation: "managed by BackupService"},
		SettingRegistration{Key: "backup_schedule", Group: "backup", ValueType: SettingValueObject, Authority: SettingAuthorityDatabaseOnly, Default: map[string]any{"enabled": false, "retain_days": 14}, ErrorPolicy: SettingErrorFailClosed, Validation: "managed by BackupService"},
	)
	sort.Slice(registrations, func(left, right int) bool { return registrations[left].Key < registrations[right].Key })
	return registrations
}

func databaseRegistration(key, raw string) SettingRegistration {
	registration := SettingRegistration{
		Key: key, Group: settingGroup(key), Authority: SettingAuthorityDatabaseOnly,
		Projection: key, Default: decodeSettingDefault(raw), ErrorPolicy: SettingErrorDefault,
		Validation: "registered database value",
	}
	registration.ValueType = settingValueType(registration.Default)
	if key == SettingKeyDefaultBalance || key == SettingKeyOpenAIOAuthSchedulingRateMultiplier || key == SettingKeyAffiliateRebateRate || key == SettingKeyAffiliateRebatePerInviteeCap {
		registration.ValueType = SettingValueFloat
	}
	_, registration.Sensitive = sensitiveSettingKeys[key]
	if _, ok := securitySettingKeys[key]; ok {
		registration.ErrorPolicy = SettingErrorFailClosed
	}
	if key == SettingKeyOpsMonitoringEnabled {
		registration.ErrorPolicy = SettingErrorFailOpen
	}
	if _, ok := installSeedSettingKeys[key]; ok {
		registration.ErrorPolicy = SettingErrorFailClosed
	}
	return registration
}

func settingValueType(value any) SettingValueType {
	switch value.(type) {
	case bool:
		return SettingValueBool
	case int, int64:
		return SettingValueInt
	case float64:
		return SettingValueFloat
	case []string:
		return SettingValueStringList
	case []any:
		return SettingValueJSON
	case map[string]any:
		return SettingValueObject
	default:
		return SettingValueString
	}
}

func installSeedSource(key string) string {
	_, ok := installSeedSettingKeys[key]
	if !ok {
		return ""
	}
	return "unknown"
}

func settingGroup(key string) string {
	switch {
	case strings.HasPrefix(key, "auth_source_default_"), strings.HasPrefix(key, "default_"):
		return "onboarding"
	case strings.Contains(key, "smtp"):
		return "email"
	case strings.HasPrefix(key, "ops_"):
		return "ops"
	case strings.HasPrefix(key, "model_plaza_"):
		return "plaza"
	case strings.HasPrefix(key, "openai_"), strings.HasPrefix(key, "codex_"):
		return "gateway"
	default:
		return "system"
	}
}

func oauthGroup(key string) string {
	for _, prefix := range []string{"linuxdo_connect", "dingtalk_connect", "wechat_connect", "oidc_connect", "github_oauth", "google_oauth"} {
		if strings.HasPrefix(key, prefix) {
			return prefix
		}
	}
	return "oauth"
}

func decodeSettingDefault(raw string) any {
	if parsed, err := strconv.ParseBool(raw); err == nil {
		return parsed
	}
	if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return parsed
	}
	if parsed, err := strconv.ParseFloat(raw, 64); err == nil {
		return parsed
	}
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
		var decoded any
		if json.Unmarshal([]byte(trimmed), &decoded) == nil {
			return decoded
		}
	}
	return raw
}

func zeroSettingValue(valueType SettingValueType) any {
	switch valueType {
	case SettingValueBool:
		return false
	case SettingValueInt:
		return int64(0)
	case SettingValueFloat:
		return float64(0)
	case SettingValueStringList:
		return []string{}
	case SettingValueJSON, SettingValueObject:
		return map[string]any{}
	default:
		return ""
	}
}

func configField(cfg *config.Config, path string) (any, bool) {
	if cfg == nil || path == "" {
		return nil, false
	}
	value := reflect.ValueOf(cfg)
	for _, component := range strings.Split(path, ".") {
		if value.Kind() == reflect.Pointer {
			if value.IsNil() {
				return nil, false
			}
			value = value.Elem()
		}
		if value.Kind() != reflect.Struct {
			return nil, false
		}
		value = value.FieldByName(component)
		if !value.IsValid() {
			panic(fmt.Sprintf("invalid settings registry config field %q", path))
		}
	}
	return value.Interface(), true
}
