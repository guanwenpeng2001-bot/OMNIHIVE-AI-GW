package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

type reviewedCatalogRepository struct{ *catalogFixtureRepository }

func (r *reviewedCatalogRepository) CommitAccountModelCatalog(ctx context.Context, id int64, baseline AccountModelCatalogBaseline, snapshot *AccountModelCatalogSnapshot, state AccountModelCatalogState, metadata *UpstreamModelMetadataSnapshot) error {
	if baseline.RequireActive && r.accounts[id].Status != StatusActive {
		return ErrAccountModelCatalogConflict
	}
	baseline.RequireActive = false
	return r.catalogFixtureRepository.CommitAccountModelCatalog(ctx, id, baseline, snapshot, state, metadata)
}

func TestAccountModelCatalogReviewedRejectsStaleApproval(t *testing.T) {
	for _, change := range []string{"none", "identity", "rules", "snapshot", "disabled", "account", "release", "membership", "capability"} {
		t.Run(change, func(t *testing.T) {
			a := &Account{ID: 1, Status: StatusActive, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test"}, Extra: map[string]any{}}
			r := &reviewedCatalogRepository{&catalogFixtureRepository{accounts: map[int64]*Account{1: a}}}
			body := `{"data":[{"id":"gpt-test","context_window":1000,"max_output_tokens":100,"reasoning":false,"supports_function_calling":false}]}`
			next := body
			if change == "membership" {
				next = `{"data":[]}`
			}
			if change == "capability" {
				next = `{"data":[{"id":"gpt-test","context_window":2000}]}`
			}
			s := &AccountTestService{accountRepo: r, cfg: upstreamModelSyncTestConfig(), modelMetadataRegistryKey: ModelRegistryDownloadKey(modelsDevRegistryURL, "", "verified-tls"), modelMetadataRegistry: map[string]modelsDevProvider{"openai": {}}, modelMetadataRegistryAt: time.Now(), httpUpstream: &httpUpstreamRecorder{responses: []*http.Response{catalogFixtureResponse(body), catalogFixtureResponse(next)}}}
			preview, err := s.PreviewAccountModelCatalog(context.Background(), a)
			require.NoError(t, err)
			require.Zero(t, r.commits)
			// The approval survives a client JSON round trip.
			encoded, err := json.Marshal(preview.Review)
			require.NoError(t, err)
			var review AccountCatalogReview
			require.NoError(t, json.Unmarshal(encoded, &review))
			switch change {
			case "identity":
				a.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 1}
			case "rules":
				a.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{RulesRevision: 1}
			case "snapshot":
				a.Extra[UpstreamModelCatalogExtraKey] = &AccountModelCatalogSnapshot{Revision: 1, Complete: true}
			case "disabled":
				a.Status = "disabled"
			case "account":
				review.AccountID++
			case "release":
				review.Release.SourceGeneration++
			}
			_, err = s.ApplyReviewedAccountModelCatalog(context.Background(), a, &review)
			if change == "none" {
				require.NoError(t, err)
				require.Equal(t, 1, r.commits)
				require.Equal(t, 2, a.GetUpstreamModelCatalog().SchemaVersion)
			} else {
				require.ErrorIs(t, err, ErrAccountModelCatalogConflict)
				require.Zero(t, r.commits)
			}
		})
	}
}

func TestAccountModelCatalogReviewedAllPlatformProfileEvidence(t *testing.T) {
	require.ElementsMatch(t, domain.ConcretePlatforms(), ConcretePlatforms())
	for _, platform := range ConcretePlatforms() {
		for _, profile := range []struct{ kind, region, base string }{{AccountTypeAPIKey, "", ""}, {AccountTypeOAuth, "us-east-1", ""}, {AccountTypeAPIKey, "cn-hangzhou", "https://custom.example/v1"}} {
			t.Run(platform+"/"+profile.kind+"/"+profile.region, func(t *testing.T) {
				a := &Account{ID: 1, Platform: platform, Type: profile.kind, Credentials: map[string]any{"base_url": profile.base, "aws_region": profile.region}}
				catalog := &UpstreamModelCatalog{SchemaVersion: 2, EnrichmentVersion: "capability-v2", SourceGeneration: 7, SourceVersions: map[string]string{"local_catalog": "approved"}, Models: []string{"native"}, Metadata: map[string]UpstreamModelMetadata{"native": {TaskType: "text_generation"}}}
				catalog.snapshot = projectAccountModelCatalog(a, catalog)
				first, err := newAccountCatalogReview(a.ID, a.ModelCatalogBaseline(), catalog)
				require.NoError(t, err)
				require.Equal(t, 2, first.Release.SchemaVersion)
				require.Equal(t, "incomplete", catalog.snapshot.CapabilityStatus)
				catalog.ObservedAt = "later"
				for i := range catalog.EligibleModels {
					catalog.EligibleModels[i].EvidenceObservedAt = "later"
				}
				second, err := newAccountCatalogReview(a.ID, a.ModelCatalogBaseline(), catalog)
				require.NoError(t, err)
				require.Equal(t, first.Digest, second.Digest)
				catalog.SourceVersions["local_catalog"] = "unapproved"
				third, err := newAccountCatalogReview(a.ID, a.ModelCatalogBaseline(), catalog)
				require.NoError(t, err)
				require.NotEqual(t, first.Digest, third.Digest)
			})
		}
	}
}
