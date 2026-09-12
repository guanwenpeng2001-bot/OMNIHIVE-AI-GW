//go:build unit

package admin

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type compositeRouteAdminServiceStub struct {
	service.AdminService
	calls   int
	groupID int64
	routeID int64
	input   service.CompositeRouteInput
}

func (s *compositeRouteAdminServiceStub) CreateCompositeRoute(_ context.Context, groupID int64, input service.CompositeRouteInput) (*service.CompositeModelRoute, error) {
	s.calls++
	s.groupID, s.input = groupID, input
	return &service.CompositeModelRoute{ID: 9, GroupID: groupID, TargetPlatform: input.TargetPlatform}, nil
}

func (s *compositeRouteAdminServiceStub) UpdateCompositeRoute(ctx context.Context, groupID, routeID int64, input service.CompositeRouteInput) (*service.CompositeModelRoute, error) {
	s.routeID = routeID
	return s.CreateCompositeRoute(ctx, groupID, input)
}

func TestCompositeRouteHandlerTargetPlatform(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, operation := range []struct {
		name, method, path string
		status             int
	}{
		{"create", http.MethodPost, "/groups/7/composite-routes", http.StatusCreated},
		{"update", http.MethodPut, "/groups/7/composite-routes/9", http.StatusOK},
	} {
		for _, platform := range []string{"cursor", "bogus", "composite", "Cursor", "cursor ", ""} {
			t.Run(operation.name+"/"+platform, func(t *testing.T) {
				svc := &compositeRouteAdminServiceStub{}
				handler := NewGroupHandler(svc, nil, nil)
				router := gin.New()
				router.POST("/groups/:id/composite-routes", handler.CreateCompositeRoute)
				router.PUT("/groups/:id/composite-routes/:route_id", handler.UpdateCompositeRoute)
				body := fmt.Sprintf(`{"public_model":"cursor-model","target_platform":%q,"upstream_model":"default"}`, platform)
				request := httptest.NewRequest(operation.method, operation.path, bytes.NewBufferString(body))
				request.Header.Set("Content-Type", "application/json")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)

				if platform != "cursor" {
					require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
					require.Zero(t, svc.calls)
					return
				}
				require.Equal(t, operation.status, recorder.Code, recorder.Body.String())
				require.Equal(t, 1, svc.calls)
				require.Equal(t, int64(7), svc.groupID)
				if operation.method == http.MethodPut {
					require.Equal(t, int64(9), svc.routeID)
				}
				require.Equal(t, service.PlatformCursor, svc.input.TargetPlatform)
				require.Equal(t, "cursor-model", svc.input.PublicModel)
				require.Equal(t, "default", svc.input.UpstreamModel)
				require.True(t, svc.input.Enabled)
				require.Contains(t, recorder.Body.String(), `"target_platform":"cursor"`)
			})
		}
	}
}
