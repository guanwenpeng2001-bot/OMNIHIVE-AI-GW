//go:build embed || unit

package web

import "net/http"

// applyHTMLNoCacheHeaders prevents browsers and CDN edges from storing HTML
// containing dynamic settings, branding and per-request CSP nonces.
// The server-side template cache remains reusable before nonce replacement.
func applyHTMLNoCacheHeaders(header http.Header) {
	header.Set("Cache-Control", "no-cache, no-store, must-revalidate")
	header.Set("Pragma", "no-cache")
	header.Set("Expires", "0")
	header.Set("CDN-Cache-Control", "no-store")
	header.Set("Surrogate-Control", "no-store")
}
