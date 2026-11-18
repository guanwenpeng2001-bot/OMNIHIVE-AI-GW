package service

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStagedImportFingerprintEndpointAndUpstreamIdentity(t *testing.T) {
	base := map[string]any{"api_key": "shared-fixture-key", "base_url": "https://notebook.example.invalid", "account_mode": "payg"}
	fingerprint := func(credentials map[string]any) [32]byte {
		digest, err := StagedAccountImportFingerprint("kaggle", AccountTypeAPIKey, credentials)
		require.NoError(t, err)
		return digest
	}
	original := fingerprint(base)
	for _, endpoint := range []string{"https://notebook.example.invalid/", "HTTPS://NOTEBOOK.EXAMPLE.INVALID:443/v1/", "https://notebook.example.invalid/v1"} {
		copy := maps.Clone(base)
		copy["base_url"] = endpoint
		require.Equal(t, original, fingerprint(copy), "equivalent effective Kaggle endpoints must deduplicate")
	}
	for _, endpoint := range []string{"https://other.example.invalid", "http://notebook.example.invalid", "https://notebook.example.invalid:8443", "https://notebook.example.invalid/tenant-b"} {
		copy := maps.Clone(base)
		copy["base_url"] = endpoint
		require.NotEqual(t, original, fingerprint(copy))
	}
	for _, field := range []string{"tenant_id", "tenant", "project_id", "project", "organization_id", "org_id", "antigravity_project_id", "zhipu_project", "account_id", "region"} {
		t.Run(field, func(t *testing.T) {
			copy := maps.Clone(base)
			copy[field] = "identity-a"
			first := fingerprint(copy)
			require.NotEqual(t, original, first)
			copy[field] = " identity-a "
			require.Equal(t, first, fingerprint(copy))
			copy[field] = "identity-b"
			require.NotEqual(t, first, fingerprint(copy))
		})
	}
	copy := maps.Clone(base)
	copy["project_id"] = ""
	copy["model_mapping"] = map[string]any{"different-alias": "different-model"}
	copy["api_protocol"] = APIProtocolChatCompletions
	copy["email"] = "different-label@example.invalid"
	copy["expires_at"] = "42"
	require.Equal(t, original, fingerprint(copy), "routing/display options must not bypass duplicate detection")
	copy = maps.Clone(base)
	copy["base_url"] = "https://unused.example.invalid"
	copy["api_base_urls"] = map[string]string{APIProtocolChatCompletions: "https://notebook.example.invalid:443/v1/"}
	require.Equal(t, original, fingerprint(copy), "use effective per-protocol endpoints")
	copy["api_base_urls"] = map[string]any{APIProtocolChatCompletions: "https://second.example.invalid/v1"}
	require.NotEqual(t, original, fingerprint(copy))
	first, err := normalizedStagedEndpoint("https://host.example.invalid/a%2Fb")
	require.NoError(t, err)
	second, err := normalizedStagedEndpoint("https://host.example.invalid/a/b")
	require.NoError(t, err)
	require.NotEqual(t, first, second, "escaped path separators must not collapse distinct endpoints")
	upper, err := normalizedStagedEndpoint("https://host.example.invalid/A")
	require.NoError(t, err)
	lower, err := normalizedStagedEndpoint("https://host.example.invalid/a")
	require.NoError(t, err)
	require.NotEqual(t, upper, lower)
}

