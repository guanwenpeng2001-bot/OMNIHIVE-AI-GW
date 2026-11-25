//go:build integration

package repository

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

const grokCatalogFixtureToken = "fixture-access"

func catalogIdentityFixture(t *testing.T) *credentialSecretFixture {
	t.Helper()
	ctx := t.Context()
	workspaceID, err := domain.ResolveDefaultWorkspaceID(ctx, integrationDB)
	require.NoError(t, err)
	repo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	repo.credentialEncryptor = persistentTestAES(t, true)
	return &credentialSecretFixture{
		ctx: domain.WithDefaultWorkspaceID(ctx, workspaceID), db: integrationDB, client: integrationEntClient, accounts: repo,
	}
}

type grokCatalogOAuthFixture struct{ service.GrokOAuthService }

func (grokCatalogOAuthFixture) RefreshAccountToken(context.Context, *service.Account) (*service.GrokTokenInfo, error) {
	return &service.GrokTokenInfo{AccessToken: "fixture-renewed-access", RefreshToken: "fixture-renewed-refresh"}, nil
}

func TestGrokOAuthCatalogRenewalAndSelectionPostgres(t *testing.T) {
	for _, scenario := range []struct {
		name string
		sub  string
		base string
	}{
		{name: "referenced-with-subject", sub: "fixture-subject", base: "https://fixture-relay.example"},
		{name: "referenced-with-opaque-tokens"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f := catalogIdentityFixture(t)
			ctx, repo := f.ctx, f.accounts
			credentials := map[string]any{
				"access_token": grokCatalogFixtureToken, "refresh_token": "fixture-refresh", "team_id": "fixture-team",
			}
			if scenario.sub != "" {
				credentials["sub"] = scenario.sub
			}
			if scenario.base != "" {
				credentials["base_url"] = scenario.base
			}
			row := f.createAccount(t, ctx, uniqueName("catalog-reference"), service.PlatformGrok, service.AccountTypeOAuth, credentials)
			load := func() *service.Account {
				account, err := repo.GetByID(ctx, row.ID)
				require.NoError(t, err)
				return account
			}
			_, err := f.db.ExecContext(ctx, `UPDATE accounts SET extra = '{
				"upstream_model_catalog":{"revision":3,"identity_revision":4,"complete":true},
				"upstream_model_catalog_state":{"identity_revision":4,"rules_revision":8,"result":"succeeded"}
			}'::jsonb WHERE id=$1`, row.ID)
			require.NoError(t, err)
			before := load()
			renewal := service.NewOAuthRefreshAPI(repo, nil)
			refresher := service.NewGrokTokenRefresher(&grokCatalogOAuthFixture{})
			result, err := renewal.RefreshIfNeeded(ctx, before, refresher, time.Minute)
			require.NoError(t, err)
			require.True(t, result.Refreshed)
			after := load()
			require.NotEqual(t, before.GetCredential("_token_version"), after.GetCredential("_token_version"))
			require.Equal(t, "fixture-renewed-access", after.GetGrokAccessToken())
			require.Equal(t, before.Credentials["base_url"], after.Credentials["base_url"])
			require.Equal(t, service.CredentialVersion(before.Credentials)+1, service.CredentialVersion(after.Credentials))
			require.Equal(t, before.ModelCatalogBaseline(), after.ModelCatalogBaseline())
			require.NotNil(t, after.EffectiveUpstreamModelCatalog())

			// Both the dedicated writer and the general admin configuration write
			// use the locked selection while keeping OAuth credentials intact.
			stored := f.storedCredentials(t, row.ID)
			selector := service.NewAccountModelSelectionService(repo)
			rules := after.ModelCatalogState().RulesRevision
			_, err = selector.SaveModelSelection(ctx, row.ID, service.ModelSelectionCommand{
				Mode: service.ModelSelectionRestricted, Models: []string{"grok-4.7"}, ActorID: 7, ExpectedRulesRevision: &rules,
			})
			require.NoError(t, err)
			require.Equal(t, stored, f.storedCredentials(t, row.ID))
			after = load()
			require.EqualValues(t, 4, after.ModelCatalogState().IdentityRevision)
			require.EqualValues(t, 9, after.ModelCatalogState().RulesRevision)
			after.ModelSelectionCommand = &service.ModelSelectionCommand{
				Mode: service.ModelSelectionRestricted, Models: []string{"grok-4.6"}, ActorID: 7, Source: service.ModelSelectionAccountWrite,
			}
			require.NoError(t, repo.UpdateWithAccountBillingSettings(ctx, after, nil, nil, nil))
			require.Equal(t, stored, f.storedCredentials(t, row.ID))
			after = load()
			require.EqualValues(t, 4, after.ModelCatalogState().IdentityRevision)
			require.EqualValues(t, 10, after.ModelCatalogState().RulesRevision)
		})
	}
}

