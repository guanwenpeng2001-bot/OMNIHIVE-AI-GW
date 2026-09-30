package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestConfiguredSelectionSurvivesDiscoveryAndMetadata(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "discovered")
	account.Credentials["model_mapping"] = map[string]string{"alias": "manual-N+1"}
	account.Extra["openai_responses_supported"] = true
	disabled := false
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{"manual-N+1": {SupportsVision: &disabled, SupportedMethods: []string{}}}})
	for _, result := range []string{"", "failed", "credentials_invalid"} {
		account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{Result: result}
		binding := ResolveAccountModelBinding(context.Background(), &account, "alias", AccountModelBindingOptions{Endpoint: "responses"})
		require.True(t, binding.Supported, result)
		require.Equal(t, "manual-N+1", binding.UpstreamModel)
		require.Empty(t, binding.EvidenceObservedAt)
		require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "discovered", AccountModelBindingOptions{}))
	}
	account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{}}
	require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "alias", AccountModelBindingOptions{}))
	delete(account.Extra, UpstreamModelAllowlistExtraKey)
	account.Credentials["model_mapping"] = map[string]string{"other": "other"}
	require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "alias", AccountModelBindingOptions{}))
}

func TestConfiguredSelectionDoesNotMutateStoredEvidence(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "selected")
	account.Credentials["model_mapping"] = map[string]string{"alias": "selected"}
	snapshot := account.GetUpstreamModelCatalog()
	original := append([]string{}, snapshot.EligibleModels[0].Endpoints...)
	projected := account.EffectiveUpstreamModelCatalog()
	require.Contains(t, projected.EligibleModels[0].Endpoints, "images")
	require.Equal(t, original, account.GetUpstreamModelCatalog().EligibleModels[0].Endpoints)
	require.Equal(t, "native_account_catalog", projected.EligibleModels[0].EvidenceKind)
}

func TestConfiguredCompactSelectionPrivateAndRevocable(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		account := catalogEvidenceAccount(1, PlatformOpenAI)
		account.Credentials["model_mapping"] = map[string]string{"main": "main"}
		account.Credentials["compact_model_mapping"] = map[string]string{"main": "compact-private"}
		account.Extra["openai_passthrough"] = passthrough
		account.Extra["openai_responses_supported"] = true
		require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "compact-private", AccountModelBindingOptions{Endpoint: "responses"}))
		require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "main", AccountModelBindingOptions{Endpoint: "responses", FinalModel: "compact-private"}))
		require.True(t, AccountSupportsResolvedModel(context.Background(), &account, "main", AccountModelBindingOptions{Endpoint: "responses", Compact: true}))
		ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Model: "main", Action: "chat"}))
		req := httptest.NewRequest(http.MethodPost, "https://fixture.test/v1/responses/compact", strings.NewReader(`{"model":"compact-private"}`)).WithContext(ctx)
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(`{"model":"compact-private"}`)), nil
		}
		require.NoError(t, validateOpenAIForwardRequest(nil, &account, req))
		account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{"main"}}
		require.ErrorIs(t, validateOpenAIForwardRequest(nil, &account, req), ErrAccountModelUnsupported)
		delete(account.Extra, UpstreamModelAllowlistExtraKey)
		delete(account.Credentials, "compact_model_mapping")
		require.ErrorIs(t, validateOpenAIForwardRequest(nil, &account, req), ErrAccountModelUnsupported)
	}
}

func TestConfiguredSelectionPassthroughWhitelist(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "selected", "new-discovery")
	account.Extra["openai_passthrough"] = true
	account.Credentials["model_mapping"] = map[string]string{"selected": "selected"}
	require.True(t, account.IsModelSupported("selected"))
	require.False(t, account.IsModelSupported("new-discovery"))
	require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "new-discovery", AccountModelBindingOptions{}))
	require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "selected", AccountModelBindingOptions{FinalModel: "absent"}))
}

