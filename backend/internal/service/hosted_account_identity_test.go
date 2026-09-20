package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHostedAccountMigrationPreservesBedrockSigningAndModels(t *testing.T) {
	legacy := &Account{ID: 71, Platform: PlatformBedrock, Type: AccountTypeBedrock,
		Credentials: map[string]any{"aws_access_key_id": "test-access", "aws_secret_access_key": "test-secret", "aws_session_token": "test-session", "aws_region": "eu-west-1", "model_mapping": map[string]any{"nova": "amazon.nova-pro-v1:0"}}}
	migrated := *legacy
	migrated.Platform = PlatformAnthropic
	migrated.Extra = map[string]any{"hosted_bedrock_runtime": "converse"}
	var signatures []string
	for _, account := range []*Account{legacy, &migrated} {
		require.True(t, account.IsBedrock())
		require.True(t, account.UsesBedrockCatalog())
		model, ok := ResolveBedrockModelID(account, "nova")
		require.True(t, ok)
		require.Equal(t, "amazon.nova-pro-v1:0", model)
		signer, err := NewBedrockSignerFromAccount(account)
		require.NoError(t, err)
		body := `{"messages":[]}`
		request, err := http.NewRequest(http.MethodPost, "https://bedrock-runtime.eu-west-1.amazonaws.com/model/amazon.nova-pro-v1:0/converse", strings.NewReader(body))
		require.NoError(t, err)
		require.NoError(t, signer.signer.SignHTTP(context.Background(), signer.credentials, request, sha256Hash([]byte(body)), "bedrock", signer.region, time.Unix(1700000000, 0)))
		signatures = append(signatures, request.Header.Get("Authorization"))
		require.Equal(t, "test-session", request.Header.Get("X-Amz-Security-Token"))
	}
	require.Equal(t, signatures[0], signatures[1])
	require.Equal(t, legacy.ID, migrated.ID)
	require.Equal(t, legacy.Credentials, migrated.Credentials)
}

func TestHostedGroupAndCompositeRestrictionsSurviveMigration(t *testing.T) {
	hosted := &Account{Platform: PlatformAnthropic, Type: AccountTypeBedrock}
	direct := &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey}
	for _, group := range []*Group{{Platform: PlatformBedrock}, {Platform: PlatformAnthropic, AccountType: AccountTypeBedrock}} {
		require.True(t, group.AllowsAccount(hosted))
		require.False(t, group.AllowsAccount(direct))
	}
	for _, decision := range []CompositeRouteDecision{
		{Matched: true, TargetPlatform: PlatformBedrock},
		{Matched: true, TargetPlatform: PlatformAnthropic, AccountType: AccountTypeBedrock},
	} {
		ctx := WithCompositeRouteDecision(context.Background(), decision)
		require.Equal(t, AccountTypeBedrock, RequiredRouteAccountType(ctx))
	}
}

func TestHostedQuotaIdentityPreservesExistingBudgets(t *testing.T) {
	for _, example := range []struct{ platform, accountType, quota string }{
		{PlatformBedrock, "", PlatformBedrock},
		{PlatformAnthropic, AccountTypeBedrock, PlatformBedrock},
		{PlatformAnthropic, "", PlatformAnthropic},
		{"vertex", "", "vertex"},
		{PlatformGemini, AccountTypeServiceAccount, "vertex"},
	} {
		apiKey := &APIKey{Group: &Group{Platform: example.platform, AccountType: example.accountType}}
		require.Equal(t, example.quota, QuotaPlatform(context.Background(), apiKey))
		require.Equal(t, example.quota, PlatformFromAPIKey(apiKey))
		composite := &APIKey{Group: &Group{Platform: PlatformComposite}}
		ctx := WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{Matched: true, TargetPlatform: example.platform, AccountType: example.accountType})
		require.Equal(t, example.quota, QuotaPlatform(ctx, composite))
	}
}

func TestHostedSelectionRejectsDirectAccountsForMigratedRoutes(t *testing.T) {
	gateway := &GatewayService{}
	hosted := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeBedrock, Status: StatusActive, Schedulable: true}
	legacy := *hosted
	legacy.Platform = PlatformBedrock
	direct := &Account{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	for _, decision := range []CompositeRouteDecision{
		{Matched: true, TargetPlatform: PlatformBedrock},
		{Matched: true, TargetPlatform: PlatformAnthropic, AccountType: AccountTypeBedrock},
	} {
		ctx := WithCompositeRouteDecision(context.Background(), decision)
		require.True(t, gateway.isAccountAllowedForPlatform(&legacy, decision.TargetPlatform, false))
		require.True(t, gateway.isAccountSchedulableForModelSelection(ctx, hosted, ""))
		require.True(t, gateway.isAccountSchedulableForModelSelection(ctx, &legacy, ""))
		require.False(t, gateway.isAccountSchedulableForModelSelection(ctx, direct, ""))
		for _, group := range []*Group{{ID: 1, Hydrated: true, Status: StatusActive, Platform: PlatformBedrock}, {ID: 1, Hydrated: true, Status: StatusActive, Platform: PlatformAnthropic, AccountType: AccountTypeBedrock}} {
			groupContext := gateway.withGroupContext(context.Background(), group)
			require.True(t, gateway.isAccountSchedulableForModelSelection(groupContext, hosted, ""))
			require.False(t, gateway.isAccountSchedulableForModelSelection(groupContext, direct, ""))
			require.True(t, group.AllowsAccount(&legacy))
			require.False(t, group.AllowsAccount(direct))
		}
	}
}

