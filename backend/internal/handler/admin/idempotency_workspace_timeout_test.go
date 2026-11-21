package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

// Detached bulk writes must retain the authorized workspace context and keep
// replay records separate when the same administrator/key acts in two workspaces.
func TestAdminIdempotentTimeoutPreservesWorkspaceAndReplayIsolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMemoryIdempotencyRepoStub()
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(repo, service.DefaultIdempotencyConfig()))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(nil) })
	executions := map[int64]int{}
	call := func(workspaceID int64) *httptest.ResponseRecorder {
		requestCtx, cancel := context.WithCancel(domain.WithWorkspaceScope(context.Background(), workspaceID))
		defer cancel()
		router := gin.New()
		router.POST("/bulk", func(c *gin.Context) {
			executeAdminIdempotentJSONWithTimeout(c, "admin.pilot.bulk", map[string]int{"days": 7}, time.Hour, time.Second, func(ctx context.Context) (any, error) {
				cancel()
				require.NoError(t, ctx.Err(), "accepted operation survives client cancellation")
				id, scoped := domain.WorkspaceScope(ctx)
				require.True(t, scoped)
				require.Equal(t, workspaceID, id)
				executions[id]++
				return gin.H{"workspace_id": id}, nil
			})
		})
		request := httptest.NewRequest(http.MethodPost, "/bulk", nil).WithContext(requestCtx)
		request.Header.Set("Idempotency-Key", "same-key-across-workspaces")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		return recorder
	}
	for _, id := range []int64{2, 3} {
		first := call(id)
		require.Equal(t, http.StatusOK, first.Code, first.Body.String())
		replay := call(id)
		require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
		require.Equal(t, "true", replay.Header().Get("X-Idempotency-Replayed"))
		require.JSONEq(t, first.Body.String(), replay.Body.String())
	}
	require.Equal(t, map[int64]int{2: 1, 3: 1}, executions)
}