func TestConfiguredSelectionRequestedAliasNormalization(t *testing.T) {
	for _, platform := range []string{PlatformGemini, PlatformAntigravity} {
		t.Run(platform, func(t *testing.T) {
			account := catalogEvidenceAccount(1, platform, "gemini-3.1-pro-preview-customtools", "gemini-other")
			account.Credentials["model_mapping"] = map[string]string{"gemini-3.1-pro-preview": "gemini-3.1-pro-preview"}
			requested := "gemini-3.1-pro-preview-customtools"
			require.True(t, account.IsModelSupported(requested))
			binding := ResolveAccountModelBinding(context.Background(), &account, requested, AccountModelBindingOptions{})
			require.True(t, binding.Supported)
			require.Equal(t, "gemini-3.1-pro-preview", binding.UpstreamModel)
			ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Model: requested, Protocol: "gemini", Action: "chat"}))
			require.NoError(t, validateAccountForwardModel(ctx, nil, &account, binding.UpstreamModel, "generate_content"))
			require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "gemini-other", AccountModelBindingOptions{}))
			account.Credentials["model_mapping"] = map[string]string{requested: "explicit-target", "gemini-3.1-pro-preview": "gemini-3.1-pro-preview"}
			binding = ResolveAccountModelBinding(context.Background(), &account, requested, AccountModelBindingOptions{})
			require.True(t, binding.Supported)
			require.Equal(t, "explicit-target", binding.UpstreamModel)
		})
	}
}

func TestConfiguredSelectionHTTPPassthroughAlias(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "discovered-only", "compact-private")
	account.Credentials["model_mapping"] = map[string]string{"wire-alias": "mapped-target"}
	account.Extra["openai_passthrough"] = true
	account.Extra["openai_responses_supported"] = true
	ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Model: "wire-alias", Protocol: "responses", Action: "responses"}))
	for _, final := range []bool{false, true} {
		options := AccountModelBindingOptions{Endpoint: "responses"}
		if final {
			options.FinalModel = "wire-alias"
		}
		binding := ResolveAccountModelBinding(ctx, &account, "wire-alias", options)
		require.True(t, binding.Supported)
		require.Equal(t, "wire-alias", binding.UpstreamModel)
	}
	require.NoError(t, validateAccountForwardModel(ctx, nil, &account, "wire-alias", "responses"))
	require.True(t, AccountSupportsResolvedModel(ctx, &account, "wire-alias", AccountModelBindingOptions{Endpoint: "responses", Compact: true}), "compact with no remap keeps the selected wire key")
	account.Credentials["compact_model_mapping"] = map[string]string{"wire-alias": "compact-private"}
	compactBinding := ResolveAccountModelBinding(ctx, &account, "wire-alias", AccountModelBindingOptions{Endpoint: "responses", Compact: true})
	require.True(t, compactBinding.Supported)
	require.Equal(t, "compact-private", compactBinding.UpstreamModel)
	for _, unselected := range []string{"discovered-only", "compact-private"} {
		require.ErrorIs(t, validateAccountForwardModel(ctx, nil, &account, unselected, "responses"), ErrAccountModelUnsupported)
	}
	account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{"mapped-target"}}
	require.ErrorIs(t, validateAccountForwardModel(ctx, nil, &account, "wire-alias", "responses"), ErrAccountModelUnsupported)
	account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{"wire-alias"}}
	require.NoError(t, validateAccountForwardModel(ctx, nil, &account, "wire-alias", "responses"))
	account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true}
	require.ErrorIs(t, validateAccountForwardModel(ctx, nil, &account, "wire-alias", "responses"), ErrAccountModelUnsupported)
}

