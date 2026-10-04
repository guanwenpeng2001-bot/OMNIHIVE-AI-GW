package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type miniMaxHandlerRepo struct {
	service.AccountRepository
	account    *service.Account
	persistErr error
	writes     int
}

func (repo *miniMaxHandlerRepo) GetByID(context.Context, int64) (*service.Account, error) {
	return repo.account, nil
}
func (repo *miniMaxHandlerRepo) UpdateAccountRuntimeSnapshot(context.Context, *service.Account, map[string]any, *time.Time) error {
	if repo.persistErr != nil {
		return repo.persistErr
	}
	repo.writes++
	return nil
}

type miniMaxHandlerUpstream struct {
	service.HTTPUpstream
	body    string
	calls   int
	request *http.Request
}

func (upstream *miniMaxHandlerUpstream) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	upstream.calls++
	upstream.request = request
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(upstream.body))}, nil
}

func TestMiniMaxPayGBalanceAdminRoute(test *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []struct {
		name, root, mode, key, body string
		persistErr                  error
		status, calls               int
		success                     bool
		balance                     float64
	}{
		{"cn", "https://api.minimax.cn/v1", "payg", "sk-api-fixture", `{"available_amount":"98.00","base_resp":{"status_code":0}}`, nil, 200, 1, true, 98},
		{"global zero", "https://api.minimax.io", "payg", "sk-api-fixture", `{"available_amount":"0.00","base_resp":{"status_code":0}}`, nil, 200, 1, true, 0},
		{"business failure", "https://api.minimax.cn", "payg", "sk-api-fixture", `{"available_amount":"98","base_resp":{"status_code":1004}}`, nil, 200, 1, false, 0},
		{"missing balance", "https://api.minimax.cn", "payg", "sk-api-fixture", `{"base_resp":{"status_code":0}}`, nil, 200, 1, false, 0},
		{"coding", "https://api.minimax.cn", "coding", "sk-api-fixture", "", nil, 400, 0, false, 0},
		{"plan key", "https://api.minimax.cn", "payg", "sk-plan-fixture", "", nil, 400, 0, false, 0},
		{"legacy root", "https://api.minimaxi.com/v1", "payg", "sk-api-fixture", "", nil, 400, 0, false, 0},
		{"custom root", "https://relay.example/v1", "payg", "sk-api-fixture", "", nil, 400, 0, false, 0},
		{"identity changed", "https://api.minimax.cn", "payg", "sk-api-fixture", `{"available_amount":"98","base_resp":{"status_code":0}}`, service.ErrAccountRuntimeIdentityChanged, 200, 1, false, 0},
		{"wrapped identity changed", "https://api.minimax.cn", "payg", "sk-api-fixture", `{"available_amount":"98","base_resp":{"status_code":0}}`, fmt.Errorf("private store details: %w", service.ErrAccountRuntimeIdentityChanged), 200, 1, false, 0},
		{"store failed", "https://api.minimax.cn", "payg", "sk-api-fixture", `{"available_amount":"98","base_resp":{"status_code":0}}`, errors.New("store failure"), 200, 1, true, 98},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			repo := &miniMaxHandlerRepo{persistErr: scenario.persistErr, account: &service.Account{ID: 42, Platform: service.PlatformMiniMax, Type: service.AccountTypeAPIKey,
				Credentials: map[string]any{"account_mode": scenario.mode, "base_url": scenario.root, "api_key": scenario.key}}}
			upstream := &miniMaxHandlerUpstream{body: scenario.body}
			handler := NewCNProviderHandler(nil, service.NewCNProviderBalanceService(repo, nil, upstream, nil))
			router := gin.New()
			router.GET("/admin/cn-providers/accounts/:id/balance", handler.QueryBalance)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/cn-providers/accounts/42/balance", nil))
			require.Equal(test, scenario.status, recorder.Code, recorder.Body.String())
			require.Equal(test, scenario.calls, upstream.calls)
			var publicEnvelope struct {
				Data map[string]json.RawMessage `json:"data"`
			}
			require.NoError(test, json.Unmarshal(recorder.Body.Bytes(), &publicEnvelope))
			require.NotContains(test, publicEnvelope.Data, "available", "MiniMax availability is unknown")
			if !scenario.success {
				require.NotContains(test, publicEnvelope.Data, "balance", "unknown balance must not become a public zero")
				require.NotContains(test, publicEnvelope.Data, "balances")
			}
			if scenario.status != 200 {
				require.Zero(test, repo.writes)
				require.NotContains(test, recorder.Body.String(), `"balance":98`)
				return
			}
			var envelope struct {
				Data service.CompatibleBalanceResult `json:"data"`
			}
			require.NoError(test, json.Unmarshal(recorder.Body.Bytes(), &envelope))
			require.Equal(test, scenario.success, envelope.Data.Success)
			if errors.Is(scenario.persistErr, service.ErrAccountRuntimeIdentityChanged) {
				require.Equal(test, "identity_changed", envelope.Data.Reason)
				require.Equal(test, "wallet_balance", envelope.Data.Kind)
				require.Equal(test, service.PlatformMiniMax, envelope.Data.Provider)
				require.False(test, envelope.Data.Persisted)
				require.Positive(test, envelope.Data.FetchedAt)
				require.Equal(test, "Account configuration changed; query again.", envelope.Data.Error)
				for _, field := range []string{"balance", "balances", "used", "credits", "project_budget", "currency", "available"} {
					require.NotContains(test, publicEnvelope.Data, field)
				}
				require.NotContains(test, recorder.Body.String(), "private store details")
			}
			if scenario.success {
				require.Contains(test, publicEnvelope.Data, "balance", "a real zero must remain present")
				require.NotNil(test, envelope.Data.Balance)
				require.Equal(test, scenario.balance, *envelope.Data.Balance)
				require.Equal(test, "wallet_balance", envelope.Data.Kind)
				require.Empty(test, envelope.Data.Currency)
				require.Len(test, envelope.Data.Balances, 1)
				require.Empty(test, envelope.Data.Balances[0].Currency)
				require.Equal(test, scenario.persistErr == nil, envelope.Data.Persisted)
			} else {
				require.Nil(test, envelope.Data.Balance)
				require.NotEmpty(test, envelope.Data.Error)
				require.Empty(test, envelope.Data.Balances)
				require.Zero(test, repo.writes)
			}
		})
	}
}

