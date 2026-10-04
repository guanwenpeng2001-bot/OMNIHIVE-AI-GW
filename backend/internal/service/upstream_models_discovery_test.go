package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFinalReviewQianwenMalformedMetadataPreservesDiscovery(test *testing.T) {
	test.Setenv("MODEL_CAPABILITY_OFFLINE", "true")
	for name, validBody := range map[string]string{
		"compatible": `{"data":[{"id":"existing-model","modalities":{"output":["text"]},"context_window":8192}]}`,
		"native":     `{"success":true,"output":{"total":1,"page_no":1,"models":[{"model":"existing-model","modalities":{"output":["text"]},"context_window":8192}]}}`,
	} {
		test.Run(name, func(test *testing.T) {
			account := &Account{ID: 99, Platform: PlatformQianwen, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://dashscope.aliyuncs.com/compatible-mode/v1"}}
			repository := &catalogFixtureRepository{accounts: map[int64]*Account{account.ID: account}}
			invalidBody := strings.Replace(validBody, `"context_window":8192`, `"context_window":"8192"`, 1)
			upstream := &httpUpstreamRecorder{responses: []*http.Response{catalogFixtureResponse(validBody), catalogFixtureResponse(invalidBody), catalogFixtureResponse(invalidBody)}}
			service := &AccountTestService{cfg: upstreamModelSyncTestConfig(), accountRepo: repository, httpUpstream: upstream,
				modelMetadataRegistry: map[string]modelsDevProvider{"alibaba": {}}, modelMetadataRegistryAt: time.Now()}
			applied, err := service.ApplyAccountModelCatalog(context.Background(), account, nil)
			require.NoError(test, err)
			require.Len(test, applied.EligibleModels, 1)
			require.Equal(test, "existing-model", applied.EligibleModels[0].ID)
			previous := account.GetUpstreamModelCatalog()
			preview, err := service.PreviewAccountModelCatalog(context.Background(), account)
			require.NoError(test, err)
			test.Logf("discovered=%v eligible=%v discovery_complete=%v removed=%v review=%v conflicts=%v", preview.DiscoveredModels, preview.EligibleModels, preview.DiscoveryComplete, preview.Diff.Removed, preview.Review != nil, preview.Metadata["existing-model"].Conflicts)
			require.True(test, preview.DiscoveryComplete)
			require.True(test, preview.Complete)
			require.Equal(test, []string{"existing-model"}, preview.DiscoveredModels)
			require.Empty(test, preview.Diff.Removed)
			require.NotNil(test, preview.Review)
			require.Equal(test, "live", preview.CatalogSource)
			require.Contains(test, preview.Metadata["existing-model"].Conflicts, "invalid_optional_metadata")
			require.Equal(test, previous, account.GetUpstreamModelCatalog())
			_, err = service.ApplyAccountModelCatalog(context.Background(), account, nil)
			require.NoError(test, err)
			require.Equal(test, []string{"existing-model"}, account.GetUpstreamModelCatalog().DiscoveredModels)
		})
	}
}

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
			if platform == PlatformHunyuan {
				account.Credentials["base_url"] = "https://api.hunyuan.cloud.tencent.com/v1"
			}
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
func TestReviewDiscoveryRetainsIDsWhenOptionalMetadataIsMalformed(test *testing.T) {
	test.Setenv("MODEL_CAPABILITY_OFFLINE", "true")
	upstream := &httpUpstreamRecorder{resp: catalogFixtureResponse(`{"data":[{"id":"existing-model","context_window":"8192"},{"id":"other-model"}]}`)}
	service := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
	old := AccountModelCatalogSnapshot{Complete: true, Revision: 7, DiscoveredModels: []string{"existing-model"}, EligibleModels: []AccountCatalogModel{{ID: "existing-model"}}}
	account := &Account{ID: 99, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://relay.example/v1"}, Extra: map[string]any{UpstreamModelCatalogExtraKey: old}}
	preview, err := service.PreviewAccountModelCatalog(context.Background(), account)
	require.NoError(test, err)
	test.Logf("discovered=%v discovery_complete=%v removed=%v review=%v", preview.DiscoveredModels, preview.DiscoveryComplete, preview.Diff.Removed, preview.Review != nil)
	require.Contains(test, preview.DiscoveredModels, "existing-model", "a malformed optional capability must not silently revoke a model reported by the upstream")
	require.True(test, preview.DiscoveryComplete)
	require.True(test, preview.Complete)
	require.NotNil(test, preview.Review)
	require.Equal(test, "incomplete", preview.CapabilityStatus)
	require.Contains(test, preview.Metadata["existing-model"].Conflicts, "invalid_optional_metadata")
	require.Equal(test, old, *account.GetUpstreamModelCatalog(), "preview must not modify the saved selection or snapshot")
}

func TestReviewCapabilityProfileRejectsTenantHostedAWSHostname(test *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "http://bedrock.customer-bucket.s3.us-east-1.amazonaws.com/v1"}}
	require.Contains(test, resolveCapabilityProfile(account), "custom:", "a customer-controlled S3 hostname is not a Bedrock regional endpoint")
	for _, host := range []string{"bedrock.us-east-1.amazonaws.com", "bedrock-runtime.cn-north-1.amazonaws.com.cn", "bedrock-runtime-fips.us-gov-west-1.amazonaws.com", "bedrock-runtime.us-east-1.api.aws"} {
		account.Credentials["base_url"] = "https://" + host
		require.Equal(test, "bedrock", resolveCapabilityProfile(account), host)
		require.True(test, upstreamBillingProbeTargetIsOfficialAPI(account.GetCredential("base_url")), host)
	}
}