func TestConfiguredSelectionHTTPAliasActualWire(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("passthrough=%t/stream=%t", passthrough, stream), func(t *testing.T) {
				account := catalogEvidenceAccount(1, PlatformOpenAI)
				account.Credentials["model_mapping"] = map[string]string{"wire-alias": "mapped-target"}
				account.Credentials["api_key"] = "fixture"
				account.Credentials["base_url"] = "https://fixture.invalid"
				account.Extra["openai_passthrough"] = passthrough
				account.Extra["openai_responses_supported"] = true
				body := []byte(fmt.Sprintf(`{"model":"wire-alias","stream":%t,"input":"hello"}`, stream))
				responseBody := `{"id":"fixture-response","model":"declared-model","usage":{"input_tokens":2,"output_tokens":1}}`
				contentType := "application/json"
				if stream {
					contentType = "text/event-stream"
					responseBody = "data: {\"type\":\"response.completed\",\"response\":" + responseBody + "}\n\ndata: [DONE]\n\n"
				}
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(responseBody))}}
				gateway := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
				plan := NewGatewayRequestPlan(GatewayRequestSpec{Model: "wire-alias", Protocol: "responses", Action: "responses"})
				installUnlimitedTPMFixture(plan)
				ctx := WithGatewayRequestPlan(context.Background(), plan)
				request, _ := gin.CreateTestContext(httptest.NewRecorder())
				request.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body))).WithContext(ctx)
				request.Request.Header.Set("Content-Type", "application/json")
				result, err := gateway.Forward(ctx, request, &account, body)
				require.NoError(t, err)
				require.Len(t, upstream.bodies, 1)
				expected := "mapped-target"
				if passthrough {
					expected = "wire-alias"
				}
				require.Equal(t, expected, gjson.GetBytes(upstream.lastBody, "model").String())
				require.Equal(t, "declared-model", result.UpstreamResponseModel)
			})
		}
	}
}

func TestConfiguredSelectionHTTPAdapterWireMapping(t *testing.T) {
	for _, responsesSupported := range []bool{false, true} {
		for _, incomingResponses := range []bool{false, true} {
			t.Run(fmt.Sprintf("responses_upstream=%t/responses_ingress=%t", responsesSupported, incomingResponses), func(t *testing.T) {
				account := catalogEvidenceAccount(1, PlatformOpenAI)
				account.Credentials["model_mapping"] = map[string]string{"wire-alias": "mapped-target"}
				account.Credentials["api_key"] = "fixture"
				account.Credentials["base_url"] = "https://fixture.invalid"
				account.Extra["openai_passthrough"] = true
				account.Extra["openai_responses_supported"] = responsesSupported
				body := []byte(`{"model":"wire-alias","messages":[{"role":"user","content":"hello"}]}`)
				protocol, path := "openai", "/v1/chat/completions"
				if incomingResponses {
					body = []byte(`{"model":"wire-alias","input":"hello","stream":false}`)
					protocol, path = "responses", "/v1/responses"
				}
				responseBody := `{"id":"fixture","model":"declared-model","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`
				if responsesSupported {
					responseBody = `{"id":"fixture","model":"declared-model","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":2,"output_tokens":1}}`
				}
				contentType := "application/json"
				if responsesSupported && !incomingResponses {
					contentType = "text/event-stream"
					responseBody = "data: {\"type\":\"response.completed\",\"response\":" + responseBody + "}\n\ndata: [DONE]\n\n"
				}
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(responseBody))}}
				gateway := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
				plan := NewGatewayRequestPlan(GatewayRequestSpec{Model: "wire-alias", Protocol: protocol, Action: "chat"})
				installUnlimitedTPMFixture(plan)
				ctx := WithGatewayRequestPlan(context.Background(), plan)
				request, _ := gin.CreateTestContext(httptest.NewRecorder())
				request.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body))).WithContext(ctx)
				request.Request.Header.Set("Content-Type", "application/json")
				upstreamEndpoint := "chat_completions"
				if responsesSupported {
					upstreamEndpoint = "responses"
				}
				binding := ResolveAccountModelBinding(ctx, &account, "wire-alias", AccountModelBindingOptions{Endpoint: upstreamEndpoint})
				require.True(t, binding.Supported)
				expectedModel := "mapped-target"
				if incomingResponses && responsesSupported {
					expectedModel = "wire-alias"
				}
				require.Equal(t, expectedModel, binding.UpstreamModel)
				if incomingResponses {
					selection := ResolveAccountModelBinding(ctx, &account, "wire-alias", AccountModelBindingOptions{Endpoint: "responses"})
					require.True(t, selection.Supported)
					require.Equal(t, expectedModel, selection.UpstreamModel)
				}
				var err error
				if incomingResponses {
					_, err = gateway.Forward(ctx, request, &account, body)
				} else {
					_, err = gateway.ForwardAsChatCompletions(ctx, request, &account, body, "", "")
				}
				require.NoError(t, err)
				require.Len(t, upstream.bodies, 1)
				require.Equal(t, expectedModel, gjson.GetBytes(upstream.lastBody, "model").String())
			})
		}
	}
}

