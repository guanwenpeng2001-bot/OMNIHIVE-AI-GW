package config

import (
	"os"
	"strings"

	"github.com/spf13/viper"
)

const (
	DefaultModelCatalogRefreshEnabled = false
	DefaultLogFormat                  = "json"
	DefaultUserConcurrency            = 5
	DefaultUserBalance                = 0
	SettingsDefaultVersion            = "v0.3.5"
)

// captureConfigurationSources records provenance at load time. Diagnostics must
// not re-read environment variables or Viper after the running config is frozen.
func captureConfigurationSources() map[string]string {
	sources := make(map[string]string)
	for _, key := range viper.AllKeys() {
		source := "registry_default"
		if viper.InConfig(key) {
			source = "yaml"
		}
		envKey := strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
		if value, ok := os.LookupEnv(envKey); ok && value != "" {
			source = "environment"
		}
		sources[key] = source
	}
	if value := os.Getenv("TZ"); strings.TrimSpace(value) != "" {
		sources["timezone"] = "environment"
	}
	if value := os.Getenv("ENABLE_SERVER_TIMING"); value != "" {
		sources["server.enable_server_timing"] = "environment"
	}
	return sources
}

// SourceFor describes the immutable deployment input actually used by Load.
// Hand-constructed Config values have no proven file/environment provenance.
func (c *Config) SourceFor(key string) string {
	if c == nil {
		return "registry_default"
	}
	if source, ok := c.sources[key]; ok {
		return source
	}
	return "configuration"
}
