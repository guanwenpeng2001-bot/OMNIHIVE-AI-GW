package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newAuthRoutesTestRouter(redisClient *redis.Client) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")

	RegisterAuthRoutes(
		v1,
		&handler.Handlers{
			Auth:    &handler.AuthHandler{},
			Setting: &handler.SettingHandler{},
		},
		servermiddleware.JWTAuthMiddleware(func(c *gin.Context) {
			c.Next()
		}),
		servermiddleware.AuditLogMiddleware(func(c *gin.Context) {
			c.Next()
		}),
		redisClient,
		nil,
		nil,
	)

	return router
}

func TestAuthRoutesRateLimitFailCloseWhenRedisUnavailable(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  50 * time.Millisecond,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
	})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	router := newAuthRoutesTestRouter(rdb)
	paths := []string{
		"/api/v1/auth/register",
		"/api/v1/auth/login",
		"/api/v1/auth/login/2fa",
		"/api/v1/auth/send-verify-code",
		"/api/v1/auth/oauth/pending/send-verify-code",
		"/api/v1/auth/oauth/bind-token",
		"/api/v1/auth/admin-ws-ticket",
	}

	for _, path := range paths {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.10:12345"

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusTooManyRequests, w.Code, "path=%s", path)
		require.Contains(t, w.Body.String(), "rate limit exceeded", "path=%s", path)
	}
}

// GET navigation must receive the same fail-closed protection as POST starts.
func TestOAuthGetStartRateLimitFailCloseWhenRedisUnavailable(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  50 * time.Millisecond,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
		MaxRetries:   -1,
	})
	t.Cleanup(func() { _ = redisClient.Close() })
	router := newAuthRoutesTestRouter(redisClient)
	for _, provider := range []string{"linuxdo", "github", "google", "wechat", "oidc", "dingtalk"} {
		paths := []string{"/api/v1/auth/oauth/" + provider + "/start"}
		if provider != "github" && provider != "google" {
			paths = append(paths, "/api/v1/auth/oauth/"+provider+"/bind/start")
		}
		if provider == "wechat" {
			paths = append(paths, "/api/v1/auth/oauth/wechat/payment/start")
		}
		for _, path := range paths {
			t.Run(path, func(t *testing.T) {
				request := httptest.NewRequest(http.MethodGet, path, nil)
				request.RemoteAddr = "203.0.113.10:12345"
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				require.Equal(t, http.StatusTooManyRequests, recorder.Code)
				require.Contains(t, recorder.Body.String(), "rate limit exceeded")
				require.Empty(t, recorder.Header().Get("Location"))
			})
		}
	}
}
