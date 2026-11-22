package service

import "strings"

const (
	DefaultModelPlazaEnabled     = false
	DefaultModelPlazaRequireAuth = false
	settingModelPlazaSourceKey   = "model_plaza_enabled_source"
)

// ModelPlazaRuntimeFromValues is shared by v1, v2, public and admin projections.
// Existing installations with an implicit v2 enablement are snapshotted by the
// additive upgrade migration; a missing key always means the new-install default.
func ModelPlazaRuntimeFromValues(values map[string]string) ModelPlazaRuntime {
	enabled, requireAuth := DefaultModelPlazaEnabled, DefaultModelPlazaRequireAuth
	if value, exists := values[SettingKeyModelPlazaEnabled]; exists {
		enabled = strings.TrimSpace(value) == "true"
	}
	if value, exists := values[SettingKeyModelPlazaRequireAuth]; exists {
		requireAuth = strings.TrimSpace(value) == "true"
	}
	return ModelPlazaRuntime{
		Enabled: enabled, RequireAuth: requireAuth,
		Description: values[SettingKeyModelPlazaDescription],
	}
}