func TestStagedImportModelAndStaticConfigurationValidation(t *testing.T) {
	valid := map[string]any{
		"api_key": "fixture-key", "model_mapping": map[string]any{"qwen-*": "Qwen/Qwen3.5-9B"},
		"compact_model_mapping": map[string]string{"short": "compact-model"},
		"account_mode":          "payg", "api_protocol": "chat_completions", "billing_plan": "payg",
		"api_base_urls":       map[string]any{"chat_completions": "https://example.invalid/v1"},
		"protocol_rules":      []any{map[string]any{"pattern": "claude-*", "protocol": "anthropic"}},
		"openai_capabilities": []any{"chat_completions", "embeddings"},
		"pool_mode":           true, "pool_mode_retry_count": float64(10), "pool_mode_retry_status_codes": []any{float64(429), float64(503)},
		"custom_error_codes_enabled": true, "custom_error_codes": []any{float64(401)},
		"account_scheduling_threshold": float64(90), "intercept_warmup_requests": false,
	}
	require.NoError(t, ValidateStagedImportCredentials(valid))
	for index, tc := range []struct {
		field string
		value any
	}{
		{"model_mapping", []any{"model"}}, {"model_mapping", map[string]any{"model": true}},
		{"model_mapping", map[string]any{"model": map[string]any{"proxy_id": 1}}},
		{"model_mapping", map[string]string{"model": ""}}, {"model_mapping", map[string]string{"model": "line\nbreak"}},
		{"model_mapping", map[string]string{"model": strings.Repeat("x", maxStagedModelNameBytes+1)}},
		{"compact_model_mapping", map[string]any{"model": 12}}, {"api_protocol", "unknown"}, {"account_mode", "unknown"},
		{"api_base_urls", map[string]any{"proxy_key": "https://example.invalid"}},
		{"api_base_urls", map[string]any{"chat_completions": "https://example.invalid?token=fixture"}},
		{"protocol_rules", []any{map[string]any{"pattern": "*", "protocol": "anthropic", "group_id": 1}}},
		{"protocol_rules", []any{map[string]any{"pattern": "a\nb", "protocol": "anthropic"}}},
		{"openai_capabilities", []any{"unsupported"}}, {"openai_capabilities", nil},
		{"pool_mode", "true"}, {"pool_mode_retry_count", 11}, {"pool_mode_retry_count", 1.5},
		{"account_scheduling_threshold", 0}, {"account_scheduling_threshold", 101},
		{"custom_error_codes", []any{200}}, {"tenant_id", map[string]any{"group_id": 4}},
		{"project_id", strings.Repeat("x", 257)}, {"proxy_id", 1}, {"group_ids", []any{1}},
		{"temp_unschedulable_rules", []any{}}, {"header_overrides", map[string]any{"x-secret": "fixture"}},
	} {
		t.Run(fmt.Sprintf("%s/%d", tc.field, index), func(t *testing.T) {
			credentials := maps.Clone(valid)
			credentials[tc.field] = tc.value
			require.Error(t, ValidateStagedImportCredentials(credentials))
		})
	}
	tooMany := make(map[string]string)
	for i := 0; i <= maxStagedModelMappings; i++ {
		tooMany[fmt.Sprint(i)] = "model"
	}
	require.Error(t, ValidateStagedImportCredentials(map[string]any{"api_key": "fixture", "model_mapping": tooMany}))
}

func TestStagedImportExtraAllowsOnlyStaticRoutingChoices(t *testing.T) {
	require.NoError(t, ValidateStagedImportExtra(map[string]any{
		"custom_base_url_enabled": true, "openai_passthrough": false, "openai_oauth_passthrough": false,
		"anthropic_passthrough": false, "mixed_scheduling": false,
		"openai_compact_mode": "force_off", "openai_responses_mode": "force_chat_completions",
	}))
	for _, field := range []string{"upstream_model_catalog", "upstream_model_allowlist", "model_capability_defaults", "model_capability_release", "model_availability", "upstream_billing_probe", "upstream_billing_probe_enabled", "quota_used", "group_ids", "proxy_id", StagedProxyDependencyExtraKey} {
		require.Error(t, ValidateStagedImportExtra(map[string]any{field: true}))
	}
	require.Error(t, ValidateStagedImportExtra(map[string]any{"openai_compact_mode": "supported"}))
	require.Error(t, ValidateStagedImportExtra(map[string]any{"mixed_scheduling": "true"}))
}
