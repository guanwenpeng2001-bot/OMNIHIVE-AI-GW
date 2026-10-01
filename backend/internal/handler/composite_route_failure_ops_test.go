package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCompositeRouteFailureOpsClassification(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []struct {
		name                              string
		cancelRequest, recordCancellation bool
		cause                             error
		status                            int
		queued                            int64
	}{
		{"client_cancel", true, false, context.Canceled, 499, 0},
		{"client_cancel_recording_enabled", true, true, context.Canceled, 499, 1},
		{"internal_cancel", false, false, context.Canceled, 500, 1},
		{"internal_deadline", false, false, context.DeadlineExceeded, 500, 1},
		{"dependency_error", false, false, errors.New("database unavailable"), 500, 1},
		{"dependency_error_racing_disconnect", true, false, errors.New("database unavailable"), 500, 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			setupOpsErrorLogTestQueue(t, 2)
			var settings service.SettingRepository
			if scenario.recordCancellation {
				settings = &opsAdvancedSettingsRepoStub{advanced: `{"ignore_context_canceled":false}`}
			}
			ops := service.NewOpsService(nil, settings, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			resolver := service.NewCompositeRouteResolver(nil)
			resolver.SetModelOwnershipResolver(func(context.Context, int64, string) (service.CompositeModelOwnership, error) {
				if scenario.cancelRequest {
					cancel()
				}
				return service.CompositeModelOwnership{}, scenario.cause
			})
			router := gin.New()
			router.Use(OpsErrorLoggerMiddleware(ops), func(requestContext *gin.Context) {
				requestContext.Set("api_key", &service.APIKey{Group: &service.Group{ID: 73, Platform: service.PlatformComposite}})
			})
			router.POST("/v1/responses", service.CompositeTargetPlatformMiddleware(resolver), func(*gin.Context) { t.Error("forwarding must not run") })
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"private-alias"}`)).WithContext(ctx)
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			require.Equal(t, scenario.status, recorder.Code)
			require.Equal(t, scenario.queued, OpsErrorLogQueueLength())
			if scenario.queued > 0 {
				job := <-opsErrorLogQueue
				require.Equal(t, scenario.status, job.entry.StatusCode)
				if scenario.status == 500 {
					require.Equal(t, "gateway", job.entry.ErrorSource)
					require.Equal(t, "internal", job.entry.ErrorPhase)
					require.Equal(t, "Failed to resolve composite model route", job.entry.ErrorMessage)
				}
			}
		})
	}
}
