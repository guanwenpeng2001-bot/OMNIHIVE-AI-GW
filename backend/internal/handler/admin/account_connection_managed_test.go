package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/response"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountUpdateManagedConnectionConflict(t *testing.T) {
	svc := newStubAdminService()
	svc.getAccountResult = &service.Account{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey}
	svc.updateAccountErr = service.ErrAccountConnectionManaged
	handler := &AccountHandler{adminService: svc}
	router := gin.New()
	router.PUT("/api/v1/admin/accounts/:id", handler.Update)
	for _, body := range []string{
		`{"name":"renamed"}`,
		`{"credentials":{"header_overrides":{"X-Example":"test"}}}`,
		`{"credentials":{"base_url":"https://new.example"}}`,
	} {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/accounts/1", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusConflict, rec.Code)
		var result response.Response
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
		require.Equal(t, "ACCOUNT_CONNECTION_MANAGED", result.Reason)
		require.Contains(t, result.Message, "Connections")
	}
}
