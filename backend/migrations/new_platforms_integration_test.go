//go:build integration

package migrations_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/migrations"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Only disposable databases are changed, never the database in the supplied DSN.
func TestNewPlatformMigrations(t *testing.T) {
	dsn := os.Getenv("NEW_PLATFORMS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set NEW_PLATFORMS_TEST_POSTGRES_DSN for isolated PostgreSQL migration tests")
	}
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer admin.Close()
	for _, fresh := range []bool{false, true} {
		t.Run(fmt.Sprintf("fresh=%t", fresh), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			name := fmt.Sprintf("sub2api_platform_m1_%d", time.Now().UnixNano())
			_, err := admin.ExecContext(ctx, `CREATE DATABASE "`+name+`"`)
			require.NoError(t, err)
			defer func() {
				_, err := admin.ExecContext(context.Background(), `DROP DATABASE "`+name+`" WITH (FORCE)`)
				require.NoError(t, err)
			}()
			parsed, err := url.Parse(dsn)
			require.NoError(t, err)
			parsed.Path = "/" + name
			db, err := sql.Open("postgres", parsed.String())
			require.NoError(t, err)
			defer db.Close()
			apply := func(file string) {
				body, err := migrations.FS.ReadFile(file)
				require.NoError(t, err)
				_, err = db.ExecContext(ctx, string(body))
				require.NoError(t, err, file)
			}
			definition := func(constraint string) string {
				var value string
				require.NoError(t, db.QueryRowContext(ctx, "SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname=$1", constraint).Scan(&value))
				return value
			}
			monitorConstraints := []string{"channel_monitors_provider_check", "channel_monitor_request_templates_provider_check"}
			if fresh {
				require.NoError(t, repository.ApplyMigrations(ctx, db))
				require.NoError(t, repository.ApplyMigrations(ctx, db))
			} else {
				// Minimal pre-241 tables make the CHECK contract independently testable.
				_, err = db.ExecContext(ctx, `CREATE TABLE user_platform_quotas(platform text);
     CREATE TABLE composite_model_routes(target_platform text);
     CREATE TABLE channel_monitors(provider text);
     CREATE TABLE channel_monitor_request_templates(provider text);`)
				require.NoError(t, err)
				apply("239_cursor_platform.sql")
				apply("240_channel_monitor_provider_capabilities.sql")
				previousMonitors := []string{definition(monitorConstraints[0]), definition(monitorConstraints[1])}
				for _, table := range []string{"user_platform_quotas", "composite_model_routes"} {
					_, err = db.ExecContext(ctx, "INSERT INTO "+table+" VALUES ('cursor'), ('opencode_go'), ('minimax')")
					require.NoError(t, err)
				}
				for i, file := range []string{"241_qianwen_platform.sql", "242_command_code_platform.sql"} {
					apply(file)
					apply(file) // Direct DDL reapplication also remains safe.
					platform := []string{"qianwen", "commandcode"}[i]
					for _, table := range []string{"user_platform_quotas", "composite_model_routes"} {
						_, err = db.ExecContext(ctx, "INSERT INTO "+table+" VALUES ($1)", platform)
						require.NoError(t, err)
						_, err = db.ExecContext(ctx, "INSERT INTO "+table+" VALUES ('composite')")
						require.Error(t, err)
						_, err = db.ExecContext(ctx, "INSERT INTO "+table+" VALUES ('bogus')")
						require.Error(t, err)
						if i == 0 {
							_, err = db.ExecContext(ctx, "INSERT INTO "+table+" VALUES ('commandcode')")
							require.Error(t, err, "241 must not prematurely register commandcode")
						}
					}
					for j, constraint := range monitorConstraints {
						require.Equal(t, previousMonitors[j], definition(constraint))
					}
				}
				for _, table := range []string{"user_platform_quotas", "composite_model_routes"} {
					var count int
					require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count))
					require.Equal(t, 5, count, "legacy rows and both new platforms must survive")
				}
				for _, table := range []string{"channel_monitors", "channel_monitor_request_templates"} {
					for _, platform := range []string{"qianwen", "commandcode", "cursor", "minimax", "opencode_go"} {
						_, err = db.ExecContext(ctx, "INSERT INTO "+table+" VALUES ($1)", platform)
						require.Error(t, err)
					}
				}
				apply("243_channel_monitor_qianwen_commandcode.sql")
				apply("243_channel_monitor_qianwen_commandcode.sql")
				for _, table := range []string{"channel_monitors", "channel_monitor_request_templates"} {
					for _, provider := range []string{"openai", "anthropic", "gemini", "grok", "antigravity", "kimi", "zhipu", "deepseek", "qianwen", "commandcode"} {
						_, err = db.ExecContext(ctx, "INSERT INTO "+table+" VALUES ($1)", provider)
						require.NoError(t, err)
					}
					for _, provider := range []string{"cursor", "minimax", "opencode_go", "bogus"} {
						_, err = db.ExecContext(ctx, "INSERT INTO "+table+" VALUES ($1)", provider)
						require.Error(t, err)
					}
				}
			}
			for _, constraint := range []string{"user_platform_quotas_platform_check", "composite_model_routes_target_platform_check"} {
				value := definition(constraint)
				for _, platform := range []string{"anthropic", "openai", "gemini", "antigravity", "grok", "kimi", "zhipu", "deepseek", "minimax", "opencode_go", "cursor", "qianwen", "commandcode"} {
					require.Contains(t, value, "'"+platform+"'")
				}
				require.NotContains(t, value, "'composite'")
			}
			for _, constraint := range monitorConstraints {
				value := definition(constraint)
				for _, platform := range []string{"openai", "anthropic", "gemini", "grok", "antigravity", "kimi", "zhipu", "deepseek", "qianwen", "commandcode"} {
					require.Contains(t, value, "'"+platform+"'")
				}
				for _, platform := range []string{"cursor", "minimax", "opencode_go"} {
					require.NotContains(t, value, "'"+platform+"'")
				}
			}
		})
	}
}
