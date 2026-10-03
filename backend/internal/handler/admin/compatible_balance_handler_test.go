package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type compatibleBalanceHandlerRepo struct {
	service.AccountRepository
	account *service.Account
}

func (repo compatibleBalanceHandlerRepo) GetByID(context.Context, int64) (*service.Account, error) {
	return repo.account, nil
}

type compatibleBalanceHandlerHTTP struct {
	service.HTTPUpstream
	calls int
}

func (upstream *compatibleBalanceHandlerHTTP) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	upstream.calls++
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"availableBalance":"10000"}`))}, nil
}

func TestCompatibleBalanceAdminRoute(test *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []struct {
		name, platform, mode string
		success              bool
		calls                int
	}{
		{"supported", "novita", "payg", true, 1},
		{"coding unsupported", "novita", "coding", false, 0},
		{"unknown endpoint", "together", "payg", false, 0},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			repo := compatibleBalanceHandlerRepo{account: &service.Account{ID: 12, Platform: scenario.platform, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"account_mode": scenario.mode, "api_key": "fixture"}}}
			upstream := &compatibleBalanceHandlerHTTP{}
			handler := NewCNProviderHandler(nil, service.NewCNProviderBalanceService(repo, nil, upstream, nil))
			router := gin.New()
			router.GET("/admin/cn-providers/accounts/:id/balance", handler.QueryBalance)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/cn-providers/accounts/12/balance", nil))
			require.Equal(test, http.StatusOK, recorder.Code)
			var envelope struct {
				Data service.CompatibleBalanceResult `json:"data"`
			}
			require.NoError(test, json.Unmarshal(recorder.Body.Bytes(), &envelope))
			require.Equal(test, scenario.success, envelope.Data.Success)
			require.Equal(test, !scenario.success, envelope.Data.Unsupported)
			require.Equal(test, scenario.calls, upstream.calls)
			if scenario.success {
				require.Equal(test, float64(1), *envelope.Data.Balance)
			} else {
				require.Nil(test, envelope.Data.Balance)
			}
		})
	}
}
