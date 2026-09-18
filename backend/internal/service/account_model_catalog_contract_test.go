//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/stretchr/testify/require"
)

func TestAccountModelCatalogQianwenDiscoveryPreservesLegacy(t *testing.T) {
	t.Setenv("MODEL_CAPABILITY_OFFLINE", "true")
	for _, fixture := range []struct {
		name, body                   string
		legacyError, catalogError    bool
		legacyCount, discoveredCount int
	}{
		{"empty compatible", `{"data":[]}`, true, false, 0, 0},
		{"empty native", `{"success":true,"output":{"total":0,"page_no":1,"models":[]}}`, true, false, 0, 0},
		{"missing list", `{}`, true, true, 0, 0},
		{"null native list", `{"success":true,"output":{"total":0,"page_no":1,"models":null}}`, true, true, 0, 0},
		{"retired diagnostics", `{"data":[{"id":"qwen1.5-72b-chat"},{"id":"qwen-plus"}]}`, false, false, 1, 2},
	} {
		for _, catalogPath := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/catalog=%t", fixture.name, catalogPath), func(t *testing.T) {
				upstream := &httpUpstreamRecorder{resp: catalogFixtureResponse(fixture.body)}
				svc := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
				account := &Account{Platform: PlatformQianwen, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://dashscope.aliyuncs.com/compatible-mode/v1"}}
				if catalogPath {
					catalog, err := svc.SyncUpstreamModelCatalog(context.Background(), account)
					if fixture.catalogError {
						require.Error(t, err)
						return
					}
					require.NoError(t, err)
					require.Len(t, catalog.DiscoveredModels, fixture.discoveredCount)
					if fixture.name == "retired diagnostics" {
						require.Contains(t, catalog.ExcludedModels, ExcludedCatalogModel{ID: "qwen1.5-72b-chat", Reason: CatalogReasonRetired})
					}
				} else {
					ids, err := svc.FetchUpstreamSupportedModels(context.Background(), account)
					if fixture.legacyError {
						require.Error(t, err)
						return
					}
					require.NoError(t, err)
					require.Len(t, ids, fixture.legacyCount)
				}
				require.Len(t, upstream.requests, 1)
			})
		}
	}
}

func TestAccountModelCatalogOAuthPreviewUsesSavedTokens(t *testing.T) {
	t.Setenv("MODEL_CAPABILITY_OFFLINE", "true")
	for _, platform := range []string{PlatformGemini, PlatformAnthropic, PlatformGrok} {
		t.Run(platform, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: catalogFixtureResponse(`{"data":[]}`)}
			// Deliberately omit refresh providers: previews must use the saved token.
			svc := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
			account := &Account{ID: 42, Platform: platform, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "saved-token"}}
			before, err := json.Marshal(account)
			require.NoError(t, err)
			catalog, err := svc.PreviewAccountModelCatalog(context.Background(), account)
			require.NoError(t, err)
			require.NotNil(t, catalog.Review)
			require.Equal(t, "Bearer saved-token", upstream.requests[0].Header.Get("Authorization"))
			after, err := json.Marshal(account)
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(after))
		})
	}
}

func TestAccountModelCatalogAntigravityPreviewPreservesNativeFacts(t *testing.T) {
	t.Setenv("MODEL_CAPABILITY_OFFLINE", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer saved-token", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, `{"models":{"native":{"displayName":"Native","supportsThinking":false,"supportsImages":false,"inputTokenLimit":128000,"maxOutputTokens":4096}}}`)
	}))
	defer server.Close()
	previous := antigravity.BaseURLs
	antigravity.BaseURLs = []string{server.URL}
	t.Cleanup(func() { antigravity.BaseURLs = previous })
	svc := &AccountTestService{cfg: upstreamModelSyncTestConfig()}
	account := &Account{ID: 42, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "saved-token", "project_id": "fixture"}}
	catalog, err := svc.PreviewAccountModelCatalog(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, []string{"native"}, catalog.DiscoveredModels)
	require.EqualValues(t, 128000, catalog.Metadata["native"].ContextWindow)
	require.NotNil(t, catalog.Metadata["native"].Reasoning)
	require.False(t, *catalog.Metadata["native"].Reasoning)
	require.Nil(t, account.Extra)
}

type catalogAllowlistRecorder struct {
	AccountRepository
	calls int
}

func (r *catalogAllowlistRecorder) UpdateAccountModelAllowlist(context.Context, int64, AccountModelCatalogBaseline, AccountModelAllowlist) error {
	r.calls++
	return nil
}

func TestAccountModelCatalogAllowlistRejectsInvalidConfirmations(t *testing.T) {
	for _, fixture := range []struct {
		name            string
		enabled         bool
		models          []string
		reason          string
		actor, revision int64
		valid           bool
	}{
		{"valid", true, []string{"native"}, "confirmed", 7, 3, true},
		{"disabled", false, nil, "confirmed", 7, 3, true},
		{"empty", true, nil, "confirmed", 7, 3, false},
		{"unknown", true, []string{"guessed"}, "confirmed", 7, 3, false},
		{"wildcard", true, []string{"*"}, "confirmed", 7, 3, false},
		{"missing actor", true, []string{"native"}, "confirmed", 0, 3, false},
		{"missing reason", true, []string{"native"}, "", 7, 3, false},
		{"stale", true, []string{"native"}, "confirmed", 7, 2, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			repo := &catalogAllowlistRecorder{}
			svc := &AccountTestService{accountRepo: repo}
			account := &Account{ID: 42, Extra: map[string]any{UpstreamModelCatalogStateExtraKey: AccountModelCatalogState{RulesRevision: 3}, UpstreamModelCatalogExtraKey: AccountModelCatalogSnapshot{Complete: true, DiscoveredModels: []string{"native"}}}}
			err := svc.ConfigureAccountModelAllowlist(context.Background(), account, fixture.enabled, fixture.models, fixture.reason, fixture.actor, fixture.revision)
			if fixture.valid {
				require.NoError(t, err)
				require.Equal(t, 1, repo.calls)
			} else {
				require.Error(t, err)
				require.Zero(t, repo.calls)
			}
		})
	}
}
