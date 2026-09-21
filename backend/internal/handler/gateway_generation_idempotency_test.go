package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func generationTestRouter(t *testing.T, next gin.HandlerFunc) *gin.Engine {
	t.Helper()
	previous := service.DefaultIdempotencyCoordinator()
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(newUserMemoryIdempotencyRepoStub(), service.DefaultIdempotencyConfig()))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previous) })
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(GatewayGenerationIdempotency(func(c *gin.Context) (service.GatewayGenerationScope, error) {
		return service.GatewayGenerationScope{WorkspaceID: 3, APIKeyID: 8, Action: c.Request.URL.Path}, nil
	}))
	router.POST("/v1/responses", next)
	router.POST("/v1/videos", next)
	router.POST("/v1/messages", next)
	router.POST("/v1beta/models/test:generateContent", next)
	return router
}

func generationTestRequest(router http.Handler, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Idempotency-Key", "same-operation")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

// Local replay suppresses both dispatch and billing; it cannot guarantee
// exactly-once upstream effects or retract a vendor call made before a crash.
func TestGatewayGenerationMiddlewareReplaysOriginalResponseAndJobWithoutBilling(t *testing.T) {
	for _, path := range []string{"/v1/responses", "/v1/videos"} {
		t.Run(path, func(t *testing.T) {
			var dispatches, charges int
			router := generationTestRouter(t, func(c *gin.Context) {
				dispatches++
				body, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				require.Equal(t, `{"prompt":"hello"}`, string(body))
				plan := service.NewGatewayRequestPlan(service.GatewayRequestSpec{APIKeyID: 8, UserID: 10, ExecutionID: "original-execution"})
				c.Request = c.Request.WithContext(service.WithGatewayRequestPlan(c.Request.Context(), plan))
				if plan.Record() {
					charges++
				}
				c.Header("Set-Cookie", "credential=must-not-replay")
				c.Header("Authorization", "Bearer must-not-replay")
				c.Data(202, "application/json", []byte("{\"id\":\"original-result\",\"seed\":9007199254740993}\n"))
			})
			first := generationTestRequest(router, path, `{"prompt":"hello"}`)
			second := generationTestRequest(router, path, `{"prompt":"hello"}`)
			require.Equal(t, first.Code, second.Code)
			require.Equal(t, first.Body.String(), second.Body.String())
			require.Equal(t, "true", second.Header().Get("X-Idempotency-Replayed"))
			require.Equal(t, "original-execution", second.Header().Get("X-Idempotency-Original-Request-ID"))
			require.Empty(t, second.Header().Get("Set-Cookie"))
			require.Empty(t, second.Header().Get("Authorization"))
			require.Equal(t, 1, dispatches)
			require.Equal(t, 1, charges)
			conflict := generationTestRequest(router, path, `{"prompt":"changed"}`)
			require.Equal(t, 409, conflict.Code)
			require.Contains(t, conflict.Body.String(), `"code":"IDEMPOTENCY_KEY_CONFLICT"`)
		})
	}
}

func TestGatewayGenerationMiddlewareInflightDoesNotDispatch(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	var executions atomic.Int32
	router := generationTestRouter(t, func(c *gin.Context) {
		executions.Add(1)
		close(started)
		<-finish
		c.JSON(200, gin.H{"id": "one"})
	})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- generationTestRequest(router, "/v1/responses", "{}") }()
	<-started
	second := generationTestRequest(router, "/v1/responses", "{}")
	close(finish)
	require.Equal(t, 200, (<-done).Code)
	require.Equal(t, 409, second.Code)
	require.Contains(t, second.Body.String(), "IDEMPOTENCY_IN_PROGRESS")
	require.EqualValues(t, 1, executions.Load())
}

