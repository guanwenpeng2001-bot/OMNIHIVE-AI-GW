package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelCatalogAliasDiagnosticsReuseNativeBinding(t *testing.T) {
	for _, scenario := range []struct {
		name, platform, accountType, target, native string
	}{
		{"anthropic short name", PlatformAnthropic, AccountTypeOAuth, "claude-sonnet-4-5", "claude-sonnet-4-5-20250929"},
		{"gemini resource name", PlatformGemini, AccountTypeAPIKey, "models/gemini-private", "gemini-private"},
		{"antigravity native mapping", PlatformAntigravity, AccountTypeOAuth, "claude-sonnet-4-5", "claude-sonnet-4-5"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			account := &Account{Platform: scenario.platform, Type: scenario.accountType, Credentials: map[string]any{"model_mapping": map[string]any{"valid": scenario.target, "invalid": "missing"}}}
			catalog := &UpstreamModelCatalog{Models: []string{scenario.native}, Metadata: map[string]UpstreamModelMetadata{scenario.native: {SupportedMethods: []string{"generateContent", "messages"}}}}
			before, err := json.Marshal(account)
			require.NoError(t, err)
			snapshot := projectAccountModelCatalog(account, catalog)
			require.Empty(t, catalog.InvalidAliases)
			require.Equal(t, snapshot.EligibleModels, catalog.EligibleModels)
			require.Equal(t, catalog.ObservedAt, catalog.EligibleModels[0].EvidenceObservedAt)
			after, err := json.Marshal(account)
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(after), "preview diagnostics must not mutate saved evidence")
			require.Empty(t, InvalidAccountCatalogAliases(account, nil))
		})
	}
}

func TestModelCatalogFailureDiagnosticsPreserveEvidence(t *testing.T) {
	for _, scenario := range []struct {
		name              string
		status            int
		result, errorCode string
	}{
		{"not found", http.StatusNotFound, "unsupported", "upstream_model_discovery_unsupported"},
		{"method unsupported", http.StatusMethodNotAllowed, "unsupported", "upstream_model_discovery_unsupported"},
		{"upstream failure", http.StatusBadGateway, "failed", "upstream_model_discovery_failed"},
		{"access denied", http.StatusForbidden, "failed", "upstream_model_discovery_failed"},
		{"invalid credentials", http.StatusUnauthorized, "credentials_invalid", "upstream_model_credentials_invalid"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			account := catalogEvidenceAccount(11, PlatformDeepseek, "private-model")
			account.Credentials["api_key"] = "test"
			account.Credentials["base_url"] = "https://api.deepseek.com"
			snapshot := account.GetUpstreamModelCatalog()
			account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{LastSuccessAt: snapshot.ObservedAt}
			repository := &catalogFixtureRepository{accounts: map[int64]*Account{account.ID: &account}}
			upstream := &httpUpstreamRecorder{responses: []*http.Response{{StatusCode: scenario.status, Body: io.NopCloser(strings.NewReader(`{"error":"sensitive upstream detail"}`))}}}
			syncService := &AccountTestService{accountRepo: repository, httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
			_, err := syncService.ApplyAccountModelCatalog(context.Background(), &account, nil)
			require.Error(t, err)
			require.Equal(t, snapshot, account.GetUpstreamModelCatalog())
			state := account.ModelCatalogState()
			require.Equal(t, scenario.result, state.Result)
			require.Equal(t, scenario.errorCode, state.Error)
			require.Equal(t, snapshot.ObservedAt, state.LastSuccessAt)
			require.NotEmpty(t, state.AttemptedAt)
			if scenario.status != http.StatusUnauthorized {
				require.NotNil(t, account.EffectiveUpstreamModelCatalog())
			}
		})
	}
}
