//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestModelAvailabilityDiagnosisExplicitScope(t *testing.T) {
	groupID := int64(42)
	for _, gateway := range []string{"generic", "openai"} {
		for _, scenario := range []struct {
			name      string
			mapping   map[string]any
			requested string
			scope     AccountModelAllowlist
			transient bool
			supported bool
		}{
			{"direct selected", map[string]any{"Private-ID": "Private-ID"}, "Private-ID", AccountModelAllowlist{Enabled: true, Models: []string{"Private-ID"}}, false, true},
			{"direct excluded", map[string]any{"Private-ID": "Private-ID"}, "Private-ID", AccountModelAllowlist{Enabled: true, Models: []string{"other"}}, false, false},
			{"alias selected by target", map[string]any{"alias": "Private-ID"}, "alias", AccountModelAllowlist{Enabled: true, Models: []string{"Private-ID"}}, false, true},
			{"alias excluded", map[string]any{"alias": "Private-ID"}, "alias", AccountModelAllowlist{Enabled: true, Models: []string{"other"}}, false, false},
			{"selected ID ignores an out-of-scope alias target", map[string]any{"alias": "Private-ID"}, "alias", AccountModelAllowlist{Enabled: true, Models: []string{"alias"}}, false, true},
			{"deny all selected", map[string]any{"alias": "Private-ID"}, "alias", AccountModelAllowlist{Enabled: true}, false, false},
			{"deny all empty mapping", nil, "Private-ID", AccountModelAllowlist{Enabled: true}, false, false},
			{"disabled filter", map[string]any{"alias": "Private-ID"}, "alias", AccountModelAllowlist{Enabled: false, Models: []string{"other"}}, false, true},
			{"selected unaliased model", map[string]any{"selected": "other"}, "Private-ID", AccountModelAllowlist{Enabled: true, Models: []string{"Private-ID"}}, false, true},
			{"exact case preserved", map[string]any{"alias": "Private-ID"}, "alias", AccountModelAllowlist{Enabled: true, Models: []string{"private-id"}}, false, false},
			{"wildcard resolves target", map[string]any{"public-*": "Private-ID"}, "public-test", AccountModelAllowlist{Enabled: true, Models: []string{"Private-ID"}}, false, true},
			{"temporary failures preserve support", map[string]any{"alias": "Private-ID"}, "alias", AccountModelAllowlist{Enabled: true, Models: []string{"Private-ID"}}, true, true},
			{"explicit exclusion wins over temporary failure", map[string]any{"alias": "Private-ID"}, "alias", AccountModelAllowlist{Enabled: true, Models: []string{"other"}}, true, false},
		} {
			t.Run(gateway+"/"+scenario.name, func(t *testing.T) {
				future, past := time.Now().Add(time.Hour), time.Now().Add(-time.Hour)
				account := Account{ID: 91, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
					AccountGroups: []AccountGroup{{GroupID: groupID}}, Credentials: map[string]any{"model_mapping": scenario.mapping},
					Extra: map[string]any{UpstreamModelAllowlistExtraKey: scenario.scope}}
				if scenario.transient {
					account.RateLimitResetAt, account.OverloadUntil, account.TempUnschedulableUntil, account.ExpiresAt = &future, &future, &future, &past
					account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{Result: "credentials_invalid"}
				}
				// No discovery snapshot or capability facts, including under a real
				// request-plan context: neither can become a diagnostic admission gate.
				ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Model: scenario.requested, Protocol: "openai"}))
				repo := &mockAccountRepoForPlatform{accounts: []Account{account}}
				var diagnoser ModelAvailabilityDiagnoser = &GatewayService{accountRepo: repo, cfg: testConfig(), schedulerSnapshot: &SchedulerSnapshotService{}}
				if gateway == "openai" {
					diagnoser = &OpenAIGatewayService{accountRepo: repo, cfg: testConfig(), schedulerSnapshot: &SchedulerSnapshotService{}}
				}
				diagnosis := diagnoser.DiagnoseModelAvailabilityForPlatform(ctx, &groupID, scenario.requested, PlatformOpenAI)
				require.True(t, diagnosis.HasAccountsInPool)
				require.Equal(t, scenario.supported, diagnosis.HasModelSupport)
			})
		}
	}
}

