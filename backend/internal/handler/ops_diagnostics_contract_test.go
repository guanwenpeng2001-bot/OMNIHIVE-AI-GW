package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpsDiagnosticsSuppressionContract(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "sse"}[stream], func(t *testing.T) {
			setupOpsErrorLogTestQueue(t, 2)
			ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			router := gin.New()
			router.Use(OpsErrorLoggerMiddleware(ops))
			router.POST("/v1/responses", func(c *gin.Context) {
				c.Set(service.OpsSkipPassthroughKey, true)
				if stream {
					service.MarkOpsStreamFailure(c, "upstream_error", "", "unavailable", 503)
					c.Status(200)
					return
				}
				c.JSON(503, gin.H{"error": gin.H{"type": "upstream_error", "message": "unavailable"}})
			})
			before := service.SnapshotOpsDiagnostics()
			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
			after := service.SnapshotOpsDiagnostics()
			require.Equal(t, uint64(1), after.IngressStarted-before.IngressStarted)
			require.Equal(t, uint64(1), after.IngressCompleted-before.IngressCompleted)
			require.Equal(t, uint64(1), after.SuppressedRuleFailures-before.SuppressedRuleFailures)
			require.Zero(t, OpsErrorLogQueueLength())
		})
	}
}

func TestOpsDiagnosticsBusinessRejectSuppression(t *testing.T) {
	setupOpsErrorLogTestQueue(t, 2)
	ops := service.NewOpsService(nil, opsDiagnosticsSettingsRepo{}, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.Use(OpsErrorLoggerMiddleware(ops))
	router.POST("/v1/responses", func(c *gin.Context) {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"type": "billing_error", "code": opsCodeInsufficientBalance, "message": "insufficient balance"}})
	})
	before := service.SnapshotOpsDiagnostics()
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
	after := service.SnapshotOpsDiagnostics()
	require.Equal(t, uint64(1), after.SuppressedSettingsFailures-before.SuppressedSettingsFailures)
}

type opsDiagnosticsSettingsRepo struct{ service.SettingRepository }

func (opsDiagnosticsSettingsRepo) GetValue(context.Context, string) (string, error) {
	return "", service.ErrSettingNotFound
}

func (opsDiagnosticsSettingsRepo) Set(context.Context, string, string) error { return nil }

func (opsDiagnosticsSettingsRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{service.SettingKeyOpsAdvancedSettings: `{"ignore_insufficient_balance_errors":true}`}, nil
}