func TestGatewayGenerationMiddlewareInterruptedStreamAndPanicNeverRegenerate(t *testing.T) {
	for _, mode := range []string{"canceled", "panic", "finished_stream"} {
		t.Run(mode, func(t *testing.T) {
			executions := 0
			router := generationTestRouter(t, func(c *gin.Context) {
				executions++
				c.Header("Content-Type", "text/event-stream")
				_, _ = c.Writer.WriteString("data: partial\n\n")
				c.Writer.Flush()
				if mode == "panic" {
					panic("fixture")
				}
				if mode == "canceled" {
					ctx, cancel := context.WithCancel(c.Request.Context())
					c.Request = c.Request.WithContext(ctx)
					cancel()
				}
			})
			generationTestRequest(router, "/v1/responses", "{}")
			second := generationTestRequest(router, "/v1/responses", "{}")
			require.Equal(t, 409, second.Code)
			state := "interrupted"
			if mode == "finished_stream" {
				state = "stream_not_replayable"
			}
			require.Equal(t, state, second.Header().Get("X-Idempotency-State"))
			require.Contains(t, second.Body.String(), "IDEMPOTENCY_"+strings.ToUpper(state))
			require.NotContains(t, second.Body.String(), "partial")
			require.Equal(t, 1, executions)
		})
	}
}

func TestGatewayGenerationMiddlewareProtocolConflictAndStoreFailClosed(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/v1beta/models/test:generateContent"} {
		t.Run(path, func(t *testing.T) {
			router := generationTestRouter(t, func(c *gin.Context) { c.JSON(200, gin.H{"id": "one"}) })
			generationTestRequest(router, path, "{}")
			conflict := generationTestRequest(router, path, `{"changed":true}`)
			require.Equal(t, 409, conflict.Code)
			if path == "/v1/messages" {
				require.Contains(t, conflict.Body.String(), `"type":"error"`)
			} else {
				require.Contains(t, conflict.Body.String(), `"status":"FAILED_PRECONDITION"`)
				require.Contains(t, conflict.Body.String(), `"reason":"IDEMPOTENCY_KEY_CONFLICT"`)
			}
			service.SetDefaultIdempotencyCoordinator(nil)
			failed := generationTestRequest(router, path, "{}")
			require.Equal(t, 503, failed.Code)
			require.Contains(t, failed.Body.String(), "IDEMPOTENCY_STORE_UNAVAILABLE")
		})
	}
}

type generationPersistenceFailureRepo struct{ *userMemoryIdempotencyRepoStub }

func (repo generationPersistenceFailureRepo) MarkSucceeded(context.Context, int64, int, string, time.Time) error {
	return errors.New("fixture persistence failure")
}

func TestGatewayGenerationMiddlewarePersistenceFailureKeepsClaim(t *testing.T) {
	executions := 0
	router := generationTestRouter(t, func(c *gin.Context) { executions++; c.JSON(200, gin.H{"id": "one"}) })
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(generationPersistenceFailureRepo{newUserMemoryIdempotencyRepoStub()}, service.DefaultIdempotencyConfig()))
	require.Equal(t, 200, generationTestRequest(router, "/v1/responses", "{}").Code)
	retry := generationTestRequest(router, "/v1/responses", "{}")
	require.Equal(t, 409, retry.Code)
	require.Contains(t, retry.Body.String(), "IDEMPOTENCY_IN_PROGRESS")
	require.Equal(t, 1, executions)
}

func TestGatewayGenerationMiddlewareOptionalKeyAndBodyLimit(t *testing.T) {
	previous := service.DefaultIdempotencyCoordinator()
	service.SetDefaultIdempotencyCoordinator(nil)
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previous) })
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4); c.Next() })
	router.Use(GatewayGenerationIdempotency(func(c *gin.Context) (service.GatewayGenerationScope, error) {
		return service.GatewayGenerationScope{APIKeyID: 8, Action: "openai.responses"}, nil
	}))
	executions := 0
	router.POST("/v1/responses", func(c *gin.Context) { executions++; c.Status(204) })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("POST", "/v1/responses", strings.NewReader("{}")))
	require.Equal(t, 204, response.Code)
	limited := generationTestRequest(router, "/v1/responses", "12345")
	require.Equal(t, 413, limited.Code)
	require.Equal(t, 1, executions)
}