func TestModelAvailabilityDiagnosisPassthroughWireScope(t *testing.T) {
	for _, gateway := range []string{"generic", "openai"} {
		for _, websocket := range []bool{false, true} {
			for _, scope := range []AccountModelAllowlist{{Enabled: true, Models: []string{"wire-name"}}, {Enabled: true, Models: []string{"mapped-target"}}, {Enabled: true}} {
				account := catalogEvidenceAccount(1, PlatformOpenAI)
				account.Credentials["model_mapping"] = map[string]string{"wire-name": "mapped-target"}
				account.Extra["openai_passthrough"] = !websocket
				account.Extra["openai_apikey_responses_websockets_v2_mode"] = OpenAIWSIngressModePassthrough
				account.Extra[UpstreamModelAllowlistExtraKey] = scope
				future, past := time.Now().Add(time.Hour), time.Now().Add(-time.Hour)
				account.RateLimitResetAt, account.OverloadUntil, account.TempUnschedulableUntil, account.ExpiresAt = &future, &future, &future, &past
				ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Model: "wire-name", Protocol: "responses", Action: "responses"}))
				if websocket {
					ctx = WithOpenAIWebSocketSelection(ctx, OpenAIWSIngressModeCtxPool)
				}
				repo := &mockAccountRepoForPlatform{accounts: []Account{account}}
				var diagnoser ModelAvailabilityDiagnoser = &GatewayService{accountRepo: repo, cfg: testConfig()}
				if gateway == "openai" {
					diagnoser = &OpenAIGatewayService{accountRepo: repo, cfg: testConfig()}
				}
				diagnosis := diagnoser.DiagnoseModelAvailabilityForPlatform(ctx, nil, "wire-name", PlatformOpenAI)
				require.True(t, diagnosis.HasAccountsInPool)
				require.Equal(t, len(scope.Models) > 0 && scope.Models[0] == "wire-name", diagnosis.HasModelSupport)
			}
		}
	}
}

func TestModelAvailabilityDiagnosisRequestedAliasNormalization(t *testing.T) {
	for _, platform := range []string{PlatformGemini, PlatformAntigravity} {
		account := catalogEvidenceAccount(1, platform)
		account.Credentials["model_mapping"] = map[string]string{"gemini-3.1-pro-preview": "gemini-3.1-pro-preview"}
		diagnoser := &GatewayService{accountRepo: &mockAccountRepoForPlatform{accounts: []Account{account}}, cfg: testConfig()}
		require.True(t, diagnoser.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "gemini-3.1-pro-preview-customtools", platform).HasModelSupport)
		require.False(t, diagnoser.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "gemini-unselected", platform).HasModelSupport)
	}
}

func TestModelAvailabilityDiagnosisWebSocketScopeRemainsLiteral(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI)
	account.Type = AccountTypeOAuth
	account.Credentials["model_mapping"] = map[string]string{"gpt-5.4": "different-target"}
	account.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModePassthrough
	ctx := WithOpenAIWebSocketSelection(context.Background(), OpenAIWSIngressModePassthrough)
	for _, allowed := range []string{"gpt-5.4", "gpt-5.4-high"} {
		account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{allowed}}
		binding := ResolveAccountModelBinding(ctx, &account, "gpt-5.4", AccountModelBindingOptions{Endpoint: "responses"})
		require.Equal(t, allowed == "gpt-5.4", binding.Supported)
		require.Equal(t, binding.Supported, accountModelScopeAllowsDiagnosis(ctx, &account, "gpt-5.4"))
	}
}

type modelDiagnosisFailingRepository struct{ AccountRepository }

func (modelDiagnosisFailingRepository) ListModelAvailabilityCandidates(context.Context, *int64, []string, bool) ([]Account, error) {
	return nil, errors.New("fixture query unavailable")
}

func TestModelAvailabilityDiagnosisConservativeFailureAndScopeIsolation(t *testing.T) {
	groupID := int64(42)
	for _, gateway := range []string{"generic", "openai"} {
		for _, scenario := range []struct {
			name     string
			repo     AccountRepository
			expected ModelAvailabilityDiagnosis
		}{
			{"query error remains service unavailable", modelDiagnosisFailingRepository{}, ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}},
			{"empty pool remains service unavailable", &mockAccountRepoForPlatform{}, ModelAvailabilityDiagnosis{}},
			{"foreign group cannot supply support", &mockAccountRepoForPlatform{accounts: []Account{{ID: 1, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, AccountGroups: []AccountGroup{{GroupID: 99}}}}}, ModelAvailabilityDiagnosis{}},
			{"foreign platform cannot supply support", &mockAccountRepoForPlatform{accounts: []Account{{ID: 1, Platform: PlatformAnthropic, Status: StatusActive, Schedulable: true, AccountGroups: []AccountGroup{{GroupID: groupID}}}}}, ModelAvailabilityDiagnosis{}},
		} {
			t.Run(gateway+"/"+scenario.name, func(t *testing.T) {
				var diagnoser ModelAvailabilityDiagnoser = &GatewayService{accountRepo: scenario.repo, cfg: testConfig()}
				if gateway == "openai" {
					diagnoser = &OpenAIGatewayService{accountRepo: scenario.repo, cfg: testConfig()}
				}
				require.Equal(t, scenario.expected, diagnoser.DiagnoseModelAvailabilityForPlatform(context.Background(), &groupID, "Private-ID", PlatformOpenAI))
			})
		}
	}
}

