//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCapabilityDisplayWiringPreservesCatalog(test *testing.T) {
	test.Setenv("MODEL_CAPABILITY_OFFLINE", "1")
	enricher, err := NewCapabilityReadOnlyEnricher(nil, &http.Client{Transport: capabilityDisplayTransport(func(*http.Request) (*http.Response, error) {
		test.Fatal("offline read must not fetch")
		return nil, nil
	})})
	require.NoError(test, err)
	service := &AccountTestService{capabilityReadOnlyEnricher: enricher}
	account := capabilityDisplayAccount()
	before, err := json.Marshal(account)
	require.NoError(test, err)
	projected := service.ReadOnlyModelCapabilities(account)
	require.Contains(test, projected, "native-display")
	require.Contains(test, projected, "private-display")
	require.NotContains(test, projected, "wild-*")
	after, err := json.Marshal(account)
	require.NoError(test, err)
	require.JSONEq(test, string(before), string(after))
	require.False(test, AccountSupportsResolvedModel(context.Background(), account, "alias", AccountModelBindingOptions{Endpoint: "chat_completions"}))
}

func TestMappedOnlyCapabilityDiscovery(test *testing.T) {
	test.Setenv("MODEL_CAPABILITY_OFFLINE", "1")
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://api.openai.com", "model_mapping": map[string]any{"alias": "private-target"}}}
	service := &AccountTestService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &httpUpstreamRecorder{resp: catalogFixtureResponse(`{"data":[]}`)}}
	catalog, err := service.DiscoverModelCapabilities(context.WithValue(context.Background(), catalogDiscoveryKey{}, true), account, nil)
	require.NoError(test, err)
	require.Empty(test, catalog.Models)
	require.Contains(test, catalog.Metadata, "private-target")
	require.Empty(test, account.GetUpstreamModelCatalog())
}

func TestExpandedPlatformAccountShape(test *testing.T) {
	for _, platform := range []string{PlatformVolcengine, PlatformQianfan, PlatformHunyuan, PlatformMistral, PlatformBedrock} {
		for _, kind := range []string{AccountTypeAPIKey, AccountTypeOAuth, AccountTypeBedrock} {
			err := validateExpandedPlatformAccount(&Account{Platform: platform, Type: kind})
			valid := kind == AccountTypeAPIKey
			if platform == PlatformBedrock {
				valid = kind == AccountTypeBedrock
			}
			require.Equal(test, valid, err == nil, platform+"/"+kind)
		}
	}
	require.Error(test, validateExpandedPlatformAccount(&Account{Platform: PlatformComposite, Type: AccountTypeAPIKey}))
	require.NoError(test, validateExpandedPlatformAccount(&Account{Platform: PlatformAnthropic, Type: AccountTypeBedrock}))
}
