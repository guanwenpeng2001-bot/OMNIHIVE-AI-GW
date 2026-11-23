package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type EffectiveSettingState string

const (
	EffectiveSettingStateEffective         EffectiveSettingState = "effective"
	EffectiveSettingStateInvalid           EffectiveSettingState = "invalid"
	EffectiveSettingStateUnavailable       EffectiveSettingState = "unavailable"
	EffectiveSettingStateBlockedByHardGate EffectiveSettingState = "blocked_by_hard_gate"
)

type EffectiveSetting struct {
	Key               string
	Group             string
	ValueType         SettingValueType
	Value             any
	Source            string
	Authority         SettingAuthority
	FallbackChain     []string
	RestartRequired   bool
	Resettable        bool
	Sensitive         bool
	State             EffectiveSettingState
	Configured        *bool
	BlockedBy         string
	InstallSeedSource string
	Baseline          any
	HasBaseline       bool
	ErrorCode         string
}

type EffectiveSettingsPage struct {
	ConfigFile  string
	GeneratedAt time.Time
	Items       []EffectiveSetting
	NextCursor  string
}

type EffectiveSettingsFilter struct {
	Group       string
	Query       string
	AfterCursor string
	Limit       int
	Source      string
	State       EffectiveSettingState
}

// EffectiveSettings resolves all registered settings without exposing secrets.
// A storage failure is reported per item, never converted into a healthy default.
func (s *SettingService) EffectiveSettings(ctx context.Context, filter EffectiveSettingsFilter) (*EffectiveSettingsPage, error) {
	if s == nil || s.settingRepo == nil {
		return nil, errors.New("setting repository not initialized")
	}
	if filter.Limit < 0 {
		return nil, fmt.Errorf("limit must be between 1 and 200")
	}
	if filter.Limit == 0 {
		filter.Limit = 50
	}
	if filter.Limit > 200 {
		return nil, fmt.Errorf("limit must be between 1 and 200")
	}
	stored, readErr := s.settingRepo.GetAll(ctx)
	if readErr != nil {
		stored = map[string]string{}
	}
	items := make([]EffectiveSetting, 0)
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	for _, registration := range s.SettingRegistry() {
		if filter.Group != "" && registration.Group != filter.Group {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(registration.Key), query) && !strings.Contains(strings.ToLower(registration.Group), query) {
			continue
		}
		if filter.AfterCursor != "" && registration.Key <= filter.AfterCursor {
			continue
		}
		item := s.resolveEffectiveSetting(registration, stored, readErr)
		if item.State == EffectiveSettingStateInvalid {
			item.ErrorCode = "CONFIG_INVALID"
		} else if item.State == EffectiveSettingStateUnavailable {
			item.ErrorCode = "CONFIG_UNAVAILABLE"
		}
		if strings.HasSuffix(registration.Key, "_install_seed_source") {
			item.InstallSeedSource = fmt.Sprint(item.Value)
			item.Value = nil
		} else {
			item.InstallSeedSource = installSeedSource(registration.Key)
		}
		s.applySecurityGate(&item, registration, stored, readErr)
		if filter.Source != "" && item.Source != filter.Source {
			continue
		}
		if filter.State != "" && item.State != filter.State {
			continue
		}
		items = append(items, item)
	}
	items = s.appendDerivedSecurityGates(s.registryEffectiveSettings(stored, readErr), filter, readErr)
	if filter.Source == "derived" {
		derived := make([]EffectiveSetting, 0, 3)
		for _, item := range items {
			if item.Source == "derived" {
				derived = append(derived, item)
			}
		}
		items = derived
	}
	page := &EffectiveSettingsPage{ConfigFile: s.ConfigFile(), GeneratedAt: time.Now().UTC(), Items: items}
	sort.Slice(page.Items, func(left, right int) bool { return page.Items[left].Key < page.Items[right].Key })
	if len(items) > filter.Limit {
		page.Items = items[:filter.Limit]
		page.NextCursor = page.Items[len(page.Items)-1].Key
	}
	return page, nil
}

func (s *SettingService) registryEffectiveSettings(stored map[string]string, readErr error) []EffectiveSetting {
	items := make([]EffectiveSetting, 0)
	for _, registration := range s.SettingRegistry() {
		item := s.resolveEffectiveSetting(registration, stored, readErr)
		s.applySecurityGate(&item, registration, stored, readErr)
		items = append(items, item)
	}
	return items
}