func TestDiagnoseModelAvailabilityForPlatform_NoModel_AlwaysAvailable(t *testing.T) {
	repo := &mockAccountRepoForPlatform{accounts: nil, accountsByID: map[int64]*Account{}}
	svc := &GatewayService{accountRepo: repo, cfg: testConfig()}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "", PlatformOpenAI)

	require.True(t, diag.HasAccountsInPool, "empty model must return HasAccountsInPool=true so caller stays on 503")
	require.True(t, diag.HasModelSupport, "empty model must return HasModelSupport=true so caller stays on 503")
}

func TestDiagnoseModelAvailabilityForPlatform_EmptyPlatform_AlwaysAvailable(t *testing.T) {
	repo := &mockAccountRepoForPlatform{accounts: nil, accountsByID: map[int64]*Account{}}
	svc := &GatewayService{accountRepo: repo, cfg: testConfig()}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "gpt-5", "")

	require.True(t, diag.HasAccountsInPool)
	require.True(t, diag.HasModelSupport, "empty platform must fall back to {true,true} so caller stays on 503")
}

func TestDiagnoseModelAvailabilityForPlatform_NilReceiver(t *testing.T) {
	var svc *GatewayService

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "gpt-5", PlatformOpenAI)

	require.True(t, diag.HasAccountsInPool)
	require.True(t, diag.HasModelSupport)
}

func TestDiagnoseModelAvailabilityForPlatform_NoAccountsInPool(t *testing.T) {
	repo := &mockAccountRepoForPlatform{accounts: nil, accountsByID: map[int64]*Account{}}
	svc := &GatewayService{accountRepo: repo, cfg: testConfig()}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "gpt-5", PlatformOpenAI)

	require.False(t, diag.HasAccountsInPool)
	require.False(t, diag.HasModelSupport, "no accounts means no support; caller stays on 503 (empty-pool branch)")
}

func TestDiagnoseModelAvailabilityForPlatform_ExplicitMappingMatches(t *testing.T) {
	repo := &mockAccountRepoForPlatform{
		accounts: []Account{
			{
				ID:          1,
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"gpt-5.1-codex-mini": "gpt-5.1-codex-mini"},
				},
			},
		},
		accountsByID: map[int64]*Account{},
	}
	for i := range repo.accounts {
		repo.accountsByID[repo.accounts[i].ID] = &repo.accounts[i]
	}
	svc := &GatewayService{accountRepo: repo, cfg: testConfig()}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "gpt-5.1-codex-mini", PlatformOpenAI)

	require.True(t, diag.HasAccountsInPool)
	require.True(t, diag.HasModelSupport)
}

func TestDiagnoseModelAvailabilityForPlatform_EmptyMappingAllowsAll(t *testing.T) {
	repo := &mockAccountRepoForPlatform{
		accounts: []Account{
			{ID: 1, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true /* no ModelMapping = allow all */},
		},
		accountsByID: map[int64]*Account{},
	}
	for i := range repo.accounts {
		repo.accountsByID[repo.accounts[i].ID] = &repo.accounts[i]
	}
	svc := &GatewayService{accountRepo: repo, cfg: testConfig()}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "gpt-5.1-codex-mini", PlatformOpenAI)

	require.True(t, diag.HasModelSupport, "empty model_mapping must be treated as 'allow all' (Account.IsModelSupported semantics)")
}

func TestDiagnoseModelAvailabilityForPlatform_WildcardMappingMatches(t *testing.T) {
	repo := &mockAccountRepoForPlatform{
		accounts: []Account{
			{
				ID:          1,
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"*": "gpt-5"},
				},
			},
		},
		accountsByID: map[int64]*Account{},
	}
	for i := range repo.accounts {
		repo.accountsByID[repo.accounts[i].ID] = &repo.accounts[i]
	}
	svc := &GatewayService{accountRepo: repo, cfg: testConfig()}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "gpt-5.1-codex-mini", PlatformOpenAI)

	require.True(t, diag.HasModelSupport, "wildcard mapping must classify the request as 'serviceable'")
}

