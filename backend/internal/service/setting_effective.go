package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	infraerrors "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/errors"
)

var ErrSettingVersionConflict = infraerrors.Conflict("SETTING_VERSION_CONFLICT", "settings changed; reload the effective settings before resetting overrides")

// VersionedSettingRepository is an additive repository capability. The reset
// must atomically compare its snapshot version and remove every selected key.
type VersionedSettingRepository interface {
	GetAllVersioned(context.Context) (map[string]string, string, error)
	ResetOverrides(context.Context, []string, string) error
}

type SettingEffectiveValue struct {
	Key             string   `json:"key"`
	Group           string   `json:"group"`
	EffectiveValue  any      `json:"effective_value"`
	Source          string   `json:"source"`
	OverrideState   string   `json:"override_state"`
	DefaultValue    any      `json:"default_value"`
	DefaultVersion  string   `json:"default_version"`
	SettingVersion  string   `json:"setting_version"`
	AppliedVersion  string   `json:"applied_version"`
	RestartRequired bool     `json:"restart_required"`
	CanReset        bool     `json:"can_reset"`
	Sensitive       bool     `json:"sensitive"`
	Configured      bool     `json:"configured"`
	KeyID           string   `json:"key_id,omitempty"`
	ValidationState string   `json:"validation_state"`
	BlockedBy       []string `json:"blocked_by"`
}

type EffectiveSettings struct {
	SettingVersion string                  `json:"setting_version"`
	Items          []SettingEffectiveValue `json:"items"`
	ResetGroups    []string                `json:"reset_groups"`
}

type ResetSettingOverridesRequest struct {
	ExpectedVersion string   `json:"expected_version"`
	Groups          []string `json:"groups"`
	Keys            []string `json:"keys"`
}

type settingDefinition struct {
	key, configPath, group string
	defaultValue           any
	sensitive, canReset    bool
}

var oauthResetGroups = []string{"dingtalk_connect", "github_oauth", "google_oauth", "linuxdo_connect", "oidc_connect", "wechat_connect"}

func settingFieldName(value string) string {
	return strings.ToLower(strings.ReplaceAll(value, "_", ""))
}

func settingsViewFields(view *SystemSettings) map[string]any {
	result := make(map[string]any)
	v := reflect.ValueOf(view).Elem()
	t := v.Type()
	for index := 0; index < v.NumField(); index++ {
		result[settingFieldName(t.Field(index).Name)] = v.Field(index).Interface()
	}
	return result
}

func (s *SettingService) settingDefinitions() []settingDefinition {
	viewFields := settingsViewFields(&SystemSettings{})
	definitions := []settingDefinition{
		{SettingKeyModelPlazaEnabled, "", "catalog", DefaultModelPlazaEnabled, false, true},
		{SettingKeyModelPlazaRequireAuth, "", "catalog", DefaultModelPlazaRequireAuth, false, true},
		{SettingKeyModelPlazaDescription, "", "catalog", "", false, true},
	}
	for _, path := range config.OperationalDefaultKeys() {
		def, _ := config.OperationalDefault(path)
		group, _, _ := strings.Cut(path, ".")
		key := strings.ReplaceAll(path, ".", "_")
		if path == "default.user_balance" {
			key = SettingKeyDefaultBalance
		}
		if path == "default.user_concurrency" {
			key = SettingKeyDefaultConcurrency
		}
		_, hasView := viewFields[settingFieldName(key)]
		canReset := hasView && (isOAuthResetGroup(group) || group == "default")
		sensitive := strings.Contains(path, "secret") || strings.HasSuffix(path, "encryption_key")
		definitions = append(definitions, settingDefinition{key, path, group, def, sensitive, canReset})
	}
	return definitions
}

func isOAuthResetGroup(group string) bool {
	for _, candidate := range oauthResetGroups {
		if candidate == group {
			return true
		}
	}
	return false
}

func configValueAt(cfg *config.Config, path string) (any, bool) {
	if cfg == nil {
		return nil, false
	}
	v := reflect.ValueOf(cfg).Elem()
	for _, part := range strings.Split(path, ".") {
		if v.Kind() != reflect.Struct {
			return nil, false
		}
		found := false
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).Tag.Get("mapstructure") == part {
				v, found = v.Field(i), true
				break
			}
		}
		if !found || !v.CanInterface() {
			return nil, false
		}
	}
	return v.Interface(), true
}