func (s *SettingService) appendDerivedSecurityGates(items []EffectiveSetting, filter EffectiveSettingsFilter, readErr error) []EffectiveSetting {
	values := map[string]EffectiveSetting{}
	for _, item := range items {
		values[item.Key] = item
	}
	passkey := EffectiveSetting{Key: "passkey.effective_enabled", Group: "security", ValueType: SettingValueBool, Source: "derived", Authority: SettingAuthorityDeployment, FallbackChain: []string{"derived"}, State: EffectiveSettingStateEffective}
	webauthn, webauthnOK := values["webauthn.enabled"]
	softPasskey, passkeyOK := values[SettingKeyPasskeyEnabled]
	passkey.Value = webauthnOK && webauthn.Value == true && passkeyOK && softPasskey.Value == true && softPasskey.State == EffectiveSettingStateEffective
	if readErr != nil {
		passkey.Value = false
		passkey.State = EffectiveSettingStateUnavailable
		passkey.ErrorCode = "CONFIG_UNAVAILABLE"
	} else if passkey.Value != true {
		passkey.State = EffectiveSettingStateBlockedByHardGate
		if webauthnOK && webauthn.Value != true {
			passkey.BlockedBy = "webauthn.enabled"
		} else {
			passkey.BlockedBy = SettingKeyPasskeyEnabled
		}
	}
	if (filter.Group == "" || filter.Group == passkey.Group) && (filter.Query == "" || strings.Contains(passkey.Key, strings.ToLower(filter.Query))) && (filter.AfterCursor == "" || passkey.Key > filter.AfterCursor) && (filter.State == "" || filter.State == passkey.State) {
		items = append(items, passkey)
	}

	turnstile := EffectiveSetting{Key: "turnstile.effective_enabled", Group: "security", ValueType: SettingValueBool, Source: "derived", Authority: SettingAuthorityDeployment, FallbackChain: []string{"derived"}, State: EffectiveSettingStateEffective}
	turnstileSoft, turnstileOK := values[SettingKeyTurnstileEnabled]
	turnstileSecret, secretOK := values[SettingKeyTurnstileSecretKey]
	turnstileRequired := s.cfg != nil && s.cfg.Server.Mode == "release" && s.cfg.Turnstile.Required
	turnstile.Value = turnstileOK && turnstileSoft.Value == true && turnstileSoft.State == EffectiveSettingStateEffective && secretOK && turnstileSecret.Configured != nil && *turnstileSecret.Configured
	if readErr != nil {
		turnstile.Value = false
		turnstile.State = EffectiveSettingStateUnavailable
		turnstile.ErrorCode = "CONFIG_UNAVAILABLE"
	} else if turnstileRequired && turnstile.Value != true {
		turnstile.State = EffectiveSettingStateBlockedByHardGate
		turnstile.BlockedBy = "turnstile.required"
		turnstile.ErrorCode = "TURNSTILE_REQUIRED_NOT_CONFIGURED"
	}
	if (filter.Group == "" || filter.Group == turnstile.Group) && (filter.Query == "" || strings.Contains(turnstile.Key, strings.ToLower(filter.Query))) && (filter.AfterCursor == "" || turnstile.Key > filter.AfterCursor) && (filter.State == "" || filter.State == turnstile.State) {
		items = append(items, turnstile)
	}
	ops := EffectiveSetting{Key: "ops.effective_enabled", Group: "ops", ValueType: SettingValueBool, Source: "derived", Authority: SettingAuthorityDeployment, FallbackChain: []string{"derived"}, State: EffectiveSettingStateEffective}
	opsHard, hardOK := values["ops.enabled"]
	opsSoft, softOK := values[SettingKeyOpsMonitoringEnabled]
	ops.Value = hardOK && opsHard.Value == true && softOK && opsSoft.Value == true && opsSoft.State == EffectiveSettingStateEffective
	if readErr != nil {
		ops.Value = false
		ops.State = EffectiveSettingStateUnavailable
		ops.ErrorCode = "CONFIG_UNAVAILABLE"
	} else if ops.Value != true {
		ops.State = EffectiveSettingStateBlockedByHardGate
		if hardOK && opsHard.Value != true {
			ops.BlockedBy = "ops.enabled"
		} else {
			ops.BlockedBy = SettingKeyOpsMonitoringEnabled
		}
	}
	if (filter.Group == "" || filter.Group == ops.Group) && (filter.Query == "" || strings.Contains(ops.Key, strings.ToLower(filter.Query))) && (filter.AfterCursor == "" || ops.Key > filter.AfterCursor) && (filter.State == "" || filter.State == ops.State) {
		return append(items, ops)
	}
	return items
}

