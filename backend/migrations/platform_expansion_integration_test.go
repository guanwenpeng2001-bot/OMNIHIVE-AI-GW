//go:build integration

package migrations_test

import (
	"context"
	"os"
	"regexp"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/testutil/testresources"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestPlatformExpansionMigrations(test *testing.T) {
	dsn := os.Getenv("NEW_PLATFORMS_TEST_POSTGRES_DSN")
	require.NoError(test, testresources.ValidatePostgres(dsn, "job_test"))
	legacy := []string{"anthropic", "openai", "gemini", "antigravity", "grok", "kimi", "zhipu", "deepseek", "minimax", "opencode_go", "cursor", "qianwen", "commandcode"}
	added := []string{"volcengine", "qianfan", "hunyuan", "mistral", "bedrock"}
	files := []string{"259_volcengine_platform.sql", "260_qianfan_platform.sql", "261_hunyuan_platform.sql", "262_mistral_platform.sql", "263_bedrock_platform.sql"}
	for _, scenario := range []string{"fresh schema", "legacy rows"} {
		test.Run(scenario, func(test *testing.T) {
			database := testresources.OpenSchema(test, dsn)
			ctx := context.Background()
			_, err := database.ExecContext(ctx, `CREATE TABLE user_platform_quotas(platform text); CREATE TABLE composite_model_routes(target_platform text); CREATE TABLE accounts(platform text, type text); INSERT INTO accounts VALUES ('anthropic','bedrock');`)
			require.NoError(test, err)
			if scenario == "legacy rows" {
				body, readErr := migrations.FS.ReadFile("242_command_code_platform.sql")
				require.NoError(test, readErr)
				_, err = database.ExecContext(ctx, string(body))
				require.NoError(test, err)
				for _, platform := range legacy {
					_, err = database.ExecContext(ctx, "INSERT INTO user_platform_quotas VALUES($1)", platform)
					require.NoError(test, err)
				}
			}
			for index, file := range files {
				body, readErr := migrations.FS.ReadFile(file)
				require.NoError(test, readErr)
				for repeat := 0; repeat < 2; repeat++ {
					_, err = database.ExecContext(ctx, string(body))
					require.NoError(test, err)
				}
				for _, table := range []string{"user_platform_quotas", "composite_model_routes"} {
					_, err = database.ExecContext(ctx, "INSERT INTO "+table+" VALUES($1)", added[index])
					require.NoError(test, err)
					for _, rejected := range append(append([]string{}, added[index+1:]...), "composite", "workbuddy", "unknown") {
						_, err = database.ExecContext(ctx, "INSERT INTO "+table+" VALUES($1)", rejected)
						require.Error(test, err)
					}
				}
			}
			expected := append(append([]string{}, legacy...), added...)
			for _, constraint := range []string{"user_platform_quotas_platform_check", "composite_model_routes_target_platform_check"} {
				var definition string
				require.NoError(test, database.QueryRowContext(ctx, "SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname=$1 AND connamespace=current_schema()::regnamespace", constraint).Scan(&definition))
				matches := regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(definition, -1)
				actual := make([]string, 0, len(matches))
				for _, match := range matches {
					actual = append(actual, match[1])
				}
				require.ElementsMatch(test, expected, actual)
			}
			var platform, kind string
			require.NoError(test, database.QueryRowContext(ctx, "SELECT platform,type FROM accounts").Scan(&platform, &kind))
			require.Equal(test, "anthropic", platform)
			require.Equal(test, "bedrock", kind)
		})
	}
}