func (s *SettingService) settingsSnapshot(ctx context.Context) (map[string]string, string, error) {
	if s == nil || s.settingRepo == nil || s.cfg == nil {
		return nil, "", infraerrors.ServiceUnavailable("CONFIG_NOT_READY", "configuration is unavailable")
	}
	if versioned, ok := s.settingRepo.(VersionedSettingRepository); ok {
		return versioned.GetAllVersioned(ctx)
	}
	values, err := s.settingRepo.GetAll(ctx)
	return values, "unversioned", err
}

func (s *SettingService) GetEffectiveSettings(ctx context.Context) (*EffectiveSettings, error) {
	values, version, err := s.settingsSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("read effective settings: %w", err)
	}
	return s.effectiveSettingsFromValues(values, version), nil
}

// Old clients submit the full GET projection. A value that is still inherited
// and unchanged must not silently become a durable override when another field
// is saved. Existing explicit overrides are removed only through reset.
func (s *SettingService) preserveInheritedSettingValues(ctx context.Context, updates map[string]string) error {
	if s == nil || s.cfg == nil || s.settingRepo == nil {
		return infraerrors.ServiceUnavailable("CONFIG_NOT_READY", "configuration is unavailable")
	}
	stored, err := s.settingRepo.GetAll(ctx)
	if err != nil {
		return fmt.Errorf("read settings before update: %w", err)
	}
	effective := settingsViewFields(s.parseSettings(stored))
	for _, def := range s.settingDefinitions() {
		if !def.canReset {
			continue
		}
		if _, exists := stored[def.key]; exists {
			continue
		}
		raw, submitted := updates[def.key]
		value, known := effective[settingFieldName(def.key)]
		if submitted && known && sameEffectiveSettingValue(raw, value) {
			delete(updates, def.key)
		}
	}
	return nil
}

func sameEffectiveSettingValue(raw string, value any) bool {
	switch expected := value.(type) {
	case string:
		return strings.TrimSpace(raw) == strings.TrimSpace(expected)
	case bool:
		return raw == strconv.FormatBool(expected)
	case int:
		parsed, err := strconv.Atoi(raw)
		return err == nil && parsed == expected
	case float64:
		parsed, err := strconv.ParseFloat(raw, 64)
		return err == nil && parsed == expected
	default:
		return false
	}
}

