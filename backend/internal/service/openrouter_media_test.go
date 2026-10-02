//go:build unit

package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestOpenRouterDiscoveredMediaModalitiesProjectInstalledFamilies(t *testing.T) {
	account := openRouterFixtureAccount()
	for _, fixture := range []struct {
		input, output []string
		endpoint      string
	}{
		{[]string{"text"}, []string{"audio"}, "speech"}, {[]string{"audio"}, []string{"text"}, "transcriptions"}, {[]string{"text"}, []string{"embeddings"}, "embeddings"}, {[]string{"text"}, []string{"rerank"}, "rerank"}, {[]string{"text"}, []string{"image"}, "images"}, {[]string{"text"}, []string{"video"}, "images"},
	} {
		metadata := UpstreamModelMetadata{ID: "vendor/future:variant", InputModalities: fixture.input, OutputModalities: fixture.output}
		snapshot := projectAccountModelCatalog(account, &UpstreamModelCatalog{Models: []string{metadata.ID}, Metadata: map[string]UpstreamModelMetadata{metadata.ID: metadata}})
		require.Len(t, snapshot.EligibleModels, 1)
		require.Contains(t, snapshot.EligibleModels[0].Endpoints, fixture.endpoint)
	}
	require.Equal(t, []string{"chat_completions"}, accountCatalogModelEndpoints(account, "vendor/unknown", UpstreamModelMetadata{}))
}

func TestOpenRouterVideoContentCannotFollowStoredURLsOrChangedEgress(t *testing.T) {
	account := openRouterFixtureAccount()
	account.Status = StatusActive
	gateway := &OpenAIGatewayService{accountRepo: openAIVideoAccountRepository{account: account}}
	job := &UpstreamJob{Platform: PlatformOpenRouter, Family: "video", AccountID: account.ID, Status: "succeeded", UpstreamTaskID: "private-job", ResultRef: "https://foreign.example/video"}
	_, err := gateway.OpenOpenRouterVideoContent(context.Background(), job)
	require.Error(t, err)
	job.ResultRef = job.UpstreamTaskID
	groupID := int64(9)
	account.ProxyGroupID = &groupID
	_, err = gateway.OpenOpenRouterVideoContent(context.Background(), job)
	require.ErrorIs(t, err, ErrProxyGroupUnavailable)
	account.ProxyGroupID = nil
	account.Type = AccountTypeOAuth
	_, err = gateway.OpenOpenRouterVideoContent(context.Background(), job)
	require.Error(t, err)
}

func TestOpenRouterVideoDurationTranslationValidation(t *testing.T) {
	for _, body := range []string{`{"model":"vendor/future","seconds":"8"}`, `{"model":"vendor/future","duration":8}`, `{"model":"vendor/future","seconds":8,"duration":8}`} {
		require.NoError(t, ValidateOpenRouterVideoRequest([]byte(body)))
	}
	for _, body := range []string{`{"model":"vendor/future","seconds":"8","duration":4}`, `{"model":"vendor/future","seconds":"8.5"}`, `{"model":"vendor/future","seconds":false}`} {
		require.Error(t, ValidateOpenRouterVideoRequest([]byte(body)))
	}
}
