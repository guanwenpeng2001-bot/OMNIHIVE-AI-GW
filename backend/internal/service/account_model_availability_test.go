package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRestrictedScopeProvidesConcreteUpstreamEvidence(t *testing.T) {
	const direct, private, outside = "direct-model", "private-model", "outside-model"
	account := catalogEvidenceAccount(42, PlatformOpenAI, "native")
	account.Credentials["model_mapping"] = map[string]string{"alias": private, "stale": outside}
	account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{direct, private}}
	require.True(t, account.IsModelSupported(direct))
	require.False(t, account.IsModelSupported(private))

	require.Equal(t, []string{direct, private}, configuredUpstreamModelsForCapabilitySync(&account))
	enricher, err := NewCapabilityReadOnlyEnricher(nil, nil)
	require.NoError(t, err)
	projection := enricher.Project(&account, false)
	require.Contains(t, projection, direct)
	require.Contains(t, projection, private)
	require.NotContains(t, projection, outside)
	require.NotContains(t, projection, "alias")

	snapshot := account.GetUpstreamModelCatalog()
	populateAccountCatalogCapabilities(&account, &UpstreamModelCatalog{}, snapshot)
	require.Equal(t, 2, snapshot.CapabilitySummary.MappingTargets)
	require.Equal(t, 3, snapshot.CapabilitySummary.Total)

	candidates, err := BuildPlazaCandidates(&Channel{GroupIDs: []int64{7}}, 7, PlatformOpenAI, []Account{account})
	require.NoError(t, err)
	require.Equal(t, []PlazaModelIdentity{
		{Platform: PlatformOpenAI, Name: direct},
		{Platform: PlatformOpenAI, Name: "native"},
		{Platform: PlatformOpenAI, Name: private},
	}, candidates.Candidates)

	now := time.Now()
	change := ModelAvailabilityChange{AccountModelCatalogBaseline: account.ModelCatalogBaseline(),
		Endpoint: CompositeRouteEndpointChatCompletions, Reason: "verified", Confirmed: true}
	declaration := ModelAvailabilityDeclaration{AccountID: account.ID, IdentityRevision: change.IdentityRevision,
		Endpoint: change.Endpoint, Reason: change.Reason, ConfirmedBy: 7, ConfirmedAt: now, EvidenceKind: ModelEvidenceAdministrator}
	for _, model := range []string{direct, private} {
		change.Model, declaration.Model = model, model
		require.NoError(t, ValidateModelAvailabilityChange(&account, change, 7, false, now))
		require.True(t, declaration.ValidFor(&account, nil, now))
	}
	change.Model, declaration.Model = outside, outside
	require.ErrorIs(t, ValidateModelAvailabilityChange(&account, change, 7, false, now), ErrModelAvailabilityInvalid)
	require.False(t, declaration.ValidFor(&account, nil, now))
}