func (s *SettingService) effectiveSettingsFromValues(values map[string]string, version string) *EffectiveSettings {
	view := settingsViewFields(s.parseSettings(values))
	// Endpoint/scopes fields have no DB projection in SystemSettings. Resolve
	// their deployment fallback through the same provider view used at login.
	emailOAuthEndpoints := map[string]map[string]string{}
	for _, provider := range []string{"github", "google"} {
		effective := s.effectiveEmailOAuthConfig(values, provider)
		emailOAuthEndpoints[provider+"_oauth"] = map[string]string{
			"authorize_url": effective.AuthorizeURL, "token_url": effective.TokenURL,
			"userinfo_url": effective.UserInfoURL, "emails_url": effective.EmailsURL, "scopes": effective.Scopes,
		}
	}
	result := &EffectiveSettings{SettingVersion: version, Items: []SettingEffectiveValue{}, ResetGroups: append([]string(nil), oauthResetGroups...)}
	var imageValues map[string]any
	imageErr := error(nil)
	if raw, ok := values[settingKeyImageStorageConfig]; ok && strings.TrimSpace(raw) != "" {
		imageErr = json.Unmarshal([]byte(raw), &imageValues)
	}
	for _, def := range s.settingDefinitions() {
		item := SettingEffectiveValue{Key: def.key, Group: def.group, EffectiveValue: def.defaultValue,
			Source: "registry_default", OverrideState: "inherit", DefaultValue: def.defaultValue,
			DefaultVersion: config.SettingsDefaultVersion, SettingVersion: version, AppliedVersion: version,
			RestartRequired: def.configPath != "" && !def.canReset && def.group != "image_storage",
			CanReset:        def.canReset, Sensitive: def.sensitive, ValidationState: "valid", BlockedBy: []string{}}
		if def.configPath != "" {
			if value, ok := configValueAt(s.cfg, def.configPath); ok {
				item.EffectiveValue = value
			}
			item.Source = s.cfg.SourceFor(def.configPath)
		}
		if endpoints, ok := emailOAuthEndpoints[def.group]; ok && !def.canReset {
			if value, exists := endpoints[strings.TrimPrefix(def.configPath, def.group+".")]; exists {
				if !reflect.DeepEqual(item.EffectiveValue, value) {
					item.Source = "derived_configuration"
					if reflect.DeepEqual(value, def.defaultValue) {
						item.Source = "registry_default"
					}
				}
				item.EffectiveValue = value
			}
		}
		if def.canReset {
			if value, ok := view[settingFieldName(def.key)]; ok {
				item.EffectiveValue = value
			}
			if raw, exists := values[def.key]; exists {
				item.OverrideState = "set"
				item.Source = "database_override"
				if def.key == SettingKeyModelPlazaEnabled && values[settingModelPlazaSourceKey] == "migration_snapshot" {
					item.Source = "migration_snapshot"
				}
				if _, isString := def.defaultValue.(string); isString && strings.TrimSpace(raw) == "" && def.configPath != "" && !strings.Contains(def.key, "userinfo_") {
					item.Source, item.OverrideState = s.cfg.SourceFor(def.configPath), "legacy_empty_inherit"
				}
				if _, isBool := def.defaultValue.(bool); isBool && raw != "true" && raw != "false" {
					item.ValidationState, item.BlockedBy = "invalid", []string{"invalid_boolean_override"}
				}
			}
			if item.OverrideState != "set" && def.configPath != "" {
				if inherited, ok := configValueAt(s.cfg, def.configPath); ok && !reflect.DeepEqual(inherited, item.EffectiveValue) {
					if reflect.DeepEqual(item.EffectiveValue, def.defaultValue) {
						item.Source = "registry_default"
					} else {
						item.Source = "derived_configuration"
					}
				}
			}
		}
		if def.group == "image_storage" {
			field := strings.TrimPrefix(def.configPath, "image_storage.")
			if imageErr != nil {
				item.EffectiveValue, item.Source, item.ValidationState = nil, "database_override", "invalid"
				item.BlockedBy = []string{"image_storage_config_corrupt"}
			} else if imageValues != nil {
				item.Source, item.OverrideState = "database_override", "set"
				value, present := imageValues[field]
				if !present {
					value = reflect.Zero(reflect.TypeOf(def.defaultValue)).Interface()
				}
				item.EffectiveValue = value
				bucket, _ := imageValues["bucket"].(string)
				if reuse, _ := imageValues["reuse_backup_s3"].(bool); reuse && (field == "endpoint" || field == "region" || field == "access_key_id" || field == "secret_access_key" || field == "force_path_style" || (field == "bucket" && bucket == "")) {
					var backup map[string]any
					if err := json.Unmarshal([]byte(values[settingKeyBackupS3Config]), &backup); err == nil {
						item.EffectiveValue, item.Source = backup[field], "credential_reference"
					} else {
						item.EffectiveValue, item.ValidationState = nil, "unavailable"
						item.BlockedBy = []string{"backup_credentials_unavailable"}
					}
				}
			}
			item.AppliedVersion = "not_observed"
		}
		if def.configPath == "turnstile.required" && s.cfg.Server.Mode == "release" && s.cfg.Turnstile.Required &&
			values[SettingKeyTurnstileEnabled] != "true" && values[SettingKeyTencentCaptchaEnabled] != "true" && values[SettingKeyAliyunCaptchaEnabled] != "true" {
			item.ValidationState, item.BlockedBy = "invalid", []string{"required_captcha_provider_missing"}
		}
		if item.Sensitive {
			text, _ := item.EffectiveValue.(string)
			item.Configured = strings.TrimSpace(text) != ""
			if def.group == "image_storage" && item.OverrideState == "set" {
				item.KeyID = settingSecretKeyID(text)
			}
			if def.configPath == "secrets.encryption_key" && item.Configured {
				item.KeyID = newSettingSecretEncryptor(s.cfg, nil).keyID
			}
			if def.configPath == "totp.encryption_key" {
				item.Configured = s.cfg.Totp.EncryptionKeyConfigured
				if item.Configured {
					item.KeyID = "legacy_totp"
				} else if text != "" {
					item.Source = "generated_ephemeral"
					item.ValidationState, item.BlockedBy = "unavailable", []string{"persistent_encryption_key_missing"}
				}
			}
			item.EffectiveValue, item.DefaultValue = nil, nil
		}
		result.Items = append(result.Items, item)
	}
	for _, runtime := range []struct {
		key, group string
		enabled    bool
		blocked    bool
		reason     string
	}{
		{SettingKeyPasskeyEnabled, "webauthn", s.passkeySettingEnabled(values), !s.passkeyConfigured(), "webauthn_deployment_disabled"},
		{SettingKeyOpsMonitoringEnabled, "ops", s.cfg.Ops.Enabled && parseOpsMonitoringEnabled(values[SettingKeyOpsMonitoringEnabled]), !s.cfg.Ops.Enabled, "ops_deployment_disabled"},
	} {
		item := SettingEffectiveValue{Key: runtime.key, Group: runtime.group, EffectiveValue: runtime.enabled,
			Source: "registry_default", OverrideState: "inherit", DefaultValue: true, DefaultVersion: config.SettingsDefaultVersion,
			SettingVersion: version, AppliedVersion: version, ValidationState: "valid", BlockedBy: []string{}}
		if _, exists := values[runtime.key]; exists {
			item.Source, item.OverrideState = "database_override", "set"
		}
		if runtime.blocked {
			item.Source, item.BlockedBy = "derived_constraint", []string{runtime.reason}
		}
		if runtime.group == "ops" {
			item.AppliedVersion = "not_observed"
		}
		result.Items = append(result.Items, item)
	}
	return result
}

