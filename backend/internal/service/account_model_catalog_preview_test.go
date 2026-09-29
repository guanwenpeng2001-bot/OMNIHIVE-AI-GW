package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccountModelCatalogSparseDiscoveryContract(test *testing.T) {
	test.Setenv("MODEL_CAPABILITY_OFFLINE", "1")
	account := &Account{ID: 81, Status: StatusActive, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}, Extra: map[string]any{}}
	repository := &catalogFixtureRepository{accounts: map[int64]*Account{account.ID: account}}
	service := &AccountTestService{accountRepo: repository, cfg: upstreamModelSyncTestConfig(), modelMetadataRegistry: map[string]modelsDevProvider{"openai": {}}, modelMetadataRegistryAt: time.Now(), httpUpstream: &httpUpstreamRecorder{responses: []*http.Response{catalogFixtureResponse(`{"data":[{"id":"private-unknown"}]}`), catalogFixtureResponse(`{"data":[{"id":"private-unknown"}]}`)}}}
	preview, err := service.PreviewAccountModelCatalog(context.Background(), account)
	require.NoError(test, err)
	require.True(test, preview.DiscoveryComplete)
	require.Equal(test, "incomplete", preview.CapabilityStatus)
	require.Equal(test, []string{"private-unknown"}, preview.CandidateModels)
	encoded, err := json.Marshal(preview)
	require.NoError(test, err)
	var response map[string]any
	require.NoError(test, json.Unmarshal(encoded, &response))
	for _, field := range []string{"discovered_models", "candidate_models", "eligible_models", "discovery_complete", "capability_status"} {
		require.Contains(test, response, field)
	}
	require.NotNil(test, response["eligible_models"])
	applied, err := service.ApplyReviewedAccountModelCatalog(context.Background(), account, preview.Review)
	require.NoError(test, err)
	require.True(test, applied.Applied)
	require.Equal(test, 1, repository.commits)
}

func TestAccountModelCatalogPartialDiscoveryPreservesCompleteSnapshot(test *testing.T) {
	test.Setenv("MODEL_CAPABILITY_OFFLINE", "1")
	account := &Account{ID: 82, Status: StatusActive, Platform: PlatformAntigravity, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://gateway.example/antigravity"}, Extra: map[string]any{UpstreamModelCatalogExtraKey: AccountModelCatalogSnapshot{Complete: true, Revision: 7, DiscoveredModels: []string{"old"}, EligibleModels: []AccountCatalogModel{{ID: "old"}}}}}
	repository := &catalogFixtureRepository{accounts: map[int64]*Account{account.ID: account}}
	response := func() []*http.Response {
		failed := catalogFixtureResponse(`{"error":"unavailable"}`)
		failed.StatusCode = http.StatusServiceUnavailable
		return []*http.Response{catalogFixtureResponse(`{"data":[{"id":"claude-fixture","supported_methods":["messages"]}]}`), failed}
	}
	transport := &httpUpstreamRecorder{responses: response()}
	service := &AccountTestService{accountRepo: repository, cfg: upstreamModelSyncTestConfig(), httpUpstream: transport}
	before, err := json.Marshal(account)
	require.NoError(test, err)
	preview, err := service.PreviewAccountModelCatalog(context.Background(), account)
	require.NoError(test, err)
	require.False(test, preview.DiscoveryComplete)
	require.Equal(test, "incomplete", preview.CapabilityStatus)
	require.Equal(test, []string{"claude-fixture"}, preview.DiscoveredModels)
	require.Equal(test, preview.DiscoveredModels, preview.CandidateModels)
	require.Empty(test, preview.EligibleModels)
	require.Nil(test, preview.Review)
	after, err := json.Marshal(account)
	require.NoError(test, err)
	require.JSONEq(test, string(before), string(after))
	require.Zero(test, repository.commits)
	_, err = service.ApplyReviewedAccountModelCatalog(context.Background(), account, preview.Review)
	require.ErrorIs(test, err, ErrAccountModelCatalogConflict)
	transport.responses = response()
	_, err = service.ApplyAccountModelCatalog(context.Background(), account, nil)
	require.Error(test, err)
	require.EqualValues(test, 7, account.GetUpstreamModelCatalog().Revision)
	require.Equal(test, []string{"old"}, account.GetUpstreamModelCatalog().DiscoveredModels)
}

func TestAccountModelCatalogPreviewHeaderValidation(test *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "header_override_enabled": true, "header_overrides": map[string]any{"Authorization": "forbidden"}}}
	transport := &httpUpstreamRecorder{}
	service := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: transport}
	_, err := service.PreviewAccountModelCatalog(context.Background(), account)
	require.Error(test, err)
	require.Nil(test, transport.lastReq)
	account.Credentials["header_overrides"] = map[string]any{"X-Route": " route "}
	before, err := json.Marshal(account.Credentials)
	require.NoError(test, err)
	transport.resp = catalogFixtureResponse(`{"data":[]}`)
	_, err = service.PreviewAccountModelCatalog(context.Background(), account)
	require.NoError(test, err)
	// The upstream wire casing is deliberately non-canonical here: ApplyHeaderOverrides
	// writes unknown header names lower-cased (resolveWireCasing) to bypass Go's
	// canonicalisation, so indexing the raw map is the only way to assert the casing.
	//nolint:staticcheck // SA1008: intentional non-canonical key, see comment above.
	require.Equal(test, []string{"route"}, transport.lastReq.Header["x-route"])
	after, err := json.Marshal(account.Credentials)
	require.NoError(test, err)
	require.JSONEq(test, string(before), string(after))
}

func TestAccountModelCatalogDuplicateDropsDiscoveryEvidence(test *testing.T) {
	fields := []string{UpstreamModelCatalogExtraKey, UpstreamModelCatalogStateExtraKey, UpstreamModelMetadataExtraKey, UpstreamModelAllowlistExtraKey, ModelAvailabilityExtraKey, ModelProbeBudgetExtraKey}
	source := map[string]any{"retained_configuration": "value"}
	for _, field := range fields {
		source[field] = map[string]any{"source_account_evidence": true}
	}
	copied, err := duplicateAccountExtra(source)
	require.NoError(test, err)
	require.Equal(test, map[string]any{"retained_configuration": "value"}, copied)
	for _, field := range fields {
		require.Contains(test, source, field)
	}
	require.False(test, canDuplicateAccountType(AccountTypeOAuth))
	require.False(test, canDuplicateAccountType(AccountTypeSetupToken))
}