func TestConfiguredSelectionWebSocketDiagnosisUsesWireScope(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI)
	account.Credentials["model_mapping"] = map[string]string{"wire-name": "ordinary-target"}
	account.Extra["openai_apikey_responses_websockets_v2_mode"] = OpenAIWSIngressModePassthrough
	account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{"wire-name"}}
	ctx := WithOpenAIWebSocketSelection(context.Background(), OpenAIWSIngressModeCtxPool)
	require.True(t, accountModelScopeAllowsDiagnosis(ctx, &account, "wire-name"))
	account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{"ordinary-target"}}
	require.False(t, accountModelScopeAllowsDiagnosis(ctx, &account, "wire-name"))
}

type selectionScopeRecorder struct {
	AccountRepository
	scope   AccountModelAllowlist
	mapping *map[string]string
}

func (repo *selectionScopeRecorder) ReplaceAccountModelSelectionScope(_ context.Context, _ int64, _ AccountModelCatalogBaseline, scope AccountModelAllowlist, mapping *map[string]string) ([]string, error) {
	repo.scope = scope
	repo.mapping = mapping
	return nil, nil
}

func TestConfiguredSelectionAtomicScopeRules(t *testing.T) {
	selected := map[string]string{"alias": "manual"}
	empty := map[string]string{}
	for _, test := range []struct {
		name    string
		mapping *map[string]string
		enabled bool
		models  []string
		deny    bool
		invalid bool
	}{
		{"scope-only cannot unlock", nil, false, nil, true, false},
		{"scope-only nonempty cannot unlock", nil, true, []string{"manual"}, true, false},
		{"explicit reselect", &selected, false, nil, false, false},
		{"explicit restricted reselect", &selected, true, []string{"manual"}, false, false},
		{"empty normalizes", &empty, false, nil, true, false},
		{"contradictory scope", &selected, true, nil, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			account := catalogEvidenceAccount(1, PlatformOpenAI)
			account.Credentials["model_mapping"] = selected
			account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{}}
			repo := &selectionScopeRecorder{}
			svc := &AccountTestService{accountRepo: repo}
			warnings, err := svc.ConfigureAccountModelAllowlist(context.Background(), &account, test.enabled, test.models, "", 7, 0, test.mapping)
			if test.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.deny, repo.scope.Enabled && len(repo.scope.Models) == 0)
			require.NotEmpty(t, repo.scope.Reason)
			if len(test.models) > 0 {
				require.Equal(t, []string{"manual"}, warnings)
			}
		})
	}
}

func TestConfiguredSelectionFreezeUsesOnlyPriorScope(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "old", "restricted")
	account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{"old"}}
	require.Equal(t, map[string]string{"old": "old"}, FrozenAccountModelSelection(&account, account.ModelCatalogBaseline()))
	delete(account.Extra, UpstreamModelCatalogExtraKey)
	require.Empty(t, FrozenAccountModelSelection(&account, account.ModelCatalogBaseline()))
	NormalizeEmptyAccountModelSelection(&account)
	require.False(t, AccountModelSelectionNeedsFreeze(&account))
	// An ordinary edit can save draft selections but cannot remove deny-all.
	account.Credentials["model_mapping"] = map[string]string{"draft": "draft"}
	next := account
	NormalizeAccountModelSelectionUpdate(&account, &next)
	var scope AccountModelAllowlist
	require.True(t, decodeCatalogExtra(next.Extra, UpstreamModelAllowlistExtraKey, &scope))
	require.True(t, scope.Enabled)
	require.Empty(t, scope.Models)
}

