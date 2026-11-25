package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	antigravitypkg "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/antigravity"
	"github.com/stretchr/testify/require"
)

func TestAccountModelCatalogReviewedProductionPlatforms(t *testing.T) {
	t.Setenv("MODEL_CAPABILITY_OFFLINE", "1")
	for _, testCase := range []struct {
		name     string
		platform string
		body     string
	}{
		{name: PlatformOpenCodeGo, platform: PlatformOpenCodeGo, body: `{"data":[{"id":"private-model","context_window":1000,"max_output_tokens":100,"reasoning":false,"supports_function_calling":false}]}`},
		{name: PlatformOpenRouter, platform: PlatformOpenRouter, body: `{"data":[{"id":"vendor/model","context_length":1000,"top_provider":{"max_completion_tokens":100},"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"1","completion":"1"}}]}`},
		{name: PlatformAntigravity, platform: PlatformAntigravity, body: `{"models":{"gemini-fixture":{"maxTokens":1000}}}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			account := &Account{ID: 1, Status: StatusActive, Platform: testCase.platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test"}, Extra: map[string]any{}}
			if testCase.platform == PlatformAntigravity {
				account.Type = AccountTypeOAuth
				account.Credentials = map[string]any{"access_token": "test", "project_id": "project"}
				originalTransport := http.DefaultTransport
				t.Cleanup(func() { http.DefaultTransport = originalTransport })
				http.DefaultTransport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(testCase.body)), Request: request}, nil
				})
				originalBaseURLs := antigravitypkg.BaseURLs
				t.Cleanup(func() { antigravitypkg.BaseURLs = originalBaseURLs })
				antigravitypkg.BaseURLs = []string{antigravitypkg.BaseURLs[0]}
			}
			repository := &reviewedCatalogRepository{&catalogFixtureRepository{accounts: map[int64]*Account{account.ID: account}}}
			transport := &httpUpstreamRecorder{responses: []*http.Response{catalogFixtureResponse(testCase.body), catalogFixtureResponse(testCase.body)}}
			service := &AccountTestService{accountRepo: repository, cfg: upstreamModelSyncTestConfig(), modelMetadataRegistry: map[string]modelsDevProvider{}, modelMetadataRegistryAt: time.Now(), httpUpstream: transport}
			preview, err := service.PreviewAccountModelCatalog(context.Background(), account)
			require.NoError(t, err)
			require.NotNil(t, preview.Review)
			encoded, err := json.Marshal(preview.Review)
			require.NoError(t, err)
			var review AccountCatalogReview
			require.NoError(t, json.Unmarshal(encoded, &review))
			applied, err := service.ApplyReviewedAccountModelCatalog(context.Background(), account, &review)
			require.NoError(t, err)
			require.True(t, applied.Applied)
			require.Equal(t, 1, repository.commits)
		})
	}
}
