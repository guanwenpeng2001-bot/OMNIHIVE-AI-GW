package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type catalogUnavailableRepository struct{ service.AccountRepository }

func (*catalogUnavailableRepository) ListSchedulableByGroupID(context.Context, int64) ([]service.Account, error) {
	return nil, errors.New("database unavailable")
}
func (*catalogUnavailableRepository) ListSchedulable(context.Context) ([]service.Account, error) {
	return nil, errors.New("database unavailable")
}
func (*catalogUnavailableRepository) ModelCatalogGeneration(context.Context, string) (int64, error) {
	return 0, errors.New("database unavailable")
}
func (*catalogUnavailableRepository) GetModelCatalogAccount(context.Context, int64) (*service.Account, error) {
	return nil, errors.New("database unavailable")
}
func (*catalogUnavailableRepository) ListModelCatalogAccounts(context.Context, *int64, []string, bool) ([]service.Account, error) {
	return nil, errors.New("database unavailable")
}
func TestModelCatalogUnavailableDoesNotPublishDefaultModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{service.PlatformAnthropic, service.PlatformComposite} {
		for _, codex := range []bool{false, true} {
			handler := newGatewayModelsHandlerForTest(&catalogUnavailableRepository{})
			recorder := httptest.NewRecorder()
			request, _ := gin.CreateTestContext(recorder)
			request.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			request.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{Group: &service.Group{ID: 1, Platform: platform}})
			if codex {
				handler.CodexModels(request)
			} else {
				handler.Models(request)
			}
			require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
			require.NotContains(t, recorder.Body.String(), "data")
		}
	}
}
