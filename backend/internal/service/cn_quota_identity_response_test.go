package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func openCodeQuotaAccount() *Account {
	return &Account{ID: 55, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "test-only"}, Extra: map[string]any{}}
}

type quotaIdentityOutcomeRepo struct {
	AccountRepository
	outcome error
	writes  int
}

func (repo *quotaIdentityOutcomeRepo) UpdateAccountRuntimeSnapshot(context.Context, *Account, map[string]any, *time.Time) error {
	repo.writes++
	return repo.outcome
}
func (repo *quotaIdentityOutcomeRepo) ApplyOpenCodeQuotaProbe(context.Context, *Account, *CNProviderQuotaProbeResult) error {
	repo.writes++
	return repo.outcome
}

func TestCNQuotaIdentityPublication(test *testing.T) {
	for _, provider := range []string{"minimax", "opencode_go"} {
		for _, scenario := range []struct {
			name    string
			outcome error
			stale   bool
		}{
			{"standard identity", ErrAccountRuntimeIdentityChanged, true},
			{"wrapped identity", fmt.Errorf("private context: %w", ErrAccountRuntimeIdentityChanged), true},
			{"ordinary store failure", errors.New("ordinary store failure"), false},
		} {
			test.Run(provider+"/"+scenario.name, func(test *testing.T) {
				account := miniMaxTokenAccount("https://api.minimax.cn/v1")
				body := strings.Replace(miniMaxTokenFixture, `{"model_remains"`, `{"current_subscribe_title":"plan-fixture","model_remains"`, 1)
				if provider == "opencode_go" {
					account = openCodeQuotaAccount()
					body = `{"usage":{"rolling":{"percent":12},"weekly":{"percent":24}}}`
				}
				repo := &quotaIdentityOutcomeRepo{outcome: scenario.outcome}
				result, err := NewCNProviderQuotaService(repo, nil, &cnBalanceResponseUpstream{statusCode: 200, body: body}, nil).QueryUsageForAccount(context.Background(), account)
				require.NoError(test, err)
				require.Equal(test, 1, repo.writes)
				require.False(test, result.Persisted)
				require.Equal(test, !scenario.stale, result.Success)
				require.True(test, result.CredentialValid, "storage identity rejection does not invalidate inference credentials")
				if scenario.stale {
					require.Equal(test, "identity_changed", result.Status)
					encoded, encodeErr := json.Marshal(result)
					require.NoError(test, encodeErr)
					for _, field := range []string{`"tiers"`, `"plan_level"`, `"credits"`, `"token_plan":`, "private context"} {
						require.NotContains(test, string(encoded), field)
					}
				} else {
					require.NotEmpty(test, result.Tiers)
					require.NotEmpty(test, result.PlanLevel)
				}
			})
		}
	}
}

func TestCloudQuotaIdentityPublication(test *testing.T) {
	for _, provider := range []string{"byteplus", "volcengine", "agent", "tencent"} {
		for _, scenario := range []struct {
			name    string
			outcome error
			stale   bool
		}{
			{"standard identity", ErrAccountRuntimeIdentityChanged, true},
			{"wrapped identity", fmt.Errorf("private context: %w", ErrAccountRuntimeIdentityChanged), true},
			{"ordinary store failure", errors.New("ordinary store failure"), false},
		} {
			test.Run(provider+"/"+scenario.name, func(test *testing.T) {
				account, body := arkTeamAccount(provider), arkSeatFixture
				if provider == "agent" {
					account, body = agentQuotaAccount(), agentQuotaFixture
				}
				if provider == "tencent" {
					account, body = tencentQuotaAccount(), tencentQuotaFixture
				}
				repo := &quotaIdentityOutcomeRepo{outcome: scenario.outcome}
				result, err := NewCNProviderQuotaService(repo, nil, &cnBalanceResponseUpstream{statusCode: 200, body: body}, nil).QueryUsageForAccount(volcengineQuotaTestContext(test), account)
				require.NoError(test, err)
				require.Equal(test, 1, repo.writes)
				require.False(test, result.Persisted)
				require.Equal(test, !scenario.stale, result.Success)
				require.True(test, result.CredentialValid)
				if scenario.stale {
					require.Equal(test, "identity_changed", result.Status)
					encoded, err := json.Marshal(result)
					require.NoError(test, err)
					for _, field := range []string{`"tiers"`, `"plan_level"`, `"credits"`, `"token_plan":`, "private context"} {
						require.NotContains(test, string(encoded), field)
					}
				} else if provider == "tencent" {
					require.NotNil(test, result.TokenPlan)
				} else {
					require.NotEmpty(test, result.Tiers)
					require.NotEmpty(test, result.PlanLevel)
				}
			})
		}
	}
}