func TestUnlistedModelEligibility(test *testing.T) {
	fixture := func() Account {
		account := catalogEvidenceAccount(42, PlatformOpenAI, "native")
		account.Credentials["model_mapping"] = map[string]any{"private-alias": "private-target", "other-alias": "other-target"}
		account.Extra[ModelAvailabilityExtraKey] = []ModelAvailabilityDeclaration{{AccountID: 42, Model: "private-target", Endpoint: "chat_completions", ConfirmedBy: 7, ConfirmedAt: time.Now().Add(-time.Minute), Reason: "deployment verified", EvidenceKind: ModelEvidenceAdministrator}}
		return account
	}
	options := AccountModelBindingOptions{FinalModel: "private-target", Endpoint: "chat_completions"}
	for _, testCase := range []struct {
		name    string
		change  func(*Account)
		allowed bool
	}{
		{"declared mapped target", func(*Account) {}, true},
		{"without native discovery", func(account *Account) { delete(account.Extra, UpstreamModelCatalogExtraKey) }, true},
		{"other account", func(account *Account) { account.ID++ }, true},
		{"mapping removed", func(account *Account) { delete(account.Credentials, "model_mapping") }, false},
		{"identity replaced", func(account *Account) {
			account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 1}
		}, false},
		{"configuration changed", func(account *Account) {
			account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{RulesRevision: 1}
		}, true},
		{"credentials invalid", func(account *Account) {
			account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{Result: "credentials_invalid"}
		}, true},
		{"account allowlist excludes", func(account *Account) {
			account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{"native"}}
		}, false},
		{"expired", func(account *Account) {
			declarations := account.ModelAvailabilityDeclarations()
			expiry := time.Now().Add(-time.Second)
			declarations[0].ExpiresAt = &expiry
			account.Extra[ModelAvailabilityExtraKey] = declarations
		}, true},
		{"revoked", func(account *Account) {
			declarations := account.ModelAvailabilityDeclarations()
			now := time.Now()
			declarations[0].RevokedAt = &now
			account.Extra[ModelAvailabilityExtraKey] = declarations
		}, true},
		{"capability defaults only", func(account *Account) {
			delete(account.Extra, ModelAvailabilityExtraKey)
			account.Extra[ModelCapabilityDefaultsExtraKey] = map[string]any{"models": map[string]any{"private-target": map[string]any{"supported_methods": []string{"chat_completions"}}}}
		}, true},
		{"historical usage only", func(account *Account) {
			declarations := account.ModelAvailabilityDeclarations()
			declarations[0].EvidenceKind = "historical_usage"
			account.Extra[ModelAvailabilityExtraKey] = declarations
		}, true},
		{"disabled account", func(account *Account) { account.Schedulable = false }, false},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			account := fixture()
			testCase.change(&account)
			before, err := json.Marshal(account)
			require.NoError(test, err)
			require.Equal(test, testCase.allowed, AccountSupportsResolvedModel(context.Background(), &account, "private-alias", options))
			after, err := json.Marshal(account)
			require.NoError(test, err)
			require.JSONEq(test, string(before), string(after), "reading evidence cannot mutate native discovery or account configuration")
		})
	}
	account := fixture()
	require.True(test, AccountSupportsResolvedModel(context.Background(), &account, "other-alias", AccountModelBindingOptions{FinalModel: "other-target", Endpoint: "chat_completions"}))
	require.True(test, AccountSupportsResolvedModel(context.Background(), &account, "private-alias", AccountModelBindingOptions{FinalModel: "private-target", Endpoint: "images"}))
	require.True(test, AccountSupportsResolvedModel(context.Background(), &account, "private-alias", AccountModelBindingOptions{Endpoint: "chat_completions"}))
	require.Equal(test, []string{"native"}, account.GetUpstreamModelCatalog().DiscoveredModels)
	test.Run("malformed_evidence_fails_closed", func(test *testing.T) {
		account := fixture()
		account.Extra[ModelAvailabilityExtraKey] = []any{account.ModelAvailabilityDeclarations()[0], map[string]any{"account_id": "invalid"}}
		require.True(test, AccountSupportsResolvedModel(context.Background(), &account, "private-alias", options))
	})
	test.Run("catalog_cannot_borrow_a_declaration_for_another_endpoint", func(test *testing.T) {
		account := fixture()
		declaration := account.ModelAvailabilityDeclarations()[0]
		snapshot := account.GetUpstreamModelCatalog()
		snapshot.EligibleModels = append(snapshot.EligibleModels, AccountCatalogModel{ID: "private-target", Endpoints: []string{"chat_completions", "images"}, EvidenceKind: ModelEvidenceAdministrator, EvidenceObservedAt: declaration.ConfirmedAt.Format(time.RFC3339Nano)})
		account.Extra[UpstreamModelCatalogExtraKey] = snapshot
		require.True(test, AccountSupportsResolvedModel(context.Background(), &account, "private-alias", AccountModelBindingOptions{FinalModel: "private-target", Endpoint: "images"}))
	})

	test.Run("shadow parent binding", func(test *testing.T) {
		account := fixture()
		parent := catalogEvidenceAccount(8, PlatformOpenAI, "native")
		parent.Type = AccountTypeOAuth
		account.ParentAccountID = &parent.ID
		account.QuotaDimension = "spark"
		declarations := account.ModelAvailabilityDeclarations()
		declarations[0].CredentialAccountID = parent.ID
		account.Extra[ModelAvailabilityExtraKey] = declarations
		require.True(test, AccountSupportsResolvedModel(context.Background(), &account, "private-alias", AccountModelBindingOptions{FinalModel: "private-target", Endpoint: "chat_completions", CredentialAccount: &parent}))
		parent.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 1}
		require.False(test, AccountSupportsResolvedModel(context.Background(), &account, "private-alias", AccountModelBindingOptions{FinalModel: "private-target", Endpoint: "chat_completions", CredentialAccount: &parent}))
	})

	test.Run("probe monthly expiry", func(test *testing.T) {
		account := fixture()
		declarations := account.ModelAvailabilityDeclarations()
		declarations[0].EvidenceKind = ModelEvidenceProbe
		declarations[0].ConfirmedBy = 0
		account.Extra[ModelAvailabilityExtraKey] = declarations
		require.True(test, AccountSupportsResolvedModel(context.Background(), &account, "private-alias", options))
		expires := declarations[0].ConfirmedAt.AddDate(0, 1, 0)
		declarations[0].ExpiresAt = &expires
		account.Extra[ModelAvailabilityExtraKey] = declarations
		require.True(test, declarations[0].ValidFor(&account, nil, expires.Add(-time.Second)))
		require.False(test, declarations[0].ValidFor(&account, nil, expires))
	})
}

