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

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/logger"
	"github.com/lib/pq"
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
				{"postgres_request_cancel", "dependency_error", 499},
				{"postgres_active", "dependency_error", 500},
				{"postgres_deadline", "dependency_error", 500},
				{"postgres_statement_timeout", "dependency_error", 500},
				{"postgres_statement_timeout_active", "dependency_error", 500},
				{"postgres_unrelated", "dependency_error", 500},
				{"postgres_other_locale", "dependency_error", 500},
				{"postgres_unknown", "dependency_error", 500},
				{"postgres_wrong_code", "dependency_error", 500},
				{"postgres_plain_text", "dependency_error", 500},
			} {
				t.Run(ingress.name+"/"+stage+"/"+scenario.name, func(t *testing.T) {
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					if scenario.name == "request_deadline" || scenario.name == "postgres_deadline" {
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
						if strings.HasPrefix(scenario.name, "postgres_") {
							if scenario.name != "postgres_active" && scenario.name != "postgres_deadline" && scenario.name != "postgres_statement_timeout_active" {
								cancel()
							}
							postgresErr := &pq.Error{Code: "57014", Message: "canceling statement due to user request", InternalQuery: "sensitive SQL token=https://private.invalid/secret"}
							switch scenario.name {
							case "postgres_statement_timeout", "postgres_statement_timeout_active":
								postgresErr.Message = "canceling statement due to statement timeout"
							case "postgres_unrelated":
								postgresErr.Code, postgresErr.Message = "42P01", "sensitive missing relation"
							case "postgres_other_locale":
								postgresErr.Message = "annulation de la requête à la demande de l’utilisateur"
							case "postgres_unknown":
								postgresErr.Message = "unknown cancellation"
							case "postgres_wrong_code":
								postgresErr.Code = "08006"
							case "postgres_plain_text":
								return errors.New("pq: canceling statement due to user request")
							}
							return fmt.Errorf("query failed: %w", postgresErr)
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
					require.Equal(t, "service.composite_route", fields["component"])
					requestState := "active"
					if errors.Is(ctx.Err(), context.Canceled) {
						requestState = "canceled"
					} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
						requestState = "deadline_exceeded"
					}
					require.Equal(t, requestState, fields["request_state"])
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

func TestCompositeRouteCancellationPreservesWrittenResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, cause := range []error{context.Canceled, fmt.Errorf("query: %w", &pq.Error{Code: "57014", Message: "canceling statement due to user request"})} {
		t.Run(fmt.Sprintf("%T", cause), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			resolver := NewCompositeRouteResolver(failingCompositeRouteRepository{fail: func(context.Context) error {
				cancel()
				return cause
			}})
			router := gin.New()
			router.Use(func(requestContext *gin.Context) {
				requestContext.Set("api_key", &APIKey{Group: &Group{ID: 73, Platform: PlatformComposite}})
				requestContext.String(http.StatusAccepted, "already written")
			})
			router.POST("/v1/responses", CompositeTargetPlatformMiddleware(resolver), func(*gin.Context) { t.Error("forwarding must not run") })
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"private-alias"}`)).WithContext(ctx)
			router.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusAccepted, recorder.Code)
			require.Equal(t, "already written", recorder.Body.String())
		})
	}
}