func TestHostedRouteValidationRejectsConflictingSubtypes(t *testing.T) {
	for _, input := range []CompositeRouteInput{
		{PublicModel: "public", TargetPlatform: PlatformBedrock, AccountType: AccountTypeServiceAccount},
		{PublicModel: "public", TargetPlatform: PlatformOpenAI, AccountType: AccountTypeBedrock},
		{PublicModel: "public", TargetPlatform: PlatformAnthropic, AccountType: AccountTypeAPIKey},
	} {
		_, err := compositeRouteFromInput(1, input)
		require.Error(t, err)
	}
	for _, input := range []CompositeRouteInput{
		{PublicModel: "public", TargetPlatform: PlatformBedrock},
		{PublicModel: "public", TargetPlatform: PlatformAnthropic, AccountType: AccountTypeBedrock},
		{PublicModel: "public", TargetPlatform: PlatformGemini, AccountType: AccountTypeServiceAccount},
	} {
		route, err := compositeRouteFromInput(1, input)
		require.NoError(t, err)
		require.Equal(t, input.AccountType, route.AccountType)
	}
}

func TestHostedAuthSnapshotRoundTripPreservesRestrictionsAndQuotaKey(t *testing.T) {
	auth := &APIKeyService{}
	for _, group := range []*Group{
		{ID: 7, Platform: PlatformBedrock},
		{ID: 7, Platform: PlatformAnthropic, AccountType: AccountTypeBedrock},
		{ID: 7, Platform: PlatformGemini, AccountType: AccountTypeServiceAccount},
	} {
		original := &APIKey{ID: 8, UserID: 9, User: &User{ID: 9}, Group: group}
		snapshot := auth.snapshotFromAPIKey(context.Background(), original)
		encoded, err := json.Marshal(snapshot)
		require.NoError(t, err)
		var decoded APIKeyAuthSnapshot
		require.NoError(t, json.Unmarshal(encoded, &decoded))
		restored := auth.snapshotToAPIKey("fixture", &decoded)
		require.Equal(t, group.Platform, restored.Group.Platform)
		require.Equal(t, group.AccountType, restored.Group.AccountType)
		require.Equal(t, group.RequiredAccountType(), restored.Group.RequiredAccountType())
		require.Equal(t, QuotaPlatform(context.Background(), original), QuotaPlatform(context.Background(), restored))
		direct := &Account{Platform: group.Platform, Type: AccountTypeAPIKey}
		require.False(t, restored.Group.AllowsAccount(direct))
	}
}

func TestHostedModelBindingDoesNotBroadenVisibility(t *testing.T) {
	model := "anthropic.claude-sonnet-fixture"
	hosted := catalogEvidenceAccount(1, PlatformBedrock, model)
	hosted.Type = AccountTypeBedrock
	migrated := hosted
	migrated.Platform = PlatformAnthropic
	migrated.Extra = map[string]any{UpstreamModelCatalogExtraKey: hosted.Extra[UpstreamModelCatalogExtraKey], "hosted_bedrock_runtime": "converse"}
	direct := catalogEvidenceAccount(2, PlatformAnthropic, model)
	for _, candidate := range []*Account{&hosted, &migrated} {
		ctx := WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{Matched: true, TargetPlatform: PlatformAnthropic, AccountType: AccountTypeBedrock})
		options := AccountModelBindingOptions{Endpoint: "messages", FinalModel: model}
		require.True(t, ResolveAccountModelBinding(ctx, candidate, model, options).Supported)
		require.False(t, ResolveAccountModelBinding(ctx, &direct, model, options).Supported)
	}
	channel := &Channel{GroupIDs: []int64{3}}
	candidates, err := BuildPlazaCandidates(channel, 3, PlatformBedrock, []Account{hosted, direct})
	require.NoError(t, err)
	require.Len(t, candidates.Candidates, 1)
	require.Equal(t, PlatformBedrock, candidates.Candidates[0].Platform)
}

func TestHostedAuthRejectsSnapshotWithoutSubtype(t *testing.T) {
	auth := &APIKeyService{}
	_, accepted, err := auth.applyAuthCacheEntry("fixture", &APIKeyAuthCacheEntry{Snapshot: &APIKeyAuthSnapshot{Version: 24}})
	require.NoError(t, err)
	require.False(t, accepted)
}

func TestHostedVertexAuthenticationIdentityIsUnchanged(t *testing.T) {
	credentials := map[string]any{"service_account_json": `{"type":"service_account","project_id":"fixture-project","private_key_id":"fixture-key","private_key":"fixture-private-key","client_email":"fixture@example.invalid"}`, "location": "europe-west1"}
	legacy := &Account{ID: 70, Platform: "vertex", Type: AccountTypeServiceAccount, Credentials: credentials}
	migrated := *legacy
	migrated.Platform = PlatformGemini
	legacyKey, err := parseVertexServiceAccountKey(legacy)
	require.NoError(t, err)
	migratedKey, err := parseVertexServiceAccountKey(&migrated)
	require.NoError(t, err)
	require.Equal(t, legacyKey, migratedKey)
	require.Equal(t, legacy.ID, migrated.ID)
	require.True(t, legacy.IsGemini())
	require.True(t, migrated.IsVertexServiceAccount())
	legacyURL, err := buildVertexGeminiURL(legacy.VertexProjectID(), legacy.VertexLocation("fixture"), "fixture", "generateContent", false)
	require.NoError(t, err)
	migratedURL, err := buildVertexGeminiURL(migrated.VertexProjectID(), migrated.VertexLocation("fixture"), "fixture", "generateContent", false)
	require.NoError(t, err)
	require.Equal(t, legacyURL, migratedURL)
}