func TestConfiguredSelectionDirectory401RetainsSelection(t *testing.T) {
	account := catalogEvidenceAccount(42, PlatformOpenAI, "discovered")
	account.Credentials["api_key"] = "fixture"
	account.Credentials["model_mapping"] = map[string]any{"manual": "manual"}
	repo := &catalogFixtureRepository{accounts: map[int64]*Account{42: &account}}
	svc := &AccountTestService{accountRepo: repo, cfg: upstreamModelSyncTestConfig(), httpUpstream: &httpUpstreamRecorder{resp: &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"catalog denied"}}`))}}}
	_, err := svc.ApplyAccountModelCatalog(context.Background(), &account, nil)
	require.Error(t, err)
	require.Equal(t, "credentials_invalid", account.ModelCatalogState().Result)
	for _, stream := range []bool{false, true} {
		ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Model: "manual", Action: "chat"}))
		body := `{"model":"manual","stream":false}`
		if stream {
			body = `{"model":"manual","stream":true}`
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://fixture.test/v1/chat/completions", strings.NewReader(body))
		require.NoError(t, validateOpenAIForwardRequest(nil, &account, req))
	}
	require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "discovered", AccountModelBindingOptions{}))
}

func TestConfiguredCompactSelectionTriggerAndSharedTarget(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI)
	account.Extra["openai_responses_supported"] = true
	account.Credentials["model_mapping"] = map[string]any{"main": "main"}
	account.Credentials["compact_model_mapping"] = map[string]any{"main": "compact-private"}
	ctx := WithGatewayRequestPlan(WithOpenAIForwardModel(context.Background(), "main", false), NewGatewayRequestPlan(GatewayRequestSpec{Model: "main", Action: "chat"}))
	body := `{"model":"compact-private","input":[{"type":"compaction_trigger"}]}`
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://fixture.test/v1/responses", strings.NewReader(body))
	require.NoError(t, validateOpenAIForwardRequest(nil, &account, req))
	account.Credentials["compact_model_mapping"] = map[string]any{"main": "main"}
	snapshot := account.EffectiveUpstreamModelCatalog()
	require.Len(t, snapshot.EligibleModels, 1)
	require.Equal(t, ModelEvidenceConfiguredSelection, snapshot.EligibleModels[0].EvidenceKind)
}

func TestConfiguredSelectionVariantCannotBorrowDiscovery(t *testing.T) {
	account := approvedAntigravityVariantAccount()
	account.Credentials["model_mapping"] = map[string]any{"selected": "selected"}
	_, ok := resolveGeminiThinkingVariantForLevel(account, "gemini-3-flash", "high")
	require.False(t, ok)
	account.Credentials["model_mapping"] = map[string]any{"gemini-3-flash-low": "gemini-3-flash-low"}
	target, ok := resolveGeminiThinkingVariantForLevel(account, "gemini-3-flash", "high")
	require.True(t, ok)
	require.Equal(t, "gemini-3-flash-low", target)
}

func TestConfiguredSelectionShadowFreezeUsesOwnCatalog(t *testing.T) {
	parent := catalogEvidenceAccount(8, PlatformOpenAI, "parent")
	shadow := catalogEvidenceAccount(9, PlatformOpenAI, "shadow")
	shadow.ParentAccountID = &parent.ID
	snapshot := shadow.GetUpstreamModelCatalog()
	snapshot.CredentialAccountID = parent.ID
	snapshot.CredentialIdentityRevision = 3
	shadow.Extra[UpstreamModelCatalogExtraKey] = snapshot
	baseline := shadow.ModelCatalogBaseline()
	baseline.CredentialAccountID = parent.ID
	baseline.CredentialIdentityRevision = 3
	require.True(t, AccountModelSelectionNeedsFreeze(&shadow))
	require.Equal(t, map[string]string{"shadow": "shadow"}, FrozenAccountModelSelection(&shadow, baseline))
	baseline.CredentialIdentityRevision++
	require.Empty(t, FrozenAccountModelSelection(&shadow, baseline))
}

func TestConfiguredSelectionPreparedProtocolNames(t *testing.T) {
	for _, test := range []struct{ platform, accountType, target string }{
		{PlatformGemini, AccountTypeAPIKey, "models/gemini-private"},
		{PlatformAnthropic, AccountTypeOAuth, "claude-sonnet-4-5"},
	} {
		account := catalogEvidenceAccount(1, test.platform)
		account.Type = test.accountType
		account.Credentials["model_mapping"] = map[string]any{"alias": test.target}
		require.True(t, AccountSupportsResolvedModel(context.Background(), &account, "alias", AccountModelBindingOptions{}))
		prepareCatalogReadAccount(&account, nil)
		ctx := context.WithValue(context.Background(), visibleModelRevisionKey{}, visibleModelRevision{generation: 1, authoritative: true})
		require.True(t, AccountSupportsResolvedModel(ctx, &account, "alias", AccountModelBindingOptions{}))
	}
}

func TestConfiguredSelectionAvailabilityKeepsIdentityAndRevocationChecks(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI)
	account.Credentials["model_mapping"] = map[string]any{"alias": "manual"}
	declaration := ModelAvailabilityDeclaration{AccountID: 1, Model: "manual", Endpoint: "chat_completions", ConfirmedAt: time.Now().Add(-time.Minute), ConfirmedBy: 7, Reason: "fixture", EvidenceKind: ModelEvidenceAdministrator}
	account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{RulesRevision: 9}
	require.True(t, declaration.ValidFor(&account, nil, time.Now()), "unrelated rule edits do not invalidate a live selection")
	account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 1}
	require.False(t, declaration.ValidFor(&account, nil, time.Now()))
	delete(account.Extra, UpstreamModelCatalogStateExtraKey)
	revoked := time.Now()
	declaration.RevokedAt = &revoked
	require.False(t, declaration.ValidFor(&account, nil, time.Now()))
	declaration.RevokedAt = nil
	delete(account.Credentials, "model_mapping")
	require.False(t, declaration.ValidFor(&account, nil, time.Now()))
}

func TestConfiguredSelectionFinalModelCannotBorrowDiscovery(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "selected", "discovered-only")
	account.Credentials["model_mapping"] = map[string]any{"alias": "selected"}
	require.True(t, AccountSupportsResolvedModel(context.Background(), &account, "alias", AccountModelBindingOptions{FinalModel: "selected"}))
	require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "alias", AccountModelBindingOptions{FinalModel: "discovered-only"}))
	require.True(t, AccountSupportsResolvedModel(context.Background(), &account, "admin-public", AccountModelBindingOptions{FinalModel: "discovered-only", MessagesDispatchModel: "discovered-only"}))
	require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "admin-public", AccountModelBindingOptions{FinalModel: "selected-other", MessagesDispatchModel: "discovered-only"}))
}

func TestConfiguredSelectionPassthroughWireBoundary(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "discovered-only", "compact-private")
	account.Credentials["model_mapping"] = map[string]string{"channel-name": "ordinary-target"}
	account.Credentials["compact_model_mapping"] = map[string]string{"channel-name": "compact-private"}
	account.Extra["openai_responses_supported"] = true
	ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Model: "public-alias", Protocol: "responses", Action: "responses"}))
	require.NoError(t, validateAccountPassthroughForwardModel(ctx, nil, &account, "channel-name"))
	require.ErrorIs(t, validateAccountForwardModel(ctx, nil, &account, "channel-name", "responses"), ErrAccountModelUnsupported,
		"ordinary FinalModel cannot borrow the passthrough exception")
	for _, unselected := range []string{"discovered-only", "compact-private"} {
		require.ErrorIs(t, validateAccountPassthroughForwardModel(ctx, nil, &account, unselected), ErrAccountModelUnsupported)
	}
	account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{"ordinary-target"}}
	require.ErrorIs(t, validateAccountPassthroughForwardModel(ctx, nil, &account, "channel-name"), ErrAccountModelUnsupported)
	account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{"channel-name"}}
	require.NoError(t, validateAccountPassthroughForwardModel(ctx, nil, &account, "channel-name"))
	account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true}
	require.ErrorIs(t, validateAccountPassthroughForwardModel(ctx, nil, &account, "channel-name"), ErrAccountModelUnsupported)
	delete(account.Extra, UpstreamModelAllowlistExtraKey)
	account.Credentials["model_mapping"] = map[string]string{"other": "ordinary-target"}
	require.ErrorIs(t, validateAccountPassthroughForwardModel(ctx, nil, &account, "channel-name"), ErrAccountModelUnsupported)
	account.Credentials["model_mapping"] = map[string]string{"channel-name": "ordinary-target"}
	account.Schedulable = false
	require.ErrorIs(t, validateAccountPassthroughForwardModel(ctx, nil, &account, "channel-name"), ErrAccountModelUnsupported)
}

func TestConfiguredSelectionWebSocketModeDoesNotBorrowHTTPMetadata(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI)
	account.Credentials["model_mapping"] = map[string]string{"wire-name": "ordinary-target"}
	account.Extra["openai_apikey_responses_websockets_v2_mode"] = OpenAIWSIngressModePassthrough
	account.Extra["openai_responses_supported"] = false
	ctx := WithOpenAIWebSocketSelection(context.Background(), OpenAIWSIngressModeCtxPool)
	options := AccountModelBindingOptions{Endpoint: "responses"}
	ws := ResolveAccountModelBinding(ctx, &account, "wire-name", options)
	require.True(t, ws.Supported)
	require.Equal(t, "wire-name", ws.UpstreamModel)
	ordinary := ResolveAccountModelBinding(context.Background(), &account, "wire-name", options)
	require.Equal(t, "ordinary-target", ordinary.UpstreamModel, "HTTP still applies the account mapping")
	account.Extra["openai_apikey_responses_websockets_v2_mode"] = OpenAIWSIngressModeCtxPool
	pooled := ResolveAccountModelBinding(ctx, &account, "wire-name", options)
	require.Equal(t, "ordinary-target", pooled.UpstreamModel, "ctx-pool retains its mapped-wire protocol")
	require.False(t, AccountSupportsResolvedModel(ctx, &account, "wire-name", AccountModelBindingOptions{Endpoint: "responses", FinalModel: "wire-name"}))
}

func TestConfiguredSelectionHTTPPassthroughDoesNotChangeOtherTransports(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI)
	account.Credentials["model_mapping"] = map[string]string{"wire-name": "mapped-target"}
	account.Extra["openai_passthrough"] = true
	account.Extra["openai_responses_supported"] = true
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModeOff, "legacy"} {
		t.Run(mode, func(t *testing.T) {
			account.Extra["openai_apikey_responses_websockets_v2_mode"] = mode
			ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Model: "wire-name", Protocol: "responses", ExecutionKind: "turn"}))
			if mode != "legacy" {
				ctx = WithOpenAIWebSocketSelection(ctx, OpenAIWSIngressModeCtxPool)
			}
			binding := ResolveAccountModelBinding(ctx, &account, "wire-name", AccountModelBindingOptions{Endpoint: "responses"})
			require.True(t, binding.Supported)
			require.Equal(t, "mapped-target", binding.UpstreamModel)
			require.ErrorIs(t, validateAccountForwardModel(ctx, nil, &account, "wire-name", "responses"), ErrAccountModelUnsupported)
			require.NoError(t, validateAccountForwardModel(ctx, nil, &account, "mapped-target", "responses"))
			account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{"mapped-target"}}
			require.True(t, accountModelScopeAllowsDiagnosis(ctx, &account, "wire-name"))
			delete(account.Extra, UpstreamModelAllowlistExtraKey)
		})
	}
	for _, endpoint := range []string{"chat_completions", "messages", "embeddings"} {
		ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Model: "wire-name", Protocol: map[string]string{"chat_completions": "openai", "messages": "anthropic", "embeddings": "embeddings"}[endpoint]}))
		binding := ResolveAccountModelBinding(ctx, &account, "wire-name", AccountModelBindingOptions{Endpoint: endpoint})
		require.Equal(t, "mapped-target", binding.UpstreamModel)
		require.ErrorIs(t, validateAccountForwardModel(ctx, nil, &account, "wire-name", endpoint), ErrAccountModelUnsupported)
	}
}

func TestConfiguredSelectionPassthroughTransportDoesNotShareNameMemo(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI)
	account.Credentials["model_mapping"] = map[string]string{"wire-*": "mapped-target"}
	account.Extra["openai_passthrough"] = true
	account.Extra["openai_responses_supported"] = true
	account.Extra["openai_apikey_responses_websockets_v2_mode"] = OpenAIWSIngressModeCtxPool
	prepareCatalogReadAccount(&account, nil)
	ctx := context.WithValue(context.Background(), visibleModelRevisionKey{}, visibleModelRevision{generation: 1, authoritative: true})
	options := AccountModelBindingOptions{Endpoint: "responses"}
	require.False(t, ResolveAccountModelBinding(ctx, &account, "wire-name", options).Supported, "wildcard is not an exact selected passthrough wire key")
	websocket := WithOpenAIWebSocketSelection(ctx, OpenAIWSIngressModeCtxPool)
	binding := ResolveAccountModelBinding(websocket, &account, "wire-name", options)
	require.True(t, binding.Supported)
	require.Equal(t, "mapped-target", binding.UpstreamModel)
	require.False(t, ResolveAccountModelBinding(ctx, &account, "wire-name", options).Supported)
}

func TestConfiguredSelectionMessagesDispatchSurvivesForwardBoundary(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "selected", "dispatched")
	account.Credentials["model_mapping"] = map[string]any{"alias": "selected"}
	group := &Group{Platform: PlatformOpenAI, AllowMessagesDispatch: true, MessagesDispatchModelConfig: OpenAIMessagesDispatchModelConfig{ExactModelMappings: map[string]string{"admin-public": "dispatched"}}}
	requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	requestContext.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"admin-public"}`))
	requestContext.Request.Header.Set("Content-Type", "application/json")
	requestContext.Set("api_key", &APIKey{Group: group})
	require.True(t, gatewayInputAdmission(requestContext, &GatewayService{}))
	ctx := WithGatewayRequestPlan(requestContext.Request.Context(), NewGatewayRequestPlan(GatewayRequestSpec{Model: "admin-public", Protocol: "anthropic", Action: "responses"}))
	ctx = WithOpenAIForwardModel(ctx, "dispatched", false)
	require.NoError(t, validateAccountForwardModel(ctx, nil, &account, "dispatched", "responses"))
	ordinary := WithGatewayRequestPlan(requestContext.Request.Context(), NewGatewayRequestPlan(GatewayRequestSpec{Model: "admin-public", Protocol: "responses", Action: "responses"}))
	require.ErrorIs(t, validateAccountForwardModel(ordinary, nil, &account, "dispatched", "responses"), ErrAccountModelUnsupported)
}

