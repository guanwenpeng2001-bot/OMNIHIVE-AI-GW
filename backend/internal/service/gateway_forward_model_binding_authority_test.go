package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/testutil/testassert"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestForwardModelBindingUsesSelectedWireFamily(t *testing.T) {
	for _, scenario := range []struct {
		name, platform, kind, protocol, ingress, model, endpoint string
	}{
		{"adaptive kimi messages", PlatformKimi, AccountTypeAPIKey, APIProtocolAdaptive, "anthropic", "k3", "messages"},
		{"adaptive qianwen messages", PlatformQianwen, AccountTypeAPIKey, APIProtocolAdaptive, "anthropic", "qwen-plus", "messages"},
		{"grok missing task", PlatformGrok, AccountTypeAPIKey, "", "chat_completions", "grok-4.6", "chat_completions"},
		{"cursor oauth chat", PlatformCursor, AccountTypeOAuth, "", "chat_completions", "composer-2.5", "chat_completions"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			account := &Account{ID: 51, Platform: scenario.platform, Type: scenario.kind, Status: StatusActive, Schedulable: true,
				Credentials: map[string]any{"api_protocol": scenario.protocol, "model_mapping": map[string]string{scenario.model: scenario.model}}, Extra: map[string]any{}}
			catalog := projectAccountModelCatalog(account, &UpstreamModelCatalog{Models: []string{scenario.model}})
			account.Extra[UpstreamModelCatalogExtraKey] = catalog
			require.Contains(t, catalog.EligibleModels[0].Endpoints, scenario.endpoint)
			ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Model: scenario.model, Protocol: scenario.ingress}))
			require.Equal(t, scenario.endpoint, openAICompatiblePlanCatalogEndpoint(ctx, account))
			require.True(t, (&OpenAIGatewayService{}).supportsOpenAIAccountModel(ctx, account, scenario.model, OpenAIEndpointCapabilityChatCompletions))
			options := AccountModelBindingOptions{Endpoint: scenario.endpoint, FinalModel: scenario.model}
			require.NoError(t, validateAccountForwardModelBinding(ctx, nil, account, options))
			require.False(t, AccountSupportsResolvedModel(ctx, account, "unselected", AccountModelBindingOptions{Endpoint: scenario.endpoint}))
			options.FinalModel = "unselected"
			require.ErrorIs(t, validateAccountForwardModelBinding(ctx, nil, account, options), ErrAccountModelUnsupported)
			options.FinalModel = scenario.model
			account.Schedulable = false
			require.ErrorIs(t, validateAccountForwardModelBinding(ctx, nil, account, options), ErrAccountModelUnsupported)
			account.Schedulable = true
			account.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{Result: "credentials_invalid"}
			require.NoError(t, validateAccountForwardModelBinding(ctx, nil, account, options), "model-list authentication failure does not revoke the saved generation selection")
			delete(account.Extra, UpstreamModelCatalogStateExtraKey)
			// Existing snapshots from before this fix must work without discovery.
			stored := catalogEvidenceAccount(account.ID, account.Platform, scenario.model)
			stale := testassert.Type[*AccountModelCatalogSnapshot](t, stored.Extra[UpstreamModelCatalogExtraKey])
			stale.EligibleModels[0].Endpoints = nil
			account.Extra = stored.Extra
			require.NoError(t, validateAccountForwardModelBinding(ctx, nil, account, options))
			require.Nil(t, stale.EligibleModels[0].Endpoints, "effective endpoints do not mutate stored snapshots")
			account.Status = "inactive"
			require.ErrorIs(t, validateAccountForwardModelBinding(ctx, nil, account, options), ErrAccountModelUnsupported)
			account.Status = StatusActive
			account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true}
			require.ErrorIs(t, validateAccountForwardModelBinding(ctx, nil, account, options), ErrAccountModelUnsupported)
		})
	}
}

func TestForwardModelBindingFixedProtocolStillRejectsOtherWireFamilies(t *testing.T) {
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolResponses, APIProtocolAnthropic} {
		t.Run(protocol, func(t *testing.T) {
			account := &Account{ID: 56, Platform: PlatformQianwen, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
				Credentials: map[string]any{"api_protocol": protocol, "model_mapping": map[string]string{"qwen-plus": "qwen-plus"}}}
			for _, endpoint := range []string{"chat_completions", "responses", "messages"} {
				options := AccountModelBindingOptions{FinalModel: "qwen-plus", Endpoint: endpoint}
				require.Equal(t, endpoint == catalogProtocolEndpoint(protocol), AccountSupportsResolvedModel(context.Background(), account, "qwen-plus", options), endpoint)
			}
			// Stale broad endpoint declarations cannot override a fixed wire protocol.
			declared := catalogEvidenceAccount(account.ID, account.Platform, "qwen-plus")
			account.Extra = declared.Extra
			for _, endpoint := range []string{"chat_completions", "responses", "messages"} {
				require.Equal(t, endpoint == catalogProtocolEndpoint(protocol), AccountSupportsResolvedModel(context.Background(), account, "qwen-plus", AccountModelBindingOptions{FinalModel: "qwen-plus", Endpoint: endpoint}), endpoint)
			}
		})
	}
}

