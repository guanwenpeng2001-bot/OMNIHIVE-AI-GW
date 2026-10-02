package migrations

import (
	"github.com/stretchr/testify/require"
	"regexp"
	"strings"
	"testing"
)

func TestOpenRouterMigrationExpandsExistingConstraints(t *testing.T) {
	body, err := FS.ReadFile("282_openrouter_platform.sql")
	require.NoError(t, err)
	sql := string(body)
	require.NotContains(t, strings.ToUpper(sql), "DELETE FROM")
	require.NotContains(t, strings.ToUpper(sql), "UPDATE ")
	prior := map[string]string{"user_platform_quotas_platform_check": "266_hosted_account_identity.sql", "composite_model_routes_target_platform_check": "263_bedrock_platform.sql", "channel_monitors_provider_check": "264_platform_monitor_storage.sql", "channel_monitor_request_templates_provider_check": "264_platform_monitor_storage.sql"}
	pattern := func(constraint string) *regexp.Regexp {
		return regexp.MustCompile(`(?s)ADD CONSTRAINT ` + constraint + `\s+CHECK\s*\([^;]+`)
	}
	valuePattern := regexp.MustCompile(`'([^']+)'`)
	for name, file := range prior {
		old, err := FS.ReadFile(file)
		require.NoError(t, err)
		before := pattern(name).FindString(string(old))
		after := pattern(name).FindString(sql)
		require.NotEmpty(t, before)
		require.NotEmpty(t, after)
		for _, value := range valuePattern.FindAllString(before, -1) {
			require.Contains(t, after, value)
		}
		require.Contains(t, after, "'openrouter'")
		require.NotContains(t, after, "'unknown'")
	}
}
