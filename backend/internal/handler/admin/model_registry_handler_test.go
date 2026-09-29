package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelRegistryReloadRejectsRemoteAndUnapprovedPackages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	baseline := service.NewModelRegistry().Snapshot()
	directory := t.TempDir()
	// A package's self-proclaimed approval can never replace the compiled pin.
	for _, name := range []string{"catalog.json", "release-manifest.json", "release-pin.json", "platform_model_presets.json"} {
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(`{}`), 0600))
	}
	for _, location := range []string{"https://example.com/registry", "relative-package", directory} {
		body, err := json.Marshal(map[string]string{"directory": location})
		require.NoError(t, err)
		recorder := httptest.NewRecorder()
		requestContext, _ := gin.CreateTestContext(recorder)
		requestContext.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/model-registry/reload", strings.NewReader(string(body)))
		requestContext.Request.Header.Set("Content-Type", "application/json")
		(&AccountHandler{}).ReloadModelRegistry(requestContext)
		require.Equal(t, http.StatusBadRequest, recorder.Code)
		require.Contains(t, recorder.Body.String(), "previous generation retained")
		require.Same(t, baseline, service.NewModelRegistry().Snapshot())
	}
}
