package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type ownedUsageRepository struct {
	service.UsageLogRepository
}

func (s *ownedUsageRepository) GetByID(_ context.Context, id int64) (*service.UsageLog, error) {
	if id == 7 {
		return &service.UsageLog{ID: id, UserID: 99}, nil
	}
	return nil, service.ErrUsageLogNotFound
}

func TestUsageOwnershipHidesResourceExistence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	usageService := service.NewUsageService(&ownedUsageRepository{}, nil, nil, nil)
	keyService := service.NewAPIKeyService(&dailyUsageAPIKeyRepoStub{keys: map[int64]*service.APIKey{
		7: {ID: 7, UserID: 99},
	}}, nil, nil, nil, nil, nil, nil)
	usageHandler := NewUsageHandler(usageService, keyService, nil, nil)
	router := gin.New()
	router.Use(func(ctx *gin.Context) {
		ctx.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
	})
	router.GET("/usage/:id", usageHandler.GetByID)
	router.GET("/usage", usageHandler.List)
	router.GET("/keys/:id/daily", usageHandler.GetMyAPIKeyDailyUsage)
	for _, paths := range [][2]string{
		{"/usage/7", "/usage/8"},
		{"/usage?api_key_id=7", "/usage?api_key_id=8"},
		{"/keys/7/daily", "/keys/8/daily"},
	} {
		t.Run(paths[0], func(t *testing.T) {
			foreign := httptest.NewRecorder()
			router.ServeHTTP(foreign, httptest.NewRequest(http.MethodGet, paths[0], nil))
			missing := httptest.NewRecorder()
			router.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, paths[1], nil))
			require.Equal(t, http.StatusNotFound, foreign.Code)
			require.Equal(t, missing.Code, foreign.Code)
			require.JSONEq(t, missing.Body.String(), foreign.Body.String())
		})
	}
}

func TestUsageListAndExportPagesKeepAuthenticatedOwner(t *testing.T) {
	repository := &userUsageRepoCapture{}
	router := newUserUsageRequestTypeTestRouter(repository)
	// CSV export consumes the same paginated endpoint as the list.
	for _, path := range []string{"/usage?user_id=99", "/usage?page=2&page_size=100&user_id=99"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, response.Code)
		require.Equal(t, int64(42), repository.listFilters.UserID)
	}
}
