package migrations_test

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

const compatibleIdentityMigration = "283_compatible_provider_identities.sql"

// Frozen from the JSON records at c4adf6e1f, 701682bda, 6e7765f6c and 40a222e87.
// This remains independent of the embedded registry so an empty F1 base cannot
// make the migration assertion vacuously pass.
const compatibleIdentityIDs = `a6api aicodemirror aicodewith aicoding aigocode aihubmix amux apikey_fun apinebula astron_coding atlascloud azure_openai bailing byteplus ccsub cherryin claudeapi claudecn code0 code_9527 compshare compshare_coding crazyrouter crazyrouter_intl cubence dmxapi dmxapi_com dmxapi_ssvip eflowcode fennoai fluxa jiekou kat_coder longcat micu mimo mimo_token_cn modelscope novita nvidia packycode pateway pipellm ppio qiniu qwencloud qwencloud_maas_token qwencloud_token relaxycode rightcode runapi shengsuanyun siliconflow_cn siliconflow_global soleapi soshow sssaicode stepfun_api stepfun_api_intl stepfun_plan stepfun_plan_intl subrouter sudocode_chat sudocode_us teamorouter teamorouter_com therouter together xycai zetaapi`

var compatibleIdentityConstraints = []struct{ table, column, name string }{
	{"user_platform_quotas", "platform", "user_platform_quotas_platform_check"},
	{"composite_model_routes", "target_platform", "composite_model_routes_target_platform_check"},
	{"channel_monitors", "provider", "channel_monitors_provider_check"},
	{"channel_monitor_request_templates", "provider", "channel_monitor_request_templates_provider_check"},
}

func compatibleIdentitySQL(test *testing.T, file string) string {
	test.Helper()
	body, err := migrations.FS.ReadFile(file)
	require.NoError(test, err)
	return string(body)
}

func compatibleIdentityValues(test *testing.T, body, constraint string) []string {
	test.Helper()
	pattern := regexp.MustCompile(`(?s)ADD CONSTRAINT ` + regexp.QuoteMeta(constraint) + `\s+CHECK\s*\(\w+ IN \(([^;]+)\)\);`)
	match := pattern.FindStringSubmatch(body)
	require.Len(test, match, 2, constraint)
	var values []string
	for _, quoted := range regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(match[1], -1) {
		values = append(values, quoted[1])
	}
	return values
}

func TestCompatibleProviderIdentityMigrationFiniteExpansion(test *testing.T) {
	ids := strings.Fields(compatibleIdentityIDs)
	require.Len(test, ids, 70)
	require.Equal(test, "08d898819f0f5deb675aa726a5eda853187229041fc33a5f0a1f7d1d5a81f7ad", fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(ids, "\n")+"\n"))))
	body := compatibleIdentitySQL(test, compatibleIdentityMigration)
	prior := compatibleIdentitySQL(test, "282_openrouter_platform.sql")
	native := strings.Fields("anthropic openai gemini antigravity grok kimi zhipu deepseek minimax opencode_go cursor qianwen commandcode qianfan hunyuan mistral openrouter bedrock volcengine")
	// Only the eight intended DDL statements may occur: no data mutation or
	// unrelated schema changes can hide alongside an otherwise correct list.
	remaining := regexp.MustCompile(`(?m)--[^\n]*`).ReplaceAllString(body, "")
	for _, constraint := range compatibleIdentityConstraints {
		test.Run(constraint.table, func(test *testing.T) {
			before := compatibleIdentityValues(test, prior, constraint.name)
			expectedNative := append([]string{}, native...)
			if constraint.table == "user_platform_quotas" {
				expectedNative = append(expectedNative, "vertex")
			}
			require.ElementsMatch(test, expectedNative, before)
			after := compatibleIdentityValues(test, body, constraint.name)
			require.ElementsMatch(test, append(before, ids...), after)
			for _, platform := range domain.ConcretePlatforms() {
				require.Contains(test, after, platform, "current domain platform must be writable")
			}
		})
		drop := `ALTER TABLE ` + constraint.table + `\s+DROP CONSTRAINT IF EXISTS ` + constraint.name + `;`
		add := `(?s)ALTER TABLE ` + constraint.table + `\s+ADD CONSTRAINT ` + constraint.name + `\s+CHECK \(` + constraint.column + ` IN \(\s*'[a-z0-9_]+'(?:\s*,\s*'[a-z0-9_]+')*\s*\)\);`
		for _, pattern := range []string{drop, add} {
			expression := regexp.MustCompile(pattern)
			require.Len(test, expression.FindAllString(remaining, -1), 1)
			remaining = expression.ReplaceAllString(remaining, "")
		}
	}
	require.Empty(test, strings.TrimSpace(remaining))
	// Partial registry check is meaningful on this F1 branch; once all batches
	// are integrated, require exact combined-corpus parity as well.
	var embedded []string
	for _, provider := range domain.CompatibleProviders() {
		require.Contains(test, ids, provider.ID)
		embedded = append(embedded, provider.ID)
	}
	if len(embedded) == len(ids) {
		require.ElementsMatch(test, ids, embedded)
	}
	test.Logf("verified 70 frozen IDs; embedded compatible registry currently has %d IDs", len(embedded))
}
