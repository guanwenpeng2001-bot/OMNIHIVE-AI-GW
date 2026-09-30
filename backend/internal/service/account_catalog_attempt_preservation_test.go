package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestScheduledCatalogVersionRejectionRecordsAttemptWithoutChangingSelection(t *testing.T) {
	now := time.Now().UTC()
	previousTime := now.Add(-24 * time.Hour).Format(time.RFC3339Nano)
	previous := &AccountModelCatalogSnapshot{
		SchemaVersion: AccountModelCatalogSchemaVersion, EnrichmentVersion: "capability-v2",
		SourceGeneration: 1 << 60, SourceVersions: map[string]string{"local_catalog": "newer-release"},
		Revision: 7, Complete: true, ObservedAt: previousTime,
		DiscoveredModels: []string{}, EligibleModels: []AccountCatalogModel{},
	}
	account := &Account{ID: 47, Status: StatusActive, Schedulable: false,
		Platform: PlatformCommandCode, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "synthetic-key", "model_mapping": map[string]string{}},
		Extra: map[string]any{
			UpstreamModelCatalogExtraKey:         previous,
			UpstreamModelCatalogStateExtraKey:    AccountModelCatalogState{AttemptedAt: previousTime, LastSuccessAt: previousTime, Result: "succeeded"},
			UpstreamModelAllowlistExtraKey:       AccountModelAllowlist{Enabled: true, Models: []string{}},
			ModelSelectionBaselineFrozenExtraKey: true,
			"commandcode_monthlyCredits":         0.1,
		},
	}
	repository := &refreshCatalogRepository{catalogFixtureRepository: &catalogFixtureRepository{accounts: map[int64]*Account{account.ID: account}}, active: []Account{*account}}
	upstream := &httpUpstreamRecorder{responses: []*http.Response{catalogFixtureResponse(`{"data":[]}`), catalogFixtureResponse(`{"data":[]}`)}}
	accountTests := &AccountTestService{accountRepo: repository, httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
	runner := &ScheduledTestRunnerService{accountTestSvc: accountTests}
	require.True(t, accountCatalogRefreshDue(account, now))
	runner.refreshDueCatalogs(context.Background(), now)
	require.Equal(t, 1, repository.commits, "a completed discovery rejected by release validation must record its attempt")
	require.Equal(t, "failed", account.ModelCatalogState().Result)
	require.Equal(t, "upstream_model_catalog_version_rejected", account.ModelCatalogState().Error)
	require.NotEqual(t, previousTime, account.ModelCatalogState().AttemptedAt)
	require.Equal(t, previousTime, account.ModelCatalogState().LastSuccessAt)
	require.Equal(t, previous, account.GetUpstreamModelCatalog())
	require.False(t, account.Schedulable)
	require.Equal(t, map[string]string{}, account.Credentials["model_mapping"])
	require.Equal(t, 0.1, account.Extra["commandcode_monthlyCredits"])
	require.False(t, accountCatalogRefreshDue(account, now.Add(time.Minute)))
	runner.refreshDueCatalogs(context.Background(), now.Add(time.Minute))
	require.Equal(t, 1, repository.commits)
	require.True(t, accountCatalogRefreshDue(account, now.Add(7*time.Hour)))
}

