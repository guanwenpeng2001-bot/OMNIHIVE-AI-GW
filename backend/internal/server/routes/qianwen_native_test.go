package routes

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestQianwenNativeUnsupportedFamilies(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformQianwen)
	for _, family := range []string{"image-generation", "video-generation", "audio", "3d", "realtime", "rag", "files", "batch"} {
		path := "/api/v1/services/aigc/" + family + "/generation"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(`{"model":"qwen-plus"}`)))
		require.Equal(t, 404, w.Code)
		require.Contains(t, w.Body.String(), "Unsupported DashScope operation")
	}
	for _, path := range []string{"/api/v1/tasks/task-id", "/api/v1/apps/app-id/completion", "/api/v1/files", "/api/v1/files/file-id", "/api/v1/batches", "/api/v1/realtime", "/api-ws/v1/realtime", "/api-ws/v1/inference"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(`{"model":"qwen-plus"}`)))
		require.Equal(t, 404, w.Code)
		require.Contains(t, w.Body.String(), "Unsupported DashScope operation")
	}
}

func TestQianwenNativePathsRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformQianwen)
	for _, path := range []string{service.QianwenNativeTextPath, service.QianwenNativeMultimodalPath} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(`{"model":"qwen-plus"}`)))
		require.NotEqual(t, 404, w.Code)
	}
}

func TestQianwenNativeKnowledgeSearchFailsClosed(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformQianwen)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/indices/knowledge/search", strings.NewReader(`{"agent_id":"aid-test","query":"hello"}`)))
	require.Equal(t, 404, w.Code)
	require.Contains(t, w.Body.String(), "Unsupported DashScope operation")
}

func TestQianwenNativeAsyncPathsRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformQianwen)
	for _, path := range []string{service.QianwenNative3DPath, service.QianwenNativeASRPath, service.QianwenNativeText2ImagePath} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(`{"model":"qwen-plus"}`)))
		require.Equal(t, 503, w.Code)
		require.Contains(t, w.Body.String(), "Persistent async jobs unavailable")
	}
}
