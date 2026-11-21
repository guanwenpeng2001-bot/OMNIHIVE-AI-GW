package routes

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/admin"
	"github.com/stretchr/testify/require"
)

func TestWorkBuddyOAuthRouteContract(t *testing.T) {
	router := gin.New()
	h := &handler.Handlers{Admin: &handler.AdminHandlers{WorkBuddyOAuth: admin.NewWorkBuddyOAuthHandler(nil)}}
	registerWorkBuddyOAuthRoutes(router.Group("/api/v1/admin"), h)
	registerWorkBuddyOAuthRoutes(router.Group("/api/v1/workspaces/:workspace_id"), h)
	paths := map[string]string{}
	for _, route := range router.Routes() {
		paths[route.Path] = route.Method
	}
	for _, prefix := range []string{"/api/v1/admin", "/api/v1/workspaces/:workspace_id"} {
		for _, suffix := range []string{"start", "poll"} {
			require.Equal(t, "POST", paths[prefix+"/workbuddy/oauth/"+suffix])
		}
	}
}