func TestMiniMaxProjectionPreservesDeepSeekAvailability(test *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, available := range []bool{true, false} {
		test.Run(fmt.Sprint(available), func(test *testing.T) {
			repo := &miniMaxHandlerRepo{account: &service.Account{ID: 42, Platform: service.PlatformDeepseek, Type: service.AccountTypeAPIKey,
				Credentials: map[string]any{"account_mode": "payg", "base_url": "https://api.deepseek.com", "api_key": "sk-fixture"}}}
			upstream := &miniMaxHandlerUpstream{body: fmt.Sprintf(`{"is_available":%t,"balance_infos":[{"currency":"CNY","total_balance":"0"}]}`, available)}
			handler := NewCNProviderHandler(nil, service.NewCNProviderBalanceService(repo, nil, upstream, nil))
			router := gin.New()
			router.GET("/admin/cn-providers/accounts/:id/balance", handler.QueryBalance)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/cn-providers/accounts/42/balance", nil))
			require.Equal(test, http.StatusOK, recorder.Code)
			var envelope struct {
				Data map[string]json.RawMessage `json:"data"`
			}
			require.NoError(test, json.Unmarshal(recorder.Body.Bytes(), &envelope))
			require.Contains(test, envelope.Data, "available", "DeepSeek retains its known boolean contract")
			require.JSONEq(test, fmt.Sprint(available), string(envelope.Data["available"]))
			require.JSONEq(test, "true", string(envelope.Data["success"]))
			require.JSONEq(test, "0", string(envelope.Data["balance"]))
			require.Equal(test, 1, upstream.calls)
		})
	}
}

func TestMiniMaxProjectionPreservesDeepSeekPersistenceBehavior(test *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &miniMaxHandlerRepo{persistErr: service.ErrAccountRuntimeIdentityChanged,
		account: &service.Account{ID: 42, Platform: service.PlatformDeepseek, Type: service.AccountTypeAPIKey,
			Credentials: map[string]any{"account_mode": "payg", "base_url": "https://api.deepseek.com", "api_key": "sk-fixture"}}}
	upstream := &miniMaxHandlerUpstream{body: `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"98"}]}`}
	handler := NewCNProviderHandler(nil, service.NewCNProviderBalanceService(repo, nil, upstream, nil))
	router := gin.New()
	router.GET("/admin/cn-providers/accounts/:id/balance", handler.QueryBalance)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/cn-providers/accounts/42/balance", nil))
	require.Equal(test, http.StatusOK, recorder.Code)
	require.Equal(test, 1, upstream.calls)
	require.Zero(test, repo.writes)
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	require.NoError(test, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	// The existing DeepSeek native path keeps a successful read on store errors.
	// MiniMax's public identity projection must not change that contract.
	require.JSONEq(test, "true", string(envelope.Data["success"]))
	require.JSONEq(test, "true", string(envelope.Data["available"]))
	require.JSONEq(test, "false", string(envelope.Data["persisted"]))
	require.JSONEq(test, "98", string(envelope.Data["balance"]))
	require.NotContains(test, envelope.Data, "reason")
}
