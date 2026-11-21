//go:build unit

package web

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestStaticRepresentationCompressionAndCaching(t *testing.T) {
	cache := newStaticResponseCache([]byte(strings.Repeat("console.log('omnihive');", 100)))
	for _, tc := range []struct {
		accept   string
		encoding string
	}{
		{accept: "br, gzip", encoding: "br"},
		{accept: "gzip", encoding: "gzip"},
		{accept: "br;q=0, gzip", encoding: "gzip"},
		{accept: "identity", encoding: ""},
	} {
		t.Run(tc.encoding, func(t *testing.T) {
			router := gin.New()
			router.GET("/assets/app-AbCd1234.js", func(c *gin.Context) {
				applyStaticAssetCacheHeaders(c.Writer.Header(), "assets/app-AbCd1234.js")
				serveStaticRepresentation(c, cache, "text/javascript; charset=utf-8")
			})
			req := httptest.NewRequest(http.MethodGet, "/assets/app-AbCd1234.js", nil)
			req.Header.Set("Accept-Encoding", tc.accept)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, tc.encoding, response.Header().Get("Content-Encoding"))
			require.Equal(t, "Accept-Encoding", response.Header().Get("Vary"))
			require.Equal(t, staticAssetsCacheControl, response.Header().Get("Cache-Control"))
			var reader io.Reader = response.Body
			switch tc.encoding {
			case "br":
				reader = brotli.NewReader(response.Body)
			case "gzip":
				gzipReader, err := gzip.NewReader(response.Body)
				require.NoError(t, err)
				reader = gzipReader
			}
			content, err := io.ReadAll(reader)
			require.NoError(t, err)
			require.Contains(t, string(content), "console.log('omnihive')")

			conditional := httptest.NewRequest(http.MethodGet, "/assets/app-AbCd1234.js", nil)
			conditional.Header.Set("Accept-Encoding", tc.accept)
			conditional.Header.Set("If-None-Match", response.Header().Get("ETag"))
			notModified := httptest.NewRecorder()
			router.ServeHTTP(notModified, conditional)
			require.Equal(t, http.StatusNotModified, notModified.Code)
			require.Empty(t, notModified.Body.Bytes())
		})
	}
}

func TestStaticRepresentationHeadOmitsBody(t *testing.T) {
	router := gin.New()
	cache := newStaticResponseCache([]byte("asset"))
	router.HEAD("/asset.js", func(c *gin.Context) { serveStaticRepresentation(c, cache, "text/javascript") })
	req := httptest.NewRequest(http.MethodHead, "/asset.js", nil)
	req.Header.Set("Accept-Encoding", "br")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	require.Equal(t, http.StatusOK, response.Code)
	require.Empty(t, response.Body.Bytes())
	require.Equal(t, "br", response.Header().Get("Content-Encoding"))
}
