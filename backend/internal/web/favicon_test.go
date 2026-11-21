//go:build embed

package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFaviconAndLogoCache(t *testing.T) {
	for _, withLogo := range []bool{true, false} {
		dist := fstest.MapFS{}
		if withLogo {
			dist["logo.svg"] = &fstest.MapFile{Data: []byte("<svg/>")}
		}
		server := &FrontendServer{distFS: dist}
		router := gin.New()
		router.Use(server.Middleware())
		paths := []string{"/favicon.ico"}
		if withLogo {
			paths = append(paths, "/logo.svg")
		}
		for _, path := range paths {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodHead, path, nil))
			require.Equal(t, http.StatusOK, w.Code)
			require.Empty(t, w.Body.String())
			require.NotEmpty(t, w.Header().Get("ETag"))
			require.Equal(t, "public, max-age=300", w.Header().Get("Cache-Control"))
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("If-None-Match", w.Header().Get("ETag"))
			w = httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusNotModified, w.Code)
		}
	}
}

func TestLegacyFaviconHead(t *testing.T) {
	middleware, err := ServeEmbeddedFrontend()
	require.NoError(t, err)
	router := gin.New()
	router.Use(middleware)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodHead, "/favicon.ico", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Empty(t, w.Body.String())
	require.NotEmpty(t, w.Header().Get("ETag"))
	require.Equal(t, "public, max-age=300", w.Header().Get("Cache-Control"))
}