func TestModelProbeBudgetWindows(test *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	var budget ModelProbeBudget
	for count := 0; count < 50; count++ {
		at := now.AddDate(0, 0, count/5)
		require.NoError(test, budget.Reserve(ModelProbeAttempt{ID: fmt.Sprint(count), ReservedAt: at, Endpoint: "chat_completions"}))
		if count%5 == 4 {
			require.ErrorIs(test, budget.Reserve(ModelProbeAttempt{ReservedAt: at, Endpoint: "chat_completions"}), ErrModelProbeBudgetExceeded)
		}
	}
	require.ErrorIs(test, budget.Reserve(ModelProbeAttempt{ReservedAt: now.AddDate(0, 0, 11), Endpoint: "chat_completions"}), ErrModelProbeBudgetExceeded)
	// A restart and pruning result detail do not reset counters.
	encoded, err := json.Marshal(budget)
	require.NoError(test, err)
	var restarted ModelProbeBudget
	require.NoError(test, json.Unmarshal(encoded, &restarted))
	restarted.Attempts = nil
	require.ErrorIs(test, restarted.Reserve(ModelProbeAttempt{ReservedAt: now.AddDate(0, 0, 12)}), ErrModelProbeBudgetExceeded)
	require.NoError(test, restarted.Reserve(ModelProbeAttempt{ReservedAt: now.AddDate(0, 1, 0)}))
	require.Equal(test, 1, restarted.MonthlyUsed)
	require.Equal(test, int64(51), restarted.Revision)
	require.Len(test, restarted.Attempts, 1)
	var media ModelProbeBudget
	for day := 0; day < 8; day++ {
		at := now.AddDate(0, 0, day)
		require.NoError(test, media.Reserve(ModelProbeAttempt{ReservedAt: at, Endpoint: "images"}))
		require.ErrorIs(test, media.Reserve(ModelProbeAttempt{ReservedAt: at, Endpoint: "videos"}), ErrModelProbeBudgetExceeded)
	}
	require.Equal(test, 8, media.MonthlyUsed)
	require.ErrorIs(test, media.Reserve(ModelProbeAttempt{ReservedAt: now.AddDate(0, 0, 8), Endpoint: "images"}), ErrModelProbeBudgetExceeded)
	require.NoError(test, media.Reserve(ModelProbeAttempt{ReservedAt: now.AddDate(0, 0, 8), Endpoint: "chat_completions"}))
	require.ErrorIs(test, media.Reserve(ModelProbeAttempt{ReservedAt: now, Endpoint: "chat_completions"}), ErrModelProbeBudgetExceeded)
}

func TestModelProbeOutcomeClassification(test *testing.T) {
	for _, status := range []int{0, 400, 401, 403, 404, 429, 500, 503} {
		require.Equal(test, ModelProbeTemporary, ClassifyModelProbeOutcome(status, true, false, false))
	}
	require.Equal(test, ModelProbeMissing, ClassifyModelProbeOutcome(400, true, true, false))
	require.Equal(test, ModelProbeMissing, ClassifyModelProbeOutcome(404, true, true, false))
	require.Equal(test, ModelProbeTemporary, ClassifyModelProbeOutcome(404, false, true, false))
	require.Equal(test, ModelProbeTemporary, ClassifyModelProbeOutcome(202, true, false, false), "async submission is not final media success")
	require.Equal(test, ModelProbeSucceeded, ClassifyModelProbeOutcome(200, true, false, true))
}
