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
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// pluginUIEncryptor issues opaque handles and rejects any unissued ciphertext.
type pluginUIEncryptor struct {
	plaintext map[string]string
}

func (e *pluginUIEncryptor) Encrypt(plaintext string) (string, error) {
	token := fmt.Sprintf("test-ciphertext-%d", len(e.plaintext))
	e.plaintext[token] = plaintext
	return token, nil
}

func (e *pluginUIEncryptor) Decrypt(ciphertext string) (string, error) {
	plaintext, ok := e.plaintext[ciphertext]
	if !ok {
		return "", fmt.Errorf("invalid ciphertext")
	}
	return plaintext, nil
}

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

func TestPluginUIBoundary(testingT *testing.T) {
	cfg := &config.Config{}
	cfg.Plugins.DataDir = testingT.TempDir()
	encryptor := &pluginUIEncryptor{plaintext: make(map[string]string)}
	repo := &pluginUIRepository{installations: make(map[int64]*service.PluginInstallation)}
	for _, id := range []int64{42, 73} {
		installation := &service.PluginInstallation{ID: id, InstallPath: filepath.Join(cfg.Plugins.DataDir, fmt.Sprint(id))}
		require.NoError(testingT, os.MkdirAll(filepath.Join(installation.InstallPath, "ui"), 0700))
		binary := []byte("unused runtime fixture")
		digest := sha256.Sum256(binary)
		installation.BinarySHA256 = hex.EncodeToString(digest[:])
		require.NoError(testingT, os.WriteFile(filepath.Join(installation.InstallPath, "runtime"), binary, 0600))
		page := []byte(fmt.Sprintf("<h1>Plugin %d</h1>", id))
		digest = sha256.Sum256(page)
		installation.Manifest = service.PluginManifest{
			UI:       service.PluginUIManifest{Entrypoint: "ui/index.html"},
			Files:    map[string]string{"ui/index.html": hex.EncodeToString(digest[:])},
			Runtimes: map[string]service.PluginRuntime{installation.Manifest.RuntimeKey(): {Path: "runtime"}},
		}
		require.NoError(testingT, os.WriteFile(filepath.Join(installation.InstallPath, "ui", "index.html"), page, 0600))
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
	require.Equal(testingT, http.StatusOK, issued.Code)
	require.Equal(testingT, "no-store", issued.Header().Get("Cache-Control"))
	var response struct {
		Data struct {
			URL     string    `json:"url"`
			Expires time.Time `json:"expires_at"`
		} `json:"data"`
	}
	require.NoError(testingT, json.Unmarshal(issued.Body.Bytes(), &response))
	require.WithinDuration(testingT, time.Now().Add(5*time.Minute), response.Data.Expires, 2*time.Second)
	pageURL, err := url.Parse(response.Data.URL)
	require.NoError(testingT, err)
	token := strings.Split(pageURL.Path, "/")[4]
	require.NotContains(testingT, token, "bridge_token")
	resolved, err := manager.ResolveUIAssetToken(token)
	require.NoError(testingT, err)
	require.Equal(testingT, int64(42), resolved)

	testingT.Run("page and assets stay bound to issuing plugin", func(testingT *testing.T) {
		for _, suffix := range []string{"", "?plugin_id=73&audience=admin"} {
			result := request(http.MethodGet, pageURL.Path+suffix)
			require.Equal(testingT, http.StatusOK, result.Code)
			require.Equal(testingT, "<h1>Plugin 42</h1>", result.Body.String())
			require.Equal(testingT, "no-referrer", result.Header().Get("Referrer-Policy"))
			require.Contains(testingT, result.Header().Get("Content-Security-Policy"), "connect-src 'none'")
			require.Contains(testingT, result.Header().Get("Content-Security-Policy"), "sandbox allow-scripts;")
		}
		result := request(http.MethodGet, strings.TrimSuffix(pageURL.Path, "index.html")+"../73/ui/index.html")
		require.Equal(testingT, http.StatusNotFound, result.Code)
	})

	testingT.Run("rejects wrong audience expired and modified capabilities", func(testingT *testing.T) {
		raw, decodeErr := base64.RawURLEncoding.DecodeString(token)
		require.NoError(testingT, decodeErr)
		plaintext, decryptErr := encryptor.Decrypt(string(raw))
		require.NoError(testingT, decryptErr)
		const audience = "sub2api:plugin-ui:v1:"
		require.True(testingT, strings.HasPrefix(plaintext, audience))
		var claims map[string]any
		require.NoError(testingT, json.Unmarshal([]byte(strings.TrimPrefix(plaintext, audience)), &claims))
		claims["expires"] = time.Now().Add(-time.Second).Unix()
		expired, marshalErr := json.Marshal(claims)
		require.NoError(testingT, marshalErr)
		for _, payload := range []string{
			strings.Replace(plaintext, audience, "sub2api:admin-session:", 1),
			audience + string(expired),
		} {
			encrypted, encryptErr := encryptor.Encrypt(payload)
			require.NoError(testingT, encryptErr)
			invalid := base64.RawURLEncoding.EncodeToString([]byte(encrypted))
			result := request(http.MethodGet, "/api/v1/plugin-ui/"+invalid+"/index.html")
			require.Equal(testingT, http.StatusGone, result.Code)
			require.Equal(testingT, "private, no-store", result.Header().Get("Cache-Control"))
		}
		raw[len(raw)/2] ^= 1
		_, resolveErr := manager.ResolveUIAssetToken(base64.RawURLEncoding.EncodeToString(raw))
		require.Error(testingT, resolveErr)
		_, resolveErr = manager.ResolveUIAssetToken("eyJhbGciOiJIUzI1NiJ9.main.session")
		require.Error(testingT, resolveErr)
	})
}
