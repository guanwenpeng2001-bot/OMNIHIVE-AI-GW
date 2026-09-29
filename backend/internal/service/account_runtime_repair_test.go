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

// Cache misses force the providers through their authoritative version check.
type runtimeRepairTokenCache struct{ GeminiTokenCache }

func (*runtimeRepairTokenCache) GetAccessToken(context.Context, string) (string, error) {
	return "", nil
}
func (*runtimeRepairTokenCache) SetAccessToken(context.Context, string, string, time.Duration) error {
	return nil
}

func TestReviewRuntimeGrokTokenDoesNotCrossEndpoint(review *testing.T) {
	original := &Account{ID: 92001, Platform: PlatformGrok, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"access_token": "old-owner-token", "refresh_token": "old-refresh", "base_url": "https://previous-relay.example/v1", "expires_at": time.Now().Add(4 * time.Hour).Format(time.RFC3339)}}
	current := snapshotOAuthRefreshAccount(original)
	current.Credentials["access_token"] = "replacement-owner-token"
	current.Credentials["refresh_token"] = "replacement-refresh"
	current.Credentials["base_url"] = "https://replacement-relay.example/v1"
	provider := NewGrokTokenProvider(&runtimeSnapshotTestRepo{current: current}, &runtimeRepairTokenCache{})
	token, err := provider.GetAccessToken(context.Background(), original)
	require.ErrorIs(review, err, ErrAccountRuntimeIdentityChanged)
	require.Empty(review, token, "no credential may be supplied to the old relay request")
	gateway := &OpenAIGatewayService{accountRepo: &runtimeSnapshotTestRepo{current: current}, grokTokenProvider: provider}
	token, _, err = gateway.GetRequestCredential(context.Background(), nil, original)
	require.Error(review, err, "the gateway must not recover a replacement bearer through credential-failure fallback")
	require.Empty(review, token)
	class := classifyGrokCredentialFailure(original, ErrAccountRuntimeIdentityChanged)
	require.Equal(review, GrokCredentialReasonAccountChanged, class.reason)
	require.False(review, class.transient)
	require.False(review, class.permanent)

}