func TestReviewBillingDoesNotSkipTenantAWSRelay(test *testing.T) {
	baseURL := "http://ec2-192-0-2-142.us-east-2.compute.amazonaws.com/v1"
	require.False(test, upstreamBillingProbeTargetIsOfficialAPI(baseURL), "an EC2 instance running a custom relay is not a model-provider API")
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
		Credentials: map[string]any{"api_key": "fixture", "base_url": baseURL}}
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{42: account}}
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": []string{"application/json"}}, Body: upstreamBillingProbeValidBody()}}
	service := newUpstreamBillingProbeTestService(repo, upstream, &upstreamBillingProbeSettingRepo{})
	snapshot, err := service.ProbeAccount(context.Background(), account.ID)
	require.NoError(test, err)
	require.Equal(test, UpstreamBillingProbeStatusOK, snapshot.Status)
	require.NotNil(test, upstream.lastReq)
	require.Equal(test, baseURL+"/sub2api/billing", upstream.lastReq.URL.String())
}

func TestDiscoveryMissingOptionalMetadataRemainsComplete(test *testing.T) {
	upstream := &httpUpstreamRecorder{resp: catalogFixtureResponse(`{"data":[{"id":"existing-model"}]}`)}
	service := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://relay.example/v1"}}
	preview, err := service.PreviewAccountModelCatalog(context.Background(), account)
	require.NoError(test, err)
	require.Equal(test, []string{"existing-model"}, preview.DiscoveredModels)
	require.True(test, preview.DiscoveryComplete)
	require.True(test, preview.Complete)
	require.Equal(test, "incomplete", preview.CapabilityStatus)
}

func TestReviewOfficialPlatformCandidatesStayIncomplete(test *testing.T) {
	for _, platform := range []string{PlatformHunyuan, PlatformVolcengine} {
		upstream := &httpUpstreamRecorder{}
		service := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
		account := &Account{Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}
		if platform == PlatformHunyuan {
			account.Credentials["base_url"] = "https://api.hunyuan.cloud.tencent.com/v1"
		}
		preview, err := service.PreviewAccountModelCatalog(context.Background(), account)
		require.NoError(test, err)
		require.Equal(test, "platform-candidates", preview.CatalogSource)
		require.NotEmpty(test, preview.CandidateModels, platform)
		require.Empty(test, preview.DiscoveredModels)
		require.Empty(test, preview.EligibleModels)
		require.False(test, preview.DiscoveryComplete)
		require.False(test, preview.Complete)
		require.Nil(test, preview.Review)
		require.Nil(test, upstream.lastReq)
	}
}

