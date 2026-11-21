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

func TestMissingFaviconDoesNotReturnSPAHTML(t *testing.T) {
	distFS := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}}
	server := &FrontendServer{distFS: distFS, baseHTML: []byte("<!doctype html>"), cache: NewHTMLCache()}
	for name, middleware := range map[string]gin.HandlerFunc{
		"settings": server.Middleware(),
		"legacy": func(c *gin.Context) {
			if c.Request.URL.Path == "/favicon.ico" {
				if _, err := distFS.Stat("favicon.ico"); err != nil {
					c.Status(http.StatusNotFound)
					c.Abort()
					return
				}
			}
			serveIndexHTML(c, distFS)
		},
	} {
		t.Run(name, func(t *testing.T) {
			router := gin.New()
			router.Use(middleware)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
			require.Equal(t, http.StatusNotFound, response.Code)
			require.NotContains(t, response.Body.String(), "<!doctype html>")
		})
	}
}
