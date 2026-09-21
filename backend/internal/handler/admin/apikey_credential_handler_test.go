package admin

import (
	"bytes"

	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupAPIKeyCredentialHandler(adminSvc service.AdminService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewAdminAPIKeyHandler(adminSvc)
	router.PUT("/api/v1/admin/api-keys/:id/status", h.SetStatus)
	router.DELETE("/api/v1/admin/api-keys/:id", h.Delete)
	router.POST("/api/v1/admin/api-keys/:id/credentials/rotate", h.RotateCredential)
	return router
}

func decodeAdminIssue(t *testing.T, body string) map[string]any {
	t.Helper()
	envelope := map[string]any{}
	require.NoError(t, json.Unmarshal([]byte(body), &envelope))
	data, ok := envelope["data"].(map[string]any)
	require.True(t, ok, "the response must carry a data object: %s", body)
	return data
}

func TestAdminAPIKeyHandler_SetStatus_DisablesAnotherUsersKey(t *testing.T) {
	svc := newStubAdminService()
	svc.apiKeys = []service.APIKey{{ID: 10, UserID: 7, Status: service.StatusAPIKeyActive, Key: "sk-stored-secret"}}
	router := setupAPIKeyCredentialHandler(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/api-keys/10/status", bytes.NewBufferString(`{"status":"disabled"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, service.StatusAPIKeyDisabled, svc.apiKeys[0].Status)
	require.NotContains(t, rec.Body.String(), "sk-stored-secret", "no read path may return a credential")
}

func TestAdminAPIKeyHandler_SetStatus_RejectsUnknownStatus(t *testing.T) {
	router := setupAPIKeyCredentialHandler(newStubAdminService())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/api-keys/10/status", bytes.NewBufferString(`{"status":"quota_exhausted"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAdminAPIKeyHandler_Delete_RemovesAnotherUsersKey(t *testing.T) {
	svc := newStubAdminService()
	svc.apiKeys = []service.APIKey{{ID: 10, UserID: 7, Status: service.StatusAPIKeyActive, Key: "sk-stored-secret"}}
	router := setupAPIKeyCredentialHandler(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/api-keys/10", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, svc.apiKeys)
}

func TestAdminAPIKeyHandler_RotateCredential_ReturnsTheSecretExactlyOnce(t *testing.T) {
	svc := newStubAdminService()
	svc.apiKeys = []service.APIKey{{ID: 10, UserID: 7, Status: service.StatusAPIKeyActive, Key: "sk-previous-secret"}}
	router := setupAPIKeyCredentialHandler(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/api-keys/10/credentials/rotate", bytes.NewBufferString(`{"grace_ttl_hours":6,"revoke_previous":true}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	data := decodeAdminIssue(t, rec.Body.String())
	require.Equal(t, "rotated-secret", data["secret"])
	apiKey, ok := data["api_key"].(map[string]any)
	require.True(t, ok)
	require.NotContains(t, apiKey["key"], "rotated-secret")
	require.NotContains(t, apiKey["key"], "sk-previous-secret")
}

func TestAdminAPIKeyHandler_RotateCredential_RejectsNegativeGraceWindow(t *testing.T) {
	router := setupAPIKeyCredentialHandler(newStubAdminService())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/api-keys/10/credentials/rotate", bytes.NewBufferString(`{"grace_ttl_hours":-1}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAdminAPIKeyHandler_RotateCredential_KeyNotFound(t *testing.T) {
	router := setupAPIKeyCredentialHandler(newStubAdminService())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/api-keys/999/credentials/rotate", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)

}