func TestConfiguredSelectionApplyFreezeKeepsPostCommitRevision(t *testing.T) {
	account := catalogEvidenceAccount(81, PlatformOpenAI, "old")
	account.Credentials["api_key"] = "fixture"
	repo := &selectionCatalogReadbackRepository{&catalogFixtureRepository{accounts: map[int64]*Account{81: &account}}}
	svc := &AccountTestService{accountRepo: repo, cfg: upstreamModelSyncTestConfig(), httpUpstream: &httpUpstreamRecorder{resp: catalogFixtureResponse(`{"data":[{"id":"new"}]}`)}}
	catalog, err := svc.ApplyAccountModelCatalog(context.Background(), &account, nil)
	require.NoError(t, err)
	require.True(t, catalog.Applied)
	require.EqualValues(t, 1, catalog.RulesRevision)
	require.EqualValues(t, 1, account.ModelCatalogState().RulesRevision)
	require.Equal(t, map[string]string{"old": "old"}, account.GetModelMapping())
	require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "new", AccountModelBindingOptions{}))
	require.True(t, AccountSupportsResolvedModel(context.Background(), &account, "old", AccountModelBindingOptions{}))
}

type selectionCatalogReadbackRepository struct{ *catalogFixtureRepository }

func (repo *selectionCatalogReadbackRepository) GetModelCatalogAccount(_ context.Context, id int64) (*Account, error) {
	return repo.accounts[id], nil
}
func (repo *selectionCatalogReadbackRepository) ListModelCatalogAccounts(context.Context, *int64, []string, bool) ([]Account, error) {
	return nil, nil
}