func TestReviewRuntimeModelsDevCacheSeparatesProxy(review *testing.T) {
	review.Setenv("MODEL_CAPABILITY_OFFLINE", "false")
	response := func(name string) *http.Response {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"provider":{"id":"` + name + `","models":{}}}`))}
	}
	upstream := &httpUpstreamRecorder{responses: []*http.Response{response("first-proxy"), response("second-proxy")}}
	service := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
	firstProxyID, secondProxyID := int64(91001), int64(91002)
	first := &Account{ID: 91001, ProxyID: &firstProxyID, Proxy: &Proxy{Protocol: "http", Host: "first.proxy.example", Port: 8080}}
	second := &Account{ID: 91002, ProxyID: &secondProxyID, Proxy: &Proxy{Protocol: "http", Host: "second.proxy.example", Port: 8080}}
	_, err := service.fetchModelsDevRegistry(context.Background(), first)
	require.NoError(review, err)
	_, err = service.fetchModelsDevRegistry(context.Background(), second)
	require.NoError(review, err)
	require.Len(review, upstream.requests, 2, "second proxy/trust scope must perform its own fetch instead of reading first scope cache")
}

func TestReviewRuntimeInvalidatorClearsCredentialSchedulingBlock(review *testing.T) {
	original := &Account{ID: 93001, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "old"}}
	current := snapshotOAuthRefreshAccount(original)
	current.Credentials["api_key"] = "replacement"
	gateway := &OpenAIGatewayService{}
	gateway.BlockAccountScheduling(original, time.Now().Add(time.Hour), openAIAPIKeyHealthBreakerReason)
	invalidator := &AccountRuntimeInvalidator{Gateway: gateway}
	require.NoError(review, invalidator.Invalidate(context.Background(), original, current))
	require.False(review, gateway.isOpenAIAccountRuntimeBlocked(current), "new identity must not retain the previous credential health block")
	require.False(review, gateway.isOpenAIAccountRuntimeBlocked(original), "invalidation removes the old observation")
	gateway.BlockAccountScheduling(original, time.Now().Add(time.Hour), openAIAPIKeyHealthBreakerReason)
	require.False(review, gateway.isOpenAIAccountRuntimeBlocked(current), "late old failures cannot block the new identity")
	gateway.BlockAccountScheduling(current, time.Now().Add(time.Hour), openAIAPIKeyHealthBreakerReason)
	require.NoError(review, invalidator.Invalidate(context.Background(), original, current))
	require.True(review, gateway.isOpenAIAccountRuntimeBlocked(current), "late old invalidation must preserve a new health failure")
	gateway.ClearAccountSchedulingBlock(current.ID)
	require.False(review, gateway.isOpenAIAccountRuntimeBlocked(current))
	gateway.BlockAccountScheduling(current, time.Now().Add(time.Hour), "upstream_disable")
	require.NoError(review, invalidator.Invalidate(context.Background(), original, current))
	require.True(review, gateway.isOpenAIAccountRuntimeBlocked(current), "account-level operational blocks remain intact")
}

func TestReviewRuntimeClearObservationsRemovesGrokUsage(review *testing.T) {
	account := &Account{Platform: PlatformGrok, Extra: map[string]any{grokQuotaSnapshotExtraKey: "previous-owner-quota", "quota_used": 88}, Status: StatusDisabled}
	ClearAccountRuntimeObservations(account)
	require.NotContains(review, account.Extra, grokQuotaSnapshotExtraKey)
	require.Equal(review, 88, account.Extra["quota_used"])
	require.Equal(review, StatusDisabled, account.Status)
}

func TestRuntimeTokenProvidersRejectChangedRequestBinding(test *testing.T) {
	for _, platform := range []string{PlatformGrok, PlatformOpenAI, PlatformAnthropic, PlatformGemini} {
		for name, change := range map[string]func(*Account){
			"endpoint":      func(account *Account) { account.Credentials["base_url"] = "https://replacement.example/v1" },
			"same_proxy_id": func(account *Account) { account.Proxy.Host = "replacement.proxy" },
			"headers": func(account *Account) {
				account.Credentials["header_overrides"] = map[string]any{"X-Tenant": "replacement"}
			},
			"secret_revision": func(account *Account) { account.Extra["secret_revision"] = 2 },
			"owner":           func(account *Account) { account.Credentials["account_id"] = "replacement-owner" },
		} {
			test.Run(platform+"/"+name, func(test *testing.T) {
				proxyID := int64(5)
				selected := &Account{ID: 42, Platform: platform, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, ProxyID: &proxyID,
					Proxy: &Proxy{Protocol: "http", Host: "selected.proxy", Port: 8080}, Extra: map[string]any{},
					Credentials: map[string]any{"access_token": "selected-token", "refresh_token": "selected-refresh", "expires_at": time.Now().Add(4 * time.Hour).Format(time.RFC3339), "base_url": "https://selected.example/v1"}}
				latest := snapshotOAuthRefreshAccount(selected)
				latest.Credentials["access_token"] = "replacement-token"
				latest.Credentials["_token_version"] = int64(10)
				change(latest)
				repo := &runtimeSnapshotTestRepo{current: latest}
				cache := &runtimeRepairTokenCache{}
				var provider interface {
					GetAccessToken(context.Context, *Account) (string, error)
				}
				switch platform {
				case PlatformGrok:
					provider = NewGrokTokenProvider(repo, cache)
				case PlatformOpenAI:
					provider = NewOpenAITokenProvider(repo, cache, nil)
				case PlatformAnthropic:
					provider = NewClaudeTokenProvider(repo, cache, nil)
				case PlatformGemini:
					provider = NewGeminiTokenProvider(repo, cache, nil)
				}
				token, err := provider.GetAccessToken(context.Background(), selected)
				require.ErrorIs(test, err, ErrAccountRuntimeIdentityChanged)
				require.Empty(test, token)
				// A token-only renewal within the selected request configuration still works.
				repo.current = snapshotOAuthRefreshAccount(selected)
				repo.current.Credentials["access_token"] = "renewed-token"
				repo.current.Credentials["id_token"] = "renewed-id-token"
				repo.current.Credentials["_token_version"] = int64(11)
				token, err = provider.GetAccessToken(context.Background(), selected)
				require.NoError(test, err)
				require.Equal(test, "renewed-token", token)
			})
		}
	}
}

func TestRuntimeRegistryStaleCannotCrossDownloadIdentity(test *testing.T) {
	test.Setenv("MODEL_CAPABILITY_OFFLINE", "false")
	for _, changedDimension := range []string{"source", "proxy", "trust"} {
		test.Run(changedDimension, func(test *testing.T) {
			sourceURL, proxyURL, trustDomain := modelsDevRegistryURL, "", "verified-tls"
			switch changedDimension {
			case "source":
				sourceURL = "https://previous-source.example/registry.json"
			case "proxy":
				proxyURL = "http://previous-proxy.example:8080"
			case "trust":
				trustDomain = "previous-trust-policy"
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("unavailable"))}}
			service := &AccountTestService{httpUpstream: upstream, modelMetadataRegistry: map[string]modelsDevProvider{"previous": {}}, modelMetadataRegistryAt: time.Now().Add(-7 * time.Hour), modelMetadataRegistryKey: ModelRegistryDownloadKey(sourceURL, proxyURL, trustDomain)}
			registry, err := service.fetchModelsDevRegistry(context.Background(), &Account{})
			require.Error(test, err)
			require.Empty(test, registry, "a different download identity must not supply stale data")
			require.Len(test, upstream.requests, 1)
		})
	}
}

func TestRuntimeRegistryDraftEnrichmentRemainsRequestLocal(test *testing.T) {
	test.Setenv("MODEL_CAPABILITY_OFFLINE", "false")
	response := func(body string) *http.Response {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}
	}
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		response(`{"data":[{"id":"private-draft-model","context_window":4096}]}`),
		response(`{"openai":{"models":{"private-draft-model":{"limit":{"output":1024}}}}}`),
		response(`{"data":[{"id":"private-draft-model","context_window":8192}]}`),
	}}
	service := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
	first := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "first-draft", "base_url": "https://first.example/v1"}}
	second := snapshotOAuthRefreshAccount(first)
	second.Credentials["api_key"] = "second-draft"
	second.Credentials["base_url"] = "https://second.example/v1"
	firstCatalog, err := service.DiscoverModelCapabilities(context.Background(), first, nil)
	require.NoError(test, err)
	secondCatalog, err := service.DiscoverModelCapabilities(context.Background(), second, nil)
	require.NoError(test, err)
	require.Equal(test, int64(4096), firstCatalog.Metadata["private-draft-model"].ContextWindow)
	require.Equal(test, int64(8192), secondCatalog.Metadata["private-draft-model"].ContextWindow)
	require.Len(test, upstream.requests, 3, "public download is shared but native draft facts are independently fetched")
	require.Equal(test, "Bearer first-draft", upstream.requests[0].Header.Get("Authorization"))
	require.Equal(test, "Bearer second-draft", upstream.requests[2].Header.Get("Authorization"))
	require.Empty(test, upstream.requests[1].Header.Get("Authorization"))
}

func TestRuntimeHealthBlockRequestFastpathAndLateClear(test *testing.T) {
	until := time.Now().Add(time.Hour)
	selected := &Account{ID: 87, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "selected"}, TempUnschedulableUntil: &until}
	gateway := &OpenAIGatewayService{}
	gateway.BlockAccountScheduling(selected, until, openAIAPIKeyHealthBreakerReason)
	require.True(test, gateway.isOpenAIAccountRequestRuntimeBlocked(selected, "test-model"))
	snapshot := gateway.peekOpenAIAccountRuntimeBlock(selected)
	require.True(test, snapshot.blocked)
	gateway.BlockAccountScheduling(selected, until, openAIAPIKeyHealthBreakerReason)
	gateway.clearOpenAIAccountRuntimeBlockIfUnchanged(selected.ID, snapshot)
	require.True(test, gateway.isOpenAIAccountRequestRuntimeBlocked(selected, "test-model"), "late clear cannot erase a later health block at the same deadline")
	current := snapshotOAuthRefreshAccount(selected)
	current.Credentials["api_key"] = "replacement"
	current.TempUnschedulableUntil = nil
	require.False(test, gateway.isOpenAIAccountRequestRuntimeBlocked(current, "test-model"))
	gateway.BlockAccountScheduling(current, until, "upstream_disable")
	gateway.openaiAccountRuntimeBlockUntil.Store(AccountRuntimeIdentityKey(current), time.Now().Add(-time.Second))
	require.True(test, gateway.peekOpenAIAccountRuntimeBlock(current).blocked, "expired identity observation must not hide another account-level block")
}
