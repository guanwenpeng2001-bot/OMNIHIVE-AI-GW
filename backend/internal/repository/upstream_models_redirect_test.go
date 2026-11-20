package repository

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type modelsDevRedirectTransport struct {
	targetURL        string
	localTransport   *http.Transport
	registryCalls    atomic.Int64
	registryDisabled atomic.Bool
}

func (transport *modelsDevRedirectTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response := func(status int, body string) *http.Response {
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}
	}
	switch request.URL.String() {
	case "https://api.openai.com/v1/models":
		return response(http.StatusOK, `{"data":[{"id":"runtime-redirect-probe"}]}`), nil
	case "https://models.dev/api.json":
		transport.registryCalls.Add(1)
		transport.registryDisabled.Store(service.HTTPUpstreamRedirectsDisabled(request.Context()))
		redirect := response(http.StatusFound, "redirect")
		redirect.Header.Set("Location", transport.targetURL)
		return redirect, nil
	case transport.targetURL:
		return transport.localTransport.RoundTrip(request)
	default:
		return nil, fmt.Errorf("unexpected URL; no external network permitted: %s", request.URL)
	}
}

func TestModelsDevRegistryRejectsRedirectWithoutCaching(t *testing.T) {
	t.Setenv("MODEL_CAPABILITY_OFFLINE", "false")
	var redirectedCalls atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		redirectedCalls.Add(1)
		assert.Empty(t, request.Header.Get("Authorization"))
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{"openai":{"models":{"runtime-redirect-probe":{"limit":{"context":999001,"output":777}}}}}`)
	}))
	defer target.Close()
	baseTransport, _ := http.DefaultTransport.(*http.Transport)
	localTransport := baseTransport.Clone()
	localTransport.Proxy = nil
	defer localTransport.CloseIdleConnections()
	transport := &modelsDevRedirectTransport{targetURL: target.URL + "/registry", localTransport: localTransport}
	upstream, _ := NewHTTPUpstream(nil).(*httpUpstreamService)
	entry, err := upstream.getOrCreateClient("", 99301, 1)
	require.NoError(t, err)
	// Substitute only network responses; the real repository HTTPUpstream,
	// http.Client redirect policy, service constructor and discovery path execute.
	entry.client.Transport = transport
	settings := &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}}
	catalogService := service.NewAccountTestService(nil, nil, nil, nil, nil, upstream, settings, nil)
	account := &service.Account{ID: 99301, Concurrency: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "fake-review-key", "base_url": "https://api.openai.com/v1"}}
	catalog, err := catalogService.DiscoverModelCapabilities(context.Background(), account, nil)
	require.NoError(t, err)
	require.NotNil(t, catalog)
	secondCatalog, err := catalogService.DiscoverModelCapabilities(context.Background(), account, nil)
	require.NoError(t, err)
	t.Logf("registry_redirects_disabled=%t local_redirect_target_hits=%d registry_downloads=%d first_context=%d second_context=%d sources=%v warnings=%v", transport.registryDisabled.Load(), redirectedCalls.Load(), transport.registryCalls.Load(), catalog.Metadata["runtime-redirect-probe"].ContextWindow, secondCatalog.Metadata["runtime-redirect-probe"].ContextWindow, catalog.MetadataSources, catalog.Warnings)
	assert.True(t, transport.registryDisabled.Load(), "registry must preserve the preexisting no-redirect policy")
	assert.Zero(t, redirectedCalls.Load(), "public registry must not follow a 302 to a different source")
	assert.Equal(t, int64(2), transport.registryCalls.Load(), "each discovery must retry the rejected registry download")
	assert.Contains(t, fmt.Sprint(catalog.Warnings), "HTTP 302")
	assert.Contains(t, fmt.Sprint(secondCatalog.Warnings), "HTTP 302")
	assert.Zero(t, catalog.Metadata["runtime-redirect-probe"].ContextWindow, "redirected bytes must not be accepted as models.dev metadata")
	assert.Zero(t, secondCatalog.Metadata["runtime-redirect-probe"].ContextWindow, "redirected bytes must not poison the shared registry cache")
}