func (s *SettingService) EffectiveSettingByKeys(ctx context.Context, keys []string) ([]EffectiveSetting, error) {
	page, err := s.EffectiveSettings(ctx, EffectiveSettingsFilter{Limit: 200})
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		wanted[key] = struct{}{}
	}
	items := make([]EffectiveSetting, 0, len(keys))
	for _, item := range page.Items {
		if _, ok := wanted[item.Key]; ok {
			items = append(items, item)
		}
	}
	return items, nil
}

func (s *SettingService) resolveEffectiveSetting(registration SettingRegistration, stored map[string]string, readErr error) EffectiveSetting {
	item := EffectiveSetting{
		Key: registration.Key, Group: registration.Group, ValueType: registration.ValueType,
		Authority: registration.Authority, RestartRequired: registration.RestartRequired,
		Resettable: registration.Resettable, Sensitive: registration.Sensitive, State: EffectiveSettingStateEffective,
	}
	switch registration.Authority {
	case SettingAuthorityDeployment:
		item.FallbackChain = []string{"environment", "yaml", "default"}
	case SettingAuthorityDatabaseOverride:
		item.FallbackChain = []string{"database", "environment", "yaml", "default"}
	case SettingAuthorityDatabaseOnly:
		item.FallbackChain = []string{"database", "default"}
	default:
		item.FallbackChain = []string{"artifact"}
	}

	raw, databaseConfigured := stored[registration.Key]
	if strings.HasPrefix(registration.Key, "image_storage.") {
		raw, databaseConfigured = stored[settingKeyImageStorageConfig]
		if databaseConfigured {
			var settings ImageStorageSettings
			if err := json.Unmarshal([]byte(raw), &settings); err == nil {
				raw, databaseConfigured = imageStorageFieldRaw(registration.Key, settings)
			} else {
				item.Source = "database"
				item.State = EffectiveSettingStateInvalid
				item.Value = failClosedValue(registration)
				return s.redactEffectiveSetting(item, true)
			}
		}
	}
	if registration.Authority == SettingAuthorityDatabaseOnly || registration.Authority == SettingAuthorityDatabaseOverride {
		if databaseConfigured {
			item.Source = "database"
			value, valid := parseEffectiveValue(raw, registration.ValueType)
			if valid {
				item.Value = value
			} else {
				item.State = EffectiveSettingStateInvalid
				item.Value = failClosedValue(registration)
			}
		}
		if readErr != nil && registration.Authority == SettingAuthorityDatabaseOnly {
			item.Source = "database"
			item.State = EffectiveSettingStateUnavailable
			item.Value = failClosedValue(registration)
		}
		if registration.Authority == SettingAuthorityDatabaseOnly {
			if item.Source == "" {
				item.Source = "default"
				item.Value = registration.Default
			}
			return s.redactEffectiveSetting(item, databaseConfigured)
		}
	}

	baseline, source, valid := s.deploymentBaseline(registration)
	item.Baseline = baseline
	item.HasBaseline = true
	if !databaseConfigured {
		item.Source = source
		item.Value = baseline
		if !valid {
			item.State = EffectiveSettingStateInvalid
			item.Value = failClosedValue(registration)
		}
	}
	return s.redactEffectiveSetting(item, databaseConfigured)
}

func imageStorageFieldRaw(key string, settings ImageStorageSettings) (string, bool) {
	switch key {
	case "image_storage.enabled":
		return strconv.FormatBool(settings.Enabled), true
	case "image_storage.presign_expiry_hours":
		return strconv.Itoa(settings.PresignExpiry), true
	case "image_storage.max_download_bytes":
		return strconv.FormatInt(settings.MaxDownloadBytes, 10), true
	case "image_storage.secret_access_key":
		return settings.SecretAccessKey, true
	case "image_storage.access_key_id":
		return settings.AccessKeyID, true
	case "image_storage.reuse_backup_s3":
		return strconv.FormatBool(settings.ReuseBackupS3), true
	default:
		return "", false
	}
}

func (s *SettingService) redactEffectiveSetting(item EffectiveSetting, databaseConfigured bool) EffectiveSetting {
	if item.Sensitive {
		configured := item.Value != nil && item.Value != "" && item.State != EffectiveSettingStateUnavailable
		if !databaseConfigured && item.HasBaseline {
			configured = item.Baseline != nil && item.Baseline != ""
		}
		item.Configured = &configured
		item.Value = nil
		item.Baseline = nil
		item.HasBaseline = false
	}
	return item
}

