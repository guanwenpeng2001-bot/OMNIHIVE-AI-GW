//go:build unit

package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsModelsPageNavigation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		method  string
		target  string
		headers map[string]string
		want    bool
	}{
		{name: "browser_navigation", method: http.MethodGet, target: "/models", headers: map[string]string{"Sec-Fetch-Mode": "navigate", "Accept": "text/html"}, want: true},
		{name: "html_accept_without_fetch_metadata", method: http.MethodGet, target: "/models/", headers: map[string]string{"Accept": "text/html,application/xhtml+xml"}, want: true},
		{name: "head_navigation", method: http.MethodHead, target: "/models", headers: map[string]string{"Sec-Fetch-Mode": "navigate"}, want: true},
		{name: "api_client_json", method: http.MethodGet, target: "/models", headers: map[string]string{"Accept": "application/json"}, want: false},
		{name: "api_client_without_accept", method: http.MethodGet, target: "/models", want: false},
		{name: "bearer_credential", method: http.MethodGet, target: "/models", headers: map[string]string{"Accept": "text/html", "Authorization": "Bearer sk-test"}, want: false},
		{name: "x_api_key_credential", method: http.MethodGet, target: "/models", headers: map[string]string{"Accept": "text/html", "X-Api-Key": "sk-test"}, want: false},
		{name: "goog_api_key_credential", method: http.MethodGet, target: "/models", headers: map[string]string{"Accept": "text/html", "X-Goog-Api-Key": "test"}, want: false},
		{name: "query_key_credential", method: http.MethodGet, target: "/models?key=test", headers: map[string]string{"Accept": "text/html"}, want: false},
		{name: "fetch_from_script", method: http.MethodGet, target: "/models", headers: map[string]string{"Sec-Fetch-Mode": "cors", "Accept": "text/html"}, want: false},
		{name: "model_detail_path", method: http.MethodGet, target: "/models/gpt-4o", headers: map[string]string{"Sec-Fetch-Mode": "navigate"}, want: false},
		{name: "post", method: http.MethodPost, target: "/models", headers: map[string]string{"Sec-Fetch-Mode": "navigate"}, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(tc.method, tc.target, nil)
			for key, value := range tc.headers {
				request.Header.Set(key, value)
			}
			assert.Equal(t, tc.want, isModelsPageNavigation(request))
		})
	}
}
