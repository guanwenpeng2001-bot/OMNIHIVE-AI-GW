//go:build embed || unit

package web

import (
	"net/http"
	"strings"
)

const modelPlazaPath = "/model-plaza"

// isModelsPageNavigation distinguishes a browser opening /models from an API
// client listing models on the root alias. Only credential-free top-level
// navigations are redirected; every API request keeps reaching the gateway.
func isModelsPageNavigation(request *http.Request) bool {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		return false
	}
	if strings.TrimRight(request.URL.Path, "/") != "/models" {
		return false
	}
	for _, header := range []string{"Authorization", "X-Api-Key", "X-Goog-Api-Key", "Api-Key"} {
		if strings.TrimSpace(request.Header.Get(header)) != "" {
			return false
		}
	}
	if request.URL.Query().Has("key") {
		return false
	}
	if mode := request.Header.Get("Sec-Fetch-Mode"); mode != "" {
		return strings.EqualFold(mode, "navigate")
	}
	return strings.Contains(strings.ToLower(request.Header.Get("Accept")), "text/html")
}
