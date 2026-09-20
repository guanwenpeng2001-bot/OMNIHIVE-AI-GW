package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQianwenNativeDiscoveryTraversesAllPages(test *testing.T) {
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		catalogFixtureResponse(`{"success":true,"output":{"total":2,"page_no":1,"models":[{"model":"first"}]}}`),
		catalogFixtureResponse(`{"success":true,"output":{"total":2,"page_no":2,"models":[{"model":"second"}]}}`),
	}}
	service := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
	account := &Account{Platform: PlatformQianwen, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}
	models, err := service.FetchUpstreamSupportedModels(context.Background(), account)
	require.NoError(test, err)
	require.Equal(test, []string{"first", "second"}, models)
	require.Equal(test, "2", upstream.lastReq.URL.Query().Get("page_no"))
}

func TestQianwenPartialDiscoveryPreservesValidatedPages(test *testing.T) {
	for _, failedBody := range []string{
		`{"success":false,"data":[{"id":"not-valid"}]}`,
		`{"success":true,"output":{"total":3,"page_no":2,"models":[{"model":"not-valid"}]}}`,
		`{"success":true,"output":{"total":2,"page_no":1,"models":[{"model":"not-valid"}]}}`,
	} {
		upstream := &httpUpstreamRecorder{responses: []*http.Response{
			catalogFixtureResponse(`{"success":true,"output":{"total":2,"page_no":1,"models":[{"model":"first"}]}}`),
			catalogFixtureResponse(failedBody),
		}}
		service := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
		account := &Account{Platform: PlatformQianwen, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}
		preview, err := service.PreviewAccountModelCatalog(context.Background(), account)
		require.NoError(test, err)
		require.Equal(test, []string{"first"}, preview.DiscoveredModels)
		require.False(test, preview.DiscoveryComplete)
		require.Nil(test, preview.Review)
	}
}

func TestQianwenUnknownContinuationIsPartial(test *testing.T) {
	upstream := &httpUpstreamRecorder{resp: catalogFixtureResponse(`{"data":[{"id":"first"}],"next":"https://other.example/models"}`)}
	service := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
	account := &Account{Platform: PlatformQianwen, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}
	preview, err := service.PreviewAccountModelCatalog(context.Background(), account)
	require.NoError(test, err)
	require.Equal(test, []string{"first"}, preview.DiscoveredModels)
	require.False(test, preview.DiscoveryComplete)
	require.Nil(test, preview.Review)
	require.NotEqual(test, "other.example", upstream.lastReq.URL.Hostname())
}
