package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestCompatibleUsageDoesNotChangeNativeGetUsage(test *testing.T) {
	for _, provider := range domain.CompatibleProviders() {
		repo := &compatibleBalanceRepo{account: compatibleBalanceAccount(provider.ID)}
		service := &AccountUsageService{accountRepo: repo}
		usage, err := service.GetUsage(context.Background(), repo.account.ID)
		require.Error(test, err, provider.ID)
		require.Nil(test, usage, provider.ID)
		require.Empty(test, repo.writes)
	}
}

const compatibleProjectBudgetFixture = `{"success":true,"data":{"gateway_type":"enterprise","scope":"account_in_project","project_id":65,"ram_user_id":0,"budget_kind":"monthly","period_kind":"monthly","period_start":"2026-09-01T00:00:00+08:00","period_end":"2026-10-01T00:00:00+08:00","period_consumed_cny":0,"period_limit_cny":1000,"remaining_percentage":100,"unlimited":false}}`

func TestCompatibleProjectBudgetNeverPersists(test *testing.T) {
	for _, scenario := range []struct {
		name, body string
		unlimited  bool
	}{
		{"monthly", compatibleProjectBudgetFixture, false},
		{"daily", strings.ReplaceAll(compatibleProjectBudgetFixture, "monthly", "daily"), false},
		{"unlimited", strings.NewReplacer(`"budget_kind":"monthly"`, `"budget_kind":"unlimited"`, `"period_limit_cny":1000`, `"period_limit_cny":null`, `"remaining_percentage":100`, `"remaining_percentage":null`, `"unlimited":false`, `"unlimited":true`).Replace(compatibleProjectBudgetFixture), true},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			repo := &compatibleBalanceRepo{account: compatibleBalanceAccount("shengsuanyun")}
			upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(scenario.body))}, nil
			}}
			value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), repo.account.ID)
			require.NoError(test, err)
			result := requireCompatibleBalanceResult(test, value)
			require.True(test, result.Success)
			require.Equal(test, "project_budget", result.Kind)
			require.Nil(test, result.Balance)
			require.Empty(test, result.Balances)
			require.False(test, result.Persisted)
			require.Empty(test, repo.writes)
			budget := result.ProjectBudget
			require.Equal(test, int64(65), budget.ProjectID)
			require.Zero(test, budget.RAMUserID)
			require.Equal(test, "account_in_project", budget.Scope)
			require.Equal(test, "2026-09-01T00:00:00+08:00", budget.PeriodStart)
			require.Equal(test, "CNY", budget.Currency)
			require.Zero(test, budget.Consumed)
			require.Equal(test, scenario.unlimited, budget.Unlimited)
			if scenario.unlimited {
				require.Nil(test, budget.Limit)
				require.Nil(test, budget.RemainingPercentage)
			} else {
				require.Equal(test, float64(1000), *budget.Limit)
			}
			encoded, err := json.Marshal(result)
			require.NoError(test, err)
			require.NotContains(test, string(encoded), `"balance":`)
		})
	}
}

func TestCompatibleProjectBudgetMalformed(test *testing.T) {
	for _, scenario := range []struct{ old, replacement string }{
		{`"project_id":65`, `"project_id":65.1`}, {`"project_id":65`, `"project_id":9223372036854775808`},
		{`"ram_user_id":0`, `"ram_user_id":null`}, {`"ram_user_id":0`, `"other":0`},
		{`"scope":"account_in_project"`, `"scope":"account"`},
		{`"period_limit_cny":1000`, `"period_limit_cny":null`}, {`"period_limit_cny":1000`, `"other":1000`},
		{`"period_consumed_cny":0`, `"period_consumed_cny":"0"`}, {`"period_consumed_cny":0`, `"period_consumed_cny":null`},
		{`"period_consumed_cny":0`, `"period_consumed_cny":1e400`},
		{`"unlimited":false`, `"unlimited":true`}, {`"unlimited":false`, `"unlimited":null`},
		{`"budget_kind":"monthly"`, `"budget_kind":"unlimited"`},
		{`"period_kind":"monthly"`, `"period_kind":"yearly"`},
		{`"2026-10-01T00:00:00+08:00"`, `"2026-08-01T00:00:00+08:00"`},
		{`"2026-10-01T00:00:00+08:00"`, `"2026-10-01T00:00:00"`},
		{`"success":true`, `"success":false`},
	} {
		body := strings.Replace(compatibleProjectBudgetFixture, scenario.old, scenario.replacement, 1)
		budget, err := parseCompatibleProjectBudget([]byte(body))
		require.Error(test, err, body)
		require.Nil(test, budget)
	}
}

