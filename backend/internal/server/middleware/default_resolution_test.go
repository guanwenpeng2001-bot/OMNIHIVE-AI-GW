package middleware

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type restoredWorkspaceRepo struct {
	service.WorkspaceRepository
	id    int64
	calls int
}

func (s *restoredWorkspaceRepo) DefaultWorkspaceID(context.Context) (int64, error) {
	s.calls++
	if s.id == 0 {
		return 0, errors.New("missing Default")
	}
	return s.id, nil
}
func TestDefaultWorkspaceResolutionBoundaries(t *testing.T) {
	repo := &restoredWorkspaceRepo{id: 17}
	engine := gin.New()
	engine.Use(DefaultWorkspaceResolution(service.NewWorkspaceService(repo)))
	engine.GET("/global", func(c *gin.Context) {
		_, scoped := domain.WorkspaceScope(c.Request.Context())
		require.False(t, scoped)
		c.Status(200)
	})
	engine.GET("/owner", DefaultWorkspaceScope(), func(c *gin.Context) {
		ctx := c.Request.Context()
		id, ok := domain.WorkspaceScope(ctx)
		require.True(t, ok)
		require.EqualValues(t, 17, id)
		require.Equal(t, "user:7", domain.WorkspaceActorScope(ctx, "user:7"))
		c.Status(200)
	})
	call := func(path string) int {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		return rec.Code
	}
	require.Equal(t, 200, call("/global"))
	require.Zero(t, repo.calls)
	require.Equal(t, 200, call("/owner"))
	require.Equal(t, 1, repo.calls)
	repo.id = 0
	require.Equal(t, 503, call("/owner"))
	require.Equal(t, 200, call("/global"))
}
