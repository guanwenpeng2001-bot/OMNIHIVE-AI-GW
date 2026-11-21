package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type workspaceAuthorizationRepository struct{ service.WorkspaceRepository }

func (s *workspaceAuthorizationRepository) Role(_ context.Context, workspaceID, userID int64, globalAdmin bool) (string, error) {
	if workspaceID != 1 && workspaceID != 2 {
		return "", service.ErrWorkspaceNotFound
	}
	if globalAdmin {
		return "admin", nil
	}
	if userID == 20 && workspaceID == 2 {
		return "admin", nil
	}
	if userID == 10 && workspaceID == 2 {
		return "member", nil
	}
	return "", service.ErrWorkspaceNotFound
}

func (s *workspaceAuthorizationRepository) ContainsResources(_ context.Context, workspaceID int64, _ string, identifiers []int64) (bool, error) {
	for _, identifier := range identifiers {
		if identifier/100 != workspaceID {
			return false, nil
		}
	}
	return true, nil
}

func TestWorkspaceThreeSessionResourceMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspaces := service.NewWorkspaceService(&workspaceAuthorizationRepository{})
	for _, session := range []struct {
		name    string
		userID  int64
		role    string
		allowed bool
	}{
		{"ordinary", 10, service.RoleUser, false},
		{"delegated", 20, service.RoleUser, true},
		{"global", 30, service.RoleAdmin, true},
	} {
		for _, resource := range []string{"accounts", "groups", "channels", "proxies", "api-keys", "channel-monitors", "channel-monitor-templates", "scheduled-test-plans"} {
			t.Run(session.name+"/"+resource, func(t *testing.T) {
				router := gin.New()
				router.Use(func(ctx *gin.Context) {
					ctx.Set(string(ContextKeyUser), AuthSubject{UserID: session.userID})
					ctx.Set(string(ContextKeyUserRole), session.role)
				})
				calls := 0
				handler := func(ctx *gin.Context) {
					workspaceID, scoped := domain.WorkspaceScope(ctx.Request.Context())
					require.True(t, scoped)
					require.Equal(t, int64(2), workspaceID)
					role, _ := GetUserRoleFromContext(ctx)
					require.Equal(t, session.role, role, "delegation must not elevate the global role")
					calls++
					ctx.Status(http.StatusOK)
				}
				resources := router.Group("/workspaces/:workspace_id/"+resource, WorkspaceScope(workspaces, true), WorkspaceResources(workspaces))
				for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
					resources.Handle(method, "/:id", handler)
				}
				resources.POST("/batch", handler)
				router.GET("/admin", AdminOnly(), func(ctx *gin.Context) { ctx.Status(http.StatusOK) })
				for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
					for _, identifier := range []int{201, 101, 999} {
						response := httptest.NewRecorder()
						router.ServeHTTP(response, httptest.NewRequest(method, fmt.Sprintf("/workspaces/2/%s/%d", resource, identifier), nil))
						expected := http.StatusNotFound
						if session.allowed && identifier == 201 {
							expected = http.StatusOK
						}
						require.Equal(t, expected, response.Code)
					}
				}
				before := calls
				for _, payload := range []string{`{"ids":[201,101]}`, `{"account_id":201,"group_ids":[101]}`, `{"accounts":[{"proxy_id":101}]}`, `{"entries":[{"user_id":101,"rate_multiplier":1}]}`} {
					request := httptest.NewRequest(http.MethodPost, "/workspaces/2/"+resource+"/batch", strings.NewReader(payload))
					request.Header.Set("Content-Type", "application/json")
					response := httptest.NewRecorder()
					router.ServeHTTP(response, request)
					require.Equal(t, http.StatusNotFound, response.Code)
				}
				require.Equal(t, before, calls, "batch/import must reject before any side effect")
				adminResponse := httptest.NewRecorder()
				router.ServeHTTP(adminResponse, httptest.NewRequest(http.MethodGet, "/admin", nil))
				if session.role == service.RoleAdmin {
					require.Equal(t, http.StatusOK, adminResponse.Code)
				} else {
					require.Equal(t, http.StatusForbidden, adminResponse.Code)
				}
			})
		}
	}
}
