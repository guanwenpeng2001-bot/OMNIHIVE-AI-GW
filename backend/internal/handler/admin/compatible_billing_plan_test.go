//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/dto"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

// Model the JSON persistence boundary without a database or vendor request.
type persistedPlanCapture struct{ compatibleAdminCapture }

func (capture *persistedPlanCapture) UpdateAccount(_ context.Context, _ int64, input *service.UpdateAccountInput) (*service.Account, error) {
	capture.updated = input
	if len(input.Credentials) > 0 {
		encoded, err := json.Marshal(service.PreserveCurrentModelMapping(input.Credentials, capture.account.Credentials))
		if err != nil {
			return nil, err
		}
		capture.account.Credentials = nil
		if err := json.Unmarshal(encoded, &capture.account.Credentials); err != nil {
			return nil, err
		}
	}
	return &capture.account, nil
}

func TestCompatibleBillingPlanAdminRoundtrip(t *testing.T) {
	for _, scenario := range []struct{ platform, plan string }{{"aicodemirror", "pro"}, {"ccsub", "subscription"}, {"shengsuanyun", "enterprise"}} {
		t.Run(scenario.platform, func(t *testing.T) {
			svc := &persistedPlanCapture{compatibleAdminCapture{stubAdminService: newStubAdminService()}}
			handler := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			body := fmt.Sprintf(`{"name":"fixture","platform":%q,"type":"apikey","credentials":{"api_key":"fixture-key","account_mode":"payg","api_protocol":"adaptive","billing_plan":%q,"base_url":"https://relay.example/v1","api_base_urls":{"anthropic":"https://private.example/native"},"model_mapping":{"alias":"model"},"credential_secret_reference":"fixture-reference","credential_secret_version":3}}`, scenario.platform, scenario.plan)
			created := compatibleRequest(handler.Create, http.MethodPost, body)
			require.Equal(t, http.StatusOK, created.Code, created.Body.String())
			require.Equal(t, scenario.plan, svc.account.Credentials["billing_plan"])
			require.NotContains(t, created.Body.String(), "fixture-key")
			var response struct {
				Data dto.Account `json:"data"`
			}
			require.NoError(t, json.Unmarshal(created.Body.Bytes(), &response))
			require.Equal(t, scenario.plan, response.Data.Credentials["billing_plan"])
			require.NotContains(t, response.Data.Credentials, "api_key")
			updated := compatibleRequest(handler.Update, http.MethodPut, `{"credentials":{"api_protocol":"anthropic"}}`)
			require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
			require.Equal(t, scenario.plan, svc.account.Credentials["billing_plan"])
			for _, field := range []string{"api_key", "account_mode", "base_url", "api_base_urls", "model_mapping", "credential_secret_reference", "credential_secret_version"} {
				require.Equal(t, svc.created.Credentials[field], svc.account.Credentials[field])
			}
			for _, clear := range []string{`null`, `""`} {
				updated = compatibleRequest(handler.Update, http.MethodPut, `{"credentials":{"billing_plan":`+clear+`}}`)
				require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
				require.NotContains(t, svc.account.Credentials, "billing_plan")
				require.NoError(t, json.Unmarshal(updated.Body.Bytes(), &response))
				// Unmarshal into a fresh map: JSON omission must stay omission.
				var cleared struct {
					Data dto.Account `json:"data"`
				}
				require.NoError(t, json.Unmarshal(updated.Body.Bytes(), &cleared))
				require.NotContains(t, cleared.Data.Credentials, "billing_plan")
				allowed, err := service.PaygBalanceAllowed(scenario.platform, cleared.Data.Credentials)
				require.NoError(t, err)
				require.True(t, allowed)
				svc.account.Credentials["billing_plan"] = scenario.plan
			}
			for _, bad := range []string{`"unknown"`, `"PRO"`, `1`, `{}`} {
				patch := `{"credentials":{"billing_plan":` + bad + `}}`
				svc.updated = nil
				require.Equal(t, http.StatusBadRequest, compatibleRequest(handler.Update, http.MethodPut, patch).Code)
				require.Nil(t, svc.updated)
				request := fmt.Sprintf(`{"name":"fixture","platform":%q,"type":"apikey","credentials":{"api_key":"fixture-key","billing_plan":%s}}`, scenario.platform, bad)
				require.Equal(t, http.StatusBadRequest, compatibleRequest(handler.Create, http.MethodPost, request).Code)
			}
		})
	}
}

func TestCompatibleBillingPlanSafeReadAndExport(t *testing.T) {
	for _, selected := range []bool{false, true} {
		credentials := map[string]any{"api_key": "fixture-key", "account_mode": "payg"}
		if selected {
			credentials["billing_plan"] = "pro"
		}
		account := service.Account{ID: 12, Platform: "aicodemirror", Type: service.AccountTypeAPIKey, Credentials: credentials}
		safe := dto.AccountFromService(&account)
		listed := dto.AccountListItemFromAccount(safe)
		require.NotContains(t, safe.Credentials, "api_key")
		require.Equal(t, credentials["billing_plan"], safe.Credentials["billing_plan"])
		require.Equal(t, safe.Credentials, listed.Credentials)
		_, present := safe.Credentials["billing_plan"]
		require.Equal(t, selected, present)
		router, capture := setupAccountDataRouter(t)
		capture.accounts = []service.Account{account}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/data", nil))
		require.Equal(t, http.StatusOK, recorder.Code)
		var exported dataResponse
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &exported))
		require.Len(t, exported.Data.Accounts, 1)
		require.Equal(t, credentials, exported.Data.Accounts[0].Credentials)
	}
}