func (s *SettingService) ResetSettingOverrides(ctx context.Context, req ResetSettingOverridesRequest) (*EffectiveSettings, error) {
	if strings.TrimSpace(req.ExpectedVersion) == "" || req.ExpectedVersion == "unversioned" {
		return nil, infraerrors.BadRequest("SETTING_VERSION_REQUIRED", "an effective settings version is required")
	}
	values, version, err := s.settingsSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	if version != req.ExpectedVersion {
		return nil, ErrSettingVersionConflict
	}
	repo, ok := s.settingRepo.(VersionedSettingRepository)
	if !ok {
		return nil, infraerrors.ServiceUnavailable("SETTING_RESET_UNAVAILABLE", "atomic settings reset is unavailable")
	}
	groups, keys := map[string]bool{}, map[string]bool{}
	for _, group := range req.Groups {
		if !isOAuthResetGroup(group) {
			return nil, infraerrors.BadRequest("INVALID_SETTING_GROUP", "unsupported reset group")
		}
		groups[group] = true
	}
	definitions := s.settingDefinitions()
	allowed := map[string]settingDefinition{}
	for _, def := range definitions {
		if def.canReset {
			allowed[def.key] = def
		}
		if def.canReset && groups[def.group] {
			keys[def.key] = true
		}
	}
	for _, key := range req.Keys {
		def, ok := allowed[key]
		if !ok {
			return nil, infraerrors.BadRequest("INVALID_SETTING_KEY", "the setting cannot be reset through this endpoint")
		}
		if isOAuthResetGroup(def.group) {
			return nil, infraerrors.BadRequest("SETTING_GROUP_RESET_REQUIRED", "OAuth settings must be reset as a complete provider group")
		}
		keys[key] = true
	}
	if len(keys) == 0 {
		return nil, infraerrors.BadRequest("SETTING_RESET_EMPTY", "select at least one supported setting or OAuth provider group")
	}
	if keys[SettingKeyModelPlazaEnabled] {
		keys[settingModelPlazaSourceKey] = true
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
		delete(values, key)
	}
	sort.Strings(ordered)
	if err := repo.ResetOverrides(ctx, ordered, req.ExpectedVersion); err != nil {
		return nil, err
	}
	s.refreshCachedSettings(s.parseSettings(values))
	return s.GetEffectiveSettings(ctx)
}