func TestDiagnoseModelAvailabilityForPlatform_NoMatchingModel_ReturnsNotFoundSignal(t *testing.T) {
	groupID := int64(42)
	repo := &mockAccountRepoForPlatform{
		accounts: []Account{
			{
				ID:          1,
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
				AccountGroups: []AccountGroup{
					{GroupID: groupID},
				},
				Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5": "gpt-5"}},
			},
			{
				ID:          2,
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
				AccountGroups: []AccountGroup{
					{GroupID: groupID},
				},
				Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5-mini": "gpt-5-mini"}},
			},
		},
		accountsByID: map[int64]*Account{},
	}
	for i := range repo.accounts {
		repo.accountsByID[repo.accounts[i].ID] = &repo.accounts[i]
	}
	svc := &GatewayService{accountRepo: repo, cfg: testConfig()}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), &groupID, "gpt-5.1-codex-mini", PlatformOpenAI)

	require.True(t, diag.HasAccountsInPool, "group has OpenAI accounts")
	require.False(t, diag.HasModelSupport, "no account mapping admits the requested model — handler should return 404")
}

func TestDiagnoseModelAvailabilityForPlatform_RateLimitedSupportingAccountRemainsConfigured(t *testing.T) {
	groupID := int64(42)
	cooldownUntil := time.Now().Add(time.Hour)
	repo := &mockAccountRepoForPlatform{
		accounts: []Account{
			{
				ID:                     1,
				Platform:               PlatformAnthropic,
				Status:                 StatusActive,
				Schedulable:            true,
				RateLimitResetAt:       &cooldownUntil,
				OverloadUntil:          &cooldownUntil,
				TempUnschedulableUntil: &cooldownUntil,
				AccountGroups:          []AccountGroup{{GroupID: groupID}},
				Credentials: map[string]any{
					"model_mapping": map[string]any{"claude-opus-4-8": "claude-opus-4-8"},
				},
			},
		},
		accountsByID: map[int64]*Account{},
	}
	require.False(t, repo.accounts[0].IsSchedulable(), "test account must be excluded from normal scheduling while cooling down")
	svc := &GatewayService{
		accountRepo:       repo,
		cfg:               testConfig(),
		schedulerSnapshot: &SchedulerSnapshotService{}, // diagnosis must bypass the transient-only snapshot
	}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), &groupID, "claude-opus-4-8", PlatformAnthropic)

	require.True(t, diag.HasAccountsInPool)
	require.True(t, diag.HasModelSupport, "a configured model remains supported while every matching account is temporarily cooling down")
}

func TestOpenAIDiagnoseModelAvailabilityForPlatform_RateLimitedSupportingAccountRemainsConfigured(t *testing.T) {
	groupID := int64(43)
	cooldownUntil := time.Now().Add(time.Hour)
	repo := &mockAccountRepoForPlatform{
		accounts: []Account{
			{
				ID:                     2,
				Platform:               PlatformOpenAI,
				Status:                 StatusActive,
				Schedulable:            true,
				RateLimitResetAt:       &cooldownUntil,
				OverloadUntil:          &cooldownUntil,
				TempUnschedulableUntil: &cooldownUntil,
				AccountGroups:          []AccountGroup{{GroupID: groupID}},
				Credentials: map[string]any{
					"model_mapping": map[string]any{"claude-opus-4-8": "claude-opus-4-8"},
				},
			},
		},
		accountsByID: map[int64]*Account{},
	}
	require.False(t, repo.accounts[0].IsSchedulable(), "test account must be excluded from normal scheduling while cooling down")
	svc := &OpenAIGatewayService{
		accountRepo:       repo,
		cfg:               testConfig(),
		schedulerSnapshot: &SchedulerSnapshotService{}, // diagnosis must bypass the transient-only snapshot
	}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), &groupID, "claude-opus-4-8", PlatformOpenAI)

	require.True(t, diag.HasAccountsInPool)
	require.True(t, diag.HasModelSupport, "OpenAI-compatible diagnosis must keep transiently limited supporting accounts in the configured pool")
}

func TestDiagnoseModelAvailabilityForPlatform_WrongPlatformFiltersOut(t *testing.T) {
	// Group has only Anthropic accounts; user routes to OpenAI gateway.
	// Diagnosis must NOT see Anthropic accounts (listSchedulableAccounts filters
	// by platform), so HasAccountsInPool is false and the caller stays on 503.
	repo := &mockAccountRepoForPlatform{
		accounts: []Account{
			{
				ID:          1,
				Platform:    PlatformAnthropic,
				Status:      StatusActive,
				Schedulable: true,
				Credentials: map[string]any{"model_mapping": map[string]any{"claude-sonnet-4-5": "claude-sonnet-4-5"}},
			},
		},
		accountsByID: map[int64]*Account{},
	}
	for i := range repo.accounts {
		repo.accountsByID[repo.accounts[i].ID] = &repo.accounts[i]
	}
	svc := &GatewayService{accountRepo: repo, cfg: testConfig()}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "gpt-5", PlatformOpenAI)

	require.False(t, diag.HasAccountsInPool, "OpenAI route must not see Anthropic accounts in pool")
	require.False(t, diag.HasModelSupport)
}