// A known PAYG candidate list cannot escape into an unknown plan or a relay.
func TestNativeCandidateFallbackScope(t *testing.T) {
	for _, fixture := range []struct{ name, platform, mode, base string }{
		{"Ark coding", PlatformVolcengine, AccountModeCoding, DefaultVolcengineCodingBaseURL},
		{"Ark unknown official prefix", PlatformVolcengine, AccountModePayG, "https://ark.cn-beijing.volces.com/private"},
		{"TokenHub personal Intl", PlatformHunyuan, AccountModeCoding, "https://tokenhub-intl.tencentcloudmaas.com/plan/v3"},
		{"Messages-only relay", PlatformVolcengine, AccountModePayG, "https://relay.example/messages"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{}
			service := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
			account := nativeProfileAccount(fixture.platform, fixture.mode, fixture.base)
			account.Credentials["api_protocol"] = APIProtocolAnthropic
			catalog, err := service.PreviewAccountModelCatalog(context.Background(), account)
			require.Error(t, err)
			require.Nil(t, catalog)
			require.Nil(t, upstream.lastReq)
		})
	}
	for _, base := range []string{"https://relay.example/v1", "https://ark.cn-beijing.volces.com/private/v1"} {
		t.Run("explicit Chat "+base, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{responses: []*http.Response{catalogFixtureResponse(`{"data":[{"id":"relay-model"}]}`)}}
			service := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
			account := nativeProfileAccount(PlatformVolcengine, AccountModePayG, base)
			models, _, err := service.fetchUpstreamModelList(context.Background(), account)
			require.NoError(t, err)
			require.Equal(t, []string{"relay-model"}, models)
			require.Equal(t, base+"/models", upstream.lastReq.URL.String())
		})
	}
}

func TestModelDiscoveryTransportFailureReason(test *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformQianwen} {
		test.Run(platform, func(test *testing.T) {
			upstream := &httpUpstreamRecorder{err: context.DeadlineExceeded}
			syncService := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
			_, err := syncService.SyncUpstreamModelCatalog(context.Background(), &Account{Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "private-secret"}})
			var failure *UpstreamModelSyncError
			require.ErrorAs(test, err, &failure)
			require.Equal(test, UpstreamModelSyncErrorNetwork, failure.Kind)
			require.NotContains(test, failure.SafeMessage(), "private-secret")
		})
	}
}

func TestModelDiscoveryClosedProfilesDoNotRequestUpstream(test *testing.T) {
	for _, platform := range []string{PlatformZhipu, PlatformVolcengine, "aicodewith"} {
		test.Run(platform, func(test *testing.T) {
			upstream := &httpUpstreamRecorder{}
			syncService := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
			_, err := syncService.SyncUpstreamModelCatalog(context.Background(), &Account{Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "private-secret"}})
			var failure *UpstreamModelSyncError
			require.ErrorAs(test, err, &failure)
			require.Equal(test, UpstreamModelSyncErrorDiscoveryUnconfirmed, failure.Kind)
			require.Empty(test, upstream.requests)
		})
	}
}

func TestModelDiscoveryPartialPagesRetainFailureReason(test *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformQianwen} {
		test.Run(platform, func(test *testing.T) {
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				catalogFixtureResponse(`{"data":[{"id":"first-page"}],"has_more":true,"last_id":"first-page"}`),
				{StatusCode: 429, Body: io.NopCloser(strings.NewReader(`{"error":"private-secret"}`))},
			}}
			syncService := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
			_, err := syncService.SyncUpstreamModelCatalog(context.Background(), &Account{Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "private-secret"}})
			var failure *UpstreamModelSyncError
			require.ErrorAs(test, err, &failure)
			require.Equal(test, UpstreamModelSyncErrorRateLimited, failure.Kind)
			require.Equal(test, 429, failure.StatusCode)
			require.NotNil(test, failure.PartialCatalog)
			require.Equal(test, []string{"first-page"}, failure.PartialCatalog.DiscoveredModels)
			require.Empty(test, failure.PartialCatalog.EligibleModels)
			require.NotContains(test, failure.SafeMessage(), "private-secret")
		})
	}
}

