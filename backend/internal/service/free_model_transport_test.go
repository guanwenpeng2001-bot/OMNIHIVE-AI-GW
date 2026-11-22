//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestZenFreeSendBoundaryAndConfiguredAliases(t *testing.T) {
	for _, path := range []string{"/chat/completions", "/responses", "/messages"} {
		for _, target := range []string{"gpt-5.4", "jev-free", "clef-free", "retired-free", "invented-free", "big-pickle"} {
			t.Run(path+target, func(t *testing.T) {
				account := &Account{ID: 4, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Credentials: map[string]any{"account_mode": AccountModeZenFree, "model_mapping": map[string]string{"alias": target}}}
				sent := false
				req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://opencode.ai/zen/v1"+path, strings.NewReader(`{"model":"`+target+`"}`))
				require.NoError(t, err)
				response, err := (&freeModelUpstream{account: account}).do(req, func(r *http.Request) (*http.Response, error) {
					sent = true
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
				})
				catalog := account.EffectiveUpstreamModelCatalog()
				if target == "big-pickle" {
					require.NoError(t, err)
					require.True(t, sent)
					require.NoError(t, response.Body.Close())
					require.NotNil(t, catalog)
					require.Equal(t, target, catalog.EligibleModels[0].ID)
				} else {
					require.ErrorIs(t, err, ErrAccountModelUnsupported)
					require.False(t, sent)
					require.Nil(t, catalog)
				}
			})
		}
	}
	for _, mode := range []string{AccountModeGo, AccountModeZen} {
		a := &Account{Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Credentials: map[string]any{"account_mode": mode, "model_mapping": map[string]string{"alias": "gpt-5.4"}}}
		require.True(t, zenFreeModelAllowed(a, "gpt-5.4"))
		require.NotEmpty(t, a.EffectiveUpstreamModelCatalog().EligibleModels)
	}
}

func TestZenFreeIdentityDiscoveryOverridesBootstrap(t *testing.T) {
	a := &Account{Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Credentials: map[string]any{"account_mode": AccountModeZenFree, "model_mapping": map[string]string{"alias": "big-pickle"}}, Extra: map[string]any{}}
	raw := []byte(`{"data":[{"id":"new-model","cost":{"input":0,"output":0}},{"id":"big-pickle","cost":{"input":1}},{"id":"retired-free","status":"retired"},{"id":"jev-free","cost":{"input":0}}]}`)
	ids, metadata, err := extractAccountModelCatalog(raw, a)
	require.NoError(t, err)
	require.Equal(t, []string{"new-model"}, ids)
	a.Extra[UpstreamModelCatalogExtraKey] = AccountModelCatalogSnapshot{Complete: true, SourceKind: "discovery", DiscoveredModels: ids, EligibleModels: []AccountCatalogModel{{ID: "new-model"}}}
	a.Extra[UpstreamModelMetadataExtraKey] = UpstreamModelMetadataSnapshot{Models: metadata}
	require.True(t, zenFreeModelAllowed(a, "new-model"))
	for _, target := range []string{"big-pickle", "retired-free", "jev-free", "clef-free", "gpt-5.4"} {
		require.False(t, zenFreeModelAllowed(a, target), target)
	}
	require.Len(t, a.EffectiveUpstreamModelCatalog().EligibleModels, 1)
	a.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 1}
	require.False(t, zenFreeModelAllowed(a, "new-model"))
	require.False(t, zenFreeModelAllowed(a, "big-pickle"))
}
