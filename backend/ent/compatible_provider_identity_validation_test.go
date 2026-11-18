package ent_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/channelmonitor"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/channelmonitorrequesttemplate"
	_ "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent/runtime"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/migrations"
	"github.com/stretchr/testify/require"
)

var errIdentityReachedSQL = errors.New("identity validation reached SQL driver")

// Stop at the SQL boundary, after the actual generated Save path validates.
// No database, account, credential, or service is contacted by these tests.
type identityRecordingDriver struct{ calls int }

func (driver *identityRecordingDriver) Dialect() string { return dialect.Postgres }
func (driver *identityRecordingDriver) Close() error    { return nil }
func (driver *identityRecordingDriver) Exec(context.Context, string, any, any) error {
	driver.calls++
	return errIdentityReachedSQL
}
func (driver *identityRecordingDriver) Query(context.Context, string, any, any) error {
	driver.calls++
	return errIdentityReachedSQL
}
func (driver *identityRecordingDriver) Tx(context.Context) (dialect.Tx, error) {
	driver.calls++
	return nil, errIdentityReachedSQL
}

func TestCompatibleProviderGeneratedWriteValidation(test *testing.T) {
	body, err := migrations.FS.ReadFile("284_copilot_nous_identities.sql")
	require.NoError(test, err)
	native := strings.Fields("anthropic openai gemini antigravity grok kimi zhipu deepseek minimax opencode_go cursor qianwen commandcode qianfan hunyuan mistral openrouter bedrock volcengine")
	for _, entity := range []struct {
		name, constraint, field string
		count                   int
	}{
		{"quota", "user_platform_quotas_platform_check", "platform", 92},
		{"monitor", "channel_monitors_provider_check", "provider", 91},
		{"template", "channel_monitor_request_templates_provider_check", "provider", 91},
	} {
		match := regexp.MustCompile(`(?s)ADD CONSTRAINT ` + entity.constraint + `\s+CHECK\s*\(\w+ IN \(([^;]+)\)\);`).FindStringSubmatch(string(body))
		require.Len(test, match, 2)
		var allowed, compatible []string
		for _, value := range regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(match[1], -1) {
			id := value[1]
			allowed = append(allowed, id)
			legacy := id == "vertex"
			for _, old := range native {
				legacy = legacy || id == old
			}
			if !legacy {
				compatible = append(compatible, id)
			}
		}
		require.Len(test, allowed, entity.count)
		require.Len(test, compatible, 72)
		sort.Strings(compatible)
		require.Equal(test, "5f4ee54c46f62235239a4d9196dc840a3075b48133bee60afefd89e4f2b502bf", fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(compatible, "\n")+"\n"))))
		for _, old := range native {
			require.Contains(test, allowed, old)
		}
		// Migration 285 extends the frozen historical set without changing it.
		allowed = append(allowed, "astron_token", "mimo_token_sg", "mimo_token_eu")
		// Migration 286 adds the Kaggle self-hosted notebook provider.
		allowed = append(allowed, "kaggle")
		rejected := []string{"unknown", "composite", "relaycode", "etok", "A6API", "", "a6api ", " a6api"}
		if entity.name != "quota" {
			rejected = append(rejected, "vertex")
		}
		for _, operation := range []string{"create", "update", "update_one"} {
			for _, id := range append(append([]string{}, allowed...), rejected...) {
				test.Run(entity.name+"/"+operation+"/"+id, func(test *testing.T) {
					driver := &identityRecordingDriver{}
					client := ent.NewClient(ent.Driver(driver))
					defer func() { require.NoError(test, client.Close()) }()
					ctx := context.Background()
					var err error
					switch entity.name {
					case "quota":
						switch operation {
						case "create":
							_, err = client.UserPlatformQuota.Create().SetUserID(1).SetPlatform(id).Save(ctx)
						case "update":
							_, err = client.UserPlatformQuota.Update().SetPlatform(id).Save(ctx)
						case "update_one":
							_, err = client.UserPlatformQuota.UpdateOneID(1).SetPlatform(id).Save(ctx)
						}
					case "monitor":
						provider := channelmonitor.Provider(id)
						switch operation {
						case "create":
							_, err = client.ChannelMonitor.Create().SetWorkspaceID(1).SetName("validation fixture").SetProvider(provider).SetEndpoint("").SetAPIKeyEncrypted("synthetic-not-a-key").SetPrimaryModel("fixture").SetIntervalSeconds(60).SetCreatedBy(1).Save(ctx)
						case "update":
							_, err = client.ChannelMonitor.Update().SetProvider(provider).Save(ctx)
						case "update_one":
							_, err = client.ChannelMonitor.UpdateOneID(1).SetProvider(provider).Save(ctx)
						}
					case "template":
						provider := channelmonitorrequesttemplate.Provider(id)
						switch operation {
						case "create":
							_, err = client.ChannelMonitorRequestTemplate.Create().SetWorkspaceID(1).SetName("validation fixture").SetProvider(provider).Save(ctx)
						case "update":
							_, err = client.ChannelMonitorRequestTemplate.Update().SetProvider(provider).Save(ctx)
						case "update_one":
							_, err = client.ChannelMonitorRequestTemplate.UpdateOneID(1).SetProvider(provider).Save(ctx)
						}
					}
					valid := false
					for _, accepted := range allowed {
						valid = valid || accepted == id
					}
					if valid {
						require.ErrorIs(test, err, errIdentityReachedSQL)
						require.Positive(test, driver.calls, "valid identity must reach generated SQL path")
					} else {
						var validation *ent.ValidationError
						require.ErrorAs(test, err, &validation)
						require.Equal(test, entity.field, validation.Name)
						require.Zero(test, driver.calls, "invalid identity must fail before SQL")
					}
				})
			}
		}
	}
}
