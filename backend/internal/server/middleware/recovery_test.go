//go:build unit

package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRecovery_PanicLogContainsInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 临时替换 DefaultErrorWriter 以捕获日志输出
	var buf bytes.Buffer
	originalWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &buf
	t.Cleanup(func() {
		gin.DefaultErrorWriter = originalWriter
	})

	r := gin.New()
	r.Use(Recovery())
	r.GET("/panic", func(c *gin.Context) {
		panic("custom panic message for test")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.RequestID, "panic-correlation-id"))
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)

	logOutput := buf.String()
	require.NotContains(t, logOutput, "custom panic message for test")
	require.Contains(t, logOutput, "type=string")
	require.Contains(t, logOutput, `request_id="panic-correlation-id"`)
	require.Contains(t, logOutput, "recovery_test.go", "日志应包含堆栈跟踪文件名")
}

func TestRecovery_DoesNotLogPanicCredentials(t *testing.T) {
	for _, panicValue := range []any{
		"Authorization: Bearer synthetic-bearer",
		`{"headers":{"Authorization":"Bearer synthetic-json"}}`,
		"Cookie: session=synthetic-cookie",
		http.Header{"Authorization": {"Bearer synthetic-map"}},
		"token: synthetic-first synthetic-tail",
	} {
		t.Run(fmt.Sprintf("%T_%v", panicValue, panicValue), func(t *testing.T) {
			var output bytes.Buffer
			originalWriter := gin.DefaultErrorWriter
			gin.DefaultErrorWriter = &output
			t.Cleanup(func() { gin.DefaultErrorWriter = originalWriter })
			router := gin.New()
			router.Use(Recovery())
			router.GET("/panic", func(*gin.Context) { panic(panicValue) })
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/panic", nil))
			require.Equal(t, http.StatusInternalServerError, recorder.Code)
			require.NotContains(t, output.String(), "synthetic-")
			require.NotContains(t, recorder.Body.String(), "synthetic-")
			require.Contains(t, output.String(), "recovery_test.go")
		})
	}
}

func TestRecovery_DoesNotLogRequestCredentials(t *testing.T) {
	previousMode := gin.Mode()
	gin.SetMode(gin.DebugMode)
	t.Cleanup(func() { gin.SetMode(previousMode) })
	var buf bytes.Buffer
	originalWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &buf
	t.Cleanup(func() { gin.DefaultErrorWriter = originalWriter })
	r := gin.New()
	r.Use(Recovery())
	r.GET("/panic", func(c *gin.Context) { panic("password=panic-secret") })
	req := httptest.NewRequest(http.MethodGet, "/panic?api_key=query-secret", nil)
	req.Header.Set("Authorization", "Bearer auth-secret")
	req.Header.Set("X-Api-Key", "header-secret")
	req.Header.Set("Cookie", "session=cookie-secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	for _, secret := range []string{"query-secret", "auth-secret", "header-secret", "cookie-secret", "panic-secret"} {
		require.NotContains(t, buf.String(), secret)
		require.NotContains(t, w.Body.String(), secret)
	}
	require.Contains(t, buf.String(), "recovery_test.go")
}

func TestRecovery(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		handler      gin.HandlerFunc
		wantHTTPCode int
		wantBody     response.Response
	}{
		{
			name: "panic_returns_standard_json_500",
			handler: func(c *gin.Context) {
				panic("boom")
			},
			wantHTTPCode: http.StatusInternalServerError,
			wantBody: response.Response{
				Code:    http.StatusInternalServerError,
				Message: infraerrors.UnknownMessage,
			},
		},
		{
			name: "no_panic_passthrough",
			handler: func(c *gin.Context) {
				response.Success(c, gin.H{"ok": true})
			},
			wantHTTPCode: http.StatusOK,
			wantBody: response.Response{
				Code:    0,
				Message: "success",
				Data:    map[string]any{"ok": true},
			},
		},
		{
			name: "panic_after_write_does_not_override_body",
			handler: func(c *gin.Context) {
				response.Success(c, gin.H{"ok": true})
				panic("boom")
			},
			wantHTTPCode: http.StatusOK,
			wantBody: response.Response{
				Code:    0,
				Message: "success",
				Data:    map[string]any{"ok": true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(Recovery())
			r.GET("/t", tt.handler)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/t", nil)
			r.ServeHTTP(w, req)

			require.Equal(t, tt.wantHTTPCode, w.Code)

			var got response.Response
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
			require.Equal(t, tt.wantBody, got)
		})
	}
}
