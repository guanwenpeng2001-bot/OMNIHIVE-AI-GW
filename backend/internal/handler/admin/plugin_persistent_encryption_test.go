package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type processLocalPluginEncryptor struct{ pluginUIEncryptor }

func (*processLocalPluginEncryptor) EncryptionKeyConfigured() bool { return false }

func TestPluginSaveConfigReturnsActionablePersistentKeyError(t *testing.T) {
	cfg := &config.Config{Totp: config.TotpConfig{EncryptionKey: strings.Repeat("42", 32)}}
	encryptor := &processLocalPluginEncryptor{}
	manager := service.NewPluginManager(nil, encryptor, cfg, service.PluginHostInfo{})
	handler := NewPluginHandler(manager)
	router := gin.New()
	router.PUT("/plugins/:id/config", handler.SaveConfig)
	request := httptest.NewRequest(http.MethodPut, "/plugins/42/config", strings.NewReader(`{"config":{"token":"synthetic"}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), `"reason":"encryption_key_required"`)
	require.Contains(t, response.Body.String(), "TOTP_ENCRYPTION_KEY")
	require.NotContains(t, response.Body.String(), "synthetic")
}
