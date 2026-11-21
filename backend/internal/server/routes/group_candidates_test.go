package routes

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceRejectsInvalidGroupCandidateID(t *testing.T) {
	engine := gin.New()
	engine.GET("/api/v1/workspaces/:workspace_id/groups/:id/model-allowlist-candidates", func(c *gin.Context) {
		c.Request = c.Request.WithContext(domain.WithWorkspaceScope(c.Request.Context(), 4))
	}, middleware.WorkspaceResources(nil), func(c *gin.Context) { c.Status(200) })
	for _, id := range []string{"0", "-1", "invalid"} {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/workspaces/4/groups/"+id+"/model-allowlist-candidates", nil))
		require.Equal(t, 404, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), "GROUP_NOT_FOUND")
		require.NotContains(t, rec.Body.String(), "WORKSPACE_NOT_FOUND")
	}
}
