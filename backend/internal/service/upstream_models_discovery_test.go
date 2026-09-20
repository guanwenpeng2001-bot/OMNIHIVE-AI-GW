package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiscoveryPaginationAcrossEntrypoints(test *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformAnthropic, PlatformMistral, PlatformGemini} {
		test.Run(platform, func(test *testing.T) {
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				catalogFixtureResponse(`{"data":[{"id":"first"}],"next":"?cursor=second"}`),
				catalogFixtureResponse(`{"data":[{"id":"second"}]}`),
			}}
			service := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
			account := &Account{Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://relay.example/v1"}}
			models, err := service.FetchUpstreamSupportedModels(context.Background(), account)
			require.NoError(test, err)
			require.Equal(test, []string{"first", "second"}, models)
			require.Equal(test, "second", upstream.lastReq.URL.Query().Get("cursor"))
		})
	}
}

func TestDiscoveryPartialPreviewPreservesEvidence(test *testing.T) {
	for _, nextPage := range []string{
		`{"data":[{"id":"second"}],"next":"https://other.example/models"}`,
		`{"data":[{"id":"second"}],"next":"?cursor=second"}`,
		`{"data":[{"id":"second"}],"has_more":true}`,
		`{"error":"invalid"}`,
	} {
		test.Run(nextPage, func(test *testing.T) {
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				catalogFixtureResponse(`{"data":[{"id":"first"}],"next":"?cursor=second"}`),
				catalogFixtureResponse(nextPage),
			}}
			service := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
			account := &Account{Platform: PlatformMistral, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://relay.example/v1"}}
			catalog, err := service.PreviewAccountModelCatalog(context.Background(), account)
			require.NoError(test, err)
			require.False(test, catalog.DiscoveryComplete)
			require.False(test, catalog.Complete)
			require.Contains(test, catalog.DiscoveredModels, "first")
			require.Empty(test, catalog.EligibleModels)
			require.Empty(test, catalog.Models)
			require.Nil(test, catalog.Review)
			require.NotEmpty(test, catalog.Warnings)
			require.Equal(test, "relay.example", upstream.lastReq.URL.Host)
		})
	}
}

func TestDiscoveryOfficialCandidateFallback(test *testing.T) {
	for _, platform := range []string{PlatformHunyuan, PlatformVolcengine} {
		test.Run(platform, func(test *testing.T) {
			upstream := &httpUpstreamRecorder{}
			service := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
			account := &Account{Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "model_mapping": map[string]any{"public": "account-declared-model"}}}
			catalog, err := service.PreviewAccountModelCatalog(context.Background(), account)
			require.NoError(test, err)
			require.Equal(test, "platform-candidates", catalog.CatalogSource)
			require.NotEmpty(test, catalog.CandidateModels)
			require.Empty(test, catalog.DiscoveredModels)
			require.Empty(test, catalog.EligibleModels)
			require.False(test, catalog.DiscoveryComplete)
			require.Nil(test, upstream.lastReq)
		})
	}
}

func TestUpstreamBillingProbeMissingOfficialDomainsNoRequests(test *testing.T) {
	for _, baseURL := range []string{"https://ark.cn-beijing.volces.com/api/v3", "https://qianfan.baidubce.com/v2", "https://api.hunyuan.cloud.tencent.com/v1", "https://api.mistral.ai/v1", "https://bedrock.us-east-1.amazonaws.com", "https://api.openai.com/v1", ""} {
		test.Run(baseURL, func(test *testing.T) {
			account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Credentials: map[string]any{"api_key": "fixture", "base_url": baseURL}}
			repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{42: account}}
			upstream := &httpUpstreamRecorder{}
			service := newUpstreamBillingProbeTestService(repo, upstream, &upstreamBillingProbeSettingRepo{})
			snapshot, err := service.ProbeAccount(context.Background(), 42)
			require.NoError(test, err)
			require.Equal(test, UpstreamBillingProbeStatusUnsupported, snapshot.Status)
			require.Nil(test, upstream.lastReq)
		})
	}
	require.False(test, upstreamBillingProbeTargetIsOfficialAPI("https://mistral.ai.relay.example"))
	require.False(test, upstreamBillingProbeTargetIsOfficialAPI("https://notamazonaws.com"))
}

func TestCapabilityProfilesOfficialAndRelayBoundaries(test *testing.T) {
	for host, profile := range map[string]string{
		"open.bigmodel.cn": "zhipu", "api.minimax.io": "minimax", "qianfan.baidubce.com": "qianfan",
		"api.hunyuan.cloud.tencent.com": "hunyuan", "ark.cn-beijing.volces.com": "volcengine",
		"api.mistral.ai": "mistral", "bedrock-runtime.us-east-1.amazonaws.com": "bedrock",
	} {
		account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://" + host + "/v1"}}
		require.Equal(test, profile, resolveCapabilityProfile(account), host)
		registry := map[string]modelsDevProvider{profile: {ID: profile, Models: map[string]modelsDevModel{"fixture": {ID: "fixture"}}}}
		matched, found := matchModelsDevProviderByKnownHost(registry, account.GetCredential("base_url"))
		require.True(test, found, host)
		require.Equal(test, profile, matched.ID)
		account.Credentials["base_url"] = "https://" + host + ".relay.example/v1"
		require.Contains(test, resolveCapabilityProfile(account), "custom:", host)
	}
}

