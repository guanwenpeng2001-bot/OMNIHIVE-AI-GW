//go:build embed

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/server/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFrontendServer_HTMLCacheRevalidation(t *testing.T) {
	for _, nonceEnabled := range []bool{false, true} {
		name := "without_nonce"
		if nonceEnabled {
			name = "with_real_csp_nonce"
		}
		t.Run(name, func(t *testing.T) {
			provider := &mockSettingsProvider{settings: map[string]any{
				"site_name": "OmniHive Before", "site_logo": "/before.svg", "registration_enabled": false,
			}}
			server, err := NewFrontendServer(provider)
			require.NoError(t, err)
			router := gin.New()
			router.Use(middleware.SecurityHeaders(config.CSPConfig{Enabled: nonceEnabled}, nil))
			router.Use(server.Middleware())
			router.Use(func(c *gin.Context) { t.Error("HTML must abort the downstream handler") })

			request := func(path, etag string) *httptest.ResponseRecorder {
				t.Helper()
				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodGet, path, nil)
				req.Header.Set("If-None-Match", etag)
				router.ServeHTTP(w, req)
				assertNoStoreHTMLHeaders(t, w.Header())
				assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
				if w.Code == http.StatusOK {
					assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
					assert.NotContains(t, w.Body.String(), NonceHTMLPlaceholder)
					if nonceEnabled {
						match := regexp.MustCompile(`'nonce-([^']+)'`).FindStringSubmatch(w.Header().Get("Content-Security-Policy"))
						require.Len(t, match, 2)
						assert.Contains(t, w.Body.String(), `nonce="`+match[1]+`"`)
					}
				}
				return w
			}

			first := request("/", "")
			require.Equal(t, http.StatusOK, first.Code)
			etag := first.Header().Get("ETag")
			require.NotEmpty(t, etag)
			assert.Contains(t, first.Body.String(), "<title>OmniHive Before - AI API Gateway</title>")
			assert.Contains(t, first.Body.String(), `href="/before.svg"`)

			matched := request("/index.html", etag)
			if nonceEnabled {
				assert.Equal(t, http.StatusOK, matched.Code)
				assert.Equal(t, etag, matched.Header().Get("ETag"))
				assert.NotEqual(t, first.Header().Get("Content-Security-Policy"), matched.Header().Get("Content-Security-Policy"))
				assert.NotEqual(t, first.Body.String(), matched.Body.String())
			} else {
				assert.Equal(t, http.StatusNotModified, matched.Code)
				assert.Empty(t, matched.Body.String())
			}
			unmatched := request("/workspace", `"different-etag"`)
			assert.Equal(t, http.StatusOK, unmatched.Code)
			assert.Equal(t, etag, unmatched.Header().Get("ETag"))
			assert.Equal(t, 1, provider.called, "rendered settings remain cached on the server")

			provider.settings = map[string]any{
				"site_name": "OmniHive After & Team", "site_logo": "/after.svg", "registration_enabled": true,
			}
			// The existing settings-update callback invalidates this template cache.
			server.InvalidateCache()
			for _, path := range []string{"/workspace", "/usage"} {
				updated := request(path, etag)
				assert.Equal(t, http.StatusOK, updated.Code)
				require.NotEmpty(t, updated.Header().Get("ETag"))
				assert.NotEqual(t, etag, updated.Header().Get("ETag"))
				assert.Contains(t, updated.Body.String(), "<title>OmniHive After &amp; Team - AI API Gateway</title>")
				assert.Contains(t, updated.Body.String(), `href="/after.svg"`)
				assert.Contains(t, updated.Body.String(), `"registration_enabled":true`)
				assert.NotContains(t, updated.Body.String(), "OmniHive Before")
				assert.NotContains(t, updated.Body.String(), "/before.svg")
			}
			assert.Equal(t, 2, provider.called, "both cache miss and cache hit serve the updated settings")
		})
	}
}

func TestFrontendServer_HTMLCacheFallbacks(t *testing.T) {
	for name, provider := range map[string]*mockSettingsProvider{
		"settings_error": {err: context.DeadlineExceeded},
		"marshal_error":  {settings: make(chan struct{})},
	} {
		t.Run(name, func(t *testing.T) {
			server, err := NewFrontendServer(provider)
			require.NoError(t, err)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				// A prior cache policy must not survive an HTML fallback.
				for _, key := range []string{"Cache-Control", "CDN-Cache-Control", "Surrogate-Control"} {
					c.Header(key, "public, max-age=3600")
				}
				c.Set(middleware.CSPNonceKey, "fallback-nonce")
				c.Next()
			})
			router.Use(server.Middleware())
			router.Use(func(c *gin.Context) { t.Error("fallback must abort the downstream handler") })
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/workspace", nil)
			req.Header.Set("If-None-Match", `"previous-settings"`)
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
			assert.Equal(t, server.baseHTML, w.Body.Bytes(), "preserve the existing base-HTML fallback")
			assert.Empty(t, w.Header().Get("ETag"))
			assert.Nil(t, server.cache.Get(), "failures must not populate the settings cache")
			assertNoStoreHTMLHeaders(t, w.Header())
		})
	}
}

func TestFrontendServer_HTMLNoStorePreservesStaticAssets(t *testing.T) {
	for _, tc := range []struct {
		name, path, cacheControl string
		override                 bool
	}{
		{name: "hashed_js", path: "assets/index-AbCd1234.js", cacheControl: staticAssetsCacheControl},
		{name: "hashed_css", path: "assets/app-a1B2c3D4.css", cacheControl: staticAssetsCacheControl},
		{name: "unhashed_js", path: "assets/index.js"},
		{name: "logo", path: "logo.svg"},
		{name: "override_hashed_js", path: "assets/index-AbCd1234.js", override: true},
		{name: "override_logo", path: "logo.svg", override: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &mockSettingsProvider{settings: map[string]string{"site_name": "OmniHive"}}
			server, err := NewFrontendServer(provider)
			require.NoError(t, err)
			server.distFS = fstest.MapFS{tc.path: &fstest.MapFile{Data: []byte("embedded asset")}}
			server.fileServer = http.FileServer(http.FS(server.distFS))
			server.overrideDir = ""
			wantBody := "embedded asset"
			if tc.override {
				server.overrideDir = t.TempDir()
				filePath := filepath.Join(server.overrideDir, tc.path)
				require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0o755))
				wantBody = "local override"
				require.NoError(t, os.WriteFile(filePath, []byte(wantBody), 0o644))
			}
			router := gin.New()
			router.Use(server.Middleware())
			// A preceding HTML request must not change the static response policy.
			html := httptest.NewRecorder()
			router.ServeHTTP(html, httptest.NewRequest(http.MethodGet, "/", nil))
			require.Equal(t, http.StatusOK, html.Code)
			assertNoStoreHTMLHeaders(t, html.Header())

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/"+tc.path, nil)
			req.Header.Set("If-None-Match", html.Header().Get("ETag"))
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, wantBody, w.Body.String())
			assert.Equal(t, tc.cacheControl, w.Header().Get("Cache-Control"))
			for _, key := range []string{"Pragma", "Expires", "CDN-Cache-Control", "Surrogate-Control"} {
				assert.Empty(t, w.Header().Get(key), key)
			}
			assert.Equal(t, 1, provider.called, "static files do not fetch dynamic settings")
		})
	}
}
