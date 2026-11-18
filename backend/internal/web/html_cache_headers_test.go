//go:build embed || unit

package web

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func assertNoStoreHTMLHeaders(t *testing.T, header http.Header) {
	t.Helper()
	assert.Equal(t, "no-cache, no-store, must-revalidate", header.Get("Cache-Control"))
	assert.Equal(t, "no-cache", header.Get("Pragma"))
	assert.Equal(t, "0", header.Get("Expires"))
	assert.Equal(t, "no-store", header.Get("CDN-Cache-Control"))
	assert.Equal(t, "no-store", header.Get("Surrogate-Control"))
}

func TestApplyHTMLNoCacheHeaders(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "empty"
		if existing {
			name = "replaces_existing_cache_policy"
		}
		t.Run(name, func(t *testing.T) {
			header := make(http.Header)
			header.Set("Content-Security-Policy", "script-src 'nonce-test'")
			header.Set("ETag", `"settings-version"`)
			if existing {
				for _, key := range []string{"Cache-Control", "Pragma", "Expires", "CDN-Cache-Control", "Surrogate-Control"} {
					header.Add(key, "public, max-age=3600")
					header.Add(key, "max-age=7200")
				}
			}
			applyHTMLNoCacheHeaders(header)
			assertNoStoreHTMLHeaders(t, header)
			for _, key := range []string{"Cache-Control", "Pragma", "Expires", "CDN-Cache-Control", "Surrogate-Control"} {
				assert.Len(t, header.Values(key), 1)
			}
			assert.Equal(t, "script-src 'nonce-test'", header.Get("Content-Security-Policy"))
			assert.Equal(t, `"settings-version"`, header.Get("ETag"))
		})
	}
}
