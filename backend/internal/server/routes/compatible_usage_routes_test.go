package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/admin"
	"github.com/stretchr/testify/require"
)

func TestCompatibleUsageRoutesInheritAdminAuthentication(test *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminGroup := router.Group("/api/v1/admin")
	adminGroup.Use(func(ctx *gin.Context) { ctx.AbortWithStatus(http.StatusUnauthorized) })
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{CNProvider: admin.NewCNProviderHandler(nil, nil)}}
	registerCNProviderRoutes(adminGroup, handlers)
	for _, suffix := range []string{"usage", "balance", "quota"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/cn-providers/accounts/12/"+suffix, nil))
		require.Equal(test, http.StatusUnauthorized, recorder.Code, suffix)
	}
}