func (s *SettingService) applySecurityGate(item *EffectiveSetting, registration SettingRegistration, stored map[string]string, readErr error) {
	switch registration.Key {
	case SettingKeyPasskeyEnabled:
		if s.cfg == nil || !s.cfg.WebAuthn.Enabled {
			item.Value = false
			item.State = EffectiveSettingStateBlockedByHardGate
			item.BlockedBy = "webauthn.enabled"
		} else if readErr != nil {
			item.Value = false
			item.State = EffectiveSettingStateUnavailable
		}
	case SettingKeyOpsMonitoringEnabled:
		if s.cfg != nil && !s.cfg.Ops.Enabled {
			item.Value = false
			item.State = EffectiveSettingStateBlockedByHardGate
			item.BlockedBy = "ops.enabled"
		}
	case "turnstile.required":
		if s.cfg != nil && s.cfg.Server.Mode != "release" {
			item.Value = false
		}
	case SettingKeyTurnstileEnabled:
		required := s.cfg != nil && s.cfg.Server.Mode == "release" && s.cfg.Turnstile.Required
		enabled := item.State == EffectiveSettingStateEffective && item.Value == true
		secretConfigured := strings.TrimSpace(stored[SettingKeyTurnstileSecretKey]) != ""
		if required && !enabled {
			item.State = EffectiveSettingStateBlockedByHardGate
			item.BlockedBy = "turnstile.required"
		}
		if required && (!enabled || !secretConfigured) {
			item.Value = false
		}
	}
}

func parseEffectiveValue(raw string, valueType SettingValueType) (any, bool) {
	switch valueType {
	case SettingValueBool:
		value, err := strconv.ParseBool(strings.TrimSpace(raw))
		return value, err == nil
	case SettingValueInt:
		value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		return value, err == nil
	case SettingValueFloat:
		value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		return value, err == nil && !math.IsNaN(value) && !math.IsInf(value, 0)
	case SettingValueString:
		return raw, true
	case SettingValueStringList:
		var value []string
		if err := json.Unmarshal([]byte(raw), &value); err != nil || value == nil {
			return []string{}, false
		}
		return value, true
	case SettingValueJSON, SettingValueObject:
		var value any
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil || value == nil {
			return nil, false
		}
		if decoder.Decode(&struct{}{}) != io.EOF {
			return nil, false
		}
		return value, true
	default:
		return nil, false
	}
}

func failClosedValue(registration SettingRegistration) any {
	if registration.ValueType == SettingValueBool {
		return false
	}
	if registration.ErrorPolicy == SettingErrorFailClosed {
		return zeroSettingValue(registration.ValueType)
	}
	return registration.Default
}

func (s *SettingService) deploymentBaseline(registration SettingRegistration) (value any, source string, valid bool) {
	value = registration.Default
	source = "default"
	valid = true
	if raw, ok := os.LookupEnv(registration.EnvironmentKey); registration.EnvironmentKey != "" && ok {
		value, valid = parseEffectiveValue(raw, registration.ValueType)
		return value, "environment", valid
	}
	if loaded, exists := configField(s.cfg, registration.ConfigField); exists {
		value = loaded
	}
	configPath := strings.TrimSpace(os.Getenv("CONFIG_FILE"))
	if configPath == "" {
		return value, source, valid
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		return value, source, false
	}
	var document map[string]any
	if err := yaml.Unmarshal(content, &document); err != nil {
		return value, source, false
	}
	path := registration.Key
	if registration.Group == "image_storage" {
		path = "image_storage." + strings.TrimPrefix(registration.Key, "image_storage.")
	} else if registration.Authority == SettingAuthorityDatabaseOverride {
		path = registration.Group + "." + strings.TrimPrefix(registration.Key, registration.Group+"_")
	}
	if registration.Key == "oidc_connect_userinfo_url" {
		path = "oidc_connect.userinfo_url"
	}
	if strings.HasPrefix(registration.Key, "github_oauth_") {
		path = "github_oauth." + strings.TrimPrefix(registration.Key, "github_oauth_")
	}
	if strings.HasPrefix(registration.Key, "google_oauth_") {
		path = "google_oauth." + strings.TrimPrefix(registration.Key, "google_oauth_")
	}
	var current any = document
	for _, component := range strings.Split(path, ".") {
		mapping, ok := current.(map[string]any)
		if !ok {
			return value, "default", true
		}
		current, ok = mapping[component]
		if !ok {
			return value, "default", true
		}
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		return registration.Default, "yaml", false
	}
	if registration.ValueType == SettingValueString {
		text, ok := current.(string)
		return text, "yaml", ok
	}
	parsed, ok := parseEffectiveValue(string(encoded), registration.ValueType)
	return parsed, "yaml", ok
}

func (s *SettingService) ConfigFile() string {
	path := strings.TrimSpace(os.Getenv("CONFIG_FILE"))
	if path == "" {
		return ""
	}
	if absolute, err := filepath.Abs(path); err == nil {
		return absolute
	}
	return path
}
