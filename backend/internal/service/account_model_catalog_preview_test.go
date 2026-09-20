package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccountModelCatalogSparseDiscoveryContract(t *testing.T) {
	t.Setenv("MODEL_CAPABILITY_OFFLINE", "1")
	account := &Account{ID: 81, Status: StatusActive, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}, Extra: map[string]any{}}
	repository := &catalogFixtureRepository{accounts: map[int64]*Account{account.ID: account}}
	service := &AccountTestService{accountRepo: repository, cfg: upstreamModelSyncTestConfig(), modelMetadataRegistry: map[string]modelsDevProvider{"openai": {}}, modelMetadataRegistryAt: time.Now(), httpUpstream: &httpUpstreamRecorder{responses: []*http.Response{catalogFixtureResponse(`{"data":[{"id":"private-unknown"}]}`), catalogFixtureResponse(`{"data":[{"id":"private-unknown"}]}`)}}}
	preview, err := service.PreviewAccountModelCatalog(context.Background(), account)
	require.NoError(t, err)
	require.True(t, preview.DiscoveryComplete)
	require.Equal(t, "incomplete", preview.CapabilityStatus)
	require.Equal(t, []string{"private-unknown"}, preview.CandidateModels)
	encoded, err := json.Marshal(preview)
	require.NoError(t, err)
	var response map[string]any
	require.NoError(t, json.Unmarshal(encoded, &response))
	for _, field := range []string{"discovered_models", "candidate_models", "eligible_models", "discovery_complete", "capability_status"} {
		require.Contains(t, response, field)
	}
	require.NotNil(t, response["eligible_models"])
	applied, err := service.ApplyReviewedAccountModelCatalog(context.Background(), account, preview.Review)
	require.NoError(t, err)
	require.True(t, applied.Applied)
	require.Equal(t, 1, repository.commits)
}

func TestAccountModelCatalogPartialDiscoveryPreservesCompleteSnapshot(t *testing.T) {
	t.Setenv("MODEL_CAPABILITY_OFFLINE", "1")
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
	require.NoError(t, err)
	preview, err := service.PreviewAccountModelCatalog(context.Background(), account)
	require.NoError(t, err)
	require.False(t, preview.DiscoveryComplete)
	require.Equal(t, "incomplete", preview.CapabilityStatus)
	require.Equal(t, []string{"claude-fixture"}, preview.DiscoveredModels)
	require.Equal(t, preview.DiscoveredModels, preview.CandidateModels)
	require.Empty(t, preview.EligibleModels)
	require.Nil(t, preview.Review)
	after, err := json.Marshal(account)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after))
	require.Zero(t, repository.commits)
	_, err = service.ApplyReviewedAccountModelCatalog(context.Background(), account, preview.Review)
	require.ErrorIs(t, err, ErrAccountModelCatalogConflict)
	transport.responses = response()
	_, err = service.ApplyAccountModelCatalog(context.Background(), account, nil)
	require.Error(t, err)
	require.EqualValues(t, 7, account.GetUpstreamModelCatalog().Revision)
	require.Equal(t, []string{"old"}, account.GetUpstreamModelCatalog().DiscoveredModels)
}

func TestAccountModelCatalogPreviewHeaderValidation(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "header_override_enabled": true, "header_overrides": map[string]any{"Authorization": "forbidden"}}}
	transport := &httpUpstreamRecorder{}
	service := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: transport}
	_, err := service.PreviewAccountModelCatalog(context.Background(), account)
	require.Error(t, err)
	require.Nil(t, transport.lastReq)
	account.Credentials["header_overrides"] = map[string]any{"X-Route": " route "}
	before, err := json.Marshal(account.Credentials)
	require.NoError(t, err)
	transport.resp = catalogFixtureResponse(`{"data":[]}`)
	_, err = service.PreviewAccountModelCatalog(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, []string{"route"}, transport.lastReq.Header["x-route"])
	after, err := json.Marshal(account.Credentials)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after))
}
