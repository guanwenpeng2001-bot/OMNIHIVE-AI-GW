//go:build unit

package handler

import (
	"github.com/gin-gonic/gin"
	middleware "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIKeyHandlerCreateRejectsMissingGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{`{"name":"test"}`, `{"name":"test","group_id":null}`, `{"name":"test","group_id":0}`, `{"name":"test","group_id":-1}`} {
		t.Run(body, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/keys", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			NewAPIKeyHandler(&service.APIKeyService{}).Create(c)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Contains(t, rec.Body.String(), "API_KEY_GROUP_REQUIRED")
			require.Contains(t, rec.Body.String(), "必须选择分组")
			require.Contains(t, rec.Body.String(), "must select a group")
		})
	}
}