func TestScheduledCatalogRefreshPublishesCandidatesAndPreservesPausedSelection(t *testing.T) {
	now := time.Now().UTC()
	previousTime := now.Add(-24 * time.Hour).Format(time.RFC3339Nano)
	account := &Account{ID: 48, Status: StatusActive, Schedulable: false,
		Platform: PlatformCommandCode, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "synthetic-key", "model_mapping": map[string]string{"public-alias": "chosen-model"}},
		Extra: map[string]any{
			UpstreamModelCatalogExtraKey:      &AccountModelCatalogSnapshot{Revision: 7, Complete: true, ObservedAt: previousTime},
			UpstreamModelCatalogStateExtraKey: AccountModelCatalogState{AttemptedAt: previousTime, LastSuccessAt: previousTime, Result: "succeeded"},
			"commandcode_monthlyCredits":      0.1,
		},
	}
	repository := &refreshCatalogRepository{catalogFixtureRepository: &catalogFixtureRepository{accounts: map[int64]*Account{account.ID: account}}, active: []Account{*account}}
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		catalogFixtureResponse(`{"data":[{"id":"chosen-model"},{"id":"new-candidate"}]}`),
		catalogFixtureResponse(`{}`), // Optional metadata lookup stays offline.
	}}
	runner := &ScheduledTestRunnerService{accountTestSvc: &AccountTestService{accountRepo: repository, httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}}
	runner.refreshDueCatalogs(context.Background(), now)
	require.Equal(t, 1, repository.commits)
	require.Equal(t, "succeeded", account.ModelCatalogState().Result)
	require.NotEqual(t, previousTime, account.ModelCatalogState().AttemptedAt)
	require.NotEqual(t, previousTime, account.ModelCatalogState().LastSuccessAt)
	require.EqualValues(t, 8, account.GetUpstreamModelCatalog().Revision)
	require.ElementsMatch(t, []string{"chosen-model", "new-candidate"}, account.GetUpstreamModelCatalog().DiscoveredModels)
	require.Equal(t, map[string]string{"public-alias": "chosen-model"}, account.Credentials["model_mapping"])
	require.False(t, account.Schedulable)
	require.Equal(t, 0.1, account.Extra["commandcode_monthlyCredits"])
	runner.refreshDueCatalogs(context.Background(), now.Add(time.Minute))
	require.Equal(t, 1, repository.commits)
}

type catalogAttemptConflictRepository struct {
	*catalogFixtureRepository
	newerState AccountModelCatalogState
}

func (repository *catalogAttemptConflictRepository) CommitAccountModelCatalog(ctx context.Context, id int64, baseline AccountModelCatalogBaseline, snapshot *AccountModelCatalogSnapshot, state AccountModelCatalogState, metadata *UpstreamModelMetadataSnapshot, freeze bool) error {
	// Simulate an independent rules/identity writer committing after discovery
	// started but before the scheduled failure can commit its attempt.
	repository.accounts[id].Extra[UpstreamModelCatalogStateExtraKey] = repository.newerState
	return repository.catalogFixtureRepository.CommitAccountModelCatalog(ctx, id, baseline, snapshot, state, metadata, freeze)
}

func TestScheduledCatalogRejectedAttemptCannotOverwriteNewerRulesOrIdentity(t *testing.T) {
	for _, newerState := range []AccountModelCatalogState{
		{RulesRevision: 1, AttemptedAt: "2030-01-01T00:00:00Z", Result: "succeeded"},
		{IdentityRevision: 1, AttemptedAt: "2030-01-01T00:00:00Z", Result: "succeeded"},
	} {
		account := &Account{ID: 49, Status: StatusActive, Platform: PlatformCommandCode, Type: AccountTypeAPIKey,
			Credentials: map[string]any{"api_key": "synthetic-key"}, Extra: map[string]any{
				UpstreamModelCatalogExtraKey: &AccountModelCatalogSnapshot{SchemaVersion: AccountModelCatalogSchemaVersion, EnrichmentVersion: "capability-v2", SourceGeneration: 1 << 60, Complete: true},
			}}
		repository := &catalogAttemptConflictRepository{catalogFixtureRepository: &catalogFixtureRepository{accounts: map[int64]*Account{account.ID: account}}, newerState: newerState}
		accountTests := &AccountTestService{accountRepo: repository, httpUpstream: &httpUpstreamRecorder{resp: catalogFixtureResponse(`{"data":[]}`)}, cfg: upstreamModelSyncTestConfig()}
		ctx := context.WithValue(context.Background(), catalogScheduledRefreshKey{}, true)
		_, err := accountTests.ApplyAccountModelCatalog(ctx, account, nil)
		require.ErrorIs(t, err, ErrAccountModelCatalogConflict)
		require.Zero(t, repository.commits)
		require.Equal(t, newerState, account.ModelCatalogState())
	}
}

