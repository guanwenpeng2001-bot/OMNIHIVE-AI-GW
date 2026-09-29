//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountModelCatalogAntigravityAPIKeyGemini(test *testing.T) {
	test.Setenv("MODEL_CAPABILITY_OFFLINE", "1")
	account := catalogEvidenceAccount(101, PlatformAntigravity, "old-claude", "old-gemini")
	account.Credentials = map[string]any{"base_url": "https://gateway.example/antigravity", "api_key": "fixture-key"}
	messages := `{"data":[{"id":"claude-fixture","supported_methods":["messages"]}]}`
	gemini := `{"models":[{"name":"models/gemini-fixture","supportedGenerationMethods":["generateContent"],"inputTokenLimit":8192}],"nextPageToken":"next"}`
	for _, partial := range []bool{false, true} {
		name := "complete pages"
		if partial {
			name = "failed Gemini path preserves history"
		}
		test.Run(name, func(test *testing.T) {
			responses := []*http.Response{catalogFixtureResponse(messages), catalogFixtureResponse(gemini), catalogFixtureResponse(`{"models":[]}`)}
			if partial {
				responses[1] = &http.Response{StatusCode: 503, Header: make(http.Header), Body: responses[1].Body}
			}
			transport := &httpUpstreamRecorder{responses: responses}
			service := &AccountTestService{cfg: rawChatCompletionsTestConfig(), httpUpstream: transport}
			before, err := json.Marshal(account)
			require.NoError(test, err)
			preview, err := service.PreviewAccountModelCatalog(context.Background(), &account)
			require.NoError(test, err)
			if partial {
				require.False(test, preview.Complete)
				require.Nil(test, preview.Review)
				require.Empty(test, preview.Models)
				require.Equal(test, []string{"claude-fixture"}, preview.DiscoveredModels)
			} else {
				require.True(test, preview.Complete)
				require.NotNil(test, preview.Review)
				require.ElementsMatch(test, []string{"claude-fixture", "gemini-fixture"}, preview.DiscoveredModels)
				require.EqualValues(test, 8192, preview.Metadata["gemini-fixture"].ContextWindow)
				require.Equal(test, "next", transport.lastReq.URL.Query().Get("pageToken"))
			}
			require.Equal(test, "gateway.example", transport.lastReq.URL.Host)
			require.Equal(test, "/antigravity/v1beta/models", transport.lastReq.URL.Path)
			require.Equal(test, "fixture-key", transport.lastReq.Header.Get("x-goog-api-key"))
			require.Empty(test, transport.lastReq.Header.Get("x-api-key"))
			after, err := json.Marshal(account)
			require.NoError(test, err)
			require.JSONEq(test, string(before), string(after))
		})
	}
}