func TestForwardModelBindingCursorStoredResponsesDeclarationUsesChat(t *testing.T) {
	account := catalogEvidenceAccount(50, PlatformCursor, "composer-2.5")
	account.Type = AccountTypeOAuth
	snapshot := testassert.Type[*AccountModelCatalogSnapshot](t, account.Extra[UpstreamModelCatalogExtraKey])
	snapshot.EligibleModels[0].Endpoints = []string{"responses"}
	ctx := context.Background()
	require.Equal(t, "chat_completions", gatewaySelectionCatalogEndpoint(ctx, &account))
	require.True(t, AccountSupportsResolvedModel(ctx, &account, "composer-2.5", AccountModelBindingOptions{Endpoint: "chat_completions"}))
	require.Equal(t, []string{"responses"}, snapshot.EligibleModels[0].Endpoints)
	account.Type = AccountTypeUpstream
	require.False(t, AccountSupportsResolvedModel(ctx, &account, "composer-2.5", AccountModelBindingOptions{Endpoint: "chat_completions"}))
}

func TestAccountCatalogUnknownTaskDoesNotDenyText(t *testing.T) {
	account := &Account{Platform: PlatformGrok, Type: AccountTypeAPIKey}
	for _, task := range []string{"", "unknown"} {
		metadata := UpstreamModelMetadata{TaskType: task, ReasoningControl: &ReasoningControl{Kind: "unknown"}}
		require.Equal(t, []string{"chat_completions"}, accountCatalogModelEndpoints(account, "grok-4.6", metadata))
		require.Nil(t, metadata.SupportsFunctionCalling)
		require.Equal(t, "unknown", metadata.ReasoningControl.Kind)
	}
	require.NotContains(t, accountCatalogModelEndpoints(account, "grok-4.6", UpstreamModelMetadata{TaskType: "image_generation"}), "chat_completions")
}

func TestForwardModelBindingSavedCatalogWithoutMappingUsesWireFamily(t *testing.T) {
	for _, scenario := range []struct {
		platform, model, endpoint string
		stored                    []string
	}{
		{PlatformGrok, "grok-4.6", "chat_completions", nil},
		{PlatformKimi, "k3", "messages", []string{"chat_completions", "responses"}},
		{PlatformQianwen, "qwen-plus", "messages", []string{"chat_completions", "responses"}},
	} {
		t.Run(scenario.platform, func(t *testing.T) {
			account := catalogEvidenceAccount(52, scenario.platform, scenario.model)
			if scenario.platform != PlatformGrok {
				account.Credentials["api_protocol"] = APIProtocolAdaptive
			}
			snapshot := testassert.Type[*AccountModelCatalogSnapshot](t, account.Extra[UpstreamModelCatalogExtraKey])
			snapshot.EligibleModels[0].Endpoints = scenario.stored
			options := AccountModelBindingOptions{Endpoint: scenario.endpoint, FinalModel: scenario.model}
			require.True(t, AccountSupportsResolvedModel(context.Background(), &account, scenario.model, options))
			require.Equal(t, scenario.stored, snapshot.EligibleModels[0].Endpoints)
			options.FinalModel = "unselected"
			require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "unselected", options))
		})
	}
}

func TestForwardModelBindingManualMediaSelectionUsesInstalledAdapter(t *testing.T) {
	for _, endpoint := range []string{"images", "embeddings", "speech", "transcriptions"} {
		for _, task := range []string{"", "unknown", "text_generation"} {
			t.Run(endpoint+"/"+task, func(t *testing.T) {
				platform := PlatformOpenAI
				if endpoint == "speech" || endpoint == "transcriptions" {
					platform = PlatformQianwen
				}
				account := legacyCatalogSeedAccount(platform)
				account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{"actual-model": {TaskType: task}}})
				require.True(t, platformActionAdapterBound(&account, chatOnlyCatalogEndpointAction(endpoint)))
				options := AccountModelBindingOptions{Endpoint: endpoint, FinalModel: "actual-model"}
				require.True(t, AccountSupportsResolvedModel(context.Background(), &account, "public-model", options))
				options.FinalModel = "unselected"
				require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "unselected", options))
			})
		}
	}
	account := legacyCatalogSeedAccount(PlatformCommandCode)
	require.False(t, AccountSupportsResolvedModel(context.Background(), &account, "public-model", AccountModelBindingOptions{Endpoint: "images", FinalModel: "actual-model"}))
}

func TestForwardModelBindingRejectsRevokedCurrentAccount(t *testing.T) {
	for _, change := range []string{"deleted", "disabled", "credential identity revoked", "selection revision changed", "selection removed"} {
		t.Run(change, func(t *testing.T) {
			selected := legacyCatalogSeedAccount(PlatformKimi)
			current := legacyCatalogSeedAccount(PlatformKimi)
			repo := &forwardCatalogRepo{current: &current}
			switch change {
			case "deleted":
				repo.current = nil
			case "disabled":
				current.Status = "inactive"
			case "credential identity revoked":
				current.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{IdentityRevision: 1}
			case "selection revision changed":
				current.Extra[UpstreamModelCatalogStateExtraKey] = AccountModelCatalogState{RulesRevision: 1}
			case "selection removed":
				current.Credentials["model_mapping"] = map[string]string{}
				NormalizeEmptyAccountModelSelection(&current)
			}
			ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{Model: "public-model", Protocol: "anthropic"}))
			require.ErrorIs(t, validateAccountForwardModelBinding(ctx, repo, &selected, AccountModelBindingOptions{FinalModel: "actual-model", Endpoint: "messages"}), ErrAccountModelUnsupported)
		})
	}
}
