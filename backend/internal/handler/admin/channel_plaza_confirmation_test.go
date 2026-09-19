//go:build unit

package admin

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type plazaHandlerRepository struct {
	service.ChannelRepository
	saved   bool
	actor   int64
	failure error
}

func (repository *plazaHandlerRepository) ListAll(context.Context) ([]service.Channel, error) {
	return nil, nil
}

func (repository *plazaHandlerRepository) PlazaCandidates(context.Context, int64, int64) (*service.PlazaCandidateView, error) {
	return &service.PlazaCandidateView{Revision: "current"}, nil
}
func (repository *plazaHandlerRepository) ConfirmPlazaModels(_ context.Context, _ int64, _ service.PlazaConfirmationInput, actor int64) error {
	repository.actor = actor
	repository.saved = repository.failure == nil
	return repository.failure
}

func TestChannelPlazaConfirmationAdminContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, role := range []string{service.RoleAdmin, service.RoleUser, ""} {
		t.Run("identity-"+role, func(t *testing.T) {
			repository := &plazaHandlerRepository{}
			handler := NewChannelHandler(service.NewChannelService(repository, nil, nil, nil, nil), nil, nil)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 17})
				c.Set(string(middleware.ContextKeyUserRole), role)
			})
			router.GET("/channels/:id", handler.GetByID)
			router.PUT("/channels/:id", handler.Update)
			read := httptest.NewRecorder()
			router.ServeHTTP(read, httptest.NewRequest("GET", "/channels/1?plaza_group_id=2", nil))
			write := httptest.NewRecorder()
			request := httptest.NewRequest("PUT", "/channels/1?plaza_confirmation=true", strings.NewReader(`{"group_id":2,"revision":"current","models":[],"confirmed_by":999}`))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(write, request)
			if role == service.RoleAdmin {
				require.Equal(t, 200, read.Code)
				require.Equal(t, 200, write.Code)
				require.True(t, repository.saved)
				require.Equal(t, int64(17), repository.actor)
			} else {
				require.Equal(t, 403, read.Code)
				require.Equal(t, 403, write.Code)
				require.False(t, repository.saved)
			}
		})
	}
}
