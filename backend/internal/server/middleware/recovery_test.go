//go:build unit

package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

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
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)

	logOutput := buf.String()
	require.Contains(t, logOutput, "custom panic message for test", "日志应包含 panic 信息")
	require.Contains(t, logOutput, "recovery_test.go", "日志应包含堆栈跟踪文件名")
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
				Code:     http.StatusInternalServerError,
				Message:  infraerrors.UnknownMessage,
				Reason:   "INTERNAL_ERROR",
				Metadata: map[string]any{},
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

func TestRecovery_RepanicsErrAbortHandlerWithoutEnvelope(t *testing.T) {
	var output bytes.Buffer
	originalWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &output
	t.Cleanup(func() { gin.DefaultErrorWriter = originalWriter })
	for _, committed := range []bool{false, true} {
		router := gin.New()
		router.Use(Recovery())
		router.GET("/abort", func(context *gin.Context) {
			if committed {
				context.String(http.StatusOK, "partial")
			}
			panic(http.ErrAbortHandler)
		})
		recorder := httptest.NewRecorder()
		require.PanicsWithValue(t, http.ErrAbortHandler, func() {
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/abort", nil))
		})
		require.NotContains(t, recorder.Body.String(), "INTERNAL_ERROR")
	}
	require.Empty(t, output.String())
}

func TestRecovery_OtherPanicsKeepExistingEnvelope(t *testing.T) {
	for _, recovered := range []any{fmt.Errorf("wrapped: %w", http.ErrAbortHandler), []string{"non-comparable panic"}} {
		router := gin.New()
		router.Use(Recovery())
		router.GET("/panic", func(context *gin.Context) { panic(recovered) })
		recorder := httptest.NewRecorder()
		require.NotPanics(t, func() { router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/panic", nil)) })
		require.Equal(t, http.StatusInternalServerError, recorder.Code)
		require.Contains(t, recorder.Body.String(), "INTERNAL_ERROR")
	}
	router := gin.New()
	router.Use(Recovery())
	router.GET("/broken", func(context *gin.Context) {
		panic(&net.OpError{Op: "write", Err: &os.SyscallError{Syscall: "write", Err: errors.New("broken pipe")}})
	})
	recorder := httptest.NewRecorder()
	require.NotPanics(t, func() { router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/broken", nil)) })
	require.Empty(t, recorder.Body.String())
}
