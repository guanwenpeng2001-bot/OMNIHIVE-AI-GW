package admin

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type pluginUIRepository struct {
	service.PluginRepository
	installations map[int64]*service.PluginInstallation
}

func (repo *pluginUIRepository) GetByID(_ context.Context, id int64) (*service.PluginInstallation, error) {
	installation, exists := repo.installations[id]
	if !exists {
		return nil, os.ErrNotExist
	}
	copy := *installation
	return &copy, nil
}

func TestPluginUIBoundary(t *testing.T) {
	cfg := &config.Config{}
	cfg.Totp.EncryptionKey = strings.Repeat("ab", 32)
	cfg.Plugins.DataDir = t.TempDir()
	encryptor, err := repository.NewAESEncryptor(cfg)
	require.NoError(t, err)
	repo := &pluginUIRepository{installations: make(map[int64]*service.PluginInstallation)}
	for _, id := range []int64{42, 73} {
		installation := &service.PluginInstallation{ID: id, InstallPath: filepath.Join(cfg.Plugins.DataDir, fmt.Sprint(id))}
		require.NoError(t, os.MkdirAll(filepath.Join(installation.InstallPath, "ui"), 0700))
		binary := []byte("unused runtime fixture")
		digest := sha256.Sum256(binary)
		installation.BinarySHA256 = hex.EncodeToString(digest[:])
		require.NoError(t, os.WriteFile(filepath.Join(installation.InstallPath, "runtime"), binary, 0600))
		page := []byte(fmt.Sprintf("<h1>Plugin %d</h1>", id))
		digest = sha256.Sum256(page)
		installation.Manifest = service.PluginManifest{
			UI:       service.PluginUIManifest{Entrypoint: "ui/index.html"},
			Files:    map[string]string{"ui/index.html": hex.EncodeToString(digest[:])},
			Runtimes: map[string]service.PluginRuntime{installation.Manifest.RuntimeKey(): {Path: "runtime"}},
		}
		require.NoError(t, os.WriteFile(filepath.Join(installation.InstallPath, "ui", "index.html"), page, 0600))
		repo.installations[id] = installation
	}
	manager := service.NewPluginManager(repo, encryptor, cfg, service.PluginHostInfo{})
	handler := NewPluginHandler(manager)
	router := gin.New()
	router.POST("/plugins/:id/ui-session", handler.CreateUISession)
	router.GET("/api/v1/plugin-ui/:token/*path", handler.ServeUIAsset)
	request := func(method, path string) *httptest.ResponseRecorder {
		result := httptest.NewRecorder()
		router.ServeHTTP(result, httptest.NewRequest(method, path, nil))
		return result
	}
	issued := request(http.MethodPost, "/plugins/42/ui-session")
	require.Equal(t, http.StatusOK, issued.Code)
	require.Equal(t, "no-store", issued.Header().Get("Cache-Control"))
	var response struct {
		Data struct {
			URL     string    `json:"url"`
			Expires time.Time `json:"expires_at"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(issued.Body.Bytes(), &response))
	require.WithinDuration(t, time.Now().Add(5*time.Minute), response.Data.Expires, 2*time.Second)
	pageURL, err := url.Parse(response.Data.URL)
	require.NoError(t, err)
	token := strings.Split(pageURL.Path, "/")[4]
	require.NotContains(t, token, "bridge_token")
	resolved, err := manager.ResolveUIAssetToken(token)
	require.NoError(t, err)
	require.Equal(t, int64(42), resolved)

	t.Run("page and assets stay bound to issuing plugin", func(t *testing.T) {
		for _, suffix := range []string{"", "?plugin_id=73&audience=admin"} {
			result := request(http.MethodGet, pageURL.Path+suffix)
			require.Equal(t, http.StatusOK, result.Code)
			require.Equal(t, "<h1>Plugin 42</h1>", result.Body.String())
			require.Equal(t, "no-referrer", result.Header().Get("Referrer-Policy"))
			require.Contains(t, result.Header().Get("Content-Security-Policy"), "connect-src 'none'")
			require.Contains(t, result.Header().Get("Content-Security-Policy"), "sandbox allow-scripts;")
		}
		result := request(http.MethodGet, strings.TrimSuffix(pageURL.Path, "index.html")+"../73/ui/index.html")
		require.Equal(t, http.StatusNotFound, result.Code)
	})

	t.Run("rejects wrong audience expired and modified capabilities", func(t *testing.T) {
		raw, decodeErr := base64.RawURLEncoding.DecodeString(token)
		require.NoError(t, decodeErr)
		plaintext, decryptErr := encryptor.Decrypt(string(raw))
		require.NoError(t, decryptErr)
		const audience = "sub2api:plugin-ui:v1:"
		require.True(t, strings.HasPrefix(plaintext, audience))
		var claims map[string]any
		require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(plaintext, audience)), &claims))
		claims["expires"] = time.Now().Add(-time.Second).Unix()
		expired, marshalErr := json.Marshal(claims)
		require.NoError(t, marshalErr)
		for _, payload := range []string{
			strings.Replace(plaintext, audience, "sub2api:admin-session:", 1),
			audience + string(expired),
		} {
			encrypted, encryptErr := encryptor.Encrypt(payload)
			require.NoError(t, encryptErr)
			invalid := base64.RawURLEncoding.EncodeToString([]byte(encrypted))
			result := request(http.MethodGet, "/api/v1/plugin-ui/"+invalid+"/index.html")
			require.Equal(t, http.StatusGone, result.Code)
			require.Equal(t, "private, no-store", result.Header().Get("Cache-Control"))
		}
		raw[len(raw)/2] ^= 1
		_, resolveErr := manager.ResolveUIAssetToken(base64.RawURLEncoding.EncodeToString(raw))
		require.Error(t, resolveErr)
		_, resolveErr = manager.ResolveUIAssetToken("eyJhbGciOiJIUzI1NiJ9.main.session")
		require.Error(t, resolveErr)
	})
}
