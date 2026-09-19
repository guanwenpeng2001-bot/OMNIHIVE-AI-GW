package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBoundedModelProbesConfiguration(test *testing.T) {
	for _, scenario := range []struct {
		name, yaml, environment string
		enabled                 bool
	}{
		{"default", "", "", false},
		{"YAML enabled", "bounded_model_probes_enabled: true\n", "", true},
		{"environment enabled", "", "true", true},
		{"environment disables YAML", "bounded_model_probes_enabled: true\n", "false", false},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			resetViperWithJWTSecret(test)
			directory := test.TempDir()
			test.Setenv("DATA_DIR", directory)
			test.Setenv("BOUNDED_MODEL_PROBES_ENABLED", scenario.environment)
			require.NoError(test, os.WriteFile(filepath.Join(directory, "config.yaml"), []byte(scenario.yaml), 0600))
			configuration, err := Load()
			require.NoError(test, err)
			require.Equal(test, scenario.enabled, configuration.BoundedModelProbesEnabled)
		})
	}
}
