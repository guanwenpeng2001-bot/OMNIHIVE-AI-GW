package admin

import (
	"context"
	"encoding/json"
	"errors"
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
		{"identity changed", "https://api.minimax.cn", "payg", "sk-api-fixture", `{"available_amount":"98","base_resp":{"status_code":0}}`, service.ErrAccountRuntimeIdentityChanged, 500, 1, false, 0},
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
			if scenario.status != 200 {
				require.Zero(test, repo.writes)
				require.NotContains(test, recorder.Body.String(), `"balance":98`)
				return
			}
			var envelope struct {
				Data service.CNProviderBalanceResult `json:"data"`
			}
			require.NoError(test, json.Unmarshal(recorder.Body.Bytes(), &envelope))
			require.Equal(test, scenario.success, envelope.Data.Success)
			if scenario.success {
				require.Equal(test, scenario.balance, envelope.Data.Balance)
				require.Empty(test, envelope.Data.Currency)
				require.Len(test, envelope.Data.Balances, 1)
				require.Empty(test, envelope.Data.Balances[0].Currency)
				require.Equal(test, scenario.persistErr == nil, envelope.Data.Persisted)
			} else {
				require.NotEmpty(test, envelope.Data.Error)
				require.Empty(test, envelope.Data.Balances)
				require.Zero(test, repo.writes)
			}
		})
	}
}