type catalogRecoveryUpstream struct{ service.HTTPUpstream }

func (catalogRecoveryUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	if req.Method != http.MethodGet || !strings.HasSuffix(req.URL.Path, "/models") || req.Header.Get("Authorization") != "Bearer "+grokCatalogFixtureToken {
		return nil, fmt.Errorf("unexpected catalog discovery request")
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(
		`{"data":[{"id":"grok-4.5"},{"id":"grok-4.6"},{"id":"grok-4.7"},{"id":"grok-4.7-build-fast","output_modalities":["text"]},{"id":"grok-4.20-reasoning"}]}`,
	))}, nil
}

type catalogRecoveryRepo struct {
	*accountRepository
	targets   map[int64]bool
	committed chan int64
}

func (r *catalogRecoveryRepo) ListActiveWithInvalidatedModelCatalog(ctx context.Context) ([]service.Account, error) {
	accounts, err := r.accountRepository.ListActiveWithInvalidatedModelCatalog(ctx)
	var fixtures []service.Account
	for _, account := range accounts {
		if r.targets[account.ID] {
			fixtures = append(fixtures, account)
		}
	}
	return fixtures, err
}

func (r *catalogRecoveryRepo) CommitAccountModelCatalog(ctx context.Context, id int64, baseline service.AccountModelCatalogBaseline, snapshot *service.AccountModelCatalogSnapshot, state service.AccountModelCatalogState, metadata *service.UpstreamModelMetadataSnapshot, freeze bool) error {
	err := r.accountRepository.CommitAccountModelCatalog(ctx, id, baseline, snapshot, state, metadata, freeze)
	if err == nil && snapshot != nil {
		r.committed <- id
	}
	return err
}

type catalogRecoveryNoPlans struct {
	service.ScheduledTestPlanRepository
}

func (catalogRecoveryNoPlans) ListDue(context.Context, time.Time) ([]*service.ScheduledTestPlan, error) {
	return nil, nil
}

