package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestWorkBuddyOAuthHandlerValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := service.NewWorkBuddyOAuthService(&config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: true, UpstreamHosts: []string{"blocked.test"}}}}, nil)
	h := NewWorkBuddyOAuthHandler(svc)
	router := gin.New()
	router.POST("/start", h.Start)
	router.POST("/poll", h.Poll)
	for _, fixture := range []struct{ path, body string }{{"/start", "{"}, {"/poll", "{}"}, {"/start", `{"site":"global"}`}, {"/poll", `{"session_id":"unknown"}`}} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, fixture.path, strings.NewReader(fixture.body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, req)
		require.GreaterOrEqual(t, rec.Code, 400)
		require.NotContains(t, rec.Body.String(), "access_token")
		if fixture.path == "/poll" && strings.Contains(fixture.body, "unknown") {
			require.Contains(t, rec.Body.String(), "PENDING_AUTH_BROWSER_MISMATCH")
		}
	}
}
