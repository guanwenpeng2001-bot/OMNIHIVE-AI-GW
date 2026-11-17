package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupAPIKeyBudgetHandler(adminSvc service.AdminService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewAdminAPIKeyHandler(adminSvc, nil)
	// 管理端路由组已经挂了 adminAuth 与 auditLog；这里只补上身份注入。
	router.PUT("/api/v1/admin/api-keys/:id/budget", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		h.AdminUpdateAPIKeyBudget(c)
	})
	return router
}

func decodeAdminBudgetResponse(t *testing.T, body string) map[string]any {
	t.Helper()
	envelope := map[string]any{}
	require.NoError(t, json.Unmarshal([]byte(body), &envelope))
	data, ok := envelope["data"].(map[string]any)
	require.True(t, ok, "the response must carry a data object: %s", body)
	return data
}

func TestAdminAPIKeyHandler_UpdateBudgetSetsTheEnforcedSwitch(t *testing.T) {
	svc := newStubAdminService()
	router := setupAPIKeyBudgetHandler(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/api-keys/10/budget", bytes.NewBufferString(
		`{"budget":{"mode":"duration","limit_usd":5,"duration_seconds":600,"enforced":true}}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	saved, ok := svc.budgetPolicies[10]
	require.True(t, ok, "the handler must reach the admin-only budget write path")
	require.True(t, saved.Enforced)
	require.InDelta(t, 5, saved.LimitUSD, 1e-9)
	require.Equal(t, service.APIKeyBudgetModeDuration, saved.Mode)
}

func TestAdminAPIKeyHandler_UpdateBudgetRecordsTheAuditedAdjustment(t *testing.T) {
	svc := newStubAdminService()
	router := setupAPIKeyBudgetHandler(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/api-keys/10/budget", bytes.NewBufferString(
		`{"adjustment":{"delta_usd":-2.5,"reason":"goodwill credit"}}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, svc.budgetAdjustments, 1)
	require.Equal(t, int64(10), svc.budgetAdjustments[0].APIKeyID)
	require.Equal(t, int64(42), svc.budgetAdjustments[0].ActorUserID,
		"the adjustment carries the acting administrator for the audit trail")
	require.Equal(t, "goodwill credit", svc.budgetAdjustments[0].Reason)
	require.InDelta(t, -2.5, svc.budgetAdjustments[0].DeltaUSD, 1e-9)
}

func TestAdminAPIKeyHandler_UpdateBudgetAppliesBothInOneRequest(t *testing.T) {
	svc := newStubAdminService()
	router := setupAPIKeyBudgetHandler(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/api-keys/10/budget", bytes.NewBufferString(
		`{"budget":{"mode":"duration","limit_usd":9,"duration_seconds":3600,"enforced":true},"adjustment":{"delta_usd":1.25,"reason":"correction"}}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	data := decodeAdminBudgetResponse(t, rec.Body.String())
	require.NotNil(t, data["policy"])
	require.NotNil(t, data["state"])
	require.Len(t, svc.budgetAdjustments, 1)
}

func TestAdminAPIKeyHandler_UpdateBudgetRequiresSomethingToWrite(t *testing.T) {
	router := setupAPIKeyBudgetHandler(newStubAdminService())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/api-keys/10/budget", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAdminAPIKeyHandler_UpdateBudgetRejectsInvalidID(t *testing.T) {
	router := setupAPIKeyBudgetHandler(newStubAdminService())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/api-keys/not-a-number/budget", bytes.NewBufferString(
		`{"budget":{"mode":"off"}}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}
