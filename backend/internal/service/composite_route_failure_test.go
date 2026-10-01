package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type failingCompositeRouteRepository struct {
	compositeRouteRepoStub
	fail func(context.Context) error
}

func (repo failingCompositeRouteRepository) ListByGroup(ctx context.Context, _ int64, _ bool) ([]CompositeModelRoute, error) {
	return nil, repo.fail(ctx)
}

func TestCompositeRouteFailureClassification(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, ingress := range []struct{ name, method, route, path string }{
		{"responses", http.MethodPost, "/v1/responses", "/v1/responses"},
		{"realtime", http.MethodGet, "/v1/realtime", "/v1/realtime?model=private-alias"},
		{"gemini", http.MethodPost, "/v1beta/models/*modelAction", "/v1beta/models/private-alias:generateContent"},
	} {
		for _, stage := range []string{"list_routes", "account_ownership"} {
			for _, scenario := range []struct {
				name, cause string
				status      int
			}{
				{"request_cancel", "canceled", 499},
				{"query_cancel", "canceled", 500},
				{"query_deadline", "deadline_exceeded", 500},
				{"request_deadline", "deadline_exceeded", 500},
				{"dependency_error", "dependency_error", 500},
				{"dependency_error_after_cancel", "dependency_error", 500},
			} {
				t.Run(ingress.name+"/"+stage+"/"+scenario.name, func(t *testing.T) {
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					if scenario.name == "request_deadline" {
						var expire context.CancelFunc
						ctx, expire = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
						defer expire()
					}
					calls := 0
					fail := func(queryCtx context.Context) error {
						calls++
						switch scenario.name {
						case "request_cancel":
							cancel()
							return fmt.Errorf("query failed: %w", queryCtx.Err())
						case "query_cancel":
							return fmt.Errorf("query failed: %w", context.Canceled)
						case "query_deadline", "request_deadline":
							return fmt.Errorf("query failed: %w", context.DeadlineExceeded)
						case "dependency_error_after_cancel":
							cancel()
						}
						return errors.New("sensitive SQL token=https://private.invalid/secret")
					}
					resolver := NewCompositeRouteResolver(failingCompositeRouteRepository{fail: fail})
					if stage == "account_ownership" {
						resolver = NewCompositeRouteResolver(nil)
						resolver.SetModelOwnershipResolver(func(queryCtx context.Context, _ int64, _ string) (CompositeModelOwnership, error) {
							return CompositeModelOwnership{}, fail(queryCtx)
						})
					}
					core, logs := observer.New(zap.DebugLevel)
					ctx = logger.IntoContext(ctx, zap.New(core))
					router := gin.New()
					router.Use(func(requestContext *gin.Context) {
						requestContext.Set("api_key", &APIKey{Group: &Group{ID: 73, Platform: PlatformComposite}})
					})
					middleware := CompositeTargetPlatformMiddleware(resolver)
					if ingress.name == "gemini" {
						middleware = CompositeGeminiTargetPlatformMiddleware(resolver)
					}
					router.Handle(ingress.method, ingress.route, middleware, func(*gin.Context) { t.Error("failed resolution reached forwarding") })
					request := httptest.NewRequest(ingress.method, ingress.path, strings.NewReader(`{"model":"private-alias"}`)).WithContext(ctx)
					request.Header.Set("Content-Type", "application/json")
					recorder := httptest.NewRecorder()
					router.ServeHTTP(recorder, request)
					require.Equal(t, 1, calls)
					require.Equal(t, scenario.status, recorder.Code)
					if scenario.status == 499 {
						require.Empty(t, recorder.Body.String())
						require.Zero(t, logs.Len())
						return
					}
					require.JSONEq(t, `{"error":{"type":"server_error","message":"Failed to resolve composite model route"}}`, recorder.Body.String())
					require.Equal(t, 1, logs.Len())
					fields := logs.All()[0].ContextMap()
					require.Equal(t, stage, fields["stage"])
					require.Equal(t, scenario.cause, fields["cause"])
					require.Equal(t, int64(73), fields["group_id"])
					require.NotContains(t, fmt.Sprint(logs.All()), "sensitive")
					require.NotContains(t, fmt.Sprint(logs.All()), "private.invalid")
				})
			}
		}
	}
}
