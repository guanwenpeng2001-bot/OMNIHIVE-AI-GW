//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// Synthetic discovery responses exercise the production sync parser. They do
// not attest to vendor support or invoke any generation/media endpoint.
func TestAccountModelCatalogEndpointSyncFixtures(test *testing.T) {
	test.Setenv("MODEL_CAPABILITY_OFFLINE", "1")
	endpoints := []string{"chat_completions", "responses", "messages", "generate_content", "embeddings", "images", "videos", "alpha_search", "live", "grok_media_generation"}
	require.Len(test, endpoints, 10)
	for _, endpoint := range endpoints {
		for _, scenario := range []string{"complete", "empty", "paged", "failure"} {
			test.Run(endpoint+"/"+scenario, func(test *testing.T) {
				account := &Account{ID: 101, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://gateway.example/v1"}, Extra: map[string]any{}}
				before, err := json.Marshal(account)
				require.NoError(test, err)
				entry := fmt.Sprintf(`{"id":"fixture-target","supported_methods":[%q]}`, endpoint)
				body := `{"data":[` + entry + `]}`
				if scenario == "empty" {
					body = `{"data":[]}`
				}
				if scenario == "paged" {
					body = `{"data":[` + entry + `],"has_more":true,"last_id":"fixture-target"}`
				}
				response := catalogFixtureResponse(body)
				if scenario == "failure" {
					response.StatusCode = 503
				}
				transport := &httpUpstreamRecorder{responses: []*http.Response{response, catalogFixtureResponse(`{"data":[]}`)}}
				service := &AccountTestService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
				catalog, err := service.SyncUpstreamModelCatalog(context.Background(), account)
				if scenario == "failure" {
					require.Error(test, err)
					require.Nil(test, catalog)
				} else {
					require.NoError(test, err)
					require.True(test, catalog.Complete)
					if scenario == "empty" {
						require.Empty(test, catalog.DiscoveredModels)
					} else {
						require.Equal(test, []string{"fixture-target"}, catalog.DiscoveredModels)
						require.Equal(test, []string{endpoint}, catalog.Metadata["fixture-target"].SupportedMethods)
					}
				}
				after, err := json.Marshal(account)
				require.NoError(test, err)
				require.JSONEq(test, string(before), string(after))
			})
		}
	}
}