func TestDiscoveryPartialHTTPFailureDoesNotRevoke(test *testing.T) {
	old := AccountModelCatalogSnapshot{Complete: true, Revision: 7, DiscoveredModels: []string{"old"}, EligibleModels: []AccountCatalogModel{{ID: "old"}}}
	account := &Account{ID: 73, Platform: PlatformMistral, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}, Extra: map[string]any{UpstreamModelCatalogExtraKey: old}}
	failure := catalogFixtureResponse(`{"error":"unavailable"}`)
	failure.StatusCode = http.StatusServiceUnavailable
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		catalogFixtureResponse(`{"data":[{"id":"first"}],"has_more":true,"last_id":"first"}`), failure,
	}}
	service := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
	preview, err := service.PreviewAccountModelCatalog(context.Background(), account)
	require.NoError(test, err)
	require.Equal(test, []string{"first"}, preview.DiscoveredModels)
	require.Nil(test, preview.Review)
	require.EqualValues(test, 7, account.GetUpstreamModelCatalog().Revision)
	require.Equal(test, []string{"old"}, account.GetUpstreamModelCatalog().DiscoveredModels)
	require.Equal(test, "first", upstream.lastReq.URL.Query().Get("after"))
}

func TestDiscoveryMistralCursorAndOverlappingPages(test *testing.T) {
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		catalogFixtureResponse(`{"data":[{"id":"first"}],"next_cursor":"second"}`),
		catalogFixtureResponse(`{"data":[{"id":"first"},{"id":"second"}]}`),
	}}
	service := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
	account := &Account{Platform: PlatformMistral, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}
	models, err := service.FetchUpstreamSupportedModels(context.Background(), account)
	require.NoError(test, err)
	require.Equal(test, []string{"first", "second"}, models)
	require.Equal(test, "second", upstream.lastReq.URL.Query().Get("cursor"))
}

func TestDiscoveryCandidatesDoNotAcceptOAuthCredentials(test *testing.T) {
	for _, platform := range []string{PlatformMistral, PlatformHunyuan, PlatformVolcengine} {
		upstream := &httpUpstreamRecorder{}
		service := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
		account := &Account{Platform: platform, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "fixture"}}
		_, err := service.PreviewAccountModelCatalog(context.Background(), account)
		require.Error(test, err)
		require.Nil(test, upstream.lastReq)
	}
}

func TestDiscoveryIdentityBoundariesDoNotSendWrongCredentials(test *testing.T) {
	for _, identity := range []struct{ platform, kind string }{
		{PlatformGemini, AccountTypeServiceAccount},
		{PlatformAntigravity, AccountTypeServiceAccount},
		{PlatformMistral, AccountTypeOAuth},
		{PlatformHunyuan, AccountTypeOAuth},
		{PlatformVolcengine, AccountTypeOAuth},
		{PlatformCursor, AccountTypeServiceAccount},
		{PlatformQianfan, AccountTypeOAuth},
	} {
		upstream := &httpUpstreamRecorder{}
		service := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
		account := &Account{Platform: identity.platform, Type: identity.kind, Credentials: map[string]any{"api_key": "fixture", "access_token": "fixture"}}
		_, err := service.PreviewAccountModelCatalog(context.Background(), account)
		require.Error(test, err, identity)
		require.Nil(test, upstream.lastReq, identity)
	}
}

func TestDiscoveryOAuthAndHostedCapabilityProfiles(test *testing.T) {
	require.Equal(test, "openai-codex", resolveCapabilityProfile(&Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}))
	require.Equal(test, "bedrock", resolveCapabilityProfile(&Account{Platform: PlatformAnthropic, Type: AccountTypeBedrock}))
	account := &Account{Platform: PlatformGemini, Type: AccountTypeServiceAccount, Credentials: map[string]any{"base_url": "https://us-central1-aiplatform.googleapis.com/v1"}}
	require.Equal(test, "vertex_ai", resolveCapabilityProfile(account))
	account.Credentials["base_url"] = "https://us-central1-aiplatform.googleapis.com.relay.example/v1"
	require.Contains(test, resolveCapabilityProfile(account), "custom:")
}

func TestDiscoveryEmptyPageWithContinuationIsPartial(test *testing.T) {
	service := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: &httpUpstreamRecorder{
		resp: catalogFixtureResponse(`{"data":[],"has_more":true}`),
	}}
	account := &Account{Platform: PlatformMistral, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}
	preview, err := service.PreviewAccountModelCatalog(context.Background(), account)
	require.NoError(test, err)
	require.False(test, preview.DiscoveryComplete)
	require.Empty(test, preview.DiscoveredModels)
	require.Nil(test, preview.Review)
}