func TestCompatibleAtlasWalletScopeAndPermission(test *testing.T) {
	for _, status := range []int{200, 403} {
		repo := &compatibleBalanceRepo{account: compatibleBalanceAccount("atlascloud")}
		repo.account.Credentials["api_key"] = "apikey-fixture-only"
		upstream := compatibleBalanceHTTP{send: func(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
			require.Equal(test, "https://api.atlascloud.ai/public/v1/balance", request.URL.String())
			require.Equal(test, "Bearer apikey-fixture-only", request.Header.Get("Authorization"))
			body := `{"object":"balance","scope":"account","available":{"value":"0.000000","currency":"usd"}}`
			if status == 403 {
				body = `apikey-fixture-only permission detail`
			}
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
		}}
		value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), repo.account.ID)
		require.NoError(test, err)
		result := requireCompatibleBalanceResult(test, value)
		if status == 200 {
			require.True(test, result.Success)
			require.True(test, result.Persisted)
			require.Equal(test, "account", result.Scope)
			require.Equal(test, "wallet_balance", result.Kind)
			require.Equal(test, "usd", result.Currency)
			require.Zero(test, *result.Balance)
			require.Len(test, repo.writes, 1)
		} else {
			require.False(test, result.Success)
			require.False(test, result.Unsupported)
			require.Equal(test, "permission_denied", result.Reason)
			require.NotContains(test, result.Error, "apikey-fixture-only")
			require.Empty(test, repo.writes)
		}
	}
	for _, body := range []string{
		`{"object":"balance","scope":"key","available":{"value":"0","currency":"usd"}}`,
		`{"object":"balance","scope":"account","available":{"value":0,"currency":"usd"}}`,
		`{"object":"balance","scope":"account","available":{"value":null,"currency":"usd"}}`,
		`{"object":"balance","scope":"account","available":{"value":"0","currency":null}}`,
		`{"object":"balance","scope":"account","available":{"value":"NaN","currency":"usd"}}`,
	} {
		_, err := parseCompatibleBalance("atlascloud", []byte(body))
		require.Error(test, err)
	}
	account := compatibleBalanceAccount("atlascloud")
	account.Extra = map[string]any{"atlascloud_balance": 0, "atlascloud_balance_currency": "usd", "atlascloud_balance_updated_at": "old"}
	ClearAccountRuntimeObservations(account)
	require.Empty(test, account.Extra)
}

func TestCompatibleBalanceCustomRelayAndConsumedNeverSend(test *testing.T) {
	for _, provider := range []string{"novita", "qiniu", "atlascloud", "astron_token", "mimo_token_cn", "mimo_token_sg", "mimo_token_eu"} {
		account := compatibleBalanceAccount(provider)
		if provider == "novita" {
			account.Extra = map[string]any{"custom_base_url_enabled": true}
		}
		if provider == "atlascloud" {
			account.Credentials["account_mode"] = "coding"
		}
		repo := &compatibleBalanceRepo{account: account}
		upstream := compatibleBalanceHTTP{send: func(*http.Request, string, int64, int) (*http.Response, error) {
			test.Fatal("unsupported query sent")
			return nil, nil
		}}
		value, err := NewCNProviderBalanceService(repo, nil, upstream, nil).QueryAccountBalance(context.Background(), account.ID)
		require.NoError(test, err)
		result := requireCompatibleBalanceResult(test, value)
		require.True(test, result.Unsupported)
		require.Equal(test, "unsupported", result.Kind)
		require.Nil(test, result.Balance)
		require.Empty(test, repo.writes)
	}
}