func TestModelDiscoveryEnabledHunyuanProfilesRequestNativeList(test *testing.T) {
	test.Setenv("MODEL_CAPABILITY_OFFLINE", "true")
	for _, profile := range nativeProviderProfiles {
		if profile.Platform != PlatformHunyuan || !profile.Discovery {
			continue
		}
		test.Run(profile.Label, func(test *testing.T) {
			upstream := &httpUpstreamRecorder{resp: catalogFixtureResponse(`{"data":[{"id":"native-listed"}]}`)}
			syncService := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
			account := nativeProfileAccount(profile.Platform, profile.Mode, profile.Chat)
			catalog, err := syncService.SyncUpstreamModelCatalog(context.Background(), account)
			require.NoError(test, err)
			require.Equal(test, []string{"native-listed"}, catalog.DiscoveredModels)
			require.Len(test, upstream.requests, 1)
			require.Equal(test, strings.TrimRight(profile.Chat, "/")+"/models", upstream.requests[0].URL.String())
		})
	}
}

// Official contracts: developer.qiniu.com/aitokenapi/13379/real-time-ai-interface-api,
// doc.dmxapi.cn/lingpai.html, docs.aihubmix.com/cn/api/Models-API (legacy authenticated list).
func TestVerifiedCompatibleModelDiscoveryContracts(test *testing.T) {
	test.Setenv("MODEL_CAPABILITY_OFFLINE", "true")
	for _, scenario := range []struct{ platform, url string }{
		{"qiniu", "https://api.qnaigc.com/v1/models"},
		{"dmxapi", "https://www.dmxapi.cn/v1/models"},
		{"aihubmix", "https://aihubmix.com/v1/models"},
	} {
		test.Run(scenario.platform, func(test *testing.T) {
			upstream := &httpUpstreamRecorder{resp: catalogFixtureResponse(`{"object":"list","data":[{"id":"from-account-response","object":"model","created":1,"owned_by":"upstream"}]}`)}
			syncService := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
			account := &Account{Platform: scenario.platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key", "account_mode": "payg", "model_mapping": map[string]any{"alias": "not-discovered"}}}
			catalog, err := syncService.SyncUpstreamModelCatalog(context.Background(), account)
			require.NoError(test, err)
			require.Equal(test, []string{"from-account-response"}, catalog.DiscoveredModels)
			require.Len(test, upstream.requests, 1, "single page contract")
			require.Equal(test, scenario.url, upstream.requests[0].URL.String())
			require.Equal(test, http.MethodGet, upstream.requests[0].Method)
			require.Equal(test, "Bearer fixture-key", upstream.requests[0].Header.Get("Authorization"))
			require.Empty(test, upstream.requests[0].Header.Get("x-api-key"))
			require.Equal(test, map[string]any{"alias": "not-discovered"}, account.Credentials["model_mapping"])
		})
	}
}

func TestUnconfirmedCompatibleRegionsAndPlansStayClosed(test *testing.T) {
	for _, scenario := range []struct{ platform, mode string }{
		{"qiniu", "subscription"}, {"dmxapi_com", "payg"}, {"dmxapi_ssvip", "payg"}, {"atlascloud", "payg"}, {"atlascloud", "coding"}, {"a6api", "payg"},
	} {
		test.Run(scenario.platform+"/"+scenario.mode, func(test *testing.T) {
			upstream := &httpUpstreamRecorder{}
			syncService := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream}
			_, err := syncService.SyncUpstreamModelCatalog(context.Background(), &Account{Platform: scenario.platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key", "account_mode": scenario.mode}})
			var failure *UpstreamModelSyncError
			require.ErrorAs(test, err, &failure)
			require.Equal(test, UpstreamModelSyncErrorDiscoveryUnconfirmed, failure.Kind)
			require.Empty(test, upstream.requests, "must not fall through to a neighboring region or plan")
		})
	}
}

func TestModelDiscoveryExplicitAuthentication403(test *testing.T) {
	for _, code := range []string{"invalid_api_key", "invalid_token", "token_expired", "token_revoked", "authentication_error", "permission_denied", ""} {
		test.Run(code, func(test *testing.T) {
			for _, field := range []string{"code", "type"} {
				err := newUpstreamModelDiscoveryHTTPError(403, []byte(`{"error":{"`+field+`":"`+code+`","message":"private-secret"}}`))
				var failure *UpstreamModelSyncError
				require.ErrorAs(test, err, &failure)
				expected := UpstreamModelSyncErrorCredentials
				if code == "permission_denied" || code == "" {
					expected = UpstreamModelSyncErrorPermission
				}
				require.Equal(test, expected, failure.Kind)
				require.NotContains(test, err.Error(), "private-secret")
			}
		})
	}
}