func TestScheduledCatalogRefreshRequiresGenerationAdvanceForChangedRelease(t *testing.T) {
	// Sanitized production envelope: the discovery parser release changed the
	// local catalog hash without advancing this generation. Reproduce that
	// mismatch, then prove a normally advanced release can publish candidates.
	const previousGeneration int64 = 1790554945
	const previousVersion = "53ef68d8c76983b141a8c22560a3d4f755bb60029051d94dacdabca07b0132c5"
	const changedVersion = "65543b9b6ed9960ef48f12f724777e17ec3bb0183823f0c3cb41cdd4b1951c49"
	for _, scenario := range []struct {
		name       string
		generation int64
		result     string
	}{
		{"same_generation_changed_hash", previousGeneration, "failed"},
		{"advanced_generation", previousGeneration + 1, "succeeded"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			previous := &AccountModelCatalogSnapshot{SchemaVersion: 2, EnrichmentVersion: "capability-v2", SourceGeneration: previousGeneration,
				SourceVersions: map[string]string{"endpoint_rules": "endpoint-contract-v1", "heuristics": "name-rules-v1", "local_catalog": previousVersion},
				Revision:       1, RulesRevision: 1, Complete: true, ObservedAt: "2026-09-30T12:26:01.01061Z"}
			state := AccountModelCatalogState{RulesRevision: 1, LegacyPolicyRevision: 1, Result: "succeeded",
				AttemptedAt: "2026-09-30T12:26:01.0880642Z", LastSuccessAt: previous.ObservedAt}
			account := &Account{ID: 47, Status: StatusActive, Schedulable: true, Platform: PlatformCommandCode, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "synthetic-key", "model_mapping": map[string]string{"alias": "chosen-model"}},
				Extra:       map[string]any{UpstreamModelCatalogExtraKey: previous, UpstreamModelCatalogStateExtraKey: state}}
			repository := &catalogFixtureRepository{accounts: map[int64]*Account{account.ID: account}}
			provider := &countedCapabilityProvider{snapshot: &ModelCapabilitySnapshot{generation: scenario.generation, version: changedVersion}}
			accountTests := &AccountTestService{accountRepo: repository, modelCapabilityProvider: provider, cfg: upstreamModelSyncTestConfig(),
				httpUpstream: &httpUpstreamRecorder{responses: []*http.Response{catalogFixtureResponse(`{"data":[{"id":"chosen-model"},{"id":"new-candidate"}]}`), catalogFixtureResponse(`{}`)}}}
			ctx := context.WithValue(context.Background(), catalogScheduledRefreshKey{}, true)
			_, err := accountTests.ApplyAccountModelCatalog(ctx, account, nil)
			if scenario.result == "failed" {
				require.ErrorIs(t, err, ErrAccountModelCatalogConflict)
				require.Equal(t, previous, account.GetUpstreamModelCatalog())
				require.Equal(t, state.LastSuccessAt, account.ModelCatalogState().LastSuccessAt)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, 2, account.GetUpstreamModelCatalog().Revision)
				require.ElementsMatch(t, []string{"chosen-model", "new-candidate"}, account.GetUpstreamModelCatalog().DiscoveredModels)
			}
			require.Equal(t, 1, repository.commits)
			require.Equal(t, scenario.result, account.ModelCatalogState().Result)
			require.NotEqual(t, state.AttemptedAt, account.ModelCatalogState().AttemptedAt)
			require.False(t, accountCatalogRefreshDue(account, time.Now().Add(time.Minute)))
			require.Equal(t, map[string]string{"alias": "chosen-model"}, account.Credentials["model_mapping"])
			require.True(t, account.Schedulable, "retain the latest explicit management choice")
		})
	}
}