func TestGrokCatalogScheduledIdentityRecoveryPostgres(t *testing.T) {
	f := catalogIdentityFixture(t)
	ctx, repo := f.ctx, f.accounts
	t.Setenv("MODEL_CAPABILITY_OFFLINE", "1")
	models := []string{"grok-4.5", "grok-4.6", "grok-4.7", "grok-4.7-build-fast"}
	keyCtx := service.WithVisibleModelKeyPolicy(ctx, models)
	groups := make(map[int64]int64)
	recoveryRepo := &catalogRecoveryRepo{accountRepository: repo, targets: map[int64]bool{}, committed: make(chan int64, 2)}
	cfg := &config.Config{}
	upstream := catalogRecoveryUpstream{}
	initialDiscovery := service.NewAccountTestService(repo, nil, nil, nil, nil, upstream, cfg, nil)
	gateway := service.NewGatewayService(repo, NewGroupRepository(f.client, f.db), nil, nil, nil, nil, nil, nil, cfg,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	for _, legacy := range []bool{true, false} {
		account := f.createAccount(t, ctx, uniqueName("catalog-recovery"), service.PlatformGrok, service.AccountTypeOAuth, map[string]any{
			"access_token": grokCatalogFixtureToken, "refresh_token": "fixture-refresh", "sub": "fixture-subject", "team_id": "fixture-team",
		})
		require.NoError(t, repo.SetSchedulable(ctx, account.ID, true))
		group := mustCreateGroup(t, f.client, &service.Group{Platform: service.PlatformGrok, Name: uniqueName("catalog-grok-group"), RateMultiplier: 1})
		require.NoError(t, repo.BindGroups(ctx, account.ID, []int64{group.ID}))
		groups[account.ID] = group.ID
		recoveryRepo.targets[account.ID] = true
		_, err := f.db.ExecContext(ctx, `UPDATE accounts SET extra = '{
			"upstream_model_catalog_state":{"identity_revision":4,"rules_revision":8}
		}'::jsonb WHERE id=$1`, account.ID)
		require.NoError(t, err)
		selector := service.NewAccountModelSelectionService(repo)
		_, err = selector.SaveModelSelection(ctx, account.ID, service.ModelSelectionCommand{
			Mode: service.ModelSelectionRestricted, Models: models, ActorID: 7, Source: service.ModelSelectionAccountWrite,
		})
		require.NoError(t, err)
		_, err = initialDiscovery.ApplyAccountModelCatalog(ctx, account, nil)
		require.NoError(t, err)
		view, err := gateway.VisibleModels(keyCtx, &group.ID, service.PlatformGrok)
		require.NoError(t, err)
		require.ElementsMatch(t, models, view.Models)
		if legacy {
			// Mirrors the historical SQL writer in v0.3.8: retain the successful
			// attempt timestamp and result, while advancing the identity alone.
			_, err = f.db.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(extra,
				'{upstream_model_catalog_state,identity_revision}','5') #- '{upstream_model_catalog_state,attempted_identity_revision}'
				WHERE id=$1`, account.ID)
			require.NoError(t, err)
		} else {
			old, err := repo.GetByID(ctx, account.ID)
			require.NoError(t, err)
			nextCredentials := maps.Clone(old.Credentials)
			nextCredentials["team_id"] = "fixture-new-team"
			applied, err := repo.UpdateGrokOAuthCredentialsIfUnchanged(ctx, old.ID, old.Credentials, old.ProxyID, old.ProxyGroupID, nextCredentials)
			require.NoError(t, err)
			require.True(t, applied)
		}
		stale, err := repo.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.EqualValues(t, 5, stale.ModelCatalogState().IdentityRevision)
		require.EqualValues(t, 4, stale.GetUpstreamModelCatalog().IdentityRevision)
		require.Nil(t, stale.EffectiveUpstreamModelCatalog())
		view, err = gateway.VisibleModels(keyCtx, &group.ID, service.PlatformGrok)
		require.NoError(t, err)
		require.Empty(t, view.Models)
	}

	accountTests := service.NewAccountTestService(recoveryRepo, nil, nil, nil, nil, upstream, cfg, nil)
	runner := service.NewScheduledTestRunnerService(catalogRecoveryNoPlans{}, nil, accountTests, nil, cfg)
	runner.Start()
	defer runner.Stop()
	deadline := time.NewTimer(75 * time.Second)
	defer deadline.Stop()
	recovered := make(map[int64]bool, len(groups))
	for range groups {
		select {
		case id := <-recoveryRepo.committed:
			recovered[id] = true
		case <-deadline.C:
			t.Fatal("catalogs did not recover on the next scheduled refresh")
		}
	}
	for id, groupID := range groups {
		require.True(t, recovered[id])
		saved, err := repo.GetByID(ctx, id)
		require.NoError(t, err)
		require.EqualValues(t, 5, saved.GetUpstreamModelCatalog().IdentityRevision)
		require.EqualValues(t, 2, saved.GetUpstreamModelCatalog().Revision)
		require.Equal(t, "succeeded", saved.ModelCatalogState().Result)
		require.Equal(t, models, service.AccountModelSelectionView(saved).Scope.Models)
		effective := saved.EffectiveUpstreamModelCatalog()
		require.NotNil(t, effective)
		var eligible []string
		for _, member := range effective.EligibleModels {
			eligible = append(eligible, member.ID)
		}
		require.ElementsMatch(t, models, eligible)
		view, err := gateway.VisibleModels(keyCtx, &groupID, service.PlatformGrok)
		require.NoError(t, err)
		require.ElementsMatch(t, models, view.Models)
	}
}
